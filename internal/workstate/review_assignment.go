package workstate

import "fmt"

var reviewEvents = map[string][]milestone{
	"review_plan_registered":    {{Pending, "planned"}},
	"workgroup_registered":      {{Pending, "planned"}, {Active, "dispatched"}},
	"review_result_submitted":   {{Done, "consumed"}, {Active, "dispatched"}},
	"review_assignment_blocked": {{Blocked, "blocked"}},
	"review_plan_revised":       {{Pending, "planned"}, {Active, "dispatched"}, {Blocked, "blocked"}, {Done, "consumed"}},
}

// ReviewAssignmentActivity is the compatibility seam for original S7
// Claim/Result consumers. The five-state field itself is not a Result proof.
func ReviewAssignmentActivity(row map[string]any, versions ...string) string {
	status, _ := row["status"].(string)
	event, _ := row["last_lifecycle_event"].(string)
	version := "1.2.0"
	if event != "" || status == string(Pending) || status == string(Active) || status == string(Done) || status == string(Cancelled) {
		version = UnifiedVersion
	}
	if len(versions) == 1 && versions[0] != "" {
		version = versions[0]
	}
	value, _ := LegacyReviewAssignmentActivity(version, status, event)
	return value
}

func LegacyReviewAssignmentActivity(version, status, event string) (string, error) {
	if version == "1.1.0" || version == "1.2.0" {
		switch status {
		case "queued", "ready", "pending":
			status = "planned"
		case "submitted", "awaiting_consumption":
			status = "result_submitted"
		}
	}
	return activity("review assignment", version, status, event, reviewEvents,
		[]string{"planned", "dispatched", "result_submitted", "consumed", "blocked"})
}

func RecordReviewAssignmentActivity(runtime, row map[string]any, legacyActivity, event string) error {
	if !Unified(runtime["schema_version"]) {
		row["status"] = legacyActivity
		return nil
	}
	for _, candidate := range reviewEvents[event] {
		if candidate.activity == legacyActivity {
			if row["status"] == string(Done) && candidate.state != Done && event != "review_plan_revised" {
				return fmt.Errorf("completed review responsibility requires the original plan revision")
			}
			row["status"] = string(candidate.state)
			row["last_lifecycle_event"] = event
			return nil
		}
	}
	return fmt.Errorf("review event %q cannot record activity %q", event, legacyActivity)
}
