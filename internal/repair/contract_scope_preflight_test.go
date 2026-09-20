package repair

import "testing"

func TestContractUnitScopesBeforeApproval(t *testing.T) {
	for _, tc := range []struct {
		name, scope string
		ok          bool
	}{
		{"within", "server/api/person.go", true}, {"sibling-prefix", "server/api-extra/person.go", false},
		{"outside", "web/src/types/crm.ts", false}, {"forbidden", "server/api/secrets.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(`{"repair_units":[{"id":"unit-1","scope":["` + tc.scope + `"]}],"prospective_scope":["server/api"],"forbidden_scope":["server/api/secrets.go"]}`)
			if err := ValidateContractUnitScopes(data); (err == nil) != tc.ok {
				t.Fatalf("scope=%s error=%v", tc.scope, err)
			}
		})
	}
}

func TestDelegatedExecutionMapRejectsLatePlanningGaps(t *testing.T) {
	for _, tc := range []struct {
		name, units string
		wantError   bool
	}{
		{"complete", `[{"id":"u","assertion_ids":["all"]}]`, false},
		{"missing declaration", `[{"id":"u"}]`, true},
		{"unowned root", `[{"id":"u","assertion_ids":["symptom-1"]}]`, true},
		{"unknown slot", `[{"id":"u","assertion_ids":["symptom-9","root-1"]}]`, true},
		{"cycle", `[{"id":"u","assertion_ids":["all"],"depends_on":["u"]}]`, true},
		{"phantom dependency", `[{"id":"u","assertion_ids":["all"],"depends_on":["missing"]}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(`{"repair_units":` + tc.units + `,"symptom_assertions":["s"],"root_invariant_assertions":["r"]}`)
			err := ValidateContractExecutionMap(data)
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
