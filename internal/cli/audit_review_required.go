package cli

import (
	"encoding/json"
	"fmt"
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
			integerValue(row["review_round"]) != integerValue(nestedStateValue(state, "review", "round")) {
			continue
		}
		data, err := workpacket.ReadFile(root, stringValue(row["path"]), 20<<20)
		if err != nil || workpacket.Digest(data) != row["sha256"] {
			return false
		}
		var envelope struct {
			Conclusion string `json:"conclusion"`
			Path       string `json:"audit_manifest_path"`
			SHA        string `json:"audit_manifest_sha256"`
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Conclusion != "review_required" || envelope.Path != manifestPath {
			return false
		}
		manifest, err := workpacket.ReadFile(root, manifestPath, 20<<20)
		return err == nil && workpacket.Digest(manifest) == envelope.SHA
	}
	return false
}

// A full-review decision changes the applicability of the old review; it
// does not assert a product edit. Reuse the existing impact envelope and
// original Writer without a RepairSession, fabricated Changeset or new form.
func recordAuditReviewImpact(root, manifestPath, sourceID, sourceRef, sourceSHA string) error {
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return err
	}
	if currentAuditKind(snapshot.State) != "acceptance" || !receivedAuditReviewPending(root, snapshot.State, manifestPath) {
		return fmt.Errorf("the original received review_required report is no longer current; preserve its receipt and inspect the current Main responsibility")
	}
	if _, ok := registeredAuditEvidence(snapshot.State, sourceID, sourceRef, sourceSHA); !ok {
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
	round := integerValue(nestedStateValue(snapshot.State, "review", "round"))
	generation := integerValue(nestedStateValue(snapshot.State, "baseline", "generation"))
	if plan.ReviewRound != round || ptr.ReviewRound != round || plan.BaselineGeneration != generation || len(plan.FrozenSubjects) == 0 {
		return fmt.Errorf("Main must restore the original current-round/generation frozen review scope at %s", planPath)
	}
	paths := make([]string, 0, len(plan.FrozenSubjects))
	seen := map[string]bool{}
	for _, subject := range plan.FrozenSubjects {
		if err := workpacket.CheckPath(root, subject.Path); err != nil {
			return fmt.Errorf("Main must restore the original review scope at %s: %w", planPath, err)
		}
		if !seen[subject.Path] {
			seen[subject.Path] = true
			paths = append(paths, subject.Path)
		}
	}
	sort.Strings(paths)
	id := "impact-" + sourceID + "-" + ptr.SHA256[:12]
	ref := filepath.ToSlash(filepath.Join(filepath.Dir(sourceRef), id+".json"))
	// subject_refs retain the exact sources of the original re-review decision.
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": id, "kind": "change_impact",
		"runtime_id": snapshot.State["runtime_id"], "baseline_generation": generation, "review_round": round,
		"producer_agent_id": "Main", "producer_responsibility": "Acceptance", "conclusion": "recorded",
		"subject_refs": []any{
			map[string]any{"path": sourceRef, "sha256": sourceSHA, "version": "1.0.0"},
			map[string]any{"path": ptr.Path, "sha256": ptr.SHA256, "version": fmt.Sprintf("r%d", ptr.Revision)},
		},
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeAuditArtifactExclusive(root, ref, data); err != nil {
		return err
	}
	if _, ok := registeredAuditEvidence(snapshot.State, id, ref, workpacket.Digest(data)); !ok {
		_, err = runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
			ExpectedRevision: snapshot.Revision, ID: id, Kind: "change_impact", Path: ref,
			ProducedBy: []string{"Main"}, ResponsibilityID: "Acceptance", ReviewRound: &round, ScopeRefs: paths,
			Validator: semantic.RuntimeCandidateValidator{},
		})
	}
	return err
}
