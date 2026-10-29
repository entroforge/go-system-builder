package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS3DeliveryGuidanceNamesOutputAndTemplatePathsWithoutChoosingApplicability(t *testing.T) {
	root := t.TempDir()
	state := planningGuidanceState("S3", true)
	packet := makePlanningGuidancePacket(t, state, "S3", 7).(workpacket.ContractPacket)
	guidance := contractDeliveryGuidance(root, packet, nil)
	for _, path := range []string{"CONTRACTS-001.md", "FE-001.md", "BE-001.md", "SYNC-001.md", "CONTRACTS-template.md", "FE-contract-template.md", "BE-contract-template.md", "SYNC-contract-template.md"} {
		if !strings.Contains(guidance, filepath.Join(root, "docs/dev/contracts", path)) {
			t.Fatalf("S3 output/template path missing for %s: %s", path, guidance)
		}
	}
	if !strings.Contains(guidance, "candidates") || !strings.Contains(guidance, "choose contract IDs and FE/BE/SYNC applicability") {
		t.Fatal("path guidance replaced professional scope judgment", guidance)
	}
	pass, index := "pass", "docs/dev/contracts/CONTRACTS-SERVICE-001.md"
	draft := packet.Draft
	draft.Answer = workpacket.ContractAnswer{Conclusion: &pass, IndexPath: &index, ContractPaths: []string{"docs/dev/contracts/BE-SERVICE-001.md"}, Applicability: map[string]workpacket.ContractApplicability{
		"FE": {Disposition: "not_applicable", Rationale: "No client in this scope."}, "BE": {Disposition: "required", Rationale: "Service boundary."}, "SYNC": {Disposition: "not_applicable", Rationale: "No separate consumer/provider integration."},
	}}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	answer := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if err := os.MkdirAll(filepath.Dir(answer), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(answer, data, 0600); err != nil {
		t.Fatal(err)
	}
	guidance = contractDeliveryGuidance(root, packet, nil)
	if !strings.Contains(guidance, filepath.Join(root, filepath.FromSlash(index))) || !strings.Contains(guidance, filepath.Join(root, "docs/dev/contracts/BE-SERVICE-001.md")) || strings.Contains(guidance, filepath.Join(root, "docs/dev/contracts/FE-001.md")) {
		t.Fatal("S3 did not use the author's actual named delivery", guidance)
	}
	for _, command := range []string{"stage-answer issue", "stage-answer consume", "runtime evidence record"} {
		if strings.Contains(guidance, command) {
			t.Fatal("S3 added a manual submission step", guidance)
		}
	}
}

const planningGuidanceDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestS5PreparationGuidanceNamesCurrentEntryManifestAndOriginalSkill(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{
		"schema_version": "1.3.0",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"lifecycle":      map[string]any{"state": "document_verification", "phase_revision": 8},
		"bound_req":      map[string]any{"id": "REQ-001"},
	}
	guidance := stageS5PreparationGuidance(root, state)
	for _, path := range []string{".claude/workgroups/REQ-001/review-e8/manifest.json", ".claude/skills/team-planning/SKILL.md"} {
		if !strings.Contains(guidance, filepath.Join(root, filepath.FromSlash(path))) {
			t.Fatalf("S5 preparation omitted exact path %s: %s", path, guidance)
		}
	}
	for _, want := range []string{"S5 entry 8", "DV-SPEC-CONSISTENCY and DV-TASK-EXECUTABILITY", "accepted S4", "runtime stage-answer issue", "普通读取与 Stop 不补登记"} {
		if !strings.Contains(guidance, want) {
			t.Fatalf("S5 preparation lost %q: %s", want, guidance)
		}
	}
	if strings.Contains(guidance, "review-e4/") {
		t.Fatal("S5 preparation borrowed an earlier entry", guidance)
	}
}

func TestS4DeliveryGuidanceNamesExactIndexTemplatesAndAuthorTaskPaths(t *testing.T) {
	root := t.TempDir()
	packet := makePlanningGuidancePacket(t, planningGuidanceState("S4", true), "S4", 7).(workpacket.TaskPlanningPacket)
	guidance := taskPlanningDeliveryGuidance(root, packet, nil)
	for _, path := range []string{"docs/dev/tasks/index-REQ-001.md", "docs/dev/tasks/TASK-template.md", "docs/dev/tasks/index-template.md", "docs/rules/dispatch-plan.md"} {
		if !strings.Contains(guidance, filepath.Join(root, filepath.FromSlash(path))) {
			t.Fatalf("missing S4 output/template path %s: %s", path, guidance)
		}
	}
	if !strings.Contains(guidance, "choose TASK IDs and professional scope") {
		t.Fatal("path guidance must preserve professional task decomposition", guidance)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/dev/tasks"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TASK-EXISTING.md", "TASK-OTHER-REQ.md", "TASK-template.md"} {
		if err := os.WriteFile(filepath.Join(root, "docs/dev/tasks", name), []byte("candidate only"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	guidance = taskPlanningDeliveryGuidance(root, packet, fileview.Disk{Root: root})
	if !strings.Contains(guidance, "Existing TASK candidates (verify current REQ membership)") || !strings.Contains(guidance, filepath.Join(root, "docs/dev/tasks/TASK-EXISTING.md")) {
		t.Fatal("existing file candidates must be shown without claiming REQ membership", guidance)
	}
	pass, index := "pass", "docs/dev/tasks/index-REQ-001.md"
	draft := packet.Draft
	draft.Answer = workpacket.TaskPlanningAnswer{Conclusion: &pass, IndexPath: &index, TaskPaths: []string{"docs/dev/tasks/TASK-SERVICE-001.md"}}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	answer := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if err := os.MkdirAll(filepath.Dir(answer), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(answer, data, 0600); err != nil {
		t.Fatal(err)
	}
	guidance = taskPlanningDeliveryGuidance(root, packet, nil)
	if !strings.Contains(guidance, "Your named TASK delivery") || !strings.Contains(guidance, filepath.Join(root, "docs/dev/tasks/TASK-SERVICE-001.md")) {
		t.Fatal("S4 omitted the author's actual task path", guidance)
	}
	if strings.Contains(guidance, "TASK-OTHER-REQ.md") {
		t.Fatal("candidate paths leaked into the named professional delivery", guidance)
	}
	for _, command := range []string{"stage-answer issue", "stage-answer consume", "runtime evidence record", "stage-answer recover"} {
		if strings.Contains(guidance, command) {
			t.Fatal("S4 added a manual system step", guidance)
		}
	}
}

func TestS4DeliveryGuidanceNamesCommittedBranchOnlyForGitOutputs(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "--initial-branch=guidance-development"}, {"-c", "user.name=fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture", "--allow-empty"}} {
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("Git fixture: %s: %v", output, err)
		}
	}
	packet := makePlanningGuidancePacket(t, planningGuidanceState("S4", true), "S4", 7).(workpacket.TaskPlanningPacket)
	for _, diskTasks := range []bool{false, true} {
		rules := []fileview.Rule{{Path: ".", Source: "git_tree"}, {Path: ".claude", Source: "disk"}}
		if diskTasks {
			rules = append(rules, fileview.Rule{Path: "docs/dev/tasks", Source: "disk"})
		}
		files, err := fileview.New(root, "refs/heads/guidance-development", rules)
		if err != nil {
			t.Fatal(err)
		}
		guidance := taskPlanningDeliveryGuidance(root, packet, files)
		if diskTasks {
			if strings.Contains(guidance, "bound development branch") {
				t.Fatal("disk outputs incorrectly require a commit", guidance)
			}
		} else if !strings.Contains(guidance, "bound development branch guidance-development") || !strings.Contains(guidance, "can change in this entry") || !strings.Contains(guidance, "Before saving this answer") {
			t.Fatal("Git output preparation is left implicit", guidance)
		}
	}
}

func planningGuidanceState(stage string, packetMode bool) map[string]any {
	phase := map[string]string{"S2": "design", "S3": "contracts", "S4": "tasks"}[stage]
	state := map[string]any{
		"schema_version": "1.2.0",
		"runtime_id":     "loop-guidance",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": packetMode}},
		"baseline":       map[string]any{"generation": 2},
		"lifecycle":      map[string]any{"state": "planning", "phase": phase, "phase_revision": 7},
		"bound_req": map[string]any{
			"id": "REQ-001", "path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": planningGuidanceDigest, "status": "locked",
		},
		"documents": []any{},
		"entities":  map[string]any{"work_packets": []any{}, "submissions": []any{}},
	}
	if stage == "S3" {
		state["documents"] = []any{map[string]any{
			"id": "ARCHITECTURE-001", "path": "docs/architecture/ARCHITECTURE-001.md", "version": "v1.0.0", "sha256": strings.Repeat("b", 64), "status": "locked", "kind": "design", "generation": 2,
		}}
	}
	return state
}

func makePlanningGuidancePacket(t *testing.T, state map[string]any, stage string, entry int) any {
	t.Helper()
	if entry != 7 {
		state["lifecycle"].(map[string]any)["phase_revision"] = entry
	}
	switch stage {
	case "S2":
		packet, err := runtime.CurrentDesignPacket(state)
		if err != nil {
			t.Fatal(err)
		}
		return packet
	case "S3":
		binding, designs, err := runtime.CurrentContractBinding(state)
		if err != nil {
			t.Fatal(err)
		}
		packet, err := workpacket.BuildContracts(binding, workpacket.ContractInputs{Designs: designs, Sources: []workpacket.Subject{}, Directories: map[string][]string{}})
		if err != nil {
			t.Fatal(err)
		}
		return packet
	case "S4":
		binding, err := runtime.CurrentTaskPlanningBinding(state)
		if err != nil {
			t.Fatal(err)
		}
		inputs := workpacket.TaskPlanningInputs{
			Contracts: []workpacket.Reference{
				{ID: "CONTRACTS-REQ-001", Path: "docs/dev/contracts/CONTRACTS-REQ-001.md", Version: "v1.0.0", SHA256: strings.Repeat("c", 64)},
				{ID: "BE-REQ-001", Path: "docs/dev/contracts/BE-REQ-001.md", Version: "v1.0.0", SHA256: strings.Repeat("d", 64)},
			},
			Sources: []workpacket.Subject{}, Directories: map[string][]string{},
			ClauseUniverse: []workpacket.TaskClause{{ContractID: "BE-REQ-001", Section: 1}},
		}
		packet, err := workpacket.BuildTaskPlanning(binding, inputs)
		if err != nil {
			t.Fatal(err)
		}
		return packet
	default:
		t.Fatalf("unsupported stage %q", stage)
		return nil
	}
}

func addPlanningGuidancePacket(t *testing.T, state map[string]any, packet any) {
	t.Helper()
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	entities := state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	entities["work_packets"] = append(rows, row)
}

func planningGuidancePacketPaths(packet any) (id, view, answer string) {
	switch p := packet.(type) {
	case workpacket.DesignPacket:
		return p.ID, p.ViewPath, p.AnswerPath
	case workpacket.ContractPacket:
		return p.ID, p.ViewPath, p.AnswerPath
	case workpacket.TaskPlanningPacket:
		return p.ID, p.ViewPath, p.AnswerPath
	default:
		return "", "", ""
	}
}

func mutatePlanningGuidancePacket(t *testing.T, packet any, mutation string) any {
	t.Helper()
	mutate := func(binding *workpacket.PlanningBinding) {
		switch mutation {
		case "old_entry":
			binding.StageEntry--
		case "wrong_req":
			binding.Requirement.ID = "REQ-002"
			binding.Requirement.Path = "docs/requirements/REQ-002.md"
		case "wrong_owner":
			binding.Owner = "worker-1"
		}
	}
	switch p := packet.(type) {
	case workpacket.DesignPacket:
		mutate(&p.Binding)
		return p
	case workpacket.ContractPacket:
		mutate(&p.Binding)
		return p
	case workpacket.TaskPlanningPacket:
		mutate(&p.Binding)
		return p
	default:
		t.Fatalf("unsupported packet type %T", packet)
		return nil
	}
}

func TestMainPlanningNextNamesAutomaticQuestionnairePreparation(t *testing.T) {
	for _, stage := range []string{"S2", "S3", "S4"} {
		t.Run(stage, func(t *testing.T) {
			state := planningGuidanceState(stage, true)
			next := buildNextProjection(state, stage, "specification-planning", "legacy action", t.TempDir())
			if !(strings.Contains(next.Action, "prepared by the Hook listener") || ((stage == "S2" && strings.Contains(next.Action, "system design-entry preparation")) || (stage != "S2" && strings.Contains(next.Action, "questionnaire is missing")))) || strings.Contains(next.Action, "runtime stage-answer issue") {
				t.Fatalf("missing automatic questionnaire preparation: %s", next.Action)
			}
			if next.mainPlanning == nil || next.mainPlanning.IssueAction {
				t.Fatalf("normal continuation must not require an issue command: %+v", next.mainPlanning)
			}
		})
	}
}

func TestMainPlanningNextShowsOnlyCurrentIssuedPacketPaths(t *testing.T) {
	for _, stage := range []string{"S2", "S3", "S4"} {
		t.Run(stage, func(t *testing.T) {
			state := planningGuidanceState(stage, true)
			packet := makePlanningGuidancePacket(t, state, stage, 7)
			id, view, answer := planningGuidancePacketPaths(packet)
			addPlanningGuidancePacket(t, state, packet)
			next := buildNextProjection(state, stage, "specification-planning", "legacy action", t.TempDir())
			if !strings.Contains(next.Action, view) || !strings.Contains(next.Action, answer) || !strings.Contains(next.Action, "fill only the answer") {
				t.Fatalf("current packet paths or fill guidance missing for %s: %s", stage, next.Action)
			}
			if strings.Contains(next.Action, "runtime stage-answer issue") || !strings.Contains(next.Action, id) {
				t.Fatalf("current packet must be shown directly without an issue action: %s", next.Action)
			}
			if next.mainPlanning == nil || next.mainPlanning.IssueAction {
				t.Fatalf("current packet must not require manual missing-view recovery: %+v", next.mainPlanning)
			}
		})
	}
}

func TestMainPlanningNextRejectsOldEntryAndWrongBindings(t *testing.T) {
	for _, stage := range []string{"S2", "S3", "S4"} {
		for _, mutation := range []string{"old_entry", "wrong_req", "wrong_owner"} {
			t.Run(stage+"/"+mutation, func(t *testing.T) {
				state := planningGuidanceState(stage, true)
				packet := makePlanningGuidancePacket(t, state, stage, 7)
				_, view, answer := planningGuidancePacketPaths(packet)
				addPlanningGuidancePacket(t, state, mutatePlanningGuidancePacket(t, packet, mutation))
				next := buildNextProjection(state, stage, "specification-planning", "legacy action", t.TempDir())
				if mutation != "old_entry" {
					if !strings.Contains(next.Action, answer) {
						t.Fatalf("current S2 professional path hidden by source/owner qualification: %s", next.Action)
					}
					return
				}
				if strings.Contains(next.Action, view) || strings.Contains(next.Action, answer) {
					t.Fatalf("stale or misbound packet path leaked: %s", next.Action)
				}
				if mutation == "old_entry" {
					if !strings.Contains(next.Action, "prepared by the Hook listener") && !((stage == "S2" && strings.Contains(next.Action, "system design-entry preparation")) || (stage != "S2" && strings.Contains(next.Action, "questionnaire is missing"))) {
						t.Fatalf("old entry should await current questionnaire preparation: %s", next.Action)
					}
					return
				}
				if strings.Contains(next.Action, "runtime stage-answer issue") || next.mainPlanning == nil || next.mainPlanning.IssueAction {
					t.Fatalf("same-entry mismatched binding must not expose paths or suggest another issue: action=%s planning=%+v", next.Action, next.mainPlanning)
				}
			})
		}
	}
}

func TestMainPlanningNextPreservesPacketOffGuidance(t *testing.T) {
	state := planningGuidanceState("S2", false)
	next := buildNextProjection(state, "S2", "specification-planning", "legacy action", t.TempDir())
	if next.Action != "legacy action" || next.mainPlanning != nil || strings.Contains(next.Action, "stage-answer issue") {
		t.Fatalf("packet-off path changed: %+v", next)
	}
}

func TestWorkerGuidanceDoesNotReceiveMainPlanningIssueAction(t *testing.T) {
	state := planningGuidanceState("S2", true)
	input := policy.Input{
		AgentID: "builder-1",
		Runtime: policy.RuntimeContext{Agent: &policy.AgentContext{ID: "builder-1", State: "working", AssignmentID: "assignment-1"}},
	}
	guidance := buildGuidance(t.TempDir(), state, "SessionStart", input)
	if strings.Contains(guidance.Action, "runtime stage-answer issue") || strings.Contains(strings.Join(guidance.Automation, " "), "Main S2/S3/S4 issue") {
		t.Fatalf("Worker received Main planning action: action=%q automation=%#v", guidance.Action, guidance.Automation)
	}
}

func TestMainPlanningGuidanceUsesAutomaticQuestionnairePreparation(t *testing.T) {
	state := planningGuidanceState("S2", true)
	guidance := buildGuidance(t.TempDir(), state, "SessionStart", policy.Input{})
	if !strings.Contains(guidance.Action, "system design-entry preparation") || strings.Contains(guidance.Action, "runtime stage-answer issue") {
		t.Fatalf("Main guidance still requires a manual issue: %q", guidance.Action)
	}
	joined := strings.Join(guidance.Automation, " ")
	if !strings.Contains(joined, "does not prepare or restore") {
		t.Fatalf("Main guidance lacks automatic questionnaire preparation: %#v", guidance.Automation)
	}
}

func TestS3DeliveryGuidanceNamesCommittedBranchOnlyForGitOutputs(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "--initial-branch=guidance-development"}, {"-c", "user.name=fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture", "--allow-empty"}} {
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("Git fixture: %s: %v", output, err)
		}
	}
	packet := makePlanningGuidancePacket(t, planningGuidanceState("S3", true), "S3", 7).(workpacket.ContractPacket)
	for _, diskContracts := range []bool{false, true} {
		rules := []fileview.Rule{{Path: ".", Source: "git_tree"}, {Path: ".claude", Source: "disk"}}
		if diskContracts {
			rules = append(rules, fileview.Rule{Path: "docs/dev/contracts", Source: "disk"})
		}
		files, err := fileview.New(root, "refs/heads/guidance-development", rules)
		if err != nil {
			t.Fatal(err)
		}
		guidance := contractDeliveryGuidance(root, packet, files)
		if diskContracts {
			if strings.Contains(guidance, "bound development branch") {
				t.Fatal("disk outputs incorrectly require a commit", guidance)
			}
		} else if !strings.Contains(guidance, "bound development branch guidance-development") || !strings.Contains(guidance, "S3 outputs can change in this entry") || !strings.Contains(guidance, "Before saving this answer") {
			t.Fatal("Git output preparation is left implicit", guidance)
		}
	}
}

func TestPlanningReceiptLabelsCompletedAnswerWithoutGuessingCurrentPath(t *testing.T) {
	for _, next := range []string{
		"Current next-stage questionnaire: read /current/S3/view.md; answer /current/S3/answer.md.",
		"The original next-stage questionnaire preparation failed: missing actual source.",
	} {
		var out bytes.Buffer
		renderStageAnswerHook(&out, "/completed/S2/answer.md", submission.Receipt{Accepted: true, WorkComplete: true, CurrentStage: "S3", NextAction: next}, "S2")
		if strings.Contains(out.String(), "Questionnaire: /completed/") || !strings.Contains(out.String(), "Submitted questionnaire (not the current pending answer): /completed/S2/answer.md") || !strings.Contains(out.String(), next) {
			t.Fatalf("completed answer replaced or impersonated the current path: %s", out.String())
		}
	}
	var out bytes.Buffer
	renderStageAnswerHook(&out, "/current/S2/answer.md", submission.Receipt{CurrentStage: "S2"}, "S2")
	if !strings.Contains(out.String(), "Questionnaire: /current/S2/answer.md") {
		t.Fatalf("current unfinished answer path lost: %s", out.String())
	}
}
