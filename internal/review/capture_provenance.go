package review

// Capture provenance binds an automatically captured command to the ReviewPlan
// it was executing against.  The command wrapper remains useful in dirty or
// non-Git diagnostic directories; provenance only becomes a hard gate when a
// command-output ref is used to certify a passing Claim.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/evidence"
)

// CapturedFrozenSubject records the content or presence observed at one side
// of a capture window. Canonical deleted subjects must be absent at both
// boundaries and are authorized by ProductBaseline's Git preimage proof.
type CapturedFrozenSubject struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Status string `json:"status,omitempty"` // present | missing | unreadable
}

// CaptureProvenance is optional so old hand-authored CaptureStep JSON remains
// readable.  Capture exec fills it whenever it can read the live ReviewPlan;
// no-plan captures are explicitly diagnostic_only and cannot certify a PASS.
type CaptureProvenance struct {
	ReviewPlanID                string                  `json:"review_plan_id,omitempty"`
	ReviewRound                 int                     `json:"review_round,omitempty"`
	ReviewPlanRevision          int                     `json:"review_plan_revision,omitempty"`
	AssignmentID                string                  `json:"assignment_id,omitempty"`
	ExecutionRoot               string                  `json:"execution_root,omitempty"`
	ExecutionRepositoryRoot     string                  `json:"execution_repository_root,omitempty"`
	StartedAt                   string                  `json:"started_at,omitempty"`
	EndedAt                     string                  `json:"ended_at,omitempty"`
	StartCommit                 string                  `json:"start_commit,omitempty"`
	EndCommit                   string                  `json:"end_commit,omitempty"`
	StartFrozenSubjects         []CapturedFrozenSubject `json:"start_frozen_subjects,omitempty"`
	EndFrozenSubjects           []CapturedFrozenSubject `json:"end_frozen_subjects,omitempty"`
	StartUndeclaredProductDrift []string                `json:"start_undeclared_product_drift,omitempty"`
	EndUndeclaredProductDrift   []string                `json:"end_undeclared_product_drift,omitempty"`
	ProductBaseline             *buildsurface.Snapshot  `json:"product_baseline,omitempty"`
	DiagnosticOnly              bool                    `json:"diagnostic_only,omitempty"`
	DiagnosticReason            string                  `json:"diagnostic_reason,omitempty"`

	// Runtime-only fields used while the command is executing. They are never
	// serialized into the capture buffer.
	subjectRoot   string
	authorityRoot string
	runtimeState  map[string]any
	context       context.Context
	subjects      []FrozenSubject
}

// BeginCaptureProvenance snapshots the plan subjects immediately before the
// wrapped command starts. It deliberately degrades to a diagnostic record
// when no plan is registered, so `capture exec` remains useful outside S7.
func BeginCaptureProvenance(root string, state map[string]any, assignmentID, executionRoot string, started time.Time) *CaptureProvenance {
	return BeginCaptureProvenanceContext(context.Background(), root, state, assignmentID, executionRoot, started)
}

func BeginCaptureProvenanceContext(ctx context.Context, root string, state map[string]any, assignmentID, executionRoot string, started time.Time) *CaptureProvenance {
	ctx = buildsurface.WithReadCache(ctx)
	p := &CaptureProvenance{
		AssignmentID:  strings.TrimSpace(assignmentID),
		ExecutionRoot: canonicalCapturePath(executionRoot),
		StartedAt:     started.UTC().Format(time.RFC3339Nano),
	}
	p.context = ctx
	p.subjectRoot = captureSubjectRootContext(ctx, root, executionRoot)
	p.authorityRoot = canonicalCapturePath(root)
	p.runtimeState = state
	p.ExecutionRepositoryRoot = p.subjectRoot
	p.StartCommit = captureGitCommitContext(ctx, p.subjectRoot)

	plan, ptr, err := LoadPlan(root, state)
	if err != nil || plan == nil || ptr == nil {
		p.DiagnosticOnly = true
		p.DiagnosticReason = "no active ReviewPlan; capture is diagnostic-only"
		if err != nil && !strings.Contains(err.Error(), "no ReviewPlan is registered") {
			p.DiagnosticReason = "ReviewPlan unavailable; capture is diagnostic-only"
		}
		return p
	}
	if !captureAssignmentInPlan(plan, assignmentID) {
		p.DiagnosticOnly = true
		p.DiagnosticReason = fmt.Sprintf("assignment %q is not part of ReviewPlan %s; capture is diagnostic-only", assignmentID, plan.ReviewPlanID)
		return p
	}
	p.ReviewPlanID = plan.ReviewPlanID
	p.ReviewRound = plan.ReviewRound
	p.ReviewPlanRevision = ptr.Revision
	p.subjects = append([]FrozenSubject(nil), plan.FrozenSubjects...)
	if plan.ProductBaseline != nil {
		baselineCopy := *plan.ProductBaseline
		baselineCopy.Tasks = append([]buildsurface.Task(nil), plan.ProductBaseline.Tasks...)
		baselineCopy.Contributions = append([]buildsurface.Contribution(nil), plan.ProductBaseline.Contributions...)
		baselineCopy.Paths = append([]buildsurface.Path(nil), plan.ProductBaseline.Paths...)
		p.ProductBaseline = &baselineCopy
		if err := verifyProductBaselineDeclarationContext(ctx, root, state, plan); err != nil {
			p.markDiagnosticOnly("canonical product baseline is unavailable: " + err.Error())
		}
		if p.StartCommit == "" || p.StartCommit != plan.ProductBaseline.FinalCommit {
			p.markDiagnosticOnly(fmt.Sprintf("capture checkout starts at commit %q; canonical ReviewPlan requires %q", p.StartCommit, plan.ProductBaseline.FinalCommit))
		}
	} else if buildsurface.HasCanonical(state) {
		p.markDiagnosticOnly("canonical S6 generation has no pinned product baseline in the ReviewPlan")
	}
	p.StartFrozenSubjects = captureSubjectDigests(p.subjectRoot, p.subjects)
	if drift, driftErr := detectUndeclaredProductDriftWithState(p.subjectRoot, plan, p.runtimeState, p.authorityRoot, ctx); driftErr == nil {
		p.StartUndeclaredProductDrift = drift
	} else if p.ProductBaseline != nil || errors.Is(driftErr, errResultPacketDriftProof) {
		p.markDiagnosticOnly("capture start drift cannot be checked: " + driftErr.Error())
	}
	return p
}

// FinishCaptureProvenance records the post-command snapshot. Hashing is
// best-effort at capture time; submit performs the fail-closed comparison for
// a passing Claim and explains missing or changed subjects to the Reviewer.
func (p *CaptureProvenance) Finish(ended time.Time) {
	ctx := context.Background()
	if p != nil && p.context != nil {
		ctx = p.context
	}
	p.FinishContext(ctx, ended)
}

func (p *CaptureProvenance) FinishContext(ctx context.Context, ended time.Time) {
	if p == nil {
		return
	}
	p.EndedAt = ended.UTC().Format(time.RFC3339Nano)
	p.EndCommit = captureGitCommitContext(ctx, p.subjectRoot)
	if p.ProductBaseline != nil {
		if p.EndCommit == "" || p.EndCommit != p.ProductBaseline.FinalCommit {
			p.markDiagnosticOnly(fmt.Sprintf("capture checkout ends at commit %q; canonical ReviewPlan requires %q", p.EndCommit, p.ProductBaseline.FinalCommit))
		}
		plan := &Plan{ProductBaseline: p.ProductBaseline, FrozenSubjects: append([]FrozenSubject(nil), p.subjects...)}
		if err := verifyProductBaselineDeclarationContext(ctx, p.authorityRoot, p.runtimeState, plan); err != nil {
			p.markDiagnosticOnly("canonical product baseline changed during capture: " + err.Error())
		}
	}
	if len(p.subjects) > 0 {
		p.EndFrozenSubjects = captureSubjectDigests(p.subjectRoot, p.subjects)
	}
	if len(p.subjects) > 0 {
		plan := &Plan{ProductBaseline: p.ProductBaseline, FrozenSubjects: append([]FrozenSubject(nil), p.subjects...)}
		if drift, driftErr := detectUndeclaredProductDriftWithState(p.subjectRoot, plan, p.runtimeState, p.authorityRoot, ctx); driftErr == nil {
			p.EndUndeclaredProductDrift = drift
		} else if p.ProductBaseline != nil || errors.Is(driftErr, errResultPacketDriftProof) {
			p.markDiagnosticOnly("capture end drift cannot be checked: " + driftErr.Error())
		}
	}
	p.subjectRoot = ""
	p.authorityRoot = ""
	p.runtimeState = nil
	p.context = nil
	p.subjects = nil
}

func (p *CaptureProvenance) markDiagnosticOnly(reason string) {
	if p == nil {
		return
	}
	p.DiagnosticOnly = true
	if p.DiagnosticReason == "" {
		p.DiagnosticReason = reason
	}
}

func captureAssignmentInPlan(plan *Plan, assignmentID string) bool {
	for _, assignment := range plan.Assignments {
		if assignment.AssignmentID == assignmentID {
			return true
		}
	}
	return false
}

func canonicalCapturePath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

func captureSubjectRoot(authorityRoot, executionRoot string) string {
	return captureSubjectRootContext(context.Background(), authorityRoot, executionRoot)
}
func captureSubjectRootContext(ctx context.Context, authorityRoot, executionRoot string) string {
	executionRoot = canonicalCapturePath(executionRoot)
	if executionRoot == "" {
		executionRoot = canonicalCapturePath(authorityRoot)
	}
	if gitRoot := captureGitRootContext(ctx, executionRoot); gitRoot != "" {
		return gitRoot
	}
	authorityRoot = canonicalCapturePath(authorityRoot)
	if authorityRoot != "" && pathWithinCaptureRoot(executionRoot, authorityRoot) {
		return authorityRoot
	}
	return executionRoot
}

func pathWithinCaptureRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func captureGitRoot(path string) string {
	return captureGitRootContext(context.Background(), path)
}
func captureGitRootContext(ctx context.Context, path string) string {
	if path == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return canonicalCapturePath(strings.TrimSpace(string(output)))
}

func captureGitCommit(path string) string {
	return captureGitCommitContext(context.Background(), path)
}
func captureGitCommitContext(ctx context.Context, path string) string {
	if path == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func captureSubjectDigests(root string, subjects []FrozenSubject) []CapturedFrozenSubject {
	out := make([]CapturedFrozenSubject, 0, len(subjects))
	for _, subject := range subjects {
		captured := CapturedFrozenSubject{Path: filepath.ToSlash(subject.Path)}
		path, err := captureContainedPath(root, subject.Path)
		if err != nil {
			captured.Status = "unreadable"
			out = append(out, captured)
			continue
		}
		if _, statErr := os.Lstat(path); statErr != nil {
			if os.IsNotExist(statErr) {
				captured.Status = "missing"
			} else {
				captured.Status = "unreadable"
			}
			out = append(out, captured)
			continue
		}
		captured.Status = "present"
		if data, readErr := os.ReadFile(path); readErr == nil {
			sum := sha256.Sum256(data)
			captured.SHA256 = hex.EncodeToString(sum[:])
		} else {
			captured.Status = "unreadable"
		}
		out = append(out, captured)
	}
	return out
}

func captureContainedPath(root, rel string) (string, error) {
	root = canonicalCapturePath(root)
	if root == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("capture subject path must be relative")
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if !pathWithinCaptureRoot(path, root) {
		return "", fmt.Errorf("capture subject path escapes execution root")
	}
	return path, nil
}

type commandOutputEvidence = evidence.CommandOutputReference

func parseCommandOutputEvidenceRef(ref string) (commandOutputEvidence, error) {
	return evidence.ParseCommandOutputReference(ref)
}

// captureEvidenceKey returns the stable path+digest identity shared by the
// generated command_output ref and a path: ref copied into a ReviewResult.
func captureEvidenceKey(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "command_output:") {
		parsed, err := parseCommandOutputEvidenceRef(ref)
		if err != nil || parsed.Withheld || parsed.SHA256 == "" {
			return "", false
		}
		return filepath.ToSlash(parsed.RelativePath) + "#sha256=" + parsed.SHA256, true
	}
	if strings.HasPrefix(ref, "path:") {
		rel, digest, err := parsePathEvidenceRef(ref)
		if err != nil || digest == "" {
			return "", false
		}
		return filepath.ToSlash(rel) + "#sha256=" + digest, true
	}
	return "", false
}

func isAutomaticCaptureRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "command_output:") {
		return true
	}
	if !strings.HasPrefix(ref, "path:") {
		return false
	}
	rel, _, err := parsePathEvidenceRef(ref)
	if err != nil {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	parts := strings.Split(strings.Trim(clean, "/"), "/")
	// capture exec stores immutable stream files at
	// .claude/evidence/<runtime>/g<generation>/captures/<assignment>/exec/.
	// A path: ref into that surface is still an automatic capture ref and
	// cannot bypass the provenance check by changing only its prefix.
	for index := 0; index+3 < len(parts); index++ {
		if parts[index] == "captures" && parts[index+2] == "exec" {
			return true
		}
	}
	return false
}

func captureProvenanceForKey(steps []CaptureStep, key string) []*CaptureProvenance {
	seen := map[*CaptureProvenance]bool{}
	var out []*CaptureProvenance
	for _, step := range steps {
		if step.Provenance == nil {
			continue
		}
		for _, ref := range step.Evidence {
			if refKey, ok := captureEvidenceKey(ref); ok && refKey == key && !seen[step.Provenance] {
				seen[step.Provenance] = true
				out = append(out, step.Provenance)
			}
		}
	}
	return out
}

func validateOneCaptureProvenance(root string, state map[string]any, plan *Plan, assignmentID string, assignmentRevision int, provenance *CaptureProvenance, contexts ...context.Context) error {
	ctx := baselineContext(contexts)
	if err := ctx.Err(); err != nil {
		return err
	}
	if provenance == nil {
		return fmt.Errorf("capture evidence has no execution provenance")
	}
	if provenance.DiagnosticOnly {
		return fmt.Errorf("capture evidence is diagnostic-only and is not bound to a ReviewPlan")
	}
	if provenance.ReviewPlanID != plan.ReviewPlanID || provenance.ReviewRound != plan.ReviewRound || provenance.ReviewPlanRevision != assignmentRevision {
		return fmt.Errorf("capture provenance binds ReviewPlan %s round %d revision %d, want %s round %d revision %d", provenance.ReviewPlanID, provenance.ReviewRound, provenance.ReviewPlanRevision, plan.ReviewPlanID, plan.ReviewRound, assignmentRevision)
	}
	if provenance.AssignmentID != assignmentID {
		return fmt.Errorf("capture provenance assignment %s does not match %s", provenance.AssignmentID, assignmentID)
	}
	if provenance.ExecutionRoot == "" || provenance.ExecutionRepositoryRoot == "" || provenance.EndedAt == "" {
		return fmt.Errorf("capture provenance is incomplete (execution root or end snapshot missing)")
	}
	canonicalBaseline := plan.ProductBaseline != nil || buildsurface.HasCanonical(state)
	if canonicalBaseline {
		if plan.ProductBaseline == nil {
			return fmt.Errorf("canonical S6 ReviewPlan has no product baseline for capture provenance")
		}
		if provenance.ProductBaseline == nil || !reflect.DeepEqual(*provenance.ProductBaseline, *plan.ProductBaseline) {
			return fmt.Errorf("capture provenance does not carry the pinned ReviewPlan product baseline")
		}
		if err := verifyProductBaselineDeclarationContext(ctx, root, state, plan); err != nil {
			return fmt.Errorf("pinned product baseline cannot be reverified at ReviewResult submit: %w", err)
		}
		if provenance.StartCommit == "" || provenance.StartCommit != plan.ProductBaseline.FinalCommit || provenance.EndCommit == "" || provenance.EndCommit != plan.ProductBaseline.FinalCommit {
			return fmt.Errorf("capture checkout commits must both equal pinned product baseline final commit %s (start=%q end=%q)", plan.ProductBaseline.FinalCommit, provenance.StartCommit, provenance.EndCommit)
		}
	} else if provenance.ProductBaseline != nil {
		return fmt.Errorf("capture provenance carries a canonical product baseline but the ReviewPlan is historical")
	}
	if _, err := os.Stat(provenance.ExecutionRoot); err != nil {
		return fmt.Errorf("capture execution root %s is unavailable: %w", provenance.ExecutionRoot, err)
	}
	if !canonicalBaseline && provenance.StartCommit != "" && provenance.EndCommit != "" && provenance.StartCommit != provenance.EndCommit {
		return fmt.Errorf("capture execution commit changed during command (%s -> %s)", provenance.StartCommit, provenance.EndCommit)
	}
	if len(provenance.StartUndeclaredProductDrift) > 0 || len(provenance.EndUndeclaredProductDrift) > 0 {
		return fmt.Errorf("capture execution checkout has undeclared product drift (start=%s end=%s)", strings.Join(provenance.StartUndeclaredProductDrift, ","), strings.Join(provenance.EndUndeclaredProductDrift, ","))
	}
	start := make(map[string]CapturedFrozenSubject, len(provenance.StartFrozenSubjects))
	end := make(map[string]CapturedFrozenSubject, len(provenance.EndFrozenSubjects))
	for _, subject := range provenance.StartFrozenSubjects {
		path := filepath.ToSlash(subject.Path)
		if path == "" {
			return fmt.Errorf("capture start frozen subject has an empty path")
		}
		if _, duplicate := start[path]; duplicate {
			return fmt.Errorf("capture start frozen subjects repeat %s", path)
		}
		start[path] = subject
	}
	for _, subject := range provenance.EndFrozenSubjects {
		path := filepath.ToSlash(subject.Path)
		if path == "" {
			return fmt.Errorf("capture end frozen subject has an empty path")
		}
		if _, duplicate := end[path]; duplicate {
			return fmt.Errorf("capture end frozen subjects repeat %s", path)
		}
		end[path] = subject
	}
	if canonicalBaseline && (len(start) != len(plan.FrozenSubjects) || len(end) != len(plan.FrozenSubjects)) {
		return fmt.Errorf("canonical capture provenance must contain the exact frozen subject set at both command boundaries")
	}
	for _, expected := range plan.FrozenSubjects {
		path := filepath.ToSlash(expected.Path)
		if canonicalBaseline && expected.Kind == "deleted" {
			startSubject, startOK := start[path]
			endSubject, endOK := end[path]
			if !startOK || !endOK {
				return fmt.Errorf("capture deleted subject %s is missing its start or end presence record", path)
			}
			if startSubject.Status != "missing" || endSubject.Status != "missing" || startSubject.SHA256 != "" || endSubject.SHA256 != "" {
				return fmt.Errorf("capture deleted subject %s must remain absent at both command boundaries", path)
			}
			continue
		}
		startSubject, startOK := start[path]
		endSubject, endOK := end[path]
		if canonicalBaseline && (!startOK || !endOK || startSubject.Status != "present" || endSubject.Status != "present") {
			return fmt.Errorf("capture frozen subject %s must be readable at both command boundaries", path)
		}
		if !startOK || startSubject.SHA256 == "" || startSubject.SHA256 != expected.SHA256 {
			return fmt.Errorf("capture frozen subject %s start hash %s does not match ReviewPlan baseline %s", path, startSubject.SHA256, expected.SHA256)
		}
		if !endOK || endSubject.SHA256 == "" || endSubject.SHA256 != expected.SHA256 {
			return fmt.Errorf("capture frozen subject %s end hash %s does not match ReviewPlan baseline %s", path, endSubject.SHA256, expected.SHA256)
		}
	}
	return nil
}

// validateCapturePassProvenance checks only command-output evidence attached
// to PASS claims/checks. Failure findings may intentionally use dirty or
// partial diagnostics; unrelated capture steps do not block a claim.
func validateCapturePassProvenance(root string, state map[string]any, plan *Plan, assignment *PlanAssignment, result *Result, steps []CaptureStep, contexts ...context.Context) error {
	ctx := baselineContext(contexts)
	if err := ctx.Err(); err != nil {
		return err
	}
	checkRefs := func(claimID string, refs []string) error {
		for _, ref := range refs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !isAutomaticCaptureRef(ref) {
				// Legacy hand-authored path: refs remain valid evidence. They
				// have no command execution window to bind and therefore do not
				// enter the automatic capture provenance gate.
				continue
			}
			key, ok := captureEvidenceKey(ref)
			if !ok {
				return fmt.Errorf("PASS automatic capture evidence %q is not a valid immutable capture ref", ref)
			}
			provenances := captureProvenanceForKey(steps, key)
			if len(provenances) == 0 {
				return fmt.Errorf("PASS evidence %q has no capture execution provenance", ref)
			}
			for _, step := range steps {
				for _, capturedRef := range step.Evidence {
					capturedKey, matched := captureEvidenceKey(capturedRef)
					if !matched || capturedKey != key || !strings.HasPrefix(strings.TrimSpace(capturedRef), "command_output:") {
						continue
					}
					parsed, parseErr := parseCommandOutputEvidenceRef(capturedRef)
					if parseErr != nil {
						return fmt.Errorf("PASS evidence %q is backed by malformed capture output: %w", ref, parseErr)
					}
					if parsed.Withheld || parsed.Truncated {
						return fmt.Errorf("PASS evidence %q is backed by withheld or truncated command output", ref)
					}
				}
			}
			for _, provenance := range provenances {
				if err := validateOneCaptureProvenance(root, state, plan, assignment.AssignmentID, result.AssignmentRevision, provenance, ctx); err != nil {
					return fmt.Errorf("capture provenance for PASS evidence %q: %w", ref, err)
				}
			}
		}
		return nil
	}
	for _, claimResult := range result.ClaimResults {
		if claimResult.Conclusion == "pass" {
			if err := checkRefs(claimResult.ClaimID, claimResult.EvidenceRefs); err != nil {
				return err
			}
		}
	}
	for _, check := range result.Checks {
		if check.Result == "pass" {
			if err := checkRefs("", check.EvidenceRefs); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func validateCapturePassEvidence(result *Result) error {
	checkRefs := func(owner string, refs []string) error {
		for _, ref := range refs {
			ref = strings.TrimSpace(ref)
			switch {
			case strings.HasPrefix(ref, "command_output:"):
				parsed, err := parseCommandOutputEvidenceRef(ref)
				if err != nil {
					return fmt.Errorf("%s command_output ref is malformed: %w", owner, err)
				}
				if parsed.Withheld {
					return fmt.Errorf("%s uses a withheld command_output ref; redaction gaps cannot certify PASS", owner)
				}
				if parsed.Truncated {
					return fmt.Errorf("%s uses truncated command_output evidence; an incomplete stream cannot certify PASS", owner)
				}
			case strings.HasPrefix(ref, "artifact:"), strings.HasPrefix(ref, "env:"):
				return fmt.Errorf("%s uses observation-only capture metadata %q; use a hash-bound command_output/path ref for PASS", owner, ref)
			}
		}
		return nil
	}
	for _, claim := range result.ClaimResults {
		if claim.Conclusion == "pass" {
			if err := checkRefs("claim "+claim.ClaimID, claim.EvidenceRefs); err != nil {
				return err
			}
		}
	}
	for _, check := range result.Checks {
		if check.Result == "pass" {
			if err := checkRefs("check "+check.Name, check.EvidenceRefs); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateCommandOutputEvidence(root, ref, owner string) error {
	parsed, err := parseCommandOutputEvidenceRef(ref)
	if err != nil {
		return fmt.Errorf("%s evidence reference %q is invalid: %w", owner, ref, err)
	}
	if parsed.Withheld {
		// A withheld stream is a truthful observation of a redaction gap. It
		// is retained for a Finding, but validateCapturePassEvidence prevents
		// it from certifying a PASS.
		return nil
	}
	path, err := repositoryContainedPath(root, parsed.RelativePath)
	if err != nil {
		return fmt.Errorf("%s evidence reference %q is invalid: %w", owner, ref, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s evidence reference %q does not exist: %w", owner, ref, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s evidence reference %q is not a regular file", owner, ref)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s evidence reference %q cannot be read: %w", owner, ref, err)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != parsed.SHA256 {
		return s7GateError(
			"S7_RESULT_EVIDENCE_REF",
			fmt.Sprintf("%s evidence reference %q has a digest mismatch", owner, ref),
			[]string{fmt.Sprintf("got %s, want %s", got, parsed.SHA256)},
			[]string{"rerun capture exec or reference the current immutable artifact"},
			"runtime review-result submit --assignment-id <assignment-id> --result <result.json>",
		)
	}
	if parsed.Truncated {
		if parsed.Bytes <= int64(len(data)) {
			return fmt.Errorf("%s evidence reference %q has inconsistent truncation metadata", owner, ref)
		}
	} else if parsed.Bytes != int64(len(data)) {
		return fmt.Errorf("%s evidence reference %q declares %d bytes but stores %d", owner, ref, parsed.Bytes, len(data))
	}
	return nil
}

func validateArtifactCaptureRef(ref, owner string) error {
	_, _, err := evidence.ParseFindingReference(ref)
	if err != nil {
		return fmt.Errorf("%s evidence reference %q is invalid: %w", owner, ref, err)
	}
	return nil
}

func validateEnvironmentCaptureRef(ref, owner string) error {
	_, _, err := evidence.ParseFindingReference(ref)
	if err != nil {
		return fmt.Errorf("%s evidence reference %q is invalid: %w", owner, ref, err)
	}
	return nil
}
