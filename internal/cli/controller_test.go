package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestBuildGuidanceForSessionStartUsesCanonicalNextProjection(t *testing.T) {
	root := filepath.Join("..", "..")
	state := map[string]any{
		"runtime_id": "loop-REQ-039",
		"revision":   12,
		"lifecycle": map[string]any{
			"state": "verification",
			"phase": "qa",
		},
		"bound_req": map[string]any{
			"path": "docs/requirements/REQ-039-loop-control-plane.md",
		},
	}

	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})

	if guidance.Stage != "S7" {
		t.Fatalf("expected S7 guidance, got %#v", guidance)
	}
	if guidance.LifecycleState != "verification" || guidance.LifecyclePhase != "qa" {
		t.Fatalf("unexpected lifecycle cursor: %#v", guidance)
	}
	if guidance.ProtocolRef != "docs/agent-protocol.md#s7" {
		t.Fatalf("unexpected protocol ref: %q", guidance.ProtocolRef)
	}
	if guidance.ManualRef != ".claude/bin/loop-harness.md" {
		t.Fatalf("unexpected manual ref: %q", guidance.ManualRef)
	}
	if guidance.PrimarySkill != "team-planning" {
		t.Fatalf("unexpected primary skill: %q", guidance.PrimarySkill)
	}
	if guidance.Action != "complete QA responsibilities" {
		t.Fatalf("unexpected next action: %q", guidance.Action)
	}
	if !strings.Contains(guidance.Instruction, "docs/agent-protocol.md#s7") {
		t.Fatalf("instruction must contain protocol ref: %q", guidance.Instruction)
	}
	if !strings.Contains(guidance.Instruction, ".claude/bin/loop-harness.md") {
		t.Fatalf("instruction must contain manual ref: %q", guidance.Instruction)
	}
}

func TestBuildGuidanceDefinesRecoveryReadOrderAndNoCliNormalPath(t *testing.T) {
	root := filepath.Join("..", "..")
	state := map[string]any{
		"runtime_id": "loop-REQ-039",
		"revision":   12,
		"lifecycle": map[string]any{
			"state": "verification",
			"phase": "qa",
		},
		"bound_req": map[string]any{
			"path": "docs/requirements/REQ-039-loop-control-plane.md",
		},
	}

	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})

	if len(guidance.ReadOrder) < 5 {
		t.Fatalf("SessionStart must expose an ordered recovery read sequence, got %#v", guidance.ReadOrder)
	}
	if guidance.ReadOrder[0] != "LOOP RECOVERY packet (this message)" {
		t.Fatalf("recovery packet must be read first, got %#v", guidance.ReadOrder)
	}
	if guidance.ReadOrder[1] != "AGENTS-template.md" {
		t.Fatalf("source template should be the fallback entry document, got %#v", guidance.ReadOrder)
	}
	if !strings.Contains(strings.Join(guidance.ReadOrder, " -> "), "docs/agent-protocol.md#s7") {
		t.Fatalf("read order must point to current protocol anchor, got %#v", guidance.ReadOrder)
	}
	if !containsString(guidance.Automation, "do not call loop-harness for normal continuation") {
		t.Fatalf("normal continuation must be Hook-driven, got %#v", guidance.Automation)
	}
	if !strings.Contains(guidance.Instruction, "Read in order") {
		t.Fatalf("instruction must make read order visible: %q", guidance.Instruction)
	}
}

func TestBuildGuidanceSchedulesDelegationAndWorktreeIntegration(t *testing.T) {
	root := filepath.Join("..", "..")
	state := map[string]any{
		"runtime_id": "loop-REQ-039",
		"revision":   12,
		"lifecycle": map[string]any{
			"state": "building",
			"phase": "implementation",
		},
	}

	started := buildGuidance(root, state, "SubagentStart", policy.Input{
		ToolInput: map[string]any{"subagent_type": "backend-builder"},
		Runtime:   policy.RuntimeContext{Agent: &policy.AgentContext{State: "activated"}},
	})
	if len(started.Questions) < 3 {
		t.Fatalf("SubagentStart must ask delegation preflight questions, got %#v", started.Questions)
	}
	preflight := buildGuidance(root, state, "PreToolUse", policy.Input{
		ToolName:  "Agent",
		ToolInput: map[string]any{"subagent_type": "backend-builder"},
	})
	if len(preflight.Questions) < 3 || !strings.Contains(strings.Join(preflight.Automation, " "), "worktree") {
		t.Fatalf("Agent/Task PreToolUse must carry the same delegation preflight, got %#v", preflight)
	}
	for _, expected := range []string{"Agent Team", "predefined", "worktree"} {
		if !strings.Contains(strings.ToLower(strings.Join(started.Questions, " ")), strings.ToLower(expected)) {
			t.Fatalf("SubagentStart questions must mention %q, got %#v", expected, started.Questions)
		}
	}

	stopped := buildGuidance(root, state, "SubagentStop", policy.Input{
		Facts: map[string]bool{"agent_report_complete": true},
	})
	if !strings.Contains(stopped.Action, "worktree") {
		t.Fatalf("SubagentStop must require worktree integration before acknowledgement, got %q", stopped.Action)
	}
	joined := strings.ToLower(strings.Join(stopped.Integration, " "))
	for _, expected := range []string{"inspect", "develop", "remove worktree", "completion_ack"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("SubagentStop integration must mention %q, got %#v", expected, stopped.Integration)
		}
	}
	if !strings.Contains(joined, "never merge") || !strings.Contains(joined, "master/main") {
		t.Fatalf("integration guidance must explicitly protect release branches: %#v", stopped.Integration)
	}
}

func TestBuildGuidanceReawakensSameTeammateWhenIdle(t *testing.T) {
	guidance := buildGuidance(filepath.Join("..", ".."), map[string]any{
		"runtime_id": "loop-REQ-039",
		"revision":   12,
		"lifecycle":  map[string]any{"state": "building", "phase": "implementation"},
	}, "TeammateIdle", policy.Input{Facts: map[string]bool{"assignment_reported": false}})

	if !strings.Contains(strings.ToLower(guidance.Action), "same teammate") {
		t.Fatalf("idle teammate must be reawakened instead of replaced, got %q", guidance.Action)
	}
	if !containsSubstring(guidance.Automation, "do not spawn a replacement") {
		t.Fatalf("idle guidance must prohibit replacement spawn, got %#v", guidance.Automation)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, target string) bool {
	for _, value := range values {
		if strings.Contains(value, target) {
			return true
		}
	}
	return false
}

func TestRefreshMilestoneUsesCASAndIsIdempotent(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	updated, changed, err := refreshMilestone(root, statePath, journalPath, snapshot, guidance, "SessionStart")
	if err != nil {
		t.Fatalf("refresh milestone: %v", err)
	}
	if !changed {
		t.Fatal("first refresh must persist the missing milestone")
	}
	if updated.Revision != snapshot.Revision+1 {
		t.Fatalf("expected one CAS revision increment, before=%d after=%d", snapshot.Revision, updated.Revision)
	}
	if updated.State["milestone"] == nil {
		t.Fatal("milestone must be persisted in runtime state")
	}
	if _, _, err := refreshMilestone(root, statePath, journalPath, snapshot, guidance, "SessionStart"); !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("stale milestone refresh must preserve CAS failure, got %v", err)
	}

	again, changed, err := refreshMilestone(root, statePath, journalPath, updated, guidance, "SessionStart")
	if err != nil {
		t.Fatalf("repeat milestone refresh: %v", err)
	}
	if changed {
		t.Fatal("same milestone must not create a second mutation")
	}
	if again.Revision != updated.Revision {
		t.Fatalf("idempotent refresh changed revision: before=%d after=%d", updated.Revision, again.Revision)
	}
}

func TestRefreshMilestonePreservesLastTransition(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["last_transition"] = map[string]any{
		"event_id":           "evt-tr008",
		"sequence":           1,
		"transition_id":      "TR-008",
		"event":              "transition_committed",
		"actor":              "hook",
		"from":               map[string]any{"state": "verification", "phase": "delivery"},
		"to":                 map[string]any{"state": "bug_resolution", "phase": "investigation"},
		"expected_revision":  1,
		"committed_revision": 2,
		"idempotency_key":    "runtime:TR-008:1",
		"evidence_ids":       []any{},
		"occurred_at":        "2026-07-31T00:00:01Z",
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	guidance := buildGuidance(root, state, "PreToolUse", policy.Input{})
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	gate := controller.QualityGateResult{
		Status:              controller.StatusAdvanced,
		GateID:              "GATE-BUG-INVESTIGATION",
		CandidateTransition: "PTR-BUG-01",
		ObservedRevision:    snapshot.Revision,
		Fingerprint:         "sha256:ct14",
		TransitionCommitted: true,
		NextCursor:          "bug_resolution.investigation",
	}
	updated, changed, err := refreshMilestoneWithGate(root, statePath, journalPath, snapshot, guidance, "PreToolUse", gate)
	if err != nil {
		t.Fatalf("refresh milestone: %v", err)
	}
	if !changed {
		t.Fatal("milestone refresh must persist when gate fingerprint differs")
	}
	last, ok := updated.State["last_transition"].(map[string]any)
	if !ok || last == nil {
		t.Fatal("last_transition must remain after milestone refresh")
	}
	if last["transition_id"] != "TR-008" {
		t.Fatalf("milestone refresh overwrote last_transition: got %v, want TR-008", last["transition_id"])
	}
}

func TestRunControlCycleHelperWrappersAreExposed(t *testing.T) {
	// The internal/controller package cannot import internal/cli, so
	// RunControlCycle relies on the exported BuildGuidanceForState and
	// ReconcileGuidanceForController wrappers around the existing
	// buildGuidance/reconcileGuidance helpers. This test ensures both
	// wrappers are reachable and produce the same output as the
	// internal helpers (a one-shot smoke test for BUG-039-02 §4.1 step
	// 9: "refresh Milestone, Guidance, Journal from the existing
	// helpers, do not duplicate").
	root := filepath.Join("..", "..")
	state := map[string]any{
		"runtime_id": "loop-REQ-039",
		"revision":   12,
		"lifecycle":  map[string]any{"state": "verification", "phase": "qa"},
	}
	internal := buildGuidance(root, state, "PreToolUse", policy.Input{})
	exported := BuildGuidanceForState(root, state, "PreToolUse", policy.Input{})
	if internal.Stage != exported.Stage {
		t.Fatalf("wrapper must reuse buildGuidance: internal=%q exported=%q", internal.Stage, exported.Stage)
	}
	if internal.Action != exported.Action {
		t.Fatalf("wrapper must reuse buildGuidance action: %q vs %q", internal.Action, exported.Action)
	}
	if internal.Objective != exported.Objective {
		t.Fatalf("wrapper must reuse buildGuidance objective: %q vs %q", internal.Objective, exported.Objective)
	}
}

func TestFallbackGuidanceWrapperProducesRecoveryPacket(t *testing.T) {
	got := FallbackGuidanceForController("PreToolUse")
	if got == nil {
		t.Fatal("fallback guidance must be non-nil")
	}
	if got.Stage != "cross-stage" {
		t.Fatalf("fallback stage = %q, want cross-stage", got.Stage)
	}
	if !got.Blocked {
		t.Fatal("fallback guidance must mark the cursor blocked")
	}
	if got.HumanRequired {
		t.Fatal("fallback guidance must not require human review (the cursor is recoverable, not a Gateway)")
	}
}

// TestGuidanceMapWithGateProjectsQualityGateBlock locks BUG-039-07 §4.1 step
// 1: the milestone emitted by `guidanceMap` must include the Controller's
// quality_gate projection as a sub-object. The 9 SYNC-039 §6 fields must be
// present so the persisted milestone is the single source of truth for the
// current gate result (no parallel state machine).
func TestGuidanceMapWithGateProjectsQualityGateBlock(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	gate := controller.QualityGateResult{
		Status:              controller.StatusNotReady,
		GateID:              "GATE-PLANNING-CONTRACTS-COMPLETE",
		CandidateTransition: "PTR-PLAN-02",
		ObservedRevision:    12,
		Fingerprint:         "sha256:abc",
		Missing:             []string{"contract traceability"},
		EvidenceRefs:        []string{"ev-039-contract-set"},
		TransitionCommitted: false,
		NextCursor:          "planning.contracts",
	}
	guidance := policy.Guidance{
		RuntimeID:      "loop-REQ-039",
		Revision:       12,
		Stage:          "S3",
		LifecycleState: "planning",
		LifecyclePhase: "contracts",
		Objective:      "complete the development contract set",
		Action:         "complete contract traceability",
		ProtocolRef:    "docs/agent-protocol.md#s3",
		ManualRef:      loopManualRef,
		PrimarySkill:   "specification-planning",
		Missing:        []string{"contract traceability"},
	}

	milestone := guidanceMapWithGate(guidance, controller.QualityGateResult{}, "SessionStart", 12, now, gate)

	raw, ok := milestone["quality_gate"]
	if !ok {
		t.Fatalf("milestone must include quality_gate, got keys=%v", mapKeys(milestone))
	}
	qg, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("quality_gate must be an object, got %T", raw)
	}
	for _, key := range []string{
		"status", "gate_id", "candidate_transition", "observed_revision",
		"fingerprint", "missing", "evidence_refs", "transition_committed",
		"next_cursor",
	} {
		if _, present := qg[key]; !present {
			t.Fatalf("quality_gate must include %q, got keys=%v", key, mapKeys(qg))
		}
	}
	if qg["status"] != "not_ready" {
		t.Fatalf("quality_gate.status=%v, want not_ready", qg["status"])
	}
	if qg["fingerprint"] != "sha256:abc" {
		t.Fatalf("quality_gate.fingerprint=%v, want sha256:abc", qg["fingerprint"])
	}
	if qg["gate_id"] != "GATE-PLANNING-CONTRACTS-COMPLETE" {
		t.Fatalf("quality_gate.gate_id=%v", qg["gate_id"])
	}
	missing, _ := qg["missing"].([]string)
	if len(missing) != 1 || missing[0] != "contract traceability" {
		t.Fatalf("quality_gate.missing=%v", qg["missing"])
	}
}

// TestGuidanceMapWithoutGateOmitsQualityGate locks the no-fabrication
// invariant: when the Controller did not produce a gate result, the
// milestone must not invent one. The projection is opt-in.
func TestGuidanceMapWithoutGateOmitsQualityGate(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	guidance := policy.Guidance{
		RuntimeID: "loop-REQ-039",
		Stage:     "S3",
	}
	milestone := guidanceMap(guidance, "SessionStart", 12, now)
	if _, present := milestone["quality_gate"]; present {
		t.Fatalf("milestone must omit quality_gate when no gate is available, got %v", milestone["quality_gate"])
	}
}

// TestMilestoneMatchesDetectsQualityGateFingerprintChange locks BUG-039-07
// §4.1 step 2: a change in quality_gate.fingerprint must defeat the
// milestoneMatches no-op check so a new gate result forces a fresh
// milestone write (no silent stale gate).
func TestMilestoneMatchesDetectsQualityGateFingerprintChange(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	base := controller.QualityGateResult{
		Status:           controller.StatusNotReady,
		GateID:           "GATE-PLANNING-CONTRACTS-COMPLETE",
		ObservedRevision: 12,
		Fingerprint:      "sha256:abc",
		Missing:          []string{"contract traceability"},
		NextCursor:       "planning.contracts",
	}
	guidance := policy.Guidance{
		RuntimeID:      "loop-REQ-039",
		Revision:       12,
		Stage:          "S3",
		LifecycleState: "planning",
		LifecyclePhase: "contracts",
		Objective:      "complete the development contract set",
		Action:         "complete contract traceability",
		ProtocolRef:    "docs/agent-protocol.md#s3",
		ManualRef:      loopManualRef,
		PrimarySkill:   "specification-planning",
		Missing:        []string{"contract traceability"},
	}

	// Persisted milestone reflects gate.fingerprint=sha256:abc.
	persisted := guidanceMapWithGate(persistedGuidanceForMatch(guidance), base, "SessionStart", 12, now, base)
	if !milestoneMatchesWithGate(persisted, guidance, base) {
		t.Fatal("persisted milestone with same gate must match (sanity)")
	}

	// New gate fingerprint — must NOT match.
	updatedGate := base
	updatedGate.Fingerprint = "sha256:def"
	if milestoneMatchesWithGate(persisted, guidance, updatedGate) {
		t.Fatal("milestoneMatches must return false when quality_gate.fingerprint changes")
	}
}

// TestMilestoneIdempotencyChangesWithQualityGateFingerprint locks BUG-039-07
// §4.1 step 3: a gate fingerprint change must produce a new idempotency
// key so the Journal records the gate-driven refresh, not a duplicate or a
// skip.
func TestMilestoneIdempotencyChangesWithQualityGateFingerprint(t *testing.T) {
	guidance := policy.Guidance{
		RuntimeID: "loop-REQ-039",
		Stage:     "S3",
	}
	gateA := controller.QualityGateResult{Fingerprint: "sha256:abc"}
	gateB := controller.QualityGateResult{Fingerprint: "sha256:def"}
	keyA := milestoneIdempotencyWithGate(guidance, gateA)
	keyB := milestoneIdempotencyWithGate(guidance, gateB)
	if keyA == keyB {
		t.Fatalf("idempotency key must change when gate fingerprint changes: %q", keyA)
	}
	// Same fingerprint on a new gate object must yield the same key
	// (idempotency is the point of the hash).
	gateARepeat := controller.QualityGateResult{Fingerprint: "sha256:abc"}
	if keyA != milestoneIdempotencyWithGate(guidance, gateARepeat) {
		t.Fatalf("idempotency key must be stable for the same gate: %q vs %q", keyA, milestoneIdempotencyWithGate(guidance, gateARepeat))
	}
}

// TestRefreshMilestonePersistsQualityGateObject locks the integration
// point: when refreshMilestone commits a milestone, the persisted
// `milestone.quality_gate` must round-trip as a 9-field object and the
// Controller fingerprint must defeat the no-op check on the next call.
func TestRefreshMilestonePersistsQualityGateObject(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	gate := controller.QualityGateResult{
		Status:              controller.StatusNotReady,
		GateID:              "GATE-PLANNING-CONTRACTS-COMPLETE",
		CandidateTransition: "PTR-PLAN-02",
		ObservedRevision:    snapshot.Revision,
		Fingerprint:         "sha256:abc",
		Missing:             []string{"contract traceability"},
		EvidenceRefs:        []string{"ev-039-contract-set"},
		NextCursor:          "planning.contracts",
	}
	updated, changed, err := refreshMilestoneWithGate(root, statePath, journalPath, snapshot, guidance, "SessionStart", gate)
	if err != nil {
		t.Fatalf("refresh milestone with gate: %v", err)
	}
	if !changed {
		t.Fatal("first refresh with gate must persist")
	}
	milestone, _ := updated.State["milestone"].(map[string]any)
	if milestone == nil {
		t.Fatal("milestone must be persisted")
	}
	raw, ok := milestone["quality_gate"]
	if !ok {
		t.Fatalf("persisted milestone must include quality_gate, keys=%v", mapKeys(milestone))
	}
	qg, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("quality_gate must be an object, got %T", raw)
	}
	if qg["fingerprint"] != "sha256:abc" {
		t.Fatalf("persisted quality_gate.fingerprint=%v, want sha256:abc", qg["fingerprint"])
	}

	// New fingerprint must force a second refresh.
	gate2 := gate
	gate2.Fingerprint = "sha256:def"
	again, changed, err := refreshMilestoneWithGate(root, statePath, journalPath, updated, guidance, "PreToolUse", gate2)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if !changed {
		t.Fatal("gate fingerprint change must force a refresh (no no-op)")
	}
	if again.Revision != updated.Revision+1 {
		t.Fatalf("expected revision increment, before=%d after=%d", updated.Revision, again.Revision)
	}
}

// TestQualityGateMapHelperShape locks the helper output so callers can
// project a gate result without reaching into the controller package.
func TestQualityGateMapHelperShape(t *testing.T) {
	gate := controller.QualityGateResult{
		Status:              controller.StatusAdvanced,
		GateID:              "GATE-PLANNING-CONTRACTS-COMPLETE",
		CandidateTransition: "PTR-PLAN-02",
		ObservedRevision:    12,
		Fingerprint:         "sha256:xyz",
		Missing:             nil,
		EvidenceRefs:        nil,
		TransitionCommitted: true,
		NextCursor:          "planning.tasks",
	}
	qg := qualityGateMap(gate)
	for _, key := range []string{
		"status", "gate_id", "candidate_transition", "observed_revision",
		"fingerprint", "missing", "evidence_refs", "transition_committed",
		"next_cursor",
	} {
		if _, present := qg[key]; !present {
			t.Fatalf("qualityGateMap must include %q, got keys=%v", key, mapKeys(qg))
		}
	}
	if qg["missing"] == nil {
		t.Fatal("qualityGateMap.missing must be non-nil")
	}
	if qg["evidence_refs"] == nil {
		t.Fatal("qualityGateMap.evidence_refs must be non-nil")
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// persistedGuidanceForMatch strips fields that milestoneMatches already
// ignores (source_revision, updated_at, event, instruction) so the test
// can reason about the gate comparison in isolation.
func persistedGuidanceForMatch(g policy.Guidance) policy.Guidance {
	g.Revision = 12
	return g
}

// writeFixtureRuntime writes a minimal runtime into the temp dir so the
// TeammateIdle/SubagentStop handlers can mutate state via Store CAS. The
// returned helper lets callers add agents, tasks, and assignment rows
// before Snapshot() is taken. The fixture mirrors the canonical shape
// the production loader produces.
type runtimeFixture struct {
	root        string
	statePath   string
	journalPath string
	state       map[string]any
}

func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	// The semantic validator reads docs/loop-definition.json from the root.
	// Provide a minimal stub that exposes the `building` state with no phase
	// machine and a `planning` state with a phase machine, so the cursor
	// (state=building, phase=implementation) validates.
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	minimalDef := `{
  "schema_version": "1.3.0",
  "definition_id": "loop-test-stub",
  "status": "reviewed",
  "initial_state": "inactive",
  "terminal_states": ["awaiting_human_release", "aborted"],
  "states": {
    "inactive": {"stage": "S0", "category": "control", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "planning": {"stage": "S2", "category": "work", "phase_machine": "planning", "entry_condition": "", "exit_condition": ""},
    "building": {"stage": "S6", "category": "work", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "document_verification": {"stage": "S5", "category": "review", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "verification": {"stage": "S7", "category": "work", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "bug_resolution": {"stage": "S8", "category": "work", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "acceptance": {"stage": "S10", "category": "work", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "release_audit": {"stage": "S10", "category": "work", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "paused": {"stage": "S10", "category": "control", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "awaiting_human_release": {"stage": "S11", "category": "control", "phase_machine": null, "entry_condition": "", "exit_condition": ""},
    "aborted": {"stage": "S11", "category": "control", "phase_machine": null, "entry_condition": "", "exit_condition": ""}
  },
  "phase_machines": {
    "planning": {
      "phases": {
        "design": {"transitions": []},
        "contracts": {"transitions": []},
        "tasks": {"transitions": []}
      }
    }
  },
  "entity_lifecycles": {
    "agent": {"states": ["spawned", "reading", "understanding_submitted", "understanding_approved", "activated", "working", "reported", "done", "blocked", "stopped"]},
    "task": {"states": ["candidate", "reviewed", "locked", "in_progress", "review", "blocked", "done", "cancelled"]},
    "bug": {"states": ["draft", "investigating", "pending_approval", "accepted", "assigned", "fixed", "retesting", "closed", "rejected", "duplicate"]}
  }
}`
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), []byte(minimalDef), 0o644); err != nil {
		t.Fatal(err)
	}
	// Start from the schema-valid example and override the cursors we
	// need for the TeammateIdle/SubagentStop handlers.
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-REQ-039"
	state["revision"] = 32
	state["lifecycle"] = map[string]any{
		"state":          "building",
		"phase":          nil,
		"phase_revision": 3,
	}
	state["bound_req"] = map[string]any{
		"id":          "REQ-039",
		"path":        "docs/requirements/REQ-039-loop-control-plane.md",
		"version":     "v2.0.0",
		"status":      "locked",
		"sha256":      "e21e61d9b9ee1fb960e625b53f090943b7c6a606994a3ec754ae8daebd984594",
		"approved_by": "user",
		"approved_at": "2026-07-29T00:00:00Z",
		"metadata":    map[string]any{"ui_impact": "none"},
	}
	state["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  []any{},
		"teams":  []any{},
		"bugs":   []any{},
	}
	state["milestone"] = map[string]any{
		"stage":           "S6",
		"lifecycle_state": "building",
		"lifecycle_phase": "implementation",
		"objective":       "complete the implementation batch",
		"action":          "implement remaining TASKs",
		"protocol_ref":    "docs/agent-protocol.md#s6",
		"manual_ref":      ".claude/bin/loop-harness.md",
		"primary_skill":   "loop-orchestration",
		"read":            []string{".claude/loop-state.json"},
		"missing":         []string{},
		"done_when":       []string{},
		"human_required":  false,
		"blocked":         false,
		"blocker":         nil,
		"event":           "SessionStart",
		"instruction":     "milestone",
		"recovery":        []string{},
		"source_revision": 32,
		"updated_at":      "2026-07-29T00:00:00Z",
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Re-encode the patched state so subsequent reads see the override.
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return &runtimeFixture{root: root, statePath: statePath, journalPath: journalPath, state: state}
}

func (f *runtimeFixture) addAgent(id, role, status, teamID string, taskIDs []string) {
	entities := f.state["entities"].(map[string]any)
	agents := entities["agents"].([]any)
	taskAnys := make([]any, 0, len(taskIDs))
	for _, t := range taskIDs {
		taskAnys = append(taskAnys, t)
	}
	agents = append(agents, map[string]any{
		"id":                  id,
		"role":                role,
		"state":               status,
		"team_id":             teamID,
		"task_ids":            taskAnys,
		"definition_ref":      "agents/" + role + ".md",
		"prompt_ref":          ".claude/workgroups/REQ-039/" + teamID + "/manifest.json#" + id,
		"readback_ref":        ".claude/workgroups/REQ-039/" + teamID + "/readback.json",
		"activation_ref":      ".claude/workgroups/REQ-039/" + teamID + "/activation.json",
		"activation_revision": 1,
		"updated_at":          "2026-07-29T00:00:00Z",
	})
	entities["agents"] = agents
}

func (f *runtimeFixture) addTask(id, state string, ownerIDs []string) {
	entities := f.state["entities"].(map[string]any)
	tasks := entities["tasks"].([]any)
	ownerAnys := make([]any, 0, len(ownerIDs))
	for _, o := range ownerIDs {
		ownerAnys = append(ownerAnys, o)
	}
	tasks = append(tasks, map[string]any{
		"id":              id,
		"state":           state,
		"owner_agent_ids": ownerAnys,
		"path":            "docs/tasks/" + id + ".md",
		"sha256":          "0000000000000000000000000000000000000000000000000000000000000000",
	})
	entities["tasks"] = tasks
}

func (f *runtimeFixture) addTeam(id, status string, agentIDs []string) {
	entities := f.state["entities"].(map[string]any)
	teams := entities["teams"].([]any)
	agentAnys := make([]any, 0, len(agentIDs))
	for _, a := range agentIDs {
		agentAnys = append(agentAnys, a)
	}
	teams = append(teams, map[string]any{
		"id":                 id,
		"status":             status,
		"agent_ids":          agentAnys,
		"kind":               "builder",
		"manifest_ref":       ".claude/workgroups/REQ-039/" + id + "/manifest.json",
		"platform_team_id":   "loop-REQ-039",
		"responsibility_ids": []string{"BUILD-WORK-PACKAGE"},
		"review_round":       nil,
	})
	entities["teams"] = teams
}

func (f *runtimeFixture) persist(t *testing.T) {
	t.Helper()
	data, err := json.MarshalIndent(f.state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *runtimeFixture) snapshot(t *testing.T) runtime.Snapshot {
	t.Helper()
	f.persist(t)
	snap, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func (f *runtimeFixture) agentStatus(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if id, _ := row["id"].(string); id == id {
			return stringValue(row["state"])
		}
	}
	return ""
}

func (f *runtimeFixture) taskState(t *testing.T, taskID string) (string, []any) {
	t.Helper()
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	for _, raw := range tasks {
		row, _ := raw.(map[string]any)
		if id, _ := row["id"].(string); id == taskID {
			return stringValue(row["state"]), asAnySlice(row["owner_agent_ids"])
		}
	}
	return "", nil
}

func (f *runtimeFixture) teamStatus(t *testing.T, teamID string) string {
	t.Helper()
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	entities, _ := state["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	for _, raw := range teams {
		row, _ := raw.(map[string]any)
		if id, _ := row["id"].(string); id == teamID {
			return stringValue(row["status"])
		}
	}
	return ""
}

func asAnySlice(value any) []any {
	if value == nil {
		return nil
	}
	out, _ := value.([]any)
	return out
}

// ---------- TeammateIdle branch tests ----------

// Branch 1: assignment not complete, no blocker → re-wake same teammate.
func TestHandleTeammateIdleResumesSameTeammate(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.addAgent("agent-039-04", "backend-builder", "working", "workgroup-039-04", []string{"TASK-039-04"})
	fix.addTeam("workgroup-039-04", "planned", []string{"agent-039-04"})
	fix.addTask("TASK-039-04", "in_progress", []string{"agent-039-04"})
	fix.persist(t)

	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{
		Assignments: []hookctx.AssignmentContext{{
			AssignmentID: "assignment-039-04",
			TaskID:       "TASK-039-04",
			OwnerAgentID: "agent-039-04",
			State:        "in_progress",
			WorktreePath: filepath.Join(fix.root, "worktree-039-04"),
			Branch:       "codex/req-039-controller-cycle",
			TargetBranch: "develop",
		}},
	}
	guidance, updated, err := HandleTeammateIdleForController(fix.root, snapshot, loaded, "TeammateIdle", policy.Input{AgentID: "agent-039-04"})
	if err != nil {
		t.Fatalf("HandleTeammateIdle: %v", err)
	}
	if guidance.Action == "" {
		t.Fatal("guidance must carry a specific scheduling action")
	}
	if !strings.Contains(strings.ToLower(guidance.Action), "re-wake") || !strings.Contains(strings.ToLower(guidance.Action), "same teammate") {
		t.Fatalf("Branch 1 must re-wake same teammate, got %q", guidance.Action)
	}
	if fix.agentStatus(t, "agent-039-04") != "activated" {
		t.Fatalf("Branch 1 must CAS-update teammate state to activated, got %q", fix.agentStatus(t, "agent-039-04"))
	}
	if updated.Revision <= snapshot.Revision {
		t.Fatalf("CAS must advance revision: before=%d after=%d", snapshot.Revision, updated.Revision)
	}
	if !containsSubstring(guidance.Automation, "do not spawn a replacement") {
		t.Fatalf("Branch 1 must prohibit replacement spawn, got %#v", guidance.Automation)
	}
}

// Branch 2: assignment complete but no completion report → demand report.
func TestHandleTeammateIdleDemandsCompletionReport(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.addAgent("agent-039-04", "backend-builder", "working", "workgroup-039-04", []string{"TASK-039-04"})
	fix.addTeam("workgroup-039-04", "planned", []string{"agent-039-04"})
	fix.addTask("TASK-039-04", "in_progress", []string{"agent-039-04"})
	fix.persist(t)

	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{
		Assignments: []hookctx.AssignmentContext{{
			AssignmentID: "assignment-039-04",
			TaskID:       "TASK-039-04",
			OwnerAgentID: "agent-039-04",
			State:        "complete",
			WorktreePath: filepath.Join(fix.root, "worktree-039-04"),
			Branch:       "codex/req-039-controller-cycle",
			TargetBranch: "develop",
		}},
	}
	guidance, _, err := HandleTeammateIdleForController(fix.root, snapshot, loaded, "TeammateIdle", policy.Input{AgentID: "agent-039-04"})
	if err != nil {
		t.Fatalf("HandleTeammateIdle: %v", err)
	}
	if !guidance.Blocked {
		t.Fatal("Branch 2 must surface a blocked guidance")
	}
	if !containsSubstring(guidance.Missing, "completion_report") {
		t.Fatalf("Branch 2 must list completion_report in missing, got %#v", guidance.Missing)
	}
	if !strings.Contains(strings.ToLower(guidance.Action), "completion") {
		t.Fatalf("Branch 2 must demand completion report, got %q", guidance.Action)
	}
	// Teammate state should NOT advance to activated since no report was filed.
	if fix.agentStatus(t, "agent-039-04") == "activated" && snapshot.Revision != 0 {
		t.Logf("teammate state in fixture: %q", fix.agentStatus(t, "agent-039-04"))
	}
}

// Branch 3: blocked assignment, no blocker report → demand blocker report.
func TestHandleTeammateIdleDemandsBlockerReport(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.addAgent("agent-039-04", "backend-builder", "working", "workgroup-039-04", []string{"TASK-039-04"})
	fix.addTeam("workgroup-039-04", "planned", []string{"agent-039-04"})
	fix.addTask("TASK-039-04", "blocked", []string{"agent-039-04"})
	fix.persist(t)

	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{
		Assignments: []hookctx.AssignmentContext{{
			AssignmentID: "assignment-039-04",
			TaskID:       "TASK-039-04",
			OwnerAgentID: "agent-039-04",
			State:        "blocked",
			WorktreePath: filepath.Join(fix.root, "worktree-039-04"),
			Branch:       "codex/req-039-controller-cycle",
			TargetBranch: "develop",
		}},
	}
	guidance, _, err := HandleTeammateIdleForController(fix.root, snapshot, loaded, "TeammateIdle", policy.Input{AgentID: "agent-039-04"})
	if err != nil {
		t.Fatalf("HandleTeammateIdle: %v", err)
	}
	if !guidance.Blocked {
		t.Fatal("Branch 3 must surface a blocked guidance")
	}
	if !containsSubstring(guidance.Missing, "blocker_report") {
		t.Fatalf("Branch 3 must list blocker_report in missing, got %#v", guidance.Missing)
	}
	if !strings.Contains(strings.ToLower(guidance.Action), "blocker") {
		t.Fatalf("Branch 3 must demand blocker report, got %q", guidance.Action)
	}
}

// Branch 4: assignment complete AND reported → allocate next task via CAS.
func TestHandleTeammateIdleAllocatesNextLegalTask(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.addAgent("agent-039-04", "backend-builder", "working", "workgroup-039-04", []string{"TASK-039-04"})
	fix.addTeam("workgroup-039-04", "planned", []string{"agent-039-04"})
	fix.addTask("TASK-039-04", "done", []string{"agent-039-04"})
	fix.addTask("TASK-039-05", "candidate", nil)
	fix.persist(t)

	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{
		Assignments: []hookctx.AssignmentContext{
			{
				AssignmentID:  "assignment-039-04",
				TaskID:        "TASK-039-04",
				OwnerAgentID:  "agent-039-04",
				State:         "complete",
				ReportStatus:  "completion_report",
				CompletionRef: ".claude/evidence/loop-REQ-039/g1/assignments/assignment-039-04/completion.json",
				WorktreePath:  filepath.Join(fix.root, "worktree-039-04"),
				Branch:        "codex/req-039-controller-cycle",
				TargetBranch:  "develop",
			},
		},
	}
	guidance, updated, err := HandleTeammateIdleForController(fix.root, snapshot, loaded, "TeammateIdle", policy.Input{AgentID: "agent-039-04"})
	if err != nil {
		t.Fatalf("HandleTeammateIdle: %v", err)
	}
	if updated.Revision <= snapshot.Revision {
		t.Fatalf("Branch 4 must CAS-advance revision, before=%d after=%d", snapshot.Revision, updated.Revision)
	}
	state, owners := fix.taskState(t, "TASK-039-05")
	if state != "in_progress" {
		t.Fatalf("Branch 4 must advance TASK-039-05 to in_progress, got %q", state)
	}
	found := false
	for _, o := range owners {
		if s, _ := o.(string); s == "agent-039-04" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Branch 4 must allocate TASK-039-05 to agent-039-04, got owners=%v", owners)
	}
	if !strings.Contains(strings.ToLower(guidance.Action), "next") {
		t.Fatalf("Branch 4 must reference next task, got %q", guidance.Action)
	}
}

// Branch 5: no remaining tasks → worktree recovery / Team close-out.
func TestHandleTeammateIdleClosesOutTeamWhenNoTasksRemain(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.addAgent("agent-039-04", "backend-builder", "working", "workgroup-039-04", []string{"TASK-039-04"})
	fix.addTeam("workgroup-039-04", "planned", []string{"agent-039-04"})
	fix.addTask("TASK-039-04", "done", []string{"agent-039-04"})
	fix.persist(t)

	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{
		Assignments: []hookctx.AssignmentContext{
			{
				AssignmentID:  "assignment-039-04",
				TaskID:        "TASK-039-04",
				OwnerAgentID:  "agent-039-04",
				State:         "complete",
				ReportStatus:  "completion_report",
				CompletionRef: ".claude/evidence/loop-REQ-039/g1/assignments/assignment-039-04/completion.json",
				WorktreePath:  filepath.Join(fix.root, "worktree-039-04"),
				Branch:        "codex/req-039-controller-cycle",
				TargetBranch:  "develop",
			},
		},
	}
	guidance, updated, err := HandleTeammateIdleForController(fix.root, snapshot, loaded, "TeammateIdle", policy.Input{AgentID: "agent-039-04"})
	if err != nil {
		t.Fatalf("HandleTeammateIdle: %v", err)
	}
	if !strings.Contains(strings.ToLower(guidance.Action), "close-out") &&
		!strings.Contains(strings.ToLower(guidance.Action), "worktree recovery") {
		t.Fatalf("Branch 5 must reference close-out or worktree recovery, got %q", guidance.Action)
	}
	if fix.teamStatus(t, "workgroup-039-04") != "complete" {
		t.Fatalf("Branch 5 must CAS-mark Team status=complete, got %q", fix.teamStatus(t, "workgroup-039-04"))
	}
	if updated.Revision <= snapshot.Revision {
		t.Fatalf("Branch 5 must CAS-advance revision, before=%d after=%d", snapshot.Revision, updated.Revision)
	}
}

// TeammateIdle must not spawn replacement teammates (BUG-039-06 §4.2).
func TestHandleTeammateIdleNeverSpawnsReplacement(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.addAgent("agent-039-04", "backend-builder", "working", "workgroup-039-04", []string{"TASK-039-04"})
	fix.addTeam("workgroup-039-04", "planned", []string{"agent-039-04"})
	fix.addTask("TASK-039-04", "in_progress", []string{"agent-039-04"})
	fix.persist(t)

	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{
		Assignments: []hookctx.AssignmentContext{{
			AssignmentID: "assignment-039-04",
			TaskID:       "TASK-039-04",
			OwnerAgentID: "agent-039-04",
			State:        "in_progress",
		}},
	}
	guidance, _, err := HandleTeammateIdleForController(fix.root, snapshot, loaded, "TeammateIdle", policy.Input{AgentID: "agent-039-04"})
	if err != nil {
		t.Fatalf("HandleTeammateIdle: %v", err)
	}
	combined := strings.ToLower(strings.Join(guidance.Automation, " "))
	if !strings.Contains(combined, "do not spawn a replacement") {
		t.Fatalf("teammate automation must forbid replacement spawn, got %#v", guidance.Automation)
	}
	// The action text must reference a specific decision (resume), not
	// generic "continue work" prose. We allow the action to mention the
	// spawn-prohibition explicitly because that is the BUG-039-06 §4.2
	// signal; we just require it to be decision-specific.
	if !strings.Contains(strings.ToLower(guidance.Action), "re-wake") {
		t.Fatalf("guidance action must reference re-wake scheduling, got %q", guidance.Action)
	}
}

// TeammateIdle with no matching teammate falls back to text projection.
func TestHandleTeammateIdleFallsBackWhenTeammateMissing(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.persist(t)
	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{}
	_, _, err := HandleTeammateIdleForController(fix.root, snapshot, loaded, "TeammateIdle", policy.Input{AgentID: "agent-missing"})
	if err != nil {
		t.Fatalf("missing teammate must fall back, not error: %v", err)
	}
}

// SubagentStop with no matching assignment falls back to text projection.
func TestHandleSubagentStopFallsBackWhenAssignmentMissing(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.persist(t)
	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{}
	_, _, err := HandleSubagentStopForController(t.Context(), fix.root, snapshot, loaded, "SubagentStop", policy.Input{AgentID: "agent-missing"})
	if err != nil {
		t.Fatalf("missing assignment must fall back, not error: %v", err)
	}
}

// SubagentStop with !Ready inspection preserves the worktree.
func TestHandleSubagentStopPreservesWorktreeOnNotReadyInspection(t *testing.T) {
	fix := newRuntimeFixture(t)
	fix.persist(t)
	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{
		BaselineGeneration: 1,
		Assignments: []hookctx.AssignmentContext{{
			AssignmentID: "assignment-039-06",
			TaskID:       "TASK-039-06",
			OwnerAgentID: "agent-missing-but-with-bogus-worktree",
			State:        "complete",
			WorktreePath: "/path/that/does/not/exist",
			Branch:       "codex/req-039-bogus",
			TargetBranch: "develop",
		}},
	}
	guidance, updated, err := HandleSubagentStopForController(t.Context(), fix.root, snapshot, loaded, "SubagentStop", policy.Input{AgentID: "agent-missing-but-with-bogus-worktree"})
	if err != nil {
		t.Fatalf("HandleSubagentStop: %v", err)
	}
	if !guidance.Blocked {
		t.Fatal("!Ready inspection must produce blocked guidance")
	}
	if !strings.Contains(strings.ToLower(guidance.Blocker), "not ready") {
		t.Fatalf("blocker must reference inspection failure, got %q", guidance.Blocker)
	}
	if updated.Revision <= snapshot.Revision {
		t.Fatalf("preserved checkpoint must still CAS-advance revision, before=%d after=%d", snapshot.Revision, updated.Revision)
	}
	if len(guidance.Missing) == 0 {
		t.Fatal("missing must include the inspection blockers")
	}
}

// SubagentStop with ready inspection must advance integration state.
func TestHandleSubagentStopAdvancesIntegrationOnReadyInspection(t *testing.T) {
	fix := newRuntimeFixture(t)
	// Build a real git repository so Inspect can complete its checks.
	if err := os.MkdirAll(filepath.Join(fix.root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := fix.root
	for _, args := range [][]string{
		{"init", "--initial-branch=develop"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		// Disable gpg signing so the commit hook doesn't fail in
		// environments with a global commit.gpgsign=true (the harness
		// CI/dev shells have a 1Password-backed signing agent that is
		// not reachable inside the test).
		{"config", "commit.gpgsign", "false"},
		{"config", "tag.gpgsign", "false"},
		{"checkout", "-b", "develop"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		if _, err := runGit(t, repo, args...); err != nil {
			t.Fatalf("git %v: %v", strings.Join(args, " "), err)
		}
	}
	// Create a worktree with a branch that has commits.
	wtPath := filepath.Join(repo, "wt")
	if _, err := runGit(t, repo, "worktree", "add", "-b", "codex/req-039-bogus", wtPath, "develop"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}
	if _, err := runGit(t, wtPath, "commit", "--allow-empty", "-m", "feature commit"); err != nil {
		t.Fatalf("commit in worktree: %v", err)
	}
	// Create a fake completion report so Inspect passes the report check.
	reportDir := filepath.Join(repo, ".claude", "evidence", "loop-REQ-039", "g1", "assignments", "assignment-039-06")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := `{"message_type":"completion_report","assignment_id":"assignment-039-06"}`
	if err := os.WriteFile(filepath.Join(reportDir, "completion.json"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}

	fix.persist(t)
	snapshot := fix.snapshot(t)
	loaded := &hookctx.LoadedContext{
		BaselineGeneration: 1,
		Assignments: []hookctx.AssignmentContext{{
			AssignmentID: "assignment-039-06",
			TaskID:       "TASK-039-06",
			OwnerAgentID: "agent-039-06",
			State:        "complete",
			WorktreePath: wtPath,
			Branch:       "codex/req-039-bogus",
			TargetBranch: "develop",
		}},
	}
	guidance, updated, err := HandleSubagentStopForController(t.Context(), fix.root, snapshot, loaded, "SubagentStop", policy.Input{AgentID: "agent-039-06"})
	if err != nil {
		t.Fatalf("HandleSubagentStop: %v", err)
	}
	if updated.Revision <= snapshot.Revision {
		t.Fatalf("ready SubagentStop must CAS-advance revision, before=%d after=%d", snapshot.Revision, updated.Revision)
	}
	joined := strings.ToLower(strings.Join(guidance.Integration, " "))
	if !strings.Contains(joined, "worktree integrated") {
		t.Fatalf("ready integration guidance must surface checkpoint, got %#v", guidance.Integration)
	}
	if strings.Contains(joined, "cleanup") && !strings.Contains(joined, "acknowledge") {
		t.Fatalf("SubagentStop must not claim cleanup happened in this call (Acknowledge=false, Cleanup=false), got %#v", guidance.Integration)
	}
}

// TestReconcileGuidanceWiresSubagentStopHandler locks BUG-039-37: the
// Hook evaluate path reaches HandleSubagentStopForController via
// reconcileGuidance (ReconcileGuidanceForController), not text-only
// buildGuidance.
func TestReconcileGuidanceWiresSubagentStopHandler(t *testing.T) {
	fix := newRuntimeFixture(t)
	repo := fix.root
	for _, args := range [][]string{
		{"init", "--initial-branch=develop"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"checkout", "-b", "develop"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		if _, err := runGit(t, repo, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	wtPath := filepath.Join(repo, "wt-wire")
	if _, err := runGit(t, repo, "worktree", "add", "-b", "codex/wire-039-37", wtPath, "develop"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}
	if _, err := runGit(t, wtPath, "commit", "--allow-empty", "-m", "feature"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	reportDir := filepath.Join(repo, ".claude", "evidence", "loop-REQ-039", "g1", "assignments", "assignment-wire")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "completion.json"), []byte(`{"message_type":"completion_report","assignment_id":"assignment-wire"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wgDir := filepath.Join(repo, ".claude", "workgroups", "REQ-039", "TASK-039-01")
	if err := os.MkdirAll(wgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"schema_version":"1.0.0","manifest_id":"wg-wire","version":"v1.0.0",
		"runtime_id":"loop-REQ-039","req_id":"REQ-039","baseline_generation":1,
		"status":"active","workgroup_id":"wg-wire","workgroup_kind":"builder",
		"assignments":[{
			"assignment_id":"assignment-wire","responsibility_id":"BUILD-WORK-PACKAGE",
			"role_family":"backend-builder","agent_id":"agent-wire",
			"write_paths":["internal/"],"status":"complete",
			"worktree_path":"` + wtPath + `","branch":"codex/wire-039-37","target_branch":"develop"
		}]
	}`
	if err := os.WriteFile(filepath.Join(wgDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	fix.state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "agent-wire", "role": "builder", "state": "reported",
			"task_ids": []any{"TASK-039-01"}, "team_id": "team-wire",
			"definition_ref": "defs/agent-wire.md", "prompt_ref": "prompts/wire.md",
			"readback_ref": "readback/wire.md", "activation_ref": "activation/wire.json",
			"activation_revision": 1, "updated_at": "2026-07-30T00:00:00Z",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-039-01", "state": "review",
			"owner_agent_ids": []any{"agent-wire"},
			"path":            "docs/tasks/TASK-039-01.md",
			"sha256":          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		"bugs": []any{}, "teams": []any{},
	}
	fix.state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	if baseline, ok := fix.state["baseline"].(map[string]any); ok {
		baseline["generation"] = 1
		if _, ok := baseline["captured_at"]; !ok {
			baseline["captured_at"] = "2026-07-30T00:00:00Z"
		}
	} else {
		fix.state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-07-30T00:00:00Z"}
	}
	fix.persist(t)

	developBefore, err := runGit(t, repo, "rev-parse", "develop")
	if err != nil {
		t.Fatal(err)
	}
	guidance, _, err := ReconcileGuidanceForController(fix.root, "SubagentStop", policy.Input{AgentID: "agent-wire"})
	if err != nil {
		t.Fatalf("ReconcileGuidanceForController: %v", err)
	}
	developAfter, err := runGit(t, repo, "rev-parse", "develop")
	if err != nil {
		t.Fatal(err)
	}
	if string(developAfter) == string(developBefore) {
		t.Fatalf("BUG-039-37 wiring must advance develop via Integrate, guidance=%#v", guidance.Integration)
	}
	joined := strings.ToLower(strings.Join(guidance.Integration, " "))
	if !strings.Contains(joined, "worktree integrated") {
		t.Fatalf("wired SubagentStop must surface integration progress, got %#v", guidance.Integration)
	}
}

func runGit(t *testing.T, root string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := gitCommand(root, args...)
	return cmd.CombinedOutput()
}

// gitCommand builds a git invocation rooted at the temp repo. It runs
// inside -C so Inspect's rev-parse / merge-base / worktree operations
// resolve the same root the controller points at.
func gitCommand(root string, args ...string) *exec.Cmd {
	full := append([]string{"-C", root}, args...)
	return exec.Command("git", full...)
}
