package assignment

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestValidateNativeOwnedCheckReceiptBindsRecordContextActivationAndSource(t *testing.T) {
	fixture := nativeCheckSourceFixture(t, true)
	source, err := ResolveFrozenNativeCheckSource(fixture.root, fixture.state, fixture.record)
	if err != nil {
		t.Fatal(err)
	}
	check := fixture.record.ClosingContract.WorkerChecks[0]
	receipt := NativeOwnedCheckReceipt{
		CheckIndex: check.Index, Command: check.Command,
		Record:    fixture.record.Reference(),
		RuntimeID: fixture.record.RuntimeID, BaselineGeneration: fixture.record.BaselineGeneration,
		AssignmentID: fixture.record.AssignmentID, OwnerAgentID: fixture.record.OwnerAgentID,
		ExecutionGeneration: 0, NativeContextGeneration: 2, NativeContextRef: ".claude/evidence/native-context.json",
		ActivationRef: ".claude/evidence/activation.json",
		Checkout:      source.Checkout, SourceBranch: source.Branch, TargetBranch: source.TargetBranch,
		ToolReceipt: checkexec.ToolReceipt{
			Path:     filepath.Join(fixture.root, ".claude/evidence/check-runs/attempt/receipt.json"),
			Checkout: source.Checkout, Command: check.Command,

			Binding: checkexec.Binding{RuntimeID: fixture.record.RuntimeID, Generation: fixture.record.BaselineGeneration,
				AssignmentID: fixture.record.AssignmentID, Owner: fixture.record.OwnerAgentID,
				ExecutionGeneration: 0, Responsibility: "owned_check"},
		},
	}
	if err := ValidateNativeOwnedCheckReceipt(fixture.record, 2, receipt.NativeContextRef, receipt.ActivationRef, source, receipt); err != nil {
		t.Fatalf("valid native check receipt rejected: %v", err)
	}
	mutations := map[string]func(*NativeOwnedCheckReceipt){
		"record revision":     func(r *NativeOwnedCheckReceipt) { r.Record.Revision++ },
		"context generation":  func(r *NativeOwnedCheckReceipt) { r.NativeContextGeneration++ },
		"context path":        func(r *NativeOwnedCheckReceipt) { r.NativeContextRef = "../context.json" },
		"tool execution kind": func(r *NativeOwnedCheckReceipt) { r.ToolReceipt.Binding.ExecutionGeneration = 1 },
		"tool responsibility": func(r *NativeOwnedCheckReceipt) { r.ToolReceipt.Binding.Responsibility = "main_required_integration" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := receipt
			mutate(&candidate)
			if err := ValidateNativeOwnedCheckReceipt(fixture.record, 2, receipt.NativeContextRef, receipt.ActivationRef, source, candidate); err == nil {
				t.Fatal("mutated native check receipt was accepted")
			}
		})
	}
}
