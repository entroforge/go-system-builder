package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS4OriginalSavePreparesS5WithoutMainMetadataOrConfirmation(t *testing.T) {
	root, packet := s4HandoffFixture(t)
	stageJSON(t, root, packet.AnswerPath, completedTaskPlanningDraft(packet))
	path := filepath.Join(root, packet.AnswerPath)
	body, _ := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "normal-s4-to-s5", "cwd": root,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{},
	})
	var out, diag bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(body), &out, &diag); code != 0 {
		t.Fatalf("original save failed %d: %s %s", code, &out, &diag)
	}
	snapshot := stageSnapshot(t, root)
	if snapshot.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatalf("normal S4 outlet did not enter S5: %s %s", &out, &diag)
	}
	packets, err := runtime.S5Packets(root, snapshot.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("S4 completion left machine preparation to Main: packets=%d err=%v out=%s diag=%s", len(packets), err, &out, &diag)
	}
	for _, p := range packets {
		for _, source := range packet.Inputs.Sources {
			if source.Path != "docs/rules/dispatch-plan.md" && source.Path != "docs/dev/tasks/TASK-template.md" {
				continue
			}
			found := false
			for _, input := range p.Inputs {
				if input.Path == source.Path {
					found = true
					version := source.Version
					if version == "" {
						version = "unversioned"
					}
					if input.Version != version {
						t.Fatalf("unchanged method changed S5 input version: %s", source.Path)
					}
				}
			}
			if !found {
				t.Fatalf("unchanged method omitted from S5 identity: %s", source.Path)
			}
		}
		if _, found, err := runtime.FindWorkPacket(snapshot.State, p.ID); err != nil || !found {
			t.Fatalf("S5 responsibility not actually issued: %s %v", p.ID, err)
		}
		for _, ref := range []string{p.Binding.Owner, p.Binding.Responsibility, filepath.Join(root, p.ViewPath), filepath.Join(root, p.AnswerPath)} {
			if !strings.Contains(out.String(), ref) {
				t.Fatalf("handoff did not give actual S5 dispatch reference %q: %s", ref, &out)
			}
		}
	}
}

func s4HandoffFixture(t *testing.T) (string, workpacket.TaskPlanningPacket) {
	return newTaskPlanningAnswerFixture(t, func(root string) {
		for _, rel := range []string{"docs/control/agent-protocol.md", "docs/rules"} {
			copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, rel))
		}
		for _, rel := range []string{"agents/document-verifier.md", "skills/document-verification", "skills/specification-planning", "skills/api-contracts", "skills/testing-strategy", "skills/agent-dispatch"} {
			copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, ".claude", rel))
		}
	})
}

func TestS4NamedOutletsDeliverPreparedS5InSameResponse(t *testing.T) {
	for _, action := range []string{"consume", "recover", "post"} {
		t.Run(action, func(t *testing.T) {
			root, p := s4HandoffFixture(t)
			stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
			if action == "recover" || action == "post" {
				service, err := submission.New(root)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := pendingMainFollowupContext(service, p.ID)
				r, err := service.SaveTaskPlanning(ctx, p.AnswerPath)
				cancel()
				if err != nil || !r.Accepted {
					t.Fatalf("pending reception: %+v %v", r, err)
				}
			}
			args := []string{"runtime", "stage-answer", action, "--root", root, "--agent", "Main"}
			if action == "consume" {
				args = append(args, "--path", p.AnswerPath)
			}
			var output string
			if action == "post" {
				body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "s4-original-post-followup", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, "docs/control/agent-protocol.md")}, "tool_response": map[string]any{}})
				var out, diag bytes.Buffer
				if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(body), &out, &diag); code != 0 {
					t.Fatalf("post %d: %s %s", code, &out, &diag)
				}
				output = out.String()
			} else {
				output = stageCLI(t, args...)
			}
			snap := stageSnapshot(t, root)
			packets, err := runtime.S5Packets(root, snap.State, "")
			if err != nil || len(packets) != 2 {
				t.Fatalf("packets %d %v %s", len(packets), err, output)
			}
			for _, packet := range packets {
				for _, ref := range []string{packet.ViewPath, packet.AnswerPath, "source root", root} {
					if !strings.Contains(output, ref) {
						t.Fatalf("missing %s: %s", ref, output)
					}
				}
			}
			before := snap.Revision
			again := stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
			if !strings.Contains(again, "source root") || stageSnapshot(t, root).Revision != before {
				t.Fatal("replay changed original reception", again)
			}
		})
	}
}

func TestS4PreparationFailureSurvivesSessionAndIssueRepair(t *testing.T) {
	root, p := s4HandoffFixture(t)
	missing := filepath.Join(root, ".claude/skills/document-verification/SKILL.md")
	data, err := os.ReadFile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	out := stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
	if !strings.Contains(out, "stage-answer issue") || !strings.Contains(out, "document-verification") {
		t.Fatalf("failure lost repair %s", out)
	}
	snap := stageSnapshot(t, root)
	if snap.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatal("transition revoked")
	}
	session := s3DeliveredContext(t, designGuidanceEvent(t, root, "s5-prepare-failure-new-session", "SessionStart", "Read"))
	if !strings.Contains(session, "document-verification") || !strings.Contains(session, "stage-answer issue") {
		t.Fatal("session lost cause", session)
	}
	if stageSnapshot(t, root).Revision != snap.Revision {
		t.Fatal("session mutated preparation")
	}
	if err := os.WriteFile(missing, data, 0600); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	repaired := stageSnapshot(t, root)
	if diagnostic := submission.PlanningDiagnostic(root, repaired.State, "preparation"); diagnostic != "" {
		t.Fatal("stale cause", diagnostic)
	}
	packets, err := runtime.S5Packets(root, repaired.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("repair %d %v", len(packets), err)
	}
	// A later partial publication failure must retain the peer's authored
	// bytes and both original packet identities when issue resumes it.
	authored := []byte("{\"author_work\":\"keep this answer\"}\n")
	answerPath := filepath.Join(root, packets[0].AnswerPath)
	if err := os.WriteFile(answerPath, authored, 0600); err != nil {
		t.Fatal(err)
	}
	blockedView := filepath.Join(root, packets[1].ViewPath)
	if err := os.Remove(blockedView); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blockedView, 0700); err != nil {
		t.Fatal(err)
	}
	var failedOut, failedErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", root, "--agent", "Main"}, nil, &failedOut, &failedErr); code == 0 {
		t.Fatal("blocked publication succeeded")
	}
	if err := os.Remove(blockedView); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	if actual, err := os.ReadFile(answerPath); err != nil || !bytes.Equal(actual, authored) {
		t.Fatalf("peer answer overwritten: %s %v", actual, err)
	}
	resumed, err := runtime.S5Packets(root, stageSnapshot(t, root).State, "")
	if err != nil || len(resumed) != 2 || resumed[0].ID != packets[0].ID || resumed[1].ID != packets[1].ID {
		t.Fatalf("partial recovery replaced original packets %v %v", resumed, err)
	}
}

func TestS4ReceptionFieldFailurePersistsAcrossSessionAndClears(t *testing.T) {
	root, p := s4HandoffFixture(t)
	draft := completedTaskPlanningDraft(p)
	invalid := "/outside/index.md"
	draft.Answer.IndexPath = &invalid
	stageJSON(t, root, p.AnswerPath, draft)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := service.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err == nil || r.Accepted {
		t.Fatal("invalid path received")
	}
	// The rejected attempt keeps its concrete field reason in the current S4
	// scope, so a later session can still name it.
	pending := stageSnapshot(t, root)
	cause := submission.PlanningDiagnostic(root, pending.State, "reception")
	if !strings.Contains(cause, "index_path") || !strings.Contains(cause, invalid) {
		t.Fatalf("pending reception reason is not the field reason: %s", cause)
	}
	for _, event := range []string{"PreToolUse", "SessionStart"} {
		text := s3DeliveredContext(t, designGuidanceEvent(t, root, "s4-field-"+event, event, "Read"))
		if !strings.Contains(text, "index_path") || !strings.Contains(text, "outside") {
			t.Fatalf("%s lost cause %s", event, text)
		}
		if !strings.Contains(text, "/answer/index_path") || !strings.Contains(text, invalid) {
			t.Fatalf("%s lost the exact field cause %s", event, text)
		}
	}
	// Blueprint L3-S4-task-split.md:494 and L4-state-transition-core.md:163:
	// the natural Hook and the original consumer no longer compute, register
	// or verify a digest. An edited accepted upstream contract therefore stays
	// the same original source by its association, so editing it must not
	// change the pending reason or the S4 reception result.
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	sourcePath := p.Inputs.Contracts[0].Path
	originalSource, readErr := os.ReadFile(filepath.Join(root, sourcePath))
	if readErr != nil {
		t.Fatal(readErr)
	}
	writeContractSource(t, root, sourcePath, string(originalSource)+"\nchanged accepted contract description\n")
	fixtures.CommitCurrentFixture(t, root)
	service, err = submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	editedSession := s3DeliveredContext(t, designGuidanceEvent(t, root, "s4-source-edited-session", "SessionStart", "Read"))
	if !strings.Contains(editedSession, invalid) {
		t.Fatalf("edited original source lost the pending field reason %s", editedSession)
	}
	for _, invented := range []string{"source changed", "changed source", "source has changed", "source is changed", "source drift"} {
		if strings.Contains(strings.ToLower(editedSession), invented) {
			t.Fatalf("session invented a %q reason: %s", invented, editedSession)
		}
	}
	if strings.Contains(editedSession, "changed accepted contract description") {
		t.Fatalf("session reported the edited original source as changed: %s", editedSession)
	}
	cause = submission.PlanningDiagnostic(root, pending.State, "reception")
	if !strings.Contains(cause, "index_path") || !strings.Contains(cause, invalid) {
		t.Fatalf("edited original source changed the pending reason: %s", cause)
	}
	if strings.Contains(cause, sourcePath) {
		t.Fatalf("pending reception invented a source change reason: %s", cause)
	}
	r, err = service.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil {
		t.Fatalf("original source association was lost: %v", err)
	}
	if !r.Accepted {
		t.Fatalf("original source association was rejected: %+v", r)
	}
	// The correct answer clears the pending reception reason.
	if diagnostic := submission.PlanningDiagnostic(root, pending.State, "reception"); diagnostic != "" {
		t.Fatal("stale field cause", diagnostic)
	}
}

func TestS4ChangedMethodTextPreparesS5WithCurrentMethodSource(t *testing.T) {
	root, p := s4HandoffFixture(t)
	updated := map[string]bool{}
	for _, path := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		body := append(data, []byte("\nUpdated professional explanation for this S5 preparation.\n")...)
		writeContractSource(t, root, path, string(body))
		updated[path] = true
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "s4-method-update-handoff", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}, "tool_response": map[string]any{}})
	var out, diag bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(body), &out, &diag); code != 0 {
		t.Fatalf("normal method update save: %d %s %s", code, &out, &diag)
	}
	snap := stageSnapshot(t, root)
	packets, err := runtime.S5Packets(root, snap.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("S5 method preparation %d %v %s %s", len(packets), err, &out, &diag)
	}
	for _, packet := range packets {
		if _, found, err := runtime.FindWorkPacket(snap.State, packet.ID); err != nil || !found {
			t.Fatalf("unissued packet %s %v", packet.ID, err)
		}
		for path := range updated {
			found := false
			for _, ref := range packet.Inputs {
				if ref.Path == path {
					found = true
				}
			}
			if !found {
				t.Fatalf("current S5 method omitted %s", path)
			}
		}
		for _, ref := range []string{packet.ViewPath, packet.AnswerPath, "source root"} {
			if !strings.Contains(out.String(), ref) {
				t.Fatalf("missing %s: %s", ref, &out)
			}
		}
	}
	// Reissuing after method edits keeps the original issued view and responsibility.
	frozenView, err := os.ReadFile(filepath.Join(root, packets[0].ViewPath))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, "docs/rules/dispatch-plan.md", "later method drift after S5 issuance\n")
	fixtures.CommitCurrentFixture(t, root)
	var issueOut, issueErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", root, "--agent", "Main"}, nil, &issueOut, &issueErr); code != 0 {
		t.Fatalf("reissuing the original S5 responsibility failed after method edit: %s", issueErr.String())
	}
	if actual, err := os.ReadFile(filepath.Join(root, packets[0].ViewPath)); err != nil || !bytes.Equal(actual, frozenView) {
		t.Fatalf("issued view overwritten: %v", err)
	}
	for _, packet := range packets {
		if _, found, err := runtime.FindWorkPacket(stageSnapshot(t, root).State, packet.ID); err != nil || !found {
			t.Fatalf("original S5 identity replaced %s %v", packet.ID, err)
		}
	}
}

// Reconstruct a prior issuer through the original packet Writer before any S4
// receipt exists. Acceptance and both successor issuances use production paths.
func TestS4LegacyContractsDirectoryMetadataPreparesOriginalS5(t *testing.T) {
	root, s3 := newContractAnswerFixture(t, func(root string) {
		path := filepath.Join(root, "docs/dev/contracts/CONTRACTS-039.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, "docs/dev/contracts/CONTRACTS-039.md", string(data)+"| REQ-039/NFR-001 | — | BE-039 §1 | — |\n")
		for _, rel := range []string{"docs/control/agent-protocol.md", "docs/rules"} {
			copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, rel))
		}
		for _, rel := range []string{"agents/document-verifier.md", "skills/document-verification", "skills/specification-planning", "skills/api-contracts", "skills/testing-strategy", "skills/agent-dispatch"} {
			copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, ".claude", rel))
		}
		if err := os.Remove(filepath.Join(root, "docs/dev/tasks/TASK-template.md")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	})
	stageJSON(t, root, s3.AnswerPath, completedContractDraft(s3))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveContracts(context.Background(), s3.AnswerPath)
	if err != nil || !receipt.Accepted || receipt.CurrentStage != "S4" {
		t.Fatalf("S3 original exit %+v %v", receipt, err)
	}
	for _, raw := range stageSnapshot(t, root).State["entities"].(map[string]any)["work_packets"].([]any) {
		if raw.(map[string]any)["template_id"] == "s4-task-planning-v1" {
			t.Fatal("fixture already issued S4 before historical issuer")
		}
	}
	template, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), "docs/dev/tasks/TASK-template.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, "docs/dev/tasks/TASK-template.md", string(template))
	writeContractSource(t, root, "docs/dev/tasks/TASK-039-01.md", "# Task\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-039\n> Primary contract: BE-039\n\n## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-039 | §1 |\n\n## 4. Scope\n\n| Type | Paths / Commands |\n|:--|:--|\n| prospective write paths | `internal/fixture/service.go` |\n\n## 7. Closing Contract\n\n```text\nassert BE-039 §1 == satisfied\n```\n")
	writeContractSource(t, root, "docs/dev/tasks/index-REQ-039.md", "# REQ-039 dispatch\n> REQ: REQ-039\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1 First wave\n\n- [ ] [TASK-039-01](TASK-039-01.md)\n")
	fixtures.CommitCurrentFixture(t, root)
	service, err = submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	state := stageSnapshot(t, root).State
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, service.Files)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "docs/dev/contracts"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	inputs.Directories["docs/dev/contracts"] = names
	oldTemplate, err := os.ReadFile(filepath.Join("..", "schema", "assets", "stage-form-s4-template-before-guidance.json"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	var issuedTemplate struct {
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal(oldTemplate, &issuedTemplate); err != nil {
		t.Fatal(err)
	}
	old.Draft.Instruction = issuedTemplate.Instruction
	if _, err := runtime.RecordTaskPlanningPacket(root, service.StatePath, service.JournalPath, old, service.Files, service.Validator); err != nil {
		t.Fatalf("historical original issuance %v", err)
	}
	issued, err := service.IssueTaskPlanning(context.Background())
	if err != nil || issued.ID != old.ID {
		t.Fatalf("original publication %+v %v", issued, err)
	}
	stageJSON(t, root, old.AnswerPath, completedTaskPlanningDraft(old))
	output := stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", old.AnswerPath)
	snap := stageSnapshot(t, root)
	packets, err := runtime.S5Packets(root, snap.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("legacy S4 to S5 %d %v %s", len(packets), err, output)
	}
	for _, packet := range packets {
		if _, found, err := runtime.FindWorkPacket(snap.State, packet.ID); err != nil || !found {
			t.Fatalf("successor not issued %s %v", packet.ID, err)
		}
	}
}
