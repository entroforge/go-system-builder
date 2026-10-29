package buildsurface

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// SelectCanonical also recognizes the original pinned Plan's declaration.
// Removing the current Assignment index must not turn that Plan into legacy.
// This only selects the strict reader; it grants no baseline authority.
func SelectCanonical(root string, state map[string]any) (bool, error) {
	return SelectCanonicalContext(context.Background(), root, state)
}

func SelectCanonicalContext(ctx context.Context, root string, state map[string]any) (selected bool, readErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if ctx.Err() != nil {
			selected, readErr = false, ctx.Err()
		}
	}()

	if err := ctx.Err(); err != nil {
		return false, err
	}
	if HasCanonical(state) {
		return true, nil
	}
	baseline, _ := state["baseline"].(map[string]any)
	evidence, _ := state["evidence"].([]any)
	for _, raw := range evidence {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		row, _ := raw.(map[string]any)
		if row["kind"] != "completion_report" || row["status"] != "valid" || number(row["baseline_generation"]) != number(baseline["generation"]) {
			continue
		}
		data, err := workpacket.ReadFile(root, textField(row["path"]), 20<<20)
		if err != nil {
			return false, fmt.Errorf("read current completion source %q (%s): %w; Main must inspect its original registered contribution", textField(row["path"]), textField(row["id"]), err)
		}
		if hashBytes(data) != row["sha256"] {
			return false, fmt.Errorf("current completion source %q (%s) changed; Main must inspect its original registered contribution", textField(row["path"]), textField(row["id"]))
		}
		var declaration map[string]json.RawMessage
		if err := json.Unmarshal(data, &declaration); err != nil {
			return false, fmt.Errorf("decode current completion source: %w", err)
		}
		if _, canonical := declaration["assignment_record"]; canonical {
			return true, nil
		}
	}
	ptr := reviewmeta.FromState(state)
	if ptr == nil {
		return false, nil
	}
	data, err := workpacket.ReadFile(root, ptr.Path, 20<<20)
	if err != nil || hashBytes(data) != ptr.SHA256 {
		return false, fmt.Errorf("original pinned ReviewPlan is missing or changed")
	}
	var declaration map[string]json.RawMessage
	if err := json.Unmarshal(data, &declaration); err != nil {
		return false, fmt.Errorf("decode original pinned ReviewPlan: %w", err)
	}
	_, canonical := declaration["product_baseline"]
	if canonical {
		// A retained historical Plan cannot select the new generation's
		// implementation mode. Current malformed declarations remain strict.
		var planGeneration int
		if rawGeneration, present := declaration["baseline_generation"]; present && json.Unmarshal(rawGeneration, &planGeneration) == nil && planGeneration > 0 && planGeneration < number(baseline["generation"]) {
			return false, nil
		}
	}
	return canonical, nil
}
