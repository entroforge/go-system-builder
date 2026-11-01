package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestWorkspaceToolReceiptBoundedRegistrationAndExactResponsibility(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	p := checkexec.Profile{ID: "CP-OWN", Version: "v1", CheckIndex: 0, Command: "true", Location: "worker", TimeoutSeconds: 1, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "same_input_same_responsibility"}
	e.CheckLocation = "worker"
	e.CheckProfiles = []checkexec.Profile{p}
	e.Checks = []string{p.Command}
	observation, err := checkexec.ObserveProfile(ctx, e.Path, p)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks/check-fixture")
	os.MkdirAll(dir, 0700)
	log := filepath.Join(dir, "output.log")
	os.WriteFile(log, []byte("fixture output"), 0600)
	binding := CheckBinding(e)
	data, err := checkexec.SealReceipt(map[string]any{"version": 2, "command": p.Command, "status": "pass", "output": log, "profile_observation": observation, "check_binding": binding})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "receipt.json")
	os.WriteFile(path, data, 0600)
	var ref checkexec.ToolReceipt
	capture := checkexec.WithReceiptCapture(checkexec.WithBinding(checkexec.WithProfile(context.Background(), p), binding), func(r checkexec.ToolReceipt) { ref = r })
	if err := checkexec.CaptureReceipt(capture, path, e.Path, data); err != nil {
		t.Fatal(err)
	}
	if err := b.AcceptToolReceipt(&e, ref); err != nil {
		t.Fatal(err)
	}
	if err := b.AcceptToolReceipt(&e, ref); err != nil || len(e.ToolCheckReceipts) != 1 {
		t.Fatal("duplicate registration", err)
	}
	// Distinct real invocations must not collapse just because both receipts
	// carry no historical digest.
	dir2 := filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks/second-check")
	if err := os.MkdirAll(dir2, 0700); err != nil {
		t.Fatal(err)
	}
	log2 := filepath.Join(dir2, "output.log")
	if err := os.WriteFile(log2, []byte("second output"), 0600); err != nil {
		t.Fatal(err)
	}
	data2, err := checkexec.SealReceipt(map[string]any{"version": 2, "command": p.Command, "status": "pass", "output": log2, "profile_observation": observation, "check_binding": binding})
	if err != nil {
		t.Fatal(err)
	}
	path2 := filepath.Join(dir2, "receipt.json")
	if err := os.WriteFile(path2, data2, 0600); err != nil {
		t.Fatal(err)
	}
	var second checkexec.ToolReceipt
	capture2 := checkexec.WithReceiptCapture(checkexec.WithBinding(checkexec.WithProfile(context.Background(), p), binding), func(r checkexec.ToolReceipt) { second = r })
	if err := checkexec.CaptureReceipt(capture2, path2, e.Path, data2); err != nil {
		t.Fatal(err)
	}
	if err := b.AcceptToolReceipt(&e, second); err != nil || len(e.ToolCheckReceipts) != 2 {
		t.Fatalf("distinct actual invocation lost: %v", err)
	}
	other := ref
	other.Binding.Owner = "another"
	if err := b.AcceptToolReceipt(&e, other); err == nil {
		t.Fatal("registered foreign responsibility")
	}
	other = ref
	other.Command = "other command"
	if err := b.AcceptToolReceipt(&e, other); err == nil {
		t.Fatal("registered a different command")
	}
	e.ToolCheckReceipts = make([]checkexec.ToolReceipt, 64)
	if err := b.AcceptToolReceipt(&e, ref); err != nil || len(e.ToolCheckReceipts) != 64 {
		t.Fatal("unbounded tool index", err)
	}
	projection := b.CheckReuse(ctx, e)
	if len(projection) != 1 || projection[0].Decision.Action != "run" {
		t.Fatal("shadow advice changed actual execution", projection)
	}
}
