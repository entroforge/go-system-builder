package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// Cancel immediately after the initial routing snapshot has been returned.
// A cached snapshot makes this an exact boundary without sleeps or I/O races.
type acceptanceCancelAfterRouting struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int64
}

func (c *acceptanceCancelAfterRouting) Err() error {
	if c.calls.Add(1) == 2 {
		c.cancel()
		return nil
	}
	return c.Context.Err()
}

func TestAcceptanceS9MainReturnDoesNotReadBodyAfterCancellation(t *testing.T) {
	root, _, packet, _, _ := nativeS9WorkerFixture(t)
	snap := repairDispatchSnapshot(t, root)
	var body string
	for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
		if stringValue(row["packet_id"]) != packet.ID {
			continue
		}
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil || !external {
			t.Fatalf("expected original external body: %v %v", external, err)
		}
		body = filepath.Join(root, ref.Path)
	}
	if body == "" {
		t.Fatal("missing selected original packet")
	}
	watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(watch)
	if _, err := syscall.InotifyAddWatch(watch, body, syscall.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	boundary := &acceptanceCancelAfterRouting{Context: parent, cancel: cancel}
	cached := &nativeRoutingRead{root: root, snap: snap}
	cached.once.Do(func() {})
	ctx := context.WithValue(boundary, nativeRoutingKey{}, cached)
	brief, _, _ := nativeS9Paths(packet)
	input := policy.Input{Context: ctx, Event: "PostToolUse", CWD: root, ToolName: "Agent", ToolInput: map[string]any{"prompt": filepath.Join(root, brief)}}
	var out bytes.Buffer
	handled, code := nativeMainReturnHook(root, input, &out)
	if !handled || code != 0 || parent.Err() != context.Canceled {
		t.Fatalf("cancellation fixture did not reach Main return: handled=%v code=%d canceled=%v calls=%d", handled, code, parent.Err(), boundary.calls.Load())
	}
	var events [4096]byte
	n, err := syscall.Read(watch, events[:])
	if err != nil && err != syscall.EAGAIN {
		t.Fatal(err)
	}
	if n > 0 {
		t.Fatalf("Main return opened original packet body after request cancellation (%d watch bytes); known facts may be projected but fresh I/O must retain caller budget. Output: %s", n, &out)
	}
}
