// Package investigation implements the S8 Finding and human-report intake
// boundary. It creates one immutable-at-revision InvestigationCase artifact
// and pins it into Runtime through the shared CAS writer. Investigation does
// not create BUGs or re-run the S7 symptom.
package investigation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	loopreview "github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

const intakeNextCommand = "runtime investigation ingest --root ."

const intakePhaseNext = "next: follow the actual accepted Finding handoff or human defect decision to S8; inspect the current Runtime if preparation failed"

// ErrAlreadyIngested means the current Runtime already points at the same
// Case. It is intentionally an error so a caller cannot
// mistake an idempotent retry for a new investigation revision.
var ErrAlreadyIngested = errors.New("investigation case already ingested")

// ErrIngestConflict means an existing Case or Runtime pointer has a different
// identity and must not be overwritten by intake.
var ErrIngestConflict = errors.New("investigation intake identity conflict")

// IngestRequest carries the caller's Runtime CAS revision and the only
// authoring decision allowed at intake: why the actual Finding set is a
// provisional grouping. CaseID is optional.
type IngestRequest struct {
	ExpectedRevision  int
	CaseID            string
	GroupingRationale string
	OccurredAt        time.Time
}

// Ingest consumes accepted Findings or an actual human defect decision.
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
	// intake (docs/control/loop-definition.json bug_resolution.investigation
	// entry_condition). Without this gate a stale batch pointer left behind by
	// a phase change — or a Case-route replay after route_consume clears the
	// pointer and leaves the phase — could enter the Case write path.
	lifecycle, _ := current.State["lifecycle"].(map[string]any)
	if state := stringField(lifecycle["state"]); state != "bug_resolution" || stringField(lifecycle["phase"]) != "investigation" {
		return runtime.Snapshot{}, actionableError(
			"investigation intake requires lifecycle bug_resolution.investigation but the Runtime cursor is %s.%s; %s",
			stringField(lifecycle["state"]), stringField(lifecycle["phase"]), intakePhaseNext)
	}

	baselineGeneration, err := baselineGeneration(current.State)
	if err != nil {
		return runtime.Snapshot{}, actionableError("Runtime baseline.generation is missing: %v", err)
	}
	findingIDs := []string{}
	humanID, humanPath := "", ""
	review, _ := current.State["review"].(map[string]any)
	if stringField(s8Map(current.State["last_transition"])["transition_id"]) == "TR-027" {
		rows, _ := current.State["evidence"].([]any)
		for index := len(rows) - 1; index >= 0; index-- {
			row, _ := rows[index].(map[string]any)
			if stringField(row["kind"]) != "human_decision" || integerValueOrZero(row["baseline_generation"]) != baselineGeneration {
				continue
			}
			if stringField(row["consumed_by"]) != "TR-027" {
				continue
			}
			source, sourceErr := humanDefectSource(root, current.State, []string{stringField(row["id"])})
			if sourceErr != nil {
				return runtime.Snapshot{}, actionableError("Main must restore the original human defect source: %v", sourceErr)
			}
			humanID, humanPath = source[0], source[1]
			break
		}
	}
	if humanID == "" && stringField(s8Map(current.State["last_transition"])["transition_id"]) == "TR-027" {
		return runtime.Snapshot{}, actionableError("Main must restore the original human_decision consumed by TR-027; no Finding is required for that human report")
	}

	acceptedFindings := loopreview.RoundFindings(current.State)
	directFindings := humanID == "" && len(acceptedFindings) > 0
	if humanID == "" && !directFindings {
		return runtime.Snapshot{}, actionableError("no accepted Finding or actual human defect decision is available for S8; Main should inspect the original handoff")
	}
	if directFindings {
		for _, finding := range acceptedFindings {
			if id := stringField(finding["finding_id"]); id != "" {
				findingIDs = append(findingIDs, id)
			}
		}
	}

	sourceID := humanID
	if directFindings {
		sourceID = reviewmeta.FromReview(review).PlanID
		if sourceID == "" && len(findingIDs) > 0 {
			sourceID = findingIDs[0]
		}
	}

	caseID := request.CaseID
	if strings.TrimSpace(caseID) == "" {
		caseID = "investigation-case-" + sourceID
	}
	if !validCaseID(caseID) {
		return runtime.Snapshot{}, actionableError("case_id %q is invalid; use investigation-case-<token>", caseID)
	}
	if err := rejectExistingInvestigation(current.State, caseID); err != nil {
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

	caseBytes, err := buildInitialCase(caseID, findingIDs, request.GroupingRationale, baselineGeneration)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	if humanID != "" {
		var document map[string]any
		if err := json.Unmarshal(caseBytes, &document); err != nil {
			return runtime.Snapshot{}, err
		}
		document["source_finding_ids"] = []string{}
		document["unexplained_finding_ids"] = []string{}
		document["failure_boundary_refs"] = []string{humanID, humanPath}
		caseBytes, err = json.MarshalIndent(document, "", "  ")
		if err != nil {
			return runtime.Snapshot{}, err
		}
		caseBytes = append(caseBytes, '\n')
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", caseBytes); err != nil {
		return runtime.Snapshot{}, actionableError("InvestigationCase schema is invalid before write: %v", err)
	}
	if err := writeExclusive(casePath, caseBytes); err != nil {
		return runtime.Snapshot{}, fmt.Errorf("write InvestigationCase %s: %w", caseRel, err)
	}
	cleanup := func() {
		_, _ = runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{
			ExpectedRevision: current.Revision, ArtifactPath: caseRel, ArtifactBytes: caseBytes,
			ReferencedPaths: stateStringReferences(current.State, caseRel),
		})
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	runtimeID := stringField(current.State["runtime_id"])
	cursor := map[string]any{"state": stringField(lifecycle["state"]), "phase": lifecycle["phase"]}
	pointerMap := map[string]any{
		"case_id":            caseID,
		"path":               caseRel,
		"revision":           1,
		"status":             "investigating",
		"source_finding_ids": stringSliceAny(sortedStrings(findingIDs)),
		"updated_at":         occurredAt.UTC().Format(time.RFC3339Nano),
	}
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	snapshot, err := updateRuntime(store, request.ExpectedRevision, runtime.Mutation{
		// G18 §1: the ingest is its own event domain, so the Case creation
		// never shares one event id with the Case revision or the accepted
		// hypothesis result of the same Case. The immutable Case artifact
		// cases/<case_id>.json may exist only once, so one Case is ingested
		// once and a retried ingest keeps this same identity.
		EventID:      "evt-investigation-case-ingested-" + caseID,
		TransitionID: "INVESTIGATION-CASE-INGESTED",
		Event:        "transition_committed",
		Actor:        "orchestrator",
		// G18 §1.3: the idempotency key carries the responsibility coordinate
		// (the Case) and the intent key (the accepted source that opened it).
		IdempotencyKey:         fmt.Sprintf("runtime:investigation-case-ingested:%s:%s", caseID, sourceID),
		RuntimeID:              runtimeID,
		From:                   cursor,
		To:                     cursor,
		EvidenceIDs:            []string{sourceID},
		RequestID:              "investigation-ingest",
		BaselineGeneration:     baselineGeneration,
		GateID:                 "S8-INVESTIGATION-INTAKE",
		ProducerResponsibility: "Investigation Intake",
		Message:                fmt.Sprintf("investigation_case_ingested: %s consumes %s", caseID, sourceID),
		OccurredAt:             occurredAt,
		Apply: func(state map[string]any) error {
			if humanID != "" {
				var original map[string]any
				if err := json.Unmarshal(caseBytes, &original); err != nil {
					return err
				}
				if _, err := humanCaseSource(root, state, original); err != nil {
					return err
				}

			}
			review, ok := state["review"].(map[string]any)
			if !ok {
				return actionableError("Runtime review section is missing; restore state.review before retry")
			}
			if existing, ok := review["investigation"].(map[string]any); ok && existing != nil {
				existingCase := stringField(existing["case_id"])
				if existingCase == caseID {
					return fmt.Errorf("%w: Case %s is already active", ErrAlreadyIngested, caseID)
				}
				return fmt.Errorf("%w: Runtime already points to Case %s, requested %s", ErrIngestConflict, existingCase, caseID)
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

func buildInitialCase(caseID string, findingIDs []string, rationale string, baseline int) ([]byte, error) {
	boundaries := make([]string, 0, len(findingIDs))
	for _, id := range findingIDs {
		if id != "" {
			boundaries = append(boundaries, "finding:"+id)
		}
	}
	document := map[string]any{
		"schema_version": "1.0.0", "case_id": caseID, "revision": 1, "status": "investigating",
		"source_finding_ids": sortedStrings(findingIDs), "baseline_generation": baseline,
		"grouping_rationale": strings.TrimSpace(rationale), "unexplained_finding_ids": sortedStrings(findingIDs),
		"failure_boundary_refs": sortedStrings(boundaries), "cross_layer_trace": nil, "evidence_gaps": []string{},
		"hypotheses": []any{}, "hypothesis_results": []any{}, "causal_model": nil, "primary_root_cause": nil,
		"blast_radius": nil, "detection_gap": nil, "route": nil, "route_reason": nil, "repair_contract_ref": nil,
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode InvestigationCase: %w", err)
	}
	return append(data, '\n'), nil
}

func rejectExistingInvestigation(state map[string]any, caseID string) error {
	review, _ := state["review"].(map[string]any)
	existing, _ := review["investigation"].(map[string]any)
	if existing == nil {
		return nil
	}
	existingCase := stringField(existing["case_id"])
	if existingCase == caseID {
		return fmt.Errorf("%w: Case %s is already active", ErrAlreadyIngested, caseID)
	}
	return fmt.Errorf("%w: Runtime already points to Case %s, requested %s", ErrIngestConflict, existingCase, caseID)
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
	// The immutable Case file must be durably named before the original
	// Runtime pointer transaction can refer to it. If the directory sync fails,
	// retain the file: its directory-entry durability is uncertain and a blind
	// unlink could destroy an artifact that recovery must inspect.
	if err := runtime.SyncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync parent directory for immutable Case %s (retained for recovery): %w", path, err)
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
	return pointer != nil && stringField(pointer["path"]) == caseRel
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

func actionableError(format string, args ...any) error {
	return fmt.Errorf(format+"; next: %s", append(args, intakeNextCommand)...)
}
