package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestNativeS9BlockedWithoutPlanNaturalEvents(t *testing.T) {
	for _, verdict := range []string{"blocked", "contract_conflict"} {
		for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
			t.Run(verdict+"/"+event, func(t *testing.T) {
				root, worker, plan, in, _ := nativeS9WorkerFixture(t)
				planPath, resultPath := nativeS9TestAuthorPaths(t, root, plan)
				if err := os.Remove(filepath.Join(worker, planPath)); err != nil {
					t.Fatal(err)
				}
				answer := map[string]any{"verdict": verdict, "residual_risks": []string{"environment unavailable"}}
				data, _ := json.Marshal(answer)
				if err := os.WriteFile(filepath.Join(worker, resultPath), data, 0600); err != nil {
					t.Fatal(err)
				}
				in.Event, in.CWD = event, worker
				nativeS9SplitChild(t, &in)
				out, diagnostic := s9ReferencePublicHook(t, root, in)
				state := repairDispatchSnapshot(t, root).State
				packets, err := runtime.RepairResultPacketsForOwnerContext(runtime.WithPacketBodyRoot(t.Context(), root), state, plan.Binding.OwnerAgentID)
				if err != nil || len(packets) != 1 {
					t.Fatalf("result packet=%d err=%v out=%s diagnostic=%s", len(packets), err, out, diagnostic)
				}
				if packets[0].Binding.PlanReportRef != "" || string(packets[0].Inputs.PlanReport) != "null" {
					t.Fatalf("invented Plan: %+v", packets[0].Binding)
				}
				svc, err := submission.NewReadOnly(root)
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := svc.RepairResultReceipt(plan.Binding.OwnerAgentID, packets[0].ID)
				if err != nil || !receipt.Accepted || receipt.WorkComplete {
					t.Fatalf("blocker receipt=%+v err=%v out=%s diagnostic=%s", receipt, err, out, diagnostic)
				}
			})
		}
	}
}
