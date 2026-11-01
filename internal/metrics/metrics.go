// Package metrics records ARCHITECTURE-039 §14.1 loop control-plane
// observability counters and durations. Metrics persist under
// <root>/.claude/loop-metrics.json so short-lived hook processes can
// accumulate observations across invocations without external deps.
package metrics

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/filelock"
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
	metricSubmissionSave           = "loop_submission_save_total"
	metricSubmissionSaveDuration   = "loop_submission_save_duration_ms"
	// Match the shipped platform budgets (PostToolUse has an S4 handoff
	// ceiling). Forty percent is the early
	// warning point: PreToolUse command timeouts can silently let the tool
	// proceed, so operators need signal before the platform budget is near.
	defaultHookTimeoutMS int64 = 10000
)

// DurationStats aggregates observed integration durations per status label.
type DurationStats struct {
	Count int64 `json:"count"`
	SumMS int64 `json:"sum_ms"`
}

// Snapshot is the durable on-disk metrics document.
type Snapshot struct {
	GateEvaluations                map[string]int64              `json:"loop_gate_evaluations_total"`
	TransitionCommits              map[string]int64              `json:"loop_transition_commits_total"`
	CASConflicts                   int64                         `json:"loop_cas_conflicts_total"`
	MilestoneRefreshFailures       int64                         `json:"loop_milestone_refresh_failures_total"`
	MilestoneRefreshFailureReasons map[string]int64              `json:"loop_milestone_refresh_failure_reasons,omitempty"`
	RecoveryPackets                int64                         `json:"loop_recovery_packets_total"`
	IntegrationDuration            map[string]DurationStats      `json:"loop_integration_duration_ms"`
	SubmissionSaves                map[string]int64              `json:"loop_submission_save_total,omitempty"`
	SubmissionSaveDuration         map[string]DurationStats      `json:"loop_submission_save_duration_ms,omitempty"`
	HookTransportFailures          map[string]HookTransportStats `json:"loop_hook_transport_failures,omitempty"`
	S7ResultSubmits                map[string]int64              `json:"loop_s7_result_submits_total,omitempty"`
	// S7SubmitPhases records per-phase durations of the review-result submit
	// CAS transaction (RC-10; see RecordS7SubmitPhase in s7.go).
	S7SubmitPhases map[string]DurationStats `json:"loop_s7_submit_phase_ms,omitempty"`
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
		GateEvaluations:                map[string]int64{},
		TransitionCommits:              map[string]int64{},
		MilestoneRefreshFailureReasons: map[string]int64{},
		IntegrationDuration:            map[string]DurationStats{},
		SubmissionSaves:                map[string]int64{},
		SubmissionSaveDuration:         map[string]DurationStats{},
		HookTransportFailures:          map[string]HookTransportStats{},
		S7ResultSubmits:                map[string]int64{},
		S7SubmitPhases:                 map[string]DurationStats{},
	}
}

// Read loads the durable metrics snapshot. A missing file yields zeroes.
func (s *Store) Read() (Snapshot, error) {
	snap, err := s.readBase()
	if err != nil {
		return snap, err
	}
	if err := s.readObservations(&snap); err != nil {
		return snap, err
	}
	return snap, nil
}

func (s *Store) readBase() (Snapshot, error) {
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
	if snap.MilestoneRefreshFailureReasons == nil {
		snap.MilestoneRefreshFailureReasons = map[string]int64{}
	}
	if snap.IntegrationDuration == nil {
		snap.IntegrationDuration = map[string]DurationStats{}
	}
	if snap.SubmissionSaves == nil {
		snap.SubmissionSaves = map[string]int64{}
	}
	if snap.SubmissionSaveDuration == nil {
		snap.SubmissionSaveDuration = map[string]DurationStats{}
	}
	if snap.HookTransportFailures == nil {
		snap.HookTransportFailures = map[string]HookTransportStats{}
	}
	if snap.S7ResultSubmits == nil {
		snap.S7ResultSubmits = map[string]int64{}
	}
	if snap.S7SubmitPhases == nil {
		snap.S7SubmitPhases = map[string]DurationStats{}
	}
	return snap, nil
}

// RecordGateEvaluation increments loop_gate_evaluations_total{status}.
func RecordGateEvaluation(root, status string) error { return ObserveGateEvaluation(root, status) }
func RecordTransitionCommit(root, transition string) error {
	return ObserveTransitionCommit(root, transition)
}
func RecordCASConflict(root string) error { return ObserveCASConflict(root) }
func RecordMilestoneRefreshFailure(root, reason string) error {
	return ObserveMilestoneRefreshFailure(root, reason)
}
func RecordRecoveryPacket(root string) error { return ObserveRecoveryPacket(root) }
func RecordIntegrationDuration(root, status string, durationMS int64) error {
	return ObserveIntegrationDuration(root, status, durationMS)
}

func (s *Store) mutate(apply func(*Snapshot)) error {
	return s.mutateWith(apply, atomicWriteMetrics)
}

func (s *Store) mutateWith(apply func(*Snapshot), write func(string, []byte) error) error {
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

	snap, err := s.readBase()
	if err != nil {
		return err
	}
	apply(&snap)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode metrics: %w", err)
	}
	if err := write(s.path, append(data, '\n')); err != nil {
		return fmt.Errorf("write metrics: %w", err)
	}
	return nil
}

// maxTrackedFamilies covers the current recorded metric families.
const maxTrackedFamilies = 12

func atomicWriteMetrics(path string, data []byte) error {
	return atomicWriteMetricsWith(path, data, metricsWriteHooks{})
}

// Private hooks let tests pause the real publisher at its filesystem
// boundaries without adding synchronization to production callers.
type metricsWriteHooks struct {
	created      func(*os.File) error
	write        func(*os.File, []byte) error
	beforeRename func() error
}

func atomicWriteMetricsWith(path string, data []byte, hooks metricsWriteHooks) error {
	dir := filepath.Dir(path)
	file, err := createMetricsTemp(dir)
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if hooks.created != nil {
		if err := hooks.created(file); err != nil {
			file.Close()
			return err
		}
	}
	write := hooks.write
	if write == nil {
		write = func(file *os.File, data []byte) error {
			_, err := file.Write(data)
			return err
		}
	}
	if err := write(file, data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if hooks.beforeRename != nil {
		if err := hooks.beforeRename(); err != nil {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	_ = f.Sync()
	return nil
}

// createMetricsTemp owns the reserved v1 name independently of CreateTemp's
// undocumented random-suffix encoding. Exclusive creation preserves any
// existing file, including one that happens to have the same name.
func createMetricsTemp(dir string) (*os.File, error) {
	for tries := 0; tries < 100; tries++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		name := fmt.Sprintf(".loop-metrics-v1-%x.tmp", random)
		file, err := os.OpenFile(filepath.Join(dir, name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return file, err
	}
	return nil, fmt.Errorf("create metrics temporary file: too many name collisions")
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
	writeLabeledCounter(&b, metricMilestoneRefreshFailures, "reason", snap.MilestoneRefreshFailureReasons)
	fmt.Fprintf(&b, "  %s %d\n", metricRecoveryPackets, snap.RecoveryPackets)
	writeDurationStats(&b, metricIntegrationDuration, snap.IntegrationDuration)
	writeSubmissionSaves(&b, snap)
	writeHookTransportFailures(&b, snap)
	hookTiming, err := readHookTiming(root)
	if err != nil {
		return "", err
	}
	writeHookTimingStats(&b, hookTiming)
	return strings.TrimRight(b.String(), "\n"), nil
}

// FormatHealth renders runtime observations separately from repository
// structure checks. The counters are intentionally historical and
// cumulative; a non-zero signal means "inspect the runtime history", not
// that the current repository is structurally invalid. Callers that need a
// hard CI decision can use HealthDegraded and choose --fail-on-degraded at
// the CLI boundary.
func FormatHealth(root string) (string, error) {
	snap, err := NewStore(root).Read()
	if err != nil {
		return "", err
	}
	timing, err := readHookTiming(root)
	if err != nil {
		return "", err
	}
	degraded := healthDegraded(snap, timing)
	var b strings.Builder
	if degraded {
		b.WriteString("runtime health: degraded (historical runtime signals; not a structural validation)\n")
	} else {
		b.WriteString("runtime health: healthy (historical runtime signals; not a structural validation)\n")
	}
	fmt.Fprintf(&b, "  %s=%d\n", metricCASConflicts, snap.CASConflicts)
	fmt.Fprintf(&b, "  %s=%d\n", metricMilestoneRefreshFailures, snap.MilestoneRefreshFailures)
	writeLabeledCounter(&b, metricGateEvaluations, "status", snap.GateEvaluations)
	writeHookTimingStats(&b, timing)
	writeSubmissionSaves(&b, snap)
	writeHookTransportFailures(&b, snap)
	if degraded {
		b.WriteString("  next: inspect runtime state/journal with `loop-harness runtime reconcile`\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// HealthDegraded reports whether historical runtime signals warrant operator
// inspection. It does not replace a current state/journal validation.
func HealthDegraded(root string) (bool, error) {
	snap, err := NewStore(root).Read()
	if err != nil {
		return false, err
	}
	timing, err := readHookTiming(root)
	if err != nil {
		return false, err
	}
	return healthDegraded(snap, timing), nil
}

func healthDegraded(snap Snapshot, timing map[string]hookTimingStats) bool {
	if snap.CASConflicts > 0 || snap.MilestoneRefreshFailures > 0 || len(snap.HookTransportFailures) > 0 {
		return true
	}
	if snap.GateEvaluations["unknown"] > 0 {
		return true
	}
	for event, stats := range timing {
		for _, sample := range stats.samples {
			if sample >= hookPlatformTimeoutMS(event)*40/100 {
				return true
			}
		}
	}
	return false
}

type hookTimingStats struct {
	samples []int64
	phases  map[string]DurationStats
}

// readHookTiming reads the durable Hook decision outbox without adding a
// second per-hook metrics write. Older records without elapsed_ms are valid
// historical records and are skipped; new records carry the measured value.
func readHookTiming(root string) (map[string]hookTimingStats, error) {
	path := filepath.Join(root, ".claude", "hook-decisions.jsonl")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]hookTimingStats{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Hook timing outbox: %w", err)
	}
	defer file.Close()

	timing := map[string]hookTimingStats{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var record struct {
			HookEvent string                   `json:"hook_event"`
			ElapsedMS *int64                   `json:"elapsed_ms"`
			Phases    map[string]DurationStats `json:"call_phases"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode Hook timing outbox: %w", err)
		}
		if record.ElapsedMS == nil {
			continue
		}
		if *record.ElapsedMS < 0 {
			return nil, fmt.Errorf("Hook timing outbox contains negative elapsed_ms: %d", *record.ElapsedMS)
		}
		event := normalizeLabel(record.HookEvent, "unknown")
		stats := timing[event]
		if stats.phases == nil {
			stats.phases = map[string]DurationStats{}
		}
		for name, span := range record.Phases {
			if span.Count < 0 || span.SumMS < 0 {
				return nil, fmt.Errorf("invalid completed Hook phase %s", name)
			}
			total := stats.phases[name]
			total.Count += span.Count
			total.SumMS += span.SumMS
			stats.phases[name] = total
		}
		const maxSamples = 256
		if len(stats.samples) == maxSamples {
			copy(stats.samples, stats.samples[1:])
			stats.samples[len(stats.samples)-1] = *record.ElapsedMS
		} else {
			stats.samples = append(stats.samples, *record.ElapsedMS)
		}
		timing[event] = stats
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan Hook timing outbox: %w", err)
	}
	return timing, nil
}

func writeHookTimingStats(b *strings.Builder, timing map[string]hookTimingStats) {
	if len(timing) == 0 {
		fmt.Fprintf(b, "  loop_hook_evaluation_duration_ms (no samples)\n")
		return
	}
	events := make([]string, 0, len(timing))
	for event := range timing {
		events = append(events, event)
	}
	sort.Strings(events)
	for _, event := range events {
		samples := append([]int64(nil), timing[event].samples...)
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		if len(samples) == 0 {
			continue
		}
		index := (len(samples)*95+99)/100 - 1
		if index < 0 {
			index = 0
		}
		if index >= len(samples) {
			index = len(samples) - 1
		}
		p95 := samples[index]
		max := samples[len(samples)-1]
		fmt.Fprintf(b, "  loop_hook_evaluation_duration_ms{event=%q} count=%d p95_ms=%d max_ms=%d\n", event, len(samples), p95, max)
		names := make([]string, 0, len(timing[event].phases))
		for name := range timing[event].phases {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			span := timing[event].phases[name]
			fmt.Fprintf(b, "  loop_hook_completed_phase_ms{event=%q,phase=%q} count=%d sum_ms=%d (nested spans overlap)\n", event, name, span.Count, span.SumMS)
		}

		timeoutMS := hookPlatformTimeoutMS(event)
		thresholdMS := timeoutMS * 40 / 100
		if p95 >= thresholdMS || max >= thresholdMS {
			fmt.Fprintf(b, "  WARNING: Hook event %q is at or above 40%% of the %dms platform timeout", event, timeoutMS)
			if event == "PreToolUse" {
				fmt.Fprint(b, "; progress observation may be incomplete while ordinary tools continue")
			}
			fmt.Fprintln(b)
		}
	}
}

func hookPlatformTimeoutMS(event string) int64 {
	if event == "PostToolUse" {
		return 30000
	}
	return defaultHookTimeoutMS
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

func normalizeMilestoneFailureReason(value string) string {
	switch strings.TrimSpace(value) {
	case "stale_revision", "pending_runtime", "candidate_validation", "write_or_integrity":
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func acquireLock(path string, timeout time.Duration) (func(), error) {
	// A separate inode avoids treating an older binary's sentinel as an OS lock.
	// Never unlink or age-reclaim a process-owned lock.
	if timeout > 250*time.Millisecond {
		timeout = 250 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return filelock.Acquire(ctx, path+".process")
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
