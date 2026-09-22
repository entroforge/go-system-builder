package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/review"
)

// TestFinalAuditCaptureExecRefsSubmit keeps the capture compatibility check at
// the real CLI boundary. capture exec writes command_output: references into
// the buffer. The same ref must be consumable by review-result submit without
// hand conversion, and the final assignment proves that the round consumer
// still reaches clean.
func TestFinalAuditCaptureExecRefsSubmit(t *testing.T) {
	planID := "review-plan-final-capture-refs"
	root, bufferDir, rawRef := finalCaptureExecFixture(t, planID, "printf clean-capture")
	var stdout, stderr bytes.Buffer

	digest := subjectDigestForPlan(t, root, planID)
	rawResultPath := writeFinalCaptureResult(t, root, "review-result-final-capture-raw", planID, digest, "assignment-dv-1", "claim-dv-1", "agent-capture-dv", rawRef)
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"runtime", "review-result", "submit",
		"--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl", "--expected-revision", "2",
		"--assignment-id", "assignment-dv-1", "--result", rawResultPath,
		"--captures", bufferDir,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("review-result submit rejected raw command_output ref: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	// Consume the second required assignment so this is also a clean-round
	// check, rather than only a one-assignment acceptance check.
	markAssignmentDispatched(t, root, "assignment-qa-1", "agent-capture-qa")
	qaRef := typedEvidenceRef(t, root, "final-capture-qa.md")
	qaResultPath := writeFinalCaptureResult(t, root, "review-result-final-capture-qa", planID, digest, "assignment-qa-1", "claim-qa-1", "agent-capture-qa", qaRef)
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"runtime", "review-result", "submit",
		"--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl", "--expected-revision", "3",
		"--assignment-id", "assignment-qa-1", "--result", qaResultPath,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("final clean review-result submit: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stateBytes, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	reviewState, _ := state["review"].(map[string]any)
	cleanRound, _ := reviewState["clean_round"].(float64)
	if cleanRound != 1 {
		t.Fatalf("two accepted pass results must consume a clean round: review=%s", stateBytes)
	}
}

func finalCaptureExecFixture(t *testing.T, planID, script string) (root, bufferDir, rawRef string) {
	t.Helper()
	root = worktreeProjectRoot(t)
	planPath := minimalReviewPlan(t, t.TempDir(), planID)
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-plan",
		"--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "1", "--file", planPath,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("register review plan: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	markAssignmentDispatched(t, root, "assignment-dv-1", "agent-capture-dv")
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"capture", "exec", "--root", root,
		"--assignment", "assignment-dv-1", "--cwd", root,
		"--", "sh", "-c", script,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("capture exec: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	bufferDir = filepath.Join(root, ".claude", "evidence", "loop-REQ-WORKTREE", "g1", "captures", "assignment-dv-1")
	steps, err := review.LoadCaptureStepsStrict(filepath.Join(bufferDir, "steps.jsonl"))
	if err != nil {
		t.Fatalf("load capture buffer: %v", err)
	}
	if len(steps) != 1 || len(steps[0].Evidence) == 0 {
		t.Fatalf("capture exec must persist one step with evidence refs: %#v", steps)
	}
	if provenance := steps[0].Provenance; provenance == nil || provenance.ReviewPlanID != planID || provenance.ReviewPlanRevision != 1 || provenance.AssignmentID != "assignment-dv-1" || provenance.ExecutionRoot != root || provenance.StartCommit == "" || provenance.EndCommit == "" || provenance.StartCommit != provenance.EndCommit || len(provenance.StartFrozenSubjects) == 0 || len(provenance.EndFrozenSubjects) == 0 {
		t.Fatalf("capture exec must persist plan/assignment/root/frozen-subject provenance: %#v", provenance)
	}
	for _, ref := range steps[0].Evidence {
		if strings.HasPrefix(ref, "command_output:") {
			rawRef = ref
			break
		}
	}
	if rawRef == "" {
		t.Fatalf("capture exec did not persist a command_output ref: %#v", steps[0].Evidence)
	}
	return root, bufferDir, rawRef
}

func TestFinalAuditCaptureRejectsTamperedOutputAndMissingBuffer(t *testing.T) {
	planID := "review-plan-final-capture-tamper"
	root, bufferDir, rawRef := finalCaptureExecFixture(t, planID, "printf clean-capture")
	digest := subjectDigestForPlan(t, root, planID)

	// The reference metadata is unchanged, but the immutable stream file is
	// altered after capture. Submit must check the bytes, not only the suffix.
	outputRel := strings.TrimPrefix(rawRef, "command_output:")
	if index := strings.IndexByte(outputRel, '#'); index >= 0 {
		outputRel = outputRel[:index]
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(outputRel)), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultPath := writeFinalCaptureResult(t, root, "review-result-final-capture-tamper", planID, digest, "assignment-dv-1", "claim-dv-1", "agent-capture-dv", rawRef)
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-result", "submit", "--root", root,
		"--state", ".claude/loop-state.json", "--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2", "--assignment-id", "assignment-dv-1",
		"--result", resultPath, "--captures", bufferDir,
	}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(strings.ToLower(stdout.String()+stderr.String()), "digest mismatch") {
		t.Fatalf("tampered command output must be rejected by digest: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	// Omitting --captures must not turn an automatic output path into legacy
	// manual evidence. Test both the generated command_output form and the
	// prefix-converted path form against a fresh runtime revision.
	root, _, rawRef = finalCaptureExecFixture(t, "review-plan-final-capture-missing-buffer", "printf clean-capture")
	digest = subjectDigestForPlan(t, root, "review-plan-final-capture-missing-buffer")
	resultPath = writeFinalCaptureResult(t, root, "review-result-final-capture-missing-buffer", "review-plan-final-capture-missing-buffer", digest, "assignment-dv-1", "claim-dv-1", "agent-capture-dv", rawRef)
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"runtime", "review-result", "submit", "--root", root,
		"--state", ".claude/loop-state.json", "--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2", "--assignment-id", "assignment-dv-1", "--result", resultPath,
	}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stdout.String()+stderr.String(), "S7_CAPTURE_PROVENANCE") {
		t.Fatalf("automatic command_output without capture buffer must be rejected: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	pathResultPath := writeFinalCaptureResult(t, root, "review-result-final-capture-missing-path", "review-plan-final-capture-missing-buffer", digest, "assignment-dv-1", "claim-dv-1", "agent-capture-dv", finalCommandOutputPathRef(rawRef))
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"runtime", "review-result", "submit", "--root", root,
		"--state", ".claude/loop-state.json", "--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2", "--assignment-id", "assignment-dv-1", "--result", pathResultPath,
	}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stdout.String()+stderr.String(), "S7_CAPTURE_PROVENANCE") {
		t.Fatalf("automatic path ref without capture buffer must be rejected: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestFinalAuditCaptureRejectsFrozenSubjectChangedDuringCommand(t *testing.T) {
	root := worktreeProjectRoot(t)
	writeRecheckCaptureModule(t, root)
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "capture mutation fixture")
	workerPath := filepath.Join(t.TempDir(), "capture-mutation-worker")
	runGit(t, root, "worktree", "add", "-b", "wt/capture-mutation", workerPath, "HEAD")
	planID := "review-plan-final-capture-mutation"
	planPath := minimalReviewPlan(t, workerPath, planID)
	writeRecheckCaptureReviewPlan(t, planPath, root, planID)
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-plan", "--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl", "--expected-revision", "1", "--file", planPath,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("register mutation plan: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	markAssignmentDispatched(t, root, "assignment-qa-1", "agent-capture-mutation")
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"capture", "exec", "--root", root, "--assignment", "assignment-qa-1", "--cwd", workerPath,
		"--", "sh", "-c", "printf 'package captureprobe\\n\\nfunc Probe() int { return 1 }\\n' > capture_probe.go",
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("capture mutation command: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	bufferDir := filepath.Join(root, ".claude", "evidence", "loop-REQ-WORKTREE", "g1", "captures", "assignment-qa-1")
	steps, err := review.LoadCaptureStepsStrict(filepath.Join(bufferDir, "steps.jsonl"))
	if err != nil || len(steps) != 1 {
		t.Fatalf("load mutation capture: steps=%d err=%v", len(steps), err)
	}
	var rawRef string
	for _, ref := range steps[0].Evidence {
		if strings.HasPrefix(ref, "command_output:") {
			rawRef = ref
			break
		}
	}
	if rawRef == "" {
		t.Fatalf("mutation capture has no command_output ref: %#v", steps[0].Evidence)
	}
	digest := subjectDigestForPlan(t, root, planID)
	resultPath := writeFinalCaptureResult(t, root, "review-result-final-capture-mutation", planID, digest, "assignment-qa-1", "claim-qa-1", "agent-capture-mutation", rawRef)
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"runtime", "review-result", "submit", "--root", root,
		"--state", ".claude/loop-state.json", "--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2", "--assignment-id", "assignment-qa-1", "--result", resultPath,
		"--captures", bufferDir,
	}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stdout.String()+stderr.String(), "S7_CAPTURE_PROVENANCE") {
		t.Fatalf("frozen subject changed during command must reject PASS: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestFinalAuditCaptureRejectsUndeclaredWorkerProductDrift(t *testing.T) {
	root := worktreeProjectRoot(t)
	writeRecheckCaptureModule(t, root)
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "capture undeclared drift fixture")
	workerPath := filepath.Join(t.TempDir(), "capture-undeclared-worker")
	runGit(t, root, "worktree", "add", "-b", "wt/capture-undeclared", workerPath, "HEAD")
	planID := "review-plan-final-capture-undeclared"
	planPath := minimalReviewPlan(t, workerPath, planID)
	writeRecheckCaptureReviewPlan(t, planPath, root, planID)
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-plan", "--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl", "--expected-revision", "1", "--file", planPath,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("register undeclared-drift plan: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	markAssignmentDispatched(t, root, "assignment-qa-1", "agent-capture-undeclared")
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"capture", "exec", "--root", root, "--assignment", "assignment-qa-1", "--cwd", workerPath,
		"--", "sh", "-c", "printf 'package extra\\n' > undeclared_probe.go",
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("capture undeclared-drift command: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	bufferDir := filepath.Join(root, ".claude", "evidence", "loop-REQ-WORKTREE", "g1", "captures", "assignment-qa-1")
	steps, err := review.LoadCaptureStepsStrict(filepath.Join(bufferDir, "steps.jsonl"))
	if err != nil || len(steps) != 1 {
		t.Fatalf("load undeclared-drift capture: steps=%d err=%v", len(steps), err)
	}
	var rawRef string
	for _, ref := range steps[0].Evidence {
		if strings.HasPrefix(ref, "command_output:") {
			rawRef = ref
			break
		}
	}
	if rawRef == "" {
		t.Fatalf("undeclared-drift capture has no command_output ref: %#v", steps[0].Evidence)
	}
	digest := subjectDigestForPlan(t, root, planID)
	resultPath := writeFinalCaptureResult(t, root, "review-result-final-capture-undeclared", planID, digest, "assignment-qa-1", "claim-qa-1", "agent-capture-undeclared", rawRef)
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"runtime", "review-result", "submit", "--root", root,
		"--state", ".claude/loop-state.json", "--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2", "--assignment-id", "assignment-qa-1", "--result", resultPath,
		"--captures", bufferDir,
	}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stdout.String()+stderr.String(), "undeclared product drift") {
		t.Fatalf("undeclared worker product drift must reject PASS: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestFinalAuditCaptureWithheldOutputCannotCertifyPass(t *testing.T) {
	planID := "review-plan-final-capture-withheld"
	root, bufferDir, rawRef := finalCaptureExecFixture(t, planID, "printf 'pass'; printf 'word: secret-value'")
	if !strings.Contains(rawRef, "withheld") {
		t.Fatalf("secret output must produce a withheld command_output ref, got %q", rawRef)
	}
	digest := subjectDigestForPlan(t, root, planID)
	resultPath := writeFinalCaptureResult(t, root, "review-result-final-capture-withheld", planID, digest, "assignment-dv-1", "claim-dv-1", "agent-capture-dv", rawRef)
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-result", "submit", "--root", root,
		"--state", ".claude/loop-state.json", "--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2", "--assignment-id", "assignment-dv-1", "--result", resultPath,
		"--captures", bufferDir,
	}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(strings.ToLower(stdout.String()+stderr.String()), "withheld") {
		t.Fatalf("withheld command output must not certify PASS: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func finalCommandOutputPathRef(ref string) string {
	body := strings.TrimPrefix(ref, "command_output:")
	if index := strings.IndexByte(body, ' '); index >= 0 {
		body = body[:index]
	}
	return "path:" + body
}

func writeFinalCaptureResult(t *testing.T, root, resultID, planID, subjectDigest, assignmentID, claimID, producer, evidenceRef string) string {
	t.Helper()
	result := map[string]any{
		"schema_version":      "1.0.0",
		"result_id":           resultID,
		"assignment_id":       assignmentID,
		"assignment_revision": 1,
		"review_plan_id":      planID,
		"review_round":        1,
		"baseline_generation": 1,
		"producer_agent_id":   producer,
		"subject_digest":      subjectDigest,
		"claim_results": []any{map[string]any{
			"claim_id": claimID, "conclusion": "pass",
			"observed": "clean capture command completed", "evidence_refs": []string{evidenceRef},
		}},
		"verdict": "pass",
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs", "reports", resultID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
