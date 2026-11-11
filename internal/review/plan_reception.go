package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// PlanReception reports what the original Plan consumer actually did.
type PlanReception string

const (
	PlanReceptionAccepted PlanReception = "accepted"
	PlanReceptionSame     PlanReception = "same"
	PlanReceptionRevised  PlanReception = "revised"
)

// ReceivePlanAnswer accepts a first Plan or a natural amendment to the same
// responsibility. It does not turn observation into an author submission.
func ReceivePlanAnswer(root, statePath, journalPath string, request PlanRequest) (loopruntime.Snapshot, PlanReception, error) {
	data := request.AnswerBytes
	if data == nil {
		var err error
		data, err = os.ReadFile(request.PlanPath)
		if err != nil {
			return loopruntime.Snapshot{}, "", fmt.Errorf("read current ReviewPlan answer: %w", err)
		}
	}
	store := loopruntime.NewStore(statePath, journalPath).WithContext(request.Context)
	snapshot, err := store.Snapshot()
	if err != nil {
		return snapshot, "", err
	}
	// The Runtime write count never qualifies this professional reception
	// (L4-revision-usage.md §4/§8, G18 §3.1): an unrelated Runtime commit
	// must not refuse a legitimate Plan registration. What decides this
	// reception is the three-piece proof below — the received Plan row (the
	// registered Plan pointer and its identity), the journal-level dedup of
	// evt-review-plan-<planID>, and the Plan pointer's own revision.
	//
	// The registration commits against the state it reads itself, so the
	// downstream registration receives the omission marker instead of a
	// caller-held counter.
	request.ExpectedRevision = -1
	ptr := PlanPointerFromState(snapshot.State)
	registered := ptr != nil && reviewmeta.CompareIdentity(ptr.Identity(), activeReviewIdentity(snapshot.State)) != reviewmeta.Different
	review, _ := snapshot.State["review"].(map[string]any)
	entry, _ := review["round_entry"].(map[string]any)
	seed := ptr != nil && entry["transition_id"] == "TR-012" && ptr.Revision == 1 && entry["review_plan_seed_ref"] == ptr.Path
	// The current Main questionnaire is bound to its issued entry; its machine
	// identity is maintained by the system even on the first reception. For a
	// repair seed, only reuse an actually associated entry and seed pointer.
	entryIdentity := reviewmeta.FromRoundEntry(review)
	identity := entryIdentity.PlanID
	if identity == "" && ptr != nil && reviewmeta.CompareIdentity(ptr.Identity(), activeReviewIdentity(snapshot.State)) == reviewmeta.Same {
		// Preserve the existing continuation of numbered, already accepted plans.
		identity = ptr.PlanID
	}
	associated := identity != ""
	if entry["transition_id"] == "TR-012" && (!registered || seed) {
		associated = seed && reviewmeta.CompareIdentity(ptr.Identity(), activeReviewIdentity(snapshot.State)) == reviewmeta.Same
	}
	if registered && !seed && reviewmeta.CompareIdentity(ptr.Identity(), activeReviewIdentity(snapshot.State)) != reviewmeta.Same {
		associated = false
	}
	if lifecycle, _ := snapshot.State["lifecycle"].(map[string]any); lifecycle["state"] == "verification" && associated {
		expected := PlanQuestionnairePath(root, snapshot.State)
		actualAbs, actualErr := filepath.Abs(request.PlanPath)
		expectedAbs, expectedErr := filepath.Abs(expected)
		if expected != "" && actualErr == nil && expectedErr == nil && filepath.Clean(actualAbs) == filepath.Clean(expectedAbs) && workpacket.ValidateUniqueJSON(data) == nil {
			var object map[string]json.RawMessage
			if json.Unmarshal(data, &object) == nil && object != nil {
				encodedIdentity, _ := json.Marshal(identity)
				object["review_plan_id"] = encodedIdentity
				if normalized, err := json.Marshal(object); err == nil {
					data = normalized
				}
			}
		}
	}
	request.AnswerBytes = data
	if !registered || seed {
		snapshot, err = RegisterPlan(root, statePath, journalPath, request)
		return snapshot, PlanReceptionAccepted, err
	}

	acceptedPath := ptr.Path
	if !filepath.IsAbs(acceptedPath) {
		acceptedPath = filepath.Join(root, filepath.FromSlash(acceptedPath))
	}
	accepted, err := os.ReadFile(acceptedPath)
	if err != nil {
		return snapshot, "", fmt.Errorf("read accepted ReviewPlan for comparison: %w; preserve the accepted facts and current answer; their relationship is unknown", err)
	}
	same, err := sameProfessionalPlan(data, accepted)
	if err != nil {
		return snapshot, "", err
	}
	if same {
		return snapshot, PlanReceptionSame, nil
	}
	snapshot, err = RevisePlan(root, statePath, journalPath, ReviseRequest{
		Context: request.Context, ExpectedRevision: -1, PlanPath: request.PlanPath,
		OccurredAt: request.OccurredAt, answerBytes: data, natural: true,
	})
	return snapshot, PlanReceptionRevised, err
}

// Compare actual author content while ignoring only the framework's legacy
// machine fields. Do not normalize arbitrary professional strings or fields.
func sameProfessionalPlan(authored, accepted []byte) (bool, error) {
	if err := workpacket.ValidateUniqueJSON(authored); err != nil {
		return false, &PlanAnswerError{fmt.Errorf("decode current ReviewPlan: %w", err)}
	}
	// Compare the actual ReviewPlan fields; unrelated JSON metadata is neither
	// authority nor part of the professional Plan. The saved input stays intact.
	var authoredPlan, acceptedPlan Plan
	if err := json.Unmarshal(authored, &authoredPlan); err != nil {
		return false, &PlanAnswerError{fmt.Errorf("decode current ReviewPlan: %w", err)}
	}
	if err := json.Unmarshal(accepted, &acceptedPlan); err != nil {
		return false, fmt.Errorf("decode accepted ReviewPlan: %w", err)
	}
	left, err := json.Marshal(authoredPlan)
	if err != nil {
		return false, err
	}
	right, err := json.Marshal(acceptedPlan)
	if err != nil {
		return false, err
	}
	var a, b map[string]any
	if err := json.Unmarshal(left, &a); err != nil {
		return false, &PlanAnswerError{fmt.Errorf("decode current ReviewPlan: %w", err)}
	}
	if err := json.Unmarshal(right, &b); err != nil {
		return false, fmt.Errorf("decode accepted ReviewPlan: %w", err)
	}
	for _, key := range []string{"change_impact", "coverage_justification", "verification_artifact_workspace", "e2e_coverage_state"} {
		if a[key] == nil {
			delete(a, key)
		}
		if b[key] == nil {
			delete(b, key)
		}
	}
	if a["e2e_coverage_state"] == "" {
		delete(a, "e2e_coverage_state")
	}
	if b["e2e_coverage_state"] == "" {
		delete(b, "e2e_coverage_state")
	}
	for _, object := range []map[string]any{a, b} {
		if object["dispatch_capacity_policy"] == "" || object["dispatch_capacity_policy"] == nil {
			delete(object, "dispatch_capacity_policy")
		}
		if subjects, ok := object["frozen_subjects"].([]any); ok && len(subjects) == 0 || object["frozen_subjects"] == nil {
			delete(object, "frozen_subjects")
		}
		claims, _ := object["claims"].([]any)
		for _, raw := range claims {
			claim, _ := raw.(map[string]any)
			for _, key := range []string{"target", "assertion", "oracle", "method", "applicability", "lens"} {
				if claim[key] == nil || claim[key] == "" {
					delete(claim, key)
				}
			}
			if refs, ok := claim["source_refs"].([]any); ok && len(refs) == 0 || claim["source_refs"] == nil {
				delete(claim, "source_refs")
			}
		}
	}
	// Older registrations expanded a unique short CASE reference and filled an
	// asset id. Reuse only that accepted content, without opening sources.
	currentAssets, _ := a["e2e_assets"].([]any)
	acceptedAssets, _ := b["e2e_assets"].([]any)
	if len(currentAssets) == len(acceptedAssets) {
		var plan Plan
		_ = json.Unmarshal(right, &plan)
		for i, raw := range currentAssets {
			current, ok := raw.(map[string]any)
			prior, priorOK := acceptedAssets[i].(map[string]any)
			if !ok || !priorOK {
				continue
			}
			if (current["asset_id"] == nil || current["asset_id"] == "") && prior["asset_id"] != nil {
				current["asset_id"] = prior["asset_id"]
			}
			ref, ok := current["case_ref"].(string)
			if !ok || strings.Contains(ref, "#") {
				continue
			}
			matches := map[string]bool{}
			for _, asset := range plan.E2EAssets {
				_, id, err := caseReferenceParts(asset.CaseRef)
				if err == nil && id == ref {
					matches[asset.CaseRef] = true
				}
			}
			if len(matches) == 1 {
				for exact := range matches {
					current["case_ref"] = exact
				}
			}
		}
	}
	return reflect.DeepEqual(a, b), nil
}

// PlanObservationSnapshot keeps the first authoring baseline without copying
// legacy framework digest fields into another persistent artifact.
func PlanObservationSnapshot(data []byte) ([]byte, error) {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

// SamePlanObservation compares original JSON expressions without creating a
// second hash or re-reading the professional sources named by the Plan.
func SamePlanObservation(current, baseline []byte) (bool, error) {
	return sameProfessionalPlan(current, baseline)
}

// Empty and omitted optional lists express the same Claim responsibility.
func sameClaimContent(a, b Claim) bool {
	normalize := func(c Claim) Claim {
		if len(c.SourceRefs) == 0 {
			c.SourceRefs = nil
		}
		if len(c.RequiredEvidence) == 0 {
			c.RequiredEvidence = nil
		}
		if len(c.DependsOn) == 0 {
			c.DependsOn = nil
		}
		if len(c.ResourceLocks) == 0 {
			c.ResourceLocks = nil
		}
		return c
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}
