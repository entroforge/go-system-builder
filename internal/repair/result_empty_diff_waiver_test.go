package repair_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

// The former environment escape cannot turn a bare empty-diff result into
// a formal confirmation. Confirmation requires a pinned predecessor Session.
func TestS9PassEmptyDiffWaiverRequiresEscapeAndEmptyChangedArtifacts(t *testing.T) {
	root := t.TempDir()
	contractRef, _ := writeRuntimeContract(t, root)
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-empty-diff", RuntimeID: "loop-REQ-039",
		ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{
		Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-empty-diff", CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	fabricated := []repair.ChangedArtifact{{Path: "internal/api/first.go", SHA256: strings.Repeat("a", 64), Status: "added"}}
	submit := func(resultID string, changed []repair.ChangedArtifact) (repair.RepairResult, error) {
		result, _, err := repair.SubmitRepairResult(root, repair.RepairResultRequest{
			Contract: contractRef, Session: sessionRef, Plan: planRef, ResultID: resultID,
			ProducerAgentID:  "builder",
			UnitResults:      []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://confirm"}}},
			ChangedArtifacts: changed, Result: "pass",
			BeforeFixChecks: []repair.RepairCheck{{Name: "pre-fix", Command: "go test ./...", Result: "fail", EvidenceRefs: []string{"test://red"}}},
			Checks:          []repair.RepairCheck{{Name: "confirm", Command: "git show HEAD:<path> | sha256sum", Result: "pass", EvidenceRefs: []string{"test://green"}}},
		})
		return result, err
	}

	// Default behavior is unchanged: an empty-diff pass is rejected whether or
	// not the seat enumerates artifacts.
	if _, err := submit("repair-result-empty-diff-none", nil); err == nil || !strings.Contains(err.Error(), "must enumerate changed artifacts") {
		t.Fatalf("default empty-changed pass should demand enumeration: %v", err)
	}
	if _, err := submit("repair-result-empty-diff-listed", fabricated); err == nil || !strings.Contains(err.Error(), "requires a repository change") {
		t.Fatalf("default empty-diff pass should be rejected: %v", err)
	}

	// The old compatibility variable must not permit a bare empty-diff PASS.
	t.Setenv("LOOP_ALLOW_EMPTY_FINGERPRINT", "1")
	if _, err := submit("repair-result-empty-diff-env", nil); err == nil {
		t.Fatal("environment bypassed missing confirmation provenance")
	}
	if _, err := submit("repair-result-empty-diff-fabricated", fabricated); err == nil {
		t.Fatal("environment laundered predecessor changes")
	}
}
