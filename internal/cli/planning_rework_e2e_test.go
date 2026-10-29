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
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func reworkCLI(t *testing.T, root, target string) submission.PlanningReworkResult {
	t.Helper()
	var r submission.PlanningReworkResult
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "rework", "--root", root, "--target", target, "--reason", "The professional boundary needs a corrected delivery")), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Committed || r.PreparationStatus != "ready" {
		t.Fatalf("rework: %+v", r)
	}
	return r
}

func TestPlanningReworkCLIOriginalRoutes(t *testing.T) {
	for _, tc := range []struct{ stage, target string }{{"S3", "design"}, {"S3", "contracts"}, {"S4", "design"}, {"S4", "contracts"}, {"S4", "tasks"}} {
		t.Run(tc.stage+"_"+tc.target, func(t *testing.T) {
			var root, oldID, oldPath string
			if tc.stage == "S4" {
				var p workpacket.TaskPlanningPacket
				root, p = newTaskPlanningAnswerFixture(t)
				oldID, oldPath = p.ID, p.AnswerPath
				stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
			} else {
				var p workpacket.ContractPacket
				root, p = newContractAnswerFixture(t)
				oldID, oldPath = p.ID, p.AnswerPath
				stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
			}
			before := stageSnapshot(t, root)
			oldBytes, err := os.ReadFile(filepath.Join(root, oldPath))
			if err != nil {
				t.Fatal(err)
			}
			r := reworkCLI(t, root, tc.target)
			after := stageSnapshot(t, root)
			if !reflect.DeepEqual(before.State["bound_req"], after.State["bound_req"]) || !reflect.DeepEqual(before.State["baseline"], after.State["baseline"]) {
				t.Fatal("rework changed locked REQ or generation")
			}
			life := after.State["lifecycle"].(map[string]any)
			if life["phase"] != tc.target || life["phase_revision"].(float64) != before.State["lifecycle"].(map[string]any)["phase_revision"].(float64)+1 {
				t.Fatal("missing fresh entry", life)
			}
			for _, raw := range after.State["evidence"].([]any) {
				e := raw.(map[string]any)
				if e["kind"] == "planning_design" {
					want := "valid"
					if tc.target == "design" {
						want = "invalid"
					}
					if e["status"] != want {
						t.Fatal("wrong upstream S2 invalidation", e)
					}
				}
				if e["kind"] == "planning_contract" && tc.target != "tasks" && (e["status"] != "invalid" || e["invalidation_rule"] != "planning_rework") {
					t.Fatal("dependent S3 evidence remains current", e)
				}
			}
			for _, raw := range after.State["documents"].([]any) {
				d := raw.(map[string]any)
				if d["kind"] == "design" {
					want := "locked"
					if tc.target == "design" {
						want = "draft"
					}
					if d["status"] != want {
						t.Fatal("wrong S2 document validity", d)
					}
				}
				if d["kind"] == "contract" && tc.target != "tasks" && d["status"] != "draft" {
					t.Fatal("dependent contract stayed locked", d)
				}
			}
			replay := reworkCLI(t, root, tc.target)
			if replay.StageEntry != r.StageEntry || stageSnapshot(t, root).Revision != after.Revision {
				t.Fatal("retry opened another entry")
			}
			current, err := os.ReadFile(filepath.Join(root, oldPath))
			if err != nil || !bytes.Equal(current, oldBytes) {
				t.Fatal("historical answer changed")
			}
			var out, diagnostic bytes.Buffer
			// Old unaccepted answers cannot acquire a new-entry reception.
			cli.Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", oldPath}, nil, &out, &diagnostic)
			now := stageSnapshot(t, root)
			if _, accepted, _ := runtime.FindSubmission(now.State, oldID); accepted {
				t.Fatal("old unaccepted packet replay acquired reception")
			}
			if now.Revision != after.Revision {
				t.Fatal("old packet replay moved Runtime")
			}
			assertDesignCheckpoint(t, root)
		})
	}
}

func TestPlanningReworkCLIRejectsInvalidRequests(t *testing.T) {
	root, _ := newContractAnswerFixture(t)
	for _, flags := range [][]string{{"--target", "tasks", "--reason", "wrong target"}, {"--target", "design"}, {"--target", "design", "--reason", "boundary", "--agent", "Worker"}} {
		before := stageSnapshot(t, root)
		var out, diagnostic bytes.Buffer
		args := append([]string{"runtime", "stage-answer", "rework", "--root", root}, flags...)
		if code := cli.Run(args, nil, &out, &diagnostic); code == 0 {
			t.Fatal("accepted invalid request", args, out.String())
		}
		if stageSnapshot(t, root).Revision != before.Revision {
			t.Fatal("invalid request mutated Runtime")
		}
	}
	before := stageSnapshot(t, root)
	req := before.State["bound_req"].(map[string]any)["path"].(string)
	f, err := os.OpenFile(filepath.Join(root, req), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\nchanged requirement\n")
	f.Close()
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "rework", "--root", root, "--target", "design", "--reason", "cannot hide REQ change"}, nil, &out, &diagnostic); code == 0 {
		t.Fatal("REQ change accepted as rework")
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("REQ change mutated Runtime")
	}
}

func TestPlanningReworkCLIPreservesAcceptedHistory(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	acceptPendingContractForRework(t, root, p)
	before := stageSnapshot(t, root)
	prior, accepted, err := runtime.FindSubmission(before.State, p.ID)
	if err != nil || !accepted {
		t.Fatal("fixture did not accept original answer", err)
	}
	original, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	reworkCLI(t, root, "contracts")
	after := stageSnapshot(t, root)
	preserved, accepted, err := runtime.FindSubmission(after.State, p.ID)
	if err != nil || !accepted || !reflect.DeepEqual(prior, preserved) {
		t.Fatal("original reception history changed", prior, preserved, err)
	}
	if _, err := runtime.SubmissionFollowup(after.State, prior.FollowupID); err == nil {
		t.Fatal("historical followup remained executable")
	}
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", p.AnswerPath)
	if stageSnapshot(t, root).Revision != after.Revision {
		t.Fatal("accepted replay advanced old entry")
	}
	current, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("accepted answer changed")
	}
	assertDesignCheckpoint(t, root)
	// A fresh accepted answer ends the immediate-retry window even when the
	// professional cause recurs; the next request must open another entry.
	var next workpacket.ContractPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &next); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, next.AnswerPath, completedContractDraft(next))
	acceptPendingContractForRework(t, root, next)
	acceptedNew := stageSnapshot(t, root)
	if _, accepted, _ := runtime.FindSubmission(acceptedNew.State, next.ID); !accepted {
		t.Fatal("new entry not accepted")
	}
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Recover(context.Background(), "Main", 1)
	if err != nil || len(receipts) != 1 || receipts[0].PacketID != next.ID {
		t.Fatalf("old pending entry consumed bounded Recover limit: %+v %v", receipts, err)
	}
	r := reworkCLI(t, root, "contracts")
	if r.StageEntry.(float64) <= after.State["lifecycle"].(map[string]any)["phase_revision"].(float64) {
		t.Fatal("new accepted delivery was swallowed by historical retry")
	}
}

func acceptPendingContractForRework(t *testing.T, root string, p workpacket.ContractPacket) {
	t.Helper()
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	receipt, err := s.SaveContracts(ctx, p.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("durable pending reception: %+v %v", receipt, err)
	}
}

func TestPlanningReworkCLIRejectsFrozenDesignDrift(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	path := filepath.Join(root, p.Inputs.Designs[0].Path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\nsubstantive design correction\n")
	f.Close()
	before := stageSnapshot(t, root)
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "rework", "--root", root, "--target", "contracts", "--reason", "correct contracts"}, nil, &out, &diagnostic); code == 0 {
		t.Fatal("contracts rework washed changed design")
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("rejected frozen source changed Runtime")
	}
	reworkCLI(t, root, "design")
	var design workpacket.DesignPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &design); err != nil {
		t.Fatal(err)
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, design.AnswerPath, designDraft(design))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", design.AnswerPath)
	settleMainPlanningFollowups(t, root)
	var replacement workpacket.ContractPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.ID == p.ID || replacement.Inputs.Designs[0].SHA256 == p.Inputs.Designs[0].SHA256 {
		t.Fatal("new S2 self-review failed to establish fresh S3 frozen design")
	}
	assertDesignCheckpoint(t, root)
}

func TestPlanningReworkCLISeparatesCommittedPreparationFailure(t *testing.T) {
	root, _ := newContractAnswerFixture(t)
	before := stageSnapshot(t, root)
	projection := map[string]any{}
	for k, v := range before.State {
		projection[k] = v
	}
	projection["lifecycle"] = map[string]any{"state": "planning", "phase": "design", "phase_revision": int(before.State["lifecycle"].(map[string]any)["phase_revision"].(float64)) + 1}
	p, err := runtime.CurrentDesignPacket(projection)
	if err != nil {
		t.Fatal(err)
	}
	// Make only questionnaire publication fail after the original transaction.
	if err := os.MkdirAll(filepath.Join(root, p.ViewPath), 0700); err != nil {
		t.Fatal(err)
	}
	var r submission.PlanningReworkResult
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "rework", "--root", root, "--target", "design", "--reason", "publication failure remains recoverable")), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Committed || r.PreparationStatus != "failed" || r.PreparationError == "" || r.NextAction == "" {
		t.Fatal("lost committed/preparation boundary", r)
	}
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["phase"] != "design" {
		t.Fatal("failed preparation rolled back entry")
	}
	if err := os.Remove(filepath.Join(root, p.ViewPath)); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["phase_revision"] != after.State["lifecycle"].(map[string]any)["phase_revision"] {
		t.Fatal("issue recovery opened another entry")
	}
}

func acceptPendingTaskForRework(t *testing.T, root string, p workpacket.TaskPlanningPacket) {
	t.Helper()
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	receipt, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("durable pending S4 reception: %+v %v", receipt, err)
	}
}

func TestPlanningReworkCLITasksAcceptedHistoryAndOriginalS5Outlet(t *testing.T) {
	root, original := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, original.AnswerPath, completedTaskPlanningDraft(original))
	acceptPendingTaskForRework(t, root, original)
	before := stageSnapshot(t, root)
	prior, accepted, err := runtime.FindSubmission(before.State, original.ID)
	if err != nil || !accepted {
		t.Fatal("original S4 reception missing", err)
	}
	oldBytes, err := os.ReadFile(filepath.Join(root, original.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	// Own output edits are legal even before the named rework entry; upstream
	// source drift must still be rejected instead of incorporated into new inputs.
	taskPath := "docs/dev/tasks/TASK-039-01.md"
	taskBytes, err := os.ReadFile(filepath.Join(root, taskPath))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, taskPath, string(taskBytes)+"\nCorrected planning explanation.\n")
	indexPath := "docs/dev/tasks/index-REQ-039.md"
	indexBytes, err := os.ReadFile(filepath.Join(root, indexPath))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, indexPath, string(indexBytes)+"\nCorrected dispatch rationale.\n")
	r := reworkCLI(t, root, "tasks")
	for _, want := range []string{"S4", "TASK", "dispatch", "answer", "contracts"} {
		if !strings.Contains(r.NextAction, want) {
			t.Fatalf("missing rework guidance %q: %s", want, r.NextAction)
		}
	}
	after := stageSnapshot(t, root)
	preserved, accepted, err := runtime.FindSubmission(after.State, original.ID)
	if err != nil || !accepted || !reflect.DeepEqual(prior, preserved) {
		t.Fatal("S4 history changed", err)
	}
	for _, raw := range after.State["evidence"].([]any) {
		e := raw.(map[string]any)
		if (e["kind"] == "planning_design" || e["kind"] == "planning_design_record" || e["kind"] == "planning_contract" || e["kind"] == "planning_contract_record") && e["status"] != "valid" {
			t.Fatal("upstream evidence invalidated", e)
		}
		if (e["kind"] == "planning_task" || e["kind"] == "planning_task_record") && e["status"] != "invalid" {
			t.Fatal("old S4 evidence remains valid", e)
		}
	}
	if !reflect.DeepEqual(before.State["documents"], after.State["documents"]) {
		t.Fatal("upstream documents changed")
	}
	replay := reworkCLI(t, root, "tasks")
	if replay.StageEntry != r.StageEntry || stageSnapshot(t, root).Revision != after.Revision {
		t.Fatal("retry opened another S4")
	}
	current, err := os.ReadFile(filepath.Join(root, original.AnswerPath))
	if err != nil || !bytes.Equal(current, oldBytes) {
		t.Fatal("original S4 answer rewritten")
	}
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", original.AnswerPath)
	if stageSnapshot(t, root).Revision != after.Revision {
		t.Fatal("old accepted replay moved new S4")
	}
	var next workpacket.TaskPlanningPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &next); err != nil {
		t.Fatal(err)
	}
	if next.ID == original.ID || !reflect.DeepEqual(original.Inputs, next.Inputs) {
		t.Fatal("replacement changed upstream frozen facts")
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, next.AnswerPath, completedTaskPlanningDraft(next))
	acceptPendingTaskForRework(t, root, next)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Recover(context.Background(), "Main", 1)
	if err != nil || len(receipts) != 1 || receipts[0].PacketID != next.ID {
		t.Fatalf("old pending S4 consumed recovery budget: %+v %v", receipts, err)
	}
	settleMainPlanningFollowups(t, root)
	final := stageSnapshot(t, root)
	if final.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatal("new S4 did not enter S5")
	}
	// S5 cannot use a S4 same-stage rework to bypass independent review.
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "rework", "--root", root, "--target", "tasks", "--reason", "cannot bypass review"}, nil, &out, &diagnostic); code == 0 {
		t.Fatal("tasks rework accepted in S5")
	}
	if stageSnapshot(t, root).Revision != final.Revision {
		t.Fatal("S5 rejection mutated Runtime")
	}
}

func TestPlanningReworkCLITasksRejectsUpstreamDrift(t *testing.T) {
	for _, source := range []string{"design", "contract", "REQ"} {
		t.Run(source, func(t *testing.T) {
			root, p := newTaskPlanningAnswerFixture(t)
			path := p.Inputs.Contracts[0].Path
			switch source {
			case "design":
				for _, ref := range p.Inputs.Sources {
					if strings.Contains(ref.Path, "architecture") {
						path = ref.Path
						break
					}
				}
				if path == p.Inputs.Contracts[0].Path {
					t.Fatal("no frozen architecture source")
				}
			case "REQ":
				path = p.Binding.Requirement.Path
			}
			data, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			writeContractSource(t, root, path, string(data)+"\nChanged frozen upstream fact.\n")
			before := stageSnapshot(t, root)
			var out, diagnostic bytes.Buffer
			if code := cli.Run([]string{"runtime", "stage-answer", "rework", "--root", root, "--target", "tasks", "--reason", "must not wash upstream drift"}, nil, &out, &diagnostic); code == 0 {
				t.Fatal("tasks rework washed upstream drift", source)
			}
			if stageSnapshot(t, root).Revision != before.Revision {
				t.Fatal("rejected drift changed Runtime")
			}
		})
	}
}

func TestPlanningReworkCLITasksInvalidatesOnlyDependentProjections(t *testing.T) {
	root, _ := newTaskPlanningAnswerFixture(t)
	snap := stageSnapshot(t, root)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the action's candidate before Writer commit, including dependent
	// projections that can survive an interrupted original stage outlet.
	state := snap.State
	life := state["lifecycle"].(map[string]any)
	life["phase_revision"] = life["phase_revision"].(float64) + 1
	generation := state["baseline"].(map[string]any)["generation"]
	docs := state["documents"].([]any)
	upstreamBytes, err := json.Marshal(docs)
	if err != nil {
		t.Fatal(err)
	}
	state["documents"] = append(docs,
		map[string]any{"kind": "task", "generation": generation, "status": "locked"},
		map[string]any{"kind": "dispatch_plan", "generation": generation, "status": "locked"})
	evidence := state["evidence"].([]any)
	for _, kind := range []string{"planning_task", "planning_task_record", "document_review", "document_review_record", "unrelated_evidence"} {
		evidence = append(evidence, map[string]any{"kind": kind, "baseline_generation": generation, "status": "valid"})
	}
	state["evidence"] = evidence
	if err := runtime.ApplyPlanningRework(root, state, "tasks", "correct task decomposition", "evt-test-rework", s.Files); err != nil {
		t.Fatal(err)
	}
	revised := state["documents"].([]any)
	preserved, err := json.Marshal(revised[:len(docs)])
	if err != nil || !bytes.Equal(preserved, upstreamBytes) {
		t.Fatal("upstream document projections changed")
	}
	for _, raw := range revised[len(docs):] {
		if raw.(map[string]any)["status"] != "draft" {
			t.Fatal("dependent document remained locked", raw)
		}
	}
	for _, raw := range state["evidence"].([]any) {
		e := raw.(map[string]any)
		switch e["kind"] {
		case "planning_task", "planning_task_record", "document_review", "document_review_record":
			if e["status"] != "invalid" || e["invalidated_by"] != "evt-test-rework" {
				t.Fatal("dependent evidence stayed current", e)
			}
		default:
			if e["status"] != "valid" {
				t.Fatal("unrelated or upstream evidence changed", e)
			}
		}
	}
}

func TestPlanningReworkCLITasksRefreshesMethodDescriptionsWithoutChangingFormalInputs(t *testing.T) {
	root, original := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, original.AnswerPath, completedTaskPlanningDraft(original))
	acceptPendingTaskForRework(t, root, original)
	before := stageSnapshot(t, root)
	methodDigests := map[string]string{}
	for _, path := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		updated := string(data) + "\nClarified professional method description for this planning revision.\n"
		writeContractSource(t, root, path, updated)
		methodDigests[path] = workpacket.Digest([]byte(updated))
	}
	// New issuance reads the current declared Git source for methods. Commit
	// the real description changes rather than replacing any frozen digest.
	fixtures.CommitCurrentFixture(t, root)
	result := reworkCLI(t, root, "tasks")
	encoded, err := json.Marshal(result.Packet)
	if err != nil {
		t.Fatal(err)
	}
	var next workpacket.TaskPlanningPacket
	if err := json.Unmarshal(encoded, &next); err != nil {
		t.Fatal(err)
	}
	if next.ID == original.ID {
		t.Fatal("method clarification did not open a new S4 entry")
	}
	if original.Binding.Requirement != next.Binding.Requirement || !reflect.DeepEqual(original.Inputs.Contracts, next.Inputs.Contracts) || !reflect.DeepEqual(original.Inputs.ClauseUniverse, next.Inputs.ClauseUniverse) || !reflect.DeepEqual(original.Inputs.Directories, next.Inputs.Directories) {
		t.Fatal("method refresh changed frozen formal inputs")
	}
	formal := func(p workpacket.TaskPlanningPacket) []workpacket.Subject {
		var refs []workpacket.Subject
		for _, ref := range p.Inputs.Sources {
			if _, method := methodDigests[ref.Path]; !method {
				refs = append(refs, ref)
			}
		}
		return refs
	}
	if !reflect.DeepEqual(formal(original), formal(next)) {
		t.Fatal("method refresh changed REQ/S2/S3 source closure")
	}
	for path, digest := range methodDigests {
		var oldDigest, newDigest string
		for _, ref := range original.Inputs.Sources {
			if ref.Path == path {
				oldDigest = ref.SHA256
			}
		}
		for _, ref := range next.Inputs.Sources {
			if ref.Path == path {
				newDigest = ref.SHA256
			}
		}
		if oldDigest == "" || oldDigest == digest || newDigest != digest {
			t.Fatalf("new S4 did not freeze current method %s: old=%s new=%s want=%s", path, oldDigest, newDigest, digest)
		}
	}
	after := stageSnapshot(t, root)
	if !reflect.DeepEqual(before.State["bound_req"], after.State["bound_req"]) || !reflect.DeepEqual(before.State["baseline"], after.State["baseline"]) || !reflect.DeepEqual(before.State["documents"], after.State["documents"]) {
		t.Fatal("method refresh changed locked REQ, baseline or upstream documents")
	}
	for _, raw := range after.State["evidence"].([]any) {
		e := raw.(map[string]any)
		if (e["kind"] == "planning_design" || e["kind"] == "planning_design_record" || e["kind"] == "planning_contract" || e["kind"] == "planning_contract_record") && e["status"] != "valid" {
			t.Fatal("method refresh invalidated upstream evidence", e)
		}
	}
}
