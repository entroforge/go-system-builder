package assignment

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestValidateNativeOwnedCheckReceiptBindsRecordContextActivationAndSource(t *testing.T) {
	fixture := nativeCheckSourceFixture(t, true)
	source, err := ResolveFrozenNativeCheckSource(fixture.root, fixture.state, fixture.record)
	if err != nil {
		t.Fatal(err)
	}
	recordSHA, err := fixture.record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	check := fixture.record.ClosingContract.WorkerChecks[0]
	const contextSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const activationSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const proofSHA = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	receipt := NativeOwnedCheckReceipt{
		CheckIndex: check.Index, Command: check.Command, ProfileDigest: check.Profile.Digest(),
		Record: fixture.record.Reference(), RecordSHA256: recordSHA,
		RuntimeID: fixture.record.RuntimeID, BaselineGeneration: fixture.record.BaselineGeneration,
		AssignmentID: fixture.record.AssignmentID, OwnerAgentID: fixture.record.OwnerAgentID,
		ExecutionGeneration: 0, NativeContextGeneration: 2, NativeContextRef: ".claude/evidence/native-context.json",
		NativeContextSHA256: contextSHA, ActivationRef: ".claude/evidence/activation.json", ActivationSHA256: activationSHA,
		Checkout: source.Checkout, SourceBranch: source.Branch, TargetBranch: source.TargetBranch,
		SourceHead: source.Head, SourceSnapshotSHA256: proofSHA,
		ToolReceipt: checkexec.ToolReceipt{
			Path:   filepath.Join(fixture.root, ".claude/evidence/check-runs/attempt/receipt.json"),
			SHA256: proofSHA, LogSHA256: proofSHA, Checkout: source.Checkout, Command: check.Command,
			ProfileDigest: check.Profile.Digest(),
			Binding: checkexec.Binding{RuntimeID: fixture.record.RuntimeID, Generation: fixture.record.BaselineGeneration,
				AssignmentID: fixture.record.AssignmentID, Owner: fixture.record.OwnerAgentID,
				ExecutionGeneration: 0, Responsibility: "owned_check"},
		},
	}
	if err := ValidateNativeOwnedCheckReceipt(fixture.record, 2, receipt.NativeContextRef, contextSHA, receipt.ActivationRef, activationSHA, source, receipt); err != nil {
		t.Fatalf("valid native check receipt rejected: %v", err)
	}
	mutations := map[string]func(*NativeOwnedCheckReceipt){
		"record revision":     func(r *NativeOwnedCheckReceipt) { r.Record.Revision++ },
		"context generation":  func(r *NativeOwnedCheckReceipt) { r.NativeContextGeneration++ },
		"context path":        func(r *NativeOwnedCheckReceipt) { r.NativeContextRef = "../context.json" },
		"activation digest":   func(r *NativeOwnedCheckReceipt) { r.ActivationSHA256 = strings.Repeat("d", 64) },
		"source head":         func(r *NativeOwnedCheckReceipt) { r.SourceHead = "other" },
		"tool execution kind": func(r *NativeOwnedCheckReceipt) { r.ToolReceipt.Binding.ExecutionGeneration = 1 },
		"tool responsibility": func(r *NativeOwnedCheckReceipt) { r.ToolReceipt.Binding.Responsibility = "main_required_integration" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := receipt
			mutate(&candidate)
			if err := ValidateNativeOwnedCheckReceipt(fixture.record, 2, receipt.NativeContextRef, contextSHA, receipt.ActivationRef, activationSHA, source, candidate); err == nil {
				t.Fatal("mutated native check receipt was accepted")
			}
		})
	}
}
