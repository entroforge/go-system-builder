package integration

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func TestSupersededPredecessorResultRequiresUniqueRootEvidenceFromExactRevision(t *testing.T) {
	previous := assignmentrecord.Record{BaselineGeneration: 3, OwnerAgentID: "owner-r1"}
	valid := map[string]any{
		"id": "ev-result-r1", "kind": "completion_report", "path": ".claude/evidence/result-r1.json",
		"responsibility_id": "BUILD-WORK-PACKAGE", "status": "superseded", "invalidated_by": "evt-revision-r2",
		"invalidation_rule": "assignment_revision_issued", "baseline_generation": 3,
		"produced_by": []any{"owner-r1"},
	}
	ref, err := supersededPredecessorResultRef(map[string]any{"evidence": []any{valid}}, previous, "evt-revision-r2")
	if err != nil || ref != ".claude/evidence/result-r1.json" {
		t.Fatalf("resolve exact original superseded Result ref=%q err=%v", ref, err)
	}

	tests := []struct {
		name    string
		state   map[string]any
		eventID string
	}{
		{
			name:  "entities shadow is not Runtime evidence authority",
			state: map[string]any{"entities": map[string]any{"evidence": []any{valid}}},
		},
		{
			name:    "wrong revision event",
			state:   map[string]any{"evidence": []any{valid}},
			eventID: "evt-other-revision",
		},
		{
			name: "wrong invalidation rule",
			state: map[string]any{"evidence": []any{func() map[string]any {
				copy := cloneEvidenceTestRow(valid)
				copy["invalidation_rule"] = "result_resubmitted"
				return copy
			}()}},
		},
		{
			name: "wrong owner",
			state: map[string]any{"evidence": []any{func() map[string]any {
				copy := cloneEvidenceTestRow(valid)
				copy["produced_by"] = []any{"other-owner"}
				return copy
			}()}},
		},
		{
			name: "multiple matching refs",
			state: map[string]any{"evidence": []any{
				valid,
				func() map[string]any {
					copy := cloneEvidenceTestRow(valid)
					copy["id"] = "ev-result-r1-duplicate"
					copy["path"] = ".claude/evidence/other-result-r1.json"
					return copy
				}(),
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			eventID := test.eventID
			if eventID == "" {
				eventID = "evt-revision-r2"
			}
			if _, err := supersededPredecessorResultRef(test.state, previous, eventID); err == nil {
				t.Fatal("invalid or ambiguous predecessor Result evidence was accepted")
			}
		})
	}
}

func cloneEvidenceTestRow(row map[string]any) map[string]any {
	clone := make(map[string]any, len(row))
	for key, value := range row {
		clone[key] = value
	}
	return clone
}

func TestCanonicalPredecessorExecutionGenerationComesFromOriginalWorkspaceHistory(t *testing.T) {
	previous := revisionBindingTestRecord()
	if generation, err := canonicalPredecessorExecutionGeneration(map[string]any{}, previous); err != nil || generation != 0 {
		t.Fatalf("native predecessor generation=%d err=%v, want 0", generation, err)
	}
	sha, err := previous.Digest()
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{
		"assignment_id": previous.AssignmentID, "runtime_id": previous.RuntimeID,
		"baseline_generation": previous.BaselineGeneration, "execution_generation": 7,
		"agent_id": previous.OwnerAgentID, "assignment_record": previous.Reference(),
		"assignment_record_sha256": sha, "status": "retired",
	}
	state := predecessorWorkspaceState([]any{row})
	generation, err := canonicalPredecessorExecutionGeneration(state, previous)
	if err != nil || generation != 7 {
		t.Fatalf("historical predecessor generation=%d err=%v, want original generation 7", generation, err)
	}

	for _, test := range []struct {
		name string
		rows []any
	}{
		{
			name: "record digest mismatch",
			rows: []any{func() map[string]any {
				copy := cloneEvidenceTestRow(row)
				copy["assignment_record_sha256"] = strings.Repeat("0", 64)
				return copy
			}()},
		},
		{
			name: "owner tuple without exact record ref",
			rows: []any{func() map[string]any {
				copy := cloneEvidenceTestRow(row)
				copy["assignment_record"] = nil
				return copy
			}()},
		},
		{
			name: "predecessor is not retired",
			rows: []any{func() map[string]any {
				copy := cloneEvidenceTestRow(row)
				copy["status"] = "ready"
				return copy
			}()},
		},
		{
			name: "duplicate historical record bindings",
			rows: []any{row, cloneEvidenceTestRow(row)},
		},
		{
			name: "no predecessor execution",
			rows: []any{map[string]any{
				"assignment_id": previous.AssignmentID, "runtime_id": previous.RuntimeID,
				"baseline_generation": previous.BaselineGeneration, "execution_generation": 1,
				"agent_id": "successor-owner", "status": "active",
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := canonicalPredecessorExecutionGeneration(predecessorWorkspaceState(test.rows), previous)
			if test.name == "no predecessor execution" {
				if err != nil || got != 0 {
					t.Fatalf("native/no-predecessor generation=%d err=%v, want 0", got, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("invalid historical workspace binding returned generation %d", got)
			}
		})
	}
}

func predecessorWorkspaceState(history []any) map[string]any {
	return map[string]any{
		"bound_req": map[string]any{"workspace": map[string]any{
			"project_root": "/repo", "dev_branch": "main",
			"release_upstream": "origin/main", "bound_commit": strings.Repeat("a", 40),
		}},
		"workspace": map[string]any{
			"binding_version": 1, "main_root": "/repo", "common_dir": "/repo/.git",
			"integration_branch": "main", "bound_head": strings.Repeat("a", 40),
			"executions": map[string]any{}, "execution_history": history,
		},
	}
}
