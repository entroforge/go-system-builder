package cli_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcceptanceExternalS7OriginalOutletAndMaterialRecovery(t *testing.T) {
	run := acceptanceRuntimeProcess(t)
	root, path := s10QuestionnaireHookRoot(t)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	state["lifecycle"].(map[string]any)["state"] = "verification"
	state["lifecycle"].(map[string]any)["phase"] = "clean"
	writeS10QuestionnaireFile(t, root, ".claude/loop-state.json", mustJSONForS6(t, state))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"runtime", "review-continue", "--root", root}, nil, &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "TR-009") || !strings.Contains(out.String(), path) {
		t.Fatalf("original continuation: %d %s %s", code, &out, &errOut)
	}
	promoted := readS10QuestionnaireState(t, statePath)
	if promoted["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatal("original TR-009 did not enter acceptance")
	}
	before, _ := os.ReadFile(statePath)
	draft := []byte("preserve exact professional draft\n")
	if err := os.WriteFile(path, draft, 0644); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"review-continue", "audit-prepare"} {
		out.Reset()
		errOut.Reset()
		if code := run([]string{"runtime", command, "--root", root}, nil, &out, &errOut); code != 0 {
			t.Fatalf("%s: %d %s %s", command, code, &out, &errOut)
		}
		after, _ := os.ReadFile(statePath)
		actual, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) || !bytes.Equal(draft, actual) {
			t.Fatal("replay modified original Runtime or professional report")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"runtime", "audit-prepare", "--root", root}, nil, &out, &errOut); code != 0 {
		t.Fatalf("missing report recovery: %d %s %s", code, &out, &errOut)
	}
	after, _ := os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("material recovery repeated stage transition")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptanceExternalS10SourceRepairPreservesStage(t *testing.T) {
	run := acceptanceRuntimeProcess(t)
	root, path := s10QuestionnaireHookRoot(t)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before, _ := os.ReadFile(statePath)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	state := readS10QuestionnaireState(t, statePath)
	req := state["bound_req"].(map[string]any)["path"].(string)
	source := filepath.Join(root, filepath.FromSlash(req))
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"runtime", "audit-prepare", "--root", root}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "audit-prepare") || !strings.Contains(errOut.String(), path) || !strings.Contains(errOut.String(), req) {
		t.Fatalf("failure was not actionable: %d %s %s", code, &out, &errOut)
	}
	after, _ := os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("preparation failure changed stage")
	}
	if diagnostic := submission.PlanningDiagnostic(root, state, "S7 acceptance continuation"); !strings.Contains(diagnostic, req) {
		t.Fatalf("original preparation diagnostic lost cause: %s", diagnostic)
	}
	if err := os.WriteFile(source, original, 0644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"runtime", "audit-prepare", "--root", root}, nil, &out, &errOut); code != 0 {
		t.Fatalf("retry: %d %s %s", code, &out, &errOut)
	}
	if diagnostic := submission.PlanningDiagnostic(root, state, "S7 acceptance continuation"); diagnostic != "" {
		t.Fatalf("successful recovery left stale failure: %s", diagnostic)
	}
}

func acceptanceRuntimeProcess(t *testing.T) func([]string, io.Reader, io.Writer, io.Writer) int {
	t.Helper()
	binary := os.Getenv("HOOK_ACCEPTANCE_BINARY")
	if binary == "" {
		binary = factory.BuildHarness(t)
	}
	return func(args []string, input io.Reader, out, diagnostic io.Writer) int {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = input, out, diagnostic
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("original runtime recovery exceeded test guard: %v", ctx.Err())
		}
		var status *exec.ExitError
		if errors.As(err, &status) {
			return status.ExitCode()
		}
		if err != nil {
			t.Fatalf("start original runtime recovery: %v", err)
		}
		return 0
	}
}
