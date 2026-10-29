package workstate

import "fmt"

// UnifiedVersion is the first Runtime format whose work entities use five
// states. Older files retain their own format and immutable event history.
const UnifiedVersion = "1.3.0"

func SupportsAssignments(version any) bool {
	return version == "1.2.0" || version == UnifiedVersion
}

func Unified(version any) bool { return version == UnifiedVersion }

type milestone struct {
	state    Status
	activity string
}

type EntityProjection struct {
	Status   Status
	Activity string
}

func AgentProjection(row map[string]any, version string) (EntityProjection, error) {
	return entityProjection(row, version, true)
}

func TaskProjection(row map[string]any, version string) (EntityProjection, error) {
	return entityProjection(row, version, false)
}

func entityProjection(row map[string]any, version string, agent bool) (EntityProjection, error) {
	state, _ := row["state"].(string)
	event, _ := row["last_lifecycle_event"].(string)
	var current string
	var err error
	if agent {
		current, err = LegacyAgentActivity(version, state, event)
	} else {
		current, err = LegacyTaskActivity(version, state, event)
	}
	if err != nil {
		return EntityProjection{}, err
	}
	if version == UnifiedVersion {
		return EntityProjection{Status: Status(state), Activity: current}, nil
	}
	status := Pending
	switch current {
	case "done":
		status = Done
	case "blocked":
		status = Blocked
	case "stopped", "cancelled":
		status = Cancelled
	default:
		if agent && AgentStarted(current) || !agent && (current == "in_progress" || current == "review") {
			status = Active
		}
	}
	return EntityProjection{Status: status, Activity: current}, nil
}

var agentEvents = map[string][]milestone{
	"agent_created":                {{Pending, "spawned"}},
	"workgroup_registered":         {{Pending, "queued"}, {Active, "reading"}},
	"readback_started":             {{Active, "reading"}},
	"readback_submitted":           {{Active, "understanding_submitted"}},
	"understanding_approved":       {{Active, "understanding_approved"}},
	"understanding_rejected":       {{Active, "reading"}},
	"document_conflict_reported":   {{Blocked, "blocked"}},
	"activation_sent":              {{Active, "activated"}},
	"work_started":                 {{Active, "working"}},
	"completion_reported":          {{Active, "reported"}},
	"completion_acknowledged":      {{Done, "done"}},
	"work_blocked":                 {{Blocked, "blocked"}},
	"blocker_resolved":             {{Active, "working"}},
	"integration_rework_requested": {{Active, "working"}},
	"shutdown_approved":            {{Cancelled, "stopped"}},
	"assignment_revision_issued":   {{Cancelled, "stopped"}, {Active, "reading"}},
	"reviewer_queued":              {{Pending, "queued"}},
	"reviewer_dispatched":          {{Active, "reading"}},
}

var taskEvents = map[string][]milestone{
	"task_created":                 {{Pending, "candidate"}},
	"task_registered":              {{Pending, "candidate"}},
	"workgroup_registered":         {{Active, "in_progress"}, {Pending, "candidate"}},
	"internal_review_passed":       {{Pending, "reviewed"}},
	"document_pass_lock":           {{Pending, "locked"}},
	"builder_activated":            {{Active, "in_progress"}},
	"builder_reported":             {{Active, "review"}},
	"task_closing_contract_passed": {{Done, "done"}},
	"task_blocked":                 {{Blocked, "blocked"}},
	"task_resumed":                 {{Active, "locked"}},
	"task_cancelled":               {{Cancelled, "cancelled"}},
	"integration_rework_requested": {{Active, "in_progress"}},
	"assignment_revision_issued":   {{Pending, "in_progress"}},
}

// LegacyAgentActivity reconstructs the internal authorization milestone from
// the original event. It never turns active into permission to work. Legacy
// versions use their original state vocabulary only at this compatibility seam.
func LegacyAgentActivity(version, state, event string) (string, error) {
	return activity("agent", version, state, event, agentEvents, []string{
		"spawned", "queued", "reading", "understanding_submitted", "understanding_approved",
		"activated", "working", "reported", "done", "blocked", "stopped",
	})
}

func LegacyTaskActivity(version, state, event string) (string, error) {
	return activity("task", version, state, event, taskEvents, []string{
		"candidate", "reviewed", "locked", "in_progress", "review", "blocked", "done", "cancelled",
	})
}

func activity(kind, version, state, event string, events map[string][]milestone, legacy []string) (string, error) {
	if version == "1.1.0" || version == "1.2.0" {
		for _, value := range legacy {
			if state == value {
				return state, nil
			}
		}
		return "", fmt.Errorf("unknown legacy %s activity %q", kind, state)
	}
	if version != UnifiedVersion {
		return "", fmt.Errorf("unsupported work-state format %q", version)
	}
	for _, candidate := range events[event] {
		if state == string(candidate.state) {
			return candidate.activity, nil
		}
	}
	return "", fmt.Errorf("%s state %q has no matching original lifecycle event %q", kind, state, event)
}

// AgentActivity and TaskActivity are fail-closed conveniences for already
// validated entity rows. Missing or inconsistent canonical events return no
// authorization milestone. They are not current completion predicates.
func AgentActivity(row map[string]any) string { return rowActivity(row, true) }
func TaskActivity(row map[string]any) string  { return rowActivity(row, false) }

func rowActivity(row map[string]any, agent bool) string {
	state, _ := row["state"].(string)
	event, _ := row["last_lifecycle_event"].(string)
	version := "1.2.0"
	if event != "" || state == string(Pending) || state == string(Active) || state == string(Cancelled) && agent {
		version = UnifiedVersion
	}
	var value string
	if agent {
		value, _ = LegacyAgentActivity(version, state, event)
	} else {
		value, _ = LegacyTaskActivity(version, state, event)
	}
	return value
}

// RecordAgentActivity and RecordTaskActivity are called only inside the
// existing domain Writer mutation. The work state and original event are
// written together; old Runtime versions keep their original representation.
func RecordAgentActivity(runtime, row map[string]any, legacyActivity, event string) error {
	return recordActivity(runtime, row, legacyActivity, event, agentEvents)
}
func RecordTaskActivity(runtime, row map[string]any, legacyActivity, event string) error {
	return recordActivity(runtime, row, legacyActivity, event, taskEvents)
}

func recordActivity(runtime, row map[string]any, legacyActivity, event string, events map[string][]milestone) error {
	if !Unified(runtime["schema_version"]) {
		row["state"] = legacyActivity
		return nil
	}
	for _, candidate := range events[event] {
		if candidate.activity == legacyActivity {
			if row["last_lifecycle_event"] != nil && row["state"] == string(Done) &&
				candidate.state != Done && candidate.state != Cancelled && event != "assignment_revision_issued" {
				return fmt.Errorf("completed responsibility requires an original new-attempt or cancellation event")
			}
			row["state"] = string(candidate.state)
			row["last_lifecycle_event"] = event
			return nil
		}
	}
	return fmt.Errorf("lifecycle event %q cannot record activity %q", event, legacyActivity)
}
