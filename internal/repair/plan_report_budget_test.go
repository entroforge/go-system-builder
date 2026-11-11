package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// A valid-sized professional answer is stored both as the original PlanReport
// projection and in the immutable typed receipt. The final artifact limit must
// be checked before any directory or artifact is published.
func TestPlanReportAnswerAndReceiptDoubleProjectionRejectedBeforePublication(t *testing.T) {
	root := t.TempDir()
	planText := strings.Repeat("x", 11<<20)
	answer, err := json.Marshal(map[string]any{
		"plan": planText,
		"red_checks": []any{map[string]any{
			"name": "pre-fix", "command": "go test ./internal/example", "result": "fail",
			"evidence_refs": []string{"test://pre-fix"},
		}},
		"proposed_paths": []string{"internal/example/example.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(answer) >= 20<<20 {
		t.Fatalf("professional answer itself exceeds its input bound: %d bytes", len(answer))
	}

	report := PlanReport{
		SchemaVersion: "1.0.0", RecordType: "repair_plan_report", ReportID: "repair-plan-report-budget",
		SessionID: "repair-session-budget", PlanID: "repair-plan-budget", AssignmentID: "repair-assignment-budget",
		AssertionIDs: []string{"symptom-1"}, AgentID: "builder-budget", Plan: planText,
		RedChecks:     []RepairCheck{{Name: "pre-fix", Command: "go test ./internal/example", Result: "fail", EvidenceRefs: []string{"test://pre-fix"}}},
		ProposedPaths: []string{"internal/example/example.go"}, Status: "reported", ReportedAt: "2026-10-14T00:00:00Z",
		Receipt: &runtimepkg.RepairPlanReportReceipt{
			Contract: "s9-plan-report-receipt-v1", PacketID: "packet-" + strings.Repeat("a", 64),
			SubmittedAnswer: json.RawMessage(answer),
			OwnerAgentID:    "builder-budget", RuntimeID: "runtime-budget", BaselineGeneration: 1,
			CaseID: "investigation-case-budget", CaseRef: "cases/budget.json",
			ContractID: "repair-contract-budget", ContractRef: "contracts/budget.json",
			SessionID: "repair-session-budget", SessionRef: "sessions/budget.json",
			PlanID: "repair-plan-budget", PlanRef: "plans/budget.json",
			RepairAssignmentID:   "repair-assignment-budget",
			ManifestAssignmentID: "assignment-budget",
			TeamID:               "team-budget", TaskID: "TASK-budget", TaskRef: "tasks/TASK-budget.json",
			ManifestRef: "teams/team.json", ExecutionRoot: root,
			ReportID: "repair-plan-report-budget", ReportRef: artifactRoot + "/plan-reports/repair-plan-report-budget.json",
			WorkComplete: false, IssuanceEventID: "evt-issue-budget", AcceptedEventID: "evt-accepted-budget",
			AcceptedIdempotencyKey: "runtime:accepted:budget", AcceptedAfterRuntimeRevision: 2, AcceptedSequence: 3,
		},
	}
	serialized, err := canonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(serialized) <= 20<<20 {
		t.Fatalf("answer and receipt projection did not exceed final artifact bound: %d bytes", len(serialized))
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-plan-report.schema.json", serialized); err != nil {
		t.Fatalf("synthetic doubled report does not satisfy the artifact schema: %v", err)
	}

	path := artifactRoot + "/plan-reports/repair-plan-report-budget.json"
	if _, err := writeImmutable(root, path, "repair-plan-report.schema.json", report); err == nil || !strings.Contains(err.Error(), "exceeds 20 MiB after final serialization") {
		t.Fatalf("expected final serialized report limit error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("oversized answer-plus-receipt report published control paths: %v", err)
	}
}
