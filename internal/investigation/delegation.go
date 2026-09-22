package investigation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/repairpolicy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// RepairDelegation is explicit human authority for a finite number of technical
// contract reviews. It is not a release/REQ-amendment approval and is never
// inferred from an actor name or migrated onto an existing REQ.
type RepairDelegation struct {
	Decision           string   `json:"decision"`
	DecisionID         string   `json:"decision_id"`
	RuntimeID          string   `json:"runtime_id"`
	REQSHA256          string   `json:"req_sha256"`
	BaselineGeneration int      `json:"baseline_generation"`
	ApprovedBy         string   `json:"approved_by"`
	Reviewer           string   `json:"reviewer"`
	AllowedPaths       []string `json:"allowed_paths"`
	ForbiddenPaths     []string `json:"forbidden_paths"`
	MaxContracts       int      `json:"max_contracts"`
	ExpiresAt          string   `json:"expires_at"`
}

type RepairTechnicalReview struct {
	Decision                   string   `json:"decision"`
	RuntimeID                  string   `json:"runtime_id"`
	CaseID                     string   `json:"case_id"`
	ContractID                 string   `json:"contract_id"`
	ApprovalHash               string   `json:"approval_hash"`
	ReviewedBy                 string   `json:"reviewed_by"`
	DelegationEvidenceID       string   `json:"delegation_evidence_id"`
	RequirementsUnchanged      bool     `json:"requirements_unchanged"`
	BusinessSemanticsUnchanged bool     `json:"business_semantics_unchanged"`
	Reversible                 bool     `json:"reversible"`
	ScopeComplete              bool     `json:"scope_complete"`
	EvidenceRefs               []string `json:"evidence_refs"`
	Rationale                  string   `json:"rationale"`
}

func decodeStrict(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON document")
	}
	return nil
}

// registeredArtifact checks current-generation identity and actual bytes. No
// Markdown legacy exception is permitted for delegated authority.
func registeredArtifact(root string, state map[string]any, id, kind string) (map[string]any, []byte, error) {
	generation, err := baselineGeneration(state)
	if err != nil {
		return nil, nil, err
	}
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if stringField(row["id"]) != id {
			continue
		}
		if stringField(row["kind"]) != kind || stringField(row["status"]) != "valid" || integerValueOrZero(row["baseline_generation"]) != generation {
			return nil, nil, fmt.Errorf("evidence %s is not current valid %s", id, kind)
		}
		rel := stringField(row["path"])
		if err := literalRepositoryPath(root, rel); err != nil {
			return nil, nil, err
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, nil, err
		}
		if sha256Hex(data) != stringField(row["sha256"]) {
			return nil, nil, fmt.Errorf("evidence %s sha256 drift", id)
		}
		return row, data, nil
	}
	return nil, nil, fmt.Errorf("evidence %s is not registered", id)
}

func literalRepositoryPath(root, path string) error {
	if path == "" || path == "." || filepath.IsAbs(path) || strings.ContainsAny(path, "\\*?[]:") || filepath.ToSlash(filepath.Clean(path)) != path || path == ".." || strings.HasPrefix(path, "../") {
		return fmt.Errorf("delegation requires literal repository-relative paths: %q", path)
	}
	current := root
	for _, part := range strings.Split(path, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("delegation path contains symlink: %s", path)
		}
	}
	return nil
}
func within(path, dir string) bool { return path == dir || strings.HasPrefix(path, dir+"/") }
func overlaps(a, b string) bool    { return within(a, b) || within(b, a) }

var delegatedProtectedPaths = []string{".git", ".claude", ".agents", ".codex", "AGENTS.md", "CLAUDE.md", "AGENTS-template.md", "settings.json", "hooks", "packaging", "prelude.md", "loop-template.md", "docs/requirements", "docs/dev/contracts", "docs/dev/tasks", "docs/design", "docs/control", "docs/architecture", "docs/reports/release-audits", "loop-harness.md", "agents", "skills", "internal/schema", "internal/investigation", "internal/runtime", "internal/policy", "internal/cli", "internal/hook", "internal/transition"}

func validateDelegatedApproval(root string, state, draft map[string]any, req ContractRequest) (map[string]any, error) {
	draftBytes, err := json.Marshal(draft)
	if err != nil {
		return nil, err
	}
	if err := repair.ValidateContractExecutionMap(draftBytes); err != nil {
		return nil, err
	}
	grantRow, grant, boundPolicy, err := resolveRepairAuthority(root, state, req.DelegationEvidenceID)
	if err != nil {
		return nil, err
	}
	generation, err := baselineGeneration(state)
	if err != nil {
		return nil, err
	}
	bound, _ := state["bound_req"].(map[string]any)
	if grant.Decision != "delegate_bounded_repair" || grant.DecisionID != req.DelegationEvidenceID || grant.RuntimeID != stringField(state["runtime_id"]) || grant.BaselineGeneration != generation || grant.REQSHA256 == "" || grant.REQSHA256 != stringField(bound["sha256"]) || stringField(bound["status"]) != "locked" {
		return nil, fmt.Errorf("delegation does not bind this locked REQ/runtime/generation")
	}
	// The grant must come from the recorded requirement approver, not the Driver.
	if grant.ApprovedBy == "" || grant.ApprovedBy != stringField(bound["approved_by"]) || !containsStringAny(grantRow["produced_by"], grant.ApprovedBy) || grant.Reviewer == "" || grant.Reviewer == grant.ApprovedBy || grant.Reviewer != req.ApprovedBy {
		return nil, fmt.Errorf("delegation human/reviewer identity mismatch")
	}
	if !containsStringAny(grantRow["scope_refs"], "s8_repair_delegation:"+grant.RuntimeID) {
		return nil, fmt.Errorf("delegation scope mismatch")
	}
	expires, err := time.Parse(time.RFC3339, grant.ExpiresAt)
	if (!boundPolicy || grant.ExpiresAt != "") && (err != nil || !time.Now().Before(expires)) {
		return nil, fmt.Errorf("delegation expiry missing or expired")
	}
	if grant.MaxContracts < 1 || delegationUseCount(state, req.DelegationEvidenceID) >= grant.MaxContracts {
		return nil, fmt.Errorf("delegation contract budget exhausted or invalid")
	}
	if len(grant.AllowedPaths) == 0 {
		return nil, fmt.Errorf("delegation allowed_paths is empty")
	}
	for _, path := range append(append([]string{}, grant.AllowedPaths...), grant.ForbiddenPaths...) {
		if err := literalRepositoryPath(root, path); err != nil {
			return nil, err
		}
	}
	// Re-read locked REQ bytes. No use across an unrecorded REQ change.
	reqPath := stringField(bound["path"])
	if err := literalRepositoryPath(root, reqPath); err != nil {
		return nil, err
	}
	reqBytes, err := os.ReadFile(filepath.Join(root, reqPath))
	if err != nil || sha256Hex(reqBytes) != grant.REQSHA256 {
		return nil, fmt.Errorf("locked REQ bytes drifted")
	}
	paths, err := stringSlice(draft["prospective_scope"], "prospective_scope")
	if err != nil {
		return nil, err
	}
	forbidden := append(append([]string{}, delegatedProtectedPaths...), grant.ForbiddenPaths...)
	for _, path := range paths {
		if err := literalRepositoryPath(root, path); err != nil {
			return nil, err
		}
		allowed := false
		for _, prefix := range grant.AllowedPaths {
			if within(path, prefix) {
				allowed = true
			}
		}
		if !allowed {
			return nil, fmt.Errorf("repair path %s exceeds delegated scope", path)
		}
		for _, prefix := range forbidden {
			if overlaps(path, prefix) {
				return nil, fmt.Errorf("repair path %s overlaps forbidden path %s", path, prefix)
			}
		}
	}
	reviewRow, reviewBytes, err := registeredArtifact(root, state, req.ApprovalEvidenceID, "repair_contract_review")
	if err != nil {
		return nil, err
	}
	var review RepairTechnicalReview
	if err := decodeStrict(reviewBytes, &review); err != nil {
		return nil, err
	}
	if review.Decision != "approve_bounded_repair" || review.RuntimeID != grant.RuntimeID || review.CaseID != req.CaseID || review.ContractID != stringField(draft["repair_contract_id"]) || review.ApprovalHash != req.ApprovalHash || review.ReviewedBy != grant.Reviewer || review.DelegationEvidenceID != req.DelegationEvidenceID || !containsStringAny(reviewRow["produced_by"], grant.Reviewer) || !containsStringAny(reviewRow["scope_refs"], "s8_contract_review:"+req.CaseID) {
		return nil, fmt.Errorf("technical review identity/hash/scope mismatch")
	}
	if !review.RequirementsUnchanged || !review.BusinessSemanticsUnchanged || !review.Reversible || !review.ScopeComplete || strings.TrimSpace(review.Rationale) == "" || len(review.EvidenceRefs) == 0 {
		return nil, fmt.Errorf("technical review must establish unchanged semantics, reversibility, complete scope and evidence")
	}
	for _, id := range review.EvidenceRefs {
		if id == req.ApprovalEvidenceID || id == req.DelegationEvidenceID {
			return nil, fmt.Errorf("technical review evidence cannot be its own authority")
		}
		rows, _ := state["evidence"].([]any)
		kind := ""
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if stringField(row["id"]) == id {
				kind = stringField(row["kind"])
			}
		}
		if _, _, err := registeredArtifact(root, state, id, kind); err != nil {
			return nil, fmt.Errorf("technical review supporting evidence: %w", err)
		}
	}
	return map[string]any{"mode": "delegated", "delegation_evidence_id": req.DelegationEvidenceID, "delegation_sha256": stringField(grantRow["sha256"]), "human_authorizer": grant.ApprovedBy, "review_evidence_id": req.ApprovalEvidenceID, "review_sha256": stringField(reviewRow["sha256"])}, nil
}
func delegationUseCount(state map[string]any, id string) int {
	config, _ := state["configuration"].(map[string]any)
	repair, _ := config["repair"].(map[string]any)
	uses, _ := repair["delegation_uses"].(map[string]any)
	return integerValueOrZero(uses[id])
}
func consumeDelegationUse(state map[string]any, id string) error {
	config, _ := state["configuration"].(map[string]any)
	repair, _ := config["repair"].(map[string]any)
	if repair == nil {
		return fmt.Errorf("configuration.repair is missing")
	}
	uses, _ := repair["delegation_uses"].(map[string]any)
	if uses == nil {
		uses = map[string]any{}
		repair["delegation_uses"] = uses
	}
	uses[id] = delegationUseCount(state, id) + 1
	return nil
}

// A pinned binding policy replaces per-REQ grant authoring, not the independent
// technical review. Policy changes and REQ amendments invalidate its authority.
func resolveRepairAuthority(root string, state map[string]any, id string) (map[string]any, RepairDelegation, bool, error) {
	var grant RepairDelegation
	if !strings.HasPrefix(id, "binding-policy:") {
		row, data, err := registeredArtifact(root, state, id, "human_decision")
		if err != nil {
			return nil, grant, false, err
		}
		if runtime.HumanDecisionEvidenceConsumed(row) {
			return nil, grant, false, fmt.Errorf("delegation evidence was consumed by another operation")
		}
		err = decodeStrict(data, &grant)
		return row, grant, false, err
	}
	config, _ := state["configuration"].(map[string]any)
	repairConfig, _ := config["repair"].(map[string]any)
	pin, _ := repairConfig["bound_policy"].(map[string]any)
	bound, _ := state["bound_req"].(map[string]any)
	generation, err := baselineGeneration(state)
	if err != nil {
		return nil, grant, true, err
	}
	sha := stringField(pin["sha256"])
	if sha == "" || id != "binding-policy:"+sha || stringField(pin["runtime_id"]) != stringField(state["runtime_id"]) || integerValueOrZero(pin["baseline_generation"]) != generation || stringField(pin["req_sha256"]) != stringField(bound["sha256"]) || stringField(pin["approved_by"]) != stringField(bound["approved_by"]) {
		return nil, grant, true, fmt.Errorf("repair policy was not authorized for this REQ binding")
	}
	p, err := repairpolicy.Read(root, stringField(pin["path"]), sha, stringField(bound["approved_by"]))
	if err != nil {
		return nil, grant, true, err
	}
	grant = RepairDelegation{Decision: "delegate_bounded_repair", DecisionID: id, RuntimeID: stringField(state["runtime_id"]), REQSHA256: stringField(bound["sha256"]), BaselineGeneration: generation, ApprovedBy: p.ApprovedBy, Reviewer: p.Reviewer, AllowedPaths: p.AllowedPaths, ForbiddenPaths: p.ForbiddenPaths, MaxContracts: p.MaxContracts, ExpiresAt: p.ExpiresAt}
	return map[string]any{"sha256": sha, "produced_by": []any{p.ApprovedBy}, "scope_refs": []any{"s8_repair_delegation:" + grant.RuntimeID}}, grant, true, nil
}
