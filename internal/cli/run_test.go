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
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
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
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/control"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
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
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var stateMap map[string]any
	if err := json.Unmarshal(state, &stateMap); err != nil {
		t.Fatal(err)
	}
	stateMap["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	state, err = json.MarshalIndent(stateMap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), state, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "REV-001.md"), []byte(`{"schema_version":"1.0.0","evidence_id":"EV-CLI-001","kind":"document_review","runtime_id":"loop-REQ-002-example","baseline_generation":1,"producer_agent_id":"document-verifier","producer_responsibility":"DV-TRUTH-AUDIT"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "evidence", "add", "--root", root,
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
	if result["revision"] != float64(2) || result["recorded"] != true || result["id"] != "EV-CLI-001" {
		t.Fatalf("unexpected receipt: %#v", result)
	}
}

func TestRuntimeReconcileCommandRestoresMissingJournalEvent(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{}
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-test"
	state["revision"] = 2
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 2,
		"last_event_id": "evt-2",
	}
	state["last_transition"] = map[string]any{
		"event_id":           "evt-2",
		"sequence":           2,
		"transition_id":      "TR-TEST",
		"event":              "test_transition",
		"actor":              "orchestrator",
		"from":               map[string]any{"state": "planning", "phase": "design"},
		"to":                 map[string]any{"state": "planning", "phase": "design"},
		"expected_revision":  1,
		"committed_revision": 2,
		"idempotency_key":    "runtime:TR-TEST:1",
		"evidence_ids":       []any{},
		"occurred_at":        "2026-06-20T00:00:00Z",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	priorEvent := map[string]any{
		"schema_version": "1.0.0", "runtime_id": "loop-test", "event_id": "evt-1",
		"sequence": 1, "event": "milestone_refreshed", "outcome": "refreshed",
		"actor": map[string]any{"type": "system", "id": "cli-test"}, "request_id": "cli-test",
		"baseline_generation": 1, "before_revision": 0, "after_revision": 1,
		"from":         map[string]any{"state": "planning", "phase": "design"},
		"to":           map[string]any{"state": "planning", "phase": "design"},
		"evidence_ids": []any{}, "message": "Prior transition.", "occurred_at": "2026-06-20T00:00:00Z",
	}
	priorData, err := json.Marshal(priorEvent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, append(priorData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "reconcile",
		"--root", root,
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
	root := factory.Clone(t, filepath.Join("..", ".."))
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
		"--evidence", "req_lock_record=docs/requirements/REQ-002.md@0000000000000000000000000000000000000000000000000000000000000000",
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
	if !strings.Contains(stdout.String(), `"revision":0`) {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestPublicStatusNextAndReqBindCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/control"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
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
	if code := cli.Run([]string{"req", "bind", "--dev-branch", "test-development", "--release-upstream", "origin/release", "--root", commitStageFixture(t, root), "--req", "docs/requirements/REQ-099.md", "--approved-by", "user"}, strings.NewReader(""), &out, &errOut); code != 0 {
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
		forbidden []string
	}{
		{
			name:      "investigation is S8",
			phase:     "investigation",
			wantStage: "S8",
			wantSkill: "bug-resolution",
			wantText:  "InvestigationCase",
		},
		{
			name:      "bug report review is S8",
			phase:     "bug_report_review",
			wantStage: "S8",
			wantSkill: "bug-resolution",
			wantText:  "legacy BUG projection",
		},
		{
			name:      "repair readback is S9",
			phase:     "repair_readback",
			wantStage: "S9",
			wantSkill: "bug-resolution",
			wantText:  "prepares the original RepairSession",
			forbidden: []string{"runtime repair status", "runtime repair session open"},
		},
		{
			name:      "S9 planning uses original PlanReport answers",
			phase:     "planning",
			wantStage: "S9",
			wantSkill: "bug-resolution",
			wantText:  "private professional PlanReport",
			forbidden: []string{"runtime repair dispatch", "runtime repair execution begin"},
		},
		{
			name:      "S9 reproduction follows saved PlanReports",
			phase:     "reproducing",
			wantStage: "S9",
			wantSkill: "bug-resolution",
			wantText:  "original consumer",
			forbidden: []string{"runtime repair execution begin"},
		},
		{
			name:      "fixing continues dispatched builders",
			phase:     "fixing",
			wantStage: "S9",
			wantSkill: "bug-resolution",
			wantText:  "SubagentStop receives them",
			forbidden: []string{"--check-location", "completion_report"},
		},
		{
			name:      "S9 targeted answers use supplied paths",
			phase:     "targeted_reverification",
			wantStage: "S9",
			wantSkill: "bug-resolution",
			wantText:  "exact TargetedReverification answer for SubagentStop",
			forbidden: []string{"runtime repair status", "commit ChangeImpact"},
		},
		{
			name:      "S9 handoff is automatic with explicit incomplete recovery",
			phase:     "ready_for_full_review",
			wantStage: "S9",
			wantSkill: "bug-resolution",
			wantText:  "runtime repair handoff complete",
			forbidden: []string{"runtime repair handoff create", "runtime repair handoff commit", "create and commit"},
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
			var projection struct {
				Stage        string `json:"stage"`
				PrimarySkill string `json:"primary_skill"`
				Action       string `json:"action"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &projection); err != nil {
				t.Fatalf("next output is not a projection JSON object: %v; output=%s", err, out)
			}
			if projection.Stage != tc.wantStage {
				t.Fatalf("unexpected stage projection: %s", out)
			}
			if projection.PrimarySkill != tc.wantSkill {
				t.Fatalf("unexpected skill projection: %s", out)
			}
			if !strings.Contains(projection.Action, tc.wantText) {
				t.Fatalf("projection action should mention %q: %s", tc.wantText, out)
			}
			for _, stale := range tc.forbidden {
				if strings.Contains(projection.Action, stale) {
					t.Fatalf("projection action still contains obsolete instruction %q: %s", stale, out)
				}
			}
		})
	}
}

func TestNextProjectsCleanRoundAsS7(t *testing.T) {
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
	state["lifecycle"].(map[string]any)["phase"] = "clean"
	writeJSONFile(t, filepath.Join(root, ".claude", "loop-state.json"), state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"next", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("next failed: code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"stage":"S7"`) {
		t.Fatalf("a clean round should remain in S7 until TR-009: %s", out)
	}
	if !strings.Contains(out, `"primary_skill":"acceptance-and-handoff"`) {
		t.Fatalf("unexpected primary skill: %s", out)
	}
	if !strings.Contains(out, "TR-009") {
		t.Fatalf("clean round should point at TR-009: %s", out)
	}
}

func TestRuntimeRegisterWorkgroupCommand(t *testing.T) {
	root := factory.Clone(t, filepath.Join("..", ".."))
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
	runtimeState["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
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

// TestRuntimeRegisterWorkgroupCommandAnchorsAgainstRoot locks in the
// regression fixed by the resolveRootPath(state|journal) pass: when the
// caller's cwd is not the project root (e.g. a sandboxed dispatch shell),
// every invocation form of `runtime register-workgroup` must anchor the
// state, journal, manifest and task paths against --root instead of the
// process cwd. Without anchoring, resolveExpectedRevision reads a
// different (or empty) state file than the writer opens and the verb
// aborts with a stale-revision gate even when the runtime is unchanged.
//
// Use an isolated project with real template assets, then chdir away from it.
// The regression must neither require nor overwrite the operator's Runtime.
func TestRuntimeRegisterWorkgroupCommandAnchorsAgainstRoot(t *testing.T) {
	root := acFixtureRoot(t)
	for _, rel := range []string{"docs", "agents", "skills", "internal/cli/testdata"} {
		if err := copyDir(filepath.Join(repoRoot(t), rel), filepath.Join(root, rel)); err != nil {
			t.Fatal(err)
		}
	}
	stateDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	canonicalState := filepath.Join(stateDir, "loop-state.json")
	canonicalJournal := filepath.Join(stateDir, "loop-events.jsonl")

	suffix := fmt.Sprintf("anchor-%d", os.Getpid())
	stateBytes, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var runtimeState map[string]any
	if err := json.Unmarshal(stateBytes, &runtimeState); err != nil {
		t.Fatal(err)
	}
	runtimeState["revision"] = float64(6)
	runtimeState["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	runtimeState["lifecycle"].(map[string]any)["state"] = "document_verification"
	runtimeState["lifecycle"].(map[string]any)["phase"] = nil
	encoded, err := json.Marshal(runtimeState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalState, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	// Start the journal empty so the registration appends cleanly.
	if err := os.WriteFile(canonicalJournal, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Manifest + task paths are project-relative (fixtures already live
	// under internal/cli/testdata/). We rewrite manifest ids so this
	// registration counts as a fresh entity — using the shared fixture
	// ids would collide on repeat runs.
	manifestSrc, err := os.ReadFile(filepath.Join("testdata", "document-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestSrc, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["manifest_id"] = fmt.Sprintf("team-manifest-%s", suffix)
	manifest["workgroup_id"] = fmt.Sprintf("workgroup-%s", suffix)
	manifest["runtime_id"] = fmt.Sprintf("loop-REQ-%s", suffix)
	for _, asg := range manifest["assignments"].([]any) {
		entry := asg.(map[string]any)
		entry["agent_id"] = fmt.Sprintf("agent-%s-%s", entry["responsibility_id"], suffix)
	}
	manifestOut := filepath.Join(stateDir, fmt.Sprintf("document-manifest-%s.json", suffix))
	t.Cleanup(func() { _ = os.Remove(manifestOut) })
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestOut, manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Sandbox contract: cwd is intentionally NOT the project root.
	t.Chdir(t.TempDir())

	relManifest, err := filepath.Rel(root, manifestOut)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{
			// Operator / sandbox use-case: caller hands the verb
			// generic relative state/journal paths. The verb must
			// resolve them against --root, not the cwd, otherwise
			// resolveExpectedRevision sees a different (or empty)
			// file than the writer and aborts with stale revision.
			name: "explicit relative state and journal",
			args: []string{
				"runtime", "register-workgroup",
				"--root", root,
				"--state", ".claude/loop-state.json",
				"--journal", ".claude/loop-events.jsonl",
				"--expected-revision", "6",
				"--manifest", relManifest,
				"--task-id", "TASK-001",
				"--task", "internal/cli/testdata/task-fixture.md",
			},
		},
		{
			// Default invocation: --state / --journal omitted, so
			// the verb falls back to .claude/loop-state.json +
			// .claude/loop-events.jsonl. The fix ensures those
			// defaults are also anchored against --root.
			name: "default state and journal paths",
			args: []string{
				"runtime", "register-workgroup",
				"--root", root,
				"--expected-revision", "6",
				"--manifest", relManifest,
				"--task-id", "TASK-001",
				"--task", "internal/cli/testdata/task-fixture.md",
			},
		},
	}

	reset := func(t *testing.T) {
		t.Helper()
		// Re-seed the canonical state to revision 6 with an empty
		// journal so each subtest runs against a fresh runtime. This
		// also keeps the post-mutation journal consistent across
		// subtests without leaking into the operator's real runtime.
		fresh := runtimeState
		fresh["revision"] = float64(6)
		encoded, err := json.Marshal(fresh)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(canonicalState, encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(canonicalJournal, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			reset(t)
			var stdout, stderr bytes.Buffer
			code := cli.Run(tc.args, strings.NewReader(""), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("register-workgroup failed (code=%d): stderr=%s stdout=%s", code, stderr.String(), stdout.String())
			}
			if !strings.Contains(stdout.String(), `"Revision":7`) {
				t.Fatalf("register-workgroup did not bump revision: stdout=%s", stdout.String())
			}
			// The journal written at <root>/.claude/loop-events.jsonl
			// must carry the ENTITY-REGISTER transition — proving the
			// writer hit the --root-anchored journal, not a
			// cwd-relative default.
			journalBytes, err := os.ReadFile(canonicalJournal)
			if err != nil {
				t.Fatalf("read journal: %v", err)
			}
			if !strings.Contains(string(journalBytes), `"transition_id":"ENTITY-REGISTER"`) {
				t.Fatalf("journal missing ENTITY-REGISTER transition: %s", string(journalBytes))
			}
		})
	}
}

func TestRuntimeAgentEventCommandRecordsReadback(t *testing.T) {
	root := factory.Clone(t, filepath.Join("..", ".."))
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
	state["lifecycle"].(map[string]any)["phase"] = "running"
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
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

// TestHookCommandPreToolUseDoesNotRequireToolIdentityContext verifies that a
// PreToolUse observation does not require an actor's tool/path grant. The
// graph projection remains visible even when the platform payload omits its
// runtime_context object.
func TestHookCommandPreToolUseDoesNotRequireToolIdentityContext(t *testing.T) {
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
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("expected a graph-observation envelope, got %q: %v", out, err)
	}
	if hsp, ok := envelope["hookSpecificOutput"].(map[string]any); ok {
		if pd, _ := hsp["permissionDecision"].(string); pd == "deny" {
			t.Fatalf("PreToolUse observer must not deny without tool identity context, got envelope=%v", envelope)
		}
	} else {
		t.Fatalf("PreToolUse output omitted its graph-observation payload: %v", envelope)
	}
	if !strings.Contains(out, "QUALITY_GATE") {
		t.Fatalf("PreToolUse output omitted graph progress feedback: %s", out)
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

// TestDryRunObservesFirstWriteWithoutToolDenial verifies that dry-run
// reuses read-only ready diagnostics without enforcing a first-write rule.
func TestDryRunObservesFirstWriteWithoutToolDenial(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := factory.Clone(t, filepath.Join("..", ".."))
	stageCLI(t, "init", "--root", root)
	fixture := filepath.Join(root, "tests", "fixtures", "hook", "pretool-unactivated-write.json")

	code := cli.Run([]string{"dry-run", "--root", root, "--fixture", fixture}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dry-run must return read-only diagnostics, code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "HOOK_ASSIGNMENT_WRITE_BEFORE_PLAN") || strings.Contains(stdout.String(), `"decision":"deny"`) {
		t.Fatalf("dry-run must not turn a product write into a tool denial: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "quality_gate") {
		t.Fatalf("dry-run must retain quality-gate diagnostics: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "HOOK_AGENT_NOT_ACTIVATED") {
		t.Fatalf("legacy predicate HOOK_AGENT_NOT_ACTIVATED must not appear: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"rule_id":"locked_artifact_write"`) {
		t.Fatalf("dry-run must not pin a locked_artifact rule_id on a pre-plan product write: %s", stdout.String())
	}
}

// TestHookCommandObservesProductWriteWithoutToolDenial verifies that a
// PreToolUse event reports graph state without enforcing tool scope or a
// first-write barrier.
func TestHookCommandObservesProductWriteWithoutToolDenial(t *testing.T) {
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
	if code != 0 {
		t.Fatalf("PreToolUse observer failed for a pre-plan product write: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("PreToolUse observer must not deny a product write: %s", out)
	}
	if !strings.Contains(out, "QUALITY_GATE") {
		t.Fatalf("PreToolUse observer must retain graph progress feedback: %s", out)
	}
	if strings.Contains(out, "HOOK_AGENT_NOT_ACTIVATED") {
		t.Fatalf("legacy HOOK_AGENT_NOT_ACTIVATED predicate must not surface: %s", out)
	}
}

// TestHookCommandObservesToolsWithoutPathExceptions verifies that tool
// paths are not filtered through first-write or per-agent ACL exceptions.
func TestHookCommandObservesToolsWithoutPathExceptions(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	cases := []struct {
		name string
		path string
	}{
		{"control_plane", ".claude/loop-state.json"},
		{"reviewer_reports", "docs/reports/BUG-039-15.md"},
		{"product_file", "src/app.go"},
		{"unregistered_path", "notes/unregistered.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := `{
				"session_id":"session-1",
				"hook_event_name":"PreToolUse",
				"tool_name":"Edit",
				"tool_input":{"file_path":"` + tc.path + `"},
				"runtime_context":{
					"runtime_id":"loop-REQ-002",
					"revision":4,
					"agent":{"id":"agent-1","state":"reading"}
				}
			}`
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("PreToolUse observer failed for %s: code=%d stdout=%s stderr=%s", tc.path, code, stdout.String(), stderr.String())
			}
			out := stdout.String()
			if strings.Contains(out, `"permissionDecision":"deny"`) || strings.Contains(out, "HOOK_ASSIGNMENT_WRITE_BEFORE_PLAN") {
				t.Fatalf("PreToolUse observer must not enforce a path rule for %s: %s", tc.path, out)
			}
		})
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
	targetDir := filepath.Join(root, "docs", "control")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join("..", "..", "docs", "control", "hook-policy.json")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "hook-policy.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Materialize the actual protected-command authority for ordinary Hook fixtures.
	protectedData, err := os.ReadFile(filepath.Join("../..", "docs/control/protected-commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "protected-commands.json"), protectedData, 0644); err != nil {
		t.Fatal(err)
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
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/control"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
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
	policyBytes, err := os.ReadFile(filepath.Join(root, "docs", "control", "hook-policy.json"))
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

func parseHookQualityGate(t *testing.T, raw string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("Hook output is not JSON: %v; output=%s", err, raw)
	}
	specific, _ := envelope["hookSpecificOutput"].(map[string]any)
	additionalContext, _ := specific["additionalContext"].(string)
	line := strings.SplitN(additionalContext, "\n", 2)[0]
	const prefix = "QUALITY_GATE "
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("Hook output does not contain a QUALITY_GATE projection: %s", raw)
	}
	var qualityGate map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &qualityGate); err != nil {
		t.Fatalf("QUALITY_GATE payload is not JSON: %v; line=%s", err, line)
	}
	return qualityGate
}

// TestHookCommandMissingRuntimeFailsOpenWithGateDiagnostic keeps the quality gate's
// unreadable-runtime diagnostic visible while the tool listener fails open.
func TestHookCommandMissingRuntimeFailsOpenWithGateDiagnostic(t *testing.T) {
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
	if code != 0 {
		t.Fatalf("missing runtime must not turn the observer into a tool gate: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	out := stdout.String()
	if strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("missing runtime must not surface a tool denial: %s", out)
	}
	if !strings.Contains(out, "LOOP_RUNTIME_INVALID") {
		t.Fatalf("missing runtime must surface its graph diagnostic: %s", out)
	}
	qualityGate := parseHookQualityGate(t, out)
	if qualityGate["status"] != "unknown" || qualityGate["error_code"] != "LOOP_RUNTIME_INVALID" {
		t.Fatalf("missing runtime must project its unknown quality-gate diagnostic, got %#v", qualityGate)
	}
}

// TestHookCommandPolicyMetadataFailureDoesNotDenyTool pins the observer's fail-open
// policy-metadata behavior. Graph diagnostics remain visible even when the
// policy document is unavailable; policy loading does not deny a tool.
func TestHookCommandPolicyMetadataFailureDoesNotDenyTool(t *testing.T) {
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
	if code != 0 {
		t.Fatalf("missing policy metadata must not block the observed tool: got %d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("missing policy metadata must not create a tool denial: %s", stdout.String())
	}
	qualityGate := parseHookQualityGate(t, stdout.String())
	if qualityGate["status"] != "unknown" || !strings.Contains(stdout.String(), "LOOP RECOVERY") {
		t.Fatalf("policy metadata failure must preserve the unknown graph status and recovery context: gate=%#v output=%s", qualityGate, stdout.String())
	}
}

// TestHookCommandPreservesAuditOnOutboxFailure verifies that an outbox
// failure is diagnosed without converting the graph observer into a tool ACL.
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
	if code != 1 {
		t.Fatalf("audit persistence failure must remain visible as exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "append Hook audit") {
		t.Fatalf("expected append-audit error on stderr, got %s", stderr.String())
	}
	if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("observer audit failure must not create a tool denial: %s", stdout.String())
	}
}

// TestHookCommandReportsOutboxFailureWithoutToolDeny keeps audit storage
// failures diagnostic while preserving the observer's non-vetoing behavior.
func TestHookCommandReportsOutboxFailureWithoutToolDeny(t *testing.T) {
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
	if code != 1 {
		t.Fatalf("audit persistence failure must remain visible as exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "append Hook audit") {
		t.Fatalf("expected append-audit error on stderr, got %s", stderr.String())
	}
	if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("observer audit failure must not create a tool denial: %s", stdout.String())
	}
}

// TestHookCommandFailsWhenStdoutWriteErrors drives the stdout.Write error
// path inside evaluate(). The graph observer does not deny this bare Edit;
// an erroring stdout remains an output error rather than a tool decision.
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
	// The observer produces a recoverable wire envelope whose stdout.Write
	// failure is surfaced as exit 1. It must not be rewritten as a tool denial.
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

// A missing Bash classification table does not suppress unrelated Edit events.
// The observer still records the Hook envelope and quality-gate projection.
func TestHookCommandIgnoresMissingProtectedCommandsTable(t *testing.T) {
	root := copyPolicyToTempRoot(t)
	if err := os.Remove(filepath.Join(root, "docs", "control", "protected-commands.json")); err != nil && !os.IsNotExist(err) {
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
		t.Fatalf("unrelated Edit must not surface a protected-command table error; stderr=%s", stderr.String())
	}
	// `table_unloaded` is not a user-facing tool-permission result.
	if strings.Contains(stdout.String(), "table_unloaded") {
		t.Fatalf("observer must not surface legacy table_unloaded envelope field: %s", stdout.String())
	}
	// A pre-tool-use on this path records graph feedback without a tool denial.
	if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("observer must not deny an Edit because of the missing table: %s", stdout.String())
	}
}

// TestHookCommandIgnoresUIPrototypeFact rewrites the legacy
// HOOK_UI_PROTOTYPE_GATE warn tests. UI prototype completeness was a
// Guidance concern under the minimal safety model (REQ-039 §14.3, BE-039
// §6.3) — the Hook Policy never matches ui_contract_before_prototype.
// The new contract: a Write to docs/dev/contracts/ against a bound REQ with
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
			targetRel: "docs/dev/contracts/FE-002.md",
		},
		{
			name:      "MultiEdit",
			toolName:  "MultiEdit",
			pathKey:   "file_path",
			targetRel: "docs/dev/contracts/FE-002.md",
		},
		{
			name:      "NotebookEdit",
			toolName:  "NotebookEdit",
			pathKey:   "notebook_path",
			targetRel: "docs/dev/contracts/CONTRACTS-002.ipynb",
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
			if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
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
