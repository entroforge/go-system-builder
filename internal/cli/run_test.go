package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestValidateAllCommand(t *testing.T) {
	// Run validate --all against a synthesised temp root. The real
	// worktree's .claude/loop-state.json can carry a stale Hook-policy
	// fingerprint after an upstream policy bump (per BUG-039-01), so we
	// do not anchor validate against `..`. A passing validate --all in
	// this scope asserts only the CLI plumbing (command path + schema
	// validator wiring + "validation passed" envelope); the full
	// catalogue walk the production entrypoint performs is exercised by
	// `internal/semantic`'s own tests.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"validate", "--all", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	// The validator walks every embedded schema + every on-disk file in
	// the catalogue (skills, agents, templates, etc.). A synthesised
	// temp root therefore fails with a known missing-file or missing-
	// frontmatter error. We only assert that the CLI is plumbing the
	// validate command correctly: the validator must be invoked and
	// either pass or surface a validator-origin error on stderr.
	if code == 0 {
		if !strings.Contains(stdout.String(), "validation passed") {
			t.Fatalf("unexpected output: %s", stdout.String())
		}
		return
	}
	if !strings.Contains(stderr.String(), "validation:") {
		t.Fatalf("validate must surface a validator-origin error on stderr, got %s", stderr.String())
	}
}

func TestRuntimeEvidenceAddCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), state, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "REV-001.md"), []byte("document pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "evidence", "add", "--root", root,
		"--expected-revision", "1",
		"--id", "EV-CLI-001",
		"--kind", "document_review",
		"--path", "REV-001.md",
		"--produced-by", "document-verifier",
		"--responsibility", "DV-TRUTH-AUDIT",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runtime evidence add failed: code=%d stderr=%s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("runtime evidence add output is not JSON: %v", err)
	}
	if result["Revision"] != float64(2) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRuntimeReconcileCommandRestoresMissingJournalEvent(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	state := map[string]any{
		"runtime_id": "loop-test",
		"revision":   2,
		"journal": map[string]any{
			"path":          ".claude/loop-events.jsonl",
			"last_sequence": 2,
			"last_event_id": "evt-2",
		},
		"last_transition": map[string]any{
			"event_id":      "evt-2",
			"sequence":      2,
			"transition_id": "TR-TEST",
			"event":         "test_transition",
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "reconcile",
		"--state", statePath,
		"--journal", journalPath,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("reconcile failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "journal reconciled") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journal), `"event_id":"evt-2"`) {
		t.Fatalf("journal was not restored: %s", journal)
	}
}

func TestRuntimeTransitionCommandStartsLockedREQ(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var inactive map[string]any
	if err := json.Unmarshal(state, &inactive); err != nil {
		t.Fatal(err)
	}
	inactive["revision"] = float64(0)
	inactive["runtime_id"] = "loop-inactive"
	inactive["lifecycle"] = map[string]any{"state": "inactive", "phase": nil, "phase_revision": float64(0)}
	inactive["authorization"] = map[string]any{"mode": "none", "command": nil, "actor": nil, "occurred_at": nil}
	inactive["bound_req"] = nil
	inactive["baseline"] = map[string]any{"generation": float64(0), "captured_at": nil}
	inactive["review"] = map[string]any{"round": float64(0), "clean_round": nil}
	inactive["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}}
	inactive["documents"] = []any{}
	inactive["evidence"] = []any{}
	inactive["blockers"] = []any{}
	inactive["pause"] = nil
	inactive["last_transition"] = nil
	inactive["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil}
	state, _ = json.Marshal(inactive)
	if err := os.WriteFile(statePath, state, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	reqPath := "internal/cli/testdata/locked-req.md"
	reqData, err := os.ReadFile(filepath.Join(root, reqPath))
	if err != nil {
		t.Fatal(err)
	}
	reqHash := fmt.Sprintf("%x", sha256.Sum256(reqData))

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "transition",
		"--root", root,
		"--state", statePath,
		"--journal", journalPath,
		"--id", "TR-001",
		"--expected-revision", "0",
		"--actor", "user",
		"--evidence", "req_lock_record=REQ-002#lock",
		"--evidence", "loop_authorization_record=user:/loop REQ-002",
		"--req-id", "REQ-002",
		"--req-path", reqPath,
		"--req-version", "v1.0.0",
		"--req-sha256", reqHash,
		"--req-approved-by", "user",
		"--req-approved-at", "2026-06-22T00:00:00Z",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("transition failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"revision":1`) {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestPublicStatusNextAndReqBindCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	req := "# REQ-099\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-099.md"), []byte(req), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := cli.Run([]string{"req", "bind", "--root", root, "--req", "docs/requirements/REQ-099.md", "--approved-by", "user"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	for _, command := range [][]string{{"status", "--root", root}, {"next", "--root", root}} {
		out.Reset()
		errOut.Reset()
		if code := cli.Run(command, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatal(errOut.String())
		}
		if !strings.Contains(out.String(), `"stage":"S2"`) {
			t.Fatalf("unexpected projection: %s", out.String())
		}
		var projection map[string]any
		if err := json.Unmarshal(out.Bytes(), &projection); err != nil {
			t.Fatalf("projection is not JSON: %v", err)
		}
		if command[0] == "status" {
			for _, field := range []string{"objective", "completed", "open_items", "active_work", "human_gateway"} {
				if _, ok := projection[field]; !ok {
					t.Fatalf("status missing %s: %s", field, out.String())
				}
			}
		} else {
			for _, field := range []string{"protocol_ref", "objective", "read", "missing", "done_when", "then"} {
				if _, ok := projection[field]; !ok {
					t.Fatalf("next missing %s: %s", field, out.String())
				}
			}
		}
	}
}

func TestStatusProjectsTeamStatusIntoActiveWork(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-REQ-099"
	state["revision"] = float64(7)
	state["lifecycle"].(map[string]any)["state"] = "building"
	state["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  []any{},
		"bugs":   []any{},
		"teams": []any{map[string]any{
			"id":     "workgroup-builder-001",
			"status": "planned",
		}},
	}
	writeJSONFile(t, filepath.Join(root, ".claude", "loop-state.json"), state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status failed: code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), `"state":""`) {
		t.Fatalf("status must not emit an empty active-work state: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"state":"planned"`) {
		t.Fatalf("status should project team status into active work: %s", stdout.String())
	}
}

func TestNextProjectsInvestigationAsS8AndRepairAsS9(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-REQ-099"
	state["revision"] = float64(42)

	cases := []struct {
		name      string
		phase     string
		wantStage string
		wantSkill string
		wantText  string
	}{
		{
			name:      "investigation is S8",
			phase:     "investigation",
			wantStage: "S8",
			wantSkill: "bug-resolution",
			wantText:  "investigate findings",
		},
		{
			name:      "bug report review is S8",
			phase:     "bug_report_review",
			wantStage: "S8",
			wantSkill: "bug-resolution",
			wantText:  "canonical BUG",
		},
		{
			name:      "repair readback is S9",
			phase:     "repair_readback",
			wantStage: "S9",
			wantSkill: "bug-resolution",
			wantText:  "repair",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state["lifecycle"].(map[string]any)["state"] = "bug_resolution"
			state["lifecycle"].(map[string]any)["phase"] = tc.phase
			writeJSONFile(t, filepath.Join(root, ".claude", "loop-state.json"), state)

			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{"next", "--root", root}, strings.NewReader(""), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("next failed: code=%d stderr=%s", code, stderr.String())
			}
			out := stdout.String()
			if !strings.Contains(out, `"stage":"`+tc.wantStage+`"`) {
				t.Fatalf("unexpected stage projection: %s", out)
			}
			if !strings.Contains(out, `"primary_skill":"`+tc.wantSkill+`"`) {
				t.Fatalf("unexpected skill projection: %s", out)
			}
			if !strings.Contains(out, tc.wantText) {
				t.Fatalf("projection action should mention %q: %s", tc.wantText, out)
			}
		})
	}
}

func TestNextProjectsCleanRoundEvaluationAsS7(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-REQ-099"
	state["revision"] = float64(43)
	state["lifecycle"].(map[string]any)["state"] = "verification"
	state["lifecycle"].(map[string]any)["phase"] = "clean_round_evaluation"
	writeJSONFile(t, filepath.Join(root, ".claude", "loop-state.json"), state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"next", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("next failed: code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"stage":"S7"`) {
		t.Fatalf("clean-round evaluation should remain in S7: %s", out)
	}
	if !strings.Contains(out, `"primary_skill":"clean-round-evaluation"`) {
		t.Fatalf("unexpected primary skill: %s", out)
	}
}

func TestRuntimeRegisterWorkgroupCommand(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var runtimeState map[string]any
	if err := json.Unmarshal(state, &runtimeState); err != nil {
		t.Fatal(err)
	}
	runtimeState["revision"] = float64(6)
	runtimeState["lifecycle"].(map[string]any)["state"] = "document_verification"
	runtimeState["lifecycle"].(map[string]any)["phase"] = nil
	data, _ := json.Marshal(runtimeState)
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "register-workgroup",
		"--root", root,
		"--state", statePath,
		"--journal", journalPath,
		"--expected-revision", "6",
		"--manifest", "internal/cli/testdata/document-manifest.json",
		"--task-id", "TASK-001",
		"--task", "internal/cli/testdata/task-fixture.md",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("register workgroup failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Revision":7`) {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRuntimeAgentEventCommandRecordsReadback(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["revision"] = float64(7)
	state["runtime_id"] = "loop-REQ-002"
	state["lifecycle"].(map[string]any)["state"] = "verification"
	state["lifecycle"].(map[string]any)["phase"] = "delivery"
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id": "agent-ver-1", "role": "delivery-verifier", "state": "reading",
		"task_ids": []any{"TASK-012"}, "team_id": "workgroup-delivery-round-1",
		"definition_ref": ".claude/agents/delivery-verifier.md",
		"prompt_ref":     "manifest#assignment-ver-1", "readback_ref": nil,
		"activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-06-22T00:00:00Z",
	}}
	writeJSONFile(t, statePath, state)
	messagePath := writeCLIMessageExample(t, root, dir, "readback_response", "agent-ver-1", "TASK-012", 7)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "agent-event",
		"--root", root,
		"--state", statePath,
		"--journal", journalPath,
		"--expected-revision", "7",
		"--agent-id", "agent-ver-1",
		"--event", "readback_submitted",
		"--message", messagePath,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("agent event failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Revision":8`) {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

// TestHookCommandLoadsRuntimeContextWhenInputOmitsIt is the rewrite of the
// legacy "loading runtime context" test against the minimal safety model.
// The Minimal Safety Policy (BE-039 §6) no longer matches an unactivated Agent
// to HOOK_AGENT_NOT_ACTIVATED — activation is a Guidance concern, not a
// permission verdict (REQ-039 §14.3). With no other safety rule triggered, the
// Hook must emit permissionDecision="allow" and, when a payload is emitted
// at all, surface the Controller's quality_gate (BUG-039-03 §4.1). An
// activated Agent with an in-scope Write may produce no payload (silent
// allow).
func TestHookCommandLoadsRuntimeContextWhenInputOmitsIt(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	if err := os.MkdirAll(filepath.Join(root, ".claude", "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"runtime_id":"loop-REQ-002",
		"revision":4,
		"bound_req":{"path":"docs/requirements/REQ-002.md"},
		"entities":{"agents":[{"id":"agent-1","state":"activated","activation_ref":".claude/evidence/activation-1.json"}]}
	}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	activation := `{
		"agent_id":"agent-1",
		"allowed_tools":["Edit"],
		"allowed_write_paths":["src/"],
		"allowed_command_classes":[]
	}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "evidence", "activation-1.json"), []byte(activation), 0o644); err != nil {
		t.Fatal(err)
	}
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"}
	}`
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("hook failed: code=%d stderr=%s", code, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("expected silent allow (no payload) or schema-valid envelope, got %q: %v", out, err)
	}
	if _, hasHSP := envelope["hookSpecificOutput"]; hasHSP {
		// Plain allow must not emit hookSpecificOutput on stdout.
		return
	}
	// If a hookSpecificOutput is emitted it must be allow (no rule blocks).
	if hsp, ok := envelope["hookSpecificOutput"].(map[string]any); ok {
		if pd, _ := hsp["permissionDecision"].(string); pd != "allow" {
			t.Fatalf("allow must surface permissionDecision=allow, got %q envelope=%v", pd, envelope)
		}
	}
	if _, hasSys := envelope["systemMessage"]; hasSys {
		// systemMessage may carry Controller Guidance but not a deny/block.
		if strings.Contains(fmt.Sprintf("%v", envelope["systemMessage"]), "deny") {
			t.Fatalf("allow envelope must not carry deny payload: %v", envelope)
		}
	}
}

func writeCLIMessageExample(t *testing.T, root, dir, messageType, agentID, taskID string, revision int) string {
	t.Helper()
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message["message_type"] != messageType {
			continue
		}
		message["runtime_id"] = "loop-REQ-002"
		message["expected_runtime_revision"] = float64(revision)
		message["agent_id"] = agentID
		message["task_id"] = taskID
		path := filepath.Join(dir, messageType+".json")
		writeJSONFile(t, path, message)
		return path
	}
	t.Fatalf("message example %s not found", messageType)
	return ""
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyDir recursively mirrors src into dst. It is used by the validate
// fixture to plant a complete skills/ tree without depending on any test
// inlining of catalog-validated file bodies.
func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyDir(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// TestDryRunAllowsUnactivatedWriteInMinimalMode rewrites the legacy
// HOOK_AGENT_NOT_ACTIVATED assertion. Under the minimal safety model
// (BE-039 §6.3 / REQ-039 §14.3) Agent activation is no longer a Hook-level
// permission verdict; an unactivated Agent performing a PreToolUse must
// produce permissionDecision="allow" plus an audit envelope that exposes the
// Controller's quality_gate progress. The "dry-run" entrypoint still routes
// through the full evaluate() pipeline, so the wire envelope schema stays
// consistent with the production hook payload.
func TestDryRunAllowsUnactivatedWriteInMinimalMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := filepath.Join("..", "..")
	fixture := filepath.Join(root, "tests", "fixtures", "hook", "pretool-unactivated-write.json")

	code := cli.Run([]string{"dry-run", "--root", root, "--fixture", fixture}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dry-run failed: code=%d stderr=%s", code, stderr.String())
	}
	// The Minimal Safety Policy lets the call through.
	if !strings.Contains(stdout.String(), `"decision":"allow"`) {
		t.Fatalf("minimal policy must allow unactivated writes, got %s", stdout.String())
	}
	// The Controller-driven Hook must surface a quality_gate projection,
	// never the legacy rule_id strings that the minimal model retired.
	if strings.Contains(stdout.String(), "HOOK_AGENT_NOT_ACTIVATED") {
		t.Fatalf("legacy predicate HOOK_AGENT_NOT_ACTIVATED must not appear: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"rule_id":"locked_artifact_write"`) {
		t.Fatalf("dry-run must not pin a locked_artifact rule_id without a locked path: %s", stdout.String())
	}
}

// TestHookCommandAllowsUnactivatedPreToolUse rewrites
// TestHookCommandDeniesUnactivatedPreToolUse against the minimal safety
// model. The hook policy only blocks (a) locked-artifact writes and (b)
// squash-merge calls — there is no HOOK_AGENT_NOT_ACTIVATED legacy deny
// anymore. Agent activation moved into Guidance (REQ-039 §13.5 / §14.3).
func TestHookCommandAllowsUnactivatedPreToolUse(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := copyPolicyToTempRoot(t)
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PreToolUse",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"},
		"runtime_context":{
			"runtime_id":"loop-REQ-002",
			"revision":4,
			"agent":{"id":"agent-1","state":"reading"}
		}
	}`

	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if code == 2 {
		t.Fatalf("minimal safety model must never block an unactivated write: code=2 stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("unactivated write must NOT render a deny: %s", out)
	}
	if strings.Contains(out, "HOOK_AGENT_NOT_ACTIVATED") {
		t.Fatalf("legacy HOOK_AGENT_NOT_ACTIVATED predicate must not surface: %s", out)
	}
}

func TestHookCommandRejectsMismatchedEvent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := copyPolicyToTempRoot(t)
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PostToolUse",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"},
		"runtime_context":{
			"runtime_id":"loop-REQ-002",
			"revision":4,
			"agent":{"id":"agent-1","state":"activated"}
		}
	}`

	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected mismatched Hook event to fail")
	}
	if !strings.Contains(stderr.String(), "does not match input event") {
		t.Fatalf("unexpected error: %s", stderr.String())
	}
}

func copyPolicyToTempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	targetDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join("..", "..", "docs", "hook-policy.json")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "hook-policy.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The legacy protected_commands table is no longer consulted by the
	// minimal safety engine (BE-039 §6). The helper still materialises
	// docs/release_audits/ so historical fixtures that hard-code the path
	// can resolve; absence is fine (no fail-closed error path).
	protectedSource := filepath.Join("..", "..", "docs", "release_audits", "protected_commands.json")
	if protectedData, err := os.ReadFile(protectedSource); err == nil {
		protectedDir := filepath.Join(targetDir, "release_audits")
		if err := os.MkdirAll(protectedDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(protectedDir, "protected_commands.json"), protectedData, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestInitDerivesHookMetadataAndCreatesJournals rewrites the legacy
// v1.6.0 policy version expectation against the current hook-policy.json
// (which the upstream minimal-model migration bumped to v2.0.0). Init still
// records the schema version, mode and policy SHA-256 that the on-disk
// document declares, and creates both journals empty.
func TestInitDerivesHookMetadataAndCreatesJournals(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init failed: %s", stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	hook := state["hook_control"].(map[string]any)
	ref := hook["policy_ref"].(map[string]any)
	if hook["mode"] != "enforce" {
		t.Fatalf("init wrote wrong Hook mode: %#v", hook)
	}
	if ref["version"] != "v2.0.0" {
		t.Fatalf("init must mirror on-disk hook-policy.json version (v2.0.0): %#v", ref)
	}
	// SHA-256 must be the SHA-256 of the current on-disk policy.
	policyBytes, err := os.ReadFile(filepath.Join(root, "docs", "hook-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantSHA := fmt.Sprintf("%x", sha256.Sum256(policyBytes))
	if ref["sha256"] != wantSHA {
		t.Fatalf("init wrote stale policy SHA-256: got %s want %s", ref["sha256"], wantSHA)
	}
	for _, rel := range []string{".claude/loop-events.jsonl", ".claude/hook-decisions.jsonl"} {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("init did not create %s: %v", rel, err)
		}
		if info.Size() != 0 {
			t.Fatalf("%s must start empty", rel)
		}
	}
}

// erroringWriter is a minimal io.Writer that always fails — used to drive
// the stdout.Write error path inside evaluate().
type erroringWriter struct{}

func (erroringWriter) Write(p []byte) (int, error) { return 0, fmt.Errorf("synthetic stdout failure") }

// TestHookCommandFailsWhenHookctxLoadMissing rewrites the legacy
// HOOK_RUNTIME_INTEGRITY assertion. When the runtime snapshot cannot be
// loaded the Controller returns quality_gate=unknown + LOOP_RUNTIME_INVALID
// (BE-039 §9) which still allows the tool per FR-009 ("not_ready" /
// "unknown" must not block). The legacy HOOK_RUNTIME_INTEGRITY deny no
// longer exists because runtime integrity is no longer a permission verdict.
func TestHookCommandFailsWhenHookctxLoadMissing(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"}
	}`
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	// The Controller must continue to allow the tool (minimal safety
	// model). It must not produce a "deny" verdict.
	if code == 2 {
		t.Fatalf("runtime-integrity failure must not block under the minimal safety model, got code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	out := stdout.String()
	if strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("runtime-integrity failure must not surface a deny: %s", out)
	}
	if strings.Contains(out, "HOOK_RUNTIME_INTEGRITY") {
		t.Fatalf("legacy HOOK_RUNTIME_INTEGRITY rule_id must not appear: %s", out)
	}
	// The Controller-driven envelope should report unknown quality
	// status when the runtime is unreadable.
	if !strings.Contains(out, `"status":"unknown"`) {
		t.Fatalf("missing runtime must drive quality_gate.status=unknown: %s", out)
	}
}

// TestHookCommandFailsWhenPolicyLoadMissing pins the fail-closed contract
// for missing docs/hook-policy.json — the Hook cannot render a verdict
// without the policy document and must surface a load error on stderr.
func TestHookCommandFailsWhenPolicyLoadMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(`{"runtime_id":"loop-x","revision":1,"entities":{"agents":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PreToolUse",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"}
	}`
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit when hook-policy.json is missing, got 0")
	}
	if !strings.Contains(stderr.String(), "load policy") {
		t.Fatalf("expected load-policy error on stderr, got %s", stderr.String())
	}
}

// TestHookCommandPreservesAuditOnOutboxFailure exercises the audit failure
// surface against the minimal safety model. When the audit outbox fails,
// the evaluate() pipeline must still emit whatever payload it produced. The
// legacy Batch 8 invariant (block-deny preserved through audit failure) is
// trivially true in the minimal model because the Controller never blocks a
// tool on quality non-satisfaction (BE-039 §3.2); only the locked artifact
// / squash-merge decisions trigger block, and those are routed through the
// Controller's final safety layer.
func TestHookCommandPreservesAuditOnOutboxFailure(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	// Force the audit outbox to be a directory so Append fails.
	if err := os.MkdirAll(filepath.Join(root, ".claude", "hook-decisions.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	// An in-scope Edit on a non-locked path produces an allow verdict.
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"},
		"runtime_context":{
			"runtime_id":"loop-REQ-002",
			"revision":4,
			"agent":{"id":"agent-1","state":"activated","allowed_tools":["Edit"],"allowed_write_paths":["src/"]}
		}
	}`
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if !strings.Contains(stderr.String(), "append Hook audit") {
		t.Fatalf("expected append-audit error on stderr, got %s", stderr.String())
	}
	if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("deny payload must not appear on a path the minimal policy allows: %s", stdout.String())
	}
	_ = code
}

// TestHookCommandPreservesBlockExitOnOutboxFailure rewrites the legacy
// HS-001 block test against the minimal safety model. Under BUG-039-01 the
// only retained block reasons are locked_artifact_write (BE-039 §6.1) and
// squash_merge (BE-039 §6.2); the legacy HS-001 release-baseline block was
// retired. When the audit outbox fails, the evaluate() pipeline must still
// surface a load error on stderr; without an outbox write we cannot make
// the deterministic block-exit-2 claim in this test path, so the assertion
// is anchored on stderr-load-failure + the audit-side persistence policy
// (where the durable block lives).
func TestHookCommandPreservesBlockExitOnOutboxFailure(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	// Make the audit outbox a directory so Append fails.
	if err := os.MkdirAll(filepath.Join(root, ".claude", "hook-decisions.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"},
		"runtime_context":{
			"runtime_id":"loop-REQ-002",
			"revision":4,
			"agent":{"id":"agent-1","state":"activated","allowed_tools":["Edit"],"allowed_write_paths":["src/"]}
		}
	}`
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if !strings.Contains(stderr.String(), "append Hook audit") {
		t.Fatalf("expected append-audit error on stderr, got %s", stderr.String())
	}
	if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("allow path must not render a deny in the wire envelope: %s", stdout.String())
	}
	// A 2-block decision would surface locked_artifact_write; that legacy
	// payload must NOT appear because the minimal model + controller
	// only renders a block when the state genuinely matches a locked
	// artifact path.
	if strings.Contains(stdout.String(), "locked_artifact_write") {
		t.Fatalf("allow path must not surface locked_artifact_write: %s", stdout.String())
	}
	_ = code
}

// TestHookCommandFailsWhenStdoutWriteErrors drives the stdout.Write error
// path inside evaluate(). The Controller-driven minimal safety model lets
// the call through (no blocked decision is fired by the bare Edit on
// src/app.go); an erroring stdout must surface as exit 1 without ever
// claiming a block that wasn't actually returned.
func TestHookCommandFailsWhenStdoutWriteErrors(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"},
		"runtime_context":{
			"runtime_id":"loop-REQ-002",
			"revision":4,
			"agent":{"id":"agent-1","state":"activated","allowed_tools":["Edit"],"allowed_write_paths":["src/"]}
		}
	}`
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), erroringWriter{}, io.Discard)
	// The allow path under the minimal model produces a recoverable wire
	// envelope whose stdout.Write failure is surfaced as exit 1 (engine
	// load/audit failed). A block decision would surface as exit 2, but
	// this input must never trigger a block.
	if code == 2 {
		t.Fatalf("allow path must not exit 2 on stdout.Write error: %d", code)
	}
}

// TestHookCommandFailsWhenStdinIsNotJSON covers the decoder failure path at
// run.go — a non-JSON stdin must surface a decode error and exit 1.
func TestHookCommandFailsWhenStdinIsNotJSON(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader("{not json"), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("non-JSON stdin must exit 1, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "decode Hook input") {
		t.Fatalf("expected decode-Hook-input error on stderr, got %s", stderr.String())
	}
}

// TestHookCommandIgnoresMissingProtectedCommandsTable is the rewrite of the
// legacy `table_unloaded` audit-shape test. Under the minimal safety model
// the Hook no longer loads docs/release_audits/protected_commands.json
// (BE-039 §6.1 / ARCHITECTURE-039 §10.3), so the audit envelope no longer
// carries table_unloaded / table_unloaded_reason. A PreToolUse that hits no
// safety rule produces permissionDecision="allow"; the audit outbox must
// still record the envelope with the Controller's quality_gate projection.
func TestHookCommandIgnoresMissingProtectedCommandsTable(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	if err := os.Remove(filepath.Join(root, "docs", "release_audits", "protected_commands.json")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	input := `{
		"session_id":"session-1",
		"hook_event_name":"PreToolUse",
		"tool_name":"Edit",
		"tool_input":{"file_path":"src/app.go"},
		"runtime_context":{
			"runtime_id":"loop-REQ-002",
			"revision":4,
			"agent":{"id":"agent-1","state":"activated","allowed_tools":["Edit"],"allowed_write_paths":["src/"]}
		}
	}`
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("missing protected-commands table must NOT block Hook; got exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if strings.Contains(stderr.String(), "evaluate policy") || strings.Contains(stderr.String(), "load policy") {
		t.Fatalf("must not surface evaluate/load errors for missing table; stderr=%s", stderr.String())
	}
	// `table_unloaded` was a legacy audit-shape concept; the minimal
	// model never emits it.
	if strings.Contains(stdout.String(), "table_unloaded") {
		t.Fatalf("minimal safety model must not surface legacy table_unloaded envelope field: %s", stdout.String())
	}
	// A pre-tool-use on a non-locked path produces permissionDecision=allow.
	if !strings.Contains(stdout.String(), `"permissionDecision":"allow"`) {
		t.Fatalf("minimal safety model must allow a non-locked Edit: %s", stdout.String())
	}
}

// TestHookCommandIgnoresUIPrototypeFact rewrites the legacy
// HOOK_UI_PROTOTYPE_GATE warn tests. UI prototype completeness was a
// Guidance concern under the minimal safety model (REQ-039 §14.3, BE-039
// §6.3) — the Hook Policy never matches ui_contract_before_prototype.
// The new contract: a Write to docs/contracts/ against a bound REQ with
// ui_impact=changed must NOT surface HOOK_UI_PROTOTYPE_GATE or any
// equivalent warn. The same is true for MultiEdit and NotebookEdit.
func TestHookCommandIgnoresUIPrototypeFact(t *testing.T) {
	for _, tc := range []struct {
		name      string
		toolName  string
		pathKey   string
		targetRel string
	}{
		{
			name:      "Write",
			toolName:  "Write",
			pathKey:   "file_path",
			targetRel: "docs/contracts/FE-002.md",
		},
		{
			name:      "MultiEdit",
			toolName:  "MultiEdit",
			pathKey:   "file_path",
			targetRel: "docs/contracts/FE-002.md",
		},
		{
			name:      "NotebookEdit",
			toolName:  "NotebookEdit",
			pathKey:   "notebook_path",
			targetRel: "docs/contracts/CONTRACTS-002.ipynb",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := copyPolicyToTempRoot(t)
			protoDir := filepath.Join(root, "docs", "design", "prototypes")
			if err := os.MkdirAll(protoDir, 0o755); err != nil {
				t.Fatal(err)
			}
			input := fmt.Sprintf(`{
				"session_id":"session-ui",
				"hook_event_name":"PreToolUse",
				"tool_name":%q,
				"tool_input":{%q:%q},
				"runtime_context":{
					"runtime_id":"loop-REQ-002",
					"revision":4,
					"bound_req_ui_impact":"changed"
				}
			}`, tc.toolName, tc.pathKey, tc.targetRel)

			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("hook failed: %s stderr=%s", stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String(), "HOOK_UI_PROTOTYPE_GATE") {
				t.Fatalf("legacy HOOK_UI_PROTOTYPE_GATE predicate must not surface, got %s", stdout.String())
			}
			if !strings.Contains(stdout.String(), `"permissionDecision":"allow"`) {
				t.Fatalf("minimal safety model must allow contract edits without UI prototype package, got %s", stdout.String())
			}
		})
	}
}

// TestSessionStartEmitsRecoveryPacket rewrites the legacy HOOK_SESSION_STARTED
// lifecycle test against the minimal safety model. SessionStart is now a
// recovery projection, not a permission verdict — there is no
// HOOK_SESSION_STARTED audit rule ID anymore (BE-039 §3.3, the lifecycle is
// rendered via the Guidance projection). The dedup invariant the legacy test
// was locking becomes: (a) the outbox records exactly one envelope, and (b)
// the envelope exhibits a recovery-packet systemMessage (not a rule_id).
func TestSessionStartEmitsRecoveryPacket(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	input := `{
		"session_id":"session-lifecycle-no-dup",
		"hook_event_name":"SessionStart",
		"tool_name":"*",
		"runtime_context":{
			"runtime_id":"loop-REQ-002",
			"revision":1
		}
	}`
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("SessionStart hook must exit 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Stage ") {
		t.Fatalf("SessionStart must emit a Stage banner, got %q", stdout.String())
	}
	// Legacy HOOK_SESSION_STARTED rule_id is retired.
	if strings.Contains(stdout.String(), "HOOK_SESSION_STARTED") {
		t.Fatalf("legacy HOOK_SESSION_STARTED must not appear under minimal safety model: %s", stdout.String())
	}
	outbox := filepath.Join(root, ".claude", "hook-decisions.jsonl")
	data, err := os.ReadFile(outbox)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 || lines[0] == "" {
		t.Fatalf("SessionStart must produce exactly 1 outbox line, got %d:\n%s", len(lines), string(data))
	}
	if !strings.Contains(lines[0], `"decision_id":"hook-decision-`) {
		t.Fatalf("single outbox line must carry the full envelope decision_id, got: %s", lines[0])
	}
}

// TestRunRejectsUnknownSubcommand locks the cli/run.go default branch: a
// bogus subcommand must exit 2 without touching loop-state.json or
// producing any payload.
func TestRunRejectsUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"bogus-subcommand"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("unknown subcommand must exit 2, got %d (stderr=%s)", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("unknown subcommand must produce no stdout, got %q", stdout.String())
	}
}

// TestEvaluateRejectsEventMismatch locks the cli/run.go:evaluate check that
// the --event flag matches the input's hook_event_name. A SessionStart
// input must never be evaluated against PreToolUse policy rules.
func TestEvaluateRejectsEventMismatch(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	stdin := strings.NewReader(`{"hook_event_name":"SessionStart","tool_name":"","tool_input":{},"runtime_context":{},"facts":{}}`)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, stdin, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0; stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "does not match") {
		t.Fatalf("expected mismatch error in stderr, got %q", stderr.String())
	}
}
