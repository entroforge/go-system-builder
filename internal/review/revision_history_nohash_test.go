package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOriginalPlanRevisionRetainsUnknownHistoryAndSourceReference(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, revisionSourceState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	plan, ptr, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	originalPlan := mustReadTestFile(t, filepath.Join(root, ptr.Path))
	history := map[string]any{"old": []any{nil, "historical"}}
	pointer := snap.State["review"].(map[string]any)["plan"].(map[string]any)
	pointer["sha256"] = history
	pointer["author_note"] = map[string]any{"sha256": "business description"}
	state, _ := json.Marshal(snap.State)
	if err := os.WriteFile(statePath, state, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("actual professional material"), 0600); err != nil {
		t.Fatal(err)
	}
	source := "path:source.txt#ordinary-section"
	revised := reviseFixturePlan(t, root, plan,
		map[string]any{"claim_id": "claim-qa-3", "lens": "qa", "target": "internal/example", "assertion": "new surface covered", "oracle": "observed", "method": "review", "applicability": "required"},
		map[string]any{"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []string{"claim-qa-3"}, "non_overlap_boundary": "owns new surface", "execution_wave": "static"}, []string{source})
	originalAnswer := mustReadTestFile(t, revised)
	next, err := RevisePlan(root, statePath, journalPath, ReviseRequest{ExpectedRevision: snap.Revision, PlanPath: revised, SourceRef: source, AffectedSurface: "internal/example"})
	if err != nil {
		t.Fatal(err)
	}
	current := next.State["review"].(map[string]any)["plan"].(map[string]any)
	if !reflect.DeepEqual(current["sha256"], history) || !reflect.DeepEqual(current["author_note"], pointer["author_note"]) {
		t.Fatal("revision changed unknown historical data")
	}
	if !bytes.Equal(mustReadTestFile(t, revised), originalAnswer) || !bytes.Equal(mustReadTestFile(t, filepath.Join(root, ptr.Path)), originalPlan) {
		t.Fatal("revision changed original professional files")
	}
	currentPlan, _, err := LoadPlan(root, next.State)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, claim := range currentPlan.Claims {
		for _, ref := range claim.SourceRefs {
			if ref == source {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("revision changed professional source reference")
	}
}
