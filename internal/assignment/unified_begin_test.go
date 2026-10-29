package assignment

import "testing"

func TestUnifiedAgentBeginReturnsFiveStateAndKeepsOriginalActivation(t *testing.T) {
	fixture, statePath, journalPath, record, _ := registerAgentBeginConfiguredFixtureVersion(t, true, nil, "1.3.0")
	planPath := writeThinS6Plan(t, fixture, record, "msg-unified-plan-begin", "corr-unified-plan-begin")
	snapshot, outcome, err := AgentBegin(fixture.root, statePath, journalPath, AgentBeginRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, PlanPath: planPath,
	})
	if err != nil || !outcome.Chained || outcome.FinalState != "active" {
		t.Fatalf("AgentBegin outcome=%+v err=%v", outcome, err)
	}
	agent := registrationAgent(snapshot.State, record.OwnerAgentID)
	if agent["state"] != "active" || agent["last_lifecycle_event"] != "work_started" || agent["activation_ref"] == "" {
		t.Fatalf("five-state status lost original activation: %+v", agent)
	}
}
