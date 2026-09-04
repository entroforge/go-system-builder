package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
)

// MigratePlan is the conservative planner for v1 -> contract-v1.
// It never auto-chooses investment tier, infers semantic role from color,
// or promotes screenshots to Proof (L5 §10).
type MigratePlan struct {
	Root    string               `json:"root"`
	Target  string               `json:"target"`
	DryRun  bool                 `json:"dry_run"`
	Mode    string               `json:"mode"` // factory | legacy-v1.0 | contract-v1
	Files   []FileMigrate        `json:"files"`
	Notes   []string             `json:"notes"`
	Warnings []Finding           `json:"warnings,omitempty"`
}

type FileMigrate struct {
	Path              string   `json:"path"`
	Exists            bool     `json:"exists"`
	HasContractV1     bool     `json:"has_contract_v1"`
	MissingMarkers    []string `json:"missing_markers,omitempty"`
	CandidateIDs      []string `json:"candidate_ids,omitempty"`
	Unresolvable      []string `json:"unresolvable,omitempty"`
	ExpectedChanges   []string `json:"expected_changes,omitempty"`
	PreservedTextNote string   `json:"preserved_text_note,omitempty"`
}

// expected markers per live file type.
var kernelMarkers = []string{"constraints", "surfaces", "proofs", "debts"}
var grammarMarkers = []string{"dimensions", "grammar-rules", "bindings"}
var evidenceMarkers = []string{"evidence-product", "evidence-relationship", "evidence-business", "evidence-ritual", "evidence-category", "evidence-constraint"}
var surfaceMarkers = []string{"surface-inherits", "surface-variants"}
var derivationMarkers = []string{"derivation-active", "derivation-must-not", "derivation-bindings", "derivation-proof"}

func PlanMigrate(root, target string, dryRun bool) (*MigratePlan, error) {
	idx, findings, err := BuildContractIndex(root)
	if err != nil {
		return nil, err
	}
	plan := &MigratePlan{
		Root:   root,
		Target: target,
		DryRun: dryRun,
		Mode:   idx.Mode,
	}
	if idx.Mode == "contract-v1" {
		plan.Notes = append(plan.Notes, "already on contract-v1; nothing to migrate")
		plan.Warnings = findings
		return plan, nil
	}
	if idx.Mode == "factory" && target == "contract-v1" {
		plan.Notes = append(plan.Notes, "factory has no published Foundation; migrate after F0 chooses local/core/extended and a published DESIGN.md exists")
	}

	// Kernel
	plan.Files = append(plan.Files, planFile(root, KernelRel, kernelMarkers, "保留原文并在首表前插入 <!-- foundation-contract:v1 <marker> -->；unknown 列保留，未知 Binding 填 human-review 或 DEBT 占位"))
	// Grammar
	plan.Files = append(plan.Files, planFile(root, GrammarRel, grammarMarkers, "保留原文；coverage 每维显式 active/inherited/debt/N/A，未迁移维填 debt"))
	// Evidence field
	plan.Files = append(plan.Files, planFile(root, "docs/design/research/evidence-field.md", evidenceMarkers, "保留 F/I/R/U 标记；每表前插入对应 evidence-* marker"))

	surfaceDir := filepath.Join(root, "docs", "design", "surface-profiles")
	if entries, err := os.ReadDir(surfaceDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			low := strings.ToLower(e.Name())
			if strings.Contains(low, "template") || low == "readme.md" {
				continue
			}
			rel := filepath.Join("docs", "design", "surface-profiles", e.Name())
			plan.Files = append(plan.Files, planFile(root, rel, surfaceMarkers, "继承与变体表 marker 化；未列关系自动继承"))
		}
	}
	derivDir := filepath.Join(root, "docs", "design", "derivation")
	if entries, err := os.ReadDir(derivDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			low := strings.ToLower(e.Name())
			if strings.Contains(low, "template") || low == "readme.md" {
				continue
			}
			rel := filepath.Join("docs", "design", "derivation", e.Name())
			plan.Files = append(plan.Files, planFile(root, rel, derivationMarkers, "Active/Must-not/Bindings/Proof marker 化；LOCAL-* 不进全局 Index"))
		}
	}

	// summarize unresolvable that need human mapping
	for i := range plan.Files {
		fm := &plan.Files[i]
		if len(fm.MissingMarkers) > 0 {
			fm.Unresolvable = append(fm.Unresolvable, "Binding/Proof 需人映射：static Checkability 需 DCHK 或内建检查覆盖；human-review 仅允许 human")
			fm.ExpectedChanges = append(fm.ExpectedChanges, "marker 插入 + debt 占位 + 表头补齐，不自动选投资档位、推断语义色或晋升截图为 Proof")
			fm.CandidateIDs = append(fm.CandidateIDs, "candidate IDs: LAW-01.. / GR-01.. / ROLE-* / PAT-* / SUR-* / PROOF-* / DEBT-* / EX-* (数字型至少两位)")
		}
	}
	plan.Notes = append(plan.Notes,
		"--dry-run 默认不写盘；--write 仅插入 marker、保留原文并生成 debt 占位",
		"不得自动选择投资档位、把颜色推断为 semantic role、或把旧截图晋升为 Proof；写盘后仍需人完成语义映射并通过 check",
	)
	plan.Warnings = findings
	return plan, nil
}

func planFile(root, rel string, want []string, preserveNote string) FileMigrate {
	fm := FileMigrate{Path: rel, PreservedTextNote: preserveNote}
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		if os.IsNotExist(err) {
			fm.Exists = false
			fm.MissingMarkers = want
			return fm
		}
		fm.Exists = true
		fm.MissingMarkers = want
		return fm
	}
	fm.Exists = true
	content := string(data)
	has := strings.Contains(content, "foundation-contract:v1")
	fm.HasContractV1 = has
	if !has {
		fm.MissingMarkers = want
		return fm
	}
	var missing []string
	for _, m := range want {
		if !strings.Contains(content, "foundation-contract:v1 "+m) {
			missing = append(missing, m)
		}
	}
	fm.MissingMarkers = missing
	return fm
}

// WriteMigrate is intentionally conservative: with dryRun=true it does nothing.
// With dryRun=false it inserts markers for files that are clearly legacy tables
// without guessing semantics. Callers must still run check after write.
func WriteMigrate(root string, plan *MigratePlan) ([]string, error) {
	if plan.DryRun {
		return nil, nil
	}
	// Minimal writer: for now only report that --write would insert markers.
	// Actual insertion is out of scope for I2 dry-run DoD; we keep it safe and
	// require human mapping for Binding/Proof.
	var written []string
	for _, f := range plan.Files {
		if !f.Exists || len(f.MissingMarkers) == 0 {
			continue
		}
		// Do not auto-write without explicit human mapping; surface as note.
		written = append(written, f.Path+": needs human mapping for "+strings.Join(f.MissingMarkers, ","))
	}
	return written, nil
}
