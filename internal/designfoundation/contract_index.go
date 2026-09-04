package designfoundation

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ContractIndex is the in-memory projection of all stable tables.
// It is advisory and never writes a second authority file (L5 §9.1).
type ContractIndex struct {
	Root string
	Mode string // "factory" | "legacy-v1.0" | "contract-v1"

	Tables []ParsedTable

	Constraints map[string]ConstraintRow
	GrammarRules map[string]GrammarRule
	Dimensions  map[string]DimensionRow
	Roles       map[string]RoleRow
	Surfaces    map[string]SurfaceRow
	Proofs      map[string]ProofRow
	Debts       map[string]DebtRow
	Evidence    map[string]EvidenceRow
	Derivations map[string]DerivationPack // key: REQ id, e.g. "014"
}

type ConstraintRow struct {
	ID           string
	Status       string
	Type         string
	Scope        string
	Source       string
	Binding      string
	Checkability string
	Proof        string
	File         string
	Line         int
}

type GrammarRule struct {
	ID         string
	Dimensions string
	Source     string
	Binding    string
	Proof      string
	File       string
	Line       int
}

type DimensionRow struct {
	Name    string
	Status  string
	Source  string
	Inherit string
	Proof   string
	File    string
	Line    int
}

type RoleRow struct {
	ID     string
	Source string
	Token  string
	Status string
	File   string
	Line   int
}

type SurfaceRow struct {
	ID      string
	Surface string
	Profile string
	Proof   string
	File    string
	Line    int
}

type ProofRow struct {
	ID   string
	Type string
	Path string
	Cover string
	File string
	Line int
}

type DebtRow struct {
	ID     string
	Item   string
	Impact string
	Review string
	File   string
	Line   int
}

type EvidenceRow struct {
	ID     string
	File   string
	Line   int
	Marker string
}

type DerivationPack struct {
	REQID    string
	File     string
	Active   []ParsedTable
	MustNot  []ParsedTable
	Bindings []ParsedTable
	Proofs   []ParsedTable
}

// Regex for ID prefix validation (L5 §5.10).
var idRe = regexp.MustCompile(`^(EVD|LAW|ANTI|INV|GR|SUR|PROOF|DEBT|EX|DCHK)-[A-Za-z0-9_\-]+$`)
var rolePatRe = regexp.MustCompile(`^(ROLE|PAT)-[a-z0-9\-]+$`)

func isValidID(id string) bool {
	if id == "" || id == "—" || id == "-" {
		return false
	}
	if idRe.MatchString(id) {
		return true
	}
	if rolePatRe.MatchString(id) {
		return true
	}
	// allow LOCAL-* in derivations but not in index
	if strings.HasPrefix(id, "LOCAL-") {
		return true
	}
	return false
}

// BuildContractIndex parses all stable tables and classifies mode.
// It never fails the build on parse warnings – they are returned as findings.
// Caller decides whether to surface them (advisory, default exit 0).
func BuildContractIndex(root string) (*ContractIndex, []Finding, error) {
	tables, parseFindings, err := collectAllTables(root)
	if err != nil {
		return nil, nil, err
	}
	idx := &ContractIndex{
		Root:         root,
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
	findings := append([]Finding(nil), parseFindings...)

	// Detect mode.
	kernelPath := filepath.Join(root, KernelRel)
	kernelData, kernelErr := os.ReadFile(kernelPath)
	hasKernel := kernelErr == nil && len(kernelData) > 0
	kernelStatus := ""
	if hasKernel {
		kernelStatus = kernelStatusFromBytes(kernelData)
	}
	hasAnyMarker := len(tables) > 0
	// Also consider markers inside derivation/surface files – already counted in tables.
	switch {
	case !hasKernel && !hasAnyMarker:
		idx.Mode = "factory"
	case hasKernel && kernelStatus == "published" && !hasAnyMarker:
		idx.Mode = "legacy-v1.0"
		findings = append(findings, Finding{
			Code:     "foundation_contract_legacy",
			Severity: SeverityInfo,
			Path:     KernelRel,
			Detail:   "published Foundation without foundation-contract:v1 markers; running in v1.0 compatibility mode (P4 checks only). Run loop-harness design-foundation migrate --to contract-v1 --dry-run to preview migration.",
		})
	case hasAnyMarker:
		idx.Mode = "contract-v1"
	default:
		idx.Mode = "factory"
	}

	// Populate maps from tables (even in legacy mode we populate what exists for completeness).
	seenID := map[string]string{} // id -> first file:line for duplicate detection (deferred to DF-T21 but we emit here for early feedback)
	for _, t := range tables {
		switch t.Marker {
		case "constraints":
			for i, row := range t.Rows {
				id := cellByAliases(row, []string{"id"})
				if id == "" || id == "—" {
					continue
				}
				id = strings.TrimSpace(id)
				if !isValidID(id) {
					findings = append(findings, Finding{
						Code:     "constraint_id_invalid",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "marker constraints row " + itoa(t.RowLines[i]) + " has invalid ID " + id,
					})
					continue
				}
				if prev, dup := seenID[id]; dup {
					findings = append(findings, Finding{
						Code:     "constraint_id_duplicate",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "duplicate ID " + id + " at " + t.File + ":" + itoa(t.RowLines[i]) + " previously " + prev,
					})
					continue
				}
				seenID[id] = t.File + ":" + itoa(t.RowLines[i])
				idx.Constraints[id] = ConstraintRow{
					ID:           id,
					Status:       cellByAliases(row, []string{"status", "状态"}),
					Type:         cellByAliases(row, []string{"type", "类型"}),
					Scope:        cellByAliases(row, []string{"scope"}),
					Source:       cellByAliases(row, []string{"source"}),
					Binding:      cellByAliases(row, []string{"binding"}),
					Checkability: cellByAliases(row, []string{"checkability"}),
					Proof:        cellByAliases(row, []string{"proof", "proof / review", "证明"}),
					File:         t.File,
					Line:         t.RowLines[i],
				}
			}
		case "dimensions":
			for i, row := range t.Rows {
				name := cellByAliases(row, []string{"维度", "dimension"})
				if name == "" {
					continue
				}
				status := cellByAliases(row, []string{"status", "状态"})
				idx.Dimensions[name] = DimensionRow{
					Name:    name,
					Status:  status,
					Source:  cellByAliases(row, []string{"依据约束", "source"}),
					Inherit: cellByAliases(row, []string{"inherit", "继承源", "继承源或 debt"}),
					Proof:   cellByAliases(row, []string{"proof", "证明"}),
					File:    t.File,
					Line:    t.RowLines[i],
				}
			}
		case "grammar-rules":
			for i, row := range t.Rows {
				id := cellByAliases(row, []string{"id"})
				if id == "" || id == "—" {
					continue
				}
				id = strings.TrimSpace(id)
				if !isValidID(id) && !strings.HasPrefix(id, "GR-") {
					findings = append(findings, Finding{
						Code:     "grammar_id_invalid",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "marker grammar-rules row " + itoa(t.RowLines[i]) + " has invalid ID " + id,
					})
					continue
				}
				if prev, dup := seenID[id]; dup {
					findings = append(findings, Finding{
						Code:     "constraint_id_duplicate",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "duplicate ID " + id + " at " + t.File + ":" + itoa(t.RowLines[i]) + " previously " + prev,
					})
					continue
				}
				seenID[id] = t.File + ":" + itoa(t.RowLines[i])
				idx.GrammarRules[id] = GrammarRule{
					ID:         id,
					Dimensions: cellByAliases(row, []string{"dimensions", "维度"}),
					Source:     cellByAliases(row, []string{"source", "source constraints"}),
					Binding:    cellByAliases(row, []string{"binding", "bindings"}),
					Proof:      cellByAliases(row, []string{"proof", "证明"}),
					File:       t.File,
					Line:       t.RowLines[i],
				}
			}
		case "bindings":
			for i, row := range t.Rows {
				id := cellByAliases(row, []string{"id"})
				if id == "" || id == "—" {
					continue
				}
				id = strings.TrimSpace(id)
				if !isValidID(id) {
					findings = append(findings, Finding{
						Code:     "role_id_invalid",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "marker bindings row " + itoa(t.RowLines[i]) + " invalid ID " + id,
					})
					continue
				}
				if prev, dup := seenID[id]; dup {
					findings = append(findings, Finding{
						Code:     "constraint_id_duplicate",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "duplicate ID " + id + " at " + t.File + ":" + itoa(t.RowLines[i]) + " previously " + prev,
					})
					continue
				}
				seenID[id] = t.File + ":" + itoa(t.RowLines[i])
				idx.Roles[id] = RoleRow{
					ID:     id,
					Source: cellByAliases(row, []string{"source", "source gr-*"}),
					Token:  cellByAliases(row, []string{"token", "token / component / pattern", "component"}),
					Status: cellByAliases(row, []string{"status", "状态"}),
					File:   t.File,
					Line:   t.RowLines[i],
				}
			}
		case "surfaces":
			for i, row := range t.Rows {
				id := cellByAliases(row, []string{"id"})
				if id == "" {
					continue
				}
				id = strings.TrimSpace(id)
				if prev, dup := seenID[id]; dup {
					findings = append(findings, Finding{
						Code:     "constraint_id_duplicate",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "duplicate ID " + id + " at " + t.File + ":" + itoa(t.RowLines[i]) + " previously " + prev,
					})
					continue
				}
				seenID[id] = t.File + ":" + itoa(t.RowLines[i])
				idx.Surfaces[id] = SurfaceRow{
					ID:      id,
					Surface: cellByAliases(row, []string{"surface"}),
					Profile: cellByAliases(row, []string{"profile", "profile/version", "版本"}),
					Proof:   cellByAliases(row, []string{"proof", "对比证明", "证明"}),
					File:    t.File,
					Line:    t.RowLines[i],
				}
			}
		case "proofs":
			for i, row := range t.Rows {
				id := cellByAliases(row, []string{"id"})
				if id == "" {
					continue
				}
				id = strings.TrimSpace(id)
				if prev, dup := seenID[id]; dup {
					findings = append(findings, Finding{
						Code:     "constraint_id_duplicate",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "duplicate ID " + id + " at " + t.File + ":" + itoa(t.RowLines[i]) + " previously " + prev,
					})
					continue
				}
				seenID[id] = t.File + ":" + itoa(t.RowLines[i])
				idx.Proofs[id] = ProofRow{
					ID:    id,
					Type:  cellByAliases(row, []string{"类型", "type"}),
					Path:  cellByAliases(row, []string{"路径", "path"}),
					Cover: cellByAliases(row, []string{"证明哪些约束", "covered"}),
					File:  t.File,
					Line:  t.RowLines[i],
				}
			}
		case "debts":
			for i, row := range t.Rows {
				id := cellByAliases(row, []string{"id"})
				if id == "" {
					continue
				}
				id = strings.TrimSpace(id)
				if prev, dup := seenID[id]; dup {
					findings = append(findings, Finding{
						Code:     "constraint_id_duplicate",
						Severity: SeverityWarning,
						Path:     t.File,
						Detail:   "duplicate ID " + id + " at " + t.File + ":" + itoa(t.RowLines[i]) + " previously " + prev,
					})
					continue
				}
				seenID[id] = t.File + ":" + itoa(t.RowLines[i])
				idx.Debts[id] = DebtRow{
					ID:     id,
					Item:   cellByAliases(row, []string{"项", "item"}),
					Impact: cellByAliases(row, []string{"影响", "impact"}),
					Review: cellByAliases(row, []string{"复查", "复查条件", "review"}),
					File:   t.File,
					Line:   t.RowLines[i],
				}
			}
		case "surface-inherits", "surface-variants", "derivation-active", "derivation-must-not", "derivation-bindings", "derivation-proof":
			// deriv tables are grouped per REQ file; collect separately
		}
		if strings.HasPrefix(t.Marker, "evidence-") {
			for i, row := range t.Rows {
				id := cellByAliases(row, []string{"id"})
				if id == "" || id == "—" {
					continue
				}
				id = strings.TrimSpace(id)
				idx.Evidence[id] = EvidenceRow{ID: id, File: t.File, Line: t.RowLines[i], Marker: t.Marker}
			}
		}
	}

	// Second pass: group derivations by file
	for _, t := range tables {
		switch t.Marker {
		case "derivation-active", "derivation-must-not", "derivation-bindings", "derivation-proof":
			reqID := deriveREQID(t.File)
			pack := idx.Derivations[reqID]
			pack.REQID = reqID
			pack.File = t.File
			switch t.Marker {
			case "derivation-active":
				pack.Active = append(pack.Active, t)
			case "derivation-must-not":
				pack.MustNot = append(pack.MustNot, t)
			case "derivation-bindings":
				pack.Bindings = append(pack.Bindings, t)
			case "derivation-proof":
				pack.Proofs = append(pack.Proofs, t)
			}
			idx.Derivations[reqID] = pack
		case "surface-inherits", "surface-variants":
			// surfaces already handled; variant/inherits are structural but we keep raw tables
		}
	}

	return idx, findings, nil
}

func cellByAliases(row map[string]string, aliases []string) string {
	for _, a := range aliases {
		al := strings.ToLower(a)
		for k, v := range row {
			if strings.ToLower(k) == al || strings.Contains(strings.ToLower(k), al) {
				if strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
	}
	// fallback: try contains
	for _, a := range aliases {
		al := strings.ToLower(a)
		for k, v := range row {
			if strings.Contains(strings.ToLower(k), al) {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func deriveREQID(file string) string {
	base := filepath.Base(file)
	// REQ-{id}.md
	if m := reqFile.FindStringSubmatch(base); m != nil {
		return m[1]
	}
	return base
}

func kernelStatusFromBytes(data []byte) string {
	return kernelStatus(string(data))
}
