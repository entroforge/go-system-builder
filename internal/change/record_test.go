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
	if record.WorkItems[0].Status != "open" {
		t.Fatalf("new work item status = %q, want open", record.WorkItems[0].Status)
	}
}

func TestRecordSummaryCountsOpenWorkAndChecks(t *testing.T) {
	record := Record{
		ID: "CHG-002", Summary: "docs", Class: "docs-only", Risk: "low",
		WorkItems: []WorkItem{
			{ID: "W-1", Status: "active"},
			{ID: "W-2", Status: "done"},
			{ID: "W-3", Status: "open"},
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
		WorkItems:      []WorkItem{{ID: "W-1", Text: "implement feature", Status: "open", Owner: "main"}},
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
