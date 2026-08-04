// Package metrics records ARCHITECTURE-039 §14.1 loop control-plane
// observability counters and durations. Metrics persist under
// <root>/.claude/loop-metrics.json so short-lived hook processes can
// accumulate observations across invocations without external deps.
package metrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultRelativePath is the canonical metrics file location under the
	// project root.
	DefaultRelativePath = ".claude/loop-metrics.json"

	metricGateEvaluations          = "loop_gate_evaluations_total"
	metricTransitionCommits        = "loop_transition_commits_total"
	metricCASConflicts             = "loop_cas_conflicts_total"
	metricMilestoneRefreshFailures = "loop_milestone_refresh_failures_total"
	metricRecoveryPackets          = "loop_recovery_packets_total"
	metricIntegrationDuration      = "loop_integration_duration_ms"
)

// DurationStats aggregates observed integration durations per status label.
type DurationStats struct {
	Count int64 `json:"count"`
	SumMS int64 `json:"sum_ms"`
}

// Snapshot is the durable on-disk metrics document.
type Snapshot struct {
	GateEvaluations          map[string]int64         `json:"loop_gate_evaluations_total"`
	TransitionCommits        map[string]int64         `json:"loop_transition_commits_total"`
	CASConflicts             int64                    `json:"loop_cas_conflicts_total"`
	MilestoneRefreshFailures int64                    `json:"loop_milestone_refresh_failures_total"`
	RecoveryPackets          int64                    `json:"loop_recovery_packets_total"`
	IntegrationDuration      map[string]DurationStats `json:"loop_integration_duration_ms"`
}

// Store persists metrics for one repository root.
type Store struct {
	root string
	path string
}

// NewStore returns a metrics store for repoRoot. An empty root is invalid
// for Record* calls but Read tolerates it for tests.
func NewStore(repoRoot string) *Store {
	root := strings.TrimSpace(repoRoot)
	return &Store{
		root: root,
		path: filepath.Join(root, DefaultRelativePath),
	}
}

// Path returns the absolute metrics file path.
func (s *Store) Path() string { return s.path }

func emptySnapshot() Snapshot {
	return Snapshot{
		GateEvaluations:     map[string]int64{},
		TransitionCommits:   map[string]int64{},
		IntegrationDuration: map[string]DurationStats{},
	}
}

// Read loads the durable metrics snapshot. A missing file yields zeroes.
func (s *Store) Read() (Snapshot, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return emptySnapshot(), nil
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read metrics: %w", err)
	}
	if len(data) == 0 {
		return emptySnapshot(), nil
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("decode metrics: %w", err)
	}
	if snap.GateEvaluations == nil {
		snap.GateEvaluations = map[string]int64{}
	}
	if snap.TransitionCommits == nil {
		snap.TransitionCommits = map[string]int64{}
	}
	if snap.IntegrationDuration == nil {
		snap.IntegrationDuration = map[string]DurationStats{}
	}
	return snap, nil
}

// RecordGateEvaluation increments loop_gate_evaluations_total{status}.
func RecordGateEvaluation(root, status string) error {
	return NewStore(root).mutate(func(snap *Snapshot) {
		label := normalizeLabel(status, "unknown")
		snap.GateEvaluations[label]++
	})
}

// RecordTransitionCommit increments loop_transition_commits_total{transition}.
func RecordTransitionCommit(root, transition string) error {
	return NewStore(root).mutate(func(snap *Snapshot) {
		label := normalizeLabel(transition, "unknown")
		snap.TransitionCommits[label]++
	})
}

// RecordCASConflict increments loop_cas_conflicts_total.
func RecordCASConflict(root string) error {
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.CASConflicts++
	})
}

// RecordMilestoneRefreshFailure increments loop_milestone_refresh_failures_total.
func RecordMilestoneRefreshFailure(root string) error {
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.MilestoneRefreshFailures++
	})
}

// RecordRecoveryPacket increments loop_recovery_packets_total.
func RecordRecoveryPacket(root string) error {
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.RecoveryPackets++
	})
}

// RecordIntegrationDuration records one integration duration sample under
// loop_integration_duration_ms{status}.
func RecordIntegrationDuration(root, status string, durationMS int64) error {
	if durationMS < 0 {
		durationMS = 0
	}
	return NewStore(root).mutate(func(snap *Snapshot) {
		label := normalizeLabel(status, "unknown")
		stats := snap.IntegrationDuration[label]
		stats.Count++
		stats.SumMS += durationMS
		snap.IntegrationDuration[label] = stats
	})
}

func (s *Store) mutate(apply func(*Snapshot)) error {
	if strings.TrimSpace(s.root) == "" {
		return errors.New("metrics: root is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create metrics directory: %w", err)
	}
	release, err := acquireLock(s.path+".lock", 30*time.Second)
	if err != nil {
		return err
	}
	defer release()

	snap, err := s.Read()
	if err != nil {
		return err
	}
	apply(&snap)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode metrics: %w", err)
	}
	if err := os.WriteFile(s.path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write metrics: %w", err)
	}
	return nil
}

// FormatDoctor renders a human-readable metrics summary for loop-harness doctor.
func FormatDoctor(root string) (string, error) {
	snap, err := NewStore(root).Read()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("doctor: loop control-plane metrics\n")
	writeLabeledCounter(&b, metricGateEvaluations, "status", snap.GateEvaluations)
	writeLabeledCounter(&b, metricTransitionCommits, "transition", snap.TransitionCommits)
	fmt.Fprintf(&b, "  %s %d\n", metricCASConflicts, snap.CASConflicts)
	fmt.Fprintf(&b, "  %s %d\n", metricMilestoneRefreshFailures, snap.MilestoneRefreshFailures)
	fmt.Fprintf(&b, "  %s %d\n", metricRecoveryPackets, snap.RecoveryPackets)
	writeDurationStats(&b, metricIntegrationDuration, snap.IntegrationDuration)
	return strings.TrimRight(b.String(), "\n"), nil
}

func writeLabeledCounter(b *strings.Builder, name, labelKey string, values map[string]int64) {
	if len(values) == 0 {
		fmt.Fprintf(b, "  %s (no samples)\n", name)
		return
	}
	labels := make([]string, 0, len(values))
	for label := range values {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		fmt.Fprintf(b, "  %s{%s=%q} %d\n", name, labelKey, label, values[label])
	}
}

func writeDurationStats(b *strings.Builder, name string, values map[string]DurationStats) {
	if len(values) == 0 {
		fmt.Fprintf(b, "  %s (no samples)\n", name)
		return
	}
	labels := make([]string, 0, len(values))
	for label := range values {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		stats := values[label]
		fmt.Fprintf(b, "  %s{status=%q} count=%d sum_ms=%d\n", name, label, stats.Count, stats.SumMS)
	}
}

func normalizeLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func acquireLock(path string, timeout time.Duration) (func(), error) {
	backoff := 5 * time.Millisecond
	const maxBackoff = 50 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("metrics lock timeout")
		}
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// processCounters mirror the legacy controller package-level ints for
// in-process test assertions. Durable export always goes to the file store.
var processCounters struct {
	sync.Mutex
	gateEvaluations   int
	transitionCommits int
	casConflicts      int
}

// ProcessGateEvaluations returns the in-process gate evaluation count.
func ProcessGateEvaluations() int {
	processCounters.Lock()
	defer processCounters.Unlock()
	return processCounters.gateEvaluations
}

// ProcessTransitionCommits returns the in-process transition commit count.
func ProcessTransitionCommits() int {
	processCounters.Lock()
	defer processCounters.Unlock()
	return processCounters.transitionCommits
}

// ProcessCASConflicts returns the in-process CAS conflict count.
func ProcessCASConflicts() int {
	processCounters.Lock()
	defer processCounters.Unlock()
	return processCounters.casConflicts
}

// RecordGateEvaluationProcess records gate evaluation in-process and durably.
func RecordGateEvaluationProcess(root, status string) {
	processCounters.Lock()
	processCounters.gateEvaluations++
	processCounters.Unlock()
	_ = RecordGateEvaluation(root, status)
}

// RecordTransitionCommitProcess records transition commit in-process and durably.
func RecordTransitionCommitProcess(root, transition string) {
	processCounters.Lock()
	processCounters.transitionCommits++
	processCounters.Unlock()
	_ = RecordTransitionCommit(root, transition)
}

// RecordCASConflictProcess records CAS conflict in-process and durably.
func RecordCASConflictProcess(root string) {
	processCounters.Lock()
	processCounters.casConflicts++
	processCounters.Unlock()
	_ = RecordCASConflict(root)
}
