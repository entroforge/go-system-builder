package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func registerAcceptedS5Reviewers(t *testing.T, root, taskID, taskPath string) []workpacket.Packet {
	t.Helper()
	snap := stageSnapshot(t, root)
	documents, err := runtime.CurrentFormalInputDocuments(root, snap.State, fileview.Disk{Root: root})
	if err != nil {
		t.Fatalf("read accepted S5 formal inputs: %v", err)
	}
	refs := make([]workpacket.Reference, 0, len(documents))
	for _, document := range documents {
		refs = append(refs, document.Reference)
	}

	assets := []string{"agents/document-verifier.md", "skills/agent-dispatch/SKILL.md"}
	for _, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		for _, skill := range agentcontext.RequiredSkills(responsibility) {
			assets = append(assets, filepath.ToSlash(filepath.Join("skills", skill, "SKILL.md")))
		}
	}
	installed := map[string]bool{}
	for _, rel := range assets {
		if installed[rel] {
			continue
		}
		installed[rel] = true
		data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(root, ".claude", rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	asset, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(asset, &manifest); err != nil {
		t.Fatal(err)
	}
	bound, _ := snap.State["bound_req"].(map[string]any)
	manifest["manifest_id"] = "team-manifest-accepted-s4-s5"
	manifest["runtime_id"] = snap.State["runtime_id"]
	manifest["req_id"] = bound["id"]
	manifest["baseline_generation"] = snap.State["baseline"].(map[string]any)["generation"]
	manifest["review_round"] = nil
	manifest["workgroup_id"] = "workgroup-accepted-s4-s5"
	manifest["workgroup_kind"] = "document_verifier"
	manifest["planned_agent_count"] = 2
	manifest["max_parallel_agents"] = 2
	manifest["risk_tags"] = []any{}
	manifest["quantity_rationale"] = "Two independent mandatory document review responsibilities."
	docRefs := make([]any, 0, len(refs))
	for _, ref := range refs {
		docRefs = append(docRefs, map[string]any{"id": ref.ID, "path": ref.Path, "version": ref.Version, "sha256": ref.SHA256})
	}
	manifest["documents"] = docRefs
	base := manifest["assignments"].([]any)[0].(map[string]any)
	assignments, dispositions := []any{}, []any{}
	for i, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		owner, assignmentID := []string{"reviewer-spec", "reviewer-task"}[i], []string{"assignment-spec", "assignment-task"}[i]
		assignment := map[string]any{}
		for key, value := range base {
			assignment[key] = value
		}
		assignment["assignment_id"], assignment["agent_id"], assignment["responsibility_id"] = assignmentID, owner, responsibility
		assignment["role_family"], assignment["agent_definition_ref"] = "document-verifier", ".claude/agents/document-verifier.md"
		assignment["skill_refs"] = agentcontext.RequiredSkills(responsibility)
		assignment["read_paths"] = []string{taskPath}
		assignment["write_paths"] = []string{}
		assignment["output_paths"] = []string{"docs/reports/review/" + owner + ".json"}
		assignment["required_checks"] = []string{}
		assignment["done_when"] = []string{"the original domain gate consumes this independent conclusion"}
		assignment["target_branch"] = "test-development"
		assignment["dispatch_mode"] = "plan_checkpoint"
		assignments = append(assignments, assignment)
		dispositions = append(dispositions, map[string]any{"responsibility_id": responsibility, "disposition": "assigned", "trigger": "mandatory S5", "assignment_ids": []string{assignmentID}, "na_rationale": nil, "evidence_ref": nil})
	}
	manifest["assignments"], manifest["responsibility_dispositions"] = assignments, dispositions
	manifest["separation_edges"] = []any{map[string]any{"left_assignment_id": "assignment-spec", "right_assignment_id": "assignment-task", "reason": "different_conclusion"}}
	manifestPath := ".claude/workgroups/REQ-039/review/accepted-s4-s5.json"
	stageJSON(t, root, manifestPath, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestPath, "--task-id", taskID, "--task", taskPath)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := service.Issue("")
	if err != nil || len(packets) != 2 {
		t.Fatalf("issue accepted S5 review packets: packets=%d err=%v", len(packets), err)
	}
	return packets
}

func TestAcceptedS4FormalTaskFeedsS5WorkspacePrepare(t *testing.T) {
	root, planningPacket := newTaskPlanningAnswerFixture(t)
	for _, rel := range []string{"docs/control/agent-protocol.md", "docs/rules"} {
		copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, rel))
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, planningPacket.AnswerPath, completedTaskPlanningDraft(planningPacket))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveTaskPlanning(context.Background(), planningPacket.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("accept original S4 task planning: %+v %v", receipt, err)
	}
	settleMainPlanningFollowups(t, root)
	snap := stageSnapshot(t, root)
	taskRef := workpacket.Reference{}
	for _, document := range mustFormalInputDocuments(t, root, snap.State) {
		if document.Kind == "task" && document.Reference.ID == "TASK-039-01" {
			taskRef = document.Reference
		}
	}
	if taskRef.Path == "" {
		t.Fatal("accepted S4 complete TASK is absent from the shared S5 input selector")
	}

	packets := registerAcceptedS5Reviewers(t, root, taskRef.ID, taskRef.Path)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	stageCLI(t, "runtime", "workspace", "prepare", "--root", root, "--assignment", packets[0].Binding.AssignmentID, "--agent", packets[0].Binding.Owner)
	registry, err := workspace.Decode(stageSnapshot(t, root).State)
	if err != nil {
		t.Fatal(err)
	}
	execution := registry.Executions[packets[0].Binding.AssignmentID]
	if execution.Inputs[taskRef.Path] != taskRef.SHA256 {
		t.Fatalf("accepted S4 TASK was not frozen into S5 workspace inputs: %+v", execution.Inputs)
	}
	if execution.Inputs["docs/dev/tasks/index-REQ-039.md"] == "" {
		t.Fatal("accepted S4 dispatch plan was not frozen into S5 workspace inputs")
	}

	stale := snap.State
	stale["baseline"] = map[string]any{"generation": 2}
	if _, err := runtime.CurrentFormalInputDocuments(root, stale, fileview.Disk{Root: root}); err == nil {
		t.Fatal("previous-generation S4 closure remained eligible in S5")
	}
	// New S5 responsibilities freeze current method bytes. Previously issued
	// packets and their workspaces retain the bytes actually supplied to them.
	const methodPath = "docs/rules/dispatch-plan.md"
	const updatedMethod = "updated current S5 dispatch planning method\n"
	frozenPacket := packets[0]
	frozenBytes, err := json.Marshal(frozenPacket)
	if err != nil {
		t.Fatal(err)
	}
	frozenInputs := map[string]string{}
	for path, digest := range execution.Inputs {
		frozenInputs[path] = digest
	}
	writeContractSource(t, root, methodPath, updatedMethod)
	currentMethod := false
	for _, document := range mustFormalInputDocuments(t, root, stageSnapshot(t, root).State) {
		if document.Reference.Path == methodPath {
			currentMethod = true
			if document.Reference.SHA256 != workpacket.Digest([]byte(updatedMethod)) {
				t.Fatalf("new S5 input lost current method: %+v", document.Reference)
			}
		}
	}
	if !currentMethod {
		t.Fatal("new S5 closure dropped planning method")
	}
	currentPacket, found, err := runtime.FindWorkPacket(stageSnapshot(t, root).State, frozenPacket.ID)
	if err != nil || !found {
		t.Fatalf("read frozen S5 packet: %t %v", found, err)
	}
	currentBytes, err := json.Marshal(currentPacket)
	if err != nil || string(currentBytes) != string(frozenBytes) {
		t.Fatalf("method change rewrote issued S5 packet: %v", err)
	}
	currentRegistry, err := workspace.Decode(stageSnapshot(t, root).State)
	if err != nil || !reflect.DeepEqual(currentRegistry.Executions[packets[0].Binding.AssignmentID].Inputs, frozenInputs) {
		t.Fatalf("method change rewrote frozen workspace inputs: %v", err)
	}
	for _, path := range []string{taskRef.Path, "docs/dev/tasks/index-REQ-039.md"} {
		original, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, path, "drifted accepted S4 product\n")
		if _, err := runtime.CurrentFormalInputDocuments(root, stageSnapshot(t, root).State, fileview.Disk{Root: root}); err == nil {
			t.Fatalf("drifted accepted S4 output remained eligible in S5: %s", path)
		}
		writeContractSource(t, root, path, string(original))
	}
}

func mustFormalInputDocuments(t *testing.T, root string, state map[string]any) []runtime.FormalInputDocument {
	t.Helper()
	documents, err := runtime.CurrentFormalInputDocuments(root, state, fileview.Disk{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	return documents
}
