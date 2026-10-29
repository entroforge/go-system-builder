package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestReviewResultMethodSourceVerifiesOriginalSelectionWithoutCatalogRefresh(t *testing.T) {
	root := t.TempDir()
	ref := ".claude/workgroups/qa.json"
	manifest := map[string]any{"runtime_id": "loop-REQ-METHOD", "baseline_generation": 1, "review_round": 1, "workgroup_kind": "qa", "assignments": []any{map[string]any{"assignment_id": "assignment-method", "agent_id": "agent-qa", "role_family": "qa", "claim_ids": []string{"claim-method"}, "skill_refs": []string{"database-change"}}}}
	write := func() []byte {
		t.Helper()
		data, _ := json.Marshal(manifest)
		path := filepath.Join(root, ref)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return data
	}
	data := write()
	packet := workpacket.ReviewResultPacket{Binding: workpacket.ReviewResultBinding{RuntimeID: "loop-REQ-METHOD", BaselineGeneration: 1, ReviewRound: 1, AssignmentID: "assignment-method", OwnerAgentID: "agent-qa", ClaimIDs: []string{"claim-method"}}, Inputs: workpacket.ReviewResultInputs{MethodSource: &workpacket.ReviewMethodSource{ManifestRef: ref, SHA256: workpacket.Digest(data)}, Methods: []workpacket.ReadItem{{Path: ".claude/skills/database-change/SKILL.md", Purpose: workpacket.ReviewMethodPurpose("database-change")}}}}
	// No installed catalog files are needed to verify history. The original
	// selected path remains immutable even if today's source layout differs.
	if err := verifyReviewResultMethodSource(root, packet, "qa"); err != nil {
		t.Fatal(err)
	}
	legacy := packet
	legacy.Inputs.MethodSource = nil
	legacy.Inputs.Methods = nil
	if err := verifyReviewResultMethodSource(t.TempDir(), legacy, "qa"); err != nil {
		t.Fatal("old issuance acquired new source requirements", err)
	}
	for _, name := range []string{"wrong method", "wrong purpose", "missing method", "wrong owner", "changed manifest bytes", "changed manifest selection"} {
		t.Run(name, func(t *testing.T) {
			data := write()
			candidate := packet
			candidate.Inputs.MethodSource = &workpacket.ReviewMethodSource{ManifestRef: ref, SHA256: workpacket.Digest(data)}
			candidate.Inputs.Methods = append([]workpacket.ReadItem(nil), packet.Inputs.Methods...)
			switch name {
			case "wrong method":
				candidate.Inputs.Methods[0].Path = ".claude/skills/security-review/SKILL.md"
			case "wrong purpose":
				candidate.Inputs.Methods[0].Purpose = "replace professional judgment"
			case "missing method":
				candidate.Inputs.Methods = nil
			case "wrong owner":
				candidate.Binding.OwnerAgentID = "agent-other"
			case "changed manifest bytes":
				_ = os.WriteFile(filepath.Join(root, ref), append(data, ' '), 0600)
			case "changed manifest selection":
				var other map[string]any
				_ = json.Unmarshal(data, &other)
				other["assignments"].([]any)[0].(map[string]any)["skill_refs"] = []string{"security-review"}
				changed, _ := json.Marshal(other)
				_ = os.WriteFile(filepath.Join(root, ref), changed, 0600)
				candidate.Inputs.MethodSource.SHA256 = workpacket.Digest(changed)
			}
			if err := verifyReviewResultMethodSource(root, candidate, "qa"); err == nil {
				t.Fatal("invalid original method provenance accepted")
			}
		})
	}
}
