package assignmentresult

import (
	"fmt"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func supersededAcceptedFixture(t *testing.T) (map[string]any, assignmentrecord.Record, assignmentrecord.Record, string, string, map[string][]byte) {
	t.Helper()
	state, previous, resultRef, files := acceptedFixture(t)
	successor := previous
	successor.Revision++
	successor.RegistrationRevision++
	successor.RevisionIssuance = &assignmentrecord.RevisionIssuance{
		IntentKey:      "revision-intent-2",
		PreviousRecord: previous.Reference(),
	}
	if err := assignmentrecord.ValidateRecord(successor); err != nil {
		t.Fatalf("fixture successor Record: %v", err)
	}
	entities := state["entities"].(map[string]any)
	entities["assignments"] = []any{mustRecordMap(t, previous), mustRecordMap(t, successor)}
	evidence := state["evidence"].([]any)[0].(map[string]any)
	evidence["status"] = "superseded"
	evidence["invalidated_by"] = "evt-s6-assignment-revision-test"
	evidence["invalidation_rule"] = "assignment_revision_issued"
	return state, previous, successor, evidence["invalidated_by"].(string), resultRef, files
}

func TestLoadSupersededAcceptedValidatesOriginalHistoricalClosure(t *testing.T) {
	state, previous, successor, eventID, resultRef, files := supersededAcceptedFixture(t)
	successor3 := successor
	successor3.Revision++
	successor3.RegistrationRevision++
	successor3.RevisionIssuance = &assignmentrecord.RevisionIssuance{
		IntentKey:      "revision-intent-3",
		PreviousRecord: successor.Reference(),
	}
	if err := assignmentrecord.ValidateRecord(successor3); err != nil {
		t.Fatalf("fixture later successor Record: %v", err)
	}
	state["entities"].(map[string]any)["assignments"] = []any{
		mustRecordMap(t, previous), mustRecordMap(t, successor), mustRecordMap(t, successor3),
	}
	read := func(ref string) ([]byte, error) {
		data, ok := files[ref]
		if !ok {
			return nil, fmt.Errorf("missing %s", ref)
		}
		return data, nil
	}
	result, err := LoadSupersededAccepted(state, previous, successor, eventID, resultRef, read)
	if err != nil {
		t.Fatalf("valid historical Result rejected: %v", err)
	}
	if result.AssignmentRecord != previous.Reference() || result.TaskID != previous.Task.ID {
		t.Fatalf("history reader returned the wrong predecessor binding: %+v", result)
	}
	if _, err := LoadAccepted(state, previous, resultRef, read); err == nil {
		t.Fatal("current LoadAccepted accepted a superseded historical Result")
	}
	state["runtime_id"] = "loop-current-after-revision"
	state["baseline"] = map[string]any{"generation": previous.BaselineGeneration + 1}
	if _, err := LoadSupersededAccepted(state, previous, successor, eventID, resultRef, read); err != nil {
		t.Fatalf("historical reader required this namespace/generation to remain current: %v", err)
	}
}

func TestLoadSupersededAcceptedRejectsUnprovenRevisionAndEvidenceBindings(t *testing.T) {
	for _, name := range []string{
		"wrong event", "wrong status", "wrong invalidation rule", "duplicate evidence path",
		"duplicate evidence ID", "missing predecessor", "missing successor", "successor gap",
		"oversized result",
	} {
		t.Run(name, func(t *testing.T) {
			state, previous, successor, eventID, resultRef, files := supersededAcceptedFixture(t)
			switch name {
			case "wrong event":
				eventID = "evt-s6-assignment-revision-different"
			case "wrong status":
				state["evidence"].([]any)[0].(map[string]any)["status"] = "valid"
			case "wrong invalidation rule":
				state["evidence"].([]any)[0].(map[string]any)["invalidation_rule"] = "other"
			case "duplicate evidence path":
				duplicate := make(map[string]any)
				for key, value := range state["evidence"].([]any)[0].(map[string]any) {
					duplicate[key] = value
				}
				state["evidence"] = append(state["evidence"].([]any), duplicate)
			case "duplicate evidence ID":
				duplicate := make(map[string]any)
				for key, value := range state["evidence"].([]any)[0].(map[string]any) {
					duplicate[key] = value
				}
				duplicate["path"] = ".claude/evidence/duplicate-result.json"
				state["evidence"] = append(state["evidence"].([]any), duplicate)
			case "missing predecessor":
				state["entities"].(map[string]any)["assignments"] = []any{mustRecordMap(t, successor)}
			case "missing successor":
				state["entities"].(map[string]any)["assignments"] = []any{mustRecordMap(t, previous)}
			case "successor gap":
				successor.Revision++
			case "oversized result":
				files[resultRef] = make([]byte, EvidenceReadLimit+1)
			}
			read := func(ref string) ([]byte, error) {
				data, ok := files[ref]
				if !ok {
					return nil, fmt.Errorf("missing %s", ref)
				}
				return data, nil
			}
			if _, err := LoadSupersededAccepted(state, previous, successor, eventID, resultRef, read); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

func TestLoadAcceptedRejectsDuplicateEvidenceIDOnDifferentPath(t *testing.T) {
	state, record, ref, files := acceptedFixture(t)
	duplicate := make(map[string]any)
	for key, value := range state["evidence"].([]any)[0].(map[string]any) {
		duplicate[key] = value
	}
	duplicate["path"] = ".claude/evidence/duplicate-result.json"
	state["evidence"] = append(state["evidence"].([]any), duplicate)
	read := func(path string) ([]byte, error) {
		data, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("missing %s", path)
		}
		return data, nil
	}
	if _, err := LoadAccepted(state, record, ref, read); err == nil {
		t.Fatal("duplicate registered evidence ID on a different path was accepted")
	}
}
