package review

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The existing issuer reserves paths before its Runtime transaction. A stop in
// that window leaves unknown files, never a dispatchable original responsibility.
func TestS7OriginalIssuerContinuesAfterUnregisteredReservation(t *testing.T) {
	root, statePath, journalPath, before, _ := packetModeFixture(t)
	plan, ptr, err := loadResultPacketPlan(root, before.State, false)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := buildCurrentResultPacket(root, before.State, plan, ptr, findPlanAssignment(plan, "assignment-qa-1"), "agent-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := reserveReviewResultDelivery(context.Background(), root, root, packet)
	if err != nil {
		t.Fatal(err)
	}
	originalAnswer, err := os.ReadFile(filepath.Join(root, reserved.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	originalView, err := os.ReadFile(filepath.Join(root, reserved.ViewPath))
	if err != nil {
		t.Fatal(err)
	}
	current, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := loopruntime.FindReviewResultPacketContext(loopruntime.WithPacketBodyRoot(context.Background(), root), current.State, reserved.ID); err != nil || found {
		t.Fatalf("unregistered reservation became an issued task: found=%t err=%v", found, err)
	}
	issued, err := IssueResultPackets(root, statePath, journalPath, current.Revision, "agent-qa-1")
	if err != nil || len(issued) != 1 {
		t.Fatalf("original issuer did not continue: packets=%d err=%v", len(issued), err)
	}
	if issued[0].ViewPath == reserved.ViewPath || issued[0].AnswerPath == reserved.AnswerPath {
		t.Fatal("unregistered occupant was claimed")
	}
	for _, item := range []struct {
		path    string
		content []byte
	}{{reserved.AnswerPath, originalAnswer}, {reserved.ViewPath, originalView}} {
		got, err := os.ReadFile(filepath.Join(root, item.path))
		if err != nil || !bytes.Equal(got, item.content) {
			t.Fatalf("unregistered material changed: %s %v", item.path, err)
		}
	}
	current, err = loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	registered, found, err := loopruntime.FindReviewResultPacketContext(loopruntime.WithPacketBodyRoot(context.Background(), root), current.State, issued[0].ID)
	if err != nil || !found || registered.ViewPath != issued[0].ViewPath || registered.AnswerPath != issued[0].AnswerPath {
		t.Fatalf("returned references differ from actual Writer: found=%t err=%v", found, err)
	}
	replayed, err := IssueResultPackets(root, statePath, journalPath, current.Revision, "agent-qa-1")
	if err != nil || len(replayed) != 1 || replayed[0].ViewPath != registered.ViewPath || replayed[0].AnswerPath != registered.AnswerPath {
		t.Fatalf("replay did not reuse original references: %v", err)
	}
}

func TestS7OriginalIssuerConcurrentCallsReturnRegisteredReferences(t *testing.T) {
	root, statePath, journalPath, _, _ := packetModeFixture(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	outcomes := make([][]workpacket.ReviewResultPacket, 2)
	errs := make([]error, 2)
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outcomes[i], errs[i] = IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
		}(i)
	}
	close(start)
	wg.Wait()
	current, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for i := range outcomes {
		if errs[i] != nil {
			t.Fatalf("issuer %d did not recover concurrent original Writer: %v", i, errs[i])
		}
		if len(outcomes[i]) != 1 {
			t.Fatalf("issuer %d packets=%d", i, len(outcomes[i]))
		}
		packet, found, err := loopruntime.FindReviewResultPacketContext(loopruntime.WithPacketBodyRoot(context.Background(), root), current.State, outcomes[i][0].ID)
		if err != nil || !found || packet.ViewPath != outcomes[i][0].ViewPath || packet.AnswerPath != outcomes[i][0].AnswerPath {
			t.Fatalf("issuer %d returned an unregistered candidate: found=%t err=%v", i, found, err)
		}
	}
}
