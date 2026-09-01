// Package investigation implements the minimum S8 ObservationBatch intake
// boundary. It creates one immutable-at-revision InvestigationCase artifact
// and pins it into Runtime through the shared CAS writer. Investigation does
// not create BUGs or re-run the S7 symptom.
package investigation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

const intakeNextCommand = "runtime investigation ingest --root ."

// intakeSealNext is the no-batch recovery hint (RC-18 F-H1): it must never
// point back at this ingest command. Retrying ingest can never create the
// batch — the pointer only appears once S7 seals an ObservationBatch — so a
// self-referencing hint turns the first rejection into a dead loop.
const intakeSealNext = "next: `loop-harness s7 status --explain` to read the current round, then dispatch or submit the remaining Assignments (`runtime review-result submit --assignment-id <id> --result <result.json>`) so the round consumer seals the ObservationBatch; only then re-run this ingest"

// intakePhaseNext is the RC-18 lifecycle-guard hint for the phase gate below:
// the Runtime cursor is not bug_resolution.investigation, so the batch pointer
// (if any) is not consumable yet. TR-008 (verification.observation_sealed ->
// bug_resolution.investigation) or a human defect decision must move the
// cursor first — re-running ingest can never do that.
const intakePhaseNext = "next: run `runtime investigation status` to inspect the current cursor; TR-008 (a sealed ObservationBatch handed off from verification.observation_sealed) or a human defect decision must open bug_resolution.investigation before this ingest is legal"

// ErrAlreadyIngested means the current Runtime already points at the same
// ObservationBatch/Case. It is intentionally an error so a caller cannot
// mistake an idempotent retry for a new investigation revision.
var ErrAlreadyIngested = errors.New("investigation case already ingested")

// ErrIngestConflict means an existing Case or Runtime pointer has a different
// identity and must not be overwritten by intake.
var ErrIngestConflict = errors.New("investigation intake identity conflict")

// IngestRequest carries the caller's Runtime CAS revision and the only
// authoring decision allowed at intake: why this sealed Finding set is a
// provisional grouping. CaseID is optional; when omitted it is derived from
// the sealed ObservationBatch ID.
type IngestRequest struct {
	ExpectedRevision  int
	CaseID            string
	GroupingRationale string
	OccurredAt        time.Time
}

type observationBatch struct {
	ObservationBatchID string   `json:"observation_batch_id"`
	RuntimeID          string   `json:"runtime_id"`
	BaselineGeneration int      `json:"baseline_generation"`
	SubjectDigest      string   `json:"subject_digest"`
	FindingIDs         []string `json:"finding_ids"`
	// RC-18 S8-M1: claim_coverage_summary / blocked_claims / unobserved_claim_ids
	// were previously never decoded, so the S8 boundary views derived from them
	// stayed empty at intake. The schema already makes them required.
	ClaimCoverageSummary claimCoverageSummary `json:"claim_coverage_summary"`
	UnobservedClaimIDs   []string             `json:"unobserved_claim_ids"`
}

// claimCoverageSummary mirrors the sealed ObservationBatch projection
// (observation-batch.schema.json): the round's Claim dispositions and the
// blocked Claims with their blocking Finding bindings.
type claimCoverageSummary struct {
	TotalRequired int            `json:"total_required"`
	Pass          int            `json:"pass"`
	Finding       int            `json:"finding"`
	NotApplicable int            `json:"not_applicable"`
	Blocked       int            `json:"blocked"`
	BlockedClaims []blockedClaim `json:"blocked_claims"`
	PlanRevision  int            `json:"plan_revision"`
}

type blockedClaim struct {
	ClaimID             string             `json:"claim_id"`
	BlockingFindingIDs  []string           `json:"blocking_finding_ids"`
	FailedPrecondition  failedPrecondition `json:"failed_precondition"`
	EvidenceRefs        []string           `json:"evidence_refs"`
	AfterRepairRequired bool               `json:"after_repair_required"`
	ResultID            string             `json:"result_id"`
}

type failedPrecondition struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Ingest consumes the sealed ObservationBatch pointer in state.review,
// verifies the pinned artifact and exact Finding set, writes the initial Case,
// and commits the state.review.investigation pointer with one Runtime CAS.
func Ingest(root, statePath, journalPath string, request IngestRequest) (runtime.Snapshot, error) {
	if strings.TrimSpace(root) == "" {
		return runtime.Snapshot{}, actionableError("repository root is required")
	}
	if strings.TrimSpace(request.GroupingRationale) == "" {
		return runtime.Snapshot{}, actionableError("grouping_rationale is required to create the provisional InvestigationCase")
	}

	current, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("read Runtime before investigation intake: %w", err)
	}
	if request.ExpectedRevision >= 0 && current.Revision != request.ExpectedRevision {
		return runtime.Snapshot{}, fmt.Errorf("%w: expected %d but Runtime is at %d; next: %s", runtime.ErrStaleRevision, request.ExpectedRevision, current.Revision, intakeNextCommand)
	}
	// RC-18 lifecycle gate: only TR-008 (verification.observation_sealed ->
	// bug_resolution.investigation) or a human defect decision may open S8
	// intake (docs/loop-definition.json bug_resolution.investigation
	// entry_condition). Without this gate a stale batch pointer left behind by
	// a phase change — or a Case-route replay after route_consume clears the
	// pointer and leaves the phase — could enter the Case write path.
	lifecycle, _ := current.State["lifecycle"].(map[string]any)
	if state := stringField(lifecycle["state"]); state != "bug_resolution" || stringField(lifecycle["phase"]) != "investigation" {
		return runtime.Snapshot{}, actionableError(
			"investigation intake requires lifecycle bug_resolution.investigation but the Runtime cursor is %s.%s; %s",
			stringField(lifecycle["state"]), stringField(lifecycle["phase"]), intakePhaseNext)
	}

	pointer, err := observationBatchPointer(current.State)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	batchPath, err := repositoryPath(root, pointer.Path)
	if err != nil {
		return runtime.Snapshot{}, actionableError("observation_batch.path is invalid: %v", err)
	}
	batchBytes, err := os.ReadFile(batchPath)
	if err != nil {
		return runtime.Snapshot{}, actionableError("sealed ObservationBatch %q is missing or unreadable: %v", pointer.Path, err)
	}
	actualBatchSHA := sha256Hex(batchBytes)
	if actualBatchSHA != pointer.SHA256 {
		return runtime.Snapshot{}, actionableError("sealed ObservationBatch %q sha256 drifted: state pins %s but disk is %s", pointer.Path, pointer.SHA256, actualBatchSHA)
	}
	if err := schema.NewValidator(root).ValidateBytes("observation-batch.schema.json", batchBytes); err != nil {
		return runtime.Snapshot{}, actionableError("sealed ObservationBatch %q schema is invalid: %v", pointer.Path, err)
	}
	var batch observationBatch
	if err := json.Unmarshal(batchBytes, &batch); err != nil {
		return runtime.Snapshot{}, actionableError("decode sealed ObservationBatch %q: %v", pointer.Path, err)
	}
	if batch.ObservationBatchID != pointer.BatchID {
		return runtime.Snapshot{}, actionableError("observation_batch batch_id mismatch: state pins %q but the file declares %q", pointer.BatchID, batch.ObservationBatchID)
	}
	if batch.RuntimeID != stringField(current.State["runtime_id"]) {
		return runtime.Snapshot{}, actionableError("observation_batch runtime_id %q does not match Runtime %q", batch.RuntimeID, stringField(current.State["runtime_id"]))
	}
	baselineGeneration, err := baselineGeneration(current.State)
	if err != nil {
		return runtime.Snapshot{}, actionableError("Runtime baseline.generation is missing: %v", err)
	}
	if batch.BaselineGeneration != baselineGeneration {
		return runtime.Snapshot{}, actionableError("observation_batch baseline_generation %d does not match Runtime baseline_generation %d", batch.BaselineGeneration, baselineGeneration)
	}
	if err := exactFindingSet(pointer.FindingIDs, batch.FindingIDs); err != nil {
		return runtime.Snapshot{}, actionableError("observation_batch Finding IDs are not an exact set: %v", err)
	}
	if err := validateFindingArtifacts(root, current.State, pointer.FindingIDs); err != nil {
		return runtime.Snapshot{}, actionableError("sealed ObservationBatch Finding artifacts are not investigation-ready: %v", err)
	}

	caseID := request.CaseID
	if strings.TrimSpace(caseID) == "" {
		caseID = "investigation-case-" + batch.ObservationBatchID
	}
	if !validCaseID(caseID) {
		return runtime.Snapshot{}, actionableError("case_id %q is invalid; use investigation-case-<token>", caseID)
	}
	if err := rejectExistingInvestigation(current.State, caseID, batch.ObservationBatchID); err != nil {
		return runtime.Snapshot{}, err
	}

	caseRel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "cases", caseID+".json"))
	casePath, err := repositoryPath(root, caseRel)
	if err != nil {
		return runtime.Snapshot{}, actionableError("InvestigationCase path is invalid: %v", err)
	}
	if _, err := os.Stat(casePath); err == nil {
		return runtime.Snapshot{}, fmt.Errorf("%w: Case artifact %s already exists; next: %s --case-id %s", ErrIngestConflict, caseRel, intakeNextCommand, caseID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return runtime.Snapshot{}, fmt.Errorf("inspect InvestigationCase artifact %s: %w", caseRel, err)
	}

	caseBytes, err := buildInitialCase(caseID, batch, caseRel, pointer.SHA256, request.GroupingRationale, baselineGeneration)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", caseBytes); err != nil {
		return runtime.Snapshot{}, actionableError("InvestigationCase schema is invalid before write: %v", err)
	}
	if err := writeExclusive(casePath, caseBytes); err != nil {
		return runtime.Snapshot{}, fmt.Errorf("write InvestigationCase %s: %w", caseRel, err)
	}
	caseSHA := sha256Hex(caseBytes)
	cleanup := func() {
		if !runtimeReferencesCase(statePath, caseRel, caseSHA) {
			_ = os.Remove(casePath)
		}
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	runtimeID := stringField(current.State["runtime_id"])
	commitRevision := runtimeCommitRevision(request.ExpectedRevision, current.State)
	cursor := map[string]any{"state": stringField(lifecycle["state"]), "phase": lifecycle["phase"]}
	pointerMap := map[string]any{
		"case_id":              caseID,
		"path":                 caseRel,
		"sha256":               caseSHA,
		"revision":             1,
		"status":               "investigating",
		"source_finding_ids":   stringSliceAny(sortedStrings(pointer.FindingIDs)),
		"observation_batch_id": batch.ObservationBatchID,
		"updated_at":           occurredAt.UTC().Format(time.RFC3339Nano),
	}
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	snapshot, err := updateRuntime(store, request.ExpectedRevision, runtime.Mutation{
		EventID:                fmt.Sprintf("evt-investigation-case-%s-r%d", caseID, commitRevision+1),
		TransitionID:           "INVESTIGATION-CASE-INGESTED",
		Event:                  "transition_committed",
		Actor:                  "orchestrator",
		IdempotencyKey:         fmt.Sprintf("runtime:investigation-ingest:%s:%d", batch.ObservationBatchID, commitRevision),
		RuntimeID:              runtimeID,
		From:                   cursor,
		To:                     cursor,
		EvidenceIDs:            []string{batch.ObservationBatchID},
		RequestID:              "investigation-ingest",
		BaselineGeneration:     baselineGeneration,
		GateID:                 "S8-INVESTIGATION-INTAKE",
		GateFingerprint:        "sha256:investigation-intake-v1",
		ProducerResponsibility: "Investigation Intake",
		Message:                fmt.Sprintf("investigation_case_ingested: %s consumes %s", caseID, batch.ObservationBatchID),
		OccurredAt:             occurredAt,
		Apply: func(state map[string]any) error {
			review, ok := state["review"].(map[string]any)
			if !ok {
				return actionableError("Runtime review section is missing; restore state.review before retry")
			}
			if existing, ok := review["investigation"].(map[string]any); ok && existing != nil {
				existingCase := stringField(existing["case_id"])
				existingBatch := stringField(existing["observation_batch_id"])
				if existingCase == caseID && existingBatch == batch.ObservationBatchID {
					return fmt.Errorf("%w (idempotent retry): Case %s already consumes batch %s; next: runtime investigation status --case-id %s", ErrAlreadyIngested, caseID, batch.ObservationBatchID, caseID)
				}
				return fmt.Errorf("%w: Runtime already points to Case %s/batch %s; requested Case %s/batch %s; next: runtime investigation status --case-id %s", ErrIngestConflict, existingCase, existingBatch, caseID, batch.ObservationBatchID, existingCase)
			}
			review["investigation"] = pointerMap
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
	if err != nil {
		cleanup()
		return runtime.Snapshot{}, err
	}
	return snapshot, nil
}

type batchPointer struct {
	BatchID    string
	Path       string
	SHA256     string
	FindingIDs []string
}

func observationBatchPointer(state map[string]any) (batchPointer, error) {
	review, ok := state["review"].(map[string]any)
	if !ok || review == nil {
		return batchPointer{}, fmt.Errorf("state.review is missing; S7 must seal an ObservationBatch before S8 intake; %s", intakeSealNext)
	}
	raw, ok := review["observation_batch"].(map[string]any)
	if !ok || raw == nil {
		return batchPointer{}, fmt.Errorf("state.review.observation_batch is missing; S7 must seal an ObservationBatch before S8 intake; %s", intakeSealNext)
	}
	pointer := batchPointer{BatchID: stringField(raw["batch_id"]), Path: stringField(raw["path"]), SHA256: stringField(raw["sha256"])}
	if pointer.BatchID == "" {
		return batchPointer{}, actionableError("state.review.observation_batch.batch_id is missing")
	}
	if pointer.Path == "" {
		return batchPointer{}, actionableError("state.review.observation_batch.path is missing")
	}
	if pointer.SHA256 == "" {
		return batchPointer{}, actionableError("state.review.observation_batch.sha256 is missing")
	}
	ids, err := stringSlice(raw["finding_ids"], "state.review.observation_batch.finding_ids")
	if err != nil {
		return batchPointer{}, err
	}
	pointer.FindingIDs = ids
	return pointer, nil
}

func buildInitialCase(caseID string, batch observationBatch, batchRef, batchSHA, rationale string, baseline int) ([]byte, error) {
	// RC-18 S8-M1: project the sealed Claim-coverage facts into the boundary
	// views instead of leaving them permanently empty. failure_boundary_refs
	// and evidence_gaps are content-addressed observations of what the sealed
	// batch already proves; cross_layer_trace stays null because the batch
	// carries no cross-layer topology and S8 never fabricates one.
	boundaryRefs, evidenceGaps := projectBatchViews(batch)
	document := map[string]any{
		"schema_version":           "1.0.0",
		"case_id":                  caseID,
		"revision":                 1,
		"status":                   "investigating",
		"source_finding_ids":       sortedStrings(batch.FindingIDs),
		"observation_batch_id":     batch.ObservationBatchID,
		"observation_batch_ref":    batchRef,
		"observation_batch_sha256": batchSHA,
		"baseline_generation":      baseline,
		"baseline_digest":          batch.SubjectDigest,
		"grouping_rationale":       strings.TrimSpace(rationale),
		"unexplained_finding_ids":  sortedStrings(batch.FindingIDs),
		"failure_boundary_refs":    boundaryRefs,
		"cross_layer_trace":        nil,
		"evidence_gaps":            evidenceGaps,
		"hypotheses":               []any{},
		"hypothesis_results":       []any{},
		"causal_model":             nil,
		"primary_root_cause":       nil,
		"blast_radius":             nil,
		"detection_gap":            nil,
		"route":                    nil,
		"route_reason":             nil,
		"repair_contract_ref":      nil,
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode InvestigationCase: %w", err)
	}
	return append(data, '\n'), nil
}

// projectBatchViews derives the initial failure_boundary_refs and
// evidence_gaps views from the sealed ObservationBatch. failure_boundary_refs
// names every source Finding artifact (its existence and hash were verified by
// validateFindingArtifacts) plus every blocked Claim's evidence refs — the
// boundaries the investigation must explain or resolve. evidence_gaps records
// one entry per blocked Claim (the failed precondition and the after-repair
// obligation) and per unobserved Claim (an immediate-stop capture gap).
func projectBatchViews(batch observationBatch) ([]string, []string) {
	boundarySet := map[string]struct{}{}
	for _, findingID := range batch.FindingIDs {
		if id := strings.TrimSpace(findingID); id != "" {
			boundarySet["finding:"+id] = struct{}{}
		}
	}
	// Never nil: the Case schema requires evidence_gaps to be an array, and a
	// nil slice JSON-encodes as null (breaking every downstream schema check
	// when no Claim is blocked or unobserved).
	gaps := []string{}
	for _, blocked := range batch.ClaimCoverageSummary.BlockedClaims {
		if strings.TrimSpace(blocked.ClaimID) == "" {
			continue
		}
		for _, ref := range blocked.EvidenceRefs {
			if ref = strings.TrimSpace(ref); ref != "" {
				boundarySet[ref] = struct{}{}
			}
		}
		gaps = append(gaps, fmt.Sprintf(
			"claim %s blocked by findings [%s]: precondition %s — %s (after-repair re-verification required)",
			blocked.ClaimID, strings.Join(sortedStrings(blocked.BlockingFindingIDs), ", "),
			strings.TrimSpace(blocked.FailedPrecondition.Kind), strings.TrimSpace(blocked.FailedPrecondition.Detail)))
	}
	for _, claimID := range batch.UnobservedClaimIDs {
		if id := strings.TrimSpace(claimID); id != "" {
			gaps = append(gaps, "claim "+id+" unobserved: immediate-stop capture gap; the repaired round owes this Claim a disposition")
		}
	}
	boundaries := make([]string, 0, len(boundarySet))
	for ref := range boundarySet {
		boundaries = append(boundaries, ref)
	}
	return sortedStrings(boundaries), gaps
}

func rejectExistingInvestigation(state map[string]any, caseID, batchID string) error {
	review, _ := state["review"].(map[string]any)
	existing, _ := review["investigation"].(map[string]any)
	if existing == nil {
		return nil
	}
	existingCase := stringField(existing["case_id"])
	existingBatch := stringField(existing["observation_batch_id"])
	if existingCase == caseID && existingBatch == batchID {
		return fmt.Errorf("%w (idempotent retry): Case %s already consumes batch %s; next: runtime investigation status --case-id %s", ErrAlreadyIngested, caseID, batchID, caseID)
	}
	return fmt.Errorf("%w: Runtime already points to Case %s/batch %s; requested Case %s/batch %s; next: runtime investigation status --case-id %s", ErrIngestConflict, existingCase, existingBatch, caseID, batchID, existingCase)
}

func exactFindingSet(pointer, batch []string) error {
	left, err := normalizeSet(pointer, "state.review.observation_batch.finding_ids")
	if err != nil {
		return err
	}
	right, err := normalizeSet(batch, "ObservationBatch.finding_ids")
	if err != nil {
		return err
	}
	if len(left) != len(right) {
		return fmt.Errorf("state has %v but sealed file has %v", left, right)
	}
	for index := range left {
		if left[index] != right[index] {
			return fmt.Errorf("state has %v but sealed file has %v", left, right)
		}
	}
	return nil
}

func validateFindingArtifacts(root string, state map[string]any, findingIDs []string) error {
	entities, _ := state["entities"].(map[string]any)
	rawFindings, _ := entities["findings"].([]any)
	rows := map[string]map[string]any{}
	for _, raw := range rawFindings {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		id := stringField(row["finding_id"])
		if id != "" {
			if _, exists := rows[id]; exists {
				return fmt.Errorf("entities.findings contains duplicate row for Finding %s", id)
			}
			rows[id] = row
		}
	}
	for _, findingID := range findingIDs {
		row := rows[findingID]
		if row == nil {
			return fmt.Errorf("Finding %s has no entities.findings row", findingID)
		}
		path := stringField(row["path"])
		declaredSHA := stringField(row["sha256"])
		if path == "" || declaredSHA == "" {
			return fmt.Errorf("Finding %s row is missing path or sha256", findingID)
		}
		absolute, err := repositoryPath(root, path)
		if err != nil {
			return fmt.Errorf("Finding %s path %q is invalid: %w", findingID, path, err)
		}
		data, err := os.ReadFile(absolute)
		if err != nil {
			return fmt.Errorf("Finding %s artifact %q is missing or unreadable: %w", findingID, path, err)
		}
		actualSHA := sha256Hex(data)
		if actualSHA != declaredSHA {
			return fmt.Errorf("Finding %s artifact %q sha256 drifted: state pins %s but disk is %s", findingID, path, declaredSHA, actualSHA)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("finding.schema.json", data); err != nil {
			return fmt.Errorf("Finding %s artifact %q does not satisfy finding.schema.json: %v", findingID, path, err)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("Finding %s artifact %q cannot be decoded: %v", findingID, path, err)
		}
		if stringField(document["finding_id"]) != findingID {
			return fmt.Errorf("Finding row %s points to artifact declaring finding_id %q", findingID, stringField(document["finding_id"]))
		}
		for _, field := range []string{"claim_id", "lens", "severity", "observation_mode"} {
			if rowValue, documentValue := stringField(row[field]), stringField(document[field]); rowValue != "" && rowValue != documentValue {
				return fmt.Errorf("Finding %s row field %s=%q disagrees with artifact value %q", findingID, field, rowValue, documentValue)
			}
		}
	}
	return nil
}

func normalizeSet(values []string, field string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%s must contain at least one Finding ID", field)
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s contains an empty Finding ID", field)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("%s contains duplicate Finding ID %q", field, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return sortedStrings(out), nil
}

func stringSlice(value any, field string) ([]string, error) {
	values, ok := value.([]any)
	if !ok {
		return nil, actionableError("%s is missing or must be an array", field)
	}
	result := make([]string, 0, len(values))
	for index, raw := range values {
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, actionableError("%s[%d] is missing or must be a non-empty string", field, index)
		}
		result = append(result, value)
	}
	return result, nil
}

func repositoryPath(root, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("must be a repository-relative path")
	}
	return filepath.Join(root, clean), nil
}

func writeExclusive(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create Case directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func runtimeReferencesCase(statePath, caseRel, caseSHA string) bool {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return false
	}
	var state map[string]any
	if json.Unmarshal(data, &state) != nil {
		return false
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	return pointer != nil && stringField(pointer["path"]) == caseRel && stringField(pointer["sha256"]) == caseSHA
}

func validCaseID(value string) bool {
	if !strings.HasPrefix(value, "investigation-case-") {
		return false
	}
	for _, char := range value[len("investigation-case-"):] {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '.' && char != '_' && char != '-' {
			return false
		}
	}
	return len(value) > len("investigation-case-")
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func stringSliceAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func stringField(value any) string {
	valueString, _ := value.(string)
	return valueString
}

func baselineGeneration(state map[string]any) (int, error) {
	baseline, ok := state["baseline"].(map[string]any)
	if !ok || baseline == nil {
		return 0, errors.New("baseline is missing")
	}
	value, ok := baseline["generation"].(float64)
	if !ok {
		if integer, integerOK := baseline["generation"].(int); integerOK {
			return integer, nil
		}
		return 0, errors.New("baseline.generation must be an integer")
	}
	return int(value), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func actionableError(format string, args ...any) error {
	return fmt.Errorf(format+"; next: %s", append(args, intakeNextCommand)...)
}
