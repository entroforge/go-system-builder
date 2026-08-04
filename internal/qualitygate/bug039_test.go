package qualitygate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestSubjectsMatchEmptyMeansNoConstraint(t *testing.T) {
	docs := []documentFact{{Path: "docs/a.md", Version: "v1", SHA256: "abc"}}
	if !subjectsMatch(nil, docs) {
		t.Fatal("empty subject_refs must match (no document constraint)")
	}
	mismatched := []subjectRef{{Path: "docs/other.md", Version: "v1", SHA256: "def"}}
	if subjectsMatch(mismatched, docs) {
		t.Fatal("non-empty mismatched subject_refs must not match")
	}
}

func TestEvidenceKindsEqualRequirementEnvelopeAliases(t *testing.T) {
	tests := []struct {
		requirement, envelope string
		want                  bool
	}{
		{"team_manifest_record", "builder_report", true},
		{"team_manifest_record", "team_manifest", true},
		{"team_manifest_record", "document_review", false},
		{"completion_report", "agent_completion", true},
		{"completion_report", "completion_report", true},
		{"bug_batch_record", "bug", true},
		{"finding_record", "bug", true},
		{"finding_record", "finding", false},
		{"root_cause_record", "bug", true},
		{"root_cause_record", "root_cause", false},
		{"repair_record", "bug", true},
		{"repair_record", "repair", false},
		{"clean_round_record", "clean_round", true},
		{"targeted_reverification_record", "targeted_reverification", true},
		{"document_review_record", "document_review", true},
		{"activation_record", "agent_activation", true},
		{"activation_record", "activation", false},
		{"change_impact_record", "change_impact", true},
	}
	for _, tc := range tests {
		if got := evidenceKindsEqual(tc.requirement, tc.envelope); got != tc.want {
			t.Errorf("evidenceKindsEqual(%q, %q) = %v, want %v", tc.requirement, tc.envelope, got, tc.want)
		}
	}
}

func TestGateBugDraftsReadyAcceptsShortKindRootCause(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	evaluator := NewEvaluator(registry)
	input := bugDraftsReadyShortKindInput(t)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != StatusSatisfied {
		t.Fatalf("status = %q, want satisfied (missing=%v conflicts=%v)", result.Status, result.Missing, result.Conflicts)
	}
}

func TestGateRepairBuildersActivatedAcceptsShortKindActivation(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	evaluator := NewEvaluator(registry)
	input := repairBuildersActivatedShortKindInput(t)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != StatusSatisfied {
		t.Fatalf("status = %q, want satisfied (missing=%v conflicts=%v)", result.Status, result.Missing, result.Conflicts)
	}
}

func TestGatesSatisfyWithEmptySubjectRefs(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	evaluator := NewEvaluator(registry)

	tests := []struct {
		name           string
		gateID         string
		transitionID   string
		state          string
		phase          any
		reviewRound    int
		evidenceKind   string
		responsibility string
		conclusion     string
	}{
		{
			name: "clean round incomplete", gateID: "GATE-CLEAN-ROUND-INCOMPLETE",
			transitionID: "PTR-VERIFY-05", state: "verification", phase: "clean_round_evaluation",
			reviewRound: 2, evidenceKind: "clean_round", responsibility: "Clean Round Evaluator",
			conclusion: "incomplete",
		},
		{
			name: "bug reports rejected", gateID: "GATE-BUG-REPORTS-REJECTED",
			transitionID: "PTR-BUG-03", state: "bug_resolution", phase: "bug_report_review",
			reviewRound: 0, evidenceKind: "bug", responsibility: "Orchestrator",
			conclusion: "rejected",
		},
		{
			name: "targeted reverification fail", gateID: "GATE-TARGETED-REVERIFICATION-FAIL",
			transitionID: "PTR-BUG-07", state: "bug_resolution", phase: "targeted_reverification",
			reviewRound: 3, evidenceKind: "targeted_reverification", responsibility: "Original Finder",
			conclusion: "fail",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := emptySubjectGateInput(t, tc.gateID, tc.transitionID, tc.state, tc.phase, tc.reviewRound,
				tc.evidenceKind, tc.responsibility, tc.conclusion)
			result, err := evaluator.Evaluate(context.Background(), input)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if result.Status != StatusSatisfied {
				t.Fatalf("status = %q, want satisfied (missing=%v conflicts=%v)", result.Status, result.Missing, result.Conflicts)
			}
		})
	}
}

func TestBuilderBatchGateAcceptsShortKindTeamManifest(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	evaluator := NewEvaluator(registry)
	input := builderBatchShortKindInput(t)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != StatusNotReady {
		t.Fatalf("status = %q, want not_ready (missing second task completion)", result.Status)
	}
	if len(result.EvidenceRefs) != 2 {
		t.Fatalf("evidence refs = %#v, want team_manifest + completion", result.EvidenceRefs)
	}
}

func emptySubjectGateInput(
	t *testing.T,
	gateID, transitionID, state string,
	phase any,
	reviewRound int,
	kind, responsibility, conclusion string,
) Input {
	t.Helper()
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-empty-subjects",
		"kind":                    kind,
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"review_round":            reviewRound,
		"producer_agent_id":       "agent-1",
		"producer_responsibility": responsibility,
		"subject_refs":            []any{},
		"conclusion":              conclusion,
	}
	if reviewRound > 0 {
		envelope["review_round"] = reviewRound
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	files := map[string][]byte{"evidence/empty.json": data}
	sum := sha256Hex(data)
	return Input{
		Snapshot: runtime.Snapshot{
			Revision: 1,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": state, "phase": phase},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": reviewRound},
				"documents":  []any{},
				"evidence": []any{
					map[string]any{
						"id": "ev-empty-subjects", "kind": kind, "path": "evidence/empty.json",
						"sha256": sum, "status": "valid", "baseline_generation": 1,
						"review_round": reviewRound, "produced_by": []any{"agent-1"},
						"invalidated_by": nil, "responsibility_id": responsibility,
					},
				},
			},
		},
		GateID:       gateID,
		TransitionID: transitionID,
		Files:        memoryFileView(files),
	}
}

func bugDraftsReadyShortKindInput(t *testing.T) Input {
	t.Helper()
	files := map[string][]byte{}
	add := func(id, kind, responsibility, conclusion string, reviewRound int) map[string]any {
		envelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             id,
			"kind":                    kind,
			"runtime_id":              "loop-test",
			"baseline_generation":     1,
			"producer_agent_id":       "agent-1",
			"producer_responsibility": responsibility,
			"subject_refs":            []any{},
			"conclusion":              conclusion,
		}
		if reviewRound > 0 {
			envelope["review_round"] = reviewRound
		}
		data, _ := json.Marshal(envelope)
		path := "evidence/" + id + ".json"
		files[path] = data
		idx := map[string]any{
			"id": id, "kind": kind, "path": path, "sha256": sha256Hex(data),
			"status": "valid", "baseline_generation": 1,
			"produced_by": []any{"agent-1"}, "invalidated_by": nil,
			"responsibility_id": responsibility,
		}
		if reviewRound > 0 {
			idx["review_round"] = reviewRound
		} else {
			idx["review_round"] = nil
		}
		return idx
	}
	evidence := []any{
		add("ev-finding", "bug", "Delivery Verifier", "blocking", 1),
		add("ev-root-cause", "bug", "Investigator", "complete", 0),
	}
	return Input{
		Snapshot: runtime.Snapshot{
			Revision: 1,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "bug_resolution", "phase": "investigation"},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 1},
				"documents":  []any{},
				"evidence":   evidence,
			},
		},
		GateID:       "GATE-BUG-DRAFTS-READY",
		TransitionID: "PTR-BUG-01",
		Files:        memoryFileView(files),
	}
}

func repairBuildersActivatedShortKindInput(t *testing.T) Input {
	t.Helper()
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-activation",
		"kind":                    "agent_activation",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"producer_agent_id":       "orchestrator-1",
		"producer_responsibility": "Orchestrator",
		"subject_refs":            []any{},
		"conclusion":              "approved",
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	path := "evidence/ev-activation.json"
	files := map[string][]byte{path: data}
	return Input{
		Snapshot: runtime.Snapshot{
			Revision: 1,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "bug_resolution", "phase": "repair_readback"},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 0},
				"documents":  []any{},
				"evidence": []any{
					map[string]any{
						"id": "ev-activation", "kind": "agent_activation", "path": path,
						"sha256": sha256Hex(data), "status": "valid", "baseline_generation": 1,
						"review_round": nil, "produced_by": []any{"orchestrator-1"},
						"invalidated_by": nil, "responsibility_id": "Orchestrator",
					},
				},
			},
		},
		GateID:       "GATE-REPAIR-BUILDERS-ACTIVATED",
		TransitionID: "PTR-BUG-04",
		Files:        memoryFileView(files),
	}
}

func builderBatchShortKindInput(t *testing.T) Input {
	t.Helper()
	taskOne := []byte("# TASK 1\n")
	taskTwo := []byte("# TASK 2\n")
	files := map[string][]byte{
		"docs/tasks/TASK-TEST-01.md": taskOne,
		"docs/tasks/TASK-TEST-02.md": taskTwo,
	}
	add := func(id, kind, responsibility, conclusion, taskID string) map[string]any {
		envelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             id,
			"kind":                    kind,
			"runtime_id":              "loop-test",
			"baseline_generation":     1,
			"producer_agent_id":       "orchestrator-1",
			"producer_responsibility": responsibility,
			"subject_refs":            []any{},
			"conclusion":              conclusion,
		}
		if taskID != "" {
			envelope["task_id"] = taskID
		}
		data, _ := json.Marshal(envelope)
		path := "evidence/" + id + ".json"
		files[path] = data
		return map[string]any{
			"id": id, "kind": kind, "path": path, "sha256": sha256Hex(data),
			"status": "valid", "baseline_generation": 1, "review_round": nil,
			"produced_by": []any{"orchestrator-1"}, "invalidated_by": nil,
			"responsibility_id": responsibility,
		}
	}
	evidence := []any{
		add("ev-completion-1", "agent_completion", "BUILD-WORK-PACKAGE", "completed", "TASK-TEST-01"),
		add("ev-team", "builder_report", "Orchestrator", "complete", ""),
	}
	return Input{
		Snapshot: runtime.Snapshot{
			Revision: 1,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "building", "phase": nil},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 0},
				"documents":  []any{},
				"evidence":   evidence,
				"entities": map[string]any{
					"tasks": []any{
						map[string]any{"id": "TASK-TEST-01", "state": "review"},
						map[string]any{"id": "TASK-TEST-02", "state": "review"},
					},
				},
			},
		},
		GateID:       "GATE-BUILDER-BATCH-READY",
		TransitionID: "TR-006",
		Files:        memoryFileView(files),
	}
}

type memoryFileView map[string][]byte

func (m memoryFileView) ReadFile(path string) ([]byte, error) {
	return append([]byte(nil), m[path]...), nil
}
