package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
)

// MigratePlan is the conservative planner for v1 -> contract-v1.
// It never auto-chooses investment tier, infers semantic role from color,
// or promotes screenshots to Proof. This planner never writes source files.
type MigratePlan struct {
	Root     string        `json:"root"`
	Target   string        `json:"target"`
	DryRun   bool          `json:"dry_run"`
	Mode     string        `json:"mode"` // factory | legacy-v1.0 | contract-v1
	Files    []FileMigrate `json:"files"`
	Notes    []string      `json:"notes"`
	Warnings []Finding     `json:"warnings,omitempty"`
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
		Notes:  []string{"只读迁移诊断：不修改文件；--write 和 --dry-run=false 均 unsupported。按原文含义与列出的模板逐表映射，不生成 ID 或债务占位。"},
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
	plan.Files = append(plan.Files, planFile(root, KernelRel, kernelMarkers, "对照 docs/design/DESIGN-template.md 的对应标记表；保留原文、已有 ID 和未知列，由原作者确认含义，不自动补 Binding、Proof 或 DEBT。"))
	// Grammar
	plan.Files = append(plan.Files, planFile(root, GrammarRel, grammarMarkers, "对照 docs/design/design-language-template.md 的维度、语法、绑定表；保留原文，适用状态按真实依据判断，不把缺失维度自动标成 debt。"))
	// Evidence field
	plan.Files = append(plan.Files, planFile(root, "docs/design/research/evidence-field.md", evidenceMarkers, "对照 docs/design/research/evidence-field-template.md 的 evidence-* 表；保留原证据及 F/I/R/U，不为迁移编造新的证据。"))

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
			plan.Files = append(plan.Files, planFile(root, rel, surfaceMarkers, "对照 docs/design/surface-profiles/surface-profile-template.md 的继承与变体表，按实际声明映射；迁移器不补关系。"))
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
			plan.Files = append(plan.Files, planFile(root, rel, derivationMarkers, "对照 docs/design/derivation/DERIVATION-template.md 的 Active/Must-not/Bindings/Proof 表；保留现有局部与全局归属，不生成新的 ID。"))
		}
	}

	// summarize unresolvable that need human mapping
	for i := range plan.Files {
		fm := &plan.Files[i]
		if len(fm.MissingMarkers) > 0 {
			fm.Unresolvable = append(fm.Unresolvable, "原作者按实际含义映射 Binding/Proof；static 检查须有实际 DCHK 或原内建检查支持，专业判断不能因加标记变成机器证明。")
			for _, marker := range fm.MissingMarkers {
				var columns []string
				for _, aliases := range requiredHeaders(marker) {
					columns = append(columns, strings.Join(aliases, "/"))
				}
				fm.ExpectedChanges = append(fm.ExpectedChanges, "待作者映射 "+fm.Path+" 的 <!-- foundation-contract:v1 "+marker+" --> 对应表；现有解析器所需列："+strings.Join(columns, ", ")+"。仅诊断，未插入标记或修改表格。")
			}
		}
	}
	plan.Notes = append(plan.Notes,
		"不自动选择投资档位、把颜色推断为 semantic role，或把旧截图晋升为 Proof；缺失文件先判断实际适用性，不为齐表自动创建。",
		"完成实际映射后可运行原 design-foundation check 查看结构诊断；检查绿色不代替设计语义、真实确认或产品证明。",
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
