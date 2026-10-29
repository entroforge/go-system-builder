package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// S5Packets derives the unique current lineage tip from registered dispatch facts.
// An empty owner selects every current document reviewer for Main issuance.
// It does not grant activation, write authority, or change Runtime.
func S5Packets(root string, state map[string]any, owner string) ([]workpacket.Packet, error) {
	return S5PacketsWithFiles(root, state, owner, fileview.Disk{Root: root})
}

// S5PacketsWithFiles preserves the full formal-source reconstruction while
// sharing the caller's pinned, request-scoped Git object cache and deadline.
func S5PacketsWithFiles(root string, state map[string]any, owner string, files fileview.Reader) ([]workpacket.Packet, error) {
	life, _ := state["lifecycle"].(map[string]any)
	if !PacketEnabled(state) || life["state"] != "document_verification" {
		return nil, nil
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return nil, err
	}
	entry, err := integerField(life, "phase_revision")
	if err != nil {
		return nil, err
	}
	formalInputs, err := S5FormalInputs(root, state, files)
	if err != nil {
		return nil, err
	}
	refs := formalInputs.References
	entities, _ := state["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	agents, _ := entities["agents"].([]any)
	packets := []workpacket.Packet{}
	for _, raw := range agents {
		a, _ := raw.(map[string]any)
		id := stringValue(a["id"])
		activity := workstate.AgentActivity(a)
		if (a["role"] != "document-verifier" && a["role"] != "document_verifier") || activity == "stopped" || activity == "blocked" || activity == "queued" {
			continue
		}
		var manifest string
		var manifestTeam map[string]any
		for _, raw := range teams {
			t, _ := raw.(map[string]any)
			tEntry, eErr := integerField(t, "packet_stage_entry")
			tGeneration, gErr := integerField(t, "packet_generation")
			if eErr != nil || gErr != nil || tEntry != entry || tGeneration != generation {
				continue
			}
			if t["id"] == a["team_id"] && t["kind"] == "document_verifier" && (t["status"] == "planned" || t["status"] == "active") {
				manifest = stringValue(t["manifest_ref"])
				manifestTeam = t
			}
		}
		if manifest == "" {
			continue
		}
		path, err := safeEvidencePath(root, manifest)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		if err := validateS5RegisteredManifestBytes(manifestTeam, data); err != nil {
			return nil, err
		}
		var m struct {
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
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		if m.RuntimeID != state["runtime_id"] || m.Generation != generation || m.WorkgroupID != a["team_id"] || m.Kind != "document_verifier" {
			return nil, fmt.Errorf("document reviewer manifest belongs to another Runtime, generation or workgroup")
		}
		matches := 0
		for _, assignment := range m.Assignments {
			if assignment.Owner != id || a["prompt_ref"] != manifest+"#"+assignment.ID {
				continue
			}
			matches++
			if matches > 1 {
				return nil, fmt.Errorf("ambiguous document reviewer assignment for %s", id)
			}
			p, err := workpacket.BuildS5(workpacket.Binding{RuntimeID: stringValue(state["runtime_id"]), Generation: generation, StageEntry: entry, Owner: id, AssignmentID: assignment.ID, AssignmentSHA: workpacket.Digest(data), Responsibility: assignment.Responsibility, Attempt: 1}, refs)
			if err != nil {
				return nil, err
			}
			// Never guess a new first attempt over an existing immutable slot.
			issued, _ := entities["work_packets"].([]any)
			tips, found := 0, false
			for _, raw := range issued {
				row, _ := raw.(map[string]any)
				b, _ := row["binding"].(map[string]any)
				// S7 review packets share the work_packets index but carry
				// review-plan bindings without stage_entry; skip rows that are
				// not S5 document-reviewer packets instead of aborting issuance.
				oldEntry, ok := optionalIntegerField(b, "stage_entry")
				if !ok {
					continue
				}
				if b["owner"] == id && b["responsibility"] == assignment.Responsibility && oldEntry == entry && b["runtime_id"] == state["runtime_id"] && numberBindingGeneration(b) == generation {
					found = true
					old, _, err := FindWorkPacket(state, stringValue(row["packet_id"]))
					if err != nil {
						return nil, err
					}
					if old.Binding.AssignmentID != assignment.ID || old.Binding.AssignmentSHA != workpacket.Digest(data) {
						return nil, fmt.Errorf("issued assignment changed; follow the original rework route")
					}
					if prior, accepted, err := FindSubmission(state, old.ID); err != nil {
						return nil, err
					} else if accepted && prior.RevisionPacketID != "" {
						continue
					}
					if err := validateRevisionReception(state, old); err != nil {
						return nil, err
					}
					tips++
					p = old
				}
			}
			if found && tips != 1 {
				return nil, fmt.Errorf("issued responsibility has no unique current revision tip")
			}
			if !found && len(assignment.SkillRefs) > 0 {
				methods, err := s5MethodReferences(root, assignment.SkillRefs)
				if err != nil {
					return nil, err
				}
				p, err = workpacket.BindMethods(p, methods)
				if err != nil {
					return nil, err
				}
			}
			packets = append(packets, p)
		}
		if matches == 0 {
			return nil, fmt.Errorf("registered prompt has no exact assignment for %s", id)
		}
	}
	seen := map[string]string{}
	selected := []workpacket.Packet{}
	for _, p := range packets {
		if prior, ok := seen[p.Binding.Responsibility]; ok {
			return nil, fmt.Errorf("ambiguous current %s reviewers: %s and %s", p.Binding.Responsibility, prior, p.Binding.Owner)
		}
		seen[p.Binding.Responsibility] = p.Binding.Owner
		if owner == "" || p.Binding.Owner == owner {
			selected = append(selected, p)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return strings.Compare(selected[i].ID, selected[j].ID) < 0 })
	return selected, nil
}

// The registered Skill list already owns method selection. Deliver its actual
// installed paths without turning methods into formal review subjects.
func s5MethodReferences(root string, skillRefs []string) ([]workpacket.ReadItem, error) {
	methods := make([]workpacket.ReadItem, 0, len(skillRefs))
	for _, name := range skillRefs {
		_, path, err := catalog.ReadSkillAsset(root, name)
		if err != nil {
			return nil, fmt.Errorf("S5 registered method %s: %w", name, err)
		}
		methods = append(methods, workpacket.ReadItem{Path: path, Purpose: "应用本责任已登记的专业方法；技能不是被审规格或新的审批步骤。"})
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].Path < methods[j].Path })
	return methods, nil
}

func numberBindingGeneration(b map[string]any) int {
	n, err := integerField(b, "generation")
	if err != nil {
		return -1
	}
	return n
}
