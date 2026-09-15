package assignment

import "testing"

// G11: an investigate_more re-entry returns the Case to investigating while the
// lifecycle phase stays at the S9 checkpoint earlier plan-report submissions
// advanced. Dispatch must follow the Case pointer, not the phase alone.
func TestValidateWorkgroupStateInvestigatorReentry(t *testing.T) {
	state := func(phase, status, route string) map[string]any {
		review := map[string]any{}
		if status != "" {
			review["investigation"] = map[string]any{"status": status, "route": route}
		}
		return map[string]any{
			"lifecycle": map[string]any{"state": "bug_resolution", "phase": phase},
			"review":    review,
		}
	}
	cases := []struct {
		name string
		in   map[string]any
		want bool
	}{
		{"plain investigation", state("investigation", "", ""), true},
		{"re-entry from reproducing", state("reproducing", "investigating", "investigate_more"), true},
		{"re-entry from repair_readback", state("repair_readback", "investigating", "investigate_more"), true},
		{"re-entry from planning", state("planning", "investigating", "investigate_more"), true},
		{"s9 execution, contract approved", state("targeted_reverification", "contract_approved", "s9_repair"), false},
		{"reproducing without a re-opened case", state("reproducing", "", ""), false},
		{"s9 route is not an s8 re-entry", state("reproducing", "investigating", "s9_repair"), false},
		{"wrong lifecycle state", map[string]any{"lifecycle": map[string]any{"state": "verification", "phase": "running"}}, false},
	}
	for _, testCase := range cases {
		err := validateWorkgroupState("investigator", testCase.in)
		if got := err == nil; got != testCase.want {
			t.Errorf("%s: accepted=%v want=%v (err=%v)", testCase.name, got, testCase.want, err)
		}
	}
}
