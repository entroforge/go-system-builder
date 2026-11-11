package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/submission"
	"strings"
	"testing"
)

// Exercise the original public Hook entrance with an existing accepted record,
// including the legacy digest field absent, stale, and belonging to a different owner.
func TestHookOriginalReceiptGuidanceDoesNotRequireDigest(t *testing.T) {
	for _, historicalSHA := range []string{"", "obsolete-wrong-value"} {
		t.Run(historicalSHA, func(t *testing.T) {
			f := newS9ListenerFixture(t, false)
			root, state := f.root, f.state
			state["lifecycle"] = map[string]any{"state": "planning", "phase": "design"}
			entities := state["entities"].(map[string]any)
			entities["work_packets"] = []any{map[string]any{"packet_id": "p-original", "template_id": "legacy", "answer_path": ".claude/answer.json", "binding": map[string]any{"owner": "worker"}}}
			entities["submissions"] = []any{
				map[string]any{"packet_id": "p-original", "owner": "worker", "control_status": "advanced", "content_sha256": historicalSHA},
				map[string]any{"packet_id": "p-other", "owner": "other-worker", "control_status": "pending"},
			}
			f.write(t)
			payload, err := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "agent_id": "worker", "cwd": root, "session_id": "nohash-receipt"})
			if err != nil {
				t.Fatal(err)
			}
			var out, diagnostic bytes.Buffer
			if code := Run([]string{"hook", "--root", root, "--event", "SessionStart"}, bytes.NewReader(payload), &out, &diagnostic); code != 0 {
				t.Fatalf("hook exit=%d out=%s diagnostic=%s", code, &out, &diagnostic)
			}
			if !strings.Contains(out.String(), "original answer p-original is accepted; original control status=advanced") || strings.Contains(out.String(), "p-other is accepted") {
				t.Fatalf("wrong original owner receipt guidance: %s / %s", &out, &diagnostic)
			}
		})
	}
}

// The public S9 handoff uses the previously issued packet and original owner,
// even when an obsolete Plan digest in the current pointer differs.
func TestS9HandoffOriginalPacketIgnoresHistoricalPlanDigest(t *testing.T) {
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, plan, service, &diagnostic, nil); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Plan not received: %+v %v", receipt, err)
	}
	if err := recordNativeS9OriginalLocation(t.Context(), root, plan, worker); err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueRepairResults(t.Context(), plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Result issue: %+v %v", packets, err)
	}
	packet := packets[0]
	snap := repairDispatchSnapshot(t, root)
	repairPointerCLI(snap.State)["plan_sha256"] = "stale-historical-value"
	selected, found, err := typedRepairResultPacketForAssignmentContext(t.Context(), root, snap.State, plan.Binding.ManifestAssignmentID, plan.Binding.OwnerAgentID)
	if err != nil || !found || selected.ID != packet.ID {
		t.Fatalf("original packet lost: found=%t id=%q want=%q err=%v", found, selected.ID, packet.ID, err)
	}
	if _, _, err := typedRepairResultPacketForAssignmentContext(t.Context(), root, snap.State, plan.Binding.ManifestAssignmentID, "different-owner"); err == nil {
		t.Fatal("original packet was attributed to a different owner")
	}
	handoff := s9HandoffForBinding(t.Context(), root, snap, plan.Binding)
	if handoff.Result == nil {
		t.Fatal("original result is not visible in handoff")
	}
	handoff.ResultFactKnown, handoff.ResultFactError, handoff.ResultFact = true, nil, s9ListenerFact{}
	ordinary := s9ObservedHandoffAction(t.Context(), root, snap, handoff, map[string]any{})
	legacy := s9ObservedHandoffAction(t.Context(), root, snap, handoff, map[string]any{"delivery_sha256": "stale"})
	if ordinary != legacy {
		t.Fatalf("obsolete digest claimed candidate without a delivery: %q != %q", legacy, ordinary)
	}
	withRef := s9ObservedHandoffAction(t.Context(), root, snap, handoff, map[string]any{"delivery_ref": "original-candidate.json"})
	if !strings.Contains(withRef, "binding or provenance is incomplete") {
		t.Fatalf("original missing delivery remains unknown: %s", withRef)
	}
}
