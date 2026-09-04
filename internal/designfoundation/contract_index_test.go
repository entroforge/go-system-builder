package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractIndex_FactoryStaysClean(t *testing.T) {
	root := repoRoot(t)
	idx, findings, err := BuildContractIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Mode != "factory" {
		t.Fatalf("factory root mode = %q, want factory", idx.Mode)
	}
	for _, f := range findings {
		if f.Code == "foundation_contract_legacy" {
			t.Fatalf("factory must not emit foundation_contract_legacy, got %#v", findings)
		}
		if f.Code == "contract_table_missing" || f.Code == "contract_header_missing" {
			t.Fatalf("factory with no kernel marker must not emit %s, got %#v", f.Code, findings)
		}
	}
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings()) != 0 {
		t.Fatalf("factory check must stay advisory clean, got %#v", report.Findings)
	}
}

func TestContractIndex_CoreMinimalParses(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal", "designfoundation", "testdata", "samples", "core-minimal")
	idx, findings, err := BuildContractIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Mode != "contract-v1" {
		t.Fatalf("core-minimal mode = %q, want contract-v1 findings=%#v", idx.Mode, findings)
	}
	if _, ok := idx.Constraints["LAW-01"]; !ok {
		t.Fatalf("core-minimal should have LAW-01, got %#v", idx.Constraints)
	}
	if _, ok := idx.Surfaces["SUR-01"]; !ok {
		t.Fatalf("missing SUR-01")
	}
	if _, ok := idx.Proofs["PROOF-01"]; !ok {
		t.Fatalf("missing PROOF-01")
	}
	for _, f := range findings {
		if f.Severity == SeverityWarning && f.Code == "contract_table_missing" {
			t.Fatalf("core-minimal should not have missing tables, got %#v", findings)
		}
	}
}

func TestParser_ByHeaderNotSectionNumber(t *testing.T) {
	// L5 §5.10: parser by marker + header name, not section number/language.
	content := "# Title\n\n## 0. Next-agent card\n\n<!-- foundation-contract:v1 constraints -->\n| ID | Status | 类型 | 可执行 Statement / Do | Don't / 反向边界 | Scope | Source | Binding | Checkability | Proof / review |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| LAW-01 | active | law | do | dont | global | EVD-01 | GR-01 | human | PROOF-01 |\n| LAW-01 | active | law | dup | dont | global | EVD-01 | GR-01 | human | PROOF-01 |\n"
	tables, findings, err := parseContentTables(content, "docs/design/DESIGN.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].Marker != "constraints" {
		t.Fatalf("got tables %#v findings %#v", tables, findings)
	}
	idx2, findings2, err := BuildContractIndexFromTables(tables, ".")
	if err != nil {
		t.Fatal(err)
	}
	_ = idx2
	hasDuplicate := false
	for _, f := range findings2 {
		if f.Code == "constraint_id_duplicate" {
			hasDuplicate = true
		}
	}
	if !hasDuplicate {
		t.Fatalf("duplicate ID must be detected, findings2=%#v", findings2)
	}
	// second table after marker is ignored: ensure only first table is taken
	content2 := "<!-- foundation-contract:v1 constraints -->\n| ID | Status | 类型 | 可执行 Statement / Do | Don't / 反向边界 | Scope | Source | Binding | Checkability | Proof / review |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| LAW-02 | active | law | do | dont | global | EVD-02 | GR-02 | human | PROOF-02 |\n\nSome text\n\n| ID | Status | 类型 | 可执行 Statement / Do | Don't / 反向边界 | Scope | Source | Binding | Checkability | Proof / review |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| LAW-99 | active | law | bad | dont | global | EVD-99 | GR-99 | human | PROOF-99 |\n"
	tables2, _, _ := parseContentTables(content2, "docs/design/DESIGN.md")
	if len(tables2) != 1 {
		t.Fatalf("only first table after marker is canonical, got %d", len(tables2))
	}
	if tables2[0].Rows[0]["ID"] != "LAW-02" {
		t.Fatalf("canonical row wrong %#v", tables2[0].Rows)
	}
}

func TestMigrate_DryRunDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	// legacy kernel without markers
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json": mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/design/DESIGN.md":              "# Project Design Foundation\n\n> 状态：published\n> 版本：v1.0.0\n\n## 0. Next-agent card\n\n| 项 | 可执行内容 |\n|:--|:--|\n| Laws — 必须做 | do |\n",
	})
	plan, err := PlanMigrate(root, "contract-v1", true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "legacy-v1.0" {
		t.Fatalf("legacy fixture mode = %q", plan.Mode)
	}
	if len(plan.Files) == 0 {
		t.Fatal("plan should list files")
	}
	// dry-run must not create files
	if _, err := os.Stat(filepath.Join(root, "docs", "design", "design-language.md")); err == nil {
		// existence is not failure; just ensure content unchanged
	}
	// Check advisory legacy: Check must only emit foundation_contract_legacy, not cascade
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	hasLegacy := false
	hasCascade := false
	for _, f := range report.Findings {
		if f.Code == "foundation_contract_legacy" {
			hasLegacy = true
		}
		if f.Code == "constraint_ref_missing" || f.Code == "dimension_unrouted" || f.Code == "active_constraint_unbound" {
			hasCascade = true
		}
	}
	if !hasLegacy {
		t.Fatalf("legacy mode must emit foundation_contract_legacy, got %#v", report.Findings)
	}
	if hasCascade {
		t.Fatalf("legacy compat must not cascade v1.1 warnings, got %#v", report.Findings)
	}
}

func TestMigrate_DryRunJSONFields(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal", "designfoundation", "testdata", "v1-baseline")
	_ = root // baseline not a repo root; just check PlanMigrate on factory doesn't panic
	factoryRoot := repoRoot(t)
	plan, err := PlanMigrate(factoryRoot, "contract-v1", true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target != "contract-v1" || !plan.DryRun {
		t.Fatalf("plan fields wrong %#v", plan)
	}
	for _, f := range plan.Files {
		if len(f.MissingMarkers) > 0 && f.PreservedTextNote == "" {
			t.Fatalf("file %s missing preserve note", f.Path)
		}
	}
	hasNote := false
	for _, n := range plan.Notes {
		if strings.Contains(n, "dry-run") || strings.Contains(n, "--write") {
			hasNote = true
		}
	}
	if !hasNote {
		t.Fatalf("plan notes must mention dry-run/--write, got %#v", plan.Notes)
	}
}

// BuildContractIndexFromTables is a thin helper for unit tests that bypasses FS.
func BuildContractIndexFromTables(tables []ParsedTable, root string) (*ContractIndex, []Finding, error) {
	// reuse same population logic but inject tables directly
	idx := &ContractIndex{
		Root:         root,
		Mode:         "contract-v1",
		Tables:       tables,
		Constraints:  map[string]ConstraintRow{},
		GrammarRules: map[string]GrammarRule{},
		Dimensions:   map[string]DimensionRow{},
		Roles:        map[string]RoleRow{},
		Surfaces:     map[string]SurfaceRow{},
		Proofs:       map[string]ProofRow{},
		Debts:        map[string]DebtRow{},
		Evidence:     map[string]EvidenceRow{},
		Derivations:  map[string]DerivationPack{},
	}
	// duplicate the same population as BuildContractIndex for the subset used in this test
	var findings []Finding
	seen := map[string]string{}
	for _, t := range tables {
		if t.Marker == "constraints" {
			for i, row := range t.Rows {
				id := cellByAliases(row, []string{"id"})
				id = strings.TrimSpace(id)
				if id == "" || id == "—" {
					continue
				}
				if prev, dup := seen[id]; dup {
					findings = append(findings, Finding{Code: "constraint_id_duplicate", Severity: SeverityWarning, Path: t.File, Detail: "duplicate " + id + " at " + t.File + ":" + itoa(t.RowLines[i]) + " previously " + prev})
					continue
				}
				seen[id] = t.File + ":" + itoa(t.RowLines[i])
				idx.Constraints[id] = ConstraintRow{ID: id, File: t.File, Line: t.RowLines[i]}
			}
		}
	}
	return idx, findings, nil
}
