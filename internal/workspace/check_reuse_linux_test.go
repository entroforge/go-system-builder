package workspace

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestEnabledWorkerActualRunThenExactReuseOnSupportedHost(t *testing.T) {
	b, e, state, ctx := workerFixture(t)
	p := checkexec.Profile{ID: "CP-STATIC", Version: "v1", CheckIndex: 0, Command: "test -f input.md", Location: "worker", TimeoutSeconds: 10, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "same_input_same_responsibility", ReuseMode: "enabled"}
	e.Checks = []string{p.Command}
	e.CheckProfiles = []checkexec.Profile{p}
	e.CheckLocation = "worker"
	for _, dir := range []string{"agents", "skills"} {
		if err := os.MkdirAll(filepath.Join(b.MainRoot, ".claude", dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Bootstrap(e, executable); err != nil {
		t.Fatal(err)
	}
	e.BootstrapSHA256, err = BootstrapDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	state["workspace"] = Encode(b)
	saveControlState(t, b.MainRoot, state)
	observed, err := b.observeCopiedCheck(ctx, e, p)
	if err != nil {
		t.Fatal(err)
	}
	if !observed.Permissions.Available {
		t.Skipf("actual original host isolation unavailable; no fallback: %s", observed.Permissions.Diagnostic)
	}
	var captured *checkexec.ToolReceipt
	ctx = checkexec.WithReceiptCapture(ctx, func(r checkexec.ToolReceipt) { captured = &r })
	first, err := b.RunCheck(ctx, e, 0, io.Discard)
	if err != nil || captured == nil {
		t.Fatal("first enabled request did not execute", err)
	}
	if err := b.AcceptToolReceipt(&e, *captured); err != nil {
		t.Fatal(err)
	}
	captured = nil
	second, err := b.RunCheck(ctx, e, 0, io.Discard)
	if err != nil || second != first || captured != nil {
		t.Fatal("same actual inputs were rerun or a new PASS was issued", err)
	}
	ctx = checkexec.WithReuseGuard(ctx, checkexec.ReuseGuard{Fresh: true})
	fresh, err := b.RunCheck(ctx, e, 0, io.Discard)
	if err != nil || fresh == first || captured == nil {
		t.Fatal("fresh request reused", err)
	}
	ctx = checkexec.WithReuseGuard(ctx, checkexec.ReuseGuard{})
	if err := os.WriteFile(filepath.Join(e.Path, "new-input.txt"), []byte("changed actual tree"), 0600); err != nil {
		t.Fatal(err)
	}
	captured = nil
	changed, err := b.RunCheck(ctx, e, 0, io.Discard)
	if err != nil || changed == first || captured == nil {
		t.Fatal("changed input was reused", err)
	}
}
