package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// TestReviewResultCapturesAcceptsBufferDir proves the `--captures` flag
// accepts the buffer DIRECTORY (what `capture step` prints) and not only the
// steps.jsonl file: a directory used to load zero steps silently, so the
// finding's empty timeline never absorbed the buffered observation steps.
func TestReviewResultCapturesAcceptsBufferDir(t *testing.T) {
	root := worktreeProjectRoot(t)
	dir := t.TempDir()

	planPath := minimalReviewPlan(t, dir, "review-plan-captures-dir")
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-plan",
		"--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "1", "--file", planPath,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("register plan: code=%d stderr=%s", code, stderr.String())
	}
	markAssignmentDispatched(t, root, "assignment-qa-1", "agent-qa-cap")

	// Observation buffer exactly where `capture step` puts it; the test
	// passes the DIRECTORY to --captures.
	bufferDir := filepath.Join(root, ".claude", "evidence", "loop-REQ-WORKTREE", "g1", "captures", "assignment-qa-1")
	if err := os.MkdirAll(bufferDir, 0o755); err != nil {
		t.Fatal(err)
	}
	trailRef := typedEvidenceRef(t, root, "trail.md")
	lines := `{"sequence":1,"action":"walk error branch","observed":"error dropped at boundary","evidence_refs":["` + trailRef + `"]}
{"sequence":2,"action":"forced failure","observed":"partial state persisted","evidence_refs":["` + trailRef + `"]}
`
	if err := os.WriteFile(filepath.Join(bufferDir, "steps.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	digest := subjectDigestForPlan(t, root, "review-plan-captures-dir")
	result := map[string]any{
		"schema_version": "1.0.0", "result_id": "review-result-captures-dir",
		"assignment_id": "assignment-qa-1", "assignment_revision": 1,
		"review_plan_id": "review-plan-captures-dir", "review_round": 1,
		"baseline_generation": 1, "producer_agent_id": "agent-qa-cap",
		"subject_digest": digest,
		"claim_results": []any{map[string]any{
			"claim_id": "claim-qa-1", "conclusion": "fail",
			"observed": "error path drops the store error", "evidence_refs": []string{trailRef},
		}},
		"findings": []any{map[string]any{
			"schema_version": "1.0.0", "finding_id": "finding-captures-dir-1",
			"claim_id": "claim-qa-1", "lens": "qa", "severity": "P1",
			"expected":         "the store error propagates to the caller",
			"authority_refs":   []string{"docs/contracts/CONTRACTS-001.md#errors"},
			"observed":         "update returns nil after the store write fails",
			"observation_mode": "code_inspection",
			"reproducibility":  "always",
			"evidence_refs":    []string{trailRef},
			"correlation_refs": []string{"service.go:87"},
			"visible_impact":   "callers persist partial state under success",
			"negative_facts":   []string{"delete path propagates correctly"},
			"open_questions":   []string{"does any caller rely on the drop?"},
			"encounter": map[string]any{
				"journey_summary":      "walked update error handling",
				"inspection_entry":     "internal/example/service.go:Update",
				"symbol_trail":         "Update -> store.Write (error dropped)",
				"last_good_checkpoint": "store.Write returns a typed error",
				"wall_action":          "the error return is discarded",
				"first_bad_checkpoint": "Update returns nil although the write failed",
				"terminal_state":       "caller commits and reports success",
				"timeline":             []any{},
			},
		}},
		"verdict": "finding",
	}
	resultBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(dir, "review-result-captures.json")
	if err := os.WriteFile(resultPath, append(resultBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"runtime", "review-result", "submit",
		"--root", root, "--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2",
		"--assignment-id", "assignment-qa-1",
		"--result", resultPath,
		"--captures", bufferDir,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("review-result submit: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}

	findingBytes, err := os.ReadFile(filepath.Join(root, ".claude", "evidence", "loop-REQ-WORKTREE", "g1", "findings", "finding-captures-dir-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var finding map[string]any
	if err := json.Unmarshal(findingBytes, &finding); err != nil {
		t.Fatal(err)
	}
	timeline, _ := finding["encounter"].(map[string]any)["timeline"].([]any)
	if len(timeline) != 2 {
		t.Fatalf("buffer dir must merge its steps.jsonl into the empty timeline, got %d steps: %s", len(timeline), findingBytes)
	}
}

// subjectDigestForPlan computes the frozen-baseline digest from the
// registered plan file (subjectDigestFor hardcodes the worktree fixture id).
func subjectDigestForPlan(t *testing.T, root, planID string) string {
	t.Helper()
	planBytes, err := os.ReadFile(filepath.Join(root, ".claude", "review", "plans", planID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		FrozenSubjects []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"frozen_subjects"`
	}
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0, len(plan.FrozenSubjects))
	for _, subject := range plan.FrozenSubjects {
		lines = append(lines, subject.Path+":"+subject.SHA256)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

// setStateAgentWorking flips the seeded agent row to `working` so the submit
// transaction's reviewer-state check passes.
func setStateAgentWorking(t *testing.T, root, agentID string) {
	t.Helper()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	agents, _ := state["entities"].(map[string]any)["agents"].([]any)
	for _, row := range agents {
		if agent := row.(map[string]any); agent["id"] == agentID {
			agent["state"] = "working"
		}
	}
	patched, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(patched, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
