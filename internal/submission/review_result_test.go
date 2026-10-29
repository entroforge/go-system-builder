package submission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func submissionReviewPacket(t *testing.T, owner string) workpacket.ReviewResultPacket {
	t.Helper()
	packet, err := workpacket.BuildReviewResult(workpacket.ReviewResultBinding{
		RuntimeID: "loop-REQ-041", BaselineGeneration: 3,
		ReviewPlanID: "review-plan-041-r1", ReviewPlanRef: ".claude/review/plan.json",
		ReviewPlanSHA256: strings.Repeat("a", 64), ReviewPlanRevision: 2, ReviewRound: 1,
		AssignmentID: "assignment-" + owner, AssignmentSHA256: strings.Repeat("b", 64),
		OwnerAgentID: owner, ClaimIDs: []string{"claim-login"}, SubjectDigest: strings.Repeat("c", 64),
	}, workpacket.ReviewResultInputs{
		Claims:         json.RawMessage(`[{"claim_id":"claim-login","lens":"qa","target":"login","assertion":"login works","oracle":"visible home","method":"submit credentials","applicability":"required","source_refs":["REQ-041"]}]`),
		FrozenSubjects: json.RawMessage(`[{"path":"docs/requirements/REQ-041.md","sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func submissionReviewState(t *testing.T, packets ...workpacket.ReviewResultPacket) map[string]any {
	t.Helper()
	first := packets[0]
	rows := []any{}
	assignments := map[string]any{}
	for _, packet := range packets {
		data, err := json.Marshal(packet)
		if err != nil {
			t.Fatal(err)
		}
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
		assignments[packet.Binding.AssignmentID] = map[string]any{"status": "dispatched", "agent_id": packet.Binding.OwnerAgentID, "claim_ids": []any{"claim-login"}}
	}
	return map[string]any{
		"schema_version": "1.2.0", "runtime_id": first.Binding.RuntimeID,
		"baseline":      map[string]any{"generation": first.Binding.BaselineGeneration},
		"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
		"entities":      map[string]any{"work_packets": rows},
		"review": map[string]any{"round": 1, "assignments": assignments, "plan": map[string]any{
			"plan_id": first.Binding.ReviewPlanID, "path": first.Binding.ReviewPlanRef,
			"sha256": first.Binding.ReviewPlanSHA256, "revision": first.Binding.ReviewPlanRevision, "review_round": 1,
		}},
	}
}

func TestReviewResultPathSelectsBeforeReadingUnrelatedBodies(t *testing.T) {
	for _, damage := range []string{"missing", "corrupt"} {
		t.Run(damage, func(t *testing.T) {
			root := t.TempDir()
			own, other := submissionReviewPacket(t, "reviewer-own"), submissionReviewPacket(t, "reviewer-other")
			state := submissionReviewState(t, own, other)
			rows := state["entities"].(map[string]any)["work_packets"].([]any)
			index, err := packetbody.Publish(context.Background(), root, rows[1].(map[string]any))
			if err != nil {
				t.Fatal(err)
			}
			ref, _, err := packetbody.ReferenceFromIndex(index)
			if err != nil {
				t.Fatal(err)
			}
			if damage == "missing" {
				err = os.Remove(filepath.Join(root, ref.Path))
			} else {
				err = os.WriteFile(filepath.Join(root, ref.Path), []byte("corrupted body"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			rows[1] = index
			for _, path := range []string{own.AnswerPath, filepath.Join(root, filepath.FromSlash(own.AnswerPath))} {
				got, source, found, err := currentReviewResultSourceForPathContext(context.Background(), root, state, path)
				if err != nil || !found || got.ID != own.ID || source != root || !sameReviewResultPacket(got, own) {
					t.Fatalf("unrelated %s body blocked exact original: found=%v packet=%s source=%s err=%v", damage, found, got.ID, source, err)
				}
			}
			if _, _, _, err := currentReviewResultSourceForPathContext(context.Background(), root, state, other.AnswerPath); err == nil {
				t.Fatal("selected damaged body was accepted")
			}
			for _, path := range []string{filepath.Join(t.TempDir(), filepath.FromSlash(own.AnswerPath)), filepath.Join(filepath.Dir(own.AnswerPath), "sibling.json")} {
				if _, _, found, err := currentReviewResultSourceForPathContext(context.Background(), root, state, path); err != nil || found {
					t.Fatalf("non-source or sibling selected: found=%v err=%v", found, err)
				}
			}
		})
	}
}

func TestReviewResultPathRetainsGlobalIndexChecks(t *testing.T) {
	for _, malformed := range []any{false, map[string]any{"template_id": "s5-document-review-v1"}, map[string]any{"packet_id": "other", "storage_format": "future-format"}} {
		root := t.TempDir()
		packet := submissionReviewPacket(t, "reviewer-own")
		state := submissionReviewState(t, packet)
		entities := state["entities"].(map[string]any)
		entities["work_packets"] = append(entities["work_packets"].([]any), malformed)
		if _, _, _, err := currentReviewResultSourceForPathContext(context.Background(), root, state, packet.AnswerPath); err == nil {
			t.Fatalf("malformed shared index was hidden: %#v", malformed)
		}
	}
	root := t.TempDir()
	packet := submissionReviewPacket(t, "reviewer-own")
	state := submissionReviewState(t, packet)
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = append(entities["work_packets"].([]any), map[string]any{"packet_id": packet.ID, "template_id": "s5-document-review-v1"})
	if _, _, _, err := currentReviewResultSourceForPathContext(context.Background(), root, state, "unrelated.json"); err == nil || !strings.Contains(err.Error(), "duplicate Runtime work packet ID") {
		t.Fatalf("cross-subtype duplicate hidden for a nonmatching path: %v", err)
	}
}

func TestReviewResultPathRequiresSelectedCanonicalBody(t *testing.T) {
	root := t.TempDir()
	packet := submissionReviewPacket(t, "reviewer-own")
	state := submissionReviewState(t, packet)
	row := state["entities"].(map[string]any)["work_packets"].([]any)[0].(map[string]any)
	row["draft"].(map[string]any)["title"] = "forged title"
	if _, _, _, err := currentReviewResultSourceForPathContext(context.Background(), root, state, packet.AnswerPath); err == nil {
		t.Fatal("selection coordinates substituted for canonical packet validation")
	}
}

func TestSaveReviewResultBeforeSubmitDoesNotClaimCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	receipt, err := (&Service{Root: t.TempDir()}).SaveReviewResultFrom(ctx, "reviewer-own", "answer.json", "", nil)
	var gap *ReviewResultReceiptError
	if err == nil || errors.As(err, &gap) || receipt.Accepted || receipt.WorkComplete {
		t.Fatalf("pre-submit failure claimed commit: receipt=%+v error=%v", receipt, err)
	}
}

func TestSubmittedReviewResultReceiptFailurePreservesCommitCoordinates(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	packet := submissionReviewPacket(t, "reviewer-own")
	for _, failure := range []string{"missing receipt", "missing result", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			state := submissionReviewState(t, packet)
			ctx := context.Background()
			if failure == "missing result" {
				row := state["review"].(map[string]any)["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)
				row["result_receipt"] = map[string]any{"packet_id": packet.ID, "producer_agent_id": packet.Binding.OwnerAgentID, "result_ref": ".claude/evidence/missing-result.json"}
			}
			if failure == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			snapshot := loopruntime.Snapshot{Revision: 42, State: state}
			receipt, err := submittedReviewResultReceipt(root, source, snapshot, packet, packet.Binding.OwnerAgentID, ctx)
			var gap *ReviewResultReceiptError
			if !errors.As(fmt.Errorf("caller: %w", err), &gap) || gap.PacketID != packet.ID || gap.OwnerAgentID != packet.Binding.OwnerAgentID || gap.AnswerPath != packet.AnswerPath || gap.SourceRoot != source || gap.Revision != 42 || gap.Cause == nil {
				t.Fatalf("lost successful submission coordinates: receipt=%+v error=%v", receipt, err)
			}
			if receipt.PacketID != packet.ID || receipt.Accepted || receipt.WorkComplete || receipt.ResponsibilityStatus == "not_accepted" {
				t.Fatalf("unverified receipt misrepresented: %+v", receipt)
			}
			if failure == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cause not preserved: %v", err)
			}
			// A standalone receipt lookup must not claim a successful Submit.
			_, lookupErr := issuedReviewResultReceipt(root, snapshot, packet, packet.Binding.OwnerAgentID, ctx)
			if errors.As(lookupErr, &gap) {
				t.Fatalf("independent receipt error claimed this attempt committed: %v", lookupErr)
			}
		})
	}
}
