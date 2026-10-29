package assignment

import (
	"github.com/entroforge/go-system-builder/internal/review"
	"testing"
)

func TestNewBehaviorDispatchRequiresOriginalPlanSurface(t *testing.T) {
	for _, tt := range []struct {
		surface, responsibility string
		valid                   bool
	}{
		{"", "E2E-USER-FLOW", true}, {"", "E2E-BEHAVIOR", false},
		{"browser", "E2E-BEHAVIOR", false}, {"non_browser", "E2E-USER-FLOW", false},
		{"browser", "E2E-USER-FLOW", true}, {"browser", "E2E-CONSOLE-NETWORK", true},
		{"non_browser", "E2E-BEHAVIOR", true},
	} {
		a := &review.PlanAssignment{AssignmentID: "assignment-behavior", Lens: "e2e", BehaviorSurface: tt.surface}
		if err := validateBehaviorResponsibility(a, tt.responsibility); (err == nil) != tt.valid {
			t.Errorf("%s/%s: valid=%v err=%v", tt.surface, tt.responsibility, tt.valid, err)
		}
	}
}
