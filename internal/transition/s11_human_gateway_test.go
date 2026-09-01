package transition_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestBUG104S11DefinitionHasHumanGatewayRoutes(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	if containsString(catalog.Definition.TerminalStates, "awaiting_human_release") {
		t.Fatal("awaiting_human_release must be a non-terminal human control state")
	}
	if !containsString(catalog.Definition.TerminalStates, "release_authorized") {
		t.Fatal("release_authorized must be a terminal state")
	}

	wantTargets := map[string]string{
		"TR-025": "release_authorized",
		"TR-026": "paused",
		"TR-027": "bug_resolution",
		"TR-028": "acceptance",
		"TR-029": "release_audit",
		"TR-030": "aborted",
	}
	for id, wantTarget := range wantTargets {
		spec, ok := catalog.Transitions[id]
		if !ok {
			t.Errorf("missing S11 transition %s", id)
			continue
		}
		if spec.From != "awaiting_human_release" || spec.To != wantTarget {
			t.Errorf("%s cursor = %s -> %s, want awaiting_human_release -> %s", id, spec.From, spec.To, wantTarget)
		}
	}
}

func TestBUG104S11RoutesAreHumanBoundaries(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	want := map[string]struct {
		event    string
		target   string
		actions  []string
		evidence []string
		guards   []string
	}{
		"TR-025": {event: "human_release_approved", target: "release_authorized", actions: []string{"record_human_release_decision"}, evidence: []string{"human_decision_record"}},
		"TR-026": {event: "human_release_deferred", target: "paused", actions: []string{"capture_pause_checkpoint", "record_human_release_decision"}, evidence: []string{"human_decision_record", "pause_record"}},
		"TR-027": {event: "human_release_rejected_defect", target: "bug_resolution", actions: []string{"record_human_release_decision"}, evidence: []string{"human_decision_record", "finding_record"}},
		"TR-028": {event: "human_release_rejected_acceptance", target: "acceptance", actions: []string{"record_human_release_decision", "invalidate_human_release_acceptance_evidence"}, evidence: []string{"human_decision_record"}},
		"TR-029": {event: "human_release_rejected_release_audit", target: "release_audit", actions: []string{"record_human_release_decision", "invalidate_human_release_release_audit_evidence"}, evidence: []string{"human_decision_record"}},
		"TR-030": {event: "human_release_aborted", target: "aborted", actions: []string{"record_human_release_decision"}, evidence: []string{"human_decision_record"}, guards: []string{"human_abort_approved"}},
	}
	for id, want := range want {
		spec, ok := catalog.Transitions[id]
		if !ok {
			t.Fatalf("missing S11 transition %s", id)
		}
		if spec.Event != want.event || spec.From != "awaiting_human_release" || spec.To != want.target {
			t.Errorf("%s route = %s %s -> %s, want %s awaiting_human_release -> %s", id, spec.Event, spec.From, spec.To, want.event, want.target)
		}
		if spec.Automation == nil || spec.Automation.Eligible || !spec.Automation.HumanBoundary {
			t.Errorf("%s must be human-boundary only, got %#v", id, spec.Automation)
		}
		if spec.AutoTrigger != nil {
			t.Errorf("%s must not declare auto_trigger", id)
		}
		if !sameStrings(spec.Actors, []string{"user", "orchestrator"}) {
			t.Errorf("%s actors = %v, want [user orchestrator]", id, spec.Actors)
		}
		if !sameStrings(spec.Actions, want.actions) {
			t.Errorf("%s actions = %v, want %v", id, spec.Actions, want.actions)
		}
		if !sameStrings(spec.RequiredEvidence, want.evidence) {
			t.Errorf("%s evidence = %v, want %v", id, spec.RequiredEvidence, want.evidence)
		}
		if !sameStrings(spec.Guards, want.guards) {
			t.Errorf("%s guards = %v, want %v", id, spec.Guards, want.guards)
		}
	}
}

func TestBUG104LoopStateSchemaAcceptsReleaseAuthorized(t *testing.T) {
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["lifecycle"].(map[string]any)["state"] = "release_authorized"
	updated, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", updated); err != nil {
		t.Fatalf("release_authorized must be a valid runtime state: %v", err)
	}
}

func TestBUG104HumanReleaseDecisionActionRecordsEvidenceWithoutSideEffect(t *testing.T) {
	action, ok := transition.LookupAction("record_human_release_decision")
	if !ok {
		t.Fatal("record_human_release_decision must be registered")
	}
	state := map[string]any{"release_side_effect": nil}
	result, err := action(state, &transition.ActionContext{
		Spec:     transition.TransitionSpec{ID: "TR-025", Event: "human_release_approved"},
		Evidence: map[string]string{"human_decision_record": "decision-1"},
	})
	if err != nil {
		t.Fatalf("record_human_release_decision failed: %v", err)
	}
	if result.Status != "committed" || result.MutationApplied {
		t.Fatalf("decision action result = %#v, want committed journal-only result", result)
	}
	if !strings.Contains(result.Detail, "human_release_approved") || !strings.Contains(result.Detail, "decision-1") {
		t.Fatalf("decision action detail = %q, want event and evidence reference", result.Detail)
	}
	if state["release_side_effect"] != nil {
		t.Fatal("human release decision must not create a release side effect")
	}
}

func TestBUG104HumanReleaseEvidenceInvalidationIsExact(t *testing.T) {
	cases := []struct {
		name   string
		action string
		id     string
		valid  map[string]bool
	}{
		{name: "acceptance rejection", action: "invalidate_human_release_acceptance_evidence", id: "TR-028", valid: map[string]bool{
			"acceptance": false, "acceptance_record": false, "release_audit": false, "release_audit_record": false,
			"human_decision": true, "change_impact": true,
		}},
		{name: "release audit rejection", action: "invalidate_human_release_release_audit_evidence", id: "TR-029", valid: map[string]bool{
			"acceptance": true, "acceptance_record": true, "release_audit": false, "release_audit_record": false,
			"human_decision": true, "change_impact": true,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action, ok := transition.LookupAction(tc.action)
			if !ok {
				t.Fatalf("%s must be registered", tc.action)
			}
			state := map[string]any{"evidence": []any{}}
			for kind := range tc.valid {
				state["evidence"] = append(state["evidence"].([]any), map[string]any{
					"id": kind, "kind": kind, "status": "valid",
				})
			}
			_, err := action(state, &transition.ActionContext{Spec: transition.TransitionSpec{ID: tc.id}})
			if err != nil {
				t.Fatalf("%s failed: %v", tc.action, err)
			}
			for _, raw := range state["evidence"].([]any) {
				entry := raw.(map[string]any)
				kind := entry["kind"].(string)
				wantValid := tc.valid[kind]
				if (entry["status"] == "valid") != wantValid {
					t.Errorf("evidence kind %s status = %v, want valid=%v", kind, entry["status"], wantValid)
				}
			}
		})
	}
}

func TestBUG104DeferCapturesS11BeforeCursorChange(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := inactiveState(5)
	state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": float64(1)}
	registerFixtureEvidence(t, root, state, map[string]string{"human_decision_record": "docs/reports/human/decision.md"})
	scopeFixtureEvidence(t, state, "docs/reports/human/decision.md", "runtime_release:loop-inactive@5")
	writeFullState(t, root, state)

	if err := applyT(t, root, "TR-026", 5, "orchestrator", map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"pause_record":          "generated:pause_checkpoint",
	}); err != nil {
		t.Fatalf("TR-026 failed: %v", err)
	}
	updated := readState(t, root)
	pause := updated["pause"].(map[string]any)
	if pause["from_state"] != "awaiting_human_release" || pause["from_phase"] != nil {
		t.Fatalf("S11 checkpoint = %#v, want awaiting_human_release with nil phase", pause)
	}
	if updated["lifecycle"].(map[string]any)["state"] != "paused" {
		t.Fatalf("state after defer = %#v, want paused", updated["lifecycle"])
	}
	// The defer decision has been consumed by TR-026 and must not authorize the
	// resume. A fresh decision is required for the new human boundary.
	state6 := readState(t, root)
	registerFixtureEvidence(t, root, state6, map[string]string{
		"human_decision_record": "docs/reports/human/resume-decision.md",
	})
	scopeFixtureEvidence(t, state6, "docs/reports/human/resume-decision.md", "runtime_resume:loop-inactive@6")
	writeFullState(t, root, state6)
	if err := applyT(t, root, "TR-019", 6, "user", map[string]string{
		"human_decision_record": "docs/reports/human/resume-decision.md",
		"pause_record":          "generated:pause_checkpoint",
	}); err != nil {
		t.Fatalf("TR-019 must resume the S11 checkpoint: %v", err)
	}
	resumed := readState(t, root)
	if resumed["lifecycle"].(map[string]any)["state"] != "awaiting_human_release" || resumed["pause"] != nil {
		t.Fatalf("TR-019 result = %#v, want S11 with cleared pause checkpoint", resumed["lifecycle"])
	}
}

func TestBUG104S11HumanRoutesApplyThroughCatalog(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		evidence  []s11EvidenceFixture
		wantState string
		wantPhase any
	}{
		{name: "approve", id: "TR-025", evidence: []s11EvidenceFixture{{slot: "human_decision_record", kind: "human_decision"}}, wantState: "release_authorized"},
		{name: "reject defect", id: "TR-027", evidence: []s11EvidenceFixture{{slot: "human_decision_record", kind: "human_decision"}, {slot: "finding_record", kind: "bug"}}, wantState: "bug_resolution", wantPhase: "investigation"},
		{name: "reject acceptance", id: "TR-028", evidence: []s11EvidenceFixture{{slot: "human_decision_record", kind: "human_decision"}, {slot: "old-acceptance", kind: "acceptance"}, {slot: "old-release-audit", kind: "release_audit"}}, wantState: "acceptance"},
		{name: "reject release audit", id: "TR-029", evidence: []s11EvidenceFixture{{slot: "human_decision_record", kind: "human_decision"}, {slot: "old-acceptance", kind: "acceptance"}, {slot: "old-release-audit", kind: "release_audit"}}, wantState: "release_audit"},
		{name: "abort", id: "TR-030", evidence: []s11EvidenceFixture{{slot: "human_decision_record", kind: "human_decision"}}, wantState: "aborted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			setupRepoWithDefinition(t, root)
			state := inactiveState(5)
			state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": float64(1)}
			refs := addS11Evidence(t, root, state, tc.evidence)
			writeFullState(t, root, state)

			snapshot, err := transition.Apply(root, filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl"), transition.Request{
				TransitionID: tc.id, ExpectedRevision: 5, Actor: "user", Evidence: refs,
			})
			if err != nil {
				t.Fatalf("%s failed: %v", tc.id, err)
			}
			lifecycle := snapshot.State["lifecycle"].(map[string]any)
			if lifecycle["state"] != tc.wantState || lifecycle["phase"] != tc.wantPhase {
				t.Fatalf("cursor = %#v, want %s.%v", lifecycle, tc.wantState, tc.wantPhase)
			}
			if tc.id == "TR-028" || tc.id == "TR-029" {
				assertS11EvidenceStatus(t, snapshot.State, "human-decision-record", "valid")
				assertS11EvidenceStatus(t, snapshot.State, "old-acceptance", map[string]string{"TR-028": "invalid", "TR-029": "valid"}[tc.id])
				assertS11EvidenceStatus(t, snapshot.State, "old-release-audit", "invalid")
			}
		})
	}
}

type s11EvidenceFixture struct {
	slot string
	kind string
}

func addS11Evidence(t *testing.T, root string, state map[string]any, fixtures []s11EvidenceFixture) map[string]string {
	t.Helper()
	refs := make(map[string]string, len(fixtures))
	items := state["evidence"].([]any)
	for index, fixture := range fixtures {
		ref := "docs/reports/human/" + fixture.slot + ".md"
		if fixture.slot == "old-acceptance" || fixture.slot == "old-release-audit" {
			ref = "docs/reports/release/" + fixture.slot + ".json"
		}
		content := []byte("fixture evidence: " + fixture.slot + "\n")
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, ref)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ref), content, 0o644); err != nil {
			t.Fatal(err)
		}
		id := fixture.slot
		if fixture.slot == "human_decision_record" {
			id = "human-decision-record"
		}
		scopeRefs := []any{}
		if fixture.kind == "human_decision" {
			runtimeID, _ := state["runtime_id"].(string)
			scopeRefs = []any{fmt.Sprintf("runtime_release:%s@%d", runtimeID, fixtureInt(state["revision"]))}
		}
		items = append(items, map[string]any{
			"id": id, "kind": fixture.kind, "path": ref, "sha256": transition.SHA256(content),
			"status": "valid", "baseline_generation": 0, "review_round": nil,
			"produced_by": []any{"user"}, "invalidated_by": nil, "invalidation_rule": nil,
			"invalidation_reason": nil, "responsibility_id": nil, "scope_refs": scopeRefs,
		})
		if fixture.slot != "old-acceptance" && fixture.slot != "old-release-audit" {
			refs[fixture.slot] = id
		}
		if index == len(fixtures)-1 {
			state["evidence"] = items
		}
	}
	return refs
}

func assertS11EvidenceStatus(t *testing.T, state map[string]any, id, want string) {
	t.Helper()
	for _, raw := range state["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["id"] == id {
			if entry["status"] != want {
				t.Fatalf("evidence %s status = %v, want %s", id, entry["status"], want)
			}
			return
		}
	}
	t.Fatalf("evidence %s not found", id)
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
