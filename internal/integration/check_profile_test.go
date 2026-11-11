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
func TestProfileMainRunsActualChecksWithoutSourceDriftGate(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	receipts := []string{}
	ctx := context.WithValue(context.Background(), checkEvidenceKey{}, checkEvidence{Directory: dir, Receipts: &receipts})
	p := profileFixture("printf modified > owned.txt")
	binding := checkexec.Binding{RuntimeID: "runtime", Generation: 1, AssignmentID: "assignment", Owner: "builder", ExecutionGeneration: 1, Responsibility: "main_required_integration"}
	if err := ProfileCheckRunner([]checkexec.Profile{p}, binding)(ctx, root, p.Command); err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 {
		t.Fatal("actual run did not publish receipt")
	}
	data, err := os.ReadFile(receipts[0])
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Binding checkexec.Binding `json:"check_binding"`
		Status  string            `json:"status"`
	}
	if err := json.Unmarshal(data, &r); err != nil || r.Status != "pass" || r.Binding != binding {
		t.Fatalf("real check receipt lost result or responsibility: %+v %v", r, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "owned.txt")); err != nil || string(data) != "modified" {
		t.Fatalf("real command was not executed: %q %v", data, err)
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
	ctx := context.WithValue(context.Background(), checkEvidenceKey{}, checkEvidence{Directory: dir, Receipts: &receipts})
	p := profileFixture("printf checked >> count.txt")
	p.ReusePolicy = "same_input_same_responsibility"
	binding := checkexec.Binding{RuntimeID: "runtime", Generation: 1, AssignmentID: "assignment", Owner: "builder", ExecutionGeneration: 1, Responsibility: "main_required_integration"}
	runner := ProfileCheckRunner([]checkexec.Profile{p}, binding)
	for i := 0; i < 2; i++ {
		if err := runner(ctx, root, p.Command); err != nil {
			t.Fatal(err)
		}
	}
	if len(receipts) != 2 || receipts[0] == receipts[1] {
		t.Fatalf("independent check receipts missing: %v", receipts)
	}
	data, err := os.ReadFile(filepath.Join(root, "count.txt"))
	if err != nil || string(data) != "checkedchecked" {
		t.Fatalf("actual checks did not both execute: %q %v", data, err)
	}
}
