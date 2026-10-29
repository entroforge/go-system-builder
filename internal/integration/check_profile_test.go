package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func profileFixture(command string) checkexec.Profile {
	return checkexec.Profile{ID: "CP-INTEGRATION", Version: "v1", CheckIndex: 0, Command: command, Location: "main", TimeoutSeconds: 1, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
}
func TestProfileMainRunsActualCheckCapturesBindingAndRejectsDrift(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	receipts := []string{}
	toolReceipts := []checkexec.ToolReceipt{}
	ctx := context.WithValue(context.Background(), checkEvidenceKey{}, checkEvidence{Directory: dir, Receipts: &receipts, ToolReceipts: &toolReceipts})
	p := profileFixture("true")
	binding := checkexec.Binding{RuntimeID: "runtime", Generation: 1, AssignmentID: "assignment", Owner: "builder", ExecutionGeneration: 1, Responsibility: "main_required_integration"}
	runner := ProfileCheckRunner([]checkexec.Profile{p}, binding)
	if err := runner(ctx, root, p.Command); err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 {
		t.Fatal("actual run did not publish receipt")
	}
	if len(toolReceipts) != 1 {
		t.Fatal("actual tool facts were not captured")
	}
	verified, err := checkexec.VerifyToolReceipt(dir, toolReceipts[0])
	if err != nil || !verified.Passed {
		t.Fatal("captured real check failed verification", err)
	}
	data, err := os.ReadFile(receipts[0])
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Profile *checkexec.Observation `json:"profile_observation"`
		Binding checkexec.Binding      `json:"check_binding"`
		Status  string                 `json:"status"`
	}
	json.Unmarshal(data, &r)
	if r.Status != "pass" || r.Profile == nil || r.Profile.ProfileDigest != p.Digest() || r.Profile.ReuseEligible || r.Binding != binding {
		t.Fatal("profile receipt manufactured reuse/authority", r)
	}
	p = profileFixture("printf modified > owned.txt")
	if err := ProfileCheckRunner([]checkexec.Profile{p}, binding)(ctx, root, p.Command); err == nil {
		t.Fatal("changed tested content was recorded PASS")
	}
	if _, err := os.Stat(filepath.Join(root, "owned.txt")); err != nil {
		t.Fatal("real mutation was not executed")
	}
	p = profileFixture("exec sleep 30")
	err = ProfileCheckRunner([]checkexec.Profile{p}, binding)(ctx, root, p.Command)
	var failure *checkexec.Failure
	if !errors.As(err, &failure) || failure.Category != "timeout" {
		t.Fatalf("profile timeout not enforced: %v", err)
	}
}

func TestMainRequiredCheckAlwaysRunsAndCapturesEachExecution(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	receipts := []string{}
	refs := []checkexec.ToolReceipt{}
	ctx := context.WithValue(context.Background(), checkEvidenceKey{}, checkEvidence{Directory: dir, Receipts: &receipts, ToolReceipts: &refs})
	p := profileFixture("printf checked")
	p.ReusePolicy = "same_input_same_responsibility"
	binding := checkexec.Binding{RuntimeID: "runtime", Generation: 1, AssignmentID: "assignment", Owner: "builder", ExecutionGeneration: 1, Responsibility: "main_required_integration"}
	runner := ProfileCheckRunner([]checkexec.Profile{p}, binding)
	for i := 0; i < 2; i++ {
		if err := runner(ctx, root, p.Command); err != nil {
			t.Fatal(err)
		}
	}
	if len(refs) != 2 || len(receipts) != 2 || refs[0].Path == refs[1].Path {
		t.Fatal("required checks were cached", refs)
	}
	for _, ref := range refs {
		verified, err := checkexec.VerifyToolReceipt(dir, ref)
		if err != nil || !verified.Passed {
			t.Fatal(err)
		}
		current := checkexec.ReuseRequest{Profile: p, Observation: verified.Observation, Binding: binding, Checkout: root}
		if d := checkexec.DecideReuse(dir, current, refs); d.WouldReuse || d.Reason != "independent_or_mandatory_domain_check" {
			t.Fatal(d)
		}
	}
	cp := Checkpoint{AssignmentID: binding.AssignmentID, BaselineGeneration: 1, CheckReceipts: receipts, ToolCheckReceipts: refs}
	// The original checkpoint keeps its original checks subdirectory.
	if err := VerifyCapturedChecks(filepath.Join(filepath.Dir(dir), "checkpoint.json"), cp); err == nil {
		t.Fatal("unrelated evidence directory qualified")
	}
}
