package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This records the original source behind the synthetic dispatched rows used
// by domain tests. Real registration (and its manifest SHA) is covered in
// assignment/s7_method_source_test.go and the external integration chain.
func seedResultMethodSources(t *testing.T, root string, state map[string]any, plan *Plan) {
	t.Helper()
	entities := state["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	for _, a := range plan.Assignments {
		row := resultPacketAssignmentRow(state, a.AssignmentID)
		owner := stringField(row["agent_id"])
		if owner == "" {
			continue
		}
		for _, raw := range entities["agents"].([]any) {
			agent := raw.(map[string]any)
			if agent["id"] != owner {
				continue
			}
			teamID := "team-methods-" + a.AssignmentID
			ref := ".claude/workgroups/" + a.AssignmentID + ".json"
			manifest := ManifestDraft{RuntimeID: stringField(state["runtime_id"]), BaselineGeneration: plan.BaselineGeneration, ReviewRound: plan.ReviewRound, WorkgroupID: teamID, WorkgroupKind: LensToWorkgroupKind(a.Lens), Assignments: []ManifestAssignment{{AssignmentID: a.AssignmentID, AgentID: owner, RoleFamily: stringField(agent["role"]), ClaimIDs: a.ClaimIDs, SkillRefs: []string{}}}}
			data, _ := json.Marshal(manifest)
			path := filepath.Join(root, ref)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			agent["team_id"], agent["prompt_ref"] = teamID, ref+"#"+a.AssignmentID
			teams = append(teams, map[string]any{"id": teamID, "platform_team_id": teamID, "kind": manifest.WorkgroupKind, "status": "planned", "manifest_ref": ref, "manifest_sha256": workpacket.Digest(data), "responsibility_ids": []any{}, "agent_ids": []string{owner}, "review_round": plan.ReviewRound})
		}
	}
	entities["teams"] = teams
}

func writeMethodState(t *testing.T, statePath string, state map[string]any) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func selectResultFixtureMethods(t *testing.T, root, statePath string, state map[string]any, skills []string) string {
	t.Helper()
	ref := ".claude/workgroups/assignment-qa-1.json"
	var manifest ManifestDraft
	if err := json.Unmarshal(mustReadTestFile(t, filepath.Join(root, ref)), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Assignments[0].SkillRefs = skills
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, ref), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, raw := range state["entities"].(map[string]any)["teams"].([]any) {
		row := raw.(map[string]any)
		if row["manifest_ref"] == ref {
			row["manifest_sha256"] = workpacket.Digest(data)
		}
	}
	paths := make([]string, 0, len(skills))
	for _, name := range skills {
		path := filepath.Join(root, ".claude/skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# "+name+"\nSelected fixture method.\n"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.ToSlash(filepath.Join(".claude/skills", name, "SKILL.md")))
	}
	// The selected skills are existing method sources, not new product drift.
	// Commit only these paths before issuance so later changes remain visible.
	if len(paths) > 0 {
		reviewFixtureGit(t, root, append([]string{"add", "--"}, paths...)...)
		reviewFixtureGit(t, root, "commit", "-qm", "Add selected S7 fixture methods")
	}
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" {
			agent["state"], agent["dispatch_mode"] = "reading", "plan_checkpoint"
		}
	}
	writeMethodState(t, statePath, state)
	return ref
}

func TestIssuedS7SelectedMethodsReachViewAndRestoreWithoutReissue(t *testing.T) {
	var profiles struct {
		Roles map[string]struct {
			Core []string `json:"core"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(agentcontext.ProfileBytes(), &profiles); err != nil {
		t.Fatal(err)
	}
	for _, name := range profiles.Roles["qa"].Core {
		if name == "database-change" {
			t.Fatal("fixture must exercise an explicitly added method outside role defaults")
		}
	}
	root, statePath, journalPath, snapshot, _ := packetModeFixture(t)
	ref := selectResultFixtureMethods(t, root, statePath, snapshot.State, []string{"code-quality", "database-change"})
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue selected methods: %v %d", err, len(packets))
	}
	p := packets[0]
	if p.Inputs.MethodSource == nil || p.Inputs.MethodSource.ManifestRef != ref || p.Inputs.MethodSource.SHA256 != workpacket.Digest(mustReadTestFile(t, filepath.Join(root, ref))) {
		t.Fatal("selection lacks original registered manifest identity")
	}
	want := []string{".claude/skills/code-quality/SKILL.md", ".claude/skills/database-change/SKILL.md"}
	if len(p.Inputs.Methods) != len(want) {
		t.Fatalf("default skills replaced selected set: %+v", p.Inputs.Methods)
	}
	var view workpacket.ReviewResultPacket
	originalView := mustReadTestFile(t, filepath.Join(root, p.ViewPath))
	if err := json.Unmarshal(originalView, &view); err != nil {
		t.Fatal(err)
	}
	for i, path := range want {
		method := p.Inputs.Methods[i]
		if method.Path != path || method.Purpose == "" {
			t.Fatalf("method delivery: %+v", method)
		}
		found := false
		for _, item := range view.Draft.Read {
			if item == method {
				found = true
			}
		}
		if !found {
			t.Fatalf("selected method missing from actual view.read: %s", path)
		}
	}
	author := append([]byte("\n"), mustReadTestFile(t, filepath.Join(root, p.AnswerPath))...)
	if err := os.WriteFile(filepath.Join(root, p.AnswerPath), author, 0600); err != nil {
		t.Fatal(err)
	}
	beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	if err := os.Remove(filepath.Join(root, p.ViewPath)); err != nil {
		t.Fatal(err)
	}
	restored, err := IssueResultPackets(root, statePath, journalPath, -1, p.Binding.OwnerAgentID)
	if err != nil || len(restored) != 1 || !reflect.DeepEqual(restored[0], p) {
		t.Fatalf("recovery changed original selection: %v", err)
	}
	if !bytes.Equal(originalView, mustReadTestFile(t, filepath.Join(root, p.ViewPath))) || !bytes.Equal(author, mustReadTestFile(t, filepath.Join(root, p.AnswerPath))) || !bytes.Equal(beforeState, mustReadTestFile(t, statePath)) || !bytes.Equal(beforeJournal, mustReadTestFile(t, journalPath)) {
		t.Fatal("method recovery rewrote original identity/answer/event")
	}
	if err := loopruntime.ValidateCheckpointIndices(root, snapshotState(t, statePath, journalPath), beforeJournal); err != nil {
		t.Fatal(err)
	}
	// A real professional answer still goes through the original consumer;
	// selected methods do not introduce author metadata or an approval step.
	authored := saveClaimStopAnswer(t, root, p, claimStopAnswer(t, "P0"))
	request := IssuedSubmitRequest{ExpectedRevision: -1, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: p.Binding.OwnerAgentID}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("selected-method answer did not reach original consumer: %v", err)
	}
	if err := loopruntime.ValidateCheckpointIndices(root, accepted.State, mustReadTestFile(t, journalPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, want[1])); err != nil {
		t.Fatal(err)
	}
	beforeState, beforeJournal = mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
		t.Fatalf("accepted replay reselected today's methods: %v", err)
	}
	if !bytes.Equal(beforeState, mustReadTestFile(t, statePath)) || !bytes.Equal(beforeJournal, mustReadTestFile(t, journalPath)) || !bytes.Equal(authored, mustReadTestFile(t, filepath.Join(root, p.AnswerPath))) {
		t.Fatal("method-bearing answer replay rewrote original state or author bytes")
	}
}

func snapshotState(t *testing.T, statePath, journalPath string) map[string]any {
	t.Helper()
	snap, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snap.State
}

func TestIssuedS7MethodsRejectMissingOrChangedRegisteredSources(t *testing.T) {
	for _, test := range []struct {
		name       string
		afterIssue bool
		change     func(t *testing.T, root, ref string, state map[string]any)
	}{
		{"missing manifest", false, func(t *testing.T, root, ref string, state map[string]any) { _ = os.Remove(filepath.Join(root, ref)) }},
		{"missing selected Skill", false, func(t *testing.T, root, ref string, state map[string]any) {
			_ = os.Remove(filepath.Join(root, ".claude/skills/database-change/SKILL.md"))
		}},
		{"registered manifest drift", false, func(t *testing.T, root, ref string, state map[string]any) {
			data := mustReadTestFile(t, filepath.Join(root, ref))
			_ = os.WriteFile(filepath.Join(root, ref), append(data, ' '), 0600)
		}},
		{"recovery does not refresh selection", true, func(t *testing.T, root, ref string, state map[string]any) {
			var m ManifestDraft
			_ = json.Unmarshal(mustReadTestFile(t, filepath.Join(root, ref)), &m)
			m.Assignments[0].SkillRefs = []string{"code-quality"}
			data, _ := json.Marshal(m)
			_ = os.WriteFile(filepath.Join(root, ref), data, 0600)
			for _, raw := range state["entities"].(map[string]any)["teams"].([]any) {
				row := raw.(map[string]any)
				if row["manifest_ref"] == ref {
					row["manifest_sha256"] = workpacket.Digest(data)
				}
			}
		}},
		{"lost workgroup source", false, func(t *testing.T, root, ref string, state map[string]any) {
			state["entities"].(map[string]any)["teams"] = []any{}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, statePath, journalPath, snapshot, _ := packetModeFixture(t)
			ref := selectResultFixtureMethods(t, root, statePath, snapshot.State, []string{"code-quality", "database-change"})
			if test.afterIssue {
				if _, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1"); err != nil {
					t.Fatal(err)
				}
			}
			state := snapshotState(t, statePath, journalPath)
			test.change(t, root, ref, state)
			writeMethodState(t, statePath, state)
			beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
			if _, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1"); err == nil || !(strings.Contains(err.Error(), "Main preparation") || strings.Contains(err.Error(), "issuance no longer matches") || strings.Contains(err.Error(), "original selected-method manifest")) {
				t.Fatalf("invalid source accepted: %v", err)
			}
			if !bytes.Equal(beforeState, mustReadTestFile(t, statePath)) || !bytes.Equal(beforeJournal, mustReadTestFile(t, journalPath)) {
				t.Fatal("failed preparation committed a packet")
			}
		})
	}
}

func TestS7LegacyRegistrationFirstIssueFreezesNamedMethodSource(t *testing.T) {
	root, statePath, journalPath, snapshot, _ := packetModeFixture(t)
	ref := selectResultFixtureMethods(t, root, statePath, snapshot.State, []string{"database-change"})
	for _, raw := range snapshot.State["entities"].(map[string]any)["teams"].([]any) {
		delete(raw.(map[string]any), "manifest_sha256")
	}
	writeMethodState(t, statePath, snapshot.State)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 || packets[0].Inputs.MethodSource == nil || packets[0].Inputs.MethodSource.ManifestRef != ref || len(packets[0].Inputs.Methods) != 1 {
		t.Fatalf("legacy registration lost its named method source: %+v %v", packets, err)
	}
}
