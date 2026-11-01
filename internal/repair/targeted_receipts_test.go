package repair

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetedReceiptFactsUseRecordedConclusion(t *testing.T) {
	root := t.TempDir()
	pointer := map[string]any{"targeted_reverification_artifacts": []any{
		map[string]any{"path": "unassociated.json"},
		map[string]any{"path": "result.json", "performing_assignment_id": "repair-assignment-verification-two", "result": "pass"},
	}}
	got, err := LoadTargetedReceiptFacts(context.Background(), root, pointer, "repair-assignment-verification-two", false)
	if err != nil || len(got.Rows) != 1 || got.Rows[0].Result != "pass" || len(got.Unknown) != 0 {
		t.Fatalf("recorded facts must not read reports: %+v err=%v", got, err)
	}
	got, err = LoadTargetedReceiptFacts(context.Background(), root, pointer, "repair-assignment-verification-two", true)
	if err != nil || len(got.Rows) != 1 || got.Rows[0].Result != "pass" || len(got.Unknown) != 1 {
		t.Fatalf("unavailable original impact must retain conclusion: %+v err=%v", got, err)
	}
}

func issueTargetedTask(t *testing.T, root, session, id, owner, status, event, taskID string) (map[string]any, map[string]any) {
	t.Helper()
	path := taskID + ".json"
	data, _ := json.Marshal(map[string]any{"task_id": taskID, "session_id": session, "assignment_id": id})
	if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
		t.Fatal(err)
	}
	task := map[string]any{"id": taskID, "path": path, "state": status, "last_lifecycle_event": event}
	agent := map[string]any{"id": owner, "task_ids": []any{taskID}}
	return task, agent
}

func TestTargetedResponsibilityActivityUsesActualCancelledTask(t *testing.T) {
	root := t.TempDir()
	session, id := "repair-session-1", "repair-assignment-verification-one"
	task, agent := issueTargetedTask(t, root, session, id, "qa", "cancelled", "task_cancelled", "TASK-900-001")
	state := map[string]any{"schema_version": "1.3.0", "review": map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{id: "qa"}}}, "entities": map[string]any{"tasks": []any{task}, "agents": []any{agent}}}
	if active, known := targetedResponsibilityActive(root, state, session, id); active || !known {
		t.Fatalf("cancelled should not be pending, active=%t known=%t", active, known)
	}
	state["entities"].(map[string]any)["tasks"].([]any)[0].(map[string]any)["state"] = "active"
	state["entities"].(map[string]any)["tasks"].([]any)[0].(map[string]any)["last_lifecycle_event"] = "builder_activated"
	if active, known := targetedResponsibilityActive(root, state, session, id); !active || !known {
		t.Fatalf("active responsibility must remain pending, active=%t known=%t", active, known)
	}
	if _, known := targetedResponsibilityActive(root, state, session, "repair-assignment-verification-other"); known {
		t.Fatal("unknown responsibility must not be invented")
	}
}

func TestTargetedMinimalFailurePreservesUnknownCauseAndObservations(t *testing.T) {
	root := t.TempDir()
	request := TargetedReverificationRequest{ReverificationID: "reverify-minimal", RuntimeID: "loop-1", CaseID: "case-1", BaselineGeneration: 1, OriginalAssignmentID: "repair-assignment-one", PerformingAssignmentID: "repair-assignment-verification-one", ImpactID: "impact-1", Result: "fail", AssertionResults: []AssertionResult{{AssertionID: "symptom-1"}}}
	value, ref, err := CreateTargetedReverification(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if value.Result != "fail" || value.FailureClass != "" || len(value.AssertionResults) != 1 || value.AssertionResults[0].Result != "" || value.AssertionResults[0].EvidenceRefs != nil {
		t.Fatalf("author conclusion was altered: %+v", value)
	}
	saved, err := ValidateTargetedReverification(root, ref)
	if err != nil || saved.FailureClass != "" || saved.Result != "fail" {
		t.Fatalf("saved original failure = %+v, %v", saved, err)
	}
	if strings.Contains(targetedFailureNextAction("case-1", "", ref), "same cause") {
		t.Fatal("failure action must not invent cause")
	}
}

func TestTargetedPendingResponsibilitiesRespectActualActivity(t *testing.T) {
	session := "repair-session-1"
	first, second, cancelled, unknown := "repair-assignment-verification-one", "repair-assignment-verification-two", "repair-assignment-verification-three", "repair-assignment-verification-four"
	root := t.TempDir()
	t1, a1 := issueTargetedTask(t, root, session, first, "qa1", "active", "builder_activated", "TASK-900-001")
	t2, a2 := issueTargetedTask(t, root, session, second, "qa2", "active", "builder_activated", "TASK-900-002")
	t3, a3 := issueTargetedTask(t, root, session, cancelled, "qa3", "cancelled", "task_cancelled", "TASK-900-003")
	state := map[string]any{"schema_version": "1.3.0", "review": map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{first: "qa1", second: "qa2", cancelled: "qa3", unknown: "qa4"}}}, "entities": map[string]any{"tasks": []any{t1, t2, t3}, "agents": []any{a1, a2, a3}}}

	pointer := map[string]any{"session_id": session}
	owners := map[string]string{first: "qa1", second: "qa2", cancelled: "qa3", unknown: "qa4"}
	conclusions := map[string]string{first: "pass"}
	missing, uncertain := targetedPendingResponsibilities(root, state, pointer, owners, conclusions, false)
	if len(missing) != 1 || missing[0] != second || !uncertain {
		t.Fatalf("active missing must wait, cancelled must not, absent must stay unknown: missing=%v unknown=%t", missing, uncertain)
	}
	missing, uncertain = targetedPendingResponsibilities(root, state, pointer, owners, conclusions, true)
	if len(missing) != 0 || !uncertain {
		t.Fatalf("unidentified prior receipt cannot be manufactured as missing or pass: missing=%v unknown=%t", missing, uncertain)
	}
}

func TestTargetedActivitySelectsOriginalTaskForSharedOwner(t *testing.T) {
	root := t.TempDir()
	session := "old-session"
	first := "repair-assignment-verification-first"
	second := "repair-assignment-verification-second"
	one, _ := issueTargetedTask(t, root, session, first, "qa", "active", "builder_activated", "TASK-900-041")
	two, _ := issueTargetedTask(t, root, session, second, "qa", "cancelled", "task_cancelled", "TASK-historical-opaque")
	state := map[string]any{
		"schema_version": "1.3.0",
		"review":         map[string]any{"repair": map[string]any{"assignment_owners": map[string]any{first: "qa", second: "qa"}}},
		"entities":       map[string]any{"tasks": []any{one, two}, "agents": []any{map[string]any{"id": "qa", "task_ids": []any{"TASK-900-041", "TASK-historical-opaque"}}}},
	}
	if active, known := targetedResponsibilityActive(root, state, session, first); !active || !known {
		t.Fatalf("first original responsibility: active=%v known=%v", active, known)
	}
	if active, known := targetedResponsibilityActive(root, state, session, second); active || !known {
		t.Fatalf("second original responsibility: active=%v known=%v", active, known)
	}
}
