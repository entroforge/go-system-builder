package workstate

import "testing"

func TestStatusUsesOnlyOriginalCurrentFacts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts Facts
		want  Status
	}{
		{"not started", Facts{}, Pending},
		{"started", Facts{Started: true}, Active},
		{"original consumer", Facts{OriginalConsumerAcceptedCompletion: true}, Done},
		{"blocker", Facts{Started: true, ExplicitBlocker: true}, Blocked},
		{"withdrawn", Facts{OriginalCancellationOrReplacement: true}, Cancelled},
		{"no stale completion over blocker", Facts{OriginalConsumerAcceptedCompletion: true, ExplicitBlocker: true}, Blocked},
		{"replacement revokes current completion", Facts{OriginalConsumerAcceptedCompletion: true, OriginalCancellationOrReplacement: true}, Cancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Project(tc.facts); got != tc.want || !Valid(got) {
				t.Fatalf("status=%q want=%q", got, tc.want)
			}
		})
	}
	for _, old := range []Status{"complete", "completed", "closed", "finished", "ready", "reported", "accepted"} {
		if Valid(old) {
			t.Fatalf("legacy or non-progress fact admitted as work status: %q", old)
		}
	}
}

func TestCanonicalActivityRequiresMatchingOriginalEvent(t *testing.T) {
	for kind, events := range map[string]map[string][]milestone{"agent": agentEvents, "task": taskEvents} {
		for event, rows := range events {
			for _, row := range rows {
				t.Run(kind+"/"+event+"/"+string(row.state), func(t *testing.T) {
					entity := map[string]any{}
					runtime := map[string]any{"schema_version": UnifiedVersion}
					var err error
					var projection EntityProjection
					if kind == "agent" {
						err = RecordAgentActivity(runtime, entity, row.activity, event)
						if err == nil {
							projection, err = AgentProjection(entity, UnifiedVersion)
						}
					} else {
						err = RecordTaskActivity(runtime, entity, row.activity, event)
						if err == nil {
							projection, err = TaskProjection(entity, UnifiedVersion)
						}
					}
					if err != nil || projection.Status != row.state || projection.Activity != row.activity {
						t.Fatalf("projection=%+v err=%v expected=%+v", projection, err, row)
					}
				})
			}
		}
	}
	for _, event := range []string{"", "invented_event", "completion_acknowledged"} {
		if _, err := LegacyAgentActivity(UnifiedVersion, string(Active), event); err == nil {
			t.Fatalf("active improperly accepted original event %q", event)
		}
	}
	if activity := AgentActivity(map[string]any{"state": "active"}); activity != "" {
		t.Fatalf("active without event became authorization activity %q", activity)
	}
	if activity := AgentActivity(map[string]any{"state": "active", "last_lifecycle_event": "activation_sent"}); activity != "activated" {
		t.Fatalf("activation was mistaken for started work: %q", activity)
	}
}

func TestOldRuntimeActivityCompatibilityDoesNotRewriteItsFormat(t *testing.T) {
	for _, version := range []string{"1.1.0", "1.2.0"} {
		row := map[string]any{"state": "reading"}
		if err := RecordAgentActivity(map[string]any{"schema_version": version}, row, "working", "work_started"); err != nil {
			t.Fatal(err)
		}
		if row["state"] != "working" || row["last_lifecycle_event"] != nil {
			t.Fatalf("old representation silently migrated: %+v", row)
		}
		projection, err := AgentProjection(row, version)
		if err != nil || projection.Status != Active || projection.Activity != "working" {
			t.Fatalf("%+v %v", projection, err)
		}
	}
	if _, err := LegacyAgentActivity("1.0.0", "working", ""); err == nil {
		t.Fatal("unsupported old Runtime version was guessed")
	}
}

func TestCompletedAttemptRequiresOriginalRevisionOrCancellation(t *testing.T) {
	runtime := map[string]any{"schema_version": UnifiedVersion}
	row := map[string]any{"state": "done", "last_lifecycle_event": "completion_acknowledged"}
	if err := RecordAgentActivity(runtime, row, "working", "work_started"); err == nil {
		t.Fatal("old completed attempt reopened")
	}
	if err := RecordAgentActivity(runtime, row, "stopped", "assignment_revision_issued"); err != nil {
		t.Fatal(err)
	}
	if row["state"] != "cancelled" {
		t.Fatal(row)
	}
	task := map[string]any{"state": "done", "last_lifecycle_event": "task_closing_contract_passed"}
	if err := RecordTaskActivity(runtime, task, "in_progress", "assignment_revision_issued"); err != nil {
		t.Fatal(err)
	}
	if task["state"] != "pending" {
		t.Fatalf("new task responsibility did not start pending: %+v", task)
	}
}
