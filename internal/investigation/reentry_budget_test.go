package investigation_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
)

func TestInvestigationReentryBudgetSeparatesS7AndPreservesLegacyBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		repair  map[string]any
		limit   int
		source  string
		invalid bool
	}{
		{name: "absent legacy configuration", limit: 5, source: "independent_s8_default"},
		{name: "legacy S7 budget one", repair: map[string]any{"max_full_review_rounds": 1}, limit: 5, source: "independent_s8_default"},
		{name: "legacy raised S7 budget", repair: map[string]any{"max_full_review_rounds": 100}, limit: 5, source: "independent_s8_default"},
		{name: "explicit S8", repair: map[string]any{"max_full_review_rounds": 100, "max_investigation_reentries": 2}, limit: 2, source: "configuration.repair.max_investigation_reentries"},
		{name: "JSON S8", repair: map[string]any{"max_investigation_reentries": float64(3)}, limit: 3, source: "configuration.repair.max_investigation_reentries"},
		{name: "zero cannot silently disable", repair: map[string]any{"max_investigation_reentries": 0}, invalid: true},
		{name: "fraction cannot silently truncate", repair: map[string]any{"max_investigation_reentries": 2.5}, invalid: true},
		{name: "null cannot silently default", repair: map[string]any{"max_investigation_reentries": nil}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := map[string]any{"configuration": map[string]any{"repair": tc.repair}}
			document := map[string]any{"route_history": []any{map[string]any{"to": "investigate_more"}, map[string]any{"to": "investigate_more"}}}
			budget, err := investigation.InvestigationReentryBudget(state, document)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid budget accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if budget.Limit != tc.limit || budget.Used != 2 || budget.Source != tc.source || budget.Remaining != max(0, tc.limit-2) || budget.Exhausted != (tc.limit <= 2) {
				t.Fatalf("budget = %+v", budget)
			}
		})
	}
}

func TestInvestigationReentryBudgetResetsAfterConcreteRoute(t *testing.T) {
	document := map[string]any{"route_history": []any{
		map[string]any{"to": "investigate_more"}, map[string]any{"to": "investigate_more"},
		map[string]any{"to": "s9_repair"}, map[string]any{"to": "investigate_more"},
	}}
	budget, err := investigation.InvestigationReentryBudget(nil, document)
	if err != nil || budget.Used != 1 || budget.Remaining != 4 || budget.Exhausted {
		t.Fatalf("new causal reassessment budget = %+v, %v", budget, err)
	}
}
