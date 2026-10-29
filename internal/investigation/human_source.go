package investigation

import (
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Human intake reuses its original decision and Case revision history. These
// readers add no source registry or author-maintained provenance fields.
func humanCaseSource(root string, state, document map[string]any) ([]string, error) {
	original := document
	history, err := objectArrayAllowEmpty(document["revision_history"], "Case.revision_history")
	if err != nil {
		return nil, err
	}
	if len(history) > 0 {
		first := history[0]
		if integerValueOrZero(first["revision"]) != 1 {
			return nil, fmt.Errorf("human Case original revision is unavailable; Main must restore its original source")
		}
		data, err := workpacket.ReadFile(root, stringField(first["path"]), hypothesisResultPacketReadLimit)
		if err != nil || workpacket.Digest(data) != stringField(first["sha256"]) {
			return nil, fmt.Errorf("human Case original revision bytes/SHA differ: %v", err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", data); err != nil {
			return nil, err
		}
		original = make(map[string]any)
		if err := json.Unmarshal(data, &original); err != nil {
			return nil, err
		}
	}
	if stringField(original["case_id"]) != stringField(document["case_id"]) || integerValueOrZero(original["revision"]) != 1 ||
		integerValueOrZero(original["baseline_generation"]) != integerValueOrZero(document["baseline_generation"]) ||
		integerValueOrZero(original["baseline_generation"]) != integerValueOrZero(s8Map(state["baseline"])["generation"]) {
		return nil, fmt.Errorf("human Case original identity/generation differs from the current Case")
	}
	for _, candidate := range []map[string]any{original, document} {
		if len(stringSliceValues(candidate["source_finding_ids"])) > 0 || stringField(candidate["observation_batch_id"]) != "" || stringField(candidate["observation_batch_ref"]) != "" || stringField(candidate["observation_batch_sha256"]) != "" {
			return nil, fmt.Errorf("human Case cannot replace a Finding/ObservationBatch source")
		}
	}
	if pin := stringField(original["baseline_digest"]); pin != "" && pin != stringField(document["baseline_digest"]) {
		return nil, fmt.Errorf("human Case original baseline_digest was changed; Main must reassess the original baseline")
	}
	return humanDefectSource(root, state, stringSliceValues(original["failure_boundary_refs"]))
}

func humanSourceRef(ref string, source []string) bool {
	for _, original := range source {
		if ref == original {
			return true
		}
	}
	if len(source) == 0 {
		return false
	}
	id, indexed, err := evidence.ReferenceID(ref)
	return err == nil && indexed && id == source[0]
}

func withoutHumanSource(refs, source []string) []string {
	var remaining []string
	for _, ref := range refs {
		if !humanSourceRef(ref, source) {
			remaining = append(remaining, ref)
		}
	}
	return remaining
}

func humanDefectSource(root string, state map[string]any, refs []string) ([]string, error) {
	var found map[string]any
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		id, path := stringField(row["id"]), stringField(row["path"])
		matched := false
		for _, ref := range refs {
			if humanSourceRef(ref, []string{id, path}) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("human Case has multiple original decision sources; Main must resolve the original intake")
		}
		found = row
	}
	if found == nil || stringField(found["kind"]) != "human_decision" || stringField(found["consumed_by"]) != "TR-027" {
		return nil, fmt.Errorf("human Case requires its original human_decision consumed by TR-027")
	}
	id, path := stringField(found["id"]), stringField(found["path"])
	if err := ValidateEvidenceRefs([]string{id}, EvidenceAttestationOptions{State: state, Root: root, RequireSHA: true, RequireKind: []string{"human_decision"}, RequireReviewRound: currentReviewRound(state)}); err != nil {
		return nil, fmt.Errorf("original human decision: %w", err)
	}
	data, err := workpacket.ReadFile(root, path, 1<<20)
	if err != nil || workpacket.Digest(data) != stringField(found["sha256"]) {
		return nil, fmt.Errorf("original human decision bytes/SHA differ: %v", err)
	}
	var decision struct {
		ID          string `json:"decision_id"`
		Runtime     string `json:"runtime_id"`
		Disposition string `json:"disposition"`
		Scope       string `json:"scope"`
		Cursor      struct {
			State string `json:"state"`
		} `json:"target_cursor"`
	}
	if err := json.Unmarshal(data, &decision); err != nil {
		return nil, err
	}
	if decision.ID != id || decision.Runtime != stringField(state["runtime_id"]) || decision.Disposition != "reject_defect" || decision.Scope != "runtime_release:"+decision.Runtime || decision.Cursor.State != "awaiting_human_release" {
		return nil, fmt.Errorf("original human decision does not describe this Runtime's S11 defect rejection")
	}
	return []string{id, path}, nil
}

func humanBaselineDigest(root string, state map[string]any) (string, error) {
	plan, ptr, err := review.LoadPlan(root, state)
	if err != nil {
		return "", fmt.Errorf("Main must restore the existing frozen ReviewPlan for the human report: %w", err)
	}
	generation, err := baselineGeneration(state)
	if err != nil {
		return "", err
	}
	if plan.BaselineGeneration != generation || plan.ReviewRound != currentReviewRound(state) || ptr.ReviewRound != plan.ReviewRound || len(plan.FrozenSubjects) == 0 {
		return "", fmt.Errorf("human report requires the original current-round/generation frozen product subjects; Main restores that source, not a fabricated Finding or Batch")
	}
	if err := review.VerifyFrozenSubjects(root, plan, state); err != nil {
		return "", err
	}
	return review.SubjectDigest(plan), nil
}
