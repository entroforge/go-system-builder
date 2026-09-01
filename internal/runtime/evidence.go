package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/evidence"
)

// EvidenceRequest describes one current, fingerprinted evidence artifact to
// append to Runtime. Evidence is deliberately recorded through Store.Update
// so it participates in the same revision CAS and journal contract as every
// other Runtime mutation.
type EvidenceRequest struct {
	ExpectedRevision int
	ID               string
	Kind             string
	Path             string
	ProducedBy       []string
	ResponsibilityID string
	ReviewRound      *int
	ScopeRefs        []string
	OccurredAt       time.Time
	Validator        CandidateValidator
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
	if request.ID == "" {
		return Snapshot{}, fmt.Errorf("evidence id is required")
	}
	catalog := evidence.DefaultCatalog()
	if !catalog.IsRegisteredKind(request.Kind) {
		return Snapshot{}, fmt.Errorf("unsupported evidence kind %q; registered kinds: %s; note: the Quality Gate records S10 artifacts as acceptance_record/release_audit_record, but registration uses --kind acceptance or --kind release_audit (bind --review-round to the current round; S10 envelopes also auto-inherit it from the envelope file)", request.Kind, strings.Join(catalog.RegisteredKinds(), ", "))
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
	cleanPath, err := safeEvidencePath(root, request.Path)
	if err != nil {
		return Snapshot{}, err
	}
	data, err := os.ReadFile(filepath.Join(root, cleanPath))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read evidence artifact: %w", err)
	}
	if err := acceptance.ValidateEvidenceArtifact(root, request.Kind, data); err != nil {
		return Snapshot{}, err
	}
	// S10 evidence must bind to the current review round (L3-S10 §4.2): the
	// s10 board and gates read entry.review_round verbatim. The envelope
	// already carries that fact, so a registration that omits --review-round
	// inherits it instead of persisting a round-less row the S10 layer would
	// immediately reject as stale (2026-08-28 walkthrough defect B).
	if request.ReviewRound == nil && isS10RoundScopedKind(request.Kind) {
		if round, ok := s10EnvelopeReviewRound(data); ok {
			request.ReviewRound = &round
		}
	}

	store := NewWriter(statePath, journalPath, root, request.Validator)
	snapshot, err := store.Snapshot()
	if err != nil {
		return Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	current := snapshot.State
	runtimeID, _ := current["runtime_id"].(string)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	from := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	// S10 registration must consume the same authoritative finite inventory
	// as the Quality Gate once the Runtime has entered a real review round.
	// Keep the round-zero bootstrap fixture/legacy path structural-only; a
	// production S10 state has a bound REQ and pinned ReviewPlan, which makes
	// the non-self-declared denominator reconstructible here as well.
	if isS10RoundScopedKind(request.Kind) && acceptance.S10AuthorityAvailable(current) {
		manifestType := "acceptance"
		if request.Kind == "release_audit" || request.Kind == "release_audit_record" {
			manifestType = "release_audit"
		}
		var envelope struct {
			Conclusion string `json:"conclusion"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil || strings.TrimSpace(envelope.Conclusion) == "" {
			return Snapshot{}, fmt.Errorf("S10 %s evidence requires a non-empty conclusion before authoritative inventory validation", manifestType)
		}
		baseline, baselineErr := acceptance.BuildS10ExternalBaseline(root, current, nil)
		if baselineErr != nil {
			return Snapshot{}, fmt.Errorf("S10 external baseline is unverifiable: %w; restore the current-generation completion/change-impact artifacts", baselineErr)
		}
		authority, authorityErr := acceptance.BuildS10InventoryAuthority(root, current, baseline)
		if authorityErr != nil {
			return Snapshot{}, fmt.Errorf("S10 authoritative inventory is unverifiable: %w; restore the current bound REQ, contract/TASK registrations, and pinned S7 ReviewPlan", authorityErr)
		}
		if _, err := acceptance.ValidateForOutcomeWithBaselineAndAuthority(data, manifestType, strings.TrimSpace(envelope.Conclusion), baseline, authority); err != nil {
			return Snapshot{}, err
		}
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	producedBy := append([]string(nil), request.ProducedBy...)
	commitRevision := request.ExpectedRevision
	if commitRevision < 0 {
		commitRevision = snapshot.Revision
	}
	scopeRefs, err := expandRolloverScopeRefs(request.ScopeRefs, runtimeID, commitRevision+1)
	if err != nil {
		return Snapshot{}, err
	}
	sha := sha256Hex(data)
	mutation := Mutation{
		EventID:        fmt.Sprintf("evt-evidence-%s-r%d", request.ID, commitRevision+1),
		TransitionID:   "EVIDENCE-RECORD",
		Event:          "evidence_recorded",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:evidence:%s:%d", request.ID, commitRevision),
		RuntimeID:      runtimeID,
		From:           from,
		To:             from,
		EvidenceIDs:    []string{request.ID},
		Message:        "Recorded a current fingerprinted evidence artifact.",
		OccurredAt:     occurredAt,
		Apply: func(state map[string]any) error {
			items, ok := state["evidence"].([]any)
			if !ok {
				return fmt.Errorf("runtime evidence must be an array")
			}
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item != nil && item["id"] == request.ID {
					return fmt.Errorf("evidence %s is already registered", request.ID)
				}
			}
			baseline, ok := state["baseline"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime baseline must be an object")
			}
			generation, err := integerField(baseline, "generation")
			if err != nil {
				return err
			}
			var reviewRound any
			if request.ReviewRound != nil {
				if *request.ReviewRound < 1 {
					return fmt.Errorf("review round must be at least 1")
				}
				reviewRound = *request.ReviewRound
			}
			items = append(items, map[string]any{
				"id":                  request.ID,
				"kind":                request.Kind,
				"path":                cleanPath,
				"sha256":              sha,
				"status":              "valid",
				"baseline_generation": generation,
				"review_round":        reviewRound,
				"produced_by":         producedBy,
				"invalidated_by":      nil,
				"invalidation_rule":   nil,
				"invalidation_reason": nil,
				"responsibility_id":   nullableString(request.ResponsibilityID),
				"scope_refs":          scopeRefs,
			})
			state["evidence"] = items
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	}
	if request.ExpectedRevision < 0 {
		return store.UpdateCurrent(mutation)
	}
	return store.Update(request.ExpectedRevision, mutation)
}

// expandRolloverScopeRefs resolves the explicit `runtime_rollover:current`
// token to the revision that this evidence commit will produce. This lets a
// human approval evidence item bind to its terminal runtime without manually
// predicting Store.Update's revision increment.
func expandRolloverScopeRefs(scopeRefs []string, runtimeID string, committedRevision int) ([]string, error) {
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

// isS10RoundScopedKind reports whether an evidence kind is consumed by the
// S10 board and gates with a mandatory current-round binding.
func isS10RoundScopedKind(kind string) bool {
	switch kind {
	case "acceptance", "acceptance_record", "release_audit", "release_audit_record":
		return true
	default:
		return false
	}
}

// s10EnvelopeReviewRound reads review_round out of an S10 evidence envelope.
// ok is false when the envelope omits it or the value cannot be a round.
func s10EnvelopeReviewRound(data []byte) (int, bool) {
	var envelope struct {
		ReviewRound int `json:"review_round"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.ReviewRound < 1 {
		return 0, false
	}
	return envelope.ReviewRound, true
}
