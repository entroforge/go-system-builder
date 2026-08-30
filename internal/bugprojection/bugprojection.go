// Package bugprojection emits the post-approval BUG compatibility views.
//
// InvestigationCase and RepairContract remain authoritative. This package is
// intentionally filesystem-only: it validates the approved references and
// writes immutable canonical JSON/Markdown projections, but never reads or
// mutates Runtime state.
package bugprojection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

var (
	bugIDPattern         = regexp.MustCompile(`^BUG-[0-9]{3,}$`)
	runtimeIDPattern     = regexp.MustCompile(`^loop-[A-Za-z0-9._-]+$`)
	reqIDPattern         = regexp.MustCompile(`REQ-[0-9]{3,}`)
	sha256Pattern        = regexp.MustCompile(`^[a-f0-9]{64}$`)
	canonicalTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$`)
)

// ArtifactRef identifies an immutable approved artifact. Path may be
// repository-relative or an absolute path under root.
type ArtifactRef struct {
	Path   string
	SHA256 string
}

// FindingRef supplies the immutable Finding sources needed by the legacy
// canonicalBug schema. Case and Contract preserve only the exact Finding ID
// set, not the source document paths.
type FindingRef struct {
	ID     string
	Path   string
	SHA256 string
}

// Request contains the authority references plus the small amount of legacy
// metadata required by canonicalBug. The metadata is projection context; it
// does not authorize repair and is never written to Runtime.
type Request struct {
	Case     ArtifactRef
	Contract ArtifactRef
	BugID    string

	RuntimeID string
	ReqID     string
	Severity  string

	FindingRefs              []FindingRef
	OriginalAssignmentID     string
	OriginalResponsibilityID string
	RequiredSkillRefs        []string
	ReviewedBy               string
	ReviewedAt               time.Time

	// Optional explicit destinations. Defaults are the evidence tree for the
	// machine-readable projection and the established human BUG directory.
	CanonicalJSONPath string
	MarkdownPath      string
}

// Result identifies both immutable projection files. A retry returns the
// same values without rewriting matching files.
type Result struct {
	BugID          string `json:"bug_id"`
	JSONPath       string `json:"json_path"`
	JSONSHA256     string `json:"json_sha256"`
	MarkdownPath   string `json:"markdown_path"`
	MarkdownSHA256 string `json:"markdown_sha256"`
}

// ProjectApprovedContract is the named entry point used by S8's post-approval
// handoff. It is an alias of Project so callers can make the authority boundary
// explicit at the call site.
func ProjectApprovedContract(root string, request Request) (Result, error) {
	return Project(root, request)
}

// Project validates an approved Case, approved RepairContract, and the exact
// source Finding set, then creates canonical BUG JSON and Markdown projections.
// Existing matching files are treated as an idempotent success; any differing
// file is a hard conflict and is never overwritten.
func Project(root string, request Request) (Result, error) {
	root, err := repositoryRoot(root)
	if err != nil {
		return Result{}, err
	}
	if !bugIDPattern.MatchString(strings.TrimSpace(request.BugID)) {
		return Result{}, actionable("BUG id %q must match ^BUG-[0-9]{3,}$", request.BugID)
	}
	if len(request.FindingRefs) == 0 {
		return Result{}, actionable("FindingRefs is empty; provide every immutable source Finding ref before projecting BUG %s", request.BugID)
	}

	caseRel, caseBytes, caseDocument, err := readApprovedArtifact(root, request.Case, "review-investigation-case.schema.json", "InvestigationCase")
	if err != nil {
		return Result{}, err
	}
	contractRel, contractBytes, contractDocument, err := readApprovedArtifact(root, request.Contract, "repair-contract.schema.json", "RepairContract")
	if err != nil {
		return Result{}, err
	}
	if stringField(caseDocument["status"]) != "contract_approved" {
		return Result{}, actionable("InvestigationCase %s has status %q; only contract_approved Cases can produce a BUG projection", stringField(caseDocument["case_id"]), stringField(caseDocument["status"]))
	}
	if stringField(contractDocument["status"]) != "approved" {
		return Result{}, actionable("RepairContract %s has status %q; only approved Contracts can produce a BUG projection", stringField(contractDocument["repair_contract_id"]), stringField(contractDocument["status"]))
	}
	caseID := stringField(caseDocument["case_id"])
	contractID := stringField(contractDocument["repair_contract_id"])
	if stringField(contractDocument["case_id"]) != caseID {
		return Result{}, actionable("RepairContract %s case_id %q does not match InvestigationCase %q; read the approved Case/Contract pair again", contractID, stringField(contractDocument["case_id"]), caseID)
	}
	if normalizeRel(stringField(caseDocument["repair_contract_ref"])) != contractRel {
		return Result{}, actionable("InvestigationCase %s does not point to approved RepairContract %s; regenerate from the matching approved pair", caseID, contractRel)
	}
	if stringField(caseDocument["repair_contract_sha256"]) != sha256Hex(contractBytes) {
		return Result{}, actionable("InvestigationCase %s repair_contract_sha256 does not match %s; refresh the approved Case/Contract reference", caseID, contractRel)
	}

	caseIDs, err := stringSet(caseDocument["source_finding_ids"], "InvestigationCase.source_finding_ids")
	if err != nil {
		return Result{}, actionable("%v", err)
	}
	contractIDs, err := stringSet(contractDocument["source_finding_ids"], "RepairContract.source_finding_ids")
	if err != nil {
		return Result{}, actionable("%v", err)
	}
	if err := exactSet(caseIDs, contractIDs, "Case/RepairContract source Finding IDs"); err != nil {
		return Result{}, actionable("%v", err)
	}

	findingDocuments, findingRefs, err := readFindings(root, request.FindingRefs)
	if err != nil {
		return Result{}, err
	}
	findingIDs := make([]string, 0, len(findingRefs))
	for _, finding := range findingRefs {
		findingIDs = append(findingIDs, finding.ID)
	}
	if err := exactSet(caseIDs, findingIDs, "Case/source Finding refs"); err != nil {
		return Result{}, actionable("%v", err)
	}

	runtimeID := strings.TrimSpace(request.RuntimeID)
	if runtimeID == "" {
		runtimeID = stringField(caseDocument["runtime_id"])
	}
	if !runtimeIDPattern.MatchString(runtimeID) {
		return Result{}, actionable("RuntimeID %q is missing or invalid; supply the approved runtime identity in the projection request", runtimeID)
	}
	reqID := strings.TrimSpace(request.ReqID)
	if reqID == "" {
		reqID = deriveREQ(findingDocuments)
	}
	if !regexp.MustCompile(`^REQ-[0-9]{3,}$`).MatchString(reqID) {
		return Result{}, actionable("REQ ID %q is missing or invalid; supply the bound REQ identity in the projection request", reqID)
	}

	severity := strings.TrimSpace(request.Severity)
	if severity == "" {
		severity = highestSeverity(findingDocuments)
	}
	if !map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}[severity] {
		return Result{}, actionable("severity %q is invalid; supply P0, P1, P2, or P3", severity)
	}
	if baseline(caseDocument) < 1 {
		return Result{}, actionable("InvestigationCase %s baseline_generation must be at least 1 for canonical BUG evidence", caseID)
	}
	assignmentID := strings.TrimSpace(request.OriginalAssignmentID)
	if assignmentID == "" {
		assignmentID = "assignment-s8-investigation"
	}
	if !regexp.MustCompile(`^assignment-[A-Za-z0-9._-]+$`).MatchString(assignmentID) {
		return Result{}, actionable("original assignment %q must match assignment-*", assignmentID)
	}
	responsibilityID := strings.TrimSpace(request.OriginalResponsibilityID)
	if responsibilityID == "" {
		responsibilityID = "S8-INVESTIGATION"
	}
	reviewedBy := strings.TrimSpace(request.ReviewedBy)
	if reviewedBy == "" {
		reviewedBy = stringField(contractDocument["approved_by"])
	}
	if reviewedBy == "" {
		return Result{}, actionable("reviewed_by is missing; use the Contract approver identity")
	}
	reviewedAt, err := reviewTime(request.ReviewedAt, stringField(contractDocument["approved_at"]))
	if err != nil {
		return Result{}, err
	}

	canonical := buildCanonicalBug(request.BugID, runtimeID, reqID, baseline(caseDocument), caseIDs, findingRefs, findingDocuments, contractDocument, caseDocument, severity, assignmentID, responsibilityID, request.RequiredSkillRefs, reviewedBy, reviewedAt)
	canonicalBytes, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode canonical BUG %s: %w", request.BugID, err)
	}
	canonicalBytes = append(canonicalBytes, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-evidence.schema.json", canonicalBytes); err != nil {
		return Result{}, actionable("canonical BUG %s does not satisfy review-evidence.schema.json: %v", request.BugID, err)
	}

	jsonRel, err := projectionPath(root, request.CanonicalJSONPath, filepath.ToSlash(filepath.Join(".claude", "evidence", runtimeID, "g"+strconv.Itoa(baseline(caseDocument)), "canonical-bugs", request.BugID+".json")))
	if err != nil {
		return Result{}, err
	}
	markdown := buildMarkdown(request.BugID, jsonRel, caseRel, sha256Hex(caseBytes), contractRel, sha256Hex(contractBytes), caseDocument, contractDocument, findingRefs, findingDocuments, canonical)
	markdownPath, err := projectionPath(root, request.MarkdownPath, filepath.ToSlash(filepath.Join("docs", "reports", "bugs", request.BugID+".md")))
	if err != nil {
		return Result{}, err
	}

	jsonSHA := sha256Hex(canonicalBytes)
	markdownSHA := sha256Hex(markdown)
	createdJSON, err := ensureImmutable(filepath.Join(root, filepath.FromSlash(jsonRel)), canonicalBytes, "canonical BUG JSON")
	if err != nil {
		return Result{}, err
	}
	createdMarkdown, err := ensureImmutable(filepath.Join(root, filepath.FromSlash(markdownPath)), markdown, "BUG Markdown projection")
	if err != nil {
		if createdJSON {
			_ = os.Remove(filepath.Join(root, filepath.FromSlash(jsonRel)))
		}
		return Result{}, err
	}
	_ = createdMarkdown
	return Result{BugID: request.BugID, JSONPath: jsonRel, JSONSHA256: jsonSHA, MarkdownPath: markdownPath, MarkdownSHA256: markdownSHA}, nil
}

func readApprovedArtifact(root string, ref ArtifactRef, schemaName, label string) (string, []byte, map[string]any, error) {
	rel, err := relativePath(root, ref.Path)
	if err != nil {
		return "", nil, nil, actionable("%s reference path is invalid: %v", label, err)
	}
	if !sha256Pattern.MatchString(ref.SHA256) {
		return "", nil, nil, actionable("%s %s must carry a lowercase sha256 reference", label, rel)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", nil, nil, actionable("approved %s %s is missing or unreadable: %v", label, rel, err)
	}
	actual := sha256Hex(data)
	if actual != ref.SHA256 {
		return "", nil, nil, actionable("approved %s %s sha256 drift: reference=%s disk=%s; read the approved artifact again", label, rel, ref.SHA256, actual)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
		return "", nil, nil, actionable("approved %s %s schema is invalid: %v", label, rel, err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return "", nil, nil, actionable("decode approved %s %s: %v", label, rel, err)
	}
	return rel, data, document, nil
}

func readFindings(root string, refs []FindingRef) ([]map[string]any, []FindingRef, error) {
	ids := make([]string, 0, len(refs))
	documents := make([]map[string]any, 0, len(refs))
	normalized := make([]FindingRef, 0, len(refs))
	for index, ref := range refs {
		if strings.TrimSpace(ref.ID) == "" {
			return nil, nil, actionable("FindingRefs[%d].ID is empty", index)
		}
		rel, data, document, err := readApprovedArtifact(root, ArtifactRef{Path: ref.Path, SHA256: ref.SHA256}, "finding.schema.json", "Finding")
		if err != nil {
			return nil, nil, err
		}
		if stringField(document["finding_id"]) != ref.ID {
			return nil, nil, actionable("Finding ref %q points to a document declaring %q", ref.ID, stringField(document["finding_id"]))
		}
		if contains(ids, ref.ID) {
			return nil, nil, actionable("FindingRefs contains duplicate Finding ID %q", ref.ID)
		}
		ids = append(ids, ref.ID)
		documents = append(documents, document)
		normalized = append(normalized, FindingRef{ID: ref.ID, Path: rel, SHA256: sha256Hex(data)})
	}
	return documents, normalized, nil
}

func buildCanonicalBug(bugID, runtimeID, reqID string, generation int, findingIDs []string, refs []FindingRef, findings []map[string]any, contract, investigation map[string]any, severity, assignment, responsibility string, requiredSkills []string, reviewedBy string, reviewedAt string) map[string]any {
	actual := uniqueStrings(fieldStrings(findings, "observed"))
	expected := uniqueStrings(fieldStrings(findings, "expected"))
	evidence := uniqueStrings(flattenStringArrays(findings, "evidence_refs"))
	clauses := uniqueStrings(append(flattenStringArrays(findings, "authority_refs"), stringField(contract["violated_invariant"])))
	impact := []string{objectSummary(investigation["blast_radius"])}
	if impact[0] == "{}" || impact[0] == "null" {
		impact = []string{"blast radius recorded in approved InvestigationCase"}
	}
	closing := make([]any, 0)
	for index, assertion := range appendStringArrays(contract, "symptom_assertions", "root_invariant_assertions", "detection_gap_assertions") {
		closing = append(closing, map[string]any{"id": fmt.Sprintf("RC-%03d", index+1), "statement": assertion, "evidence_ref": "RepairContract:" + stringField(contract["repair_contract_id"])})
	}
	sourceReports := make([]any, 0, len(refs))
	for _, ref := range refs {
		sourceReports = append(sourceReports, map[string]any{"id": ref.ID, "path": ref.Path, "sha256": ref.SHA256})
	}
	return map[string]any{
		"schema_version": "1.0.0", "record_type": "canonical_bug", "bug_id": bugID, "status": "accepted",
		"runtime_id": runtimeID, "req_id": reqID, "baseline_generation": generation, "source_finding_ids": findingIDs,
		"source_reports": sourceReports, "severity": severity, "violated_clauses": clauses,
		"actual_behavior": strings.Join(actual, " | "), "expected_behavior": strings.Join(expected, " | "),
		"reproduction_evidence": evidence, "root_cause": stringField(contract["root_cause_statement"]), "impact": impact,
		"repair_scope": stringSliceAny(contract["prospective_scope"]), "forbidden_scope": stringSliceAny(contract["forbidden_scope"]),
		"closing_contract": closing, "original_assignment_id": assignment, "original_responsibility_id": responsibility,
		"required_skill_refs": uniqueStrings(requiredSkills), "attempt_count": 0,
		"same_contract_failure_count": 0, "reviewed_by": reviewedBy, "reviewed_at": reviewedAt,
	}
}

func buildMarkdown(bugID, jsonPath, casePath, caseSHA, contractPath, contractSHA string, investigation, contract map[string]any, refs []FindingRef, findings []map[string]any, canonical map[string]any) []byte {
	ids := stringSliceAny(canonical["source_finding_ids"])
	actual := strings.Join(fieldStrings(findings, "observed"), " | ")
	expected := strings.Join(fieldStrings(findings, "expected"), " | ")
	journeys := make([]string, 0, len(findings))
	walls := make([]string, 0, len(findings))
	for _, finding := range findings {
		encounter, _ := finding["encounter"].(map[string]any)
		journeys = append(journeys, fmt.Sprintf("%s: %s; wall=%s; first_bad=%s", stringField(finding["finding_id"]), stringField(encounter["journey_summary"]), stringField(encounter["wall_action"]), stringField(encounter["first_bad_checkpoint"])))
		walls = append(walls, fmt.Sprintf("%s: %s -> %s", stringField(finding["finding_id"]), stringField(encounter["wall_action"]), stringField(encounter["first_bad_checkpoint"])))
	}
	scope := strings.Join(stringSliceAny(contract["prospective_scope"]), ", ")
	forbidden := strings.Join(stringSliceAny(contract["forbidden_scope"]), ", ")
	return []byte(fmt.Sprintf(`# Canonical BUG Compatibility Projection: %s

> Generated after approved InvestigationCase and RepairContract. This file is
> a human-readable compatibility projection; it is not authoritative, not an S8 intake,
> and does not authorize S9 repair.

## Projection Metadata

| Field | Value |
|:---|:---|
| BUG ID | %s |
| Projection status | accepted |
| Route | s9_repair |
| InvestigationCase | %s@%v |
| Case hash | %s |
| RepairContract | %s@%v |
| Contract hash | %s |
| Source Findings | %s |
| Runtime ref | %s |
| Severity | %s |
| Canonical JSON | %s |

## Authority and Traceability

The authoritative read order is:

InvestigationCase -> approved RepairContract -> this BUG projection -> S9 task

| Kind | ID | Path | SHA-256 |
|:---|:---|:---|:---|
| InvestigationCase | %s | %s | %s |
| RepairContract | %s | %s | %s |
%s

## 1. Observed Facts

| Field | Value |
|:---|:---|
| Expected | %s |
| Observed | %s |
| Operation path | %s |
| Wall action / first bad checkpoint | %s |

## 2. Root Cause and Causal Model

| Causal element | Approved value |
|:---|:---|
| Violated invariant | %s |
| Primary root cause | %s |
| Causal model | %s |
| Blast radius | %s |
| Detection gap | %s |

## 3. Approved Repair Contract Projection

### 3.1 Repair scope

%s

### 3.2 Forbidden scope

%s

### 3.3 Required verification assertions

%s

### 3.4 S9 handoff

S9 executes the approved RepairContract revision and hash. This BUG projection
cannot broaden scope, replace the root cause, or authorize repair.

## 4. Verification and Closure

Targeted source-Finding verification, root-invariant verification, detection-gap
verification, and a complete S7 round remain separate downstream evidence.

## 5. Route and Deduplication History

| Field | Value |
|:---|:---|
| Canonical Case | %s |
| Duplicate of | none |
| Route rationale | %s |

`, bugID, bugID, casePath, integerField(investigation["revision"]), caseSHA, contractPath, integerField(contract["revision"]), contractSHA, strings.Join(ids, ", "), stringField(canonical["runtime_id"]), stringField(canonical["severity"]), jsonPath, caseID(investigation), casePath, caseSHA, contractID(contract), contractPath, contractSHA, formatFindingRefs(refs), expected, actual, strings.Join(journeys, " | "), strings.Join(walls, " | "), stringField(contract["violated_invariant"]), stringField(contract["root_cause_statement"]), objectSummary(investigation["causal_model"]), objectSummary(investigation["blast_radius"]), objectSummary(investigation["detection_gap"]), scope, forbidden, formatAssertions(contract), caseID(investigation), stringField(investigation["route_reason"])))
}

func formatFindingRefs(refs []FindingRef) string {
	var lines []string
	for _, ref := range refs {
		lines = append(lines, fmt.Sprintf("| Finding | %s | %s | %s |", ref.ID, ref.Path, ref.SHA256))
	}
	return strings.Join(lines, "\n")
}

func formatAssertions(contract map[string]any) string {
	var lines []string
	for _, assertion := range appendStringArrays(contract, "symptom_assertions", "root_invariant_assertions", "detection_gap_assertions") {
		lines = append(lines, "- "+assertion)
	}
	return strings.Join(lines, "\n")
}

func ensureImmutable(path string, expected []byte, label string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create %s directory: %w", label, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			actual, readErr := os.ReadFile(path)
			if readErr == nil && string(actual) == string(expected) {
				return false, nil
			}
			return false, actionable("projection conflict at %s: existing %s differs; never overwrite it, inspect the approved Case/Contract and choose a new BUG id or reconcile manually", path, label)
		}
		return false, fmt.Errorf("write %s: %w", label, err)
	}
	if _, err := file.Write(expected); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return false, fmt.Errorf("write %s: %w", label, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return false, fmt.Errorf("sync %s: %w", label, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return false, fmt.Errorf("close %s: %w", label, err)
	}
	return true, nil
}

func projectionPath(root, requested, fallback string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return fallback, nil
	}
	return relativePath(root, requested)
}

func repositoryRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", actionable("repository root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	return abs, nil
}

func relativePath(root, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("path is required")
	}
	path := value
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		path = rel
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", errors.New("must be a repository-relative path or an absolute path under repository root")
	}
	return filepath.ToSlash(clean), nil
}

func normalizeRel(value string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
}

func exactSet(left, right []string, label string) error {
	left = uniqueSorted(left)
	right = uniqueSorted(right)
	if equalStrings(left, right) {
		return nil
	}
	return fmt.Errorf("%s must be exact: missing=%v extra=%v", label, difference(left, right), difference(right, left))
}

func stringSet(value any, field string) ([]string, error) {
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", field)
	}
	result := make([]string, 0, len(values))
	for index, raw := range values {
		text, ok := raw.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("%s[%d] must be a non-empty string", field, index)
		}
		result = append(result, text)
	}
	return uniqueSorted(result), nil
}

func difference(left, right []string) []string {
	known := map[string]bool{}
	for _, value := range right {
		known[value] = true
	}
	var result []string
	for _, value := range left {
		if !known[value] {
			result = append(result, value)
		}
	}
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func uniqueStrings(values []string) []string { return uniqueSorted(values) }

func fieldStrings(documents []map[string]any, field string) []string {
	result := make([]string, 0, len(documents))
	for _, document := range documents {
		if value := stringField(document[field]); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func flattenStringArrays(document []map[string]any, field string) []string {
	var result []string
	for _, item := range document {
		result = append(result, stringSliceAny(item[field])...)
	}
	return result
}

func appendStringArrays(document map[string]any, fields ...string) []string {
	var result []string
	for _, field := range fields {
		result = append(result, stringSliceAny(document[field])...)
	}
	return result
}

func stringSliceAny(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if text, ok := raw.(string); ok && text != "" {
			result = append(result, text)
		}
	}
	return result
}

func highestSeverity(findings []map[string]any) string {
	best := "P3"
	for _, finding := range findings {
		severity := stringField(finding["severity"])
		if severity == "P0" || (severity == "P1" && best != "P0") || (severity == "P2" && best == "P3") {
			best = severity
		}
	}
	return best
}

func deriveREQ(findings []map[string]any) string {
	for _, finding := range findings {
		for _, ref := range stringSliceAny(finding["authority_refs"]) {
			if match := reqIDPattern.FindString(ref); match != "" {
				return match
			}
		}
	}
	return ""
}

func baseline(document map[string]any) int { return integerField(document["baseline_generation"]) }
func integerField(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	case json.Number:
		n, _ := number.Int64()
		return int(n)
	}
	return 0
}
func stringField(value any) string              { valueString, _ := value.(string); return valueString }
func caseID(document map[string]any) string     { return stringField(document["case_id"]) }
func contractID(document map[string]any) string { return stringField(document["repair_contract_id"]) }
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func objectSummary(value any) string {
	if value == nil {
		return "null"
	}
	if object, ok := value.(map[string]any); ok {
		if surfaces := stringSliceAny(object["surfaces"]); len(surfaces) > 0 {
			return strings.Join(surfaces, ", ")
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func reviewTime(requested time.Time, candidate string) (string, error) {
	if !requested.IsZero() {
		return requested.UTC().Format(time.RFC3339Nano), nil
	}
	if canonicalTimePattern.MatchString(candidate) {
		return candidate, nil
	}
	return "", actionable("approved RepairContract has no valid approved_at; provide ReviewedAt from the approval record for a deterministic projection")
}

func sha256Hex(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func actionable(format string, args ...any) error {
	return fmt.Errorf(format+"; next: regenerate the projection from the approved Case/Contract pair and retry", args...)
}
