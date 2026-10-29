// Package acceptance owns the small, machine-readable S10 audit manifest.
//
// The manifest is the single source: the Markdown ACC and release-audit
// reports are rendered from it (RenderMarkdown) — they are the
// human-readable projection, never a second hand-maintained carrier. This
// package reads professional report structure and projects reported progress;
// it does not certify the completeness or truth of professional observations.
package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

const (
	SchemaVersionAcceptance = "1.0.0"
	ManifestAcceptance      = "acceptance"
	ManifestReleaseAudit    = "release_audit"
)

// AuditAreaIDs names the existing release-audit reading prompts. A report does
// not have to create one task, result or evidence reference for each prompt.
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
	SchemaVersion string `json:"schema_version"`
	ManifestType  string `json:"manifest_type"`
	// Outcome is the professional S10 conclusion. It is optional for existing
	// manifests; the automated Hook save path requires it and uses the same
	// value for the existing evidence envelope.
	Outcome            string                `json:"outcome,omitempty"`
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

// CoverageItem holds supplied material and its reported professional judgment.
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
	// CanonicalProduct preserves the full literal machine path denominator
	// even when the request selects an all-surface audit.
	CanonicalProduct bool
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
	canonical, selectionErr := buildsurface.SelectCanonical(root, state)
	if selectionErr != nil {
		return Baseline{}, selectionErr
	}
	if canonical {
		snapshot, err := buildsurface.ReadRegistered(root, state)
		if err != nil {
			return Baseline{}, err
		}
		baseline := Baseline{CanonicalProduct: true, ChangedPaths: []string{}}
		seen := map[string]bool{}
		for _, file := range snapshot.Paths {
			baseline.ChangedPaths = append(baseline.ChangedPaths, file.Path)
			seen[file.Path] = true
		}
		for _, file := range affectedPaths {
			if strings.EqualFold(strings.TrimSpace(file), "all") {
				baseline.AffectedPathsAll = true
				continue
			}
			if file != "" && !seen[file] {
				baseline.ChangedPaths = append(baseline.ChangedPaths, file)
				seen[file] = true
			}
		}
		sort.Strings(baseline.ChangedPaths)
		return baseline, nil
	}
	baseline := Baseline{}
	for _, path := range affectedPaths {
		if strings.EqualFold(strings.TrimSpace(path), "all") {
			baseline.AffectedPathsAll = true
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
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(data, &envelope); err != nil {
				return Baseline{}, fmt.Errorf("S10 inventory authority completion evidence %s is invalid JSON: %w", id, err)
			}
			if _, canonical := envelope["assignment_record"]; canonical {
				return Baseline{}, fmt.Errorf("S10 inventory authority canonical S6 evidence %s requires the original Main facts changed-surface projection", id)
			}
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
		if strings.EqualFold(strings.TrimSpace(path), "all") {
			continue
		}
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

// RiskItem describes an actual residual risk and responsibility. Tracking and
// recovery references are optional when there is no real artifact to cite.
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

// Metrics describe reported rows and responsibility gaps. They are derived by
// the system, not author-maintained qualifications or proof of actual coverage.
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

// Validate reads a report's existing field types and current responsibility type.
// Professional incompleteness is returned in Summary, not a reception error.
func Validate(data []byte, expectedType string) (Summary, error) {
	return validate(data, expectedType, false, nil)
}

// ValidateWithBaseline retains the existing call shape; baseline is not a
// coverage or source qualification for professional report reception.
func ValidateWithBaseline(data []byte, expectedType string, baseline Baseline) (Summary, error) {
	return validate(data, expectedType, false, nil)
}

// ValidateForOutcome reads professional facts without requiring a passing outcome.
// Completion consumers use the explicit report outcome and reported unfinished work.
func ValidateForOutcome(data []byte, expectedType, outcome string) (Summary, error) {
	return validate(data, expectedType, false, nil)
}

// ValidateForOutcomeWithBaseline preserves compatibility without source reproof.
func ValidateForOutcomeWithBaseline(data []byte, expectedType, outcome string, baseline Baseline) (Summary, error) {
	return ValidateForOutcome(data, expectedType, outcome)
}

// ValidateForOutcomeWithAuthority preserves compatibility without exact-set gates.
func ValidateForOutcomeWithAuthority(data []byte, expectedType, outcome string, authority InventoryAuthority) (Summary, error) {
	return ValidateForOutcome(data, expectedType, outcome)
}

// ValidateForOutcomeWithBaselineAndAuthority reads the report, not source proofs.
func ValidateForOutcomeWithBaselineAndAuthority(data []byte, expectedType, outcome string, baseline Baseline, authority InventoryAuthority) (Summary, error) {
	return ValidateForOutcome(data, expectedType, outcome)
}

// DeriveMetrics describes existing reported rows. These numbers do not prove
// actual coverage and are not acceptance or completion qualifications.
func DeriveMetrics(manifest Manifest) Metrics {
	counts := make(map[string]int)
	dispositioned := make(map[string]int)
	metrics := Metrics{BlockingFindingCount: len(manifest.BlockingFindings)}
	for _, item := range manifest.CoverageInventory {
		counts[item.Category]++
		if item.Disposition == "pass" || item.Disposition == "not_applicable" {
			dispositioned[item.Category]++
		}
		if item.Disposition != "pass" && item.Disposition != "not_applicable" {
			metrics.UnknownCount++
		}
	}
	for _, item := range manifest.Counterevidence {
		if item.Outcome != "pass" && item.Outcome != "not_applicable" {
			metrics.UnknownCount++
		}
	}
	coverage := func(category string) float64 {
		if counts[category] == 0 {
			return 0
		}
		return float64(dispositioned[category]) / float64(counts[category])
	}
	metrics.RequirementCoverage = coverage("requirement")
	metrics.ContractCoverage = coverage("contract")
	metrics.ChangedPathCoverage = coverage("changed_path")
	if manifest.ManifestType == ManifestReleaseAudit {
		passed := 0
		for _, area := range manifest.AuditAreas {
			if area.Conclusion == "pass" || area.Conclusion == "not_applicable" {
				passed++
			} else {
				metrics.UnknownCount++
			}
		}
		if len(manifest.AuditAreas) > 0 {
			metrics.AuditAreaCoverage = float64(passed) / float64(len(manifest.AuditAreas))
		}
	}
	// These display counts describe missing reported responsibility only; they
	// do not require external tickets or recovery references and are not gates.
	for _, risk := range manifest.Risks {
		if strings.TrimSpace(risk.Owner) == "" {
			metrics.UnownedRiskCount++
		}
	}
	for _, debt := range manifest.TechnicalDebt {
		if strings.TrimSpace(debt.Owner) == "" {
			metrics.UntrackedDebtCount++
		}
	}
	return metrics
}

// S10AuthorityAvailable reports whether the Runtime has the minimum
// production pointers needed to reconstruct the non-self-declared S10
// denominator. Rootless/unit-test callers intentionally stay on the legacy
// structural validator; a real S10 round has a registered pinned ReviewPlan.
func S10AuthorityAvailable(state map[string]any) bool {
	if buildsurface.HasCanonical(state) {
		return true
	}
	return hasS10References(state)
}

func hasS10References(state map[string]any) bool {
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
	if !hasS10References(state) {
		return InventoryAuthority{}, fmt.Errorf("S10 inventory authority is unavailable: current Runtime must contain a bound REQ and a pinned current-round ReviewPlan")
	}
	canonical, selectionErr := buildsurface.SelectCanonical(root, state)
	if selectionErr != nil {
		return InventoryAuthority{}, selectionErr
	}
	if canonical {
		snapshot, err := buildsurface.ReadRegistered(root, state)
		if err != nil {
			return InventoryAuthority{}, err
		}
		baseline.CanonicalProduct = true
		for _, file := range snapshot.Paths {
			baseline.ChangedPaths = append(baseline.ChangedPaths, file.Path)
		}
		baseline.ChangedPaths = literalUniquePaths(baseline.ChangedPaths)
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
	// Only a missing case-variant alias of the same document can use a live
	// twin. Hash equality alone must never mask tampering or substitute a
	// different document, kind, or unrelated path.
	aliasKey := func(kind, id, path, sha string) string {
		return kind + "\x00" + strings.ToLower(id) + "\x00" + strings.ToLower(filepath.ToSlash(filepath.Clean(path))) + "\x00" + sha
	}
	verifiedPaths := map[string]string{}
	type pendingRow struct {
		kind, id, path, sha string
	}
	var pending []pendingRow
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
		if _, err := readAuthoritativeS10File(root, path, sha, kind+" "+id); err == nil {
			verifiedPaths[aliasKey(kind, id, path, sha)] = path
		} else if errors.Is(err, os.ErrNotExist) {
			pending = append(pending, pendingRow{kind: kind, id: id, path: path, sha: sha})
			continue
		} else {
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
	for _, row := range pending {
		if _, live := verifiedPaths[aliasKey(row.kind, row.id, row.path, row.sha)]; !live {
			// No same-generation live twin: re-read the dead path so the
			// original missing/drifted diagnostics surface as the hard error.
			if _, err := readAuthoritativeS10File(root, row.path, row.sha, row.kind+" "+row.id); err != nil {
				return InventoryAuthority{}, fmt.Errorf("S10 inventory authority %s %s: %w", row.kind, row.id, err)
			}
		}
		switch row.kind {
		case "contract":
			authority.ContractIDs = append(authority.ContractIDs, row.id)
		case "task":
			authority.TaskIDs = append(authority.TaskIDs, row.id+"#closing-contract")
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
	if baseline.CanonicalProduct || !baseline.AffectedPathsAll {
		for _, path := range baseline.ChangedPaths {
			if baseline.CanonicalProduct {
				authority.ChangedPaths = append(authority.ChangedPaths, path)
				continue
			}
			if normalized := normalizeBaselinePath(path); normalized != "" {
				authority.ChangedPaths = append(authority.ChangedPaths, normalized)
			}
		}
		if baseline.CanonicalProduct {
			authority.ChangedPaths = literalUniquePaths(authority.ChangedPaths)
		} else {
			authority.ChangedPaths = sortedUniqueStrings(authority.ChangedPaths)
		}
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
		id := strings.TrimSpace(item.ID)
		if item.Category == "changed_path" {
			id = item.ID
		}
		actual[item.Category][id] = struct{}{}
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

// validate reads this report's existing field types and projects only its
// explicit professional results. It does not certify coverage or evidence.
func validate(data []byte, expectedType string, _ bool, _ *Baseline) (Summary, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Summary{}, fmt.Errorf("S10 manifest JSON is unreadable: %w", err)
	}
	if (expectedType != ManifestAcceptance && expectedType != ManifestReleaseAudit) || manifest.ManifestType != expectedType {
		return Summary{}, fmt.Errorf("S10 manifest_type must describe the current %s responsibility", expectedType)
	}
	summary := Summary{
		ManifestType: manifest.ManifestType, InventoryCount: len(manifest.CoverageInventory),
		CounterevidenceCount: len(manifest.Counterevidence), AuditAreaCount: len(manifest.AuditAreas),
		BlockingFindingCount: len(manifest.BlockingFindings), Metrics: DeriveMetrics(manifest),
	}
	refs := map[string]bool{}
	for _, item := range manifest.CoverageInventory {
		if item.Disposition == "pass" || item.Disposition == "not_applicable" {
			summary.DispositionedCount++
		} else {
			summary.UnknownCount++
		}
		for _, ref := range nonEmpty(item.EvidenceRefs) {
			refs[ref] = true
		}
	}
	for _, item := range manifest.Counterevidence {
		if item.Outcome != "pass" && item.Outcome != "not_applicable" {
			summary.UnknownCount++
		}
		for _, ref := range nonEmpty(item.EvidenceRefs) {
			refs[ref] = true
		}
	}
	for _, item := range manifest.AuditAreas {
		if item.Conclusion != "pass" && item.Conclusion != "not_applicable" {
			summary.UnknownCount++
		}
		for _, ref := range nonEmpty(item.EvidenceRefs) {
			refs[ref] = true
		}
	}
	for ref := range refs {
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
	if !baseline.CanonicalProduct && (baseline.AffectedPathsAll || len(baseline.ChangedPaths) == 0) {
		// No externally-projectionable denominator: nothing to reconcile.
		return nil
	}
	if baseline.CanonicalProduct && len(baseline.ChangedPaths) == 0 {
		for _, item := range inventory {
			if item.Category == "changed_path" {
				return []string{"changed_path inventory invents a path outside the verified empty canonical product baseline"}
			}
		}
		return nil
	}
	normalize := normalizeBaselinePath
	if baseline.CanonicalProduct {
		normalize = func(value string) string { return value }
	}
	baselineSet := make(map[string]struct{}, len(baseline.ChangedPaths))
	for _, path := range baseline.ChangedPaths {
		baselineSet[normalize(path)] = struct{}{}
	}
	covered := make(map[string]struct{}, len(inventory))
	var issues []string
	for i, item := range inventory {
		if item.Category != "changed_path" {
			continue
		}
		for _, ref := range item.SourceRefs {
			normalized := normalize(ref)
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
		normalized := normalize(path)
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
func literalUniquePaths(paths []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		if p != "" && !seen[p] {
			out = append(out, p)
			seen[p] = true
		}
	}
	sort.Strings(out)
	return out
}

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

// readS10ManifestData reads the report named by the existing recording envelope.
// Stored fingerprints remain metadata, not professional reception conditions.
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
	if strings.TrimSpace(envelope.ManifestPath) == "" {
		return nil, "", "", fmt.Errorf("S10 %s evidence requires its current audit_manifest_path for %s", expectedType, expectedType)
	}
	manifestPath, err := safeManifestPath(root, envelope.ManifestPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("S10 %s evidence manifest path: %w", expectedType, err)
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("S10 %s evidence manifest %q is unreadable: %w; validate the referenced file and register a new envelope", expectedType, envelope.ManifestPath, err)
	}
	return manifestData, expectedType, envelope.Conclusion, nil
}

// ResolveS10Manifest re-exports the envelope->manifest edge for Runtime
// callers that must run the baseline/authority validators over the referenced
// manifest bytes rather than over the envelope bytes (RC-15 S10-H1): the
// envelope carries registration metadata (kind, evidence_id, ...) that no
// manifest decoder may be asked to accept.
func ResolveS10Manifest(root, kind string, envelopeData []byte) ([]byte, string, string, error) {
	return readS10ManifestData(root, kind, envelopeData)
}

// ValidateCurrentS10Evidence projects current accepted professional completion.
// It does not replay hashes, upstream inventory, or evidence registration proofs.
func ValidateCurrentS10Evidence(root string, state map[string]any, kind string) error {
	expectedKind := ManifestAcceptance
	if kind == ManifestReleaseAudit || kind == "release_audit_record" {
		expectedKind = ManifestReleaseAudit
	}
	items, _ := state["evidence"].([]any)
	generation := nestedS10Int(state, "baseline", "generation")
	round := nestedS10Int(state, "review", "round")
	// RC-15 (S10-H2): the ledger is append-only; the newest matching
	// registration supersedes older ones, and first-match would wedge on
	// stale drift. Two-pass: (1) collect every matching row's identity,
	// (2) verify only the newest row's envelope end-to-end.
	var latest map[string]any
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
		if path == "" {
			continue
		}
		latest = entry
	}
	if latest == nil {
		return fmt.Errorf("no valid %s evidence entry for the current baseline/round — record the artifact before advancing", expectedKind)
	}
	entry := latest
	path := stringValue(entry["path"])
	artifactPath, err := safeManifestPath(root, path)
	if err != nil {
		return fmt.Errorf("current %s evidence %q path is invalid: %w", expectedKind, stringValue(entry["id"]), err)
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("current %s evidence %q is unreadable: %w", expectedKind, stringValue(entry["id"]), err)
	}
	manifestData, _, conclusion, err := readS10ManifestData(root, expectedKind, data)
	if err != nil {
		return err
	}
	summary, err := ValidateForOutcome(manifestData, expectedKind, conclusion)
	if err != nil {
		return err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return err
	}
	if manifest.RuntimeID != stringValue(state["runtime_id"]) || manifest.BaselineGeneration != generation || manifest.ReviewRound != round {
		return fmt.Errorf("S10 report is not associated with the current responsibility/round")
	}
	complete := conclusion == "pass"
	if expectedKind == ManifestReleaseAudit {
		complete = conclusion == "approved" || conclusion == "approved_with_risk"
	}
	if !complete || summary.UnknownCount != 0 || summary.BlockingFindingCount != 0 {
		return fmt.Errorf("S10 %s work remains incomplete: outcome=%q, unfinished reported rows=%d, reported blockers=%d", expectedKind, conclusion, summary.UnknownCount, summary.BlockingFindingCount)
	}
	return nil
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

func validateMetrics(issues *[]string, metrics, derived Metrics, manifestType string, requireClean bool) {
	coverage := []struct {
		name  string
		want  float64
		value float64
	}{
		{"requirement_coverage", derived.RequirementCoverage, metrics.RequirementCoverage},
		{"contract_coverage", derived.ContractCoverage, metrics.ContractCoverage},
		{"changed_path_coverage", derived.ChangedPathCoverage, metrics.ChangedPathCoverage},
	}
	if manifestType == ManifestReleaseAudit {
		coverage = append(coverage, struct {
			name  string
			want  float64
			value float64
		}{"audit_area_coverage", derived.AuditAreaCoverage, metrics.AuditAreaCoverage})
	}
	for _, item := range coverage {
		if item.value < 0 || item.value > 1 || math.IsNaN(item.value) {
			*issues = append(*issues, fmt.Sprintf("metrics.%s must be between 0 and 1", item.name))
			continue
		}
		if math.Abs(item.value-item.want) > 0.000001 {
			*issues = append(*issues, fmt.Sprintf("metrics.%s=%g does not match derived coverage %g; derive it from the frozen rows", item.name, item.value, item.want))
		}
	}
	if metrics.UnknownCount != derived.UnknownCount {
		*issues = append(*issues, fmt.Sprintf("metrics.unknown_count=%d does not match derived unknown_count=%d", metrics.UnknownCount, derived.UnknownCount))
	}
	if metrics.UnsupportedPassCount != derived.UnsupportedPassCount {
		*issues = append(*issues, fmt.Sprintf("metrics.unsupported_pass_count=%d does not match derived unsupported_pass_count=%d", metrics.UnsupportedPassCount, derived.UnsupportedPassCount))
	}
	if metrics.UnownedRiskCount != derived.UnownedRiskCount {
		*issues = append(*issues, fmt.Sprintf("metrics.unowned_risk_count=%d does not match derived unowned_risk_count=%d", metrics.UnownedRiskCount, derived.UnownedRiskCount))
	}
	if metrics.UntrackedDebtCount != derived.UntrackedDebtCount {
		*issues = append(*issues, fmt.Sprintf("metrics.untracked_debt_count=%d does not match derived untracked_debt_count=%d", metrics.UntrackedDebtCount, derived.UntrackedDebtCount))
	}
	if metrics.BlockingFindingCount != derived.BlockingFindingCount {
		*issues = append(*issues, fmt.Sprintf("metrics.blocking_finding_count=%d does not match blocking_findings=%d", metrics.BlockingFindingCount, derived.BlockingFindingCount))
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
