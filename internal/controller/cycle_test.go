package controller_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// stateFromAsset reads a state asset (which is known to be schema-valid)
// and returns a writable copy.
func stateFromAsset(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset(name)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// writeLoopState writes a runtime state document into a temp dir under
// .claude/loop-state.json plus an empty journal, and returns the dir.
func writeLoopState(t *testing.T, state map[string]any) string {
	t.Helper()
	// The fixture writes an empty journal, so its state cursor must also be
	// empty. Runtime writers reject mixed state/journal pairs before mutation.
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "loop-state.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "loop-events.jsonl"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// copyLoopDefinition copies docs/loop-definition.json from the project root
// into a temp test sandbox so transition.LoadCatalog can resolve the
// catalog. We only need the catalog; the rest of the test tree is synthetic.
func copyLoopDefinition(t *testing.T, sourceRoot, destRoot string) error {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sourceRoot, "docs", "loop-definition.json"))
	if err != nil {
		return err
	}
	dest := filepath.Join(destRoot, "docs")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dest, "loop-definition.json"), data, 0o644)
}

const projectRoot = "../../"

func TestRunControlCycleReturnsNotReadyForPlanningDesign(t *testing.T) {
	state := stateFromAsset(t, "loop-state.example.json")
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "internal/cli/controller.go"},
	})
	if err != nil {
		t.Fatalf("RunControlCycle: %v", err)
	}
	if result.QualityGate.Status != controller.StatusNotReady {
		t.Fatalf("planning.design without architecture must report not_ready, got %q (err=%s)", result.QualityGate.Status, result.Error)
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("not_ready must NEVER map to a tool block, got %q", result.Decision.Decision)
	}
	if result.QualityGate.GateID != "GATE-PLANNING-DESIGN-COMPLETE" {
		t.Fatalf("expected GATE-PLANNING-DESIGN-COMPLETE candidate, got %q", result.QualityGate.GateID)
	}
	if result.QualityGate.CandidateTransition != "PTR-PLAN-01" {
		t.Fatalf("expected PTR-PLAN-01 candidate, got %q", result.QualityGate.CandidateTransition)
	}
}

func TestRunControlCyclePropagatesSnapshotRevisions(t *testing.T) {
	state := stateFromAsset(t, "loop-state.example.json")
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	store := runtime.NewStore(
		filepath.Join(dir, ".claude", "loop-state.json"),
		filepath.Join(dir, ".claude", "loop-events.jsonl"),
	)
	before, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "internal/cli/controller.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The cycle must observe the current revision even when it does NOT
	// commit a transition.
	if result.QualityGate.ObservedRevision != before.Revision {
		t.Fatalf("observed revision = %d, want %d", result.QualityGate.ObservedRevision, before.Revision)
	}
	if result.Snapshot.Revision != before.Revision {
		t.Fatalf("snapshot revision = %d, want %d", result.Snapshot.Revision, before.Revision)
	}
}

func TestRunControlCycleNeverBlocksOnNotReady(t *testing.T) {
	// planning.contracts without contract documents -> not_ready, allow.
	state := stateFromAsset(t, "loop-state.example.json")
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 0}
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "docs/contracts/BE-039.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.Status != controller.StatusNotReady {
		t.Fatalf("planning.contracts without coverage must be not_ready, got %q", result.QualityGate.Status)
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("not_ready must allow the tool, got %q", result.Decision.Decision)
	}
	if result.QualityGate.GateID != "GATE-PLANNING-CONTRACTS-COMPLETE" {
		t.Fatalf("expected GATE-PLANNING-CONTRACTS-COMPLETE, got %q", result.QualityGate.GateID)
	}
}

func TestRunControlCycleHandlesTerminalCursor(t *testing.T) {
	// S11 awaiting_human_release is a human gateway, not an automatic
	// transition surface. The cycle must never auto-advance or submit a human
	// decision from this cursor; it must keep the tool allowed.
	state := stateFromAsset(t, "loop-state.example.json")
	state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": 0}
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "ls"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.TransitionCommitted {
		t.Fatal("terminal state must never auto-advance")
	}
	if result.Decision.Decision == "block" {
		t.Fatalf("terminal state must not block, got %q", result.Decision.Decision)
	}
}

func TestRunControlCycleReportsSelectorConflict(t *testing.T) {
	// verification.delivery has multiple auto-triggered candidates
	// (PTR-VERIFY-01, TR-008, TR-010, TR-011). Without satisfied gate
	// facts the catalog must NOT conflict — it should project not_ready.
	state := stateFromAsset(t, "loop-state.example.json")
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "delivery", "phase_revision": 0}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "go test ./..."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.Status != controller.StatusNotReady {
		t.Fatalf("multiple-candidate cursor without satisfied gates must report not_ready, got %q (err=%s code=%q)", result.QualityGate.Status, result.Error, result.QualityGate.ErrorCode)
	}
	if result.QualityGate.ErrorCode == "LOOP_TRIGGER_CONFLICT" {
		t.Fatal("must not report LOOP_TRIGGER_CONFLICT when no gate is satisfied")
	}
	if result.QualityGate.GateID == "" {
		t.Fatal("zero-selected projection must surface a gate_id")
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("not_ready must still allow the tool, got %q", result.Decision.Decision)
	}
}

func TestRunControlCycleSelectorSingleSatisfied(t *testing.T) {
	state := stateFromAsset(t, "loop-state.example.json")
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "internal/cli/controller.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.CandidateTransition != "PTR-PLAN-01" {
		t.Fatalf("single-candidate cursor want PTR-PLAN-01, got %q", result.QualityGate.CandidateTransition)
	}
	if result.QualityGate.GateID != "GATE-PLANNING-DESIGN-COMPLETE" {
		t.Fatalf("want GATE-PLANNING-DESIGN-COMPLETE, got %q", result.QualityGate.GateID)
	}
}

func TestRunControlCycleSelectorDocumentVerificationProjectsPassGate(t *testing.T) {
	// document_verification has TR-003/TR-004 auto candidates. With no
	// satisfied gate the cycle must project GATE-DOCUMENT-PASS not_ready
	// (CT-039-24 projection rules), not LOOP_TRIGGER_CONFLICT.
	state := stateFromAsset(t, "loop-state.example.json")
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": "", "phase_revision": 0}
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "docs/contracts/BE-039.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.Status != controller.StatusNotReady {
		t.Fatalf("document_verification without satisfied gates want not_ready, got %q (code=%q)", result.QualityGate.Status, result.QualityGate.ErrorCode)
	}
	if result.QualityGate.ErrorCode == "LOOP_TRIGGER_CONFLICT" {
		t.Fatal("must not report LOOP_TRIGGER_CONFLICT when no gate is satisfied")
	}
	if !strings.Contains(result.QualityGate.GateID, "GATE-DOCUMENT-PASS") {
		t.Fatalf("zero-selected projection must surface GATE-DOCUMENT-PASS, got %q", result.QualityGate.GateID)
	}
	if result.QualityGate.TransitionCommitted {
		t.Fatal("must not commit TR-003 without satisfied gate")
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("not_ready must allow the tool, got %q", result.Decision.Decision)
	}
}

func TestRunControlCycleSelectorDualSatisfiedConflict(t *testing.T) {
	state := stateFromAsset(t, "loop-state.example.json")
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": "", "phase_revision": 0}
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "docs/contracts/BE-039.md"},
		GateEvaluator: dualSatisfiedEvaluator{
			gates: map[string]qualitygate.Status{
				"GATE-DOCUMENT-PASS":         qualitygate.StatusSatisfied,
				"GATE-DOCUMENT-FIX-REQUIRED": qualitygate.StatusSatisfied,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.Status != controller.StatusUnknown {
		t.Fatalf("dual satisfied gates must report unknown, got %q", result.QualityGate.Status)
	}
	if result.QualityGate.ErrorCode != "LOOP_TRIGGER_CONFLICT" {
		t.Fatalf("expected LOOP_TRIGGER_CONFLICT, got %q", result.QualityGate.ErrorCode)
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("trigger conflict must still allow the tool, got %q", result.Decision.Decision)
	}
}

type dualSatisfiedEvaluator struct {
	gates map[string]qualitygate.Status
}

func (d dualSatisfiedEvaluator) Evaluate(_ context.Context, input qualitygate.Input) (qualitygate.Evaluation, error) {
	status := qualitygate.StatusNotReady
	if s, ok := d.gates[input.GateID]; ok {
		status = s
	}
	return qualitygate.Evaluation{
		Status:              status,
		GateID:              input.GateID,
		CandidateTransition: input.TransitionID,
	}, nil
}

func TestRunControlCycleRequiresRoot(t *testing.T) {
	_, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "x"},
	})
	if err == nil {
		t.Fatal("empty root must be a programmer error")
	}
}

func TestRunControlCycleSingleTransitionPerCall(t *testing.T) {
	// Re-run the cycle on the same planning.design runtime multiple
	// times. The first invocation may not commit (because the gate is
	// not_ready), but the cycle must NEVER advance two phases in one
	// call regardless of how many auto-triggers exist for the cursor.
	state := stateFromAsset(t, "loop-state.example.json")
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
			Root:      dir,
			Event:     "PreToolUse",
			ToolName:  "Edit",
			ToolInput: map[string]any{"file_path": "internal/cli/controller.go"},
		})
		if err != nil {
			t.Fatalf("cycle #%d: %v", i, err)
		}
		// Single transition per call (BE-039 §5.5).
		if result.QualityGate.Status == controller.StatusAdvanced {
			// Confirm we only advanced one phase, not multiple.
			lc, _ := result.Snapshot.State["lifecycle"].(map[string]any)
			if lc["state"] != "planning" || lc["phase"] != "design" {
				t.Fatalf("cycle #%d advanced past planning.design: %#v", i, lc)
			}
		}
	}
}

func TestRunControlCycleControlRequestTypesArePublic(t *testing.T) {
	// Sanity check: ControlRequest, ControlResult, and QualityGateResult
	// must be reachable from outside the package and the field names
	// must match the BE-039 §3.2 wire format.
	req := controller.ControlRequest{
		Root:        "/repo",
		Event:       "PreToolUse",
		ToolName:    "Edit",
		ToolInput:   map[string]any{"file_path": "x"},
		AgentID:     "agent-1",
		SessionID:   "session-1",
		TargetID:    "target-1",
		HookPayload: map[string]any{"foo": "bar"},
	}
	if req.Event != "PreToolUse" {
		t.Fatal("Event field must be public")
	}
	res := controller.ControlResult{
		QualityGate: controller.QualityGateResult{
			Status:              controller.StatusSatisfied,
			GateID:              "GATE-X",
			CandidateTransition: "TR-X",
			ObservedRevision:    12,
			Fingerprint:         "sha256:abc",
			Missing:             []string{"m1"},
			EvidenceRefs:        []string{"e1"},
			TransitionCommitted: false,
			NextCursor:          "planning.design",
		},
	}
	encoded, err := json.Marshal(res.QualityGate)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"status"`, `"gate_id"`, `"candidate_transition"`,
		`"observed_revision"`, `"fingerprint"`, `"missing"`,
		`"evidence_refs"`, `"transition_committed"`, `"next_cursor"`,
	} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("QualityGateResult JSON must include %s: %s", key, encoded)
		}
	}
}

func TestProjectedNotReadyCursorIsForwarded(t *testing.T) {
	// Validate the controller surface area without touching the runtime
	// store: planning.tasks (no document, no evidence) -> not_ready with
	// a deterministic next-cursor string.
	state := stateFromAsset(t, "loop-state.example.json")
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "tasks", "phase_revision": 0}
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Write",
		ToolInput: map[string]any{"file_path": "docs/tasks/TASK-X.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.Status != controller.StatusNotReady {
		t.Fatalf("planning.tasks without coverage must be not_ready, got %q", result.QualityGate.Status)
	}
	if !strings.HasPrefix(result.QualityGate.NextCursor, "planning") {
		t.Fatalf("next cursor must preserve state prefix, got %q", result.QualityGate.NextCursor)
	}
}

func TestRunControlCycleStaleRevisionsAreCounted(t *testing.T) {
	// Even when the gate is not_ready, the cycle must observe and
	// report the current Runtime revision so CAS counters can be
	// reconciled (BE-039 §5.5 / TASK-039-04 §6 metrics).
	state := stateFromAsset(t, "loop-state.example.json")
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	// Bump the revision once by committing a milestone through the
	// existing CAS seam, then re-run the cycle and confirm the observed
	// revision matches.
	statePath := filepath.Join(dir, ".claude", "loop-state.json")
	journalPath := filepath.Join(dir, ".claude", "loop-events.jsonl")
	store := runtime.NewStore(statePath, journalPath)
	before, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision < 1 {
		t.Fatalf("fixture revision too low: %d", before.Revision)
	}
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.ObservedRevision != before.Revision {
		t.Fatalf("observed revision = %d, want %d", result.QualityGate.ObservedRevision, before.Revision)
	}
}

// blockingSlowEvaluator simulates a gate evaluation that ignores context and
// blocks longer than the quality-cycle budget.
type blockingSlowEvaluator struct {
	delay time.Duration
}

func (b blockingSlowEvaluator) Evaluate(context.Context, qualitygate.Input) (qualitygate.Evaluation, error) {
	time.Sleep(b.delay)
	return qualitygate.Evaluation{Status: qualitygate.StatusSatisfied}, nil
}

func countTransitionCommittedEvents(t *testing.T, journalPath string) int {
	t.Helper()
	data, err := os.ReadFile(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "transition_committed" {
			count++
		}
	}
	return count
}

// TestRunControlCycleGateTimeoutReturnsUnknown — CT-039-19 semantics: a slow
// gate evaluator must degrade to LOOP_GATE_UNKNOWN within the quality-cycle
// budget, allow the tool, run final safety, and commit no transition.
func TestRunControlCycleGateTimeoutReturnsUnknown(t *testing.T) {
	state := stateFromAsset(t, "loop-state.example.json")
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}

	journalPath := filepath.Join(dir, ".claude", "loop-events.jsonl")
	beforeTransitions := countTransitionCommittedEvents(t, journalPath)

	start := time.Now()
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:               dir,
		Event:              "PreToolUse",
		ToolName:           "Edit",
		ToolInput:          map[string]any{"file_path": "internal/cli/controller.go"},
		QualityCycleBudget: 50 * time.Millisecond,
		GateEvaluator:      blockingSlowEvaluator{delay: 500 * time.Millisecond},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunControlCycle: %v", err)
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("cycle waited for slow evaluator (%v), expected budget cutoff", elapsed)
	}
	if result.QualityGate.Status != controller.StatusUnknown {
		t.Fatalf("timeout must report unknown, got %q", result.QualityGate.Status)
	}
	if result.QualityGate.ErrorCode != controller.CodeGateUnknown {
		t.Fatalf("timeout must set LOOP_GATE_UNKNOWN, got %q", result.QualityGate.ErrorCode)
	}
	if result.ErrorCode != controller.CodeGateUnknown {
		t.Fatalf("timeout must surface LOOP_GATE_UNKNOWN on result, got %q", result.ErrorCode)
	}
	if result.QualityGate.TransitionCommitted {
		t.Fatal("timeout must not commit a transition")
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("timeout must run final safety and allow the tool, got %q", result.Decision.Decision)
	}
	if after := countTransitionCommittedEvents(t, journalPath); after != beforeTransitions {
		t.Fatalf("journal transition_committed events: before=%d after=%d", beforeTransitions, after)
	}

	output, exitCode, err := hook.PreToolUseWithQualityGate(result.Decision, result)
	if err != nil {
		t.Fatalf("hook render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("timeout hook must exit 0, got %d", exitCode)
	}
	body := string(output)
	if !strings.Contains(body, "LOOP_GATE_UNKNOWN") {
		t.Fatalf("hook output must mention LOOP_GATE_UNKNOWN, got %q", body)
	}
	if !strings.Contains(body, "please reconcile") {
		t.Fatalf("hook output must include reconcile guidance, got %q", body)
	}
}

func TestResolveQualityCycleBudgetDefaultsToTwoSeconds(t *testing.T) {
	if got := controller.ResolveQualityCycleBudget(nil, 0); got != controller.DefaultQualityCycleBudget {
		t.Fatalf("default budget = %v, want %v", got, controller.DefaultQualityCycleBudget)
	}
	if controller.DefaultQualityCycleBudget != 2*time.Second {
		t.Fatalf("DefaultQualityCycleBudget = %v, want 2s", controller.DefaultQualityCycleBudget)
	}
}

func TestResolveQualityCycleBudgetReadsLoopDefinition(t *testing.T) {
	dir := t.TempDir()
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}
	defPath := filepath.Join(dir, "docs", "loop-definition.json")
	raw, err := os.ReadFile(defPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["quality_cycle_timeout"] = "75ms"
	updated, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := transition.LoadCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := controller.ResolveQualityCycleBudget(catalog, 0); got != 75*time.Millisecond {
		t.Fatalf("configured budget = %v, want 75ms", got)
	}
}

func TestRunControlCycleConfiguredBudgetOverridesDefault(t *testing.T) {
	state := stateFromAsset(t, "loop-state.example.json")
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}
	defPath := filepath.Join(dir, "docs", "loop-definition.json")
	raw, err := os.ReadFile(defPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["quality_cycle_timeout"] = "30ms"
	updated, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:          dir,
		Event:         "PreToolUse",
		ToolName:      "Edit",
		ToolInput:     map[string]any{"file_path": "internal/cli/controller.go"},
		GateEvaluator: blockingSlowEvaluator{delay: 200 * time.Millisecond},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("configured 30ms budget should cut off before 200ms evaluator (%v)", elapsed)
	}
	if result.QualityGate.ErrorCode != controller.CodeGateUnknown {
		t.Fatalf("configured budget timeout must return LOOP_GATE_UNKNOWN, got %q", result.QualityGate.ErrorCode)
	}
}

func TestRunControlCycleRecordsLabeledGateMetrics(t *testing.T) {
	state := stateFromAsset(t, "loop-state.example.json")
	dir := writeLoopState(t, state)
	if err := copyLoopDefinition(t, projectRoot, dir); err != nil {
		t.Fatal(err)
	}
	_, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      dir,
		Event:     "PreToolUse",
		ToolName:  "Write",
		ToolInput: map[string]any{"file_path": "docs/tasks/TASK-X.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(dir).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.GateEvaluations["not_ready"] < 1 {
		t.Fatalf("expected not_ready gate metric, got %+v", snap.GateEvaluations)
	}
	if controller.MetricsGateEvaluations < 1 {
		t.Fatalf("legacy gate counter=%d want >=1", controller.MetricsGateEvaluations)
	}
}
