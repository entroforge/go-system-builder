package cli

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// A pending original negative report may resume its consumer, but a changed
// authoring file is never accepted merely because another tool completed.
func receivedAuditReviewPending(root string, state map[string]any, manifestPath string) bool {
	rows := stateEvidence(state)
	for i := len(rows) - 1; i >= 0; i-- {
		row, _ := rows[i].(map[string]any)
		if row["kind"] != "acceptance" || row["status"] != "valid" || row["invalidated_by"] != nil ||
			integerValue(row["baseline_generation"]) != integerValue(nestedStateValue(state, "baseline", "generation")) ||
			!acceptance.CurrentReviewArtifact(state, stringValue(row["review_plan_id"])) {
			continue
		}
		data, err := workpacket.ReadFile(root, stringValue(row["path"]), 20<<20)
		if err != nil {
			return false
		}
		var envelope struct {
			Conclusion string `json:"conclusion"`
			Path       string `json:"audit_manifest_path"`
			AnswerPath string `json:"audit_questionnaire_path"`
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Conclusion != "review_required" {
			return false
		}
		if envelope.AnswerPath == "" {
			envelope.AnswerPath = envelope.Path
		}
		return filepath.Clean(envelope.AnswerPath) == filepath.Clean(manifestPath)
	}
	return false
}

// A full-review decision changes the applicability of the old review; it
// does not assert a product edit. Reuse the existing impact envelope and
// original Writer without a RepairSession, fabricated Changeset or new form.
func recordAuditReviewImpact(root, manifestPath, sourceID, sourceRef string) error {
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return err
	}
	if currentAuditKind(snapshot.State) != "acceptance" || !receivedAuditReviewPending(root, snapshot.State, manifestPath) {
		return fmt.Errorf("the original received review_required report is no longer current; preserve its receipt and inspect the current Main responsibility")
	}
	if _, ok := registeredAuditEvidence(snapshot.State, sourceID, sourceRef); !ok {
		return fmt.Errorf("the original acceptance receipt differs; Main restores its exact source at %s", sourceRef)
	}
	ptr := review.PlanPointerFromState(snapshot.State)
	if ptr == nil {
		return fmt.Errorf("Main must restore the original review.plan pointer in %s", statePath)
	}
	planPath := resolveRootPath(root, ptr.Path)
	plan, _, err := review.LoadPlan(root, snapshot.State)
	if err != nil {
		return fmt.Errorf("Main must restore the original ReviewPlan at %s: %w", planPath, err)
	}
	generation := integerValue(nestedStateValue(snapshot.State, "baseline", "generation"))
	identity := acceptance.CurrentReviewIdentity(snapshot.State)
	if reviewmeta.CompareIdentity(reviewmeta.ArtifactIdentity(plan.ReviewPlanID), identity) == reviewmeta.Different || reviewmeta.CompareIdentity(ptr.Identity(), identity) == reviewmeta.Different || plan.BaselineGeneration != generation {
		return fmt.Errorf("Main must restore the original current review Plan and generation at %s", planPath)
	}
	paths := make([]string, 0, len(plan.FrozenSubjects))
	seen := map[string]bool{}
	for _, subject := range plan.FrozenSubjects {
		if !seen[subject.Path] {
			seen[subject.Path] = true
			paths = append(paths, subject.Path)
		}
	}
	// New Plans can carry actual changed paths as review-scope guidance without
	// claiming that each path has a frozen file digest. Their absence is not a
	// new qualification for the already accepted S10 decision.
	for _, item := range plan.CoverageInventory {
		if item.Kind == "post_repair_change" && item.SourceRef != "" && !seen[item.SourceRef] {
			seen[item.SourceRef] = true
			paths = append(paths, item.SourceRef)
		}
	}
	sort.Strings(paths)
	id := fmt.Sprintf("impact-%s-plan-r%d", sourceID, ptr.Revision)
	ref := filepath.ToSlash(filepath.Join(filepath.Dir(sourceRef), id+".json"))
	// subject_refs retain the exact sources of the original re-review decision.
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": id, "kind": "change_impact",
		"runtime_id": snapshot.State["runtime_id"], "baseline_generation": generation,
		"producer_agent_id": "Main", "producer_responsibility": "Acceptance", "conclusion": "recorded",
		"subject_refs": []any{
			map[string]any{"path": sourceRef, "version": "1.0.0"},
			map[string]any{"path": ptr.Path, "version": fmt.Sprintf("r%d", ptr.Revision)},
		},
	}
	acceptance.StampReviewIdentity(snapshot.State, envelope)
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeAuditArtifactExclusive(root, ref, data); err != nil {
		return err
	}
	if _, ok := registeredAuditEvidence(snapshot.State, id, ref); !ok {
		_, err = runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
			ExpectedRevision: snapshot.Revision, ID: id, Kind: "change_impact", Path: ref,
			ProducedBy: []string{"Main"}, ResponsibilityID: "Acceptance", ScopeRefs: paths,
			Validator: semantic.RuntimeCandidateValidator{},
		})
	}
	return err
}
