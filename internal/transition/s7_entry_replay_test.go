package transition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
)

// writeS7EntryJournal saves one committed S7 entry transition exactly as the
// Writer would have written it, so the replay check reads a saved identity
// instead of recomputing one (G18 §1.4).
func writeS7EntryJournal(t *testing.T, eventID, transitionID string, beforeRevision int) (string, map[string]any) {
	t.Helper()
	journalPath := filepath.Join(t.TempDir(), "loop-events.jsonl")
	event := map[string]any{
		"schema_version":          "1.0.0",
		"runtime_id":              "loop-test",
		"event_id":                eventID,
		"idempotency_key":         "runtime:" + strings.ToLower(transitionID) + ":loop-test:saved-intent",
		"sequence":                1,
		"event":                   "transition_committed",
		"outcome":                 "committed",
		"actor":                   map[string]any{"type": "orchestrator", "id": "orchestrator"},
		"request_id":              "control-test",
		"baseline_generation":     1,
		"before_revision":         beforeRevision,
		"after_revision":          beforeRevision + 1,
		"from":                    map[string]any{"state": "building", "phase": nil},
		"to":                      map[string]any{"state": "verification", "phase": "planned"},
		"evidence_ids":            []string{},
		"message":                 "S7 entry committed.",
		"occurred_at":             "2026-10-25T00:00:00Z",
		"transition_id":           transitionID,
		"gate_id":                 "MANUAL",
		"producer_responsibility": "Orchestrator",
		"guard_results":           []any{},
		"action_results":          []any{},
	}
	line, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"runtime_id": "loop-test",
		"revision":   beforeRevision + 1,
		"journal":    map[string]any{"last_sequence": 1, "last_event_id": eventID},
		"review": map[string]any{"round_entry": map[string]any{
			"transition_id": transitionID,
			"plan_id":       reviewmeta.NewPlanID("loop-test", eventID),
		}},
	}
	return journalPath, state
}

func TestS7EntryReplayUsesSavedOriginalRequest(t *testing.T) {
	for _, id := range []string{"TR-006", "TR-012", "TR-016", "TR-022", "TR-031"} {
		eventID := "evt-" + strings.ToLower(id) + "-saved-entry"
		journalPath, state := writeS7EntryJournal(t, eventID, id, 9)
		request := Request{TransitionID: id, ExpectedRevision: 9}
		if !replayS7Entry(state, request, journalPath) {
			t.Fatal("original committed entry not recognized")
		}
		request.ExpectedRevision = 10
		if replayS7Entry(state, request, journalPath) {
			t.Fatal("new request recognized as old entry")
		}
		request.ExpectedRevision = -1
		if replayS7Entry(state, request, journalPath) {
			t.Fatal("unidentified request recognized as replay")
		}
		request.ExpectedRevision = 9
		entry := state["review"].(map[string]any)["round_entry"].(map[string]any)
		delete(entry, "plan_id")
		entry["round"] = 1
		if replayS7Entry(state, request, journalPath) {
			t.Fatal("legacy number invented an entry identity")
		}
	}
}

// A historical S7 entry keeps its original event id shape. The replay check
// compares saved values, so an entry committed by an earlier version is still
// recognized (G18 §1.4).
func TestS7EntryReplayAcceptsHistoricalEventIDShape(t *testing.T) {
	journalPath, state := writeS7EntryJournal(t, "evt-tr-012-r10", "TR-012", 9)
	if !replayS7Entry(state, Request{TransitionID: "TR-012", ExpectedRevision: 9}, journalPath) {
		t.Fatal("historical committed entry not recognized")
	}
	// A saved entry of another transition is never this request's replay.
	otherPath, otherState := writeS7EntryJournal(t, "evt-tr-006-r10", "TR-006", 9)
	if replayS7Entry(otherState, Request{TransitionID: "TR-012", ExpectedRevision: 9}, otherPath) {
		t.Fatal("another transition's entry recognized as this replay")
	}
}

// The committed transition identity names the responsibility and the intent,
// never the Runtime write count (G18 §1.1, §1.3).
func TestMutationEventIdentityNamesTheIntentNotTheWriteCount(t *testing.T) {
	from := map[string]any{"state": "bug_resolution", "phase": "ready_for_full_review"}
	// The same intent retried produces the same event id and the same
	// idempotency key, so the journal deduplicates the retry (G18 §1.3).
	eventID, key := mutationEventIdentity("TR-012", "loop-test", 1, from, 12)
	retryEventID, retryKey := mutationEventIdentity("TR-012", "loop-test", 1, from, 12)
	if eventID != retryEventID || key != retryKey {
		t.Fatalf("the same intent must keep one identity: %q/%q vs %q/%q", eventID, key, retryEventID, retryKey)
	}
	// A later S7 round inside the same responsibility is a different intent and
	// must not collide with the previous round (G18 §1.2).
	roundEventID, roundKey := mutationEventIdentity("TR-012", "loop-test", 1, from, 21)
	if roundEventID == eventID || roundKey == key {
		t.Fatal("a later round collided with the previous round identity")
	}
	// Two responsibilities never share an idempotency key (G18 §1.3). The
	// event id itself is scoped to one Runtime journal, so the responsibility
	// coordinate lives in the key.
	_, otherKey := mutationEventIdentity("TR-012", "loop-other", 1, from, 12)
	if otherKey == key {
		t.Fatal("two responsibilities share one idempotency key")
	}
	// The legacy revision-derived suffix must not come back.
	legacy := regexp.MustCompile(`-r\d+$`)
	if legacy.MatchString(eventID) || legacy.MatchString(key) {
		t.Fatalf("the identity still carries a Runtime write count: %q / %q", eventID, key)
	}
}
