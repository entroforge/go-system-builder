// Package impact computes which historical evidence is affected by a change.
//
// The runtime stores evidence entries with a validity status (valid / invalid /
// superseded). When a document, contract, task or BUG repair changes, any
// historical PASS evidence whose scope overlaps the changed artifact must be
// marked invalid before a clean round or acceptance gate can rely on it.
//
// This package implements the change-to-evidence-impact mapping defined by
// docs/design/loop-engineering/BUG-IMPACT-CLEAN-ROUND.md §3 and the evidence
// validity model in docs/design/loop-engineering/LOOP-RUNTIME.md §7.
package impact

import (
	"path/filepath"
	"strings"
)

// EvidenceImpact identifies one evidence entry that is affected by a change.
type EvidenceImpact struct {
	EvidenceID     string
	ScopeRef       string
	Rule           string
	Reason         string
	CurrentStatus  string
	AlreadyInvalid bool
}

// ComputeImpact walks the runtime evidence array and returns every currently
// valid evidence entry whose scope_refs overlap one of the changed paths.
//
// Impact rules (path pattern -> affected evidence):
//
//   - REQ file change  -> all evidence of the same baseline_generation
//     (the locked REQ is the root of the specification chain)
//   - contract change  -> evidence whose scope_refs reference that contract
//     path or the contracts/ directory prefix
//   - task change      -> evidence whose scope_refs reference that task path
//   - BUG repair       -> targeted_reverification evidence scoped to that BUG
//   - source change    -> builder_report / delivery_review / qa_review
//     evidence whose scope_refs reference the changed source path
//
// Already-invalid evidence is reported with AlreadyInvalid=true so callers can
// distinguish "newly affected" from "previously invalidated".
func ComputeImpact(state map[string]any, changedPaths []string) []EvidenceImpact {
	if len(changedPaths) == 0 {
		return nil
	}
	evidenceSlice, ok := state["evidence"].([]any)
	if !ok {
		return nil
	}
	baselineGeneration := readBaselineGeneration(state)
	var impacted []EvidenceImpact
	for _, raw := range evidenceSlice {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		status, _ := entry["status"].(string)
		if status == "" {
			status = "valid"
		}
		rule, reason := matchChange(entry, changedPaths, baselineGeneration)
		if rule == "" {
			continue
		}
		impacted = append(impacted, EvidenceImpact{
			EvidenceID:     id,
			ScopeRef:       reason,
			Rule:           rule,
			Reason:         ruleDescription(rule, reason),
			CurrentStatus:  status,
			AlreadyInvalid: status == "invalid",
		})
	}
	return impacted
}

// InvalidateEvidence marks every evidence entry listed in impacts as invalid.
// It mutates the evidence array inside state in place. Entries already invalid
// are left untouched (their original invalidation record is preserved).
//
// invalidatedBy is a stable reference to the transition, BUG or human decision
// that caused the invalidation (for example a transition id like "PTR-BUG-05").
// It is written into the invalidated_by field.
//
// The function returns the IDs of evidence entries it newly invalidated.
func InvalidateEvidence(state map[string]any, impacts []EvidenceImpact, invalidatedBy string) []string {
	if len(impacts) == 0 {
		return nil
	}
	evidenceSlice, ok := state["evidence"].([]any)
	if !ok {
		return nil
	}
	invalidated := make([]string, 0, len(impacts))
	impactedByID := make(map[string]EvidenceImpact, len(impacts))
	for _, item := range impacts {
		impactedByID[item.EvidenceID] = item
	}
	for _, raw := range evidenceSlice {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		item, hit := impactedByID[id]
		if !hit {
			continue
		}
		if entry["status"] == "invalid" {
			continue
		}
		entry["status"] = "invalid"
		entry["invalidated_by"] = invalidatedBy
		entry["invalidation_rule"] = item.Rule
		entry["invalidation_reason"] = item.Reason
		invalidated = append(invalidated, id)
	}
	return invalidated
}

// matchChange applies the impact rules to a single evidence entry and returns
// the (rule, matchedScopeRef) pair, or ("", "") when the entry is unaffected.
//
// RC-09 (S9-3): an evidence entry that declares no scope_refs is treated as
// full-surface sensitive. Under the previous behavior such an entry could
// never match any rule and therefore never auto-invalidated — an unrelated
// path change left an unscoped PASS evidence row "valid" forever. Fail closed:
// any changed path invalidates it and the reason says so explicitly.
func matchChange(entry map[string]any, changedPaths []string, baselineGeneration int) (string, string) {
	scopeRefs := readStringSlice(entry["scope_refs"])
	entryGeneration := readInt(entry["baseline_generation"])
	for _, changed := range changedPaths {
		normalized := normalizePath(changed)
		// Rule 1: REQ change invalidates the whole baseline generation.
		if isREQPath(normalized) && entryGeneration == baselineGeneration {
			return "req_baseline_change", normalized
		}
		// RC-09 (S9-3): unscoped evidence is full-surface sensitive.
		if len(scopeRefs) == 0 {
			return "unscoped_evidence", normalized
		}
		// Rule 2-4: scope_ref overlap with the changed path.
		for _, scope := range scopeRefs {
			normalizedScope := normalizePath(scope)
			if pathsOverlap(normalizedScope, normalized) {
				if isContractPath(normalized) {
					return "contract_change", normalized
				}
				if isTaskPath(normalized) {
					return "task_change", normalized
				}
				if isBUGPath(normalized) {
					return "bug_repair", normalized
				}
				return "scope_overlap", normalizedScope
			}
		}
	}
	return "", ""
}

func readBaselineGeneration(state map[string]any) int {
	baseline, ok := state["baseline"].(map[string]any)
	if !ok {
		return 0
	}
	return readInt(baseline["generation"])
}

func readStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func readInt(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func normalizePath(path string) string {
	cleaned := filepath.Clean(path)
	cleaned = filepath.ToSlash(cleaned)
	return cleaned
}

// pathsOverlap reports whether two normalized paths refer to the same file or
// one is a directory ancestor of the other.
func pathsOverlap(left, right string) bool {
	if left == right {
		return true
	}
	if left == "" || right == "" {
		return false
	}
	if strings.HasPrefix(left, right+"/") {
		return true
	}
	if strings.HasPrefix(right, left+"/") {
		return true
	}
	return false
}

func isREQPath(path string) bool {
	return strings.HasPrefix(path, "docs/requirements/")
}

func isContractPath(path string) bool {
	return strings.HasPrefix(path, "docs/contracts/")
}

func isTaskPath(path string) bool {
	return strings.HasPrefix(path, "docs/tasks/")
}

func isBUGPath(path string) bool {
	return strings.HasPrefix(path, "docs/reports/bugs/")
}

func ruleDescription(rule, detail string) string {
	switch rule {
	case "req_baseline_change":
		return "locked REQ changed; downstream evidence of this baseline is invalid"
	case "contract_change":
		return "contract changed; evidence scoped to this contract is invalid"
	case "task_change":
		return "task changed; evidence scoped to this task is invalid"
	case "bug_repair":
		return "BUG repair changed; targeted re-verification evidence is invalid"
	case "scope_overlap":
		return "changed artifact overlaps evidence scope"
	case "unscoped_evidence":
		return "evidence declares no scope_refs; it is full-surface sensitive and any changed path invalidates it"
	default:
		return rule
	}
}
