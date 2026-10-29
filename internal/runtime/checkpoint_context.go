package runtime

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/projectconventions"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Frozen native knowledge remains indexed in the original pair. This verifies
// the immutable profile/role/Schema sources, not a professional conclusion.
// Document changes from an original named revision may make delivery stale;
// restoring that fact must not silently rebuild a new profile.
func validateCheckpointContextBindings(root string, state map[string]any, journalBytes []byte) error {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	hasBinding := false
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if _, exists := agent["context_binding"]; exists {
			hasBinding = true
		}
	}
	if !hasBinding {
		return nil
	}
	journal, err := inspectJournalData(journalBytes)
	if err != nil {
		return err
	}
	baseline, _ := state["baseline"].(map[string]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		rawBinding, exists := agent["context_binding"]
		if !exists {
			continue
		}
		binding, ok := rawBinding.(map[string]any)
		if !ok {
			return fmt.Errorf("checkpoint fixed context binding is invalid")
		}
		ref, _ := binding["context_ref"].(map[string]any)
		generation, err := integerField(binding, "generation")
		if err != nil {
			return err
		}
		key := fmt.Sprintf("context:%v:%d:%v", agent["id"], generation, ref["sha256"])
		found := false
		for _, event := range journal.Events {
			actor, _ := event["actor"].(map[string]any)
			if event["event"] == "transition_committed" && event["transition_id"] == "CONTEXT-PREPARE" && event["idempotency_key"] == key && event["runtime_id"] == state["runtime_id"] && actor["id"] == "main" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("checkpoint fixed context has no original preparation journal")
		}

		path, _ := ref["path"].(string)
		data, err := workpacket.ReadFile(root, path, 2<<20)
		if err != nil || workpacket.Digest(data) != ref["sha256"] {
			return fmt.Errorf("checkpoint fixed context source is unverifiable for Agent %v", agent["id"])
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", data); err != nil {
			return err
		}
		var c map[string]any
		if err := json.Unmarshal(data, &c); err != nil {
			return err
		}
		d, _ := c["dispatch"].(map[string]any)
		profile, _ := c["context_selection"].(map[string]any)
		prompt, _ := agent["prompt_ref"].(string)
		manifest, assignment, _ := strings.Cut(prompt, "#")
		contextGeneration, genErr := integerField(c, "baseline_generation")
		originalGeneration, originalGenErr := integerField(baseline, "generation")
		if genErr != nil || originalGenErr != nil || c["runtime_id"] != state["runtime_id"] || contextGeneration != originalGeneration || d["owner"] != agent["id"] || d["assignment_id"] != assignment || d["manifest_ref"] != manifest || d["team_id"] != agent["team_id"] || d["agent_definition_ref"] != agent["definition_ref"] || d["role"] != agent["role"] || profile["profile_version"] != binding["profile_version"] || profile["profile_sha256"] != binding["profile_sha256"] || profile["mode"] != binding["mode"] {
			return fmt.Errorf("checkpoint fixed context differs from original Agent/profile binding")
		}
		inputs, _ := c["frozen_inputs"].(map[string]any)
		var conventions struct {
			Bundle *projectconventions.Bundle `json:"conventions"`
			Inputs map[string]string          `json:"frozen_inputs"`
		}
		if err := json.Unmarshal(data, &conventions); err != nil {
			return err
		}
		if err := conventions.Bundle.ValidateInputs(conventions.Inputs); err != nil {
			return fmt.Errorf("checkpoint frozen conventions binding: %w", err)
		}
		role, _ := d["agent_definition_ref"].(string)
		roleBytes, err := workpacket.ReadFile(root, role, 1<<20)
		if err != nil || workpacket.Digest(roleBytes) != inputs[role] {
			return fmt.Errorf("checkpoint fixed role source is unverifiable")
		}
		planSchema, _ := c["plan_schema"].(map[string]any)
		schemaPath, _ := planSchema["path"].(string)
		schemaBytes, err := workpacket.ReadFile(root, schemaPath, 2<<20)
		if err != nil || workpacket.Digest(schemaBytes) != planSchema["sha256"] {
			return fmt.Errorf("checkpoint fixed plan Schema is unverifiable")
		}
		skills, _ := profile["selected"].([]any)
		for _, raw := range skills {
			skill, _ := raw.(map[string]any)
			source, _ := skill["source"].(string)
			data, err := workpacket.ReadFile(root, source, 1<<20)
			if err != nil || workpacket.Digest(data) != skill["sha256"] {
				return fmt.Errorf("checkpoint fixed Skill source is unverifiable: %s", source)
			}
		}
	}
	return nil
}
