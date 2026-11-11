package review

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanChecksAggregateIndependentProblemsAndSkipBrokenIdentity(t *testing.T) {
	p := loadFixturePlan(t)
	p.DispatchCapacityPolicy = "queue_only"
	p.Claims[0].Oracle = "TODO(planner) oracle"
	p.Claims[1].Method = "PLANNER-REFINE method"
	p.Claims = append(p.Claims, p.Claims[0])
	err := ValidatePlan(p)
	var author *PlanAnswerError
	var report *PlanChecksError
	if !errors.As(err, &author) || !errors.As(err, &report) {
		t.Fatalf("missing original author/check diagnostics: %v", err)
	}
	for _, expected := range []string{"duplicate claim_id"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("missing independent feedback %s: %v", expected, err)
		}
	}

}
