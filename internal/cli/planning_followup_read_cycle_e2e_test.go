package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
	// A received S2 answer leaves one pending followup. An ordinary Pre belongs
	// to no lifecycle boundary and must not resume it; Main Stop is a boundary
	// and continues the original followup without another author save.
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
		// An ordinary Pre belongs to no lifecycle boundary, so it never resumes
		// the received pending followup. Main Stop is a lifecycle boundary and
		// continues the original S2 followup, so it is not a no-op there.
		events := []string{"PreToolUse"}
		if fromPhase != "design" {
			events = append(events, "Stop")
		}
		for _, event := range events {
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
			record, found, err := runtime.FindSubmission(after.State, packetID)
			if err != nil || !found || record.ControlStatus != "pending" || record.TransitionEventID != "" {
				t.Fatalf("ordinary %s resumed the pending followup: %+v found=%t err=%v", event, record, found, err)
			}
		}
	}
	var receipt submission.Receipt
	switch fromPhase {
	case "design":
		// The author's answer is already received. Main Stop is a lifecycle
		// boundary, so it continues the original S2 followup and completes
		// PTR-PLAN-01 without another author save.
		body, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "tool_name": "Read", "cwd": s.Root})
		var out, stderr bytes.Buffer
		if code := cli.Run([]string{"hook", "--event", "Stop", "--root", s.Root}, bytes.NewReader(body), &out, &stderr); code != 0 {
			t.Fatalf("Main Stop continuation: %s %s", out.String(), stderr.String())
		}
		continued := stageSnapshot(t, s.Root)
		if continued.Revision <= before.Revision {
			t.Fatalf("Main Stop did not advance the received pending followup: before=%d after=%d", before.Revision, continued.Revision)
		}
		record, found, err := runtime.FindSubmission(continued.State, packetID)
		if err != nil || !found || record.ControlStatus != "advanced" || record.TransitionEventID == "" {
			t.Fatalf("Main Stop did not stamp the original followup advanced: %+v found=%t err=%v", record, found, err)
		}
		if record.Key != prior.Key || record.EvidenceID != prior.EvidenceID {
			t.Fatalf("Main Stop replaced the original reception: %+v", record)
		}
		if planningEvidenceCount(continued.State) != planningEvidenceCount(before.State) {
			t.Fatal("Main Stop continuation received the answer a second time")
		}
		if count := planningTransitionEventCount(t, s.Root, transitionID); count != 1 {
			t.Fatalf("Main Stop recorded %d %s transitions, want exactly one", count, transitionID)
		}
		receipt, err = s.DesignReceipt("", packetID)
		if err != nil {
			t.Fatal(err)
		}
		if !receipt.WorkComplete {
			t.Fatalf("Main Stop left the original S2 outlet pending: %+v", receipt)
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

func planningEvidenceCount(state map[string]any) int {
	rows, _ := state["evidence"].([]any)
	return len(rows)
}

func planningTransitionEventCount(t *testing.T, root, transitionID string) int {
	t.Helper()
	file, err := os.Open(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event["transition_id"] == transitionID && event["event"] == "transition_committed" && event["outcome"] == "committed" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return count
}

// assertPlanningTransitionJournalEvent pins the canonical identity of one
// committed transition event.
//
// The identity names the responsibility and the intent, and it never carries
// the Runtime write count. The intent is the length-prefixed encoding of the
// baseline generation, the cursor the transition moves from, the lifecycle
// stage entry it starts at and the responsibility's own occurrence anchor —
// the transition that last committed inside this responsibility. A rework that
// re-enters the same exit therefore keeps its own identity, while a replay of
// the same request keeps one identity and is still deduplicated by the
// journal's global event-id uniqueness check.
func assertPlanningTransitionJournalEvent(t *testing.T, root, eventID, transitionID, fromPhase string) {
	t.Helper()
	file, err := os.Open(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	committed := []string{}
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event["event_id"] != eventID {
			if event["event"] == "transition_committed" && event["outcome"] == "committed" {
				if id, _ := event["transition_id"].(string); id != "" {
					committed = append(committed, id)
				}
			}
			continue
		}
		assertCanonicalTransitionIdentity(t, event, eventID, transitionID, fromPhase, committed)
		return
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("journal does not contain followup's exact transition event %q", eventID)
}

// assertCanonicalTransitionIdentity checks the recorded identity of one
// committed transition against the canonical shape the production code writes.
//
// The assertions it replaces pinned the Runtime write count:
// `runtime:<T>:<beforeRevision>` and `evt-<T>-r<beforeRevision+1>` proved only
// that the identity carried the write count, and they accepted any identity
// that happened to embed it. The canonical identity is instead a pure function
// of the domain coordinates, so every segment is pinned here: the baseline
// generation, the cursor the transition moved from, the lifecycle stage entry
// and the responsibility's occurrence anchor. The write count is absent from
// the identity by construction, and the assertions below prove that: only the
// generation and the stage entry are numbers, and the whole identity is
// re-derived from the coordinates the event itself records.
func assertCanonicalTransitionIdentity(t *testing.T, event map[string]any, eventID, transitionID, fromPhase string, committed []string) {
	t.Helper()
	from, _ := event["from"].(map[string]any)
	beforeRevision, ok := event["before_revision"].(float64)
	if event["transition_id"] != transitionID || from["state"] != "planning" || from["phase"] != fromPhase || !ok {
		t.Fatalf("followup points to a different original transition event: %+v", event)
	}
	generation, ok := event["baseline_generation"].(float64)
	if !ok {
		t.Fatalf("transition event does not record the baseline generation: %+v", event)
	}
	domain := strings.ToLower(transitionID)
	prefix := "evt-" + domain + "-"
	if !strings.HasPrefix(eventID, prefix) {
		t.Fatalf("transition event identity does not carry the canonical domain prefix %q: %+v", prefix, event)
	}
	intent := strings.TrimPrefix(eventID, prefix)
	intentParts, ok := decodeLengthPrefixedKey(intent)
	if !ok || len(intentParts) != 5 {
		t.Fatalf("transition event intent is not the canonical length-prefixed key: %q", intent)
	}
	if intentParts[0] != strconv.Itoa(int(generation)) {
		t.Fatalf("transition event intent does not name the baseline generation %d: %q", int(generation), intentParts[0])
	}
	if intentParts[1] != from["state"] || intentParts[2] != from["phase"] {
		t.Fatalf("transition event intent does not name the original cursor: %+v", intentParts)
	}
	if !strings.Contains(intent, fmt.Sprintf("%d:%s", len(from["state"].(string)), from["state"].(string))) {
		t.Fatalf("transition event intent does not carry the length-prefixed from state: %q", intent)
	}
	if !strings.Contains(intent, fmt.Sprintf("%d:%s", len(fromPhase), fromPhase)) {
		t.Fatalf("transition event intent does not carry the length-prefixed from phase: %q", intent)
	}
	stageEntry, err := strconv.Atoi(intentParts[3])
	if err != nil || stageEntry < 0 {
		t.Fatalf("transition event intent does not name the lifecycle stage entry: %q", intentParts[3])
	}
	anchor := intentParts[4]
	if anchor == "" {
		t.Fatalf("transition event intent does not name the responsibility's occurrence anchor: %q", intent)
	}
	if _, err := strconv.Atoi(anchor); err == nil {
		t.Fatalf("transition event occurrence anchor is a bare number, not a domain identity: %q", anchor)
	}
	if anchor != "none" && !slices.Contains(committed, anchor) {
		t.Fatalf("transition event occurrence anchor %q does not name a transition that committed earlier in this responsibility", anchor)
	}
	// Only the generation and the stage entry are numbers, and both are domain
	// coordinates. No other numeric segment — a Runtime write count above all
	// — can be part of this intent.
	numbers := 0
	for _, part := range intentParts {
		if _, err := strconv.Atoi(part); err == nil {
			numbers++
		}
	}
	if numbers != 2 {
		t.Fatalf("transition event intent carries numeric segments beyond the two domain coordinates: %+v", intentParts)
	}
	legacy := regexp.MustCompile(`-r\d+$`)
	if legacy.MatchString(eventID) {
		t.Fatalf("transition event identity still carries the Runtime write count: %+v", event)
	}
	// The exact identities the retired write-counter shape produced must not
	// come back under any revision.
	if eventID == fmt.Sprintf("evt-%s-r%d", domain, int(beforeRevision)+1) {
		t.Fatalf("transition event identity fell back to the retired write-count shape: %+v", event)
	}
	// The idempotency key names the responsibility coordinate — the runtime and
	// the baseline generation — and exactly the same intent.
	key, _ := event["idempotency_key"].(string)
	keyPrefix := "runtime:" + domain + ":"
	if !strings.HasPrefix(key, keyPrefix) {
		t.Fatalf("transition event does not carry the canonical idempotency key prefix %q: %+v", keyPrefix, event)
	}
	keyParts, ok := decodeLengthPrefixedKey(strings.TrimPrefix(key, keyPrefix))
	if !ok || len(keyParts) != 7 {
		t.Fatalf("transition event idempotency key is not the canonical responsibility and intent key: %q", key)
	}
	if keyParts[0] != event["runtime_id"] {
		t.Fatalf("transition event idempotency key does not name the runtime: %+v", event)
	}
	if keyParts[1] != intentParts[0] {
		t.Fatalf("transition event idempotency key does not name the baseline generation: %+v", event)
	}
	if !slices.Equal(keyParts[2:], intentParts) {
		t.Fatalf("transition event idempotency key does not carry the event id's intent: %+v", event)
	}
	if legacy.MatchString(key) {
		t.Fatalf("transition event idempotency key still carries the Runtime write count: %+v", event)
	}
	if key == fmt.Sprintf("runtime:%s:%d", transitionID, int(beforeRevision)) {
		t.Fatalf("transition event idempotency key fell back to the retired write-count shape: %+v", event)
	}
}

// decodeLengthPrefixedKey decodes the injective "<len>:<part>" encoding the
// production code uses, so a test reads the identity's own coordinates instead
// of trusting one opaque string. The parts are concatenated; a colon that
// follows a part separates two encoded groups (the responsibility coordinate
// and the intent).
func decodeLengthPrefixedKey(encoded string) ([]string, bool) {
	parts := []string{}
	for encoded != "" {
		if encoded[0] == ':' {
			encoded = encoded[1:]
			continue
		}
		colon := strings.Index(encoded, ":")
		if colon < 1 {
			return nil, false
		}
		length, err := strconv.Atoi(encoded[:colon])
		if err != nil || length < 0 {
			return nil, false
		}
		encoded = encoded[colon+1:]
		if len(encoded) < length {
			return nil, false
		}
		parts = append(parts, encoded[:length])
		encoded = encoded[length:]
	}
	return parts, true
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
