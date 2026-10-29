package cli

import (
	"encoding/binary"
	"strings"
	"syscall"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// Alternating OPEN and CLOSE_NOWRITE events prevent inotify from coalescing
// repeated sequential reads into one identical unread event. Watch only the
// original RepairPlan, excluding PlanReport receipts, packet bodies and Git.
func s9EntryPlanReadWatch(t *testing.T, path string) func() (int, int) {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fd) })
	wd, err := syscall.InotifyAddWatch(fd, path, syscall.IN_OPEN|syscall.IN_CLOSE_NOWRITE)
	if err != nil {
		t.Fatal(err)
	}
	return func() (int, int) {
		t.Helper()
		opens, closes := 0, 0
		var buf [8192]byte
		for {
			n, err := syscall.Read(fd, buf[:])
			if err == syscall.EAGAIN {
				return opens, closes
			}
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				t.Fatal("inotify ended before events were drained")
			}
			for offset := 0; offset < n; {
				if n-offset < syscall.SizeofInotifyEvent {
					t.Fatal("truncated inotify event")
				}
				event := buf[offset:]
				mask := binary.NativeEndian.Uint32(event[4:8])
				length := int(binary.NativeEndian.Uint32(event[12:16]))
				if mask&syscall.IN_Q_OVERFLOW != 0 || int(int32(binary.NativeEndian.Uint32(event[:4]))) != wd {
					t.Fatalf("unexpected inotify event: mask=%x", mask)
				}
				if mask&syscall.IN_OPEN != 0 {
					opens++
				}
				if mask&syscall.IN_CLOSE_NOWRITE != 0 {
					closes++
				}
				offset += syscall.SizeofInotifyEvent + length
				if offset > n {
					t.Fatal("truncated inotify event name")
				}
			}
		}
	}
}

func TestS9EntryAssemblyReadsOriginalRepairPlanOnce(t *testing.T) {
	root, _, _, _, _ := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	path := resolveRootPath(root, stringValue(repairPointerCLI(snap.State)["plan_ref"]))
	reads := s9EntryPlanReadWatch(t, path)
	got := currentQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root})
	opens, closes := reads()
	if opens != 1 || closes != 1 {
		t.Errorf("one current Main preparation must read the original RepairPlan once: opens=%d closes=%d context=%s", opens, closes, got)
	}
	if strings.Count(got, "S9 execution preparation uses the same Builder division answer:") != 1 {
		t.Errorf("original preparation must be displayed once: %s", got)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9EntryAssemblyKeepsIndependentVerifierPlanRead(t *testing.T) {
	root, snap, _, _ := repairDispatchFixture(t)
	snap.State["schema_version"] = "1.2.0"
	mapFieldCLI(snap.State, "configuration")["optimization"] = map[string]any{"packet": true}
	path := resolveRootPath(root, stringValue(repairPointerCLI(snap.State)["plan_ref"]))
	reads := s9EntryPlanReadWatch(t, path)
	got := currentQuestionnaireLifecycleContextFromSnapshot(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root}, snap)
	opens, closes := reads()
	// Planning has two real consumers: Builder division and verifier division.
	// The old duplicate Main dispatch produced a third open/close pair.
	if opens != 2 || closes != 2 {
		t.Errorf("Builder and verifier must retain their independent original Plan reads: opens=%d closes=%d context=%s", opens, closes, got)
	}
	for _, want := range []string{"S9 Main Builder division:", "S9 Main independent verifier division:"} {
		if strings.Count(got, want) != 1 {
			t.Errorf("parallel responsibility must display once: %q: %s", want, got)
		}
	}
}
