package controller_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// This fixture starts at a durable domain reception. Authoring and issuance
// are tested separately; these tests exercise the real gates and Writer seam.
func submissionControlFixture(t *testing.T, ready bool) (string, string, *fileview.View) {
	t.Helper()
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "document_verification", "", 0)
	req039fixtures.SeedDocumentPassS5(t, root, state, "dv-spec", "dv-task")
	if !ready {
		state["evidence"] = state["evidence"].([]any)[:1]
	}
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	refs := []workpacket.Reference{}
	for _, raw := range state["documents"].([]any) {
		doc := raw.(map[string]any)
		refs = append(refs, workpacket.Reference{ID: doc["id"].(string), Path: doc["path"].(string), Version: doc["version"].(string), SHA256: doc["sha256"].(string)})
	}
	packet, err := workpacket.BuildS5(workpacket.Binding{RuntimeID: state["runtime_id"].(string), Generation: 1, StageEntry: 0, Owner: "dv-spec", AssignmentID: "assignment-spec", AssignmentSHA: strings.Repeat("a", 64), Responsibility: "DV-SPEC-CONSISTENCY", Attempt: 1}, refs)
	if err != nil {
		t.Fatal(err)
	}
	packetData, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var packetValue map[string]any
	if err := json.Unmarshal(packetData, &packetValue); err != nil {
		t.Fatal(err)
	}
	key := workpacket.Digest([]byte("submission:" + packet.ID))
	event := workpacket.Digest([]byte(key + ":after-domain"))
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = []any{packetValue}
	entities["submissions"] = []any{map[string]any{
		"submission_key": key, "packet_id": packet.ID, "owner": "dv-spec", "content_sha256": strings.Repeat("b", 64), "evidence_id": "ev-dv-spec", "followup_event_id": event, "control_status": "pending", "origin_state": "document_verification", "origin_phase": "", "origin_entry": 0,
	}}
	// This test starts after domain reception. Both independent reviews must
	// have current received packets; naked PASS envelopes cannot replace them.
	taskPacket, err := workpacket.BuildS5(workpacket.Binding{RuntimeID: state["runtime_id"].(string), Generation: 1, StageEntry: 0, Owner: "dv-task", AssignmentID: "assignment-task", AssignmentSHA: strings.Repeat("c", 64), Responsibility: "DV-TASK-EXECUTABILITY", Attempt: 1}, refs)
	if err != nil {
		t.Fatal(err)
	}
	taskData, err := json.Marshal(taskPacket)
	if err != nil {
		t.Fatal(err)
	}
	var taskRow map[string]any
	if err := json.Unmarshal(taskData, &taskRow); err != nil {
		t.Fatal(err)
	}
	entities["work_packets"] = append(entities["work_packets"].([]any), taskRow)
	entities["submissions"] = append(entities["submissions"].([]any), map[string]any{
		"submission_key": workpacket.Digest([]byte("submission:" + taskPacket.ID)), "packet_id": taskPacket.ID,
		"owner": "dv-task", "content_sha256": strings.Repeat("d", 64), "evidence_id": "ev-dv-task",
		"followup_event_id": workpacket.Digest([]byte(workpacket.Digest([]byte("submission:"+taskPacket.ID)) + ":after-domain")),
		"control_status":    "no_transition", "origin_state": "document_verification", "origin_phase": "", "origin_entry": 0,
	})
	// Existing Runtime responsibility metadata; no platform Team is launched.
	entities["teams"] = append(entities["teams"].([]any), map[string]any{
		"id": "document-review-current", "platform_team_id": "native-review", "kind": "document_verifier", "status": "active",
		"manifest_ref": ".claude/teams/document-review.json", "review_round": nil,
		"packet_generation": 1, "packet_stage_entry": 0,
		"agent_ids": []string{"dv-spec", "dv-task"}, "responsibility_ids": []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"},
	})
	for _, id := range []string{"ev-dv-spec", "ev-dv-task"} {
		if (ready || id == "ev-dv-spec") && !runtime.CurrentDocumentReviewEvidence(state, id) {
			t.Fatalf("fixture did not establish its claimed current S5 reception: %s", id)
		}
	}
	req039fixtures.WriteState(t, root, state)
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := fileview.ValidateAuthority(root, state); err != nil {
		t.Fatal(err)
	}
	devBranch, err := fileview.DevelopmentRef(state)
	if err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, "refs/heads/"+strings.TrimPrefix(devBranch, "refs/heads/"), catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	return root, event, view
}

func TestSubmissionFollowupCommitsTransitionAndResultTogether(t *testing.T) {
	root, event, view := submissionControlFixture(t, true)
	req := controller.ControlRequest{Root: root, CWD: root, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: event, Files: view}
	first, err := controller.RunControlCycle(context.Background(), req)
	if err != nil || first.Error != "" || !first.QualityGate.TransitionCommitted {
		t.Fatalf("S5 followup failed: %+v error=%s %v", first.QualityGate, first.Error, err)
	}
	record, err := runtime.SubmissionFollowup(first.Snapshot.State, event)
	if record.ControlStatus != "advanced" || record.TransitionEventID == "" || record.TransitionEventID != first.Snapshot.State["last_transition"].(map[string]any)["event_id"] {
		t.Fatalf("transition and consumption were not one commit: %+v %v", record, err)
	}
	replay, err := controller.RunControlCycle(context.Background(), req)
	if err != nil || replay.Error != "" || replay.QualityGate.TransitionCommitted || replay.Snapshot.Revision != first.Snapshot.Revision {
		t.Fatalf("response-loss replay repeated control: %+v %v", replay.QualityGate, err)
	}
	if replay.Snapshot.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatal("old followup reached another cursor")
	}
}

func TestConcurrentSubmissionFollowupTransitionsAtMostOnce(t *testing.T) {
	root, event, view := submissionControlFixture(t, true)
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	evaluator := &submissionBarrierEvaluator{engine: qualitygate.NewEvaluator(registry), ready: make(chan struct{})}
	results := make([]controller.ControlResult, 2)
	errs := make([]error, 2)
	var group sync.WaitGroup
	for i := range 2 {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			results[i], errs[i] = controller.RunControlCycle(context.Background(), controller.ControlRequest{Root: root, CWD: root, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: event, Files: view, GateEvaluator: evaluator})
		}(i)
	}
	group.Wait()
	committed := 0
	for i, result := range results {
		if errs[i] != nil || result.Error != "" {
			t.Fatalf("concurrent followup: %v %s", errs[i], result.Error)
		}
		if result.QualityGate.TransitionCommitted {
			committed++
		}
	}
	if committed != 1 {
		t.Fatalf("%d transitions for one event", committed)
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	record, _ := runtime.SubmissionFollowup(snapshot.State, event)
	if record.ControlStatus != "advanced" {
		t.Fatal("lost committed followup")
	}
}

// Both requests must finish the original PASS evaluation before either
// reaches Apply, so this tests the locked duplicate check rather than timing.
type submissionBarrierEvaluator struct {
	engine qualitygate.Evaluator
	mutex  sync.Mutex
	reads  int
	ready  chan struct{}
}

func (e *submissionBarrierEvaluator) Evaluate(ctx context.Context, input qualitygate.Input) (qualitygate.Evaluation, error) {
	result, err := e.engine.Evaluate(ctx, input)
	if input.GateID == "GATE-DOCUMENT-PASS" {
		e.mutex.Lock()
		e.reads++
		if e.reads == 2 {
			close(e.ready)
		}
		e.mutex.Unlock()
		select {
		case <-e.ready:
		case <-ctx.Done():
			return qualitygate.Evaluation{}, ctx.Err()
		}
	}
	return result, err
}

func TestSubmissionFollowupNotReadyIsConsumedButUnknownRemainsPending(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "not ready", true: "unknown"}[unknown], func(t *testing.T) {
			root, event, view := submissionControlFixture(t, false)
			req := controller.ControlRequest{Root: root, CWD: root, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: event, Files: view}
			if unknown {
				req.GateEvaluator = dualSatisfiedEvaluator{gates: map[string]qualitygate.Status{"GATE-DOCUMENT-PASS": qualitygate.StatusUnknown, "GATE-DOCUMENT-FIX-REQUIRED": qualitygate.StatusUnknown}}
			}
			result, err := controller.RunControlCycle(context.Background(), req)
			if err != nil || result.QualityGate.TransitionCommitted {
				t.Fatalf("unexpected control result: %+v %v", result.QualityGate, err)
			}
			record, _ := runtime.SubmissionFollowup(result.Snapshot.State, event)
			want := "no_transition"
			if unknown {
				want = "pending"
			}
			if record.ControlStatus != want {
				t.Fatalf("status %s want %s", record.ControlStatus, want)
			}
		})
	}
}
