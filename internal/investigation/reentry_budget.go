package investigation

import (
	"fmt"
	"math"
)

// InvestigationBudgetStatus projects existing Case history; it grants no new
// work authority and does not count hypotheses, observations or Agent turns.
type InvestigationBudgetStatus struct {
	Used        int    `json:"used"`
	Limit       int    `json:"limit"`
	Remaining   int    `json:"remaining"`
	Exhausted   bool   `json:"exhausted"`
	Source      string `json:"source"`
	NewEvidence bool   `json:"new_evidence"`
}

const defaultMaxInvestigationReentries = 5

// InvestigationReentryBudget limits only consecutive investigate_more route
// registrations. Legacy Runtime configurations without the optional S8 field
// use the independent default; max_full_review_rounds belongs solely to S7
// and never supplies or extends investigation budget.
func InvestigationReentryBudget(state, document map[string]any) (InvestigationBudgetStatus, error) {
	budget := InvestigationBudgetStatus{Limit: defaultMaxInvestigationReentries, Source: "independent_s8_default"}
	configuration, _ := state["configuration"].(map[string]any)
	repair, _ := configuration["repair"].(map[string]any)
	if value, exists := repair["max_investigation_reentries"]; exists {
		switch v := value.(type) {
		case int:
			budget.Limit = v
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v >= float64(math.MaxInt) {
				return budget, fmt.Errorf("configuration.repair.max_investigation_reentries must be a positive integer")
			}
			budget.Limit = int(v)
		default:
			return budget, fmt.Errorf("configuration.repair.max_investigation_reentries must be a positive integer")
		}
		if budget.Limit < 1 {
			return budget, fmt.Errorf("configuration.repair.max_investigation_reentries must be a positive integer")
		}
		budget.Source = "configuration.repair.max_investigation_reentries"
	}
	history, err := objectArrayAllowEmpty(document["route_history"], "InvestigationCase.route_history")
	if err != nil {
		return budget, err
	}
	for _, entry := range history {
		if route := stringField(entry["to"]); route == "investigate_more" {
			budget.Used++
		} else if route != "" {
			budget.Used = 0
		}
	}
	budget.Remaining = max(0, budget.Limit-budget.Used)
	budget.Exhausted = budget.Used >= budget.Limit
	budget.NewEvidence = routeHasNewEvidence(document)
	return budget, nil
}
