// Package acceptance owns the small, machine-readable S10 audit manifest.
//
// The manifest is the single source: the Markdown ACC and release-audit
// reports are rendered from it (RenderMarkdown) — they are the
// human-readable projection, never a second hand-maintained carrier. This
// package validates the finite completion ledger that the Quality Gate needs
// to consume; it does not attempt to parse prose tables.
package acceptance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
)

const (
	SchemaVersionAcceptance = "1.0.0"
	ManifestAcceptance      = "acceptance"
	ManifestReleaseAudit    = "release_audit"
)

// AuditAreaIDs is the finite release-audit denominator from L3-S10 §4.5.
var AuditAreaIDs = []string{
	"state_machine",
	"transaction_uow",
	"concurrency_idempotency",
	"data_migration",
	"call_sites_topology",
	"observability_errors",
	"verification_evidence",
	"docs_release_scope",
}

// Manifest is the structured completion ledger referenced by an S10
// acceptance or release-audit evidence envelope.
type Manifest struct {
	SchemaVersion      string                `json:"schema_version"`
	ManifestType       string                `json:"manifest_type"`
	RuntimeID          string                `json:"runtime_id"`
	BaselineGeneration int                   `json:"baseline_generation"`
	ReviewRound        int                   `json:"review_round"`
	CoverageInventory  []CoverageItem        `json:"coverage_inventory"`
	Counterevidence    []CounterevidenceItem `json:"counterevidence"`
	AuditAreas         []AuditArea           `json:"audit_areas,omitempty"`
	Risks              []RiskItem            `json:"risks"`
	TechnicalDebt      []DebtItem            `json:"technical_debt"`
	BlockingFindings   []BlockingFinding     `json:"blocking_findings"`
	Metrics            Metrics               `json:"metrics"`
}

// CoverageItem is one finite object in the frozen S10 review denominator.
type CoverageItem struct {
	ID           string   `json:"id"`
	Category     string   `json:"category"`
	SourceRefs   []string `json:"source_refs"`
	Expected     string   `json:"expected"`
	Oracle       string   `json:"oracle"`
	Owner        string   `json:"owner"`
	EvidenceRefs []string `json:"evidence_refs"`
	Disposition  string   `json:"disposition"`
	NAReason     string   `json:"na_reason,omitempty"`
}

// CounterevidenceItem records the deliberate attempt to disprove one
// coverage conclusion.
type CounterevidenceItem struct {
	ID           string   `json:"id"`
	InventoryID  string   `json:"inventory_id"`
	Question     string   `json:"question"`
	EvidenceRefs []string `json:"evidence_refs"`
	Outcome      string   `json:"outcome"`
}

// Baseline is the external S10 denominator cross-check input. It is built by
// the caller (Runtime evidence registration or the Quality Gate) from facts
// the manifest author does not control: the immutable S6/S9 completion
// artifacts (BuildCoverageInventory), the change-impact ledger, and the
// affected-path set of the triggering transition. The manifest's
// `changed_path` rows must reconcile with this set exactly (RC-05 S10-5):
// the denominator may no longer be self-declared.
type Baseline struct {
	// ChangedPaths is the authoritative repo-relative changed-path set for
	// the current baseline generation. Empty means "no external projection
	// is available" — the caller decides whether that is acceptable.
	ChangedPaths []string
	// AffectedPathsAll reports that the caller explicitly declared the full
	// surface (the "all" token), which waives the exact-set comparison.
	AffectedPathsAll bool
}

// InventoryAuthority is the finite S10 denominator reconstructed from facts
// outside the manifest author: the bound REQ, current contract/TASK document
// registrations, the pinned S7 ReviewPlan claims, and the external changed
// surface. The manifest must contain exactly these ids for the corresponding
// categories; its rows remain the human-owned expected/oracle/evidence ledger.
//
// The category names deliberately mirror the source facts rather than adding
// another machine state. `task` and `claim` are additional inventory rows;
// their coverage is still accounted for by the ordinary disposition and
// counterevidence checks, while the three S10 metric categories retain their
// Blueprint meaning.
type InventoryAuthority struct {
	RequirementIDs []string
	ContractIDs    []string
	TaskIDs        []string
	ClaimIDs       []string
	ChangedPaths   []string
}

var s10RequirementToken = regexp.MustCompile(`\b(?:FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`)

// BuildS10ExternalBaseline reconstructs the changed-path denominator from
// current, fingerprinted completion/change-impact evidence plus the paths of
// the triggering request. It is intentionally implemented in this package so
// Runtime registration, the CLI and the Quality Gate consume one rule.
//
// A registered completion artifact that cannot be read, hash-verified, or
// decoded is an authority failure. Returning an empty baseline in that case
// would silently hand the denominator back to the manifest author.
func BuildS10ExternalBaseline(root string, state map[string]any, affectedPaths []string) (Baseline, error) {
	baseline := Baseline{}
	for _, path := range affectedPaths {
		if strings.EqualFold(strings.TrimSpace(path), "all") {
			baseline.AffectedPathsAll = true
			return baseline, nil
		}
	}
	seen := map[string]struct{}{}
	add := func(path string) {
		if normalized := normalizeBaselinePath(path); normalized != "" {
			if _, ok := seen[normalized]; ok {
				return
			}
			seen[normalized] = struct{}{}
			baseline.ChangedPaths = append(baseline.ChangedPaths, normalized)
		}
	}
	generation := nestedS10Int(state, "baseline", "generation")
	rawEvidence, _ := state["evidence"].([]any)
	for _, raw := range rawEvidence {
		entry, _ := raw.(map[string]any)
		if entry == nil || intValueS10(entry["baseline_generation"]) != generation || stringValue(entry["status"]) != "valid" || !s10EvidenceIsLive(entry["invalidated_by"]) {
			continue
		}
		kind := strings.TrimSpace(stringValue(entry["kind"]))
		if kind != "completion_report" && kind != "change_impact" {
			continue
		}
		id := stringValue(entry["id"])
		data, err := readAuthoritativeS10File(root, stringValue(entry["path"]), stringValue(entry["sha256"]), "evidence "+id)
		if err != nil {
			return Baseline{}, err
		}
		switch kind {
		case "completion_report":
			var completion struct {
				ChangedPaths []string `json:"changed_paths"`
			}
			if err := json.Unmarshal(data, &completion); err != nil {
				return Baseline{}, fmt.Errorf("S10 inventory authority completion evidence %s is invalid JSON: %w", id, err)
			}
			for _, path := range completion.ChangedPaths {
				add(path)
			}
		case "change_impact":
			var impact struct {
				ChangedArtifacts []struct {
					Path string `json:"path"`
				} `json:"changed_artifacts"`
			}
			if err := json.Unmarshal(data, &impact); err != nil {
				return Baseline{}, fmt.Errorf("S10 inventory authority change-impact evidence %s is invalid JSON: %w", id, err)
			}
			for _, artifact := range impact.ChangedArtifacts {
				add(artifact.Path)
			}
		}
	}
	for _, path := range affectedPaths {
		add(path)
	}
	sort.Strings(baseline.ChangedPaths)
	return baseline, nil
}

func s10EvidenceIsLive(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

// BuilderResponsibilities are the role identities whose manifest rows must
// not also be self-certified as the manifest's Owner (RC-05 S10-7). The
// manifest's owner column is the responsibility matrix: an Agent holding a
// Builder/BUILD-WORK-PACKAGE responsibility cannot simultaneously claim the
// Acceptance or Release-Auditor owner seat over its own work.
var BuilderResponsibilities = []string{"Builder", "BUILD-WORK-PACKAGE", "builder", "build-work-package"}

// AuditArea is one of the eight release-architecture audit areas.
type AuditArea struct {
	ID           string   `json:"id"`
	Conclusion   string   `json:"conclusion"`
	Owner        string   `json:"owner"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// RiskItem keeps every non-blocking risk accountable to an owner, tracking
// artifact, impact, and recovery point before it can be handed to S11.
type RiskItem struct {
	ID            string `json:"id"`
	Severity      string `json:"severity"`
	Impact        string `json:"impact"`
	Owner         string `json:"owner"`
	TrackingRef   string `json:"tracking_ref"`
	RecoveryPoint string `json:"recovery_point"`
}

// DebtItem prevents technical debt from disappearing merely because it does
// not block the release.
type DebtItem struct {
	ID          string `json:"id"`
	Impact      string `json:"impact"`
	Owner       string `json:"owner"`
	TrackingRef string `json:"tracking_ref"`
}

// BlockingFinding is an explicit route-bearing blocker. A non-empty list
// makes the S10 manifest fail; it cannot be hidden in a prose report.
type BlockingFinding struct {
	ID    string `json:"id"`
	Route string `json:"route"`
}

// Metrics are objective S10 completion indicators. Coverage values are
// checked against the manifest's declared category rows. The hard metric
// categories must be represented explicitly; optional categories may have a
// zero denominator only when they are genuinely outside this audit scope.
type Metrics struct {
	RequirementCoverage  float64 `json:"requirement_coverage"`
	ContractCoverage     float64 `json:"contract_coverage"`
	ChangedPathCoverage  float64 `json:"changed_path_coverage"`
	AuditAreaCoverage    float64 `json:"audit_area_coverage"`
	UnknownCount         int     `json:"unknown_count"`
	UnsupportedPassCount int     `json:"unsupported_pass_count"`
	UnownedRiskCount     int     `json:"unowned_risk_count"`
	UntrackedDebtCount   int     `json:"untracked_debt_count"`
	BlockingFindingCount int     `json:"blocking_finding_count"`
}

// Summary is the gate-facing, derived view of a valid manifest.
type Summary struct {
	ManifestType         string
	InventoryCount       int
	DispositionedCount   int
	CounterevidenceCount int
	AuditAreaCount       int
	BlockingFindingCount int
	UnknownCount         int
	UnsupportedPassCount int
	EvidenceRefs         []string
	Metrics              Metrics
}

// Validate decodes and validates one S10 manifest as a completion artifact.
// expectedType must be "acceptance" or "release_audit". Errors name the
// exact row or metric and include the recovery action an Agent should take.
func Validate(data []byte, expectedType string) (Summary, error) {
	return validate(data, expectedType, true, nil)
}

// ValidateWithBaseline validates the manifest against an external changed-path
// denominator (RC-05 S10-5). The S10 coverage inventory may no longer be
// entirely self-declared: every path in the external baseline must have a
// `changed_path` row (missing rows are rejected), and every `changed_path`
// row citing a repo path must trace to the baseline (an invented row is
// rejected). Baselines carrying no paths (and no explicit "all") leave the
// self-declared denominator untouched — use them only where no external
// projection exists.
func ValidateWithBaseline(data []byte, expectedType string, baseline Baseline) (Summary, error) {
	return validate(data, expectedType, true, &baseline)
}

// ValidateForOutcome validates the manifest mode appropriate for an evidence
// envelope. Passing outcomes require a clean ledger. A routed
// review_required/blocked outcome still needs a structurally complete,
// evidence-linked ledger, but may retain the unresolved rows that explain
// why it must return through S7/S8/S9 or pause.
func ValidateForOutcome(data []byte, expectedType, outcome string) (Summary, error) {
	requireClean := !allowsUnresolvedOutcome(expectedType, outcome)
	return validate(data, expectedType, requireClean, nil)
}

// ValidateForOutcomeWithBaseline is ValidateForOutcome with the RC-05
// external changed-path cross-check applied.
func ValidateForOutcomeWithBaseline(data []byte, expectedType, outcome string, baseline Baseline) (Summary, error) {
	requireClean := !allowsUnresolvedOutcome(expectedType, outcome)
	return validate(data, expectedType, requireClean, &baseline)
}

// ValidateForOutcomeWithAuthority adds the finite inventory exact-set check
// to ValidateForOutcome. It is used by the S10 production paths once the
// current Runtime and pinned S7 plan are available.
func ValidateForOutcomeWithAuthority(data []byte, expectedType, outcome string, authority InventoryAuthority) (Summary, error) {
	summary, err := ValidateForOutcome(data, expectedType, outcome)
	if err != nil {
		return summary, err
	}
	return validateInventoryAuthority(data, summary, authority)
}

// ValidateForOutcomeWithBaselineAndAuthority is the complete S10 validator:
// structural/outcome checks, external changed-path reconciliation, and the
// exact finite inventory derived from authoritative runtime facts.
func ValidateForOutcomeWithBaselineAndAuthority(data []byte, expectedType, outcome string, baseline Baseline, authority InventoryAuthority) (Summary, error) {
	summary, err := ValidateForOutcomeWithBaseline(data, expectedType, outcome, baseline)
	if err != nil {
		return summary, err
	}
	return validateInventoryAuthority(data, summary, authority)
}

// S10AuthorityAvailable reports whether the Runtime has the minimum
// production pointers needed to reconstruct the non-self-declared S10
// denominator. Rootless/unit-test callers intentionally stay on the legacy
// structural validator; a real S10 round has a registered pinned ReviewPlan.
func S10AuthorityAvailable(state map[string]any) bool {
	bound, boundOK := state["bound_req"].(map[string]any)
	review, reviewOK := state["review"].(map[string]any)
	plan, planOK := review["plan"].(map[string]any)
	return boundOK && strings.TrimSpace(stringValue(bound["id"])) != "" &&
		boundOK && strings.TrimSpace(stringValue(bound["path"])) != "" &&
		reviewOK && planOK && strings.TrimSpace(stringValue(plan["path"])) != ""
}

// BuildS10InventoryAuthority reconstructs the S10 finite denominator from
// the current Runtime and repository. Every referenced document and the S7
// plan is hash-verified before its ids are admitted. A missing or drifted
// source is an authority-construction error, not an empty denominator.
func BuildS10InventoryAuthority(root string, state map[string]any, baseline Baseline) (InventoryAuthority, error) {
	if strings.TrimSpace(root) == "" {
		return InventoryAuthority{}, fmt.Errorf("S10 inventory authority requires a repository root")
	}
	if !S10AuthorityAvailable(state) {
		return InventoryAuthority{}, fmt.Errorf("S10 inventory authority is unavailable: current Runtime must contain a bound REQ and a pinned current-round ReviewPlan")
	}
	authority := InventoryAuthority{}

	bound := state["bound_req"].(map[string]any)
	reqID := strings.TrimSpace(stringValue(bound["id"]))
	reqPath := strings.TrimSpace(stringValue(bound["path"]))
	reqSHA := strings.TrimSpace(stringValue(bound["sha256"]))
	reqData, err := readAuthoritativeS10File(root, reqPath, reqSHA, "bound REQ")
	if err != nil {
		return InventoryAuthority{}, err
	}
	reqTokens := sortedUniqueStrings(s10RequirementToken.FindAllString(string(reqData), -1))
	if len(reqTokens) == 0 {
		// A locked REQ without machine-labelled FR/NFR rows is still one
		// explicit requirement object; it cannot make the category vanish.
		authority.RequirementIDs = []string{reqID}
	} else {
		for _, token := range reqTokens {
			authority.RequirementIDs = append(authority.RequirementIDs, reqID+"/"+token)
		}
	}

	generation := nestedS10Int(state, "baseline", "generation")
	rawDocuments, _ := state["documents"].([]any)
	for _, raw := range rawDocuments {
		document, _ := raw.(map[string]any)
		if document == nil || intValueS10(document["generation"]) != generation {
			continue
		}
		kind := strings.TrimSpace(stringValue(document["kind"]))
		id := strings.TrimSpace(stringValue(document["id"]))
		path := strings.TrimSpace(stringValue(document["path"]))
		sha := strings.TrimSpace(stringValue(document["sha256"]))
		if kind != "contract" && kind != "task" {
			continue
		}
		if id == "" || path == "" || sha == "" {
			return InventoryAuthority{}, fmt.Errorf("S10 inventory authority cannot use current %s document with missing id/path/sha256", kind)
		}
		if _, err := readAuthoritativeS10File(root, path, sha, kind+" "+id); err != nil {
			return InventoryAuthority{}, err
		}
		switch kind {
		case "contract":
			authority.ContractIDs = append(authority.ContractIDs, id)
		case "task":
			// A TASK is in the S10 denominator for its Closing Contract,
			// not merely because a task file happens to be registered.
			authority.TaskIDs = append(authority.TaskIDs, id+"#closing-contract")
		}
	}

	planPointer := state["review"].(map[string]any)["plan"].(map[string]any)
	planPath := strings.TrimSpace(stringValue(planPointer["path"]))
	planSHA := strings.TrimSpace(stringValue(planPointer["sha256"]))
	planData, err := readAuthoritativeS10File(root, planPath, planSHA, "pinned S7 ReviewPlan")
	if err != nil {
		return InventoryAuthority{}, err
	}
	var plan struct {
		ReviewRound        int `json:"review_round"`
		BaselineGeneration int `json:"baseline_generation"`
		Claims             []struct {
			ClaimID string `json:"claim_id"`
		} `json:"claims"`
	}
	if err := json.Unmarshal(planData, &plan); err != nil {
		return InventoryAuthority{}, fmt.Errorf("decode pinned S7 ReviewPlan for S10 inventory authority: %w", err)
	}
	if plan.ReviewRound != nestedS10Int(state, "review", "round") || plan.BaselineGeneration != generation {
		return InventoryAuthority{}, fmt.Errorf("pinned S7 ReviewPlan is not bound to the current S10 round/baseline")
	}
	for _, claim := range plan.Claims {
		if id := strings.TrimSpace(claim.ClaimID); id != "" {
			authority.ClaimIDs = append(authority.ClaimIDs, id)
		}
	}
	if len(authority.ClaimIDs) == 0 {
		return InventoryAuthority{}, fmt.Errorf("pinned S7 ReviewPlan has no claims; S10 cannot freeze the review denominator")
	}

	authority.RequirementIDs = sortedUniqueStrings(authority.RequirementIDs)
	authority.ContractIDs = sortedUniqueStrings(authority.ContractIDs)
	authority.TaskIDs = sortedUniqueStrings(authority.TaskIDs)
	authority.ClaimIDs = sortedUniqueStrings(authority.ClaimIDs)
	if !baseline.AffectedPathsAll {
		for _, path := range baseline.ChangedPaths {
			if normalized := normalizeBaselinePath(path); normalized != "" {
				authority.ChangedPaths = append(authority.ChangedPaths, normalized)
			}
		}
		authority.ChangedPaths = sortedUniqueStrings(authority.ChangedPaths)
	}
	return authority, nil
}

func validateInventoryAuthority(data []byte, summary Summary, authority InventoryAuthority) (Summary, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return summary, fmt.Errorf("decode S10 manifest for authoritative inventory check: %w", err)
	}
	issues := inventoryAuthorityIssues(manifest.CoverageInventory, authority)
	if len(issues) > 0 {
		return summary, fmt.Errorf("S10 manifest invalid: %s; next: regenerate coverage_inventory from the current bound REQ, contracts, TASK Closing Contracts, pinned S7 Claims, and changed-path baseline, then validate and register a new fingerprinted envelope", strings.Join(issues, "; "))
	}
	return summary, nil
}

func inventoryAuthorityIssues(inventory []CoverageItem, authority InventoryAuthority) []string {
	expected := map[string][]string{
		"requirement": authority.RequirementIDs,
		"contract":    authority.ContractIDs,
		"task":        authority.TaskIDs,
		"claim":       authority.ClaimIDs,
	}
	if len(authority.ChangedPaths) > 0 {
		ids := make([]string, 0, len(authority.ChangedPaths))
		for _, path := range authority.ChangedPaths {
			ids = append(ids, "path:"+path)
		}
		expected["changed_path"] = ids
	}
	actual := map[string]map[string]struct{}{}
	for _, item := range inventory {
		if actual[item.Category] == nil {
			actual[item.Category] = map[string]struct{}{}
		}
		actual[item.Category][strings.TrimSpace(item.ID)] = struct{}{}
	}
	var issues []string
	for category, ids := range expected {
		if len(ids) == 0 {
			continue
		}
		want := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			want[id] = struct{}{}
		}
		var missing, extra []string
		for _, id := range ids {
			if _, ok := actual[category][id]; !ok {
				missing = append(missing, id)
			}
		}
		for id := range actual[category] {
			if _, ok := want[id]; !ok {
				extra = append(extra, id)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 {
			issues = append(issues, fmt.Sprintf("authoritative %s inventory is missing %s", category, strings.Join(missing, ", ")))
		}
		if len(extra) > 0 {
			issues = append(issues, fmt.Sprintf("%s inventory contains ids outside the authoritative set: %s", category, strings.Join(extra, ", ")))
		}
	}
	return issues
}

func readAuthoritativeS10File(root, path, wantSHA, label string) ([]byte, error) {
	resolved, err := safeManifestPath(root, path)
	if err != nil {
		return nil, fmt.Errorf("S10 inventory authority %s: %w", label, err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("S10 inventory authority %s is unreadable: %w", label, err)
	}
	if strings.TrimSpace(wantSHA) == "" {
		return nil, fmt.Errorf("S10 inventory authority %s has no registered sha256", label)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != wantSHA {
		return nil, fmt.Errorf("S10 inventory authority %s drifted: registered sha256 %s does not match disk", label, wantSHA)
	}
	return data, nil
}

func nestedS10Int(value map[string]any, parent, child string) int {
	if child == "" {
		return intValueS10(value[parent])
	}
	nested, _ := value[parent].(map[string]any)
	return intValueS10(nested[child])
}

func intValueS10(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func allowsUnresolvedOutcome(expectedType, outcome string) bool {
	return (expectedType == ManifestAcceptance && outcome == "review_required") ||
		(expectedType == ManifestReleaseAudit && outcome == "blocked")
}

func validate(data []byte, expectedType string, requireClean bool, baseline *Baseline) (Summary, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Summary{}, fmt.Errorf("S10 manifest JSON is invalid: %w; rewrite the file from the S10 manifest shape and run `loop-harness s10 manifest validate --file <path>`", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("s10-audit-manifest.schema.json", data); err != nil {
		return Summary{}, fmt.Errorf("S10 manifest does not satisfy s10-audit-manifest.schema.json: %w; correct the named field and rerun `loop-harness s10 manifest validate --file <path>`", err)
	}

	summary := Summary{
		ManifestType:         manifest.ManifestType,
		InventoryCount:       len(manifest.CoverageInventory),
		CounterevidenceCount: len(manifest.Counterevidence),
		AuditAreaCount:       len(manifest.AuditAreas),
		BlockingFindingCount: len(manifest.BlockingFindings),
		Metrics:              manifest.Metrics,
	}
	var issues []string
	if manifest.SchemaVersion != SchemaVersionAcceptance {
		issues = append(issues, fmt.Sprintf("schema_version must be %q (got %q)", SchemaVersionAcceptance, manifest.SchemaVersion))
	}
	if expectedType != ManifestAcceptance && expectedType != ManifestReleaseAudit {
		issues = append(issues, fmt.Sprintf("expected manifest type must be acceptance or release_audit (got %q)", expectedType))
	} else if manifest.ManifestType != expectedType {
		issues = append(issues, fmt.Sprintf("manifest_type must be %q (got %q)", expectedType, manifest.ManifestType))
	}
	if strings.TrimSpace(manifest.RuntimeID) == "" {
		issues = append(issues, "runtime_id is required; copy it from the current Runtime")
	}
	if manifest.BaselineGeneration < 1 {
		issues = append(issues, "baseline_generation must be at least 1; bind the manifest to the current baseline")
	}
	if manifest.ReviewRound < 1 {
		issues = append(issues, "review_round must be at least 1; bind the manifest to the current S7 clean round")
	}
	if len(manifest.CoverageInventory) == 0 {
		issues = append(issues, "coverage_inventory is empty; freeze the finite S10 review denominator before writing PASS")
	}

	itemsByID := make(map[string]CoverageItem, len(manifest.CoverageInventory))
	evidenceRefs := make(map[string]struct{})
	categoryCounts := make(map[string]int)
	categoryDispositioned := make(map[string]int)
	for i, item := range manifest.CoverageInventory {
		row := fmt.Sprintf("coverage_inventory[%d] (%s)", i, item.ID)
		if strings.TrimSpace(item.ID) == "" {
			issues = append(issues, fmt.Sprintf("%s.id is required", row))
			continue
		}
		if _, exists := itemsByID[item.ID]; exists {
			issues = append(issues, fmt.Sprintf("%s is duplicate; keep one frozen coverage row per object", row))
			continue
		}
		itemsByID[item.ID] = item
		categoryCounts[item.Category]++
		if item.Disposition == "pass" || item.Disposition == "not_applicable" {
			categoryDispositioned[item.Category]++
			summary.DispositionedCount++
		} else if item.Disposition == "unknown" {
			summary.UnknownCount++
		}
		if strings.TrimSpace(item.Category) == "" {
			issues = append(issues, fmt.Sprintf("%s.category is required", row))
		}
		if len(nonEmpty(item.SourceRefs)) == 0 {
			issues = append(issues, fmt.Sprintf("%s.source_refs is required; every conclusion needs an authoritative source", row))
		}
		for _, ref := range nonEmpty(item.EvidenceRefs) {
			evidenceRefs[ref] = struct{}{}
		}
		if strings.TrimSpace(item.Expected) == "" {
			issues = append(issues, fmt.Sprintf("%s.expected is required", row))
		}
		if strings.TrimSpace(item.Oracle) == "" {
			issues = append(issues, fmt.Sprintf("%s.oracle is required; state how the expected fact was observed", row))
		}
		if strings.TrimSpace(item.Owner) == "" {
			issues = append(issues, fmt.Sprintf("%s.owner is required; assign one accountable responsibility", row))
		}
		switch item.Disposition {
		case "pass":
			if len(nonEmpty(item.EvidenceRefs)) == 0 {
				summary.UnsupportedPassCount++
				issues = append(issues, fmt.Sprintf("%s is PASS without evidence_refs; replace unsupported PASS with evidence or UNKNOWN", row))
			}
		case "not_applicable":
			if strings.TrimSpace(item.NAReason) == "" {
				issues = append(issues, fmt.Sprintf("%s is not_applicable without na_reason; cite the authoritative scope decision", row))
			}
		case "unknown", "fail":
			if requireClean {
				issues = append(issues, fmt.Sprintf("%s is %s; resolve it or route back through S7/S8/S9 before S10 can pass", row, item.Disposition))
			}
		default:
			issues = append(issues, fmt.Sprintf("%s.disposition must be pass, not_applicable, unknown, or fail", row))
		}
	}
	requiredCategories := []string{"requirement", "contract", "changed_path"}
	if expectedType == ManifestReleaseAudit {
		requiredCategories = append(requiredCategories, "audit_area")
	}
	for _, category := range requiredCategories {
		if categoryCounts[category] == 0 {
			issues = append(issues, fmt.Sprintf("coverage_inventory has no %s rows; add an explicit pass or not_applicable row with source_refs and evidence", category))
		}
	}

	seenCounterevidence := make(map[string]struct{}, len(manifest.Counterevidence))
	for i, item := range manifest.Counterevidence {
		row := fmt.Sprintf("counterevidence[%d] (%s)", i, item.ID)
		if strings.TrimSpace(item.ID) == "" {
			issues = append(issues, fmt.Sprintf("%s.id is required", row))
		}
		if _, exists := seenCounterevidence[item.InventoryID]; exists {
			issues = append(issues, fmt.Sprintf("%s duplicates inventory_id %q; provide exactly one counterevidence row per coverage item", row, item.InventoryID))
		}
		seenCounterevidence[item.InventoryID] = struct{}{}
		if _, exists := itemsByID[item.InventoryID]; !exists {
			issues = append(issues, fmt.Sprintf("%s.inventory_id %q does not reference coverage_inventory; link the question to a frozen row", row, item.InventoryID))
		}
		if strings.TrimSpace(item.Question) == "" {
			issues = append(issues, fmt.Sprintf("%s.question is required; state what would disprove the conclusion", row))
		}
		// RC-05 (S10-13): a counterevidence row is a negative-path check, not
		// a problem statement. It must hook at least one real evidence
		// artifact; only the routed `unknown` outcome (a check that genuinely
		// could not run, recorded on a review_required/blocked route) may
		// omit it.
		if len(nonEmpty(item.EvidenceRefs)) == 0 && item.Outcome != "unknown" {
			issues = append(issues, fmt.Sprintf("%s.evidence_refs is empty; record the negative-path check (evidence_ref) before marking %s — a disproof question without evidence is an empty assertion", row, item.Outcome))
		}
		for _, ref := range nonEmpty(item.EvidenceRefs) {
			evidenceRefs[ref] = struct{}{}
		}
		switch item.Outcome {
		case "pass", "not_applicable":
		case "unknown", "fail":
			summary.UnknownCount++
			if requireClean {
				issues = append(issues, fmt.Sprintf("%s.outcome is %s; unresolved counterevidence cannot enter an S10 PASS", row, item.Outcome))
			}
		default:
			issues = append(issues, fmt.Sprintf("%s.outcome must be pass, not_applicable, unknown, or fail", row))
		}
	}
	if len(seenCounterevidence) != len(itemsByID) {
		missing := make([]string, 0)
		for id := range itemsByID {
			if _, ok := seenCounterevidence[id]; !ok {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
		issues = append(issues, fmt.Sprintf("counterevidence is missing for coverage_inventory %s; one disproof question is required per item", strings.Join(missing, ", ")))
	}

	if expectedType == ManifestReleaseAudit {
		areas := make(map[string]AuditArea, len(manifest.AuditAreas))
		for i, area := range manifest.AuditAreas {
			row := fmt.Sprintf("audit_areas[%d] (%s)", i, area.ID)
			if _, exists := areas[area.ID]; exists {
				issues = append(issues, fmt.Sprintf("%s is duplicate; keep one row per audit area", row))
			}
			areas[area.ID] = area
			if strings.TrimSpace(area.Owner) == "" {
				issues = append(issues, fmt.Sprintf("%s.owner is required", row))
			}
			if len(nonEmpty(area.EvidenceRefs)) == 0 {
				issues = append(issues, fmt.Sprintf("%s.evidence_refs is required", row))
			}
			for _, ref := range nonEmpty(area.EvidenceRefs) {
				evidenceRefs[ref] = struct{}{}
			}
			if area.Conclusion != "pass" && area.Conclusion != "not_applicable" {
				issues = append(issues, fmt.Sprintf("%s.conclusion must be pass or not_applicable", row))
			}
		}
		missing := make([]string, 0)
		for _, id := range AuditAreaIDs {
			if _, ok := areas[id]; !ok {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 || len(areas) != len(AuditAreaIDs) {
			issues = append(issues, fmt.Sprintf("audit_areas must contain all 8 areas; missing %s", strings.Join(missing, ", ")))
		}
	}

	for i, risk := range manifest.Risks {
		row := fmt.Sprintf("risks[%d] (%s)", i, risk.ID)
		if strings.TrimSpace(risk.ID) == "" || strings.TrimSpace(risk.Impact) == "" || strings.TrimSpace(risk.Owner) == "" || strings.TrimSpace(risk.TrackingRef) == "" || strings.TrimSpace(risk.RecoveryPoint) == "" {
			issues = append(issues, fmt.Sprintf("%s requires id, impact, owner, tracking_ref, and recovery_point; unowned or untracked risks cannot enter S11", row))
		}
		if risk.Severity != "P0" && risk.Severity != "P1" && risk.Severity != "P2" && risk.Severity != "P3" {
			issues = append(issues, fmt.Sprintf("%s.severity must be P0, P1, P2, or P3", row))
		}
		// RC-02 (S10-10): a P0 risk is business-blocking by definition. It
		// must be routed through blocking_findings (S7/S8/S9 or pause) and
		// cannot be parked as a monitored non-blocking risk that silently
		// rides into S11.
		if risk.Severity == "P0" {
			issues = append(issues, fmt.Sprintf("%s.severity is P0; P0 risks are business-blocking and must be routed through blocking_findings with a resolution route (S7/S8/S9 or pause), not parked as a monitored risk entering S11", row))
		}
		// RC-05: a tracking reference must be locatable after S11 — a free
		// string cannot be resolved to a URL or a tracker issue id.
		if ref := strings.TrimSpace(risk.TrackingRef); ref != "" && !validTrackingRef(ref) {
			issues = append(issues, fmt.Sprintf("%s.tracking_ref %q is neither a URL nor an issue id (expected https?://… or PROJECT-123); a risk must remain reachable after S11", row, risk.TrackingRef))
		}
	}
	for i, debt := range manifest.TechnicalDebt {
		row := fmt.Sprintf("technical_debt[%d] (%s)", i, debt.ID)
		if strings.TrimSpace(debt.ID) == "" || strings.TrimSpace(debt.Impact) == "" || strings.TrimSpace(debt.Owner) == "" || strings.TrimSpace(debt.TrackingRef) == "" {
			issues = append(issues, fmt.Sprintf("%s requires id, impact, owner, and tracking_ref; untracked debt cannot enter S11", row))
		}
		if ref := strings.TrimSpace(debt.TrackingRef); ref != "" && !validTrackingRef(ref) {
			issues = append(issues, fmt.Sprintf("%s.tracking_ref %q is neither a URL nor an issue id (expected https?://… or PROJECT-123); debt must remain reachable after S11", row, debt.TrackingRef))
		}
	}
	for i, finding := range manifest.BlockingFindings {
		row := fmt.Sprintf("blocking_findings[%d] (%s)", i, finding.ID)
		if strings.TrimSpace(finding.ID) == "" || strings.TrimSpace(finding.Route) == "" {
			issues = append(issues, fmt.Sprintf("%s requires id and route; resolve the blocker through S7/S8/S9 or pause", row))
		}
	}

	// RC-05 (S10-7): the coverage_inventory/audit_areas `owner` column is the
	// S10 responsibility matrix. A Builder / BUILD-WORK-PACKAGE responsibility
	// is the producer being audited; it cannot also hold the audit owner seat
	// over its own work (self-certification). Owners of record stay free —
	// this is a producer/owner split check, not a name allowlist.
	for i, item := range manifest.CoverageInventory {
		if isBuilderResponsibility(item.Owner) {
			issues = append(issues, fmt.Sprintf("coverage_inventory[%d] (%s).owner %q is a Builder responsibility; the responsibility matrix requires owner != builder — assign an independent acceptance/audit owner instead of self-certifying the produced work", i, item.ID, item.Owner))
		}
	}
	for i, area := range manifest.AuditAreas {
		if isBuilderResponsibility(area.Owner) {
			issues = append(issues, fmt.Sprintf("audit_areas[%d] (%s).owner %q is a Builder responsibility; the responsibility matrix requires owner != builder — release-audit areas must be owned independently of the builders being audited", i, area.ID, area.Owner))
		}
	}

	// RC-05 (S10-5): cross-check the self-declared changed_path rows against
	// the external denominator. The baseline is derived from immutable
	// completion artifacts and the change-impact ledger — facts the manifest
	// author does not control — so a single-row denominator can no longer
	// manufacture 100% coverage.
	if baseline != nil {
		issues = append(issues, baselineChangedPathIssues(manifest.CoverageInventory, *baseline)...)
	}

	validateMetrics(&issues, manifest.Metrics, categoryCounts, categoryDispositioned, summary, manifest.ManifestType, requireClean)
	if len(issues) > 0 {
		return summary, fmt.Errorf("S10 manifest invalid: %s; next: correct the named rows, run `loop-harness s10 manifest validate --file <path>`, then register a new fingerprinted evidence envelope", strings.Join(issues, "; "))
	}
	for ref := range evidenceRefs {
		summary.EvidenceRefs = append(summary.EvidenceRefs, ref)
	}
	sort.Strings(summary.EvidenceRefs)
	return summary, nil
}

// isBuilderResponsibility reports whether an owner string names a producer
// responsibility that the S10 audit must be independent of. Matching is
// case-insensitive on the exact responsibility identities (no substring
// matching, so "Contract Reviewer" stays legal).
func isBuilderResponsibility(owner string) bool {
	normalized := strings.ToLower(strings.TrimSpace(owner))
	for _, builder := range BuilderResponsibilities {
		if normalized == strings.ToLower(builder) {
			return true
		}
	}
	return false
}

// validTrackingRef accepts an http(s) URL or a tracker issue id of the form
// PROJECT-123 (letters/digits, at least one letter and one trailing digit).
func validTrackingRef(ref string) bool {
	lower := strings.ToLower(ref)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	// issue_id: <project>-<digits>, e.g. REQ-003, BUG-42, OPS-1001.
	dash := strings.Index(ref, "-")
	if dash <= 0 || dash == len(ref)-1 {
		return false
	}
	for _, r := range ref[:dash] {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_') {
			return false
		}
	}
	for _, r := range ref[dash+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// baselineChangedPathIssues reconciles the manifest's changed_path rows with
// the external denominator (RC-05 S10-5). Both directions are enforced:
// a baseline path without a coverage row is a missing denominator entry, and
// a changed_path row citing a repo path outside the baseline is an invented
// (or stale) surface.
func baselineChangedPathIssues(inventory []CoverageItem, baseline Baseline) []string {
	if baseline.AffectedPathsAll || len(baseline.ChangedPaths) == 0 {
		// No externally-projectionable denominator: nothing to reconcile.
		return nil
	}
	baselineSet := make(map[string]struct{}, len(baseline.ChangedPaths))
	for _, path := range baseline.ChangedPaths {
		baselineSet[normalizeBaselinePath(path)] = struct{}{}
	}
	covered := make(map[string]struct{}, len(inventory))
	var issues []string
	for i, item := range inventory {
		if item.Category != "changed_path" {
			continue
		}
		for _, ref := range nonEmpty(item.SourceRefs) {
			normalized := normalizeBaselinePath(ref)
			if normalized == "" {
				continue
			}
			// Repo paths (contain a "/" or a "." extension) must trace to the
			// external baseline; abstract ids (REQ-1, docs-scope) stay free.
			if !looksLikeRepoPath(normalized) {
				continue
			}
			if _, ok := baselineSet[normalized]; !ok {
				issues = append(issues, fmt.Sprintf("coverage_inventory[%d] (%s).source_refs %q is a changed_path row outside the external changed-surface baseline; regenerate the manifest so every changed_path row cites a path from the current completion/change-impact denominator", i, item.ID, ref))
				continue
			}
			covered[normalized] = struct{}{}
		}
	}
	missing := make([]string, 0, len(baseline.ChangedPaths))
	for _, path := range baseline.ChangedPaths {
		normalized := normalizeBaselinePath(path)
		if _, ok := covered[normalized]; !ok {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		issues = append(issues, fmt.Sprintf("coverage_inventory is missing changed_path rows for the external changed-surface baseline: %s; the S10 denominator is not self-declared — freeze one changed_path row per changed path with source_refs and evidence", strings.Join(missing, ", ")))
	}
	return issues
}

// looksLikeRepoPath distinguishes repository paths from abstract surface ids
// in source_refs. A single-segment value with no dot is an id ("REQ-1",
// "release-scope"); anything with a slash or an extension is a path.
func looksLikeRepoPath(path string) bool {
	if strings.Contains(path, "/") {
		return true
	}
	return strings.Contains(filepath.Base(path), ".")
}

// normalizeBaselinePath canonicalizes a changed-path reference so manifest
// rows and baseline entries compare equal despite "./" prefixes or
// backslashes. Values containing a ":" (e.g. "REQ-1#ac-1" anchors) are not
// repo paths and normalize to "".
func normalizeBaselinePath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "./")
	if path == "" || strings.Contains(path, ":") {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

// ValidateEvidenceArtifact validates the S10-specific part of an evidence
// envelope before Runtime registers it. Generic envelope fields remain the
// responsibility of the existing Runtime/Quality Gate checks; this helper
// only closes the envelope -> immutable manifest edge.
func ValidateEvidenceArtifact(root, kind string, envelopeData []byte) error {
	manifestData, expectedType, conclusion, err := readS10ManifestData(root, kind, envelopeData)
	if err != nil {
		return err
	}
	if expectedType == "" {
		return nil
	}
	if _, err := ValidateForOutcome(manifestData, expectedType, conclusion); err != nil {
		return fmt.Errorf("S10 %s evidence manifest is invalid: %w", expectedType, err)
	}
	return nil
}

// readS10ManifestData resolves and fingerprint-checks the immutable manifest
// named by an S10 evidence envelope. Callers then apply the appropriate
// outcome/baseline/authority validator to the returned manifest bytes.
func readS10ManifestData(root, kind string, envelopeData []byte) ([]byte, string, string, error) {
	expectedType := ""
	switch kind {
	case ManifestAcceptance, "acceptance_record":
		expectedType = ManifestAcceptance
	case ManifestReleaseAudit, "release_audit_record":
		expectedType = ManifestReleaseAudit
	default:
		return nil, "", "", nil
	}
	var envelope struct {
		ManifestPath string `json:"audit_manifest_path"`
		ManifestSHA  string `json:"audit_manifest_sha256"`
		Conclusion   string `json:"conclusion"`
	}
	if err := json.Unmarshal(envelopeData, &envelope); err != nil {
		return nil, "", "", fmt.Errorf("S10 %s evidence envelope is not valid JSON: %w", expectedType, err)
	}
	if strings.TrimSpace(envelope.ManifestPath) == "" || strings.TrimSpace(envelope.ManifestSHA) == "" {
		return nil, "", "", fmt.Errorf("S10 %s evidence requires audit_manifest_path and audit_manifest_sha256; validate the manifest first with `loop-harness s10 manifest validate --file <path> --type %s`, then register a new envelope", expectedType, expectedType)
	}
	manifestPath, err := safeManifestPath(root, envelope.ManifestPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("S10 %s evidence manifest path: %w", expectedType, err)
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("S10 %s evidence manifest %q is unreadable: %w; validate the referenced file and register a new envelope", expectedType, envelope.ManifestPath, err)
	}
	if sum := sha256.Sum256(manifestData); hex.EncodeToString(sum[:]) != envelope.ManifestSHA {
		return nil, "", "", fmt.Errorf("S10 %s evidence audit_manifest_sha256 does not match %q; do not edit in place, regenerate the manifest and register a new envelope", expectedType, envelope.ManifestPath)
	}
	return manifestData, expectedType, envelope.Conclusion, nil
}

// ValidateCurrentS10Evidence is the low-level transition boundary for S10.
// It reuses the same envelope, external-baseline, and authoritative-inventory
// validators as the CLI and Quality Gate, so TR-015/TR-017 cannot be advanced
// with only a structurally valid evidence row.
func ValidateCurrentS10Evidence(root string, state map[string]any, kind string) error {
	expectedKind := ManifestAcceptance
	if kind == ManifestReleaseAudit || kind == "release_audit_record" {
		expectedKind = ManifestReleaseAudit
	}
	items, _ := state["evidence"].([]any)
	generation := nestedS10Int(state, "baseline", "generation")
	round := nestedS10Int(state, "review", "round")
	for _, raw := range items {
		entry, _ := raw.(map[string]any)
		if entry == nil || stringValue(entry["status"]) != "valid" {
			continue
		}
		entryKind := stringValue(entry["kind"])
		if (expectedKind == ManifestAcceptance && entryKind != ManifestAcceptance && entryKind != "acceptance_record") ||
			(expectedKind == ManifestReleaseAudit && entryKind != ManifestReleaseAudit && entryKind != "release_audit_record") {
			continue
		}
		entryGeneration := intValueS10(entry["generation"])
		if entryGeneration == 0 {
			entryGeneration = intValueS10(entry["baseline_generation"])
		}
		if entryGeneration != generation {
			continue
		}
		if nestedS10Int(entry, "review_round", "") != round || round < 1 {
			continue
		}
		path := stringValue(entry["path"])
		sha := stringValue(entry["sha256"])
		if path == "" || sha == "" {
			continue
		}
		artifactPath, err := safeManifestPath(root, path)
		if err != nil {
			return fmt.Errorf("current %s evidence %q path is invalid: %w", expectedKind, stringValue(entry["id"]), err)
		}
		data, err := os.ReadFile(artifactPath)
		if err != nil {
			return fmt.Errorf("current %s evidence %q is unreadable: %w", expectedKind, stringValue(entry["id"]), err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != sha {
			return fmt.Errorf("current %s evidence %q sha256 drifted: registered %s, disk %s", expectedKind, stringValue(entry["id"]), sha, got)
		}
		manifestData, _, conclusion, err := readS10ManifestData(root, expectedKind, data)
		if err != nil {
			return err
		}
		baseline, err := BuildS10ExternalBaseline(root, state, nil)
		if err != nil {
			return fmt.Errorf("current %s evidence external baseline is unverifiable: %w", expectedKind, err)
		}
		if S10AuthorityAvailable(state) {
			authority, err := BuildS10InventoryAuthority(root, state, baseline)
			if err != nil {
				return fmt.Errorf("current %s evidence authoritative inventory is unverifiable: %w", expectedKind, err)
			}
			_, err = ValidateForOutcomeWithBaselineAndAuthority(manifestData, expectedKind, conclusion, baseline, authority)
			return err
		}
		_, err = ValidateForOutcomeWithBaseline(manifestData, expectedKind, conclusion, baseline)
		return err
	}
	return fmt.Errorf("no valid current %s evidence entry for baseline_generation %d/review_round %d", expectedKind, generation, round)
}

func safeManifestPath(root, value string) (string, error) {
	clean := filepath.Clean(value)
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("audit_manifest_path must stay inside the repository: %q", value)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve repository root symlinks: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(filepath.Join(rootAbs, clean))
	if err != nil {
		return "", fmt.Errorf("resolve manifest symlinks: %w", err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("audit_manifest_path must stay inside the repository: %q", value)
	}
	return filepath.Join(rootAbs, clean), nil
}

func validateMetrics(issues *[]string, metrics Metrics, counts, dispositioned map[string]int, summary Summary, manifestType string, requireClean bool) {
	coverage := []struct {
		name     string
		category string
		value    float64
	}{
		{"requirement_coverage", "requirement", metrics.RequirementCoverage},
		{"contract_coverage", "contract", metrics.ContractCoverage},
		{"changed_path_coverage", "changed_path", metrics.ChangedPathCoverage},
	}
	if manifestType == ManifestReleaseAudit {
		coverage = append(coverage, struct {
			name     string
			category string
			value    float64
		}{"audit_area_coverage", "audit_area", metrics.AuditAreaCoverage})
	}
	for _, item := range coverage {
		if item.value < 0 || item.value > 1 || math.IsNaN(item.value) {
			*issues = append(*issues, fmt.Sprintf("metrics.%s must be between 0 and 1", item.name))
			continue
		}
		want := 1.0
		if item.category == "audit_area" && manifestType == ManifestReleaseAudit {
			want = float64(summary.AuditAreaCount) / float64(len(AuditAreaIDs))
		} else if counts[item.category] > 0 {
			want = float64(dispositioned[item.category]) / float64(counts[item.category])
		}
		if math.Abs(item.value-want) > 0.000001 {
			*issues = append(*issues, fmt.Sprintf("metrics.%s=%g does not match %s coverage %g; derive it from the frozen rows", item.name, item.value, item.category, want))
		}
	}
	if metrics.UnknownCount != summary.UnknownCount {
		*issues = append(*issues, fmt.Sprintf("metrics.unknown_count=%d does not match derived unknown_count=%d", metrics.UnknownCount, summary.UnknownCount))
	}
	if metrics.UnsupportedPassCount != summary.UnsupportedPassCount {
		*issues = append(*issues, fmt.Sprintf("metrics.unsupported_pass_count=%d does not match derived unsupported_pass_count=%d", metrics.UnsupportedPassCount, summary.UnsupportedPassCount))
	}
	if metrics.UnownedRiskCount != 0 {
		*issues = append(*issues, fmt.Sprintf("metrics.unowned_risk_count=%d; every risk must have owner, tracking_ref, and recovery_point", metrics.UnownedRiskCount))
	}
	if metrics.UntrackedDebtCount != 0 {
		*issues = append(*issues, fmt.Sprintf("metrics.untracked_debt_count=%d; every technical_debt row must have owner and tracking_ref", metrics.UntrackedDebtCount))
	}
	if metrics.BlockingFindingCount != summary.BlockingFindingCount {
		*issues = append(*issues, fmt.Sprintf("metrics.blocking_finding_count=%d does not match blocking_findings=%d", metrics.BlockingFindingCount, summary.BlockingFindingCount))
	}
	if requireClean {
		for name, value := range map[string]int{
			"blocking_finding_count": metrics.BlockingFindingCount,
		} {
			if value != 0 {
				*issues = append(*issues, fmt.Sprintf("metrics.%s=%d; S10 PASS requires zero and an owner/tracking/route for every exception", name, value))
			}
		}
	}
}

func nonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}
