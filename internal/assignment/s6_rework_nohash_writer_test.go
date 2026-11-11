package assignment

import (
	"encoding/json"
	"errors"
	"github.com/entroforge/go-system-builder/internal/integration"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestS6RevisionAutomaticallyReusesPreviousResponsibility(t *testing.T) {
	fixture, statePath, journalPath, previous, _ := registerAgentBeginFixture(t, true)
	setRevisionManifest(t, fixture, "team-manifest-s6-registration-r2", "workgroup-s6-registration-r2", "platform-s6-r2", "agent-s6-registration-r2")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	rows := state["entities"].(map[string]any)["assignments"].([]any)
	for _, raw := range rows {
		row := raw.(map[string]any)
		for _, field := range []string{"task", "manifest", "agent_definition"} {
			if source, ok := row[field].(map[string]any); ok {
				source["sha256"] = "old-unverified-value"
			}
		}
		for _, source := range row["context_sources"].([]any) {
			source.(map[string]any)["sha256"] = "old-unverified-value"
		}
	}
	writeRegistrationJSON(t, statePath, state)
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, item := range manifest["documents"].([]any) {
		item.(map[string]any)["sha256"] = "legacy-unknown"
	}
	writeRegistrationJSON(t, manifestPath, manifest)
	makeRequest := func() Request {
		return Request{ExpectedRevision: -1, ManifestPath: manifestPath, TaskID: fixture.taskID, TaskPath: filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)), Revision: &RevisionRequest{AssignmentID: previous.AssignmentID}}
	}
	type result struct {
		revision int
		replayed bool
		intent   string
		err      error
	}
	out := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap, receipt, err := RegisterWithReceipt(fixture.root, statePath, journalPath, makeRequest())
			r := result{revision: snap.Revision, err: err}
			if receipt != nil {
				r.replayed = receipt.Replayed
				r.intent = receipt.IntentKey
			}
			out <- r
		}()
	}
	wg.Wait()
	close(out)
	var results []result
	for r := range out {
		results = append(results, r)
	}
	issued := 0
	for _, r := range results {
		if errors.Is(r.err, integration.ErrIntegrationBusy) {
			continue
		}
		if r.err != nil {
			t.Fatalf("concurrent original writer: %v", r.err)
		}
		if r.revision != 10 || r.intent != "s6-rework-r1" {
			t.Fatalf("unexpected receipt: %+v", r)
		}
		if !r.replayed {
			issued++
		}
	}
	if issued != 1 {
		t.Fatalf("expected one issuance: %+v", results)
	}
	// Busy means the short workspace lock was held. Its natural retry must
	// resolve to that same issued responsibility, without a second Writer.
	for _, r := range results {
		if errors.Is(r.err, integration.ErrIntegrationBusy) {
			snap, receipt, err := RegisterWithReceipt(fixture.root, statePath, journalPath, makeRequest())
			if err != nil || receipt == nil || !receipt.Replayed || snap.Revision != 10 {
				t.Fatalf("retry after concurrent lock: %+v %v", receipt, err)
			}
		}
	}
	// Once issued, retry requires neither re-reading TASK nor recreating a proof.
	if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef))); err != nil {
		t.Fatal(err)
	}
	snap, receipt, err := RegisterWithReceipt(fixture.root, statePath, journalPath, makeRequest())
	if err != nil || receipt == nil || !receipt.Replayed || snap.Revision != 10 {
		t.Fatalf("replay after source removed: %+v %+v %v", snap, receipt, err)
	}
}
