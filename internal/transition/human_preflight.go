package transition

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// HumanDecisionPreflight is an authoring result, never release permission.
type HumanDecisionPreflight struct {
	ArtifactValid   bool     `json:"artifact_valid"`
	TransitionReady bool     `json:"transition_ready"`
	TransitionID    string   `json:"transition_id,omitempty"`
	Problems        []string `json:"problems"`
	Warnings        []string `json:"warnings,omitempty"`
	Skipped         []string `json:"skipped"`
}

// HumanDecisionDraft copies only observed coordinates. Identity, disposition
// and the human's actual words are intentionally unfilled. The execution
// actor belongs to the command, not the approver field in this record.
func HumanDecisionDraft(state map[string]any) map[string]any {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	review, _ := state["review"].(map[string]any)
	return map[string]any{
		"decision_id": "<DECISION-ID>", "runtime_id": state["runtime_id"],
		"disposition": "<DISPOSITION>", "approved_by": "<HUMAN-IDENTITY>",
		"decision":            "<EXACT-HUMAN-DECISION>",
		"baseline_generation": baseline["generation"], "review_round": review["round"],
		"target_cursor": map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"], "phase_revision": lifecycle["phase_revision"]},
		"scope_refs":    []string{"<SEMANTIC-SCOPE>"},
	}
}

// PreflightHumanReleaseDecision shares the consumption-time field validator.
// It reads the catalog only; no Store, lock, journal append or consumption.
func PreflightHumanReleaseDecision(root string, state map[string]any, data []byte, actor string) HumanDecisionPreflight {
	report := HumanDecisionPreflight{Problems: []string{}, Skipped: []string{"transition guards, other evidence slots and one-time consumption are checked at submission"}}
	var artifact map[string]any
	if err := json.Unmarshal(data, &artifact); err != nil || artifact == nil {
		report.Problems = append(report.Problems, "decision must be a JSON object")
		report.Skipped = append(report.Skipped, "identity/scope checks depend on a parsed record")
		return report
	}
	add := func(err error) {
		if err != nil {
			report.Problems = append(report.Problems, err.Error())
		}
	}
	id, err := loopruntime.HumanReleaseTransitionID(stringValue(artifact["disposition"]))
	if err != nil {
		add(err)
		add(validateDecisionDraftFields(state, artifact))
		report.Skipped = append(report.Skipped, "scope and event checks depend on a supported disposition")
		return report
	}
	report.TransitionID = id
	catalog, err := LoadCatalog(root)
	if err != nil {
		add(err)
		return report
	}
	spec, ok := catalog.Transitions[id]
	if !ok {
		add(fmt.Errorf("Loop Definition has no transition %s", id))
		return report
	}
	if actor == "" || !contains(spec.Actors, actor) {
		add(fmt.Errorf("actor %q cannot execute %s; actor is an execution role, not approval identity", actor, id))
	}
	item := map[string]any{"id": artifact["decision_id"]}
	add(validateHumanDecisionFields(state, spec, item, artifact))
	wantScope := spec.HumanDecisionScope + ":" + stringValue(state["runtime_id"])
	if !containsString(toStringSlice(artifact["scope_refs"]), wantScope) {
		add(fmt.Errorf("scope_refs must include %q", wantScope))
	}
	entries, _ := state["evidence"].([]any)
	for _, raw := range entries {
		row, _ := raw.(map[string]any)
		if row == nil || row["id"] != artifact["decision_id"] {
			continue
		}
		if row["kind"] != "human_decision" || row["status"] != "valid" || loopruntime.HumanDecisionEvidenceConsumed(row) {
			add(fmt.Errorf("decision_id %q is already consumed, invalid or used by another evidence kind", artifact["decision_id"]))
		} else if row["sha256"] != sha256Sum(data) {
			add(fmt.Errorf("decision_id %q is already registered with different bytes", artifact["decision_id"]))
		}
	}
	for _, field := range []string{"approved_by", "decision", "baseline_generation", "review_round"} {
		if _, present := artifact[field]; !present {
			report.Warnings = append(report.Warnings, "legacy decision lacks "+field+"; no missing historical fact has been inferred")
		}
	}
	report.ArtifactValid = len(report.Problems) == 0
	return report
}

func validateDecisionDraftFields(state, artifact map[string]any) error {
	var problems []error
	if value, present := artifact["expires_at"]; present {
		expires, err := time.Parse(time.RFC3339, stringValue(value))
		if err != nil || !time.Now().UTC().Before(expires) {
			problems = append(problems, fmt.Errorf("human_decision expires_at is invalid or expired"))
		}
	}
	for _, field := range []string{"decision_id", "runtime_id", "disposition", "approved_by", "decision"} {
		value, exists := artifact[field]
		if !exists {
			continue
		} // Historical records remain readable.
		text, ok := value.(string)
		text = strings.TrimSpace(text)
		if !ok || text == "" || (strings.HasPrefix(text, "<") && strings.HasSuffix(text, ">")) {
			problems = append(problems, fmt.Errorf("human_decision %s is empty or an unfilled placeholder", field))
		}
	}
	baseline, _ := state["baseline"].(map[string]any)
	review, _ := state["review"].(map[string]any)
	for _, target := range []struct {
		name    string
		current any
	}{{"baseline_generation", baseline["generation"]}, {"review_round", review["round"]}} {
		if value, present := artifact[target.name]; present && !sameDecisionCoordinate(value, target.current) {
			problems = append(problems, fmt.Errorf("human_decision %s %v does not match current %v", target.name, value, target.current))
		}
	}
	if cursor, ok := artifact["target_cursor"].(map[string]any); ok {
		lifecycle, _ := state["lifecycle"].(map[string]any)
		if value, present := cursor["phase_revision"]; present && !sameDecisionCoordinate(value, lifecycle["phase_revision"]) {
			problems = append(problems, fmt.Errorf("human_decision target_cursor.phase_revision is stale"))
		}
	}
	return errors.Join(problems...)
}

func sameDecisionCoordinate(a, b any) bool {
	// Marshal rather than truncate floating-point values to an integer.
	x, xErr := json.Marshal(a)
	y, yErr := json.Marshal(b)
	return xErr == nil && yErr == nil && string(x) == string(y)
}
