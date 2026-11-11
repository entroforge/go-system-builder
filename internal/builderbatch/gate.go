// Package builderbatch exposes shared current dispatch and checkpoint facts.
package builderbatch

import "encoding/json"

func HasDispatchPlan(state map[string]any) bool {
	generation := intField(object(state["baseline"])["generation"])
	for _, raw := range rows(state["documents"]) {
		if raw["kind"] == "dispatch_plan" && intField(raw["generation"]) == generation {
			return true
		}
	}
	return false
}

func rows(value any) []map[string]any {
	items, _ := value.([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if row, ok := item.(map[string]any); ok && row != nil {
			result = append(result, row)
		}
	}
	return result
}

func object(value any) map[string]any {
	row, _ := value.(map[string]any)
	return row
}

func intField(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		n, _ := number.Int64()
		return int(n)
	default:
		return 0
	}
}
