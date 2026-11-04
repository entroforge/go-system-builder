package repair_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

// revisionStyleEventID matches the former Runtime-write-count S9 identities.
var revisionStyleEventID = regexp.MustCompile(`^evt-s9-[a-z0-9-]+-r[0-9]+$`)

// repairJournalEventIDs counts the journal events per event id.
func repairJournalEventIDs(t *testing.T, journalPath string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if id, ok := event["event_id"].(string); ok {
			ids[id]++
		}
	}
	return ids
}

// TestS9EventIdentityNamesDomainCoordinates proves that the recorded S9 events
// name a domain coordinate and never a Runtime write count.
func TestS9EventIdentityNamesDomainCoordinates(t *testing.T) {
	_, _, journalPath, _ := overBudgetHandoffFixture(t)
	ids := repairJournalEventIDs(t, journalPath)
	for id := range ids {
		if revisionStyleEventID.MatchString(id) {
			t.Fatalf("S9 event id still carries a Runtime write count: %q", id)
		}
	}
	for _, prefix := range []string{
		"evt-s9-session-open-",
		"evt-s9-plan-",
		"evt-s9-execution-begin-",
		"evt-s9-impact-",
		"evt-s9-targeted-",
	} {
		found := 0
		for id := range ids {
			if strings.HasPrefix(id, prefix) {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("event domain %q was not written exactly once: %v", prefix, ids)
		}
	}
	// The session is the responsibility coordinate of the session, startup and
	// impact/targeted events.
	for id := range ids {
		if strings.HasPrefix(id, "evt-s9-session-open-") && id != "evt-s9-session-open-repair-session-budget" {
			t.Fatalf("session open event id does not name the session: %q", id)
		}
	}
}

// TestS9RepeatedImpactKeepsDistinctDomainIdentity proves that an impact that
// is reconciled again is a new domain artifact and therefore a new event
// identity, while the same artifact keeps one identity.
func TestS9RepeatedImpactKeepsDistinctDomainIdentity(t *testing.T) {
	root, statePath, journalPath, _ := overBudgetHandoffFixture(t)
	before := repairJournalEventIDs(t, journalPath)
	first := ""
	for id := range before {
		if strings.HasPrefix(id, "evt-s9-impact-") {
			first = id
		}
	}
	if first == "" {
		t.Fatalf("the recorded impact event is missing: %v", before)
	}
	_, secondRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{ImpactID: "impact-budget-again", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"}, ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api/payload.go"}}, Decisions: []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", ResponsibilityID: nil, Scope: []string{"internal/api/payload.go"}, Rationale: "the impact was reconciled again", RecoveryEvidence: []string{"test://red"}}}, EscalationLevel: "assignment", AnalyzedBy: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "main"}, Impact: secondRef}); err != nil {
		t.Fatalf("the reconciled impact was rejected: %v", err)
	}
	after := repairJournalEventIDs(t, journalPath)
	distinct := ""
	for id := range after {
		if strings.HasPrefix(id, "evt-s9-impact-") && id != first {
			distinct = id
		}
	}
	if distinct == "" {
		t.Fatalf("the reconciled impact did not keep its own domain identity: %v", after)
	}
	if !strings.HasSuffix(distinct, secondRef.Path) {
		t.Fatalf("the reconciled impact event id does not name its artifact: %q", distinct)
	}
	// G18 reverse assertion: the same artifact keeps one identity, so a
	// repeated record is deduplicated by the journal instead of writing a
	// second Runtime fact.
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "main"}, Impact: secondRef}); err == nil {
		t.Fatal("the repeated impact artifact wrote a second Runtime fact")
	}
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeState) != string(afterState) {
		t.Fatal("the repeated impact artifact mutated the Runtime state")
	}
	repeated := repairJournalEventIDs(t, journalPath)
	if repeated[distinct] != 1 {
		t.Fatalf("the repeated impact artifact wrote a second journal fact: %v", repeated)
	}
}
