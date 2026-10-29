package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// HookTransportStats holds only observed outer-process facts. Optional budget
// bounds distinguish an unknown historical budget from an actual zero value.
// Neither elapsed time nor process budget asserts reception/successor status.
type HookTransportStats struct {
	Count              int64  `json:"count"`
	SumMS              int64  `json:"sum_ms"`
	ProcessBudgetMinMS *int64 `json:"process_budget_min_ms,omitempty"`
	ProcessBudgetMaxMS *int64 `json:"process_budget_max_ms,omitempty"`
}

// Only bounded categorical facts identify a series. A varying actual process
// budget is a numeric observation, never a dimension or an internal work budget.
type hookTransportLabel struct {
	Event   string `json:"event"`
	Outcome string `json:"outcome"`
}

// ObserveHookTransportFailure uses the existing lock-free observation spool.
// Successful hooks incur no extra observation; callers supply the actual outer
// budget, never an inferred child work budget or business acceptance verdict.
func ObserveHookTransportFailure(root, event, outcome string, elapsedMS, processBudgetMS int64) error {
	label := hookTransportLabel{normalizeHookTransportEvent(event), outcome}
	data, err := json.Marshal(label)
	if err != nil {
		return err
	}
	if _, err := parseHookTransportLabel(string(data)); err != nil || elapsedMS < 0 || processBudgetMS < 0 {
		return fmt.Errorf("invalid Hook transport failure")
	}
	return observeRecord(root, observation{Kind: "hook_transport_failure", Label: string(data), Duration: elapsedMS, ProcessBudgetMinMS: &processBudgetMS, ProcessBudgetMaxMS: &processBudgetMS})
}

func normalizeHookTransportEvent(event string) string {
	// The ten events currently installed by settings.json, plus one unknown
	// bucket. Author-controlled event strings cannot create new time series.
	switch event {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure", "SessionStart", "SubagentStart", "SubagentStop", "TeammateIdle", "Stop", "ConfigChange", "PreCompact":
		return event
	default:
		return "unknown"
	}
}

func parseHookTransportLabel(raw string) (hookTransportLabel, error) {
	var label hookTransportLabel
	if err := json.Unmarshal([]byte(raw), &label); err != nil {
		return label, err
	}
	if label.Event == "" {
		return label, fmt.Errorf("missing Hook transport identity")
	}
	label.Event = normalizeHookTransportEvent(label.Event)
	switch label.Outcome {
	case "deadline", "canceled", "input", "worker", "delivery":
		return label, nil
	default:
		return label, fmt.Errorf("unknown Hook transport outcome")
	}
}

// Normalize the bounded label and validate optional observed budget bounds.
func normalizeHookTransportObservation(o observation) (observation, error) {
	label, err := parseHookTransportLabel(o.Label)
	if err != nil {
		return o, err
	}
	if (o.ProcessBudgetMinMS == nil) != (o.ProcessBudgetMaxMS == nil) || o.Duration < 0 {
		return o, fmt.Errorf("invalid Hook process budget range")
	}
	if o.ProcessBudgetMinMS != nil && (*o.ProcessBudgetMinMS < 0 || *o.ProcessBudgetMaxMS < *o.ProcessBudgetMinMS) {
		return o, fmt.Errorf("invalid Hook process budget range")
	}
	data, err := json.Marshal(label)
	if err != nil {
		return o, err
	}
	o.Label = string(data)
	return o, nil
}

func mergeProcessBudgetBounds(minimum, maximum **int64, low, high *int64) {
	if low == nil {
		return
	}
	if *minimum == nil || *low < **minimum {
		value := *low
		*minimum = &value
	}
	if *maximum == nil || *high > **maximum {
		value := *high
		*maximum = &value
	}
}

func writeHookTransportFailures(b *strings.Builder, snap Snapshot) {
	labels := make([]string, 0, len(snap.HookTransportFailures))
	for label := range snap.HookTransportFailures {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, raw := range labels {
		label, err := parseHookTransportLabel(raw)
		if err != nil {
			continue
		}
		stats := snap.HookTransportFailures[raw]
		fmt.Fprintf(b, "  loop_hook_transport_failures{event=%q,outcome=%q} count=%d elapsed_sum_ms=%d", label.Event, label.Outcome, stats.Count, stats.SumMS)
		if stats.ProcessBudgetMinMS != nil && stats.ProcessBudgetMaxMS != nil {
			fmt.Fprintf(b, " process_budget_min_ms=%d process_budget_max_ms=%d", *stats.ProcessBudgetMinMS, *stats.ProcessBudgetMaxMS)
		}
		b.WriteString(" (reception and successor status unknown)\n")
	}
}
