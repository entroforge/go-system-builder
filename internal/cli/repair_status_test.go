package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestRuntimeRepairStatusProjectsBlockedResultRecovery(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 4)
	resultRel := ".claude/review/repair/results/repair-result-blocked.json"
	result := map[string]any{
		"result_id": "repair-result-blocked", "assignment_id": "repair-assignment-unit-1", "result": "blocked",
		"changed_artifacts": []any{}, "residual_risks": []string{"the environment cannot execute the approved Contract"},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, resultRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, resultRel), data, 0o644); err != nil {
		t.Fatal(err)
	}
	state["review"].(map[string]any)["repair"] = map[string]any{
		"status": "blocked", "result_ref": resultRel,
		"next_action": "route the blocker to S8 for causal reassessment",
	}
	req039fixtures.WriteState(t, root, state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "repair", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("repair status code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"result_summary"`) || !strings.Contains(stdout.String(), `"result":"blocked"`) || !strings.Contains(stdout.String(), "causal reassessment") {
		t.Fatalf("repair status must expose the blocked result and recovery route: %s", stdout.String())
	}
}

func TestRuntimeRepairTargetedResumeReopensBlockedVerification(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "investigation", 0)
	state["review"].(map[string]any)["repair"] = map[string]any{
		"session_id": "repair-session-blocked", "case_id": "investigation-case-1", "contract_id": "repair-contract-1",
		"contract_ref": ".claude/review/investigation/contracts/repair-contract-1.json", "contract_sha256": strings.Repeat("a", 64),
		"path": ".claude/review/repair/sessions/repair-session-blocked.json", "sha256": strings.Repeat("b", 64), "revision": 1,
		"status": "blocked", "targeted_reverification_refs": []string{".claude/review/repair/reverification/blocked.json"},
		"targeted_reverification_artifacts": []any{}, "failure_route": "blocked", "updated_at": "2026-08-26T00:00:00Z",
		"next_action": "after resolving the blocker, run targeted resume",
	}
	req039fixtures.WriteState(t, root, state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "repair", "targeted", "resume", "--root", root, "--actor", "qa", "--reason", "the browser session was restored"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("targeted resume code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "new independent targeted reverification") || !strings.Contains(stdout.String(), "targeted_reverification") {
		t.Fatalf("targeted resume must expose the next independent verification action: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}
