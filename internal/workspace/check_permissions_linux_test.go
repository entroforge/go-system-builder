package workspace

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

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
	if observation.Permissions == nil {
		t.Fatal("missing actual permission observation")
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
	// Historical Bootstrap bytes are not authenticated. A changed installed
	// file does not itself reject another real isolated attempt.
	settings := filepath.Join(e.Path, ".claude/settings.json")
	changedSettings := []byte("{}")
	if err := os.WriteFile(settings, changedSettings, 0600); err != nil {
		t.Fatal(err)
	}
	previous := ref
	ref = checkexec.ToolReceipt{}
	changedPath, changedErr := b.RunCheck(ctx, e, 0, io.Discard)
	if changedPath == "" || changedPath == previous.Path || ref.Path != changedPath {
		t.Fatalf("changed settings did not produce a new actual attempt: path=%q receipt=%q error=%v", changedPath, ref.Path, changedErr)
	}
	changedReceipt, err := checkexec.VerifyToolReceipt(filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"), ref)
	if err != nil {
		t.Fatal(err)
	}
	if changedReceipt.Observation.Permissions == nil || changedReceipt.Observation.ReuseEligible {
		t.Fatal("changed-settings attempt lost original constraints or enabled reuse")
	}
	if (changedErr == nil) != changedReceipt.Passed {
		t.Fatalf("changed-settings attempt error and receipt disagree: error=%v passed=%t", changedErr, changedReceipt.Passed)
	}
	if !changedReceipt.Observation.Permissions.Available && (changedErr == nil || changedReceipt.Passed) {
		t.Fatal("unavailable isolation used a fallback after settings changed")
	}
	if got, err := os.ReadFile(settings); err != nil || string(got) != string(changedSettings) {
		t.Fatalf("changed settings were not preserved: %q, %v", got, err)
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
