package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// The Runtime index is already read. A spent request may project its original
// paths and registered reference, but cannot open acceptance documents or body
// files merely to turn unknown into an acceptance/completion conclusion.
func TestS9HandoffSpentProgressPreservesIndexesWithoutNewDocumentReads(t *testing.T) {
	root, _, e, packet, _ := s9NativeIntegrationCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(watch)
	paths := []string{filepath.Join(root, e.DeliveryRef)}
	for _, ref := range repairArtifactRefs(repairPointerCLI(snap.State), "plan_report_refs", "plan_report_ref", "plan_report_sha256") {
		paths = append(paths, filepath.Join(root, ref.Path))
	}
	for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			t.Fatal(err)
		}
		if external {
			paths = append(paths, filepath.Join(root, ref.Path))
		}
	}
	for _, path := range paths {
		if _, err := syscall.InotifyAddWatch(watch, path, syscall.IN_OPEN); err != nil {
			t.Fatal(err)
		}
	}
	before := repairDispatchRuntimeBytes(t, root)
	bodyReads := 0
	parent := packetbody.WithResolver(t.Context(), func(context.Context, packetbody.Reference) ([]byte, error) {
		bodyReads++
		return nil, errors.New("spent progress must not read packet bodies")
	})
	canceled, cancel := context.WithCancel(parent)
	cancel()
	expired, expire := context.WithDeadline(parent, time.Now().Add(-time.Second))
	defer expire()
	for _, ctx := range []context.Context{canceled, expired} {
		request := policy.Input{Context: ctx, Event: "PreToolUse", AgentID: "Main", CWD: root}
		text := s9QuestionnaireContext(ctx, root, request, nil, snap, "Main", true)
		for _, want := range []string{filepath.Join(e.Path, packet.AnswerPath), "acceptance and professional completion are unknown", "Registered candidate reference", filepath.Join(root, e.DeliveryRef)} {
			if !strings.Contains(text, want) {
				t.Errorf("spent progress lost %q: %s", want, text)
			}
		}
		for _, wrong := range []string{"Original acceptance source is unavailable", "Main next runs runtime workspace integrate", "No original PlanReport acceptance is recorded", "Fill only the issued professional answer"} {
			if strings.Contains(text, wrong) {
				t.Errorf("spent progress inferred %q: %s", wrong, text)
			}
		}
		projected := s9HandoffForBinding(ctx, root, snap, packet.Binding.Sources)
		if projected.Result == nil || projected.Result.id != packet.ID || projected.RegisteredRef != e.DeliveryRef || projected.PlanKnown || projected.PlanAccepted {
			t.Errorf("spent projection lost issued Result or guessed Plan receipt: %+v", projected)
		}
		readOrder := strings.Join(s9QuestionnaireReadOrder(root, snap.State, request), "\n")
		if !strings.Contains(readOrder, filepath.Join(root, e.DeliveryRef)) {
			t.Errorf("spent read-order lost registered source: %s", readOrder)
		}
	}
	var events [8192]byte
	n, err := syscall.Read(watch, events[:])
	if err != nil && err != syscall.EAGAIN {
		t.Fatal(err)
	}
	if n > 0 || bodyReads != 0 {
		t.Fatalf("spent request started document/body reads: inotify=%d resolver=%d", n, bodyReads)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9HandoffSpentEmptyReceptionIndexDoesNotInventMissingSources(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fact, err := s9ListenerAcceptanceContext(ctx, f.root, f.state, f.packet)
	if err != nil || fact.unknown || fact.kind != "" {
		t.Fatalf("pure absent references became an I/O failure: %+v %v", fact, err)
	}
	text := s9QuestionnaireContext(ctx, f.root, policy.Input{Context: ctx, Event: "PreToolUse", AgentID: "Main"}, nil, repairDispatchSnapshot(t, f.root), "Main", true)
	if !strings.Contains(text, filepath.Join(f.worker, f.packet.answer)) || strings.Contains(text, "source is unavailable") || strings.Contains(text, "context canceled") {
		t.Fatalf("pure registered question lost known path: %s", text)
	}
	readOrder := strings.Join(s9QuestionnaireReadOrder(f.root, f.state, policy.Input{Context: ctx, Event: "PreToolUse", AgentID: "Main"}), "\n")
	if !strings.Contains(readOrder, filepath.Join(f.worker, f.packet.answer)) {
		t.Fatalf("spent read-order erased pure registered question: %s", readOrder)
	}
}
