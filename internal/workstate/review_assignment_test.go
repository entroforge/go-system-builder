package workstate

import "testing"

func TestReviewAssignmentActivityUsesOriginalEventsAndLegacyValues(t *testing.T) {
	runtime := map[string]any{"schema_version": UnifiedVersion}
	for _, tc := range []struct {
		event    string
		activity string
		status   Status
	}{
		{"review_plan_registered", "planned", Pending},
		{"workgroup_registered", "planned", Pending},
		{"workgroup_registered", "dispatched", Active},
		{"review_result_submitted", "dispatched", Active},
		{"review_result_submitted", "consumed", Done},
		{"review_assignment_blocked", "blocked", Blocked},
		{"review_plan_revised", "planned", Pending},
		{"review_plan_revised", "dispatched", Active},
		{"review_plan_revised", "blocked", Blocked},
		{"review_plan_revised", "consumed", Done},
	} {
		row := map[string]any{}
		if err := RecordReviewAssignmentActivity(runtime, row, tc.activity, tc.event); err != nil {
			t.Fatalf("record %s/%s: %v", tc.event, tc.activity, err)
		}
		if got := ReviewAssignmentActivity(row); got != tc.activity || row["status"] != string(tc.status) {
			t.Fatalf("%s/%s projected activity/status %q/%v", tc.event, tc.activity, got, row["status"])
		}
	}
	if got := ReviewAssignmentActivity(map[string]any{"status": "result_submitted"}); got != "result_submitted" {
		t.Fatalf("legacy result_submitted status projected as %q", got)
	}
	if got := ReviewAssignmentActivity(map[string]any{"status": "active", "last_lifecycle_event": "review_plan_registered"}); got != "" {
		t.Fatalf("mismatched canonical status/event projected as %q", got)
	}
}
