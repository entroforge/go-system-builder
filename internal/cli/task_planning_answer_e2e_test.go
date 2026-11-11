package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func newTaskPlanningAnswerFixture(t *testing.T, configure ...func(string)) (string, workpacket.TaskPlanningPacket) {
	t.Helper()
	setup := func(root string) {
		path := filepath.Join(root, "docs/dev/contracts/CONTRACTS-039.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, "docs/dev/contracts/CONTRACTS-039.md", string(data)+"| REQ-039/NFR-001 | — | BE-039 §1 | — |\n")
		for _, rel := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
			data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
			if err != nil {
				t.Fatal(err)
			}
			writeContractSource(t, root, rel, string(data))
		}
	}
	root, s3 := newContractAnswerFixture(t, append([]func(string){setup}, configure...)...)
	stageJSON(t, root, s3.AnswerPath, completedContractDraft(s3))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", s3.AnswerPath)
	settleMainPlanningFollowups(t, root)
	writeContractSource(t, root, "docs/dev/tasks/TASK-039-01.md", "# Task\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-039\n> Primary contract: BE-039\n\n## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-039 | §1 |\n\n## 4. Scope\n\n| Type | Paths / Commands |\n|:--|:--|\n| prospective write paths | `internal/fixture/service.go` |\n\n## 7. Closing Contract\n\n```text\nassert BE-039 §1 == satisfied\n```\n")
	writeContractSource(t, root, "docs/dev/tasks/index-REQ-039.md", "# REQ-039 dispatch\n> REQ: REQ-039\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1 First wave\n\n- [ ] [TASK-039-01](TASK-039-01.md)\n")
	fixtures.CommitCurrentFixture(t, root)
	var p workpacket.TaskPlanningPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &p); err != nil {
		t.Fatal(err)
	}
	return root, p
}

func completedTaskPlanningDraft(p workpacket.TaskPlanningPacket) workpacket.TaskPlanningDraft {
	pass, index := "pass", "docs/dev/tasks/index-REQ-039.md"
	a := p.Draft
	a.Answer = workpacket.TaskPlanningAnswer{Conclusion: &pass, IndexPath: &index, TaskPaths: []string{"docs/dev/tasks/TASK-039-01.md"}}
	return a
}

func TestTaskPlanningAnswerMainSaveOriginalTR002(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	assertDesignCheckpoint(t, root)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}, "cwd": root})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("S4 Hook %d %s %s", code, out.String(), errout.String())
	}
	if !strings.Contains(out.String(), "S4 SUBMISSION RECEIPT") {
		t.Fatal(out.String())
	}
	settleMainPlanningFollowups(t, root)
	snap := stageSnapshot(t, root)
	r, found, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil || !found || r.ControlStatus != "advanced" {
		s, _ := submission.New(root)
		catalog, _ := transition.LoadCatalog(root)
		registry, _ := qualitygate.NewRegistry(catalog)
		if s != nil {
			gate, gateErr := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{Root: root, Snapshot: snap, TransitionID: "TR-002", GateID: "GATE-PLANNING-TASKS-COMPLETE", Files: s.Files})
			t.Logf("original S4 gate: %+v %v", gate, gateErr)
		}
		t.Fatalf("original S4 reception: %+v %v %s %s", r, err, out.String(), errout.String())
	}
	if snap.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatal("original TR-002 did not enter S5")
	}
	assertDesignCheckpoint(t, root)
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
	if stageSnapshot(t, root).Revision != snap.Revision {
		t.Fatal("S4 replay repeated reception or transition")
	}
}

func TestS5FormalPacketSubjectsIncludeAcceptedS4Closure(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := s.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("accept S4 task planning: %+v %v", receipt, err)
	}
	settleMainPlanningFollowups(t, root)
	snap := stageSnapshot(t, root)
	s, err = submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	formal, err := runtime.S5FormalInputs(root, snap.State, s.Files)
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]workpacket.Reference{}
	for _, ref := range formal.References {
		refs[ref.Path] = ref
	}
	for _, source := range p.Inputs.Sources {
		if _, ok := refs[source.Path]; !ok {
			t.Errorf("S5 closure omitted accepted S4 source %s", source.Path)
		}
	}
	for _, path := range []string{"docs/dev/tasks/index-REQ-039.md", "docs/dev/tasks/TASK-039-01.md"} {
		if _, ok := refs[path]; !ok {
			t.Errorf("S5 closure omitted accepted S4 output %s", path)
		}
	}
	if got := refs["docs/dev/tasks/index-REQ-039.md"].ID; got != "dispatch-plan:REQ-039" {
		t.Errorf("S5 index subject ID = %q", got)
	}
	if refs["docs/rules/dispatch-plan.md"].Version == "" {
		t.Error("S5 source ref must make an omitted source version explicit")
	}
	binding := workpacket.Binding{RuntimeID: p.Binding.RuntimeID, Generation: p.Binding.Generation, StageEntry: int(snap.State["lifecycle"].(map[string]any)["phase_revision"].(float64)), Owner: "reviewer-task", AssignmentID: "assignment-task", Responsibility: "DV-TASK-EXECUTABILITY", Attempt: 1}
	packet, err := workpacket.BuildS5(binding, formal.References)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(packet.Inputs, formal.References) {
		t.Fatal("S5 packet did not freeze the accepted S4 source and output closure")
	}
}

func TestTaskPlanningAnswerPendingFollowupRecovery(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	r, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	if err != nil || !r.Accepted || !r.WorkComplete {
		t.Fatalf("pending S4 reception: %+v %v", r, err)
	}
	before := stageSnapshot(t, root)
	prior, found, err := runtime.FindSubmission(before.State, p.ID)
	if err != nil || !found || prior.ControlStatus != "pending" {
		t.Fatalf("S4 not pending: %+v %v", prior, err)
	}
	assertDesignCheckpoint(t, root)
	path, pair := latestStageCheckpoint(t, root)
	damageStagePair(t, root)
	plan := checkpointPlan(t, root, path, pair.State)
	stageCLI(t, "runtime", "recover", "apply", "--root", root, "--plan", plan, "--approved-by", "operator")
	restored := stageSnapshot(t, root)
	restoredReceipt, found, err := runtime.FindSubmission(restored.State, p.ID)
	if err != nil || !found || !reflect.DeepEqual(restoredReceipt, prior) || len(restored.State["evidence"].([]any)) != len(before.State["evidence"].([]any)) {
		t.Fatalf("recovery did not retain the original S4 reception: %+v %v", restoredReceipt, err)
	}
	stageCLI(t, "runtime", "stage-answer", "recover", "--root", root, "--agent", "Main")
	after := stageSnapshot(t, root)
	next, _, err := runtime.FindSubmission(after.State, p.ID)
	if err != nil || next.ControlStatus != "advanced" || next.EvidenceID != prior.EvidenceID || len(before.State["evidence"].([]any)) != len(after.State["evidence"].([]any)) {
		t.Fatalf("S4 followup changed professional judgment: %+v %v", next, err)
	}
	assertDesignCheckpoint(t, root)
}

func TestTaskPlanningAnswerRejectsRealMissingWorkButAllowsTemplateDescriptionChange(t *testing.T) {
	for _, mode := range []string{"incomplete", "missing_scope", "source_drift"} {
		t.Run(mode, func(t *testing.T) {
			root, p := newTaskPlanningAnswerFixture(t)
			if mode != "incomplete" {
				stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
			}
			if mode == "missing_scope" {
				writeContractSource(t, root, "docs/dev/tasks/TASK-039-01.md", "# Task\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-039\n> Primary contract: BE-039\n\n## 3. Delivered Clauses\n| Contract | Delivered clauses |\n|---|---|\n| BE-039 | §1 |\n\n## 7. Closing Contract\n```text\nassert BE-039 §1 == satisfied\n```\n")
				fixtures.CommitCurrentFixture(t, root)
			}
			if mode == "source_drift" {
				writeContractSource(t, root, "docs/rules/dispatch-plan.md", "changed frozen dispatch rule\n")
			}
			before := stageSnapshot(t, root)
			s, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			r, saveErr := s.SaveTaskPlanning(context.Background(), p.AnswerPath)
			if mode == "source_drift" {
				if saveErr != nil || !r.Accepted || !r.WorkComplete {
					t.Fatalf("historical template text became a new qualification: %+v %v", r, saveErr)
				}
				return
			}
			if saveErr == nil || r.Accepted {
				t.Fatalf("invalid S4 accepted: %+v %v", r, saveErr)
			}
			after := stageSnapshot(t, root)
			if before.Revision != after.Revision || len(before.State["evidence"].([]any)) != len(after.State["evidence"].([]any)) {
				t.Fatal("rejected task planning occupied original reception")
			}
		})
	}
}
