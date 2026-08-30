package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// s7RecoveryFixtureState builds an S7 cannot_clean/draining intermediate
// control plane (L3-S7 §8): a registered ReviewPlan in round 2 with one
// consumed, one running, one blocked and one queued Assignment, plus two
// required Claims still waiting for a disposition.
func s7RecoveryFixtureState(phase, planStatus string) map[string]any {
	sha := strings.Repeat("a", 64)
	claim := func(lens, applicability, disposition, assignmentID string, resultID any, findingIDs []any) map[string]any {
		return map[string]any{
			"lens": lens, "applicability": applicability, "disposition": disposition,
			"assignment_id": assignmentID, "result_id": resultID, "finding_ids": findingIDs,
		}
	}
	assignment := func(lens, status string, claimIDs []any, agentID, resultRef any) map[string]any {
		return map[string]any{
			"lens": lens, "claim_ids": claimIDs, "status": status,
			"agent_id": agentID, "result_ref": resultRef,
		}
	}
	return map[string]any{
		"runtime_id": "loop-REQ-TEST",
		"revision":   20,
		"lifecycle":  map[string]any{"state": "verification", "phase": phase, "phase_revision": 3},
		"review": map[string]any{
			"round":       2,
			"clean_round": nil,
			"plan": map[string]any{
				"plan_id": "plan-r2", "path": "docs/review/plan-r2.json", "sha256": sha,
				"revision": 1, "review_round": 2, "status": planStatus,
				"e2e_coverage_state":              "regression_available",
				"verification_artifact_workspace": nil,
				"verification_artifact_digest":    nil,
				"submitted_at":                    "2026-08-22T00:00:00Z",
			},
			"claims": map[string]any{
				"CLAIM-DV-1":   claim("delivery", "required", "pass", "assignment-dv-1", "rr-dv-1", []any{}),
				"CLAIM-QA-1":   claim("qa", "required", "finding", "assignment-qa-1", "rr-qa-1", []any{"F-1"}),
				"CLAIM-QA-2":   claim("qa", "required", "running", "assignment-qa-2", nil, []any{}),
				"CLAIM-QA-3":   claim("qa", "required", "running", "assignment-qa-3", nil, []any{}),
				"CLAIM-E2E-1":  claim("e2e", "required", "planned", "assignment-e2e-1", nil, []any{}),
				"CLAIM-E2E-NA": claim("e2e", "not_applicable", "not_applicable", "", nil, []any{}),
			},
			"assignments": map[string]any{
				"assignment-dv-1":  assignment("delivery", "consumed", []any{"CLAIM-DV-1"}, "agent-dv-1", "docs/review/rr-dv-1.json"),
				"assignment-qa-1":  assignment("qa", "consumed", []any{"CLAIM-QA-1"}, "agent-qa-1", "docs/review/rr-qa-1.json"),
				"assignment-qa-2":  assignment("qa", "dispatched", []any{"CLAIM-QA-2"}, "agent-qa-2", nil),
				"assignment-qa-3":  assignment("qa", "dispatched", []any{"CLAIM-QA-3"}, "agent-qa-3", nil),
				"assignment-e2e-1": assignment("e2e", "planned", []any{"CLAIM-E2E-1"}, nil, nil),
			},
			"observation_batch": nil,
		},
		"entities": map[string]any{
			"agents": []any{
				map[string]any{"id": "agent-qa-2", "state": "working"},
				map[string]any{"id": "agent-qa-3", "state": "blocked"},
			},
		},
	}
}

func TestBuildGuidanceS7RecoveryProjectionCannotClean(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, event := range []string{"SessionStart", "PreCompact"} {
		t.Run(event, func(t *testing.T) {
			state := s7RecoveryFixtureState("cannot_clean", "cannot_clean")
			guidance := buildGuidance(root, state, event, policy.Input{})

			if guidance.Stage != "S7" {
				t.Fatalf("expected S7 guidance, got stage %q", guidance.Stage)
			}
			automation := strings.Join(guidance.Automation, "\n")
			if !strings.Contains(automation, "S7 review round 2") || !strings.Contains(automation, "status=cannot_clean") {
				t.Fatalf("recovery must carry the current round and plan status, got:\n%s", automation)
			}
			if !strings.Contains(automation, "S7 assignments running: assignment-qa-2(agent-qa-2)") {
				t.Fatalf("running bucket must list the dispatched assignment, got:\n%s", automation)
			}
			if !strings.Contains(automation, "S7 assignments queued: assignment-e2e-1") {
				t.Fatalf("queued bucket must list the undispatched assignment, got:\n%s", automation)
			}
			if !strings.Contains(automation, "S7 assignments blocked: assignment-qa-3(agent-qa-3)") {
				t.Fatalf("blocked bucket must list the assignment whose agent is blocked, got:\n%s", automation)
			}
			if !strings.Contains(automation, "S7 unconsumed ReviewResults") ||
				!strings.Contains(automation, "assignment-qa-2") || !strings.Contains(automation, "assignment-qa-3") {
				t.Fatalf("unconsumed ReviewResults must list dispatched-not-consumed assignments, got:\n%s", automation)
			}
			for _, gap := range []string{"claim:CLAIM-QA-2", "claim:CLAIM-QA-3", "claim:CLAIM-E2E-1"} {
				if !containsString(guidance.Missing, gap) {
					t.Fatalf("coverage gap %s must surface in Missing, got %#v", gap, guidance.Missing)
				}
			}
			for _, settled := range []string{"claim:CLAIM-DV-1", "claim:CLAIM-QA-1", "claim:CLAIM-E2E-NA"} {
				if containsString(guidance.Missing, settled) {
					t.Fatalf("settled/N/A claim %s must not be reported as a coverage gap, got %#v", settled, guidance.Missing)
				}
			}
			if !strings.Contains(guidance.Action, "assignment-qa-2") || !strings.Contains(guidance.Action, "review-result submit") {
				t.Fatalf("the single next action must consume the running assignment's Result, got %q", guidance.Action)
			}
			if len(guidance.Recovery) == 0 || !strings.Contains(guidance.Recovery[0], "S7 recovery: round 2") {
				t.Fatalf("recovery block must lead with the S7 summary, got %#v", guidance.Recovery)
			}
			if !strings.Contains(guidance.Instruction, "S7 review round 2") {
				t.Fatalf("the rendered instruction must carry the S7 recovery projection, got %q", guidance.Instruction)
			}
		})
	}
}

func TestBuildGuidanceS7RecoveryProjectionDraining(t *testing.T) {
	root := filepath.Join("..", "..")
	state := s7RecoveryFixtureState("discovery_draining", "discovery_draining")
	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})

	automation := strings.Join(guidance.Automation, "\n")
	if !strings.Contains(automation, "status=discovery_draining") {
		t.Fatalf("draining round must surface its plan status, got:\n%s", automation)
	}
	if !strings.Contains(guidance.Action, "review-result submit") {
		t.Fatalf("draining round must keep consuming pending Results, got %q", guidance.Action)
	}
}

func TestBuildGuidanceS7RecoveryQueuedDispatchAction(t *testing.T) {
	root := filepath.Join("..", "..")
	state := s7RecoveryFixtureState("running", "running")
	// Remove every dispatched assignment: only queued coverage remains.
	assignments := state["review"].(map[string]any)["assignments"].(map[string]any)
	delete(assignments, "assignment-qa-2")
	delete(assignments, "assignment-qa-3")
	state["entities"] = map[string]any{"agents": []any{}}

	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})
	if !strings.Contains(guidance.Action, "assignment-e2e-1") || !strings.Contains(guidance.Action, "register-workgroup") {
		t.Fatalf("with only queued coverage the single next action must dispatch it, got %q", guidance.Action)
	}
	if !strings.Contains(strings.Join(guidance.Automation, "\n"), "S7 assignments running: none") {
		t.Fatalf("empty buckets must be explicit, got %#v", guidance.Automation)
	}
}

func TestS7RecoveryBlockedActionNamesRecoveryVerb(t *testing.T) {
	got := s7RecoveryNextAction(
		"cannot_clean", 2,
		nil,
		nil,
		[]string{"assignment-qa-3(agent-qa-3)"},
		nil,
		nil,
	)
	for _, want := range []string{
		"runtime agent-event --event blocker_resolved --agent-id <id> --message <file>",
		"resubmit",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("blocked S7 recovery action must contain %q, got %q", want, got)
		}
	}
}

func TestBuildGuidanceS7RecoveryTerminalActions(t *testing.T) {
	root := filepath.Join("..", "..")
	cases := []struct {
		phase      string
		planStatus string
		action     string
	}{
		{"observation_sealed", "observation_sealed", "TR-008"},
		{"clean", "clean", "TR-009"},
		{"planned", "", "runtime review-plan"},
	}
	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			state := s7RecoveryFixtureState(tc.phase, tc.planStatus)
			if tc.planStatus == "" {
				state["review"].(map[string]any)["plan"] = nil
			}
			guidance := buildGuidance(root, state, "PreCompact", policy.Input{})
			if !strings.Contains(guidance.Action, tc.action) {
				t.Fatalf("phase %s must produce next action containing %q, got %q", tc.phase, tc.action, guidance.Action)
			}
		})
	}
}

func TestBuildGuidanceS7RecoveryNotAppliedOutsideVerification(t *testing.T) {
	root := filepath.Join("..", "..")
	state := map[string]any{
		"runtime_id": "loop-REQ-TEST",
		"revision":   7,
		"lifecycle":  map[string]any{"state": "building", "phase": "implementation"},
	}
	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})
	if strings.Contains(strings.Join(guidance.Automation, "\n"), "S7 review round") {
		t.Fatalf("non-verification phases must not carry the S7 recovery projection, got %#v", guidance.Automation)
	}
}

// TestRefreshMilestonePersistsS7RecoveryProjection drives the CAS milestone
// write so the durable checkpoint (what PreCompact promises the next
// SessionStart will re-emit) carries the same S7 projection.
func TestRefreshMilestonePersistsS7RecoveryProjection(t *testing.T) {
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
	fixture := s7RecoveryFixtureState("cannot_clean", "cannot_clean")
	state["lifecycle"] = fixture["lifecycle"]
	state["review"] = fixture["review"]
	agentRow := func(id, agentState string) map[string]any {
		return map[string]any{
			"id": id, "role": "QA", "state": agentState, "task_ids": []any{}, "team_id": nil,
			"definition_ref": "agents/qa.md", "prompt_ref": "prompts/qa.md",
			"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-08-22T00:00:00Z",
		}
	}
	entities, _ := state["entities"].(map[string]any)
	entities["agents"] = []any{agentRow("agent-qa-2", "working"), agentRow("agent-qa-3", "blocked")}
	state["entities"] = entities
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	guidance := buildGuidance(root, state, "PreCompact", policy.Input{})
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	updated, changed, err := refreshMilestone(root, statePath, journalPath, snapshot, guidance, "PreCompact")
	if err != nil {
		t.Fatalf("refresh milestone: %v", err)
	}
	if !changed {
		t.Fatal("first refresh must persist the milestone")
	}
	milestone, _ := updated.State["milestone"].(map[string]any)
	if milestone == nil {
		t.Fatal("milestone must be persisted in runtime state")
	}
	automationJSON, _ := json.Marshal(milestone["automation"])
	if !strings.Contains(string(automationJSON), "S7 review round 2") ||
		!strings.Contains(string(automationJSON), "assignment-qa-3(agent-qa-3)") {
		t.Fatalf("persisted milestone must carry the S7 recovery projection, got %s", automationJSON)
	}
	missingJSON, _ := json.Marshal(milestone["missing"])
	if !strings.Contains(string(missingJSON), "claim:CLAIM-E2E-1") {
		t.Fatalf("persisted milestone must carry the claim coverage gaps, got %s", missingJSON)
	}
	recoveryJSON, _ := json.Marshal(milestone["recovery"])
	if !strings.Contains(string(recoveryJSON), "S7 recovery: round 2") {
		t.Fatalf("persisted milestone must lead recovery with the S7 summary, got %s", recoveryJSON)
	}
}

// TestBuildGuidanceS7RecoveryClaimsCannotCleanDrainPolicy asserts the
// recovery packet surfaces the drain_policy=complete_required_claims
// invariant and lists the exact ObservationBatch state for cannot_clean /
// discovery_draining rounds, so a compacted Agent does not mistake the
// draining phase for the round ending.
func TestBuildGuidanceS7RecoveryClaimsCannotCleanDrainPolicy(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, phase := range []string{"cannot_clean", "discovery_draining"} {
		t.Run(phase, func(t *testing.T) {
			state := s7RecoveryFixtureState(phase, phase)
			// Open an ObservationBatch with two findings so the batch line is
			// rendered with a non-trivial finding count.
			state["review"].(map[string]any)["observation_batch"] = map[string]any{
				"batch_id":     "observation-batch-r2",
				"path":         ".claude/evidence/loop-REQ-TEST/g1/review/observation-batch-r2.json",
				"sha256":       strings.Repeat("c", 64),
				"finding_ids":  []any{"F-1", "F-2"},
				"drain_policy": "complete_required_claims",
				"sealed_at":    "2026-08-23T00:00:00Z",
			}
			guidance := buildGuidance(root, state, "SessionStart", policy.Input{})

			automation := strings.Join(guidance.Automation, "\n")
			wantDrain := "S7 round status=" + phase + ": ObservationBatch is open with drain_policy=complete_required_claims; cannot_clean/discovery_draining ≠ end — finish the remaining required Claims listed in Missing"
			if !strings.Contains(automation, wantDrain) {
				t.Fatalf("draining recovery must carry the drain_policy invariant line, got:\n%s", automation)
			}
			wantBatch := "S7 ObservationBatch observation-batch-r2: drain_policy=complete_required_claims; 2 finding(s) sealed"
			if !strings.Contains(automation, wantBatch) {
				t.Fatalf("draining recovery must name the open ObservationBatch and finding count, got:\n%s", automation)
			}
		})
	}
}

// TestBuildGuidanceS7RecoveryClaimsNoBatchOpen asserts the recovery packet
// behaviour when the plan is in cannot_clean / discovery_draining but the
// ObservationBatch pointer is missing from state. The drain invariant line
// is mandatory; the batch line must NOT say "not yet opened" (that would
// contradict the drain invariant) — the pointer-missing case is surfaced
// as a control-plane inconsistency with a `loop-harness doctor` hint so a
// compacted Agent does not invent a batch that does not exist.
func TestBuildGuidanceS7RecoveryClaimsNoBatchOpen(t *testing.T) {
	root := filepath.Join("..", "..")
	state := s7RecoveryFixtureState("cannot_clean", "cannot_clean")
	state["review"].(map[string]any)["observation_batch"] = nil
	guidance := buildGuidance(root, state, "PreCompact", policy.Input{})

	automation := strings.Join(guidance.Automation, "\n")
	if !strings.Contains(automation, "ObservationBatch is open with drain_policy=complete_required_claims") {
		t.Fatalf("draining recovery must carry the drain_policy invariant line, got:\n%s", automation)
	}
	if !strings.Contains(automation, "pointer missing from state.review despite") {
		t.Fatalf("draining recovery must surface the missing-pointer diagnostic, got:\n%s", automation)
	}
	if strings.Contains(automation, "not yet opened") {
		t.Fatalf("draining recovery must NOT claim 'not yet opened' alongside the drain invariant (contradiction), got:\n%s", automation)
	}
}

// TestBuildGuidanceS7RecoveryStripsBareClaimResults asserts the recovery
// packet drops the legacy open-items `claim_results` aggregate when the S7
// projection supersedes it with the precise `claim:<id>` matrix (L3-S7 §8).
// Other lifecycle phases are not affected (the aggregate stays in the
// `next`/`status` open-items projection, which the recovery packet is not).
func TestBuildGuidanceS7RecoveryStripsBareClaimResults(t *testing.T) {
	root := filepath.Join("..", "..")
	state := s7RecoveryFixtureState("running", "running")
	// buildNextProjection seeds guidance.Missing with the legacy
	// `claim_results` open-items token from the S7 stage contract. The
	// recovery projection must drop it.
	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})

	for _, m := range guidance.Missing {
		if m == "claim_results" {
			t.Fatalf("S7 recovery must strip the legacy `claim_results` token, got %#v", guidance.Missing)
		}
	}
	for _, claimID := range []string{"CLAIM-QA-2", "CLAIM-QA-3", "CLAIM-E2E-1"} {
		if !containsString(guidance.Missing, "claim:"+claimID) {
			t.Fatalf("S7 recovery must keep the precise per-Claim matrix, missing claim:%s in %#v", claimID, guidance.Missing)
		}
	}
}

// TestBuildGuidanceS8EntryProjectionFromTR008 asserts the SessionStart /
// PreCompact recovery packet during bug_resolution carries the exact
// observation_batch source line (L3-S7 §3.7), so a compacted Agent can
// re-bind to the right sealed batch without reading the milestone log.
func TestBuildGuidanceS8EntryProjectionFromTR008(t *testing.T) {
	root := filepath.Join("..", "..")
	state := s8EntryFixtureState()
	for _, event := range []string{"SessionStart", "PreCompact"} {
		t.Run(event, func(t *testing.T) {
			guidance := buildGuidance(root, state, event, policy.Input{})

			automation := strings.Join(guidance.Automation, "\n")
			want := "S8 entered via TR-008 with observation_batch observation-batch-r2 (2 findings, drain_policy=complete_required_claims)"
			if !strings.Contains(automation, want) {
				t.Fatalf("S8 entry must carry the TR-008 observation_batch source line, got:\n%s", automation)
			}
			// Recovery block must mirror the source fact so PreCompact (which
			// may drop Automation) still preserves it.
			if len(guidance.Recovery) == 0 || !strings.Contains(guidance.Recovery[0], want) {
				t.Fatalf("S8 Recovery must lead with the TR-008 source line, got %#v", guidance.Recovery)
			}
			if !strings.Contains(guidance.Instruction, want) {
				t.Fatalf("rendered instruction must carry the S8 entry source line, got %q", guidance.Instruction)
			}
		})
	}
}

// TestBuildGuidanceS8EntryProjectionWithoutBatch asserts the S8 projection
// is a no-op when no ObservationBatch is present in state.review. The
// control plane would surface a contradiction elsewhere; the recovery
// packet must not invent a source line.
func TestBuildGuidanceS8EntryProjectionWithoutBatch(t *testing.T) {
	root := filepath.Join("..", "..")
	state := s8EntryFixtureState()
	state["review"].(map[string]any)["observation_batch"] = nil
	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})

	for _, line := range guidance.Automation {
		if strings.Contains(line, "entered via TR-008") {
			t.Fatalf("S8 entry source line must NOT be appended without an ObservationBatch, got %#v", guidance.Automation)
		}
	}
}

// TestBuildGuidanceS8EntryProjectionNotAppliedOutsideBugResolution
// asserts the S8 entry projection only fires when lifecycleState is
// bug_resolution (and only on SessionStart / PreCompact), so the recovery
// packet does not leak the source line into S7 or later.
func TestBuildGuidanceS8EntryProjectionNotAppliedOutsideBugResolution(t *testing.T) {
	root := filepath.Join("..", "..")
	state := s8EntryFixtureState()
	// S7 lifecycle must not carry the S8 entry line.
	state["lifecycle"].(map[string]any)["state"] = "verification"
	guidance := buildGuidance(root, state, "SessionStart", policy.Input{})
	for _, line := range guidance.Automation {
		if strings.Contains(line, "entered via TR-008") {
			t.Fatalf("S7 lifecycle must not carry the S8 entry source line, got %#v", guidance.Automation)
		}
	}
	// BugResolution + non-guidance event must not carry it either.
	state["lifecycle"].(map[string]any)["state"] = "bug_resolution"
	// PreToolUse path goes through the controller cycle, not the recovery
	// projection — verify the buildGuidance path skips non-SessionStart /
	// non-PreCompact events.
	guidance = buildGuidance(root, state, "PreToolUse", policy.Input{})
	for _, line := range guidance.Automation {
		if strings.Contains(line, "entered via TR-008") {
			t.Fatalf("PreToolUse must not carry the S8 entry source line, got %#v", guidance.Automation)
		}
	}
}

// s8EntryFixtureState mirrors the S7 fixture with lifecycle flipped to
// bug_resolution and an ObservationBatch pointer populated as if TR-008
// has just committed the sealed handoff (L3-S7 §3.7).
func s8EntryFixtureState() map[string]any {
	fixture := s7RecoveryFixtureState("observation_sealed", "observation_sealed")
	fixture["lifecycle"].(map[string]any)["state"] = "bug_resolution"
	fixture["lifecycle"].(map[string]any)["phase"] = "investigation"
	fixture["review"].(map[string]any)["observation_batch"] = map[string]any{
		"batch_id":     "observation-batch-r2",
		"path":         ".claude/evidence/loop-REQ-TEST/g1/review/observation-batch-r2.json",
		"sha256":       strings.Repeat("c", 64),
		"finding_ids":  []any{"F-1", "F-2"},
		"drain_policy": "complete_required_claims",
		"sealed_at":    "2026-08-23T00:00:00Z",
	}
	return fixture
}
