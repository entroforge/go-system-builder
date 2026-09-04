package designfoundation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeChecks(t *testing.T, root string, dc DesignChecks) {
	t.Helper()
	data, _ := json.Marshal(dc)
	if err := os.MkdirAll(filepath.Join(root, "docs/design"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, DesignChecksRel), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func baseContractRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":       "# Surface Profile — consumer\n> ID：SUR-01\n> 版本：v1.0.0\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html>anchor</html>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
	})
	return root
}

func intPtr(n int) *int { return &n }

func TestProjectRules_OptionalFileAbsentClean(t *testing.T) {
	root := baseContractRoot(t)
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "project_rule_unverifiable" || f.Code == "project_rule_violation" {
			t.Fatalf("no design-checks.json should not emit project_rule findings, got %#v", report.Findings)
		}
	}
}

func TestProjectRules_UnknownTypeUnverifiable(t *testing.T) {
	root := baseContractRoot(t)
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-01", Type: "no_such_type"}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_unverifiable" && strings.Contains(f.Detail, "unknown type") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown type must be unverifiable, got %#v", report.Findings)
	}
}

func TestProjectRules_MissingSourceUnverifiable(t *testing.T) {
	root := baseContractRoot(t)
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Type: "max_role_count", Role: "action.promise", Max: intPtr(1), Targets: []string{"docs/design/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_unverifiable" && strings.Contains(f.Detail, "missing source") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing source must be unverifiable, got %#v", report.Findings)
	}
}

func TestProjectRules_BadSourceIDUnverifiable(t *testing.T) {
	root := baseContractRoot(t)
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-99", Type: "max_role_count", Role: "action.promise", Max: intPtr(1), Targets: []string{"docs/design/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_unverifiable" && strings.Contains(f.Detail, "not found") {
			found = true
		}
	}
	if !found {
		t.Fatalf("bad source ID must be unverifiable, got %#v", report.Findings)
	}
}

func TestProjectRules_MaxRoleCount_MissingMarkerUnverifiable(t *testing.T) {
	root := baseContractRoot(t)
	// create a target file without data-design-role
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": "<html><button>no marker</button></html>",
	})
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-01", Type: "max_role_count", Role: "action.promise", Max: intPtr(1), Targets: []string{"docs/design/prototypes/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_unverifiable" && strings.Contains(f.Detail, "data-design-role") {
			found = true
		}
	}
	if !found {
		t.Fatalf("max_role_count without markers must be unverifiable, got %#v", report.Findings)
	}
}

func TestProjectRules_MaxRoleCount_Violation(t *testing.T) {
	root := baseContractRoot(t)
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": `<html><button data-design-role="action.promise">one</button><button data-design-role="action.promise">two</button></html>`,
	})
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-01", Type: "max_role_count", Role: "action.promise", Scope: "viewport", Max: intPtr(1), Targets: []string{"docs/design/prototypes/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_violation" && strings.Contains(f.Detail, "max_role_count") {
			found = true
			if f.Severity != SeverityWarning {
				t.Fatalf("must be warning")
			}
		}
	}
	if !found {
		t.Fatalf("expected max_role_count violation, got %#v", report.Findings)
	}
}

func TestProjectRules_MaxRoleCount_Pass(t *testing.T) {
	root := baseContractRoot(t)
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": `<html><button data-design-role="action.promise">one</button></html>`,
	})
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-01", Type: "max_role_count", Role: "action.promise", Scope: "viewport", Max: intPtr(1), Targets: []string{"docs/design/prototypes/**/*.html"}}},
	})
	report, _ := Check(root)
	for _, f := range report.Findings {
		if f.Code == "project_rule_violation" {
			t.Fatalf("single promise should pass, got %#v", report.Findings)
		}
	}
}

func TestProjectRules_ForbidBinding_Violation(t *testing.T) {
	root := baseContractRoot(t)
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": `<html><div style="color: var(--color-primitive-green-500)">green</div></html>`,
	})
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-01", Type: "forbid_binding", Subject: "color.status.verified", Forbidden: []string{"color.primitive.green.*"}, Targets: []string{"docs/design/prototypes/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_violation" && strings.Contains(f.Detail, "forbid_binding") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected forbid_binding violation, got %#v", report.Findings)
	}
}

func TestProjectRules_TokenScope_Violation(t *testing.T) {
	root := baseContractRoot(t)
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": `<html><span style="font-family: var(--font-family-mono)">code</span></html>`,
	})
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "INV-01", Type: "token_scope", Token: "font.family.mono", Allow: []string{"[data-evidence-id]"}, Targets: []string{"docs/design/prototypes/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_violation" && strings.Contains(f.Detail, "token_scope") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected token_scope violation, got %#v", report.Findings)
	}
	// now with allow match -> pass
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": `<html><span data-evidence-id="EVD-01" style="font-family: var(--font-family-mono)">code</span></html>`,
	})
	report2, _ := Check(root)
	for _, f := range report2.Findings {
		if f.Code == "project_rule_violation" && strings.Contains(f.Detail, "token_scope") {
			t.Fatalf("allow match should pass, got %#v", report2.Findings)
		}
	}
}

func TestProjectRules_RequiredImport_Violation(t *testing.T) {
	root := baseContractRoot(t)
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/proof/anchor-screens/consumer.html": "<html><body>no css link</body></html>",
	})
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-01", Type: "required_import", Import: "packages/design-tokens/tokens.css", Targets: []string{"docs/design/proof/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_violation" && strings.Contains(f.Detail, "required_import") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected required_import violation, got %#v", report.Findings)
	}
}

func TestProjectRules_ForbidLiteral_Violation(t *testing.T) {
	root := baseContractRoot(t)
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": `<html><div style="color: #ff0000">red</div></html>`,
	})
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-01", Type: "forbid_literal", Literals: []string{"#ff0000"}, Targets: []string{"docs/design/prototypes/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_violation" && strings.Contains(f.Detail, "forbid_literal") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected forbid_literal violation, got %#v", report.Findings)
	}
}

func TestProjectRules_AllFindingsAreWarning(t *testing.T) {
	root := baseContractRoot(t)
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-99", Type: "max_role_count", Role: "action.promise", Max: intPtr(1)}},
	})
	report, _ := Check(root)
	for _, f := range report.Findings {
		if f.Code == "project_rule_unverifiable" || f.Code == "project_rule_violation" {
			if f.Severity != SeverityWarning {
				t.Fatalf("project rules must be warning, got %s", f.Severity)
			}
		}
	}
}
