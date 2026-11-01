package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9PlanViewOccupiedBeforeDispatchUsesRecordedAlternative(t *testing.T) {
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	occupied := filepath.Join(root, ".claude", "work-packets")
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(occupied); err == nil {
		if err := os.Rename(occupied, occupied+"-fixture-preserved"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(occupied, []byte("unknown Main view root"), 0600); err != nil {
		t.Fatal(err)
	}
	assignment := plan.Assignments[0].AssignmentID
	repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: assignment, RoleFamily: "backend-builder"}})
	snapshot := repairDispatchSnapshot(t, root)
	owner := stringMapCLI(repairPointerCLI(snapshot.State)["assignment_owners"])[assignment]
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := service.IssueRepairPlanReportForAssignmentPrepared(context.Background(), owner, assignment, func(ctx context.Context, packet *workpacket.RepairPlanReportPacket) error {
		return prepareNativeS9BriefForIssue(ctx, root, packet)
	})
	if err != nil || len(issued) != 1 {
		t.Fatalf("original Plan issuance: %+v %v", issued, err)
	}
	packets := nativeS9PlanPackets(root, repairDispatchSnapshot(t, root).State)
	if len(packets) != 1 || !strings.HasPrefix(packets[0].ViewPath, ".claude/work-packets-alt-") {
		t.Fatalf("issued actual Plan view: %+v", packets)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(packets[0].ViewPath))); err != nil {
		t.Fatal(err)
	}
	brief := nativeS9BriefPath(packets[0])
	briefBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(brief)))
	if err != nil || !strings.Contains(string(briefBytes), filepath.Join(root, filepath.FromSlash(packets[0].ViewPath))) {
		t.Fatalf("original brief did not contain the actual view: %v", err)
	}
	replayed, err := service.IssueRepairPlanReportForAssignment(context.Background(), owner, assignment)
	if err != nil || len(replayed) != 1 || replayed[0].Packet.ViewPath != packets[0].ViewPath {
		t.Fatalf("original Plan replay: %+v %v", replayed, err)
	}
	if got, err := os.ReadFile(occupied); err != nil || string(got) != "unknown Main view root" {
		t.Fatal("unknown view root changed")
	}
}

func TestS9ResultViewOccupiedDoesNotBlockOriginalAnswerOrReplay(t *testing.T) {
	root, worker, plan, _, answer := nativeS9WorkerFixture(t)
	ctx := context.Background()
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := receiveNativeS9ReadyPlan(ctx, root, worker, plan, service, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("original Plan: %+v %v", receipt, err)
	}
	if err := recordNativeS9OriginalLocation(ctx, root, plan, worker); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(worker, ".claude", "work-packets")
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(occupied); err == nil {
		if err := os.Rename(occupied, occupied+"-fixture-preserved"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(occupied, []byte("unknown Worker view root"), 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueRepairResults(ctx, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 || !strings.HasPrefix(packets[0].ViewPath, ".claude/work-packets-alt-") {
		t.Fatalf("original Result issuance: %+v %v", packets, err)
	}
	packet := packets[0]
	draft := packet.Draft
	draft.Answer, _ = json.Marshal(answer)
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, packet.AnswerPath), draft); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveRepairResultFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(worker, packet.AnswerPath), worker, nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Result acceptance: %+v %v", receipt, err)
	}
	replay, err := service.SaveRepairResultFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(worker, packet.AnswerPath), worker, nil)
	if err != nil || !replay.Accepted {
		t.Fatalf("original Result replay: %+v %v", replay, err)
	}
	if got, err := os.ReadFile(occupied); err != nil || string(got) != "unknown Worker view root" {
		t.Fatal("unknown Worker view root changed")
	}
}

func TestS9UnavailableOptionalResultViewDoesNotBlockCapturedOriginal(t *testing.T) {
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	ctx := context.Background()
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := receiveNativeS9ReadyPlan(ctx, root, worker, plan, service, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("original Plan: %+v %v", receipt, err)
	}
	if err := recordNativeS9OriginalLocation(ctx, root, plan, worker); err != nil {
		t.Fatal(err)
	}
	originalPath := nativeS9TestResultPath(t, root, plan)
	original, err := os.ReadFile(filepath.Join(worker, originalPath))
	if err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(worker, ".claude")
	if _, err := os.Lstat(occupied); err == nil {
		if err := os.Rename(occupied, occupied+"-fixture-preserved"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(occupied, []byte("unknown Worker control path"), 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueRepairResults(ctx, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 || packets[0].ViewPath != "" {
		t.Fatalf("optional view issuance: %+v %v", packets, err)
	}
	receipt, err := service.SaveRepairResultBytes(ctx, plan.Binding.OwnerAgentID, packets[0].ID, worker, original)
	if err != nil || !receipt.Accepted {
		t.Fatalf("captured original result was blocked by view: %+v %v", receipt, err)
	}
	if got, err := os.ReadFile(occupied); err != nil || string(got) != "unknown Worker control path" {
		t.Fatal("unknown Worker path changed")
	}
}

func TestS9UnavailablePlanViewKeepsOriginalQuestionInBrief(t *testing.T) {
	root, _, original, _, _ := nativeS9WorkerFixture(t)
	packet := original
	packet.ViewPath, packet.BriefPath = "", ""
	if _, err := prepareNativeS9BriefMode(context.Background(), root, &packet); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(packet.BriefPath)))
	instruction, _ := json.Marshal(packet.Draft.Instruction)
	if err != nil || !strings.Contains(string(body), "Original professional question:") || !strings.Contains(string(body), string(instruction)) {
		t.Fatalf("original question missing from fallback brief: %v", err)
	}
}
