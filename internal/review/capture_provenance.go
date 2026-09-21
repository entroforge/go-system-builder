package review

// Capture provenance binds an automatically captured command to the ReviewPlan
// it was executing against.  The command wrapper remains useful in dirty or
// non-Git diagnostic directories; provenance only becomes a hard gate when a
// command-output ref is used to certify a passing Claim.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CapturedFrozenSubject is the content observed at one side of a capture
// window.  An empty SHA256 means that the subject was missing or unreadable
// from the execution checkout.
type CapturedFrozenSubject struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
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
	DiagnosticOnly              bool                    `json:"diagnostic_only,omitempty"`
	DiagnosticReason            string                  `json:"diagnostic_reason,omitempty"`

	// Runtime-only fields used while the command is executing. They are never
	// serialized into the capture buffer.
	subjectRoot string
	subjects    []FrozenSubject
}

// BeginCaptureProvenance snapshots the plan subjects immediately before the
// wrapped command starts. It deliberately degrades to a diagnostic record
// when no plan is registered, so `capture exec` remains useful outside S7.
func BeginCaptureProvenance(root string, state map[string]any, assignmentID, executionRoot string, started time.Time) *CaptureProvenance {
	p := &CaptureProvenance{
		AssignmentID:  strings.TrimSpace(assignmentID),
		ExecutionRoot: canonicalCapturePath(executionRoot),
		StartedAt:     started.UTC().Format(time.RFC3339Nano),
	}
	p.subjectRoot = captureSubjectRoot(root, executionRoot)
	p.ExecutionRepositoryRoot = p.subjectRoot
	p.StartCommit = captureGitCommit(p.subjectRoot)

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
	p.StartFrozenSubjects = captureSubjectDigests(p.subjectRoot, p.subjects)
	if drift, driftErr := detectUndeclaredProductDrift(p.subjectRoot, plan); driftErr == nil {
		p.StartUndeclaredProductDrift = drift
	}
	return p
}

// FinishCaptureProvenance records the post-command snapshot. Hashing is
// best-effort at capture time; submit performs the fail-closed comparison for
// a passing Claim and explains missing or changed subjects to the Reviewer.
func (p *CaptureProvenance) Finish(ended time.Time) {
	if p == nil {
		return
	}
	p.EndedAt = ended.UTC().Format(time.RFC3339Nano)
	p.EndCommit = captureGitCommit(p.subjectRoot)
	if len(p.subjects) > 0 {
		p.EndFrozenSubjects = captureSubjectDigests(p.subjectRoot, p.subjects)
	}
	if len(p.subjects) > 0 {
		plan := &Plan{FrozenSubjects: append([]FrozenSubject(nil), p.subjects...)}
		if drift, driftErr := detectUndeclaredProductDrift(p.subjectRoot, plan); driftErr == nil {
			p.EndUndeclaredProductDrift = drift
		}
	}
	p.subjectRoot = ""
	p.subjects = nil
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
	executionRoot = canonicalCapturePath(executionRoot)
	if executionRoot == "" {
		executionRoot = canonicalCapturePath(authorityRoot)
	}
	if gitRoot := captureGitRoot(executionRoot); gitRoot != "" {
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
	if path == "" {
		return ""
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return canonicalCapturePath(strings.TrimSpace(string(output)))
}

func captureGitCommit(path string) string {
	if path == "" {
		return ""
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "HEAD")
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
		if err == nil {
			if data, readErr := os.ReadFile(path); readErr == nil {
				sum := sha256.Sum256(data)
				captured.SHA256 = hex.EncodeToString(sum[:])
			}
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

var captureCommandOutputRE = regexp.MustCompile(`^command_output:(.+)#sha256=([0-9a-f]{64}) \(bytes=([0-9]+), truncated=(true|false)\)$`)

type commandOutputEvidence struct {
	RelativePath string
	SHA256       string
	Bytes        int64
	Truncated    bool
	Withheld     bool
}

func parseCommandOutputEvidenceRef(ref string) (commandOutputEvidence, error) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "command_output:") {
		return commandOutputEvidence{}, fmt.Errorf("not a command_output reference")
	}
	body := strings.TrimPrefix(ref, "command_output:")
	if strings.Contains(body, " (withheld:") {
		return commandOutputEvidence{RelativePath: strings.TrimSpace(strings.SplitN(body, " (withheld:", 2)[0]), Withheld: true}, nil
	}
	matches := captureCommandOutputRE.FindStringSubmatch(ref)
	if len(matches) != 5 {
		return commandOutputEvidence{}, fmt.Errorf("command_output reference must include #sha256, bytes and truncated metadata")
	}
	bytes, err := strconv.ParseInt(matches[3], 10, 64)
	if err != nil || bytes < 0 {
		return commandOutputEvidence{}, fmt.Errorf("command_output bytes metadata is invalid")
	}
	return commandOutputEvidence{
		RelativePath: matches[1], SHA256: matches[2], Bytes: bytes,
		Truncated: matches[4] == "true",
	}, nil
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

func validateOneCaptureProvenance(plan *Plan, assignmentID string, assignmentRevision int, provenance *CaptureProvenance) error {
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
	if _, err := os.Stat(provenance.ExecutionRoot); err != nil {
		return fmt.Errorf("capture execution root %s is unavailable: %w", provenance.ExecutionRoot, err)
	}
	if provenance.StartCommit != "" && provenance.EndCommit != "" && provenance.StartCommit != provenance.EndCommit {
		return fmt.Errorf("capture execution commit changed during command (%s -> %s)", provenance.StartCommit, provenance.EndCommit)
	}
	if len(provenance.StartUndeclaredProductDrift) > 0 || len(provenance.EndUndeclaredProductDrift) > 0 {
		return fmt.Errorf("capture execution checkout has undeclared product drift (start=%s end=%s)", strings.Join(provenance.StartUndeclaredProductDrift, ","), strings.Join(provenance.EndUndeclaredProductDrift, ","))
	}
	start := make(map[string]string, len(provenance.StartFrozenSubjects))
	end := make(map[string]string, len(provenance.EndFrozenSubjects))
	for _, subject := range provenance.StartFrozenSubjects {
		start[filepath.ToSlash(subject.Path)] = subject.SHA256
	}
	for _, subject := range provenance.EndFrozenSubjects {
		end[filepath.ToSlash(subject.Path)] = subject.SHA256
	}
	for _, expected := range plan.FrozenSubjects {
		path := filepath.ToSlash(expected.Path)
		if start[path] == "" || start[path] != expected.SHA256 {
			return fmt.Errorf("capture frozen subject %s start hash %s does not match ReviewPlan baseline %s", path, start[path], expected.SHA256)
		}
		if end[path] == "" || end[path] != expected.SHA256 {
			return fmt.Errorf("capture frozen subject %s end hash %s does not match ReviewPlan baseline %s", path, end[path], expected.SHA256)
		}
	}
	return nil
}

// validateCapturePassProvenance checks only command-output evidence attached
// to PASS claims/checks. Failure findings may intentionally use dirty or
// partial diagnostics; unrelated capture steps do not block a claim.
func validateCapturePassProvenance(plan *Plan, assignment *PlanAssignment, result *Result, steps []CaptureStep) error {
	checkRefs := func(claimID string, refs []string) error {
		for _, ref := range refs {
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
				if err := validateOneCaptureProvenance(plan, assignment.AssignmentID, result.AssignmentRevision, provenance); err != nil {
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
	return nil
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
	body := strings.TrimPrefix(strings.TrimSpace(ref), "artifact:")
	if strings.HasSuffix(body, " (deleted)") {
		if strings.TrimSpace(strings.TrimSuffix(body, " (deleted)")) == "" {
			return fmt.Errorf("%s contains an empty deleted artifact reference", owner)
		}
		return nil
	}
	first := strings.SplitN(body, " ", 2)[0]
	rel, digest, err := parsePathEvidenceRef("path:" + first)
	if err != nil || rel == "" || digest == "" {
		if err == nil {
			err = fmt.Errorf("artifact ref must include a relative path and sha256 digest")
		}
		return fmt.Errorf("%s evidence reference %q is invalid: %w", owner, ref, err)
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(rel)), "../") {
		return fmt.Errorf("%s evidence reference %q escapes its execution root", owner, ref)
	}
	return nil
}

func validateEnvironmentCaptureRef(ref, owner string) error {
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), "env:"))
	if body == "" || !strings.Contains(body, " (present; value never captured)") {
		return fmt.Errorf("%s evidence reference %q is not a valid redacted environment marker", owner, ref)
	}
	if strings.TrimSpace(strings.SplitN(body, " (present;", 2)[0]) == "" {
		return fmt.Errorf("%s evidence reference %q has no environment name", owner, ref)
	}
	return nil
}
