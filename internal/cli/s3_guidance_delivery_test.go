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
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Exercise the public Hook response: a helper returning complete text is not
// sufficient unless both the first handoff and subsequent observations carry it.
func TestS3PublicGuidanceDeliversOriginalResponsibilityWithoutSourceRebuild(t *testing.T) {
	root := newS3ReworkFixture(t)
	packet := prepareS3ThroughSourceCommit(t, root)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	before, _ := os.ReadFile(statePath)
	journal, _ := os.ReadFile(journalPath)
	for _, event := range []string{"PreToolUse", "SessionStart"} {
		output := s3DeliveredContext(t, designGuidanceEvent(t, root, "s3-complete-guidance", event, "Bash"))
		for _, want := range []string{packet.ViewPath, packet.AnswerPath, "CONTRACTS-039.md", "FE-039.md", "BE-039.md", "SYNC-039.md", "CONTRACTS-template.md", "FE-contract-template.md", "BE-contract-template.md", "SYNC-contract-template.md", "bound development branch", "specification-planning/SKILL.md", "at least one real applicable contract", "stage adaptation gap"} {
			if !strings.Contains(output, want) {
				t.Errorf("%s lacks %q: %s", event, want, output)
			}
		}
		if strings.Contains(output, "Read order: LOOP RECOVERY packet (this message) -> AGENTS") || strings.Contains(output, "-> .claude/loop-state.json") {
			t.Fatalf("%s requires general recovery reading for an issued responsibility: %s", event, output)
		}
	}
	// A broken upstream file must not cause ordinary observation to resolve and
	// reject the already issued source closure. Missing original questionnaire
	// files remain visible as concrete diagnostics, however.
	if len(packet.Inputs.Designs) == 0 {
		t.Fatal("fixture requires frozen design input")
	}
	writeContractSource(t, root, packet.Inputs.Designs[0].Path, "# Changed uncommitted upstream design\n")
	drifted := s3DeliveredContext(t, designGuidanceEvent(t, root, "s3-upstream-drift", "PreToolUse", "Bash"))
	if !strings.Contains(drifted, packet.AnswerPath) || !strings.Contains(drifted, "Conventional output candidates") {
		t.Fatalf("ordinary observation rebuilt frozen sources: %s", drifted)
	}
	if err := os.Remove(filepath.Join(root, packet.ViewPath)); err != nil {
		t.Fatal(err)
	}
	output := s3DeliveredContext(t, designGuidanceEvent(t, root, "s3-missing-view", "PreToolUse", "Read"))
	if !strings.Contains(output, packet.ViewPath) || !strings.Contains(output, "Current material missing") {
		t.Fatalf("missing original view not reported: %s", output)
	}
	if got, _ := os.ReadFile(statePath); !bytes.Equal(got, before) {
		t.Fatal("ordinary S3 guidance modified Runtime")
	}
	if got, _ := os.ReadFile(journalPath); !bytes.Equal(got, journal) {
		t.Fatal("ordinary S3 guidance modified journal")
	}
}

func TestS3FirstHandoffDeliversCandidatesAndSource(t *testing.T) {
	root := newS3ReworkFixture(t)
	body := map[string]any{"hook_event_name": "PreToolUse", "session_id": "actual-entry", "tool_use_id": "entry-commit", "cwd": root, "tool_name": "Bash", "tool_input": map[string]any{"command": "git commit -qm delivered"}}
	invoke := func(event string) string {
		body["hook_event_name"] = event
		data, _ := json.Marshal(body)
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(data), &out, &diagnostic); code != 0 {
			t.Fatalf("%s: %d %s %s", event, code, &out, &diagnostic)
		}
		return s3DeliveredContext(t, out.String())
	}
	invoke("PreToolUse")
	fixtures.CommitCurrentFixture(t, root)
	body["tool_response"] = map[string]any{"exit_code": 0}
	output := invoke("PostToolUse")
	packet := s3PacketFromCurrentRuntime(t, root)
	for _, want := range []string{packet.AnswerPath, "bound development branch", "Conventional output candidates", "SYNC-contract-template.md"} {
		if !strings.Contains(output, want) {
			t.Errorf("actual successful preparation response lacks %q: %s", want, output)
		}
	}
}

func TestS3PendingPublicGuidanceKeepsOriginalBatchBranchAndRecover(t *testing.T) {
	root, packet := newContractAnswerFixture(t)
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: service.StatePath, packetID: packet.ID, cancel: cancel}
	receipt, err := service.SaveContracts(ctx, packet.AnswerPath)
	if err != nil || !receipt.Accepted || len(receipt.WaitingOn) == 0 {
		t.Fatalf("pending receipt: %+v %v", receipt, err)
	}
	for _, event := range []string{"PreToolUse", "SessionStart"} {
		output := s3DeliveredContext(t, designGuidanceEvent(t, root, "pending-s3", event, "Read"))
		for _, want := range []string{"Recorded accepted delivery", "CONTRACTS-039.md", "BE-039.md", "bound development branch", "runtime stage-answer recover"} {
			if !strings.Contains(output, want) {
				t.Errorf("%s lacks %q: %s", event, want, output)
			}
		}
		for _, unwanted := range []string{"save the same answer", "locked_contract_set", "planning_contract_record", "outputs can change", "Before saving this answer"} {
			if strings.Contains(output, unwanted) {
				t.Errorf("%s carries conflicting %q: %s", event, unwanted, output)
			}
		}
	}
}

func TestS3SuccessfulReceiptDeliversS4WorkBeforeAnotherTool(t *testing.T) {
	root := newS3ReworkFixture(t)
	packet := prepareS3ThroughSourceCommit(t, root)
	index, contract, _ := writeS3SharedModelBatch(t, root, true, false)
	stageJSON(t, root, packet.AnswerPath, s3AnswerDraft(packet, index, contract))
	fixtures.CommitCurrentFixture(t, root)
	output, diagnostic := saveS3ThroughPostToolUse(t, root, packet.AnswerPath)
	output = s3DeliveredContext(t, output)
	for _, want := range []string{"Current next-stage questionnaire", "TASK-template.md", "index-template.md", "bound development branch", "TASK output directory"} {
		if !strings.Contains(output, want) {
			t.Errorf("S3 save stdout lacks S4 %q: %s (stderr %s)", want, output, diagnostic)
		}
	}
	resumed := s3DeliveredContext(t, designGuidanceEvent(t, root, "new-s4", "PreToolUse", "Read"))
	for _, want := range []string{"TASK-template.md", "index-template.md", "bound development branch", "TASK output directory"} {
		if !strings.Contains(resumed, want) {
			t.Errorf("S4 Pre lacks %q: %s", want, resumed)
		}
	}
}

func TestS2SuccessfulSaveDeliversCompleteS3InSameHookResponse(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	writeContractSource(t, root, "docs/design/README.md", "# Current design\n")
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "direct-s2-save", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, packet.AnswerPath)}, "tool_response": map[string]any{}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("save: %d %s %s", code, &out, &diagnostic)
	}
	next := s3PacketFromCurrentRuntime(t, root)
	context := s3DeliveredContext(t, out.String())
	for _, want := range []string{next.ViewPath, next.AnswerPath, "Conventional output candidates", "CONTRACTS-template.md", "SYNC-contract-template.md", "bound development branch"} {
		if !strings.Contains(context, want) {
			t.Errorf("S2 save stdout lacks %q: %s (stderr %s)", want, &out, &diagnostic)
		}
	}
}

func TestS3AcceptedTransitionKeepsS4PreparationFailureAcrossSession(t *testing.T) {
	root := newS3ReworkFixture(t)
	packet := prepareS3ThroughSourceCommit(t, root)
	index, contract, _ := writeS3SharedModelBatch(t, root, true, false)
	stageJSON(t, root, packet.AnswerPath, s3AnswerDraft(packet, index, contract))
	if err := os.Remove(filepath.Join(root, "docs/dev/tasks/TASK-template.md")); err != nil {
		t.Fatal(err)
	}
	fixtures.CommitCurrentFixture(t, root)
	output, diagnostic := saveS3ThroughPostToolUse(t, root, packet.AnswerPath)
	output = s3DeliveredContext(t, output)
	state := stageSnapshot(t, root)
	if life := state.State["lifecycle"].(map[string]any); life["phase"] != "tasks" {
		t.Fatalf("S3 transition did not commit: %v %s %s", life, output, diagnostic)
	}
	for _, want := range []string{"S3 SUBMISSION RECEIPT", "preparation failed", "TASK-template.md", "runtime stage-answer issue"} {
		if !strings.Contains(output, want) {
			t.Errorf("save lacks %q: %s (stderr %s)", want, output, diagnostic)
		}
	}
	resumed := s3DeliveredContext(t, designGuidanceEvent(t, root, "s4-prepare-failed", "SessionStart", "Read"))
	for _, want := range []string{"TASK-template.md", "runtime stage-answer issue", "Do not guess a path or resubmit"} {
		if !strings.Contains(resumed, want) {
			t.Errorf("SessionStart lost actual preparation failure %q: %s", want, resumed)
		}
	}
}

func s3DeliveredContext(t *testing.T, output string) string {
	t.Helper()
	var envelope struct {
		Specific struct {
			Context    string `json:"additionalContext"`
			Permission string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("invalid Hook response: %v %s", err, output)
	}
	if envelope.Specific.Permission != "" {
		t.Fatalf("guidance changed ordinary tool permission: %s", output)
	}
	if envelope.Specific.Context == "" {
		t.Fatalf("Hook did not deliver model-readable context: %s", output)
	}
	return envelope.Specific.Context
}

func TestIssuedPlanningPublicGuidanceCachePreservesChangesAndRecovery(t *testing.T) {
	for _, stage := range []string{"S3", "S4"} {
		t.Run(stage, func(t *testing.T) {
			var root, viewPath, answerPath, changedPath string
			var changeOutput func()
			if stage == "S3" {
				var packet workpacket.ContractPacket
				root, packet = newContractAnswerFixture(t)
				viewPath, answerPath = packet.ViewPath, packet.AnswerPath
				changedPath = "docs/dev/contracts/CONTRACTS-039-new-output.md"
				changeOutput = func() {
					draft := completedContractDraft(packet)
					draft.Answer.IndexPath = &changedPath
					stageJSON(t, root, answerPath, draft)
				}
			} else {
				var packet workpacket.TaskPlanningPacket
				root, packet = newTaskPlanningAnswerFixture(t)
				viewPath, answerPath = packet.ViewPath, packet.AnswerPath
				changedPath = "docs/dev/tasks/TASK-039-02.md"
				changeOutput = func() {
					draft := completedTaskPlanningDraft(packet)
					draft.Answer.TaskPaths = []string{changedPath}
					stageJSON(t, root, answerPath, draft)
				}
			}
			statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
			before, _ := os.ReadFile(statePath)
			journal, _ := os.ReadFile(journalPath)
			observe := func(event, tool string) string {
				return s3DeliveredContext(t, designGuidanceEvent(t, root, "issued-"+stage, event, tool))
			}
			first := observe("PreToolUse", "Read")
			if !strings.Contains(first, answerPath) {
				t.Fatalf("first delivery lost original answer: %s", first)
			}
			for _, tool := range []string{"Read", "Write", "Bash", "mcp__sandbox__execute"} {
				repeated := observe("PreToolUse", tool)
				if !strings.Contains(repeated, "guidance is unchanged") || len(repeated) >= len(first)/2 {
					t.Fatalf("%s repeated full responsibility: first=%d repeated=%d %s", tool, len(first), len(repeated), repeated)
				}
			}
			changeOutput()
			changed := observe("PreToolUse", "Read")
			if strings.Contains(changed, "guidance is unchanged") || !strings.Contains(changed, changedPath) {
				t.Fatalf("changed output path hidden: %s", changed)
			}
			if repeated := observe("PreToolUse", "Read"); !strings.Contains(repeated, "guidance is unchanged") {
				t.Fatalf("stable changed responsibility did not deduplicate: %s", repeated)
			}
			originalView, err := os.ReadFile(filepath.Join(root, viewPath))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(root, viewPath)); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				broken := observe("PreToolUse", "Bash")
				if strings.Contains(broken, "guidance is unchanged") || !strings.Contains(broken, "Current material missing") || !strings.Contains(broken, viewPath) {
					t.Fatalf("real missing-file error compressed: %s", broken)
				}
			}
			if err := os.WriteFile(filepath.Join(root, viewPath), originalView, 0600); err != nil {
				t.Fatal(err)
			}
			restored := observe("PreToolUse", "Read")
			if strings.Contains(restored, "guidance is unchanged") || !strings.Contains(restored, changedPath) {
				t.Fatalf("resolved material error did not restore full current responsibility: %s", restored)
			}
			resumed := observe("SessionStart", "Read")
			if strings.Contains(resumed, "guidance is unchanged") || !strings.Contains(resumed, answerPath) || !strings.Contains(resumed, changedPath) {
				t.Fatalf("SessionStart lost full current responsibility: %s", resumed)
			}
			next := observe("PreToolUse", "Read")
			if strings.Contains(next, "guidance is unchanged") || !strings.Contains(next, changedPath) {
				t.Fatalf("SessionStart did not reset presentation cache: %s", next)
			}
			if got, _ := os.ReadFile(statePath); !bytes.Equal(got, before) {
				t.Fatal("ordinary observations changed Runtime")
			}
			if got, _ := os.ReadFile(journalPath); !bytes.Equal(got, journal) {
				t.Fatal("ordinary observations changed journal")
			}
		})
	}
}
