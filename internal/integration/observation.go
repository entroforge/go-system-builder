package integration

import (
	"github.com/entroforge/go-system-builder/internal/filelock"
	"path/filepath"
	"time"
)

type WorkspaceObservation struct {
	Version     string `json:"version"`
	Checkout    string `json:"checkout"`
	LockRef     string `json:"lock_ref"`
	LockState   string `json:"lock_state"`
	ObservedAt  string `json:"observed_at"`
	Diagnostic  string `json:"diagnostic,omitempty"`
	PointInTime bool   `json:"point_in_time"`
	Authorizes  bool   `json:"authorizes"`
}

// ObserveWorkspace probes the original Main lock. A busy resource does not
// authenticate an Assignment, PID, checkpoint, completion or PASS.
func ObserveWorkspace(root string) WorkspaceObservation {
	o := WorkspaceObservation{Version: "1.0.0", LockState: "unknown", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), PointInTime: true}
	path, err := canonicalLockRoot(root)
	if err != nil {
		o.Diagnostic = err.Error()
		return o
	}
	o.Checkout = path
	o.LockRef = filepath.Join(path, ".claude/integration.lock")
	exists, busy, err := filelock.Probe(o.LockRef)
	if err != nil {
		o.Diagnostic = err.Error()
		return o
	}
	if !exists {
		o.LockState = "not_created"
	} else if busy {
		o.LockState = "busy"
	} else {
		o.LockState = "available"
	}
	return o
}
