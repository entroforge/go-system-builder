package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestMainPlanningReadCycleConsumesAcceptedS2Followup(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, packet.ID)
	defer cancel()
	receipt, err := s.SaveDesign(ctx, packet.AnswerPath)
	if err != nil || !receipt.Accepted || receipt.WorkComplete {
		t.Fatalf("accept S2 before ordinary cycle: %+v %v", receipt, err)
	}
	assertReadCycleConsumesPlanningFollowup(t, s, packet.ID, "PTR-PLAN-01", "design", "contracts")
}

func TestMainPlanningReadCycleConsumesAcceptedS3Followup(t *testing.T) {
	root, packet := newContractAnswerFixture(t)
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, packet.ID)
	defer cancel()
	receipt, err := s.SaveContracts(ctx, packet.AnswerPath)
	if err != nil || !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("accept S3 before ordinary cycle: %+v %v", receipt, err)
	}
	assertReadCycleConsumesPlanningFollowup(t, s, packet.ID, "PTR-PLAN-02", "contracts", "tasks")
}

func TestMainPlanningReadCycleConsumesAcceptedS4FollowupAndClosesS5Inputs(t *testing.T) {
	root, packet := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, packet.AnswerPath, completedTaskPlanningDraft(packet))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, packet.ID)
	defer cancel()
	receipt, err := s.SaveTaskPlanning(ctx, packet.AnswerPath)
	if err != nil || !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("accept S4 before ordinary cycle: %+v %v", receipt, err)
	}
	snapshot := assertReadCycleConsumesPlanningFollowup(t, s, packet.ID, "TR-002", "tasks", "")
	formal, err := runtime.S5FormalInputs(root, snapshot.State, s.Files)
	if err != nil || len(formal.References) == 0 {
		t.Fatalf("accepted S4 closure is not consumable by S5: references=%d err=%v", len(formal.References), err)
	}
}

func pendingMainFollowupContext(s *submission.Service, packetID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	// Interrupt after the original receipt is durable. Candidate validation
	// runs before commit and correctly cannot manufacture an accepted answer.
	return &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: packetID, cancel: cancel}, cancel
}

func assertReadCycleConsumesPlanningFollowup(t *testing.T, s *submission.Service, packetID, transitionID, fromPhase, toPhase string) runtime.Snapshot {
	t.Helper()
	before := stageSnapshot(t, s.Root)
	prior, found, err := runtime.FindSubmission(before.State, packetID)
	if err != nil || !found || prior.ControlStatus != "pending" || prior.TransitionEventID != "" {
		t.Fatalf("typed Main answer did not leave one pending followup: %+v found=%t err=%v", prior, found, err)
	}

	{
		journalBefore, err := os.ReadFile(filepath.Join(s.Root, ".claude/loop-events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range []string{"PreToolUse", "Stop"} {
			body, _ := json.Marshal(map[string]any{"hook_event_name": event, "tool_name": "Read", "cwd": s.Root})
			var out, stderr bytes.Buffer
			if code := cli.Run([]string{"hook", "--event", event, "--root", s.Root}, bytes.NewReader(body), &out, &stderr); code != 0 {
				t.Fatalf("ordinary %s: %s %s", event, out.String(), stderr.String())
			}
			after := stageSnapshot(t, s.Root)
			journalAfter, err := os.ReadFile(filepath.Join(s.Root, ".claude/loop-events.jsonl"))
			if err != nil || after.Revision != before.Revision || !bytes.Equal(journalBefore, journalAfter) {
				t.Fatalf("ordinary %s changed pending work: before=%d after=%d err=%v", event, before.Revision, after.Revision, err)
			}
		}
	}
	var receipt submission.Receipt
	switch fromPhase {
	case "design":
		packet, found, err := runtime.FindDesignPacket(before.State, packetID)
		if err != nil || !found {
			t.Fatalf("design packet: %v", err)
		}
		receipt, err = s.SaveDesign(context.Background(), packet.AnswerPath)
		if err != nil {
			t.Fatal(err)
		}
	case "contracts":
		packet, found, err := runtime.FindContractPacket(before.State, packetID)
		if err != nil || !found {
			t.Fatalf("contract packet: %v", err)
		}
		receipt, err = s.SaveContracts(context.Background(), packet.AnswerPath)
		if err != nil {
			t.Fatal(err)
		}
	case "tasks":
		// The answer and original reception already exist. A successful Post
		// Read continues that followup without another edit or Save command.
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Read", "tool_input": map[string]any{"file_path": "docs/project-map.md"}, "cwd": s.Root})
		var out, stderr bytes.Buffer
		if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", s.Root}, bytes.NewReader(body), &out, &stderr); code != 0 {
			t.Fatalf("pending PostRead: %s %s", out.String(), stderr.String())
		}
		var hook struct {
			Output struct {
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		for _, line := range bytes.Split(out.Bytes(), []byte("\n")) {
			if json.Unmarshal(line, &hook) == nil && strings.HasPrefix(hook.Output.Context, "S4 SUBMISSION RECEIPT: ") {
				if err := json.Unmarshal([]byte(strings.TrimPrefix(hook.Output.Context, "S4 SUBMISSION RECEIPT: ")), &receipt); err != nil {
					t.Fatal(err)
				}
				break
			}
		}

	}
	if !receipt.Accepted {
		t.Fatalf("same saved report lost acceptance: %+v", receipt)
	}
	snapshot := stageSnapshot(t, s.Root)

	record, found, err := runtime.FindSubmission(snapshot.State, packetID)
	if err != nil || !found || record.ControlStatus != "advanced" || record.TransitionEventID == "" {
		t.Fatalf("original Main followup was not stamped advanced with the transition: %+v found=%t err=%v", record, found, err)
	}
	lifecycle := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "planning" && transitionID != "TR-002" {
		t.Fatalf("planning route state = %v", lifecycle)
	}
	if transitionID == "TR-002" {
		if lifecycle["state"] != "document_verification" || lifecycle["phase"] != nil {
			t.Fatalf("TR-002 cursor = %+v", lifecycle)
		}
	} else if lifecycle["phase"] != toPhase {
		t.Fatalf("%s destination phase = %v, want %s", transitionID, lifecycle["phase"], toPhase)
	}
	assertPlanningTransitionJournalEvent(t, s.Root, record.TransitionEventID, transitionID, fromPhase)
	if fromPhase == "tasks" {
		journalBefore, err := os.ReadFile(filepath.Join(s.Root, ".claude/loop-events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Read", "cwd": s.Root})
		var out, stderr bytes.Buffer
		if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", s.Root}, bytes.NewReader(body), &out, &stderr); code != 0 {
			t.Fatalf("advanced PostRead: %s %s", out.String(), stderr.String())
		}
		after := stageSnapshot(t, s.Root)
		journalAfter, err := os.ReadFile(filepath.Join(s.Root, ".claude/loop-events.jsonl"))
		if err != nil || after.Revision != snapshot.Revision || !bytes.Equal(journalBefore, journalAfter) {
			t.Fatalf("advanced PostRead repeated business write: %v", err)
		}
	}
	return snapshot
}

func assertPlanningTransitionJournalEvent(t *testing.T, root, eventID, transitionID, fromPhase string) {
	t.Helper()
	file, err := os.Open(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event["event_id"] != eventID {
			continue
		}
		from, _ := event["from"].(map[string]any)
		beforeRevision, ok := event["before_revision"].(float64)
		if event["transition_id"] != transitionID || from["state"] != "planning" || from["phase"] != fromPhase || !ok {
			t.Fatalf("followup points to a different original transition event: %+v", event)
		}
		if event["idempotency_key"] != fmt.Sprintf("runtime:%s:%d", transitionID, int(beforeRevision)) {
			t.Fatalf("transition event has noncanonical original idempotency key: %+v", event)
		}
		if event["event_id"] != fmt.Sprintf("evt-%s-r%d", strings.ToLower(transitionID), int(beforeRevision)+1) {
			t.Fatalf("transition event identity does not match original revision: %+v", event)
		}
		return
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("journal does not contain followup's exact transition event %q", eventID)
}

func TestTaskPlanningPostReadDoesNotSubmitUnreceivedAnswer(t *testing.T) {
	root, packet := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, packet.AnswerPath, completedTaskPlanningDraft(packet))
	before := stageSnapshot(t, root)
	journalBefore, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Read", "cwd": root})
	var out, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(body), &out, &stderr); code != 0 {
		t.Fatalf("unreceived PostRead: %s %s", out.String(), stderr.String())
	}
	after := stageSnapshot(t, root)
	journalAfter, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil || after.Revision != before.Revision || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatalf("PostRead promoted unreceived content: %v", err)
	}
	if _, found, err := runtime.FindSubmission(after.State, packet.ID); err != nil || found {
		t.Fatalf("Read created a new submission: found=%v err=%v", found, err)
	}
}
