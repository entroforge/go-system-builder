package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// ValidateWorkPacket checks current ownership and the complete frozen subject
// set. It is repeated under the original Writer lock before consumption.
func ValidateWorkPacket(root string, state map[string]any, packet workpacket.Packet, files fileview.Reader) error {
	if !PacketEnabled(state) || files == nil {
		return fmt.Errorf("packet consumer is not enabled with an authoritative file view")
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return err
	}
	entry, err := integerField(lifecycle, "phase_revision")
	if err != nil {
		return err
	}
	b := packet.Binding
	if state["runtime_id"] != b.RuntimeID || generation != b.Generation || lifecycle["state"] != "document_verification" || entry != b.StageEntry {
		return fmt.Errorf("packet runtime, generation or stage entry is stale")
	}
	expected, err := workpacket.RebuildS5(packet)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(packet, expected) {
		return fmt.Errorf("packet differs from registered template projection")
	}
	if err := workpacket.ValidateTaskAnalysisSources(root, packet, files); err != nil {
		return err
	}
	if err := validatePacketAssignment(root, state, b, packet.Methods); err != nil {
		return err
	}
	formalInputs, err := S5FormalInputs(root, state, files)
	if err != nil {
		return fmt.Errorf("resolve current S5 formal inputs: %w", err)
	}
	refs := formalInputs.References
	for _, ref := range refs {
		data, err := files.ReadFile(ref.Path)
		if err != nil {
			return fmt.Errorf("read frozen packet input: %w", err)
		}
		if workpacket.Digest(data) != ref.SHA256 {
			return fmt.Errorf("frozen packet input drifted")
		}
		physical, err := safeEvidencePath(root, ref.Path)
		if err != nil {
			return fmt.Errorf("packet input containment: %w", err)
		}
		disk, err := os.ReadFile(filepath.Join(root, physical))
		if err != nil {
			return fmt.Errorf("read packet input checkout: %w", err)
		}
		if workpacket.Digest(disk) != ref.SHA256 {
			return fmt.Errorf("packet input checkout drifted")
		}
	}
	if !reflect.DeepEqual(refs, packet.Inputs) {
		return fmt.Errorf("packet does not cover the exact current formal S5 input set")
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["status"] != "locked" {
		return fmt.Errorf("packet requires the current locked REQ baseline")
	}
	boundFound := false
	for _, ref := range refs {
		if ref.ID == bound["id"] && ref.Path == bound["path"] && ref.Version == bound["version"] && ref.SHA256 == bound["sha256"] {
			boundFound = true
			break
		}
	}
	if !boundFound {
		return fmt.Errorf("packet document set differs from the authorized locked REQ baseline")
	}
	if verified, ok := files.(interface{ Verify() error }); ok {
		if err := verified.Verify(); err != nil {
			return fmt.Errorf("packet file source moved: %w", err)
		}
	}
	return nil
}

func validatePacketAssignment(root string, state map[string]any, b workpacket.Binding, methods []workpacket.ReadItem) error {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	var agent map[string]any
	for _, raw := range agents {
		a, _ := raw.(map[string]any)
		if a["id"] == b.Owner {
			agent = a
			break
		}
	}
	activity := ""
	if agent != nil {
		activity = workstate.AgentActivity(agent)
	}
	if agent == nil || (agent["role"] != "document-verifier" && agent["role"] != "document_verifier") || activity == "stopped" || activity == "blocked" || activity == "queued" {
		return fmt.Errorf("packet owner is not a current document reviewer")
	}
	teams, _ := entities["teams"].([]any)
	var team map[string]any
	for _, raw := range teams {
		t, _ := raw.(map[string]any)
		if t["id"] == agent["team_id"] {
			team = t
			break
		}
	}
	if team == nil || team["kind"] != "document_verifier" || (team["status"] != "planned" && team["status"] != "active") || !containsString(team["agent_ids"], b.Owner) || !containsString(team["responsibility_ids"], b.Responsibility) {
		return fmt.Errorf("packet owner has no current registered document responsibility")
	}
	teamEntry, entryErr := integerField(team, "packet_stage_entry")
	teamGeneration, generationErr := integerField(team, "packet_generation")
	if entryErr != nil || generationErr != nil || teamEntry != b.StageEntry || teamGeneration != b.Generation {
		return fmt.Errorf("document reviewer dispatch is not registered for this S5 entry and generation")
	}
	manifestRef, _ := team["manifest_ref"].(string)
	if agent["prompt_ref"] != manifestRef+"#"+b.AssignmentID {
		return fmt.Errorf("packet assignment does not match registered prompt")
	}
	manifestPath, err := safeEvidencePath(root, manifestRef)
	if err != nil {
		return fmt.Errorf("packet manifest path: %w", err)
	}
	manifestData, err := os.ReadFile(filepath.Join(root, manifestPath))
	if err != nil {
		return fmt.Errorf("read packet manifest: %w", err)
	}
	if err := validateS5RegisteredManifestBytes(team, manifestData); err != nil {
		return err
	}
	if workpacket.Digest(manifestData) != b.AssignmentSHA {
		return fmt.Errorf("packet assignment manifest drifted")
	}
	var manifest struct {
		RuntimeID   string `json:"runtime_id"`
		Generation  int    `json:"baseline_generation"`
		WorkgroupID string `json:"workgroup_id"`
		Kind        string `json:"workgroup_kind"`
		Assignments []struct {
			ID             string   `json:"assignment_id"`
			Owner          string   `json:"agent_id"`
			Responsibility string   `json:"responsibility_id"`
			SkillRefs      []string `json:"skill_refs"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("decode packet manifest: %w", err)
	}
	if manifest.RuntimeID != b.RuntimeID || manifest.Generation != b.Generation || manifest.WorkgroupID != team["id"] || manifest.Kind != "document_verifier" {
		return fmt.Errorf("packet manifest belongs to another Runtime, generation or workgroup")
	}
	for _, a := range manifest.Assignments {
		if a.ID == b.AssignmentID && a.Owner == b.Owner && a.Responsibility == b.Responsibility {
			// Old issued questionnaires have no method projection and retain
			// their original bytes. New projections must match the manifest.
			if len(methods) > 0 {
				expected, err := s5MethodReferences(root, a.SkillRefs)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(methods, expected) {
					return fmt.Errorf("S5 methods differ from the registered assignment")
				}
			}
			return nil
		}
	}
	return fmt.Errorf("packet responsibility is not assigned to this owner")
}

// New registrations retain the original manifest digest on their existing
// team row, including the interval before any packet is issued. Old rows
// without that fact keep their original packet pin; never backfill history
// from today's bytes.
func validateS5RegisteredManifestBytes(team map[string]any, data []byte) error {
	if raw, present := team["manifest_sha256"]; present {
		sha, ok := raw.(string)
		if !ok || sha == "" || sha != workpacket.Digest(data) {
			return fmt.Errorf("registered S5 manifest differs from its original registration: %s; restore the registered bytes, do not refresh its digest", stringValue(team["manifest_ref"]))
		}
	}
	return nil
}
