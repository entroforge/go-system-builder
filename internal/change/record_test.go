package change

import "testing"

func TestBuildRecordCreatesTriggeredChecksForBugfix(t *testing.T) {
	record, err := BuildRecord(Input{
		ID:      "CHG-001",
		REQRef:  "REQ-002",
		REQSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Summary: "fix timeout mapping",
		Class:   "bugfix",
		Risk:    "medium",
		Scope:   Scope{Include: []string{"internal/client/**"}},
		WorkItems: []WorkItem{{
			ID:         "W-1",
			Text:       "reproduce and correct timeout mapping",
			Owner:      "main",
			WritePaths: []string{"internal/client/**"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildRecord() error = %v", err)
	}
	if len(record.RequiredChecks) != 3 {
		t.Fatalf("expected reproduce, regression and affected checks, got %#v", record.RequiredChecks)
	}
	if record.RequiredChecks[0].ID != "CK-1" || record.RequiredChecks[0].Kind != "reproduction" {
		t.Fatalf("unexpected first check: %#v", record.RequiredChecks[0])
	}
	if record.WorkItems[0].Status != "pending" {
		t.Fatalf("new work item status = %q, want pending", record.WorkItems[0].Status)
	}
}

func TestRecordSummaryCountsOpenWorkAndChecks(t *testing.T) {
	record := Record{
		ID: "CHG-002", Summary: "docs", Class: "docs-only", Risk: "low",
		WorkItems: []WorkItem{
			{ID: "W-1", Status: "active"},
			{ID: "W-2", Status: "done"},
			{ID: "W-3", Status: "pending"},
		},
		RequiredChecks: []Check{
			{ID: "CK-1", Status: "passed"},
			{ID: "CK-2", Status: "open"},
			{ID: "CK-3", Status: "failed"},
		},
	}
	summary := Summarize(record)
	if summary.WorkItems != (Counts{Active: 1, Open: 1, Done: 1}) {
		t.Fatalf("work item summary = %#v", summary.WorkItems)
	}
	if summary.Checks != (CheckCounts{Passed: 1, Open: 1, Failed: 1}) {
		t.Fatalf("check summary = %#v", summary.Checks)
	}
}

func TestNextStepReturnsReadyWorkBeforeItsChecks(t *testing.T) {
	record := Record{
		ID: "CHG-003", Summary: "feature", Class: "behavior feature", Risk: "low",
		WorkItems:      []WorkItem{{ID: "W-1", Text: "implement feature", Status: "pending", Owner: "main"}},
		RequiredChecks: []Check{{ID: "CK-1", Kind: "acceptance_test", Status: "open"}},
	}
	next := NextStep(record)
	if next.WorkItemID != "W-1" || next.CheckID != "CK-1" {
		t.Fatalf("next step = %#v", next)
	}
	if next.Action != "implement W-1 and run CK-1" {
		t.Fatalf("next action = %q", next.Action)
	}
}

func TestDecodeLegacyOpenWorkItemProjectsToPendingAndEncodeWritesPending(t *testing.T) {
	legacy := map[string]any{
		"id":         "CHG-004",
		"work_items": []any{map[string]any{"id": "W-1", "text": "implement feature", "status": "open"}},
	}
	record, err := Decode(legacy)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if record.WorkItems[0].Status != "pending" {
		t.Fatalf("decoded legacy work status = %q, want pending", record.WorkItems[0].Status)
	}
	encoded, err := Encode(record)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	items := encoded["work_items"].([]any)
	if got := items[0].(map[string]any)["status"]; got != "pending" {
		t.Fatalf("encoded work status = %v, want pending", got)
	}
}
