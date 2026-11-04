package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// EvidenceRequest describes one current evidence artifact to
// append to Runtime. Evidence is deliberately recorded through Store.Update
// so it participates in the same revision CAS and journal contract as every
// other Runtime mutation.
type EvidenceRequest struct {
	Context          context.Context `json:"-"`
	ExpectedRevision int
	ID               string
	Kind             string
	Path             string
	ProducedBy       []string
	ResponsibilityID string
	ScopeRefs        []string
	OccurredAt       time.Time
	Validator        CandidateValidator
	Submission       *SubmissionRequest
}

// IsRegisteredEvidenceKind reports whether kind can be persisted by
// RecordEvidence using the shared evidence catalog.
func IsRegisteredEvidenceKind(kind string) bool {
	return evidence.DefaultCatalog().IsRegisteredKind(kind)
}

// RecordEvidence adds one valid evidence item and commits it as a Runtime
// mutation. It rejects absolute or escaping paths, missing artifacts, invalid
// kinds, empty producers, duplicate IDs, and stale revisions before commit.
func RecordEvidence(root, statePath, journalPath string, request EvidenceRequest) (Snapshot, error) {
	if request.Submission != nil {
		frozen := *request.Submission
		frozen.AnswerJSON = append([]byte(nil), frozen.AnswerJSON...)
		frozen.EvidenceJSON = append([]byte(nil), frozen.EvidenceJSON...)
		request.Submission = &frozen
	}
	if request.ID == "" {
		return Snapshot{}, fmt.Errorf("evidence id is required")
	}
	catalog := evidence.DefaultCatalog()
	if !catalog.IsRegisteredKind(request.Kind) {
		return Snapshot{}, fmt.Errorf("unsupported evidence kind %q; registered kinds: %s; note: the Quality Gate records S10 artifacts as acceptance_record/release_audit_record, but registration uses --kind acceptance or --kind release_audit", request.Kind, strings.Join(catalog.RegisteredKinds(), ", "))
	}
	switch strings.TrimSpace(request.Kind) {
	case "repair_result_blocker":
		return Snapshot{}, fmt.Errorf("evidence kind %q is pipeline-owned: only the original S9 work_blocked lifecycle CAS may append its blocker artifact evidence; manual evidence registration would split the blocker pointer, immutable message, and evidence row", request.Kind)
	case "repair_result_resolution":
		return Snapshot{}, fmt.Errorf("evidence kind %q is pipeline-owned: only the original S9 blocker_resolved lifecycle CAS may append its resolution evidence; manual evidence registration would split the Agent resolution pointer, immutable message, and evidence row", request.Kind)
	}
	// finding_supplement is pipeline-owned: review.SubmitSupplement persists
	// the entities.finding_supplements index row and the evidence entry in one
	// runtime CAS transaction. A manual add would register an evidence row
	// with no supplement index row — a half-registered supplement — so this
	// entry point fails closed with the authoritative path.
	if strings.TrimSpace(request.Kind) == "finding_supplement" {
		return Snapshot{}, fmt.Errorf("evidence kind %q is pipeline-owned: persist supplements with `runtime finding-supplement` (appends the entities.finding_supplements row and the finding_supplement evidence entry in one CAS transaction); manual evidence registration would split the supplement index from the evidence log", request.Kind)
	}
	if len(request.ProducedBy) == 0 {
		return Snapshot{}, fmt.Errorf("evidence produced_by is required")
	}
	for _, producer := range request.ProducedBy {
		if strings.TrimSpace(producer) == "" {
			return Snapshot{}, fmt.Errorf("evidence produced_by contains an empty actor")
		}
	}
	// The original S2/S3 consumers hand their prepared reports to this Writer;
	// it chooses and records their paths in the same locked mutation.
	publishPlanning := (request.Kind == "planning_design" || request.Kind == "planning_contract") && request.Submission != nil && request.Submission.EvidenceJSON != nil
	cleanPath := ""
	var data []byte
	var err error
	if publishPlanning {
		data = request.Submission.EvidenceJSON
	} else {
		cleanPath, err = safeEvidencePath(root, request.Path)
		if err != nil {
			return Snapshot{}, err
		}
		data, err = os.ReadFile(filepath.Join(root, cleanPath))
		if err != nil {
			return Snapshot{}, fmt.Errorf("read evidence artifact: %w", err)
		}
	}
	type s10Association struct {
		ReviewPlanID       string `json:"review_plan_id"`
		RuntimeID          string `json:"runtime_id"`
		BaselineGeneration int    `json:"baseline_generation"`
	}
	var s10Binding s10Association
	if isS10ReportKind(request.Kind) {
		if err := json.Unmarshal(data, &s10Binding); err != nil {
			return Snapshot{}, fmt.Errorf("read S10 recording association: %w", err)
		}
	} else if err := acceptance.ValidateEvidenceArtifact(root, request.Kind, data); err != nil {
		return Snapshot{}, err
	}

	store := NewWriter(statePath, journalPath, root, request.Validator).WithContext(request.Context)
	snapshot, err := store.Snapshot()
	if err != nil {
		return Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	current := snapshot.State
	// S3 performs its professional reception inside the atomic Apply below.
	// Other submission kinds retain their existing preflight behavior.
	if request.Submission != nil && request.Kind != "planning_contract" {
		if _, err := prepareSubmission(root, current, request, data); errors.Is(err, errSubmissionAccepted) {
			return snapshot, nil
		} else if err != nil {
			return Snapshot{}, err
		}
	}
	if err := validateDocumentRegistration(current, request, data); err != nil {
		return Snapshot{}, err
	}
	runtimeID, _ := current["runtime_id"].(string)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	from := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	// The public recording entry must also read the existing professional
	// report; it cannot assume the normal save receiver already validated it.
	if isS10ReportKind(request.Kind) {
		baseline, _ := current["baseline"].(map[string]any)
		review, _ := current["review"].(map[string]any)
		if s10Binding.RuntimeID != runtimeID || !hypothesisJSONEqual(baseline["generation"], s10Binding.BaselineGeneration) || reviewmeta.CompareIdentity(reviewmeta.FromReview(review), reviewmeta.ArtifactIdentity(s10Binding.ReviewPlanID)) == reviewmeta.Different {
			return Snapshot{}, fmt.Errorf("S10 report does not describe the current responsibility")
		}
		reportData, reportType, conclusion, err := acceptance.ResolveS10Manifest(root, request.Kind, data)
		if err != nil {
			return Snapshot{}, err
		}
		var reportBinding s10Association
		if err := json.Unmarshal(reportData, &reportBinding); err != nil {
			return Snapshot{}, fmt.Errorf("read S10 referenced report association: %w", err)
		}
		if reportBinding.RuntimeID != s10Binding.RuntimeID || reportBinding.BaselineGeneration != s10Binding.BaselineGeneration ||
			reviewmeta.CompareIdentity(reviewmeta.ArtifactIdentity(reportBinding.ReviewPlanID), reviewmeta.ArtifactIdentity(s10Binding.ReviewPlanID)) == reviewmeta.Different {
			return Snapshot{}, fmt.Errorf("S10 referenced report association does not match the recording responsibility")
		}
		if _, err := acceptance.ValidateForOutcome(reportData, reportType, conclusion); err != nil {
			return Snapshot{}, fmt.Errorf("S10 %s evidence manifest is invalid: %w", reportType, err)
		}
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	producedBy := append([]string(nil), request.ProducedBy...)
	scopeRefs, err := expandRolloverScopeRefs(request.ScopeRefs, runtimeID)
	if err != nil {
		return Snapshot{}, err
	}
	// G18: the evidence id is the evidence identity, so the event id is named
	// by it. The journal's global event-id uniqueness check therefore runs
	// before the Apply closure below, which used to carry the duplicate
	// rejection. A repeated record of the same evidence id must keep its
	// original observable outcome, so the same order the Apply closure uses is
	// mirrored here: the submission reception check first, then the duplicate
	// id rejection. Neither path writes Runtime state or a journal fact.
	if evidenceRegistered(current, request.ID) {
		if request.Submission != nil {
			if _, submissionErr := prepareSubmission(root, current, request, data); submissionErr != nil {
				return Snapshot{}, submissionErr
			}
		}
		return Snapshot{}, fmt.Errorf("evidence %s is already registered", request.ID)
	}
	mutation := Mutation{
		EventID:        fmt.Sprintf("evt-evidence-%s", request.ID),
		TransitionID:   "EVIDENCE-RECORD",
		Event:          "evidence_recorded",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:evidence:%s", request.ID),
		RuntimeID:      runtimeID,
		From:           from,
		To:             from,
		EvidenceIDs:    []string{request.ID},
		Message:        "Recorded a current evidence artifact.",
		OccurredAt:     occurredAt,
		Apply: func(state map[string]any) error {
			if isS10ReportKind(request.Kind) {
				baseline, _ := state["baseline"].(map[string]any)
				review, _ := state["review"].(map[string]any)
				if state["runtime_id"] != s10Binding.RuntimeID || !hypothesisJSONEqual(baseline["generation"], s10Binding.BaselineGeneration) || reviewmeta.CompareIdentity(reviewmeta.FromReview(review), reviewmeta.ArtifactIdentity(s10Binding.ReviewPlanID)) == reviewmeta.Different {
					return fmt.Errorf("current S10 responsibility changed before recording")
				}
			}
			var submission SubmissionRecord
			if request.Submission != nil {
				var err error
				submission, err = prepareSubmission(root, state, request, data)
				if err != nil {
					return err
				}
			}
			if err := validateDocumentRegistration(state, request, data); err != nil {
				return err
			}
			if publishPlanning {
				ctx := request.Context
				if ctx == nil {
					ctx = context.Background()
				}
				path, err := workpacket.PublishS9Questionnaire(ctx, root, request.Path, data, int64(len(data)))
				if err != nil {
					return fmt.Errorf("publish original planning evidence: %w", err)
				}
				cleanPath, err = safeEvidencePath(root, path)
				if err != nil {
					return err
				}
			}
			items, ok := state["evidence"].([]any)
			if !ok {
				return fmt.Errorf("runtime evidence must be an array")
			}
			if evidenceRegistered(state, request.ID) {
				return fmt.Errorf("evidence %s is already registered", request.ID)
			}
			baseline, ok := state["baseline"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime baseline must be an object")
			}
			generation, err := integerField(baseline, "generation")
			if err != nil {
				return err
			}
			row := map[string]any{
				"id":                  request.ID,
				"kind":                request.Kind,
				"path":                cleanPath,
				"status":              "valid",
				"baseline_generation": generation,
				"produced_by":         producedBy,
				"invalidated_by":      nil,
				"invalidation_rule":   nil,
				"invalidation_reason": nil,
				"responsibility_id":   nullableString(request.ResponsibilityID),
				"scope_refs":          scopeRefs,
			}
			if isS10ReportKind(request.Kind) {
				if s10Binding.ReviewPlanID != "" {
					row["review_plan_id"] = s10Binding.ReviewPlanID
				}
			}
			items = append(items, row)
			state["evidence"] = items
			if request.Submission != nil {
				if err := appendSubmission(state, submission); err != nil {
					return err
				}
			}
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	}
	if request.ExpectedRevision < 0 {
		next, err := store.UpdateCurrent(mutation)
		if err != nil && request.Submission != nil {
			// The journal rejects duplicate event IDs before Apply. Resolve a
			// concurrent replay from the atomically committed reception row,
			// never from the artifact's mere presence or the outer error text.
			latest, readErr := store.Snapshot()
			if readErr == nil {
				if request.Kind == "planning_contract" {
					if _, accepted, findErr := FindSubmission(latest.State, request.Submission.PacketID); findErr != nil || !accepted {
						return next, err
					}
				}
				_, replayErr := prepareSubmission(root, latest.State, request, data)
				if errors.Is(replayErr, errSubmissionAccepted) {
					return latest, nil
				}
				if errors.Is(replayErr, ErrSubmissionConflict) {
					return Snapshot{}, replayErr
				}
			}
		}
		return next, err
	}
	return store.Update(request.ExpectedRevision, mutation)
}

// evidenceRegistered reports whether the Runtime already holds an evidence row
// with this identity. The evidence id is the evidence identity, so it is the
// semantic coordinate of the recording event.
func evidenceRegistered(state map[string]any, id string) bool {
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item != nil && item["id"] == id {
			return true
		}
	}
	return false
}

// expandRolloverScopeRefs resolves the explicit `runtime_rollover:current`
// token to the current runtime identity. This lets a human approval evidence
// item bind to its terminal runtime without manually predicting the Runtime
// revision.
func expandRolloverScopeRefs(scopeRefs []string, runtimeID string) ([]string, error) {
	if runtimeID == "" {
		return nil, fmt.Errorf("runtime id is required for rollover scope")
	}
	result := append([]string{}, scopeRefs...)
	for index, scope := range result {
		if scope == "runtime_rollover:current" {
			result[index] = fmt.Sprintf("runtime_rollover:%s", runtimeID)
		}
	}
	return result, nil
}

func safeEvidencePath(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("evidence path is required")
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("evidence path must stay within repository: %q", path)
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
		return "", fmt.Errorf("resolve evidence path symlinks: %w", err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("evidence path must stay within repository: %q", path)
	}
	return filepath.ToSlash(clean), nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// isS10ReportKind identifies the original S10 professional reports.
func isS10ReportKind(kind string) bool {
	switch kind {
	case "acceptance", "acceptance_record", "release_audit", "release_audit_record":
		return true
	default:
		return false
	}
}

// Bound document reviews are gate envelopes, not arbitrary report attachments.
// Unbound legacy/bootstrap artifacts retain their existing compatibility.
func validateDocumentRegistration(state map[string]any, req EvidenceRequest, data []byte) error {
	if req.Kind != "document_review" {
		return nil
	}
	if req.Submission == nil && PacketEnabled(state) {
		life, _ := state["lifecycle"].(map[string]any)
		baseline, _ := state["baseline"].(map[string]any)
		entities, _ := state["entities"].(map[string]any)
		rows, _ := entities["work_packets"].([]any)
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			b, _ := row["binding"].(map[string]any)
			entry, eErr := integerField(b, "stage_entry")
			currentEntry, cErr := integerField(life, "phase_revision")
			g, gErr := integerField(baseline, "generation")
			if eErr == nil && cErr == nil && gErr == nil && life["state"] == "document_verification" && entry == currentEntry && numberBindingGeneration(b) == g && b["runtime_id"] == state["runtime_id"] && b["responsibility"] == req.ResponsibilityID {
				return fmt.Errorf("issued S5 responsibility requires its saved-answer consumer; manual evidence registration cannot bypass its current attempt")
			}
		}
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["id"] == nil || bound["id"] == "" {
		return nil
	}
	var e struct {
		Schema         string `json:"schema_version"`
		ID             string `json:"evidence_id"`
		Kind           string `json:"kind"`
		Runtime        string `json:"runtime_id"`
		Generation     int    `json:"baseline_generation"`
		Producer       string `json:"producer_agent_id"`
		Responsibility string `json:"producer_responsibility"`
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return fmt.Errorf("document_review requires a JSON evidence envelope: %w", err)
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return err
	}
	producerOK := false
	for _, p := range req.ProducedBy {
		if p == e.Producer && p != "" {
			producerOK = true
		}
	}
	if e.ID != req.ID {
		return fmt.Errorf("document_review evidence_id mismatch: registration %q, envelope %q; use the envelope ID or obtain a correctly reissued envelope", req.ID, e.ID)
	}
	if e.Schema == "" || e.Kind != req.Kind || e.Runtime != state["runtime_id"] || e.Generation != generation || !producerOK || e.Responsibility == "" || e.Responsibility != req.ResponsibilityID {
		return fmt.Errorf("document_review envelope binding mismatch (schema/kind/runtime/generation/producer/responsibility)")
	}
	return nil
}
