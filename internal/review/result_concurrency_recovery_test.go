package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func issuedConcurrentResultsFixture(t *testing.T) (string, string, string, loopruntime.Snapshot, []workpacket.ReviewResultPacket, []IssuedSubmitRequest) {
	t.Helper()
	root, sp, jp, _, plan := packetModeFixture(t)
	state := snapshotState(t, sp, jp)
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		a := raw.(map[string]any)
		if a["id"] == "agent-qa-1" || a["id"] == "agent-dv-1" {
			a["state"], a["dispatch_mode"] = "reading", "plan_checkpoint"
			a["plan_reported_ref"], a["activation_ref"], a["readback_ref"] = nil, nil, nil
		}
	}
	writeMethodState(t, sp, state)
	var packets []workpacket.ReviewResultPacket
	for _, owner := range []string{"agent-qa-1", "agent-dv-1"} {
		issued, err := IssueResultPackets(root, sp, jp, -1, owner)
		if err != nil || len(issued) != 1 {
			t.Fatalf("issue %s: %d %v", owner, len(issued), err)
		}
		packets = append(packets, issued[0])
	}
	var requests []IssuedSubmitRequest
	snapshot, err := loopruntime.NewStore(sp, jp).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range packets {
		conclusions := map[string]string{}
		for _, claim := range findPlanAssignment(plan, p.Binding.AssignmentID).ClaimIDs {
			conclusions[claim] = "pass"
		}
		result := writeResultFile(t, root, plan, p.Binding.AssignmentID, p.ResultID, p.Binding.OwnerAgentID, "pass", conclusions, nil)
		var original map[string]json.RawMessage
		if err := json.Unmarshal(mustReadTestFile(t, result), &original); err != nil {
			t.Fatal(err)
		}
		answer := s7ProfessionalTestAnswer(t, original)
		var claims []map[string]any
		if err := json.Unmarshal(answer["claim_results"], &claims); err != nil {
			t.Fatal(err)
		}
		for _, claim := range claims {
			for i, ref := range claim["evidence_refs"].([]any) {
				claim["evidence_refs"].([]any)[i] = strings.Split(ref.(string), "#sha256=")[0]
			}
		}
		answer["claim_results"], _ = json.Marshal(claims)
		p.Draft.Answer, _ = json.Marshal(answer)
		draft, _ := json.Marshal(p.Draft)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p.AnswerPath)), draft, 0o600); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, IssuedSubmitRequest{ExpectedRevision: snapshot.Revision, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: p.Binding.OwnerAgentID})
	}
	return root, sp, jp, snapshot, packets, requests
}

func TestReviewIssuedConcurrentDifferentAssignmentsContinueOriginalAnswers(t *testing.T) {
	root, sp, jp, before, packets, requests := issuedConcurrentResultsFixture(t)
	var workers sync.WaitGroup
	start := make(chan struct{})
	outcomes := make([]error, 2)
	timings := make([]*metrics.CallTimings, 2)
	for i := range requests {
		parent, spans := metrics.WithCallTimings(context.Background())
		timings[i] = spans
		bounded, cancel := context.WithTimeout(parent, 6*time.Second)
		defer cancel()
		requests[i].Context = bounded
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			_, outcomes[i] = SubmitIssuedResult(root, sp, jp, requests[i])
		}(i)
	}
	close(start)
	workers.Wait()
	for i, err := range outcomes {
		if err != nil {
			t.Fatalf("original answer %d was not received in same handoff: %v", i, err)
		}
	}
	current, err := loopruntime.NewStore(sp, jp).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != before.Revision+2 || PlanPointerFromState(current.State).Status != "clean" {
		t.Fatal("both original answers and final CleanRound were not consumed exactly once")
	}
	var tails int64
	for _, stats := range timings {
		spans := stats.Snapshot()
		tails += spans["reception/s7/concurrency_tail"].Count
		if spans["reception/s7/recovery_tail"].Count != 0 {
			t.Fatal("concurrent continuation started another deadline continuation")
		}
	}
	if tails != 1 {
		t.Fatalf("concurrency continuations=%d want 1", tails)
	}
	for i, p := range packets {
		requests[i].Context = context.Background()
		if _, found, err := IssuedResultReceipt(root, current.State, p.ID, p.Binding.OwnerAgentID); err != nil || !found {
			t.Fatalf("packet receipt %d missing: %v", i, err)
		}
		if _, err := SubmitIssuedResult(root, sp, jp, requests[i]); err != nil {
			t.Fatalf("original accepted packet replay %d: %v", i, err)
		}
	}
	final, err := loopruntime.NewStore(sp, jp).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if final.Revision != current.Revision {
		t.Fatal("accepted peer replay committed twice")
	}
}

func TestReviewNonIssuedStaleCASRemainsExplicit(t *testing.T) {
	root, sp, jp, snapshot, plan := packetModeFixture(t)
	input := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-explicit-stale", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	ctx, timings := metrics.WithCallTimings(context.Background())
	_, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: snapshot.Revision - 1, AssignmentID: "assignment-qa-1", ResultPath: input})
	if !errors.Is(err, loopruntime.ErrStaleRevision) {
		t.Fatalf("explicit nonissued CAS = %v", err)
	}
	if spans := timings.Snapshot(); spans["reception/s7/concurrency_tail"].Count != 0 || spans["reception/s7/recovery_tail"].Count != 0 {
		t.Fatal("explicit nonissued CAS retried")
	}
}

func TestReviewIssuedConcurrentContinuationRejectsChangedSources(t *testing.T) {
	for _, change := range []string{"plan", "owner", "product-source"} {
		t.Run(change, func(t *testing.T) {
			root, sp, jp, before, packets, requests := issuedConcurrentResultsFixture(t)
			p := packets[0]
			if change == "plan" {
				p = packets[1]
			}
			input := filepath.Join(root, "docs", "reports", p.ResultID+".json")
			request := SubmitRequest{Context: context.Background(), ExpectedRevision: before.Revision - 1, AssignmentID: p.Binding.AssignmentID, ResultPath: input, resultData: mustReadTestFile(t, input), issuedPacket: &p, packetOwner: p.Binding.OwnerAgentID, issuedSourceRoot: root}
			switch change {
			case "plan":
				accepted, err := SubmitIssuedResult(root, sp, jp, requests[0])
				if err != nil {
					t.Fatal(err)
				}
				path := receiptRevisionPlan(t, root, accepted.State, packets[0].ResultID, false)
				if _, err := RevisePlan(root, sp, jp, ReviseRequest{ExpectedRevision: accepted.Revision, PlanPath: path, SourceRef: packets[0].ResultID, AffectedSurface: "internal/example"}); err != nil {
					t.Fatalf("controlled actual plan revision: %v", err)
				}
			case "owner":
				resultPacketAssignmentRow(before.State, p.Binding.AssignmentID)["agent_id"] = "agent-dv-1"
				writeMethodState(t, sp, before.State)
			case "product-source":
				plan, _, err := LoadPlan(root, before.State)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(plan.FrozenSubjects[0].Path)), []byte("actual current product drift"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := continueIssuedResultAfterPeer(root, sp, jp, request)
			if err == nil {
				t.Fatalf("concurrent revision refresh admitted changed %s", change)
			}
			after, readErr := loopruntime.NewStore(sp, jp).Snapshot()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if row := resultPacketAssignmentRow(after.State, p.Binding.AssignmentID); row["result_receipt"] != nil || row["result_ref"] != nil {
				t.Fatalf("changed %s created acceptance", change)
			}
		})
	}
}

func TestReviewSubmitWrapperEarlyErrorsPreserveRequiredFieldsAndMetrics(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "required-fields", false: "missing-file"}[missing], func(t *testing.T) {
			root := t.TempDir()
			request := SubmitRequest{AssignmentID: "assignment", ResultPath: filepath.Join(root, "absent.json")}
			if missing {
				request.AssignmentID = ""
			}
			_, err := SubmitResult(root, filepath.Join(root, "state.json"), filepath.Join(root, "journal.jsonl"), request)
			if err == nil {
				t.Fatal("early input error accepted")
			}
			if missing && !strings.Contains(err.Error(), "--assignment-id and --result are required") {
				t.Fatalf("required-field priority changed: %v", err)
			}
			counts, err := metrics.NewStore(root).Read()
			if err != nil {
				t.Fatal(err)
			}
			if counts.S7ResultSubmits["rejected"] != 1 {
				t.Fatalf("early read/required failure bypassed rejection metric: %+v", counts.S7ResultSubmits)
			}
		})
	}
}
