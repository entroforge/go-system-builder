package review

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// E2E cold-start verification workspace (L3-S7 §1.4.1, §3.2): the workspace
// is the only write surface reviewers may use during S7; its digest is
// pinned at registration and re-verified at seal/clean so a spec that changed
// after a result was consumed invalidates the round honestly.
// ---------------------------------------------------------------------------

// WorkspaceDigest computes sha256 over the sorted "relpath:filesha256" lines
// of every file under dir. An empty or missing workspace digests to the
// empty-string hash (the registered cold-start baseline).
func WorkspaceDigest(root, workspaceRel string) (string, error) {
	if workspaceRel == "" {
		return "", nil
	}
	abs, err := repositoryContainedPath(root, workspaceRel)
	if err != nil {
		return "", fmt.Errorf("workspace %s is outside repository: %w", workspaceRel, err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return sha256Of([]byte("")), nil
		}
		return "", fmt.Errorf("read workspace %s: %w", workspaceRel, err)
	}
	_ = entries
	var lines []string
	err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace contains symlink %s; symlinked evidence surfaces are not digestible", path)
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines = append(lines, filepath.ToSlash(rel)+":"+sha256Of(data))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	return sha256Of([]byte(strings.Join(lines, "\n"))), nil
}

// prepareVerificationWorkspace validates and creates the E2E cold-start
// workspace: it must live under e2e-workspace/ inside the repository so the
// reviewer write-scope rule and the digest recomputation can see it.
func prepareVerificationWorkspace(root string, plan *Plan) (string, error) {
	if plan.VerificationArtifactWorkspace == nil || strings.TrimSpace(*plan.VerificationArtifactWorkspace) == "" {
		return "", nil
	}
	workspace := *plan.VerificationArtifactWorkspace
	if filepath.IsAbs(workspace) || strings.HasPrefix(workspace, "..") {
		return "", fmt.Errorf("verification_artifact_workspace must be repository-relative (got %q)", workspace)
	}
	if !strings.HasPrefix(workspace, "e2e-workspace/") {
		return "", fmt.Errorf("verification_artifact_workspace must live under e2e-workspace/ (got %q); the reviewer write-scope rule only knows that surface", workspace)
	}
	abs, err := repositoryContainedPath(root, workspace)
	if err != nil {
		return "", fmt.Errorf("verification_artifact_workspace must stay inside repository: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create verification workspace: %w", err)
	}
	digest, err := WorkspaceDigest(root, workspace)
	if err != nil {
		return "", err
	}
	return digest, nil
}

// repositoryContainedPath resolves a repository-relative path and rejects
// lexical traversal (and existing symlink escapes). Reviewer write surfaces
// are security boundaries; a prefix check such as "e2e-workspace/" is not a
// containment proof for paths containing "..".
func repositoryContainedPath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be repository-relative")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	abs := filepath.Join(rootAbs, filepath.FromSlash(rel))
	relToRoot, err := filepath.Rel(rootAbs, abs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("path %q escapes repository", rel)
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(rootAbs)
	if rootErr != nil {
		return "", fmt.Errorf("resolve repository root symlinks: %w", rootErr)
	}
	// EvalSymlinks(candidate) fails when the leaf is new. Walk upward until an
	// existing ancestor is found so a symlinked parent cannot smuggle a future
	// artifact outside the repository.
	for current := abs; ; current = filepath.Dir(current) {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			resolvedRel, relErr := filepath.Rel(resolvedRoot, resolved)
			if relErr != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRel) {
				return "", fmt.Errorf("path %q follows a symlink outside repository", rel)
			}
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return abs, nil
}

// verifyFrozenSubjects proves that the registered ReviewPlan still describes
// the bytes on disk. SubjectDigest only fingerprints the plan declarations;
// it must not be mistaken for a disk-baseline check. A changed or missing
// subject makes the round stale before any Reviewer Result is consumed.
//
// RC-03 dual-source (S7-2): the check is the union of the declared set and the
// git diff baseline. A file modified or added outside frozen_subjects is still
// product drift — the plan is stale even though the hand-written list hashes
// clean. Allowed write surfaces (.claude/, docs/reports/, e2e-workspace/) are
// excluded from the drift scan.
func verifyFrozenSubjects(root string, plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("frozen subject verification requires a plan")
	}
	for _, subject := range plan.FrozenSubjects {
		if strings.TrimSpace(subject.Path) == "" {
			return fmt.Errorf("frozen subject has an empty path")
		}
		if len(subject.SHA256) != 64 {
			return fmt.Errorf("frozen subject %s has an invalid sha256", subject.Path)
		}
		path, err := repositoryContainedPath(root, subject.Path)
		if err != nil {
			return fmt.Errorf("frozen subject %s is outside repository: %w", subject.Path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("frozen subject %s is unreadable: %w", subject.Path, err)
		}
		actual := sha256Of(data)
		if actual != subject.SHA256 {
			return fmt.Errorf("frozen subject %s drifted: plan pins %s but disk contains %s", subject.Path, subject.SHA256, actual)
		}
	}
	if root != "" {
		if drift, err := detectUndeclaredProductDrift(root, plan); err == nil && len(drift) > 0 {
			return fmt.Errorf("frozen baseline drift: undeclared product file(s) %s outside frozen_subjects; add them to frozen_subjects or revert the change (RC-03 dual-source: declared set ∪ git diff baseline)", strings.Join(drift, ", "))
		}
	}
	return nil
}

// detectUndeclaredProductDrift scans the git diff baseline for product files
// that were modified, added, or untracked without being declared in
// frozen_subjects. It is the second source of the RC-03 dual-source check.
// Non-git repositories (e.g., TempDir fixtures) degrade gracefully: no extra
// drift is reported when git is unavailable.
func detectUndeclaredProductDrift(root string, plan *Plan) ([]string, error) {
	frozen := make(map[string]bool, len(plan.FrozenSubjects))
	for _, subject := range plan.FrozenSubjects {
		frozen[normalizeSurface(subject.Path)] = true
	}
	output, err := exec.Command("git", "-C", root, "status", "--porcelain", "--no-renames").CombinedOutput()
	if err != nil {
		return nil, err
	}
	var drift []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Porcelain format: XY<space>path[ -> orig] — with --no-renames the
		// path is always the last field.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rawPath := fields[len(fields)-1]
		// Handle quoted paths (core.quotepath) by trimming quotes.
		rawPath = strings.Trim(rawPath, "\"'")
		rel := normalizeSurface(filepath.ToSlash(rawPath))
		if rel == "" || frozen[rel] {
			continue
		}
		if isAllowedDriftSurface(rel) {
			continue
		}
		drift = append(drift, rel)
	}
	sort.Strings(drift)
	return drift, nil
}

func isAllowedDriftSurface(rel string) bool {
	if rel == ".claude" || isControlPlaneDriftPath(rel) {
		return true
	}
	if rel == "docs/reports" || strings.HasPrefix(rel, "docs/reports/") {
		return true
	}
	if rel == "docs/release_audits" || strings.HasPrefix(rel, "docs/release_audits/") {
		return true
	}
	// Audit, blueprint and other non-product projections are not frozen product
	// surfaces; their presence as untracked/modified files must not turn every
	// S7 round stale. Only product surfaces (internal/, cmd/, pkg/, api/, etc.)
	// are drift-relevant — docs/ as a whole is an allowed surface for the
	// dual-source check (the frozen_subjects allow-list still governs product).
	if strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, "blueprint/") || strings.HasPrefix(rel, "schema/") {
		return true
	}
	if strings.HasPrefix(rel, "e2e-workspace/") {
		return true
	}
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return true
	}
	return false
}

func isControlPlaneDriftPath(rel string) bool {
	if rel == ".claude/loop-state.json" || rel == ".claude/loop-events.jsonl" || rel == ".claude/loop-metrics.json" || rel == ".claude/settings.json" || rel == ".claude/settings.local.json" {
		return true
	}
	for _, prefix := range []string{".claude/review/", ".claude/evidence/", ".claude/workgroups/", ".claude/plans/", ".claude/bin/"} {
		if rel == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// verifyResultArtifactDigest binds an E2E result to the workspace digest it
// actually ran against (L3-S7 §3.5): results from a workspace that has since
// drifted are stale, not submittable.
func verifyResultArtifactDigest(root string, plan *Plan, ptr *PlanPointer, result *Result, lens string) error {
	if lens != "e2e" {
		return nil
	}
	if ptr.VerificationArtifactWorkspace == "" {
		if result.VerificationArtifactDigest != nil && *result.VerificationArtifactDigest != "" {
			return fmt.Errorf("result binds a verification artifact digest but the plan declares no workspace")
		}
		return nil
	}
	digest, err := WorkspaceDigest(root, ptr.VerificationArtifactWorkspace)
	if err != nil {
		return err
	}
	if result.VerificationArtifactDigest == nil || *result.VerificationArtifactDigest == "" {
		return fmt.Errorf("E2E results on a cold-start workspace must bind verification_artifact_digest; compute it with `loop-harness s7 workspace-digest` after the spec/fixture write")
	}
	if *result.VerificationArtifactDigest != digest {
		return fmt.Errorf("verification artifact digest mismatch: result binds %s but the workspace now digests to %s; re-run the flows against the current spec before submitting", *result.VerificationArtifactDigest, digest)
	}
	return nil
}

// verifyRegressionAssetFingerprints proves that a regression_available plan
// still points at the exact files it claims to reuse. Cold-start assets are
// authored inside the pinned workspace and are checked by WorkspaceDigest.
func verifyRegressionAssetFingerprints(root string, plan *Plan) error {
	if plan.E2ECoverageState != "regression_available" {
		return nil
	}
	for _, asset := range sortE2EAssets(plan.E2EAssets) {
		path, err := repositoryContainedPath(root, asset.Path)
		if err != nil {
			return s7GateError(
				"S7_E2E_ASSET_FINGERPRINT",
				fmt.Sprintf("E2E asset %s is outside the repository", asset.AssetID),
				[]string{err.Error()},
				[]string{"use the exact repository-relative CASE/PATH file and regenerate its sha256"},
				"runtime review-plan --file plan.json",
			)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return s7GateError(
				"S7_E2E_ASSET_FINGERPRINT",
				fmt.Sprintf("E2E asset %s cannot be read", asset.AssetID),
				[]string{err.Error()},
				[]string{"restore the asset or change the plan to cold_start and author a fresh verification workspace"},
				"runtime review-plan --file plan.json",
			)
		}
		if actual := sha256Of(data); actual != asset.SHA256 {
			return s7GateError(
				"S7_E2E_ASSET_FINGERPRINT",
				fmt.Sprintf("E2E asset %s fingerprint is stale", asset.AssetID),
				[]string{fmt.Sprintf("asset %s has sha256 %s, plan declares %s", asset.Path, actual, asset.SHA256)},
				[]string{"refresh the fingerprint from the current immutable asset, or switch to cold_start"},
				"runtime review-plan --file plan.json",
			)
		}
	}
	return nil
}

// verifySealedArtifactDigests re-checks every consumed E2E assignment's
// bound digest against the workspace at close time (L3-S7 §10.1.6): a
// workspace that drifted after consumption invalidates the round.
func verifySealedArtifactDigests(root string, ptr *PlanPointer, assignments map[string]any) error {
	if ptr.VerificationArtifactWorkspace == "" {
		return nil
	}
	current, err := WorkspaceDigest(root, ptr.VerificationArtifactWorkspace)
	if err != nil {
		return err
	}
	for id, raw := range assignments {
		row, _ := raw.(map[string]any)
		if row == nil || row["lens"] != "e2e" || row["status"] != "consumed" {
			continue
		}
		bound, _ := row["artifact_digest"].(string)
		if bound != "" && bound != current {
			return fmt.Errorf("assignment %s consumed a result against workspace digest %s, but the workspace now digests to %s; the round is stale (L3-S7 §10.3)", id, bound, current)
		}
	}
	return nil
}

// projectedAssignments returns the assignment projection as it will look
// after this result is consumed (current assignment consumed + digest bound).
func projectedAssignments(state map[string]any, currentAssignment *PlanAssignment, result *Result) map[string]any {
	reviewMap, _ := state["review"].(map[string]any)
	assignments, _ := reviewMap["assignments"].(map[string]any)
	out := map[string]any{}
	for id, raw := range assignments {
		row, _ := raw.(map[string]any)
		if row != nil {
			out[id] = row
		}
	}
	digest := ""
	if result.VerificationArtifactDigest != nil {
		digest = *result.VerificationArtifactDigest
	}
	out[currentAssignment.AssignmentID] = map[string]any{
		"lens": currentAssignment.Lens, "status": "consumed",
		"agent_id": result.ProducerAgentID, "artifact_digest": digest,
	}
	return out
}

// ---------------------------------------------------------------------------
// Capture buffer (L3-S7 §3.6 auto-capture): `loop-harness capture step`
// appends one sanitized timeline step per call; submit merges them into
// findings whose encounter timeline is empty.
// ---------------------------------------------------------------------------

// secretPatterns are the redaction gate: any capture field matching one is
// rejected — the buffer never persists secrets (L3-S7 §6.3).
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password\s*[:=]`),
	regexp.MustCompile(`(?i)api[_-]?key\s*[:=]`),
	regexp.MustCompile(`(?i)secret\s*[:=]`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{12,}`),
	regexp.MustCompile(`(?i)token\s*[:=]\s*[A-Za-z0-9._-]{8,}`),
}

// CaptureStep is one sanitized timeline step.
type CaptureStep struct {
	Sequence   int      `json:"sequence"`
	FindingID  string   `json:"finding_id,omitempty"`
	ClaimID    string   `json:"claim_id,omitempty"`
	Action     string   `json:"action"`
	Observed   string   `json:"observed"`
	Evidence   []string `json:"evidence_refs,omitempty"`
	CapturedAt string   `json:"captured_at"`
}

// SanitizeCapture rejects any field that smells like a secret.
func SanitizeCapture(step CaptureStep) error {
	fields := map[string]string{
		"action":   step.Action,
		"observed": step.Observed,
	}
	for _, ref := range step.Evidence {
		fields["evidence_ref"] = ref
	}
	for name, value := range fields {
		for _, pattern := range secretPatterns {
			if pattern.MatchString(value) {
				return fmt.Errorf("capture field %q matches a secret pattern (%s); record the redacted ref, not the value (L3-S7 §6.3)", name, pattern.String())
			}
		}
	}
	return nil
}

// CaptureFile returns the buffer path for one assignment.
func CaptureFile(root, runtimeID string, generation int, assignmentID string) string {
	return filepath.Join(root, ".claude", "evidence", runtimeID,
		fmt.Sprintf("g%d", generation), "captures", assignmentID, "steps.jsonl")
}

// LoadCaptureSteps is the compatibility helper used by read-only capture
// inspectors. Submit paths must use LoadCaptureStepsStrict so malformed lines
// cannot silently disappear from S8 evidence.
func LoadCaptureSteps(path string) []CaptureStep {
	steps, _ := LoadCaptureStepsStrict(path)
	return steps
}

// LoadCaptureStepsStrict reads the buffer and reports the first malformed
// JSONL line with its line number. A missing buffer is empty, not an error.
func LoadCaptureStepsStrict(path string) ([]CaptureStep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read capture buffer %s: %w", path, err)
	}
	var steps []CaptureStep
	for lineNumber, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var step CaptureStep
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			return nil, fmt.Errorf("capture buffer %s line %d is malformed JSON: %w", path, lineNumber+1, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// mergeCapturedTimeline fills an empty encounter timeline from the buffer.
// Findings whose Reviewer wrote a real timeline are never rewritten.
func mergeCapturedTimeline(findings []Finding, steps []CaptureStep) {
	if len(steps) == 0 {
		return
	}
	timeline := make([]TimelineStep, 0, len(steps))
	for _, step := range steps {
		timeline = append(timeline, TimelineStep{
			Sequence:           step.Sequence,
			Action:             step.Action,
			ObservedCheckpoint: step.Observed,
			EvidenceRefs:       step.Evidence,
		})
	}
	for i := range findings {
		if len(findings[i].Encounter.Timeline) == 0 {
			findings[i].Encounter.Timeline = timeline
		}
	}
}

// mergeCapturedTimelineChecked is the submit-time variant. Assignment-wide
// capture is safe only when there is one Finding. Multiple Findings require a
// finding_id or claim_id on every step so the same wall is not copied into
// unrelated investigation cases.
func mergeCapturedTimelineChecked(findings []Finding, steps []CaptureStep) error {
	if len(steps) == 0 || len(findings) == 0 {
		return nil
	}
	if len(findings) == 1 {
		for _, step := range steps {
			if step.FindingID != "" && step.FindingID != findings[0].FindingID ||
				step.ClaimID != "" && step.ClaimID != findings[0].ClaimID {
				return fmt.Errorf("capture step %d is correlated to finding=%s claim=%s, outside the submitted finding set", step.Sequence, step.FindingID, step.ClaimID)
			}
		}
		mergeCapturedTimeline(findings, steps)
		return nil
	}

	byFinding := make(map[string]int, len(findings))
	byClaim := make(map[string]int, len(findings))
	for index, finding := range findings {
		byFinding[finding.FindingID] = index
		byClaim[finding.ClaimID] = index
	}
	grouped := make(map[int][]CaptureStep)
	for _, step := range steps {
		index := -1
		if step.FindingID != "" && step.ClaimID != "" {
			findingIndex, findingOK := byFinding[step.FindingID]
			claimIndex, claimOK := byClaim[step.ClaimID]
			if !findingOK || !claimOK || findingIndex != claimIndex {
				return fmt.Errorf("capture step %d correlation conflict: finding_id=%s and claim_id=%s do not identify the same submitted Finding", step.Sequence, step.FindingID, step.ClaimID)
			}
		}
		if step.FindingID != "" {
			if candidate, ok := byFinding[step.FindingID]; ok {
				index = candidate
			}
		}
		if index < 0 && step.ClaimID != "" {
			if candidate, ok := byClaim[step.ClaimID]; ok {
				index = candidate
			}
		}
		if index < 0 {
			return fmt.Errorf("capture timeline is ambiguous for %d findings: step %d has no finding_id or claim_id", len(findings), step.Sequence)
		}
		grouped[index] = append(grouped[index], step)
	}
	for index, group := range grouped {
		if len(findings[index].Encounter.Timeline) == 0 {
			mergeCapturedTimeline(findings[index:index+1], group)
		}
	}
	return nil
}
