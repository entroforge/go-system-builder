package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
)

// The fixture supplies a real approved Plan, native D execution and original
// Result issuer/consumer; it does not claim a model or product acceptance.
func TestS9PublicStatusProjectsNativeResultAndCandidate(t *testing.T) {
	root, worker, plan, _, professional := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := importNativeS9PlanReport(t.Context(), root, worker, plan, service, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("original Plan: %+v %v", receipt, err)
	}
	if err := recordNativeS9Source(t.Context(), root, worker, repairLeadGit(t, root, "rev-parse", "HEAD"), plan); err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueRepairResults(t.Context(), plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Result issue: %+v %v", packets, err)
	}
	packet := packets[0]
	status := func() (map[string]any, map[string]any) {
		t.Helper()
		before := repairDispatchRuntimeBytes(t, root)
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"runtime", "repair", "status", "--root", root}, nil, &stdout, &stderr); code != 0 {
			t.Fatalf("public status: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		repairDispatchAssertUnchanged(t, root, before)
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		rows := objectSliceAny(result["repair_result_packets"])
		if len(rows) != 1 || rows[0]["packet_id"] != packet.ID {
			t.Fatalf("status lost original Result packet: %s", stdout.String())
		}
		board := mapFieldCLI(result, "board")
		if result["next"] != board["next_action"] {
			t.Fatalf("status/board next differ: %s", stdout.String())
		}
		return result, rows[0]
	}

	result, row := status()
	if row["status"] != "issued" || !strings.Contains(stringValue(result["next"]), "PreToolUse") || !strings.Contains(stringValue(result["next"]), "PostToolUse") {
		t.Fatalf("valid native Result falsely treated as stale: row=%#v next=%v", row, result["next"])
	}
	brief, _, _ := nativeS9Paths(plan)
	_, resultPath, err := nativeS9AuthorPaths(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	if row["view_path"] != filepath.Join(root, brief) || row["answer_path"] != filepath.Join(worker, resultPath) ||
		strings.Contains(stringValue(result["next"]), packet.AnswerPath) {
		t.Fatalf("native issued guidance did not identify the original brief/flat answer: row=%#v next=%v", row, result["next"])
	}

	// Simulate an actual current-execution generation drift, without changing
	// the frozen packet. Status must not mistake the old issuance for current.
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	originalState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := repairDispatchSnapshot(t, root)
	executions := mapFieldCLI(mapFieldCLI(snapshot.State, "workspace"), "executions")
	execution := mapFieldCLI(executions, plan.Binding.ManifestAssignmentID)
	if len(execution) == 0 {
		t.Fatal("native execution missing")
	}
	execution["execution_generation"] = intFieldCLI(execution["execution_generation"]) + 1
	putInitMap(t, statePath, snapshot.State)
	result, row = status()
	if row["status"] != "stale_or_preserved" || !strings.Contains(stringValue(result["next"]), "recovery") {
		t.Fatalf("changed execution attempt was presented as current: row=%#v next=%v", row, result["next"])
	}
	if err := os.WriteFile(statePath, originalState, 0600); err != nil {
		t.Fatal(err)
	}

	answer := packet.Draft
	answer.Answer, _ = json.Marshal(professional)
	path := filepath.Join(worker, packet.AnswerPath)
	if err := persistRepairLeadQuestionnaire(path, answer); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveRepairResultFrom(t.Context(), plan.Binding.OwnerAgentID, path, worker, nil)
	if err != nil || !receipt.Accepted || receipt.PacketID != packet.ID || receipt.ResponsibilityStatus != "candidate_accepted" {
		t.Fatalf("original candidate: %+v %v", receipt, err)
	}
	result, row = status()
	if row["status"] != "worker_complete_main_pending" || !strings.Contains(stringValue(result["next"]), "runtime workspace integrate") || !strings.Contains(stringValue(result["next"]), packet.Binding.Sources.OwnerAgentID) ||
		stringValue(row["view_path"]) != "" || stringValue(row["answer_path"]) != "" || strings.Contains(stringValue(result["next"]), resultPath) {
		t.Fatalf("accepted native Candidate lacks Main integration handoff: row=%#v next=%v", row, result["next"])
	}
}
