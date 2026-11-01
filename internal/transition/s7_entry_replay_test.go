package transition

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
)

func TestS7EntryReplayUsesSavedOriginalRequest(t *testing.T) {
	for _, id := range []string{"TR-006", "TR-012", "TR-016", "TR-022", "TR-031"} {
		request := Request{TransitionID: id, ExpectedRevision: 9}
		entry := map[string]any{"transition_id": id, "plan_id": reviewmeta.NewPlanID("runtime", mutationEventID(id, 9))}
		state := map[string]any{"runtime_id": "runtime", "review": map[string]any{"round_entry": entry}}
		if !replayS7Entry(state, request) {
			t.Fatal("original committed entry not recognized")
		}
		request.ExpectedRevision = 10
		if replayS7Entry(state, request) {
			t.Fatal("new request recognized as old entry")
		}
		request.ExpectedRevision = -1
		if replayS7Entry(state, request) {
			t.Fatal("unidentified request recognized as replay")
		}
		request.ExpectedRevision = 9
		delete(entry, "plan_id")
		entry["round"] = 1
		if replayS7Entry(state, request) {
			t.Fatal("legacy number invented an entry identity")
		}
	}
}
