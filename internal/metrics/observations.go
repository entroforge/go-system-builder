package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hook telemetry is append-only, one atomically published file per observation.
// It never waits for the summary lock. Read aggregates legacy summaries and
// these observations; summary mutations read only the legacy base, avoiding
// double-counting. These are diagnostics, never authorization evidence.
type observation struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Duration int64  `json:"duration_ms"`
}

func observe(root, kind, label string, duration int64) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("metrics: root is required")
	}
	dir := filepath.Join(root, ".claude/hook-metrics")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(observation{kind, label, duration}); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), f.Name()+".json")
}
func ObserveGateEvaluation(root, status string) error {
	return observe(root, "gate", normalizeLabel(status, "unknown"), 0)
}
func ObserveTransitionCommit(root, tr string) error {
	return observe(root, "transition", normalizeLabel(tr, "unknown"), 0)
}
func ObserveCASConflict(root string) error { return observe(root, "cas", "", 0) }
func ObserveMilestoneRefreshFailure(root, reason string) error {
	return observe(root, "milestone", normalizeMilestoneFailureReason(reason), 0)
}
func ObserveRecoveryPacket(root string) error { return observe(root, "recovery", "", 0) }
func ObserveIntegrationDuration(root, status string, ms int64) error {
	if ms < 0 {
		ms = 0
	}
	return observe(root, "integration", normalizeLabel(status, "unknown"), ms)
}

// readObservations serializes readers and compaction, but never producers.
// A persisted consumed-name set makes interrupted deletion exactly-once.
func (s *Store) readObservations(snap *Snapshot) error {
	return s.scanObservations(snap, false)
}
