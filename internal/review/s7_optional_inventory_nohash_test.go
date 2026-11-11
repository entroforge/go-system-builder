package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS7DraftUsesOriginalTaskWithoutHistoricalDigest(t *testing.T) {
	for _, old := range []any{nil, "incorrect-old", 42, map[string]any{"legacy": true}} {
		root, state, _, _ := knownCaseFixture(t)
		task := state["documents"].([]any)[0].(map[string]any)
		if old == nil {
			delete(task, "sha256")
		} else {
			task["sha256"] = old
		}
		delete(state["bound_req"].(map[string]any), "metadata")
		plan, notes := DraftPlanForRoot(root, state)
		if plan == nil {
			t.Fatalf("old SHA %#v prevented a draft: %v", old, notes)
		}
		found := false
		for _, claim := range plan.Claims {
			if strings.Contains(strings.Join(claim.SourceRefs, " "), "shareholder/cases.json") {
				found = true
			}
		}
		if !found {
			t.Fatalf("old SHA %#v lost readable original CASE: %v", old, notes)
		}
		for _, note := range notes {
			if strings.Contains(note, "must restore") || strings.Contains(note, "re-draft") {
				t.Fatalf("old SHA or missing UI metadata invented a prerequisite: %s", note)
			}
		}
	}
}

func TestS7DraftKeepsAvailableCasesWhenOptionalTaskMissing(t *testing.T) {
	root, state, _, _ := knownCaseFixture(t)
	task := state["documents"].([]any)[0].(map[string]any)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(task["path"].(string)))); err != nil {
		t.Fatal(err)
	}
	plan, notes := DraftPlanForRoot(root, state)
	if plan == nil {
		t.Fatalf("optional task unavailable prevented the draft: %v", notes)
	}
	found := false
	for _, claim := range plan.Claims {
		if strings.Contains(strings.Join(claim.SourceRefs, " "), "shareholder/cases.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing task erased CASE available from the REQ: %v", notes)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "unavailable") || strings.Contains(joined, "must restore") {
		t.Fatalf("missing optional source not described as unknown: %s", joined)
	}
}
