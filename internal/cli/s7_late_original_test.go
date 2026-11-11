package cli_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// The new-Plan installation is the fixture boundary: this deliberately does
// not claim to execute the S8/S9 professional work or TR-012. Both old and new
// issuances, natural events, Result writes and Main receipt queries are real.
func lateS7NextPlan(t *testing.T, root string, differentOwner ...bool) []workpacket.ReviewResultPacket {
	t.Helper()
	old := stageSnapshot(t, root)
	oldReview := old.State["review"].(map[string]any)
	ptr := oldReview["plan"].(map[string]any)
	data, err := os.ReadFile(filepath.Join(root, ptr["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	var plan review.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	plan.ReviewPlanID += "-next"
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	planRef := ".claude/late-fixture-next-plan.json"
	stageJSON(t, root, planRef, plan)
	// stageJSON uses indentation, so pin the actual original file bytes.
	data, err = os.ReadFile(filepath.Join(root, planRef))
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for id, raw := range oldReview["assignments"].(map[string]any) {
		row := raw.(map[string]any)
		owners[id], _ = row["agent_id"].(string)
	}
	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
	life := old.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	_, err = writer.Update(old.Revision, runtime.Mutation{EventID: "evt-late-fixture-new-plan", TransitionID: "LATE-FIXTURE", Event: "late_fixture_new_plan", Actor: "orchestrator", IdempotencyKey: "late-fixture:new-plan", RuntimeID: old.State["runtime_id"].(string), BaselineGeneration: plan.BaselineGeneration, From: cursor, To: map[string]any{"state": "verification", "phase": "running"}, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		if err := review.ApplyRegisteredPlanProjection(state, plan, planRef, "", time.Now().UTC()); err != nil {
			return err
		}
		rev, _ := state["review"].(map[string]any)
		entry, _ := rev["round_entry"].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
		}
		entry["plan_id"] = plan.ReviewPlanID
		rev["round_entry"] = entry
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(differentOwner) > 0 && differentOwner[0] {
		for id, owner := range owners {
			if owner != "" {
				owners[id] = owner + "-next"
			}
		}
	}
	next := stageSnapshot(t, root)
	entities := next.State["entities"].(map[string]any)
	refs := map[string]string{}
	for _, raw := range entities["teams"].([]any) {
		team := raw.(map[string]any)
		oldRef, ok := team["manifest_ref"].(string)
		if !ok {
			continue
		}
		manifestData, err := os.ReadFile(filepath.Join(root, oldRef))
		if err != nil {
			t.Fatal(err)
		}
		var manifest map[string]any
		if err = json.Unmarshal(manifestData, &manifest); err != nil {
			t.Fatal(err)
		}
		if len(differentOwner) > 0 && differentOwner[0] {
			var ids []any
			for _, raw := range manifest["assignments"].([]any) {
				a := raw.(map[string]any)
				id := a["assignment_id"].(string)
				if owners[id] != "" {
					a["agent_id"] = owners[id]
					ids = append(ids, owners[id])
				}
			}
			team["agent_ids"] = ids
		}

		newRef := oldRef + "-next.json"
		stageJSON(t, root, newRef, manifest)
		manifestData, _ = os.ReadFile(filepath.Join(root, newRef))
		team["manifest_ref"] = newRef
		team["manifest_sha256"] = fmt.Sprintf("%x", sha256.Sum256(manifestData))
		refs[oldRef] = newRef
	}
	if len(differentOwner) > 0 && differentOwner[0] {
		agents := entities["agents"].([]any)
		original := append([]any(nil), agents...)
		for _, raw := range original {
			a := raw.(map[string]any)
			for _, owner := range owners {
				if owner == a["id"].(string)+"-next" {
					cloned := map[string]any{}
					for k, v := range a {
						cloned[k] = v
					}
					cloned["id"] = owner
					agents = append(agents, cloned)
				}
			}
		}
		entities["agents"] = agents
	}
	for _, raw := range entities["agents"].([]any) {
		agent := raw.(map[string]any)
		ref, _ := agent["prompt_ref"].(string)
		for oldRef, newRef := range refs {
			if strings.HasPrefix(ref, oldRef+"#") {
				agent["prompt_ref"] = newRef + strings.TrimPrefix(ref, oldRef)
			}
		}
		agent["dispatch_mode"] = "plan_checkpoint"
		if err := workstate.RecordAgentActivity(next.State, agent, "reading", "workgroup_registered"); err != nil {
			t.Fatal(err)
		}
	}
	for id, raw := range next.State["review"].(map[string]any)["assignments"].(map[string]any) {
		if owners[id] == "" {
			continue
		}
		row := raw.(map[string]any)
		row["agent_id"] = owners[id]
		if err := workstate.RecordReviewAssignmentActivity(next.State, row, "dispatched", "workgroup_registered"); err != nil {
			t.Fatal(err)
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", next.State)
	var packets []workpacket.ReviewResultPacket
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	if err := json.Unmarshal([]byte(issued), &packets); err != nil {
		t.Fatalf("new issuance: %v %s", err, issued)
	}
	return packets
}

func lateS7Observe(t *testing.T, root string, p workpacket.ReviewResultPacket) (string, string) {
	t.Helper()
	return s7ListenerEvent(t, root, "PostToolUse", s7ListenerPayload(t, "opaque-original-reviewer", "Review original "+filepath.Join(root, p.ViewPath)))
}

func TestS7LateOriginalSameAssignmentIDIsolatedAndDurable(t *testing.T) {
	for _, verdict := range []string{"pass", "finding", "blocked"} {
		t.Run(verdict, func(t *testing.T) {
			root, old := s7ListenerFixture(t)
			newer := lateS7NextPlan(t, root)
			if len(newer) != 1 || newer[0].Binding.AssignmentID != old.Binding.AssignmentID || newer[0].ID == old.ID {
				t.Fatalf("fixture did not reuse AssignmentID: %+v", newer)
			}
			before := stageSnapshot(t, root)
			wantReview, _ := json.Marshal(before.State["review"])
			if verdict == "finding" {
				minimalAnswerFinding(t, root, old, "finding-late-original")
			} else if verdict == "blocked" {
				old.Draft.Answer = json.RawMessage(`{"verdict":"blocked","deviations":["original environment unavailable"]}`)
				stageJSON(t, root, old.AnswerPath, old.Draft)
			} else {
				s7ListenerAnswer(t, root, old)
			}
			out, diagnostic := lateS7Observe(t, root, old)
			receipt := minimalAnswerReviewReceipt(t, root, old)
			if !receipt.Accepted || receipt.WorkComplete != (verdict != "blocked") {
				t.Fatalf("late receipt: %+v %s %s", receipt, out, diagnostic)
			}
			if got := minimalAnswerReviewReceipt(t, root, newer[0]); got.Accepted || got.WorkComplete {
				t.Fatalf("old receipt claimed new issuance: %+v", got)
			}
			after := stageSnapshot(t, root)
			gotReview, _ := json.Marshal(after.State["review"])
			if !reflect.DeepEqual(wantReview, gotReview) {
				t.Fatalf("late result changed new Plan projection: before=%s after=%s", wantReview, gotReview)
			}
			if after.State["lifecycle"].(map[string]any)["state"] != "verification" {
				t.Fatal("late result advanced new S7")
			}
			main := mainNativeReturn(t, root, filepath.Join(root, old.ViewPath))
			if !strings.Contains(main, `"accepted":true`) || !strings.Contains(main, "Main") {
				t.Fatalf("Main did not receive original handoff: %s", main)
			}
			if verdict == "finding" {
				if !strings.Contains(main, "原 S7 晚到") {
					t.Fatalf("Main lost late Finding disposition: %s", main)
				}
				if len(review.RoundFindings(after.State)) != 0 {
					t.Fatal("old Finding polluted new Plan")
				}
			}
			if err := os.Remove(filepath.Join(root, old.AnswerPath)); err != nil {
				t.Fatal(err)
			}
			prior := after.Revision
			lateS7Observe(t, root, old)
			if stageSnapshot(t, root).Revision != prior {
				t.Fatal("missing private original repeated acceptance")
			}
			got := minimalAnswerReviewReceipt(t, root, old)
			if !got.Accepted || got.WorkComplete != receipt.WorkComplete {
				t.Fatalf("lost private file revoked original receipt: %+v", got)
			}
		})
	}
}

func TestS7LateBlockedReceiptContinuesOriginalAndPreservesHistory(t *testing.T) {
	root, old := s7ListenerFixture(t)
	old.Draft.Answer = json.RawMessage(`{"verdict":"blocked","deviations":["original environment unavailable"]}`)
	stageJSON(t, root, old.AnswerPath, old.Draft)
	lateS7Observe(t, root, old)
	first := minimalAnswerReviewReceipt(t, root, old)
	if !first.Accepted || first.WorkComplete {
		t.Fatalf("first blocked receipt: %+v", first)
	}
	firstBytes, err := os.ReadFile(filepath.Join(root, first.DomainRefs[0]))
	if err != nil {
		t.Fatal(err)
	}
	// Exercise migration of an actual pre-upgrade receipt: remove only the new
	// projection; the original assignment receipt was written by real reception.
	snapshot := stageSnapshot(t, root)
	for _, raw := range snapshot.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["packet_id"] == old.ID {
			delete(row, "review_result_reception")
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", snapshot.State)
	newer := lateS7NextPlan(t, root)
	before := stageSnapshot(t, root)
	lateS7Observe(t, root, old)
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("unchanged blocked original was received twice")
	}
	s7ListenerAnswer(t, root, old)
	out, diagnostic := lateS7Observe(t, root, old)
	second := minimalAnswerReviewReceipt(t, root, old)
	if !second.Accepted || !second.WorkComplete || second.SubmissionKey == first.SubmissionKey {
		t.Fatalf("original continuation not received: %+v %s %s", second, out, diagnostic)
	}
	preserved, err := os.ReadFile(filepath.Join(root, first.DomainRefs[0]))
	if err != nil || !reflect.DeepEqual(firstBytes, preserved) {
		t.Fatal("original blocked handoff was rewritten")
	}
	after := stageSnapshot(t, root)
	want, _ := json.Marshal(before.State["review"])
	got, _ := json.Marshal(after.State["review"])
	if !reflect.DeepEqual(want, got) {
		t.Fatal("original continuation changed new Plan")
	}
	if receipt := minimalAnswerReviewReceipt(t, root, newer[0]); receipt.Accepted {
		t.Fatalf("old continuation claimed new receipt: %+v", receipt)
	}
}

func TestS7LateOriginalDifferentOwnerRejectsCrossResponsibility(t *testing.T) {
	root, old := s7ListenerFixture(t)
	newer := lateS7NextPlan(t, root, true)
	if len(newer) != 1 || newer[0].Binding.OwnerAgentID == old.Binding.OwnerAgentID {
		t.Fatal("fixture did not replace actual owner")
	}
	s7ListenerAnswer(t, root, old)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := service.SaveReviewResultFrom(context.Background(), newer[0].Binding.OwnerAgentID, filepath.Join(root, old.AnswerPath), root, nil)
	if err == nil || wrong.Accepted {
		t.Fatalf("new owner accepted old author's responsibility: %+v %v", wrong, err)
	}
	// The same original author/reference still owns the old task after replacement.
	out, diagnostic := lateS7Observe(t, root, old)
	if receipt := minimalAnswerReviewReceipt(t, root, old); !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("old owner lost original responsibility: %+v %s %s", receipt, out, diagnostic)
	}
	if receipt := minimalAnswerReviewReceipt(t, root, newer[0]); receipt.Accepted {
		t.Fatalf("old author completed new owner's task: %+v", receipt)
	}
}

func TestS7LateLostOriginalSourceRemainsUnknownWithoutBlockingNewPlan(t *testing.T) {
	root, old := s7ListenerFixture(t)
	newer := lateS7NextPlan(t, root)
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["packet_id"] == old.ID {
			delete(row, "review_result_reception")
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	s7ListenerAnswer(t, root, old)
	out, diagnostic := lateS7Observe(t, root, old)
	if !strings.Contains(out+diagnostic, "original execution source is unknown") {
		t.Fatalf("lost source guessed from current owner: %s %s", out, diagnostic)
	}
	if stageSnapshot(t, root).Revision != current.Revision {
		t.Fatal("unknown old source changed new Plan")
	}
	if got := minimalAnswerReviewReceipt(t, root, old); got.Accepted {
		t.Fatal("unknown old source invented acceptance")
	}
	// Current S7 still receives its own exact packet normally.
	s7ListenerAnswer(t, root, newer[0])
	out, diagnostic = lateS7Observe(t, root, newer[0])
	if got := minimalAnswerReviewReceipt(t, root, newer[0]); !got.Accepted {
		t.Fatalf("unknown old source blocked current S7: %+v %s %s", got, out, diagnostic)
	}
}

func TestS7LateAcceptedPrivateWorkerVolumeRemoved(t *testing.T) {
	root, old := s7ListenerFixture(t)
	// This legacy execution API checks unrelated untracked inputs. Remove the
	// untouched Main draft; the original packet remains in Runtime.
	if err := os.Remove(filepath.Join(root, old.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	current := stageSnapshot(t, root)
	binding, err := workspace.Decode(current.State)
	if err != nil || binding == nil {
		t.Fatalf("workspace: %v", err)
	}
	execution, err := binding.Plan(context.Background(), current.State, old.Binding.AssignmentID, old.Binding.OwnerAgentID, []string{"internal/example/"}, []string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	execution.Status = "ready"
	binding.Executions[execution.AssignmentID] = execution
	current.State["workspace"] = workspace.Encode(binding)
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	if err := os.MkdirAll(execution.Path, 0700); err != nil {
		t.Fatal(err)
	}
	s7ListenerAnswer(t, execution.Path, old)
	out, diagnostic := lateS7Observe(t, root, old)
	accepted := minimalAnswerReviewReceipt(t, root, old)
	if !accepted.Accepted || !accepted.WorkComplete {
		t.Fatalf("private original not accepted: %+v %s %s", accepted, out, diagnostic)
	}
	lateS7NextPlan(t, root)
	// Real original private volume is gone. Replayed acceptance is entirely
	// recorded reception and does not open, canonicalize or stat that volume.
	if err := os.RemoveAll(execution.Path); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root).Revision
	out, diagnostic = lateS7Observe(t, root, old)
	if stageSnapshot(t, root).Revision != before {
		t.Fatalf("removed private volume repeated acceptance: %s %s", out, diagnostic)
	}
	got := minimalAnswerReviewReceipt(t, root, old)
	if !got.Accepted || !got.WorkComplete || got.SubmissionKey != accepted.SubmissionKey {
		t.Fatalf("removed private volume lost original receipt: %+v", got)
	}
}
