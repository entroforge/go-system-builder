// Package metrics records ARCHITECTURE-039 §14.1 loop control-plane
// observability counters and durations. Metrics persist under
// <root>/.claude/loop-metrics.json so short-lived hook processes can
// accumulate observations across invocations without external deps.
package metrics

import (
	"bufio"
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
	// Hook registrations currently use the Claude Code default command
	// timeout configured by this repository. Forty percent is the early
	// warning point: PreToolUse command timeouts can silently let the tool
	// proceed, so operators need signal before the platform budget is near.
	defaultHookTimeoutMS         int64 = 10000
	hookTimingWarningThresholdMS int64 = defaultHookTimeoutMS * 40 / 100
)

// DurationStats aggregates observed integration durations per status label.
type DurationStats struct {
	Count int64 `json:"count"`
	SumMS int64 `json:"sum_ms"`
}

// Snapshot is the durable on-disk metrics document.
type Snapshot struct {
	GateEvaluations                map[string]int64         `json:"loop_gate_evaluations_total"`
	TransitionCommits              map[string]int64         `json:"loop_transition_commits_total"`
	CASConflicts                   int64                    `json:"loop_cas_conflicts_total"`
	MilestoneRefreshFailures       int64                    `json:"loop_milestone_refresh_failures_total"`
	MilestoneRefreshFailureReasons map[string]int64         `json:"loop_milestone_refresh_failure_reasons,omitempty"`
	RecoveryPackets                int64                    `json:"loop_recovery_packets_total"`
	IntegrationDuration            map[string]DurationStats `json:"loop_integration_duration_ms"`
	// S7 verification-round series (L3-S7 §14.2 machine-collectible subset;
	// see s7.go).
	S7Assignments        map[string]int64         `json:"loop_s7_assignments,omitempty"`
	S7Claims             map[string]int64         `json:"loop_s7_claims,omitempty"`
	S7PlanRevision       map[string]int64         `json:"loop_s7_plan_revision,omitempty"`
	S7ResultSubmits      map[string]int64         `json:"loop_s7_result_submits_total,omitempty"`
	S7ClaimLeadTime      map[string]DurationStats `json:"loop_s7_claim_lead_time_ms,omitempty"`
	S7Findings           map[string]int64         `json:"loop_s7_findings_total,omitempty"`
	S7FirstFindingToSeal map[string]DurationStats `json:"loop_s7_first_finding_to_seal_ms,omitempty"`
	S7CleanRounds        map[string]int64         `json:"loop_s7_clean_rounds_total,omitempty"`
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
		S7Assignments:                  map[string]int64{},
		S7Claims:                       map[string]int64{},
		S7PlanRevision:                 map[string]int64{},
		S7ResultSubmits:                map[string]int64{},
		S7ClaimLeadTime:                map[string]DurationStats{},
		S7Findings:                     map[string]int64{},
		S7FirstFindingToSeal:           map[string]DurationStats{},
		S7CleanRounds:                  map[string]int64{},
		S7SubmitPhases:                 map[string]DurationStats{},
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
	if snap.MilestoneRefreshFailureReasons == nil {
		snap.MilestoneRefreshFailureReasons = map[string]int64{}
	}
	if snap.IntegrationDuration == nil {
		snap.IntegrationDuration = map[string]DurationStats{}
	}
	if snap.S7Assignments == nil {
		snap.S7Assignments = map[string]int64{}
	}
	if snap.S7Claims == nil {
		snap.S7Claims = map[string]int64{}
	}
	if snap.S7PlanRevision == nil {
		snap.S7PlanRevision = map[string]int64{}
	}
	if snap.S7ResultSubmits == nil {
		snap.S7ResultSubmits = map[string]int64{}
	}
	if snap.S7ClaimLeadTime == nil {
		snap.S7ClaimLeadTime = map[string]DurationStats{}
	}
	if snap.S7Findings == nil {
		snap.S7Findings = map[string]int64{}
	}
	if snap.S7FirstFindingToSeal == nil {
		snap.S7FirstFindingToSeal = map[string]DurationStats{}
	}
	if snap.S7CleanRounds == nil {
		snap.S7CleanRounds = map[string]int64{}
	}
	if snap.S7SubmitPhases == nil {
		snap.S7SubmitPhases = map[string]DurationStats{}
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

// RecordMilestoneRefreshFailure increments loop_milestone_refresh_failures_total
// and records one bounded diagnostic reason. The reason labels are deliberately
// a small operational taxonomy rather than raw error strings, so a malformed
// candidate cannot create unbounded metric cardinality.
func RecordMilestoneRefreshFailure(root, reason string) error {
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.MilestoneRefreshFailures++
		snap.MilestoneRefreshFailureReasons[normalizeMilestoneFailureReason(reason)]++
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
	retainS7Rounds(&snap, s7RoundRetention)
	pruneDeadFamilies(&snap)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode metrics: %w", err)
	}
	if err := atomicWriteMetrics(s.path, append(data, '\n')); err != nil {
		return fmt.Errorf("write metrics: %w", err)
	}
	return nil
}

// maxTrackedFamilies documents the RC-17 family budget: the durable snapshot
// tracks sixteen loop_* families — seven core control-plane families plus
// nine S7 round/outcome families — and every one is wired at both ends:
//   - core seven: recorded by controller/cycle.go, cli/controller.go,
//     cli/run.go and integration/integrate.go; rendered by FormatDoctor /
//     FormatHealth;
//   - S9 nine: recorded from review submit/supplement (RecordS7RoundShape,
//     RecordS7ResultSubmit, RecordS7SubmitPhase, ...); all rendered by
//     FormatS7 (cli/s7_status.go).
//
// The audit's "15→≤10, remove S7PlanRevision as zero-consumer" does not
// survive contact with the wiring: S7PlanRevision has live producers
// (RecordS7RoundShape via review/supplement.go and review/submit.go) and a
// live renderer (FormatS7), so it is kept and documented instead of cut. The
// cap is therefore a review guard, not a quota — adding a seventeenth family
// requires either retiring a dead one in the same change or extending this
// comment with the new producer/consumer pair. Enforced by
// TestTrackedFamilyBudget.
const maxTrackedFamilies = 16

// pruneDeadFamilies trims unparseable labels out of the round-keyed S7 maps
// before the snapshot is persisted. Round-keyed families may only carry
// labels that roundFromLabel understands (the "r<N>" / numeric round forms
// written by the recorders); a label outside that vocabulary is a dead key
// from a legacy or corrupt writer — it can never be rendered by FormatS7
// (roundLabelVisible), never trimmed by retainS7Rounds (which skips it when
// computing retention), and would otherwise survive every write forever.
// Unlike retainS7Rounds this runs on every mutation, not only when the map
// exceeds the retention cap. Callers hold the store lock (mutate).
func pruneDeadFamilies(snap *Snapshot) {
	pruneGauge := func(m map[string]int64) {
		for label := range m {
			if _, ok := roundFromLabel(label); !ok {
				delete(m, label)
			}
		}
	}
	pruneDuration := func(m map[string]DurationStats) {
		for label := range m {
			if _, ok := roundFromLabel(label); !ok {
				delete(m, label)
			}
		}
	}
	pruneGauge(snap.S7Assignments)
	pruneGauge(snap.S7Claims)
	pruneGauge(snap.S7PlanRevision)
	pruneGauge(snap.S7Findings)
	pruneGauge(snap.S7CleanRounds)
	pruneDuration(snap.S7FirstFindingToSeal)
	pruneDuration(snap.S7ClaimLeadTime)
}

func atomicWriteMetrics(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".loop-metrics-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if _, err := file.Write(data); err != nil {
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
	if snap.CASConflicts > 0 || snap.MilestoneRefreshFailures > 0 {
		return true
	}
	if snap.GateEvaluations["unknown"] > 0 {
		return true
	}
	for _, stats := range timing {
		for _, sample := range stats.samples {
			if sample >= hookTimingWarningThresholdMS {
				return true
			}
		}
	}
	return false
}

type hookTimingStats struct {
	samples []int64
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
			HookEvent string `json:"hook_event"`
			ElapsedMS *int64 `json:"elapsed_ms"`
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
		if p95 >= hookTimingWarningThresholdMS || max >= hookTimingWarningThresholdMS {
			fmt.Fprintf(b, "  WARNING: Hook event %q is at or above %d%% of the %dms timeout; a platform timeout can bypass PreToolUse enforcement\n", event, hookTimingWarningThresholdMS*100/defaultHookTimeoutMS, defaultHookTimeoutMS)
		}
	}
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
	owner := fmt.Sprintf("%d:%d", os.Getpid(), time.Now().UnixNano())
	backoff := 5 * time.Millisecond
	const maxBackoff = 50 * time.Millisecond
	const staleAge = 30 * time.Second
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = file.WriteString(owner)
			_ = file.Close()
			return func() {
				data, err := os.ReadFile(path)
				if err == nil && strings.TrimSpace(string(data)) == owner {
					_ = os.Remove(path)
				}
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		// Stale reclaim: if lock file is older than staleAge, remove it.
		if info, statErr := os.Stat(path); statErr == nil {
			if time.Since(info.ModTime()) > staleAge {
				_ = os.Remove(path)
				continue
			}
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
