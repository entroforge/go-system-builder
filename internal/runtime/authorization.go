package runtime

import (
	"fmt"
	"strings"
	"time"
)

// HumanDecisionEvidenceConsumed reports whether a human_decision evidence
// item has already authorized a committed operation. Consumption is metadata
// on the durable evidence row so the original decision remains auditable.
func HumanDecisionEvidenceConsumed(item map[string]any) bool {
	if item == nil {
		return false
	}
	consumedBy, _ := item["consumed_by"].(string)
	return strings.TrimSpace(consumedBy) != ""
}

// ConsumeHumanDecisionEvidence marks one valid human_decision evidence item
// as used by consumer. The caller must invoke this inside the same Writer
// mutation as the operation authorized by the decision. This is a business
// one-time-use rule, independent of Runtime's internal revision bookkeeping.
func ConsumeHumanDecisionEvidence(state map[string]any, evidenceID, consumer string, occurredAt time.Time) error {
	evidenceID = strings.TrimSpace(evidenceID)
	consumer = strings.TrimSpace(consumer)
	if evidenceID == "" {
		return fmt.Errorf("human_decision evidence id is required")
	}
	if consumer == "" {
		return fmt.Errorf("human_decision evidence consumer is required")
	}
	items, ok := state["evidence"].([]any)
	if !ok {
		return fmt.Errorf("runtime evidence must be an array")
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || (strings.TrimSpace(authStringValue(item["id"])) != evidenceID && strings.TrimSpace(authStringValue(item["path"])) != evidenceID) {
			continue
		}
		if authStringValue(item["kind"]) != "human_decision" {
			return fmt.Errorf("evidence %q is %q, not human_decision", evidenceID, authStringValue(item["kind"]))
		}
		if authStringValue(item["status"]) != "valid" {
			return fmt.Errorf("human_decision evidence %q is not valid", evidenceID)
		}
		if HumanDecisionEvidenceConsumed(item) {
			return fmt.Errorf("human_decision evidence %q was already consumed by %s; create a new decision", evidenceID, authStringValue(item["consumed_by"]))
		}
		if occurredAt.IsZero() {
			occurredAt = time.Now().UTC()
		}
		item["consumed_by"] = consumer
		item["consumed_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
		return nil
	}
	return fmt.Errorf("human_decision evidence %q is not registered", evidenceID)
}

func authStringValue(value any) string {
	text, _ := value.(string)
	return text
}
