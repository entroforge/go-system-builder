package hookctx_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

func TestLoaderBuildsPolicyContextFromRuntimeAndActivation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"runtime_id":"loop-REQ-002",
		"revision":5,
		"bound_req":{"path":"docs/requirements/REQ-002.md","metadata":{"ui_impact":"changed"}},
		"hook_control":{"policy_ref":{"path":"docs/hook-policy.json","version":"v1.3.0","sha256":"abc"}},
		"entities":{"agents":[{
			"id":"agent-builder-1",
			"state":"activated",
			"activation_ref":".claude/evidence/activation-1.json"
		}]}
	}`
	activation := `{
		"agent_id":"agent-builder-1",
		"allowed_tools":["Edit","Bash"],
		"allowed_write_paths":["src/"],
		"allowed_command_classes":["test"]
	}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "evidence", "activation-1.json"), []byte(activation), 0o644); err != nil {
		t.Fatal(err)
	}

	context, err := hookctx.Load(root, "agent-builder-1")
	if err != nil {
		t.Fatalf("load context: %v", err)
	}
	if context.RuntimeID != "loop-REQ-002" || context.Revision != 5 {
		t.Fatalf("unexpected runtime context: %#v", context)
	}
	if context.BoundREQPath != "docs/requirements/REQ-002.md" {
		t.Fatalf("unexpected REQ path: %s", context.BoundREQPath)
	}
	if context.BoundREQUIImpact != "changed" {
		t.Fatalf("UI-impact metadata not propagated to runtime context: %s", context.BoundREQUIImpact)
	}
	if context.Agent == nil || context.Agent.State != "activated" {
		t.Fatalf("unexpected Agent context: %#v", context.Agent)
	}
	if len(context.Agent.AllowedWritePaths) != 1 || context.Agent.AllowedWritePaths[0] != "src/" {
		t.Fatalf("activation scope not loaded: %#v", context.Agent)
	}
}

// TestLoaderIgnoresLegacyWarningCounters verifies that hook-control warning
// counters from prior schema versions are silently ignored — Hook v1 has no
// retry/escalation path, so the loader must not surface them.
func TestLoaderIgnoresLegacyWarningCounters(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"runtime_id":"loop-REQ-002",
		"revision":1,
		"hook_control":{"warning_counters":[{"key":"warning-key","attempts":5}]}
	}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	context, err := hookctx.Load(root, "")
	if err != nil {
		t.Fatalf("load context: %v", err)
	}
	if context.RuntimeID != "loop-REQ-002" || context.Revision != 1 {
		t.Fatalf("unexpected runtime context: %#v", context)
	}
}

// TestLoaderErrorsWhenLoopStateMissing covers the load error path at
// loader.go:65 — without .claude/loop-state.json the harness must surface a
// wrapped read error so the policy engine can fail closed.
func TestLoaderErrorsWhenLoopStateMissing(t *testing.T) {
	root := t.TempDir()
	_, err := hookctx.Load(root, "")
	if err == nil {
		t.Fatal("expected error when loop-state.json is missing")
	}
	if !strings.Contains(err.Error(), "read runtime state") {
		t.Fatalf("expected wrapped read-runtime-state error, got %v", err)
	}
}

// TestLoaderErrorsWhenLoopStateMalformed covers the JSON decode error path
// at loader.go:70 — a syntactically invalid loop-state.json must surface a
// wrapped decode error so callers can distinguish read failures from
// validation failures.
func TestLoaderErrorsWhenLoopStateMalformed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hookctx.Load(root, "")
	if err == nil {
		t.Fatal("expected error for malformed loop-state.json")
	}
	if !strings.Contains(err.Error(), "decode runtime state") {
		t.Fatalf("expected wrapped decode-runtime-state error, got %v", err)
	}
}

// TestLoaderErrorsWhenActivationMissing exercises the missing-activation
// branch at loader.go:142 — the loader must surface a wrapped read error so
// the engine can emit HOOK_AGENT_NOT_ACTIVATED instead of a silent allow.
func TestLoaderErrorsWhenActivationMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"runtime_id":"loop-REQ-002",
		"revision":1,
		"entities":{"agents":[{"id":"agent-1","state":"activated","activation_ref":".claude/evidence/missing.json"}]}
	}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hookctx.Load(root, "agent-1")
	if err == nil {
		t.Fatal("expected error when activation_ref points at a missing file")
	}
	if !strings.Contains(err.Error(), "read activation") {
		t.Fatalf("expected wrapped read-activation error, got %v", err)
	}
}

// TestLoaderErrorsWhenAgentIDMismatchesActivation covers the cross-check at
// loader.go:126 — if the activation envelope names a different Agent than
// the request, the loader must surface a mismatch error rather than load a
// wrong scope.
func TestLoaderErrorsWhenAgentIDMismatchesActivation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"runtime_id":"loop-REQ-002",
		"revision":1,
		"entities":{"agents":[{"id":"agent-1","state":"activated","activation_ref":".claude/evidence/activation-1.json"}]}
	}`
	activation := `{"agent_id":"agent-other","allowed_tools":["Edit"],"allowed_write_paths":["src/"],"allowed_command_classes":[]}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "evidence", "activation-1.json"), []byte(activation), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hookctx.Load(root, "agent-1")
	if err == nil {
		t.Fatal("expected error for activation Agent mismatch")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

// TestLoaderErrorsWhenAgentNotFound covers the not-found branch at
// loader.go:134 — when the requested agentID has no entry in the runtime,
// the loader must surface a structured not-found error rather than a silent
// Agent context.
func TestLoaderErrorsWhenAgentNotFound(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"runtime_id":"loop-REQ-002",
		"revision":1,
		"entities":{"agents":[{"id":"agent-other","state":"activated"}]}
	}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hookctx.Load(root, "agent-missing")
	if err == nil {
		t.Fatal("expected error for missing Agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// TestLoaderSurfacesTeamsBugsEvidenceLifecycleAndPause covers the full
// populated surface at loader.go:73-110 — every RuntimeContext field that is
// derived from the loop-state.json must round-trip into the returned
// RuntimeContext for downstream predicates.
func TestLoaderSurfacesTeamsBugsEvidenceLifecycleAndPause(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"runtime_id":"loop-REQ-002",
		"revision":42,
		"lifecycle":{"state":"verification","phase":"delivery"},
		"pause":{"reason":"manual"},
		"review":{"round":3,"clean_round":{"round":2}},
		"entities":{
			"agents":[],
			"bugs":[
				{"id":"BUG-1","state":"fixing","severity":"P0"},
				{"id":"BUG-2","state":"accepted","severity":"P0"},
				{"id":"BUG-3","state":"fixing","severity":"P2"}
			],
			"teams":[
				{"manifest_ref":"internal/cli/testdata/document-manifest.json","responsibility_ids":["verifier","qa"]}
			]
		},
		"evidence":[
			{"status":"valid"},
			{"status":"invalid"},
			{"status":"valid"}
		]
	}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	context, err := hookctx.Load(root, "")
	if err != nil {
		t.Fatalf("load context: %v", err)
	}
	if context.CurrentState != "verification" || context.CurrentPhase != "delivery" {
		t.Fatalf("lifecycle surface not loaded: state=%s phase=%s", context.CurrentState, context.CurrentPhase)
	}
	if !context.Paused {
		t.Fatal("pause surface must surface paused=true")
	}
	if context.CurrentReviewRound != 3 || context.CleanRound == nil {
		t.Fatalf("review surface not loaded: %#v", context)
	}
	if context.EvidenceValidCount != 2 {
		t.Fatalf("evidence valid count: got %d want 2", context.EvidenceValidCount)
	}
	if context.OpenBlockingBugs != 2 {
		t.Fatalf("open blocking bugs: got %d want 2 (BUG-1 + BUG-2)", context.OpenBlockingBugs)
	}
	if len(context.Teams) != 1 || context.Teams[0].ManifestRef == "" {
		t.Fatalf("teams surface not loaded: %#v", context.Teams)
	}
	if len(context.Teams[0].ResponsibilityIDs) != 2 {
		t.Fatalf("team responsibility ids not loaded: %#v", context.Teams[0])
	}
}

// TestLoaderAcceptsAbsoluteActivationRefPath exercises the absolute-path
// branch at loader.go:139 — when activation_ref is an absolute path the
// loader must read it as-is rather than joining it under the runtime root.
func TestLoaderAcceptsAbsoluteActivationRefPath(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := `{
		"runtime_id":"loop-REQ-002",
		"revision":1,
		"entities":{"agents":[{"id":"agent-1","state":"activated","activation_ref":"__ABS__"}]}
	}`
	activation := `{"agent_id":"agent-1","allowed_tools":["Edit"],"allowed_write_paths":["src/"],"allowed_command_classes":[]}`
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(strings.ReplaceAll(state, "__ABS__", filepath.Join(external, "abs-activation.json"))), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "abs-activation.json"), []byte(activation), 0o644); err != nil {
		t.Fatal(err)
	}
	context, err := hookctx.Load(root, "agent-1")
	if err != nil {
		t.Fatalf("load context: %v", err)
	}
	if context.Agent == nil || len(context.Agent.AllowedWritePaths) != 1 || context.Agent.AllowedWritePaths[0] != "src/" {
		t.Fatalf("absolute-path activation not loaded: %#v", context.Agent)
	}
}

// TestLoaderOpenBlockingBugsAcceptsCanonicalP0AndRejectsLegacyBlocking is
// the regression test for the schema/code drift: the runtime schema for BUG
// severity is `{P0, P1, P2, P3}` (review-evidence.schema.json §canonicalBug),
// and `actions.go recordFindingBatch` defaults new findings to `P0`. The
// prior loader check `severity == "blocking"` could never match a real
// runtime because no schema-conformant BUG can carry the `"blocking"`
// literal. P0 is the canonical "blocking" severity.
func TestLoaderOpenBlockingBugsAcceptsCanonicalP0AndRejectsLegacyBlocking(t *testing.T) {
	cases := []struct {
		name     string
		bugs     string
		wantOpen int
	}{
		{
			name:     "P0 + incomplete state counts",
			bugs:     `[{"id":"BUG-1","state":"fixing","severity":"P0"}]`,
			wantOpen: 1,
		},
		{
			name:     "P1 does not count as blocking",
			bugs:     `[{"id":"BUG-1","state":"fixing","severity":"P1"}]`,
			wantOpen: 0,
		},
		{
			name:     "P2 in fixing does not count",
			bugs:     `[{"id":"BUG-1","state":"fixing","severity":"P2"}]`,
			wantOpen: 0,
		},
		{
			name:     "P3 in fixing does not count",
			bugs:     `[{"id":"BUG-1","state":"fixing","severity":"P3"}]`,
			wantOpen: 0,
		},
		{
			name:     "legacy 'blocking' literal is rejected by schema and loader",
			bugs:     `[{"id":"BUG-1","state":"fixing","severity":"blocking"}]`,
			wantOpen: 0,
		},
		{
			name:     "P0 in closed state does not count",
			bugs:     `[{"id":"BUG-1","state":"closed","severity":"P0"}]`,
			wantOpen: 0,
		},
		{
			name:     "mix of P0 and P1 only P0 counts",
			bugs:     `[{"id":"BUG-1","state":"fixing","severity":"P0"},{"id":"BUG-2","state":"accepted","severity":"P1"},{"id":"BUG-3","state":"investigating","severity":"P0"}]`,
			wantOpen: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			state := `{"runtime_id":"loop-REQ-002","revision":1,"entities":{"agents":[],"bugs":` + tc.bugs + `,"teams":[]}}`
			if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), []byte(state), 0o644); err != nil {
				t.Fatal(err)
			}
			context, err := hookctx.Load(root, "")
			if err != nil {
				t.Fatalf("load context: %v", err)
			}
			if context.OpenBlockingBugs != tc.wantOpen {
				t.Fatalf("OpenBlockingBugs: got %d want %d", context.OpenBlockingBugs, tc.wantOpen)
			}
		})
	}
}
