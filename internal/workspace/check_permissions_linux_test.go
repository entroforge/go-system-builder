package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestShellRuntimeLibrariesAndOriginalFilterHaveActualDigests(t *testing.T) {
	digest, err := shellLibrariesDigest(context.Background(), []string{})
	if err != nil || len(digest) != 64 {
		t.Fatal("actual ELF dependency closure unavailable", err)
	}
	program, err := checkSocketProgram()
	if err != nil {
		t.Fatal(err)
	}
	f, err := checkSocketFilter()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	defer os.Remove(f.Name())
	data, err := os.ReadFile(f.Name())
	if err != nil || checkexec.Hash(data) != checkexec.Hash(program) {
		t.Fatal("observed filter differs from actual runner", err)
	}
}

func TestCopiedCheckObservationUsesActualCopyAndHostProbe(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	p := checkexec.Profile{ID: "CP-STATIC", Version: "v1", CheckIndex: 0, Command: "test -f input.md", Location: "worker", TimeoutSeconds: 10, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "same_input_same_responsibility"}
	e.Checks = []string{p.Command}
	e.CheckProfiles = []checkexec.Profile{p}
	e.CheckLocation = "worker"
	// Non-default directory mode is normalized by the actual copy algorithm.
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
	nested := filepath.Join(e.Path, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "file.txt"), []byte("actual source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0511); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(nested, 0700)
	observation, err := b.observeCopiedCheck(ctx, e, p)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Permissions == nil || observation.PermissionDigest != observation.Permissions.Digest() {
		t.Fatal("missing actual permission observation")
	}
	source, err := checkexec.ObserveProfile(ctx, e.Path, p)
	if err != nil || source.SubjectDigest == observation.SubjectDigest {
		t.Fatal("authoring tree substituted for normalized tested copy", err)
	}
	if observation.Permissions.Available && !observation.Permissions.Known() {
		t.Fatal("host probe overstated incomplete constraints", observation.Permissions)
	}
	if !observation.Permissions.Available && observation.Permissions.Diagnostic == "" {
		t.Fatal("unavailable original isolation had no diagnostic")
	}
	if observation.ReuseEligible {
		t.Fatal("permission capture enabled cache")
	}
}

func TestProfiledWorkerActualAttemptCapturesPermissionFactsWithoutFallback(t *testing.T) {
	b, e, state, ctx := workerFixture(t)
	p := checkexec.Profile{ID: "CP-STATIC", Version: "v1", CheckIndex: 0, Command: "test -f input.md", Location: "worker", TimeoutSeconds: 10, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "same_input_same_responsibility"}
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
	var ref checkexec.ToolReceipt
	ctx = checkexec.WithReceiptCapture(ctx, func(r checkexec.ToolReceipt) { ref = r })
	path, runErr := b.RunCheck(ctx, e, 0, io.Discard)
	if path == "" || ref.Path != path {
		t.Fatal("actual attempt did not capture original receipt", runErr)
	}
	verified, err := checkexec.VerifyToolReceipt(filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"), ref)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Observation.Permissions == nil || verified.Observation.ReuseEligible {
		t.Fatal("actual original constraints missing or reuse enabled")
	}
	if runErr != nil && verified.Passed {
		t.Fatal("original isolated failure turned into PASS")
	}
	if runErr == nil && (!verified.Passed || !verified.Observation.Permissions.Known()) {
		t.Fatal("success did not retain actual isolation facts")
	}
	if !verified.Observation.Permissions.Available {
		if runErr == nil || verified.Passed {
			t.Fatal("unavailable isolation used a fallback")
		}
		t.Logf("actual original host isolation unavailable: %s", verified.Observation.Permissions.Diagnostic)
	}
	if runErr == nil {
		if err := b.AcceptToolReceipt(&e, ref); err != nil {
			t.Fatal(err)
		}
		// Shadow always performs another actual original invocation.
		previous := ref
		second, err := b.RunCheck(ctx, e, 0, io.Discard)
		if err != nil || second == previous.Path || ref.Path != second {
			t.Fatal("shadow skipped actual execution", err)
		}
	}
	// Tampering with installed authority fails before executing again.
	if err := os.WriteFile(filepath.Join(e.Path, ".claude/settings.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunCheck(ctx, e, 0, io.Discard); err == nil {
		t.Fatal("changed Bootstrap was accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	if original["version"] != float64(2) {
		t.Fatal("profile attempt lost v2 format")
	}
}
