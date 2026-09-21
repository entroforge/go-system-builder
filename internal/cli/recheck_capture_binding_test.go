package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/review"
)

// TestRecheckCaptureDirtyWorkerCannotCertifyCommittedSubject proves that a
// passing command in a dirty Worker checkout cannot certify the authority's
// frozen product subject. Dirty, non-product diagnostics remain executable;
// the PASS admission gate checks the captured subject window.
func TestRecheckCaptureDirtyWorkerCannotCertifyCommittedSubject(t *testing.T) {
	root := worktreeProjectRoot(t)
	writeRecheckCaptureModule(t, root)
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "capture binding fixture")

	workerPath := filepath.Join(t.TempDir(), "capture-worker")
	runGit(t, root, "worktree", "add", "-b", "wt/capture-binding", workerPath, "HEAD")
	planPath := minimalReviewPlan(t, workerPath, "review-plan-capture-binding")
	writeRecheckCaptureReviewPlan(t, planPath, root, "review-plan-capture-binding")

	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-plan",
		"--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "1", "--file", planPath,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("register plan: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	markAssignmentDispatched(t, root, "assignment-qa-1", "agent-capture-binding")

	// The Worker changes the ReviewPlan.frozen_subjects file. Its test turns
	// green, but the authority checkout remains at the failing baseline and
	// the Worker has no commit carrying this green result.
	probePath := filepath.Join(workerPath, "capture_probe.go")
	if err := os.WriteFile(probePath, []byte("package captureprobe\n\nfunc Probe() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runRecheckCommand(t, workerPath, "go", "test", "."); code != 0 {
		t.Fatalf("worker fixture must be green before capture: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	if code, _, _ := runRecheckCommand(t, root, "go", "test", "."); code == 0 {
		t.Fatal("authority baseline unexpectedly became green before capture")
	}
	workerHead := recheckGitOutput(t, workerPath, "rev-parse", "HEAD")
	workerStatus := recheckGitOutput(t, workerPath, "status", "--porcelain")
	if !strings.Contains(workerStatus, "capture_probe.go") {
		t.Fatalf("worker fixture is not dirty as intended: status=%q", workerStatus)
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"capture", "exec", "--root", root,
		"--assignment", "assignment-qa-1", "--cwd", workerPath,
		"--", "go", "test", ".",
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("capture exec from dirty worker failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	bufferDir := filepath.Join(root, ".claude", "evidence", "loop-REQ-WORKTREE", "g1", "captures", "assignment-qa-1")
	steps, err := review.LoadCaptureStepsStrict(filepath.Join(bufferDir, "steps.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || !strings.Contains(steps[0].Observed, "exit=0") {
		t.Fatalf("capture did not record the worker's green command: %#v", steps)
	}
	captureRefs := recheckCapturePathRefs(steps[0].Evidence)
	if len(captureRefs) == 0 {
		t.Fatalf("capture step has no local stream evidence that can be bound to the authority: %#v", steps[0])
	}

	digest := subjectDigestForPlan(t, root, "review-plan-capture-binding")
	result := map[string]any{
		"schema_version": "1.0.0", "result_id": "review-result-capture-binding",
		"assignment_id": "assignment-qa-1", "assignment_revision": 1,
		"review_plan_id": "review-plan-capture-binding", "review_round": 1,
		"baseline_generation": 1, "producer_agent_id": "agent-capture-binding",
		"subject_digest": digest,
		"claim_results": []any{map[string]any{
			"claim_id": "claim-qa-1", "conclusion": "pass",
			"observed": "Probe returns 1 under the captured worker command flow",
		}},
		"checks": []any{map[string]any{
			"name": "captured worker test", "command": "go test .", "result": "pass",
			"evidence_refs": captureRefs,
		}},
		"verdict": "pass",
	}
	resultBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(workerPath, "review-result-capture-binding.json")
	if err := os.WriteFile(resultPath, append(resultBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{
		"runtime", "review-result", "submit",
		"--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2", "--assignment-id", "assignment-qa-1",
		"--result", resultPath, "--captures", bufferDir,
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("review-result submit accepted a pass from dirty worker HEAD=%s without tested-commit binding; authority baseline remains red; stdout=%s stderr=%s", workerHead, stdout.String(), stderr.String())
	}
	lower := strings.ToLower(stderr.String())
	if !strings.Contains(lower, "commit") && !strings.Contains(lower, "subject") && !strings.Contains(lower, "dirty") && !strings.Contains(lower, "provenance") {
		t.Fatalf("capture submit rejected for an unrelated reason: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func writeRecheckCaptureModule(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/recheck-capture\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "capture_probe.go"), []byte("package captureprobe\n\nfunc Probe() int { return 0 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "capture_probe_test.go"), []byte("package captureprobe\n\nimport \"testing\"\n\nfunc TestProbe(t *testing.T) {\n\tif Probe() != 1 {\n\t\tt.Fatalf(\"baseline probe is expected to fail\")\n\t}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRecheckCaptureReviewPlan(t *testing.T, path, authorityRoot, planID string) {
	t.Helper()
	subjectBytes, err := os.ReadFile(filepath.Join(authorityRoot, "capture_probe.go"))
	if err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{
		"schema_version": "1.0.0", "review_plan_id": planID,
		"review_round": 1, "baseline_generation": 1,
		"frozen_subjects": []any{map[string]any{
			"path": "capture_probe.go", "sha256": fmt.Sprintf("%x", sha256.Sum256(subjectBytes)),
			"kind": "product_code",
		}},
		"claims": []any{
			map[string]any{
				"claim_id": "claim-dv-1", "lens": "delivery",
				"target": "capture_probe.go", "assertion": "Probe returns 1",
				"oracle": "go test .", "method": "command_flow", "applicability": "required",
				"source_refs": []string{"REQ-CAPTURE-BINDING"},
			},
			map[string]any{
				"claim_id": "claim-qa-1", "lens": "qa",
				"target": "capture_probe.go", "assertion": "Probe returns 1",
				"oracle": "go test .", "method": "command_flow", "applicability": "required",
				"source_refs": []string{"REQ-CAPTURE-BINDING"},
			},
			map[string]any{
				"claim_id": "claim-e2e-na", "lens": "e2e", "target": "n/a",
				"assertion": "no surface", "oracle": "impact", "method": "impact",
				"applicability": "not_applicable", "na_rationale": "unit-level command flow",
				"na_checklist_id": "REQ-CAPTURE-BINDING#ui_impact",
				"source_refs":     []string{"REQ-CAPTURE-BINDING#ui"},
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id": "assignment-dv-1", "lens": "delivery",
				"claim_ids":            []string{"claim-dv-1"},
				"non_overlap_boundary": "owns delivery trace", "execution_wave": "static",
			},
			map[string]any{
				"assignment_id": "assignment-qa-1", "lens": "qa",
				"claim_ids":            []string{"claim-qa-1"},
				"non_overlap_boundary": "owns captured command flow", "execution_wave": "static",
			},
		},
		"e2e_coverage_state":              "not_applicable",
		"verification_artifact_workspace": nil,
		"dispatch_capacity_policy":        "coverage_complete",
		"created_by":                      "recheck", "created_at": "2026-09-20T00:00:00Z",
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runRecheckCommand(t *testing.T, dir, name string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	}
	return 1, stdout.String(), err.Error()
}

func recheckGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func recheckCapturePathRefs(refs []string) []string {
	result := make([]string, 0, len(refs))
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "command_output:") {
			continue
		}
		body := strings.TrimPrefix(ref, "command_output:")
		marker := "#sha256="
		index := strings.Index(body, marker)
		if index < 0 {
			continue
		}
		rel := body[:index]
		digest := strings.Fields(body[index+len(marker):])
		if rel == "" || len(digest) == 0 || len(digest[0]) != 64 {
			continue
		}
		result = append(result, "path:"+rel+marker+digest[0])
	}
	return result
}
