package review

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
)

type repairChangeImpactArtifact struct {
	RecordType         string `json:"record_type"`
	RuntimeID          string `json:"runtime_id"`
	BaselineGeneration int    `json:"baseline_generation"`
	ChangedArtifacts   []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"changed_artifacts"`
}

// validateRepairRoundBaseline binds a TR-012 re-entry plan to the exact
// change_impact artifact that authorized the new full review. The artifact
// explains why the post-repair files changed; frozen_subjects pins their
// current bytes. Keeping those responsibilities separate avoids putting a
// commit identifier into a file-level subject fingerprint.
func validateRepairRoundBaseline(root string, state map[string]any, plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("repair-round baseline validation requires a plan")
	}
	review, _ := state["review"].(map[string]any)
	entry, _ := review["round_entry"].(map[string]any)
	if entry == nil || stringField(entry["transition_id"]) != "TR-012" {
		return nil
	}
	if intField(entry["round"]) != currentReviewRound(state) || intField(entry["baseline_generation"]) != baselineGeneration(state) {
		return s7GateError(
			"S7_REPAIR_BASELINE_SCOPE",
			"TR-012 round-entry coordinates do not match the active review round",
			[]string{fmt.Sprintf("round_entry round=%d generation=%d; active round=%d generation=%d", intField(entry["round"]), intField(entry["baseline_generation"]), currentReviewRound(state), baselineGeneration(state))},
			[]string{"re-enter S7 through the current TR-012 transition and regenerate the ReviewPlan from the active Runtime"},
			"loop-harness s7 status",
		)
	}

	missing := []string{}
	repair := []string{}
	changeImpactRef := strings.TrimSpace(stringField(entry["change_impact_ref"]))
	if changeImpactRef == "" {
		missing = append(missing, "round_entry.change_impact_ref")
		repair = append(repair, "re-run TR-012 with its current change_impact_record binding")
	}
	if plan.ChangeImpact == nil {
		missing = append(missing, "ReviewPlan.change_impact")
		repair = append(repair, "copy the current change_impact evidence id into change_impact.source_refs")
	}
	if len(missing) > 0 {
		return s7GateError(
			"S7_REPAIR_BASELINE_MISSING",
			"TR-012 re-entry requires an explicit post-repair baseline binding",
			missing,
			repair,
			"runtime review-plan --file plan.json --expected-revision <N>",
		)
	}

	if !containsString(plan.ChangeImpact.SourceRefs, changeImpactRef) {
		return s7GateError(
			"S7_REPAIR_BASELINE_REF",
			"ReviewPlan.change_impact does not cite the TR-012 change_impact evidence",
			[]string{fmt.Sprintf("required source_ref: %s", changeImpactRef)},
			[]string{"add the exact change_impact evidence id to change_impact.source_refs"},
			"runtime review-plan --file plan.json --expected-revision <N>",
		)
	}

	evidenceID := evidenceReferenceID(state, changeImpactRef)
	if evidenceID == "" {
		return s7GateError(
			"S7_REPAIR_BASELINE_REF",
			"TR-012 change_impact reference is not indexed in Runtime evidence",
			[]string{changeImpactRef},
			[]string{"use the current change_impact evidence id or its indexed artifact path"},
			"runtime s7 status --root <repo>",
		)
	}
	data, err := loadIndexedEvidenceArtifact(root, state, evidenceID, "change_impact")
	if err != nil {
		return s7GateError(
			"S7_REPAIR_BASELINE_ARTIFACT",
			"TR-012 change_impact evidence cannot be read as a current immutable artifact",
			[]string{err.Error()},
			[]string{"restore the indexed artifact or record a new valid change_impact before opening the full review"},
			"runtime review-plan --file plan.json --expected-revision <N>",
		)
	}
	if err := schema.NewValidator(root).ValidateBytes("review-evidence.schema.json", data); err != nil {
		return s7GateError(
			"S7_REPAIR_BASELINE_SCHEMA",
			"TR-012 change_impact evidence is not a canonical review-evidence artifact",
			[]string{err.Error()},
			[]string{"rewrite the change_impact artifact using review-evidence.schema.json and re-register its fingerprint"},
			"runtime review-plan --file plan.json --expected-revision <N>",
		)
	}
	var impact repairChangeImpactArtifact
	if err := json.Unmarshal(data, &impact); err != nil {
		return fmt.Errorf("decode TR-012 change_impact evidence: %w", err)
	}
	runtimeID := stringField(state["runtime_id"])
	generation := baselineGeneration(state)
	if impact.RecordType != "change_impact" || impact.RuntimeID != runtimeID || impact.BaselineGeneration != generation {
		return s7GateError(
			"S7_REPAIR_BASELINE_SCOPE",
			"TR-012 change_impact evidence is outside the active Runtime baseline",
			[]string{fmt.Sprintf("artifact runtime_id=%s baseline_generation=%d; active runtime_id=%s baseline_generation=%d", impact.RuntimeID, impact.BaselineGeneration, runtimeID, generation)},
			[]string{"record a change_impact artifact for the active runtime and baseline generation"},
			"runtime review-plan --file plan.json --expected-revision <N>",
		)
	}

	frozen := make(map[string]string, len(plan.FrozenSubjects))
	for _, subject := range plan.FrozenSubjects {
		frozen[normalizeSurface(subject.Path)] = subject.SHA256
	}
	var missingArtifacts []string
	var driftedArtifacts []string
	coverage := make(map[string]bool, len(plan.CoverageInventory))
	for _, item := range plan.CoverageInventory {
		coverage[normalizeSurface(item.SourceRef)] = true
	}
	claimed := make(map[string]bool)
	for _, claim := range plan.Claims {
		for _, sourceRef := range claim.SourceRefs {
			claimed[normalizeSurface(sourceRef)] = true
		}
	}
	for _, artifact := range impact.ChangedArtifacts {
		path := normalizeSurface(artifact.Path)
		if path == "" {
			missingArtifacts = append(missingArtifacts, "change_impact contains an empty changed_artifacts.path")
			continue
		}
		frozenSHA, ok := frozen[path]
		if !ok {
			missingArtifacts = append(missingArtifacts, path+" is absent from frozen_subjects")
			continue
		}
		if frozenSHA != artifact.SHA256 {
			driftedArtifacts = append(driftedArtifacts, fmt.Sprintf("%s: change_impact=%s frozen_subjects=%s", path, artifact.SHA256, frozenSHA))
		}
		if !coverage[path] {
			missingArtifacts = append(missingArtifacts, path+" is absent from coverage_inventory")
		}
		if !claimed[path] {
			missingArtifacts = append(missingArtifacts, path+" has no Claim source_ref")
		}
	}
	if len(missingArtifacts) > 0 || len(driftedArtifacts) > 0 {
		missing = append(missing, missingArtifacts...)
		missing = append(missing, driftedArtifacts...)
		return s7GateError(
			"S7_REPAIR_BASELINE_COVERAGE",
			"TR-012 ReviewPlan does not freeze every post-repair changed artifact",
			missing,
			[]string{"add every change_impact.changed_artifacts path to frozen_subjects with the exact post-repair SHA-256; keep it in coverage_inventory and a Claim source_refs"},
			"runtime review-plan --file plan.json --expected-revision <N>",
		)
	}
	return nil
}

func evidenceReferenceID(state map[string]any, reference string) string {
	for _, raw := range evidenceEntries(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		if stringField(entry["id"]) == reference || stringField(entry["path"]) == reference {
			return stringField(entry["id"])
		}
	}
	return ""
}
