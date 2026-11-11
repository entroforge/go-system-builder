package assignment_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

// G18 §1/§3.2: a workgroup registration is named by the registered workgroup
// and the manifest that declares it, so its event id carries no Runtime write
// counter. The journal's global event-id uniqueness check therefore runs
// before the Apply closure that carries the domain refusal, and a repeated
// registration of the same manifest would answer with the journal's key
// collision instead of the domain's own outcome.
//
// A repeated registration must keep its original observable outcome: the
// domain refuses it (a plan Assignment dispatches once), never the journal
// collision, and the repeat writes no Runtime state and no journal fact.
func TestRegisterWorkgroupRepeatKeepsDomainOutcomeAndWritesNothing(t *testing.T) {
	root := assignmentTestRoot(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 6)
	seedReviewPlan(t, root, dir, state)
	writeJSON(t, statePath, state)

	manifestPath := filepath.Join(dir, "manifest.json")
	copyManifest(t, root, manifestPath)
	taskPath := filepath.Join(dir, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := assignment.Request{
		ExpectedRevision: 6,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-012",
		TaskPath:         taskPath,
	}
	first, err := assignment.Register(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("original workgroup registration: %v", err)
	}
	if first.Revision != 7 {
		t.Fatalf("original registration committed revision %d, want 7", first.Revision)
	}

	// The repeat names the Runtime revision the caller now holds. The
	// registration is still refused, and the refusal is the domain's own: the
	// plan Assignment dispatched once, so the second registration of the same
	// manifest has nothing left to dispatch.
	request.ExpectedRevision = 7
	_, err = assignment.Register(root, statePath, journalPath, request)
	if err == nil {
		t.Fatal("repeated workgroup registration must be refused")
	}
	if !strings.Contains(err.Error(), "already dispatched") {
		t.Fatalf("repeated workgroup registration must keep the domain outcome, got %v", err)
	}
	if strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("the journal event-id collision shadowed the domain outcome: %v", err)
	}

	// The repeat wrote nothing: one workgroup, one revision, one journal event.
	after := readRuntimeState(t, statePath)
	entities := after["entities"].(map[string]any)
	if got := len(entities["teams"].([]any)); got != 1 {
		t.Fatalf("repeated registration recorded %d workgroups, want 1", got)
	}
	if revision, _ := after["revision"].(float64); int(revision) != 7 {
		t.Fatalf("repeated registration moved the Runtime revision to %v, want 7", revision)
	}
	events := readJournalEvents(t, journalPath)
	registrations := 0
	for _, event := range events {
		if event["transition_id"] != "ENTITY-REGISTER" {
			continue
		}
		registrations++
		eventID, _ := event["event_id"].(string)
		if eventID != "evt-register-workgroup-delivery-round-1" {
			t.Fatalf("workgroup registration event id = %q, want evt-register-workgroup-delivery-round-1", eventID)
		}
		if strings.Contains(eventID, "-r8") {
			t.Fatalf("workgroup registration identity leaked the Runtime write counter: %v", event)
		}
	}
	if registrations != 1 {
		t.Fatalf("journal recorded %d workgroup registrations, want 1", registrations)
	}
}
