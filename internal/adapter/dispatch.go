// Package adapter bridges typed runtime errors to high-level transition
// engine calls. The first adapter is DispatchRepairLimitExceeded, which
// converts a *transition.RepairLimitError into a transition.Apply(GTR-004)
// invocation so the runtime enters paused state with a real pause_record.
//
// Per BUG-003 §4b.2(f) the bridge is the SOLE way the runtime ever enters
// the paused state due to a repair-limit condition. The BUG lifecycle
// executor raises *RepairLimitError; the dispatcher receives it; the
// transition engine runs GTR-004 (which calls capture_pause_checkpoint
// exactly once) and the runtime is now paused awaiting human decision.
package adapter

import (
	"errors"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// DispatchRepairLimitExceeded calls transition.Apply(GTR-004) for the
// supplied RepairLimitError. The GTR-004 transition is the canonical
// "repair_limit_exceeded → paused" path declared in loop-definition.json
// global_transitions (it is the only transition that goes to paused from
// [verification, bug_resolution]).
//
// The function returns the post-transition Snapshot so callers can chain
// actions on top (e.g., record a BUG closure). The expected revision is
// taken from the supplied snapshot; if zero, the function reads the current
// runtime revision from disk via transition.Apply's internal load (the
// caller is responsible for passing the latest known revision).
//
// If err is nil the function returns (zero, nil) — the dispatcher does not
// synthesize errors. If err is not a *RepairLimitError the function
// surfaces it unchanged so callers can distinguish "not a limit error" from
// "dispatched but failed".
func DispatchRepairLimitExceeded(root, statePath, journalPath string, expectedRevision int, err error) (loopruntime.Snapshot, error) {
	if err == nil {
		return loopruntime.Snapshot{}, nil
	}
	var rle *transition.RepairLimitError
	if !errors.As(err, &rle) {
		return loopruntime.Snapshot{}, err
	}
	return transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "GTR-004",
		ExpectedRevision: expectedRevision,
		Actor:            "hook",
		Evidence: map[string]string{
			"pause_record":     "runtime:pause-checkpoint",
			"bug_batch_record": "runtime:bug:" + rle.BugID,
		},
		Params:     map[string]any{"bug_id": rle.BugID, "attempts": rle.Attempts, "max": rle.Max},
		OccurredAt: time.Now().UTC(),
	})
}

func itoa(n int) string {
	// Avoid importing strconv just for one call.
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
