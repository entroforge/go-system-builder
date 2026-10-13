package e2erun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failCaptureSink struct{}

func (failCaptureSink) Write([]byte) (int, error) { return 0, errors.New("fixture fsync failure") }

func TestCaptureWriteFailureKeepsObservedBytesAndCancelsInvocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	buffer := &limitedBuffer{cancel: cancel, sink: failCaptureSink{}}
	data := []byte(`{"type":"test_end","status":"failed"}`)
	if _, err := buffer.Write(data); err == nil || ctx.Err() == nil || buffer.String() != string(data) {
		t.Fatalf("capture error lost its first observed failure or continued execution: %q %v", buffer.String(), err)
	}
}

func capturedFixture(t *testing.T) (string, FormalRequest) {
	t.Helper()
	root := t.TempDir()
	view, profile := testFormalView(t, root, fixtureProfile())
	return root, FormalRequest{Files: view, ProfileRef: profile, Mode: "collection", Binding: Binding{RuntimeID: "loop-capture", Generation: 1, Round: 1, SourceCommit: view.Commit, SubjectDigest: strings.Repeat("a", 64)}}
}

func TestCapturePreservesRawFailuresWithoutGrantingExecutionAuthority(t *testing.T) {
	root, request := capturedFixture(t)
	capture, err := PrepareCapture(context.Background(), root, request)
	if err != nil {
		t.Fatal(err)
	}
	intent := capture.IntentBytes()
	// Storage-boundary test, not a claim of actual browser execution. The
	// separate integration test sends real subprocess events into this sink.
	events := []byte("{\"type\":\"test_end\",\"status\":\"failed\"}\n{partial")
	if _, err := capture.events.Write(events); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.output.Write([]byte("first failure details")); err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	// A saved or caller-authored PASS file is deliberately not a replay source.
	if err := os.WriteFile(filepath.Join(root, capture.rel, "returned-observation.json"), []byte(`{"outcome":"pass","complete":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	interrupted, err := InspectInterruptedCapture(root, intent)
	if err != nil || interrupted.Outcome != "unknown" || interrupted.RawEvents != string(events) || interrupted.Output != "first failure details" {
		t.Fatalf("lost original partial failure or manufactured PASS: %+v %v", interrupted, err)
	}
	if !bytes.Equal(intent, capture.IntentBytes()) {
		t.Fatal("intent changed after execution material arrived")
	}
	if _, err := capture.Run(context.Background()); err == nil {
		t.Fatal("closed capture ran again")
	}
	path := filepath.Join(root, capture.rel, "intent.json")
	if err := os.WriteFile(path, []byte(`{"run_id":"changed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectInterruptedCapture(root, intent); err == nil {
		t.Fatal("capture detached from its registered intent")
	}
	if _, err := os.Stat(filepath.Join(root, capture.rel, "events.jsonl")); err != nil {
		t.Fatal("rejected inspection removed recovery material")
	}
}

func TestCapturedAttemptIsSingleUseAndKeepsPreparationFailure(t *testing.T) {
	root, request := capturedFixture(t)
	capture, err := PrepareCapture(context.Background(), root, request)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	observed, err := capture.Run(context.Background())
	if err == nil {
		t.Fatal("missing tool profile unexpectedly ran")
	}
	r, ok := observed.Receipt()
	if !ok || r.RunID != capture.intent.RunID || r.Complete || r.Outcome != "unknown" {
		t.Fatalf("preparation attempt lost identity/failure: %s", observed.Bytes())
	}
	if _, err := capture.Run(context.Background()); err == nil {
		t.Fatal("retry silently became a second attempt")
	}
	data, err := os.ReadFile(filepath.Join(root, capture.rel, "returned-observation.json"))
	if err != nil || !bytes.Equal(data, observed.Bytes()) {
		t.Fatalf("returned observation was not preserved: %v", err)
	}
}

func TestCaptureRejectsSymlinkAndWrongIntentWithoutWritingOutsideRoot(t *testing.T) {
	root, request := capturedFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCapture(context.Background(), root, request); err == nil {
		t.Fatal("capture followed a control-plane symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("capture wrote outside authority: %v %v", entries, err)
	}
	for _, id := range []string{"../escape", "e2e-run-../../escape", "e2e-run-" + strings.Repeat("0", 31)} {
		data, _ := json.Marshal(CaptureIntent{Version: "1.0.0", RunID: id})
		if _, err := InspectInterruptedCapture(root, data); err == nil {
			t.Fatalf("unsafe capture identity accepted: %s", id)
		}
	}
}

func TestCaptureRejectsAmbiguousRequestsBeforeCreatingRecoveryInputs(t *testing.T) {
	for _, mode := range []string{"collection-with-selection", "collection-with-receipt", "execution-without-collection", "bad-subject"} {
		t.Run(mode, func(t *testing.T) {
			root, request := capturedFixture(t)
			switch mode {
			case "collection-with-selection":
				request.SelectedTestIDs = []string{"not-used"}
			case "collection-with-receipt":
				request.Collection = &Receipt{}
			case "execution-without-collection":
				request.Mode = "execution"
			case "bad-subject":
				request.Binding.SubjectDigest = strings.Repeat("z", 64)
			}
			if _, err := PrepareCapture(context.Background(), root, request); err == nil {
				t.Fatal("invalid request accepted")
			}
			if _, err := os.Stat(filepath.Join(root, captureBase)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid request created recovery files: %v", err)
			}
		})
	}
}

func TestCaptureFreezesSourceViewAndIntentBytes(t *testing.T) {
	root, request := capturedFixture(t)
	capture, err := PrepareCapture(context.Background(), root, request)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	request.Files.Root, request.Files.Ref, request.Files.Commit = "/changed", "refs/heads/changed", "changed"
	data := capture.IntentBytes()
	data[0] = '!'
	if capture.request.Files.Root != root || capture.request.Files.Commit != capture.intent.Binding.SourceCommit || !json.Valid(capture.IntentBytes()) {
		t.Fatal("caller mutated the committed request identity")
	}
	var bad map[string]any
	if err := json.Unmarshal(capture.IntentBytes(), &bad); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"prepared_at", "mode", "unexpected"} {
		copy := map[string]any{}
		for k, v := range bad {
			copy[k] = v
		}
		copy[key] = "invalid"
		encoded, _ := json.Marshal(copy)
		if _, err := DecodeCaptureIntent(encoded); err == nil {
			t.Fatalf("invalid intent %s accepted", key)
		}
	}
}

func TestExecutionCaptureFreezesAndChecksItsSelectionContract(t *testing.T) {
	root, request := capturedFixture(t)
	test := fixtureTest()
	hash := strings.Repeat("b", 64)
	// Synthetic collection for validation only; no Observation is constructed
	// and no claim of real runner provenance is made by this fixture.
	collection := Receipt{Version: "1.0.0", RunID: "e2e-run-" + strings.Repeat("c", 32), Mode: "collection", Binding: request.Binding, ProfileRef: &request.ProfileRef, ProfileSHA256: hash, InputSHA256: hash, Inputs: []FileDigest{{Path: test.Path, SHA256: hash}}, Tools: map[string]string{}, Command: []string{"fixture"}, AttemptedAt: "2026-10-04T00:00:00Z", StartedAt: "2026-10-04T00:00:00Z", Stage: "completed", Invoked: true, Tests: []Test{}, Attempts: []Attempt{}, Problems: []string{}, RawEvents: testEvents(t, []Test{test}, map[string]any{"type": "end", "status": "passed"})}
	for _, tool := range []string{"node", "modules", "browsers", "system", "isolator", "reporter"} {
		collection.Tools[tool] = hash
	}
	parseEvents(&collection, nil)
	request.Mode, request.Collection, request.SelectedTestIDs = "execution", &collection, []string{test.ID}
	for _, selection := range [][]string{{test.ID, test.ID}, {"foreign"}, nil} {
		bad := request
		bad.SelectedTestIDs = selection
		if _, err := PrepareCapture(context.Background(), root, bad); err == nil {
			t.Fatalf("invalid selection accepted: %v", selection)
		}
	}
	capture, err := PrepareCapture(context.Background(), root, request)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	request.SelectedTestIDs[0] = "replaced"
	collection.Tests[0].ID = "replaced"
	collection.ProfileRef.SHA256 = "replaced"
	if capture.request.SelectedTestIDs[0] != test.ID || capture.request.Collection.Tests[0].ID != test.ID || capture.request.Collection.ProfileRef.SHA256 != capture.intent.ProfileRef.SHA256 {
		t.Fatal("caller altered the prepared selection contract")
	}
}
