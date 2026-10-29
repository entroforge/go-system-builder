package designfoundation

import (
	"strings"
	"testing"
)

func TestProjectRuleScopeReportsOnlyObservableFileText(t *testing.T) {
	for _, tc := range []struct {
		name, scope, body  string
		violation, unknown bool
	}{
		{"viewport cannot infer visibility", "viewport", `<button hidden data-design-role="action.promise">hidden</button><button data-design-role="action.promise">shown</button>`, false, true},
		{"file counts hidden text as text", "file", `<button hidden data-design-role="action.promise">hidden</button><button data-design-role="action.promise">shown</button>`, true, false},
		{"partial role name is not this role", "file", `<button data-design-role="action.promise-secondary">one</button><button data-design-role="action.promise">two</button>`, false, false},
		{"one attribute counted once", "file", `<button data-design-role="action.promise,action.promise">one</button>`, false, false},
		{"unknown scope not aggregated", "veiwport", `<button data-design-role="action.promise">one</button><button data-design-role="action.promise">two</button>`, false, true},
		{"missing marker is unverified", "file", `<button>no declaration</button>`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := baseContractRoot(t)
			mustWriteTreeGeneric(t, root, map[string]string{"docs/design/prototypes/current.html": tc.body})
			writeChecks(t, root, DesignChecks{Version: 1, Foundation: KernelRel + "@v1.0.0", Rules: []DesignCheckRule{{ID: "DCHK-scope", Source: "LAW-01", Type: "max_role_count", Scope: tc.scope, Role: "action.promise", Max: intPtr(1), Targets: []string{"docs/design/prototypes/current.html"}}}})
			report, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			violation, unknown := false, false
			for _, f := range report.Findings {
				if !strings.Contains(f.Detail, "DCHK-scope") {
					continue
				}
				violation = violation || f.Code == "project_rule_violation"
				unknown = unknown || f.Code == "project_rule_unverifiable"
				if f.Code == "project_rule_violation" && !strings.Contains(f.Detail, "file-text") {
					t.Errorf("text count was presented as rendered UI judgment: %s", f.Detail)
				}
			}
			if violation != tc.violation || unknown != tc.unknown {
				t.Fatalf("rule inferred an unobservable scope: violation=%v unverified=%v; want=%v/%v: %+v", violation, unknown, tc.violation, tc.unknown, report.Findings)
			}
		})
	}
}

func TestTokenScopeTextCooccurrenceDoesNotCertifyDOMBinding(t *testing.T) {
	for _, tc := range []struct {
		name, body, allow string
		missing, complex  bool
	}{
		{"marker on different element", `<aside data-evidence-id="EVD-1">unrelated</aside><p style="font-family:var(--font-family-mono)">outside</p>`, "[data-evidence-id]", false, false},
		{"no marker text", `<p style="font-family:var(--font-family-mono)">text</p>`, "[data-evidence-id]", true, false},
		{"complex selector not interpreted", `<aside class="allowed">reference</aside><p style="font-family:var(--font-family-mono)">outside</p>`, ".allowed > p", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := baseContractRoot(t)
			mustWriteTreeGeneric(t, root, map[string]string{"docs/design/prototypes/current.html": tc.body})
			writeChecks(t, root, DesignChecks{Version: 1, Foundation: KernelRel + "@v1.0.0", Rules: []DesignCheckRule{{ID: "DCHK-token", Source: "INV-01", Type: "token_scope", Token: "font.family.mono", Allow: []string{tc.allow}, Targets: []string{"docs/design/prototypes/current.html"}}}})
			report, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			var ruleFindings []Finding
			for _, f := range report.Findings {
				if strings.Contains(f.Detail, "DCHK-token") {
					ruleFindings = append(ruleFindings, f)
				}
			}
			if len(ruleFindings) != 1 {
				t.Fatalf("text scope is silent or ambiguous: %+v", ruleFindings)
			}
			f := ruleFindings[0]
			wantCode := "project_rule_observation"
			if tc.missing {
				wantCode = "project_rule_violation"
			} else if tc.complex {
				wantCode = "project_rule_unverifiable"
			}
			if f.Code != wantCode || !strings.Contains(f.Detail, "file-text") || strings.Contains(f.Detail, "used outside allow") {
				t.Fatalf("file text was promoted to DOM relationship proof: %+v", f)
			}
		})
	}
}
