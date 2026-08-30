package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// s7.go records the machine-collectible subset of the L3-S7 §14.2 operational
// metrics. Only facts the harness can decide mechanically are captured here:
// round shape (Assignment/Claim counts, plan revision), per-Claim lead time,
// Result submit first-pass success, finding counts and the first-finding ->
// seal duration, and clean rounds. Human-judgment metrics from §14.2
// (pattern-fit quality, escape-rate attribution, ...) are intentionally not
// collected.

const (
	metricS7Assignments        = "loop_s7_assignments"
	metricS7Claims             = "loop_s7_claims"
	metricS7PlanRevision       = "loop_s7_plan_revision"
	metricS7ResultSubmits      = "loop_s7_result_submits_total"
	metricS7ClaimLeadTime      = "loop_s7_claim_lead_time_ms"
	metricS7Findings           = "loop_s7_findings_total"
	metricS7FirstFindingToSeal = "loop_s7_first_finding_to_seal_ms"
	metricS7CleanRounds        = "loop_s7_clean_rounds_total"
	metricS7SubmitPhase        = "loop_s7_submit_phase_ms"
)

// RecordS7RoundShape pins the round's plan shape gauges: Assignment count,
// Claim count and the current plan revision. Gauges are idempotent — callers
// may record them on every verb that loads the plan.
func RecordS7RoundShape(root string, round, assignments, claims, planRevision int) error {
	if round < 1 {
		return nil
	}
	label := strconv.Itoa(round)
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.S7Assignments[label] = int64(assignments)
		snap.S7Claims[label] = int64(claims)
		if int64(planRevision) > snap.S7PlanRevision[label] {
			snap.S7PlanRevision[label] = int64(planRevision)
		}
	})
}

// RecordS7ResultSubmit increments loop_s7_result_submits_total{outcome};
// outcome is "accepted" or "rejected" (first-pass success rate, §14.2).
func RecordS7ResultSubmit(root, outcome string) error {
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.S7ResultSubmits[normalizeLabel(outcome, "unknown")]++
	})
}

// RecordS7ClaimLeadTime records one planned -> dispositioned sample per Claim
// under loop_s7_claim_lead_time_ms{claim="r<round>:<claim_id>"}.
func RecordS7ClaimLeadTime(root string, round int, claimID string, durationMS int64) error {
	if round < 1 || strings.TrimSpace(claimID) == "" {
		return nil
	}
	if durationMS < 0 {
		durationMS = 0
	}
	label := fmt.Sprintf("r%d:%s", round, claimID)
	return NewStore(root).mutate(func(snap *Snapshot) {
		stats := snap.S7ClaimLeadTime[label]
		stats.Count++
		stats.SumMS += durationMS
		snap.S7ClaimLeadTime[label] = stats
	})
}

// RecordS7SubmitPhase records one phase-duration sample for the review-result
// submit CAS transaction under loop_s7_submit_phase_ms{phase}. phase is a
// fixed vocabulary (result_consumption | findings | advance | seal | clean |
// pause); unknown phases are still recorded under their own label — the
// vocabulary is caller-owned, and the map is bounded by the phases submit.go
// actually emits. An empty root is a no-op (metrics are best-effort).
func RecordS7SubmitPhase(root string, phase string, durationMS int64) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	if durationMS < 0 {
		durationMS = 0
	}
	return NewStore(root).mutate(func(snap *Snapshot) {
		stats := snap.S7SubmitPhases[normalizeLabel(phase, "unknown")]
		stats.Count++
		stats.SumMS += durationMS
		snap.S7SubmitPhases[normalizeLabel(phase, "unknown")] = stats
	})
}

// RecordS7Findings adds count findings to loop_s7_findings_total{round}.
func RecordS7Findings(root string, round, count int) error {
	if round < 1 || count == 0 {
		return nil
	}
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.S7Findings[strconv.Itoa(round)] += int64(count)
	})
}

// RecordS7FirstFindingToSeal records the first-finding -> ObservationBatch
// seal duration once per round (§14.2 "first finding -> final Claim set ->
// batch seal time"); later seals in the same round do not overwrite it.
func RecordS7FirstFindingToSeal(root string, round int, durationMS int64) error {
	if round < 1 {
		return nil
	}
	if durationMS < 0 {
		durationMS = 0
	}
	label := strconv.Itoa(round)
	return NewStore(root).mutate(func(snap *Snapshot) {
		if _, recorded := snap.S7FirstFindingToSeal[label]; recorded {
			return
		}
		snap.S7FirstFindingToSeal[label] = DurationStats{Count: 1, SumMS: durationMS}
	})
}

// RecordS7CleanRound increments loop_s7_clean_rounds_total{round}; a machine
// CleanRound is generated at most once per round.
func RecordS7CleanRound(root string, round int) error {
	if round < 1 {
		return nil
	}
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.S7CleanRounds[strconv.Itoa(round)]++
	})
}

// FormatS7 renders the S7 §14.2 machine-collectible metrics for
// `loop-harness s7 status`. round > 0 filters round-scoped series to that
// round; round <= 0 renders every recorded round.
func FormatS7(root string, round int) (string, error) {
	snap, err := NewStore(root).Read()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("metrics (S7 §14.2 machine-collectible):\n")
	writeRoundGauge(&b, metricS7Assignments, snap.S7Assignments, round)
	writeRoundGauge(&b, metricS7Claims, snap.S7Claims, round)
	writeRoundGauge(&b, metricS7PlanRevision, snap.S7PlanRevision, round)
	writeLabeledCounter(&b, metricS7ResultSubmits, "outcome", snap.S7ResultSubmits)
	writeRoundGauge(&b, metricS7Findings, snap.S7Findings, round)
	writeClaimLeadTime(&b, snap.S7ClaimLeadTime, round)
	writeRoundDuration(&b, metricS7FirstFindingToSeal, snap.S7FirstFindingToSeal, round)
	writeRoundGauge(&b, metricS7CleanRounds, snap.S7CleanRounds, round)
	writeSubmitPhases(&b, snap.S7SubmitPhases)
	return strings.TrimRight(b.String(), "\n"), nil
}

func roundLabelVisible(label string, round int) bool {
	if round <= 0 {
		return true
	}
	return label == strconv.Itoa(round)
}

func writeRoundGauge(b *strings.Builder, name string, values map[string]int64, round int) {
	labels := make([]string, 0, len(values))
	for label := range values {
		if roundLabelVisible(label, round) {
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		fmt.Fprintf(b, "  %s (no samples)\n", name)
		return
	}
	sort.Strings(labels)
	for _, label := range labels {
		fmt.Fprintf(b, "  %s{round=%q} %d\n", name, label, values[label])
	}
}

func writeRoundDuration(b *strings.Builder, name string, values map[string]DurationStats, round int) {
	labels := make([]string, 0, len(values))
	for label := range values {
		if roundLabelVisible(label, round) {
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		fmt.Fprintf(b, "  %s (no samples)\n", name)
		return
	}
	sort.Strings(labels)
	for _, label := range labels {
		stats := values[label]
		fmt.Fprintf(b, "  %s{round=%q} count=%d sum_ms=%d\n", name, label, stats.Count, stats.SumMS)
	}
}

// writeSubmitPhases renders loop_s7_submit_phase_ms. It is not round-scoped
// (the CAS transaction spans a single round already), so it renders the phase
// label directly and only appears when at least one phase was recorded.
func writeSubmitPhases(b *strings.Builder, values map[string]DurationStats) {
	if len(values) == 0 {
		return
	}
	labels := make([]string, 0, len(values))
	for label := range values {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		stats := values[label]
		fmt.Fprintf(b, "  %s{phase=%q} count=%d sum_ms=%d\n", metricS7SubmitPhase, label, stats.Count, stats.SumMS)
	}
}

func writeClaimLeadTime(b *strings.Builder, values map[string]DurationStats, round int) {
	prefix := ""
	if round > 0 {
		prefix = fmt.Sprintf("r%d:", round)
	}
	labels := make([]string, 0, len(values))
	for label := range values {
		if prefix == "" || strings.HasPrefix(label, prefix) {
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		fmt.Fprintf(b, "  %s (no samples)\n", metricS7ClaimLeadTime)
		return
	}
	sort.Strings(labels)
	for _, label := range labels {
		stats := values[label]
		fmt.Fprintf(b, "  %s{claim=%q} count=%d sum_ms=%d\n", metricS7ClaimLeadTime, label, stats.Count, stats.SumMS)
	}
}

// s7RoundRetention caps round-keyed S7 maps retained in the durable snapshot.
// Only round-keyed families are capped; outcome/phase-keyed families are
// bounded by their own vocabularies and left uncapped.
const s7RoundRetention = 20

func retainS7Rounds(snap *Snapshot, keep int) {
	if keep <= 0 {
		return
	}
	trimRoundGauge := func(m map[string]int64) {
		if len(m) <= keep {
			return
		}
		type entry struct {
			round int
			key   string
		}
		entries := make([]entry, 0, len(m))
		for k := range m {
			r, ok := roundFromLabel(k)
			if !ok {
				continue
			}
			entries = append(entries, entry{round: r, key: k})
		}
		// Sort descending by round, keep the highest `keep` rounds, delete the rest.
		// For S7ClaimLeadTime the key is r<round>:<claim_id> — group by round.
		sort.Slice(entries, func(i, j int) bool { return entries[i].round > entries[j].round })
		keepRounds := make(map[int]struct{})
		for _, e := range entries {
			if len(keepRounds) >= keep {
				break
			}
			keepRounds[e.round] = struct{}{}
		}
		for k := range m {
			r, ok := roundFromLabel(k)
			if !ok {
				continue
			}
			if _, keep := keepRounds[r]; !keep {
				delete(m, k)
			}
		}
	}
	trimRoundDuration := func(m map[string]DurationStats) {
		if len(m) <= keep {
			return
		}
		type entry struct {
			round int
			key   string
		}
		entries := make([]entry, 0, len(m))
		for k := range m {
			r, ok := roundFromLabel(k)
			if !ok {
				continue
			}
			entries = append(entries, entry{round: r, key: k})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].round > entries[j].round })
		keepRounds := make(map[int]struct{})
		for _, e := range entries {
			if len(keepRounds) >= keep {
				break
			}
			keepRounds[e.round] = struct{}{}
		}
		for k := range m {
			r, ok := roundFromLabel(k)
			if !ok {
				continue
			}
			if _, keep := keepRounds[r]; !keep {
				delete(m, k)
			}
		}
	}
	trimRoundGauge(snap.S7Assignments)
	trimRoundGauge(snap.S7Claims)
	trimRoundGauge(snap.S7PlanRevision)
	trimRoundGauge(snap.S7Findings)
	trimRoundDuration(snap.S7FirstFindingToSeal)
	trimRoundGauge(snap.S7CleanRounds)
	trimRoundDuration(snap.S7ClaimLeadTime)
}

func roundFromLabel(label string) (int, bool) {
	label = strings.TrimSpace(label)
	if label == "" {
		return 0, false
	}
	// S7ClaimLeadTime shape: r<round>:<claim_id>
	if strings.HasPrefix(label, "r") {
		colon := strings.Index(label, ":")
		if colon > 1 {
			n, err := strconv.Atoi(label[1:colon])
			if err == nil && n > 0 {
				return n, true
			}
		}
		return 0, false
	}
	n, err := strconv.Atoi(label)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
