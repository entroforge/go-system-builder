package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestS7OriginalExitContinuationAndS10MaterialRecovery(t *testing.T) {
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
	code := cli.Run([]string{"runtime", "review-continue", "--root", root}, nil, &out, &errOut)
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
		if code := cli.Run([]string{"runtime", command, "--root", root}, nil, &out, &errOut); code != 0 {
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
	if code := cli.Run([]string{"runtime", "audit-prepare", "--root", root}, nil, &out, &errOut); code != 0 {
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

func TestS10MaterialRecoveryNamesFailureAndPreservesStage(t *testing.T) {
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
	code := cli.Run([]string{"runtime", "audit-prepare", "--root", root}, nil, &out, &errOut)
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
	if code := cli.Run([]string{"runtime", "audit-prepare", "--root", root}, nil, &out, &errOut); code != 0 {
		t.Fatalf("retry: %d %s %s", code, &out, &errOut)
	}
	if diagnostic := submission.PlanningDiagnostic(root, state, "S7 acceptance continuation"); diagnostic != "" {
		t.Fatalf("successful recovery left stale failure: %s", diagnostic)
	}
}

func TestS7OriginalExitRefusesMissingCleanRoundEvidence(t *testing.T) {
	root, path := s10QuestionnaireHookRoot(t)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	state["lifecycle"].(map[string]any)["state"] = "verification"
	state["lifecycle"].(map[string]any)["phase"] = "clean"
	for _, raw := range state["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "clean_round" {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(row["path"].(string)))); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeS10QuestionnaireFile(t, root, ".claude/loop-state.json", mustJSONForS6(t, state))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := cli.Run([]string{"runtime", "review-continue", "--root", root}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(out.String(), "review-continue") || !strings.Contains(out.String(), "pending") {
		t.Fatalf("missing original gate was not refused: %d %s %s", code, &out, &errOut)
	}
	current := readS10QuestionnaireState(t, statePath)
	if current["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatal("failed original gate promoted stage")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed gate prepared questionnaire: %v", err)
	}
}

func TestS10RecoveryRejectsDirectoryAndObservationRemainsReadOnly(t *testing.T) {
	root, path := s10QuestionnaireHookRoot(t)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"SessionStart", "PreToolUse", "Stop"} {
		output := callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": event, "cwd": root, "tool_name": "Read"})
		if !strings.Contains(output, "not a regular file") || !strings.Contains(output, "audit-prepare") || !strings.Contains(output, path) {
			t.Fatalf("%s did not diagnose directory: %s", event, output)
		}
		after, _ := os.ReadFile(statePath)
		entries, err := os.ReadDir(path)
		if !bytes.Equal(before, after) || err != nil || len(entries) != 0 {
			t.Fatalf("%s observation modified current responsibility or placeholder: %v", event, err)
		}
	}
	for _, command := range []string{"review-continue", "audit-prepare"} {
		var out, errOut bytes.Buffer
		if code := cli.Run([]string{"runtime", command, "--root", root}, nil, &out, &errOut); code == 0 {
			t.Fatalf("%s reported success for a directory: %s %s", command, &out, &errOut)
		}
		if !strings.Contains(out.String()+errOut.String(), "not a regular file") {
			t.Fatalf("%s omitted original preparation failure: %s %s", command, &out, &errOut)
		}
		after, _ := os.ReadFile(statePath)
		if !bytes.Equal(before, after) {
			t.Fatalf("%s changed stage during failed preparation", command)
		}
	}
}

func TestS7S10RecoveryCommandsAppearInUsageAndHelp(t *testing.T) {
	for _, args := range [][]string{{"runtime"}, {"runtime", "--help"}, {"runtime", "review-continue", "--help"}, {"runtime", "audit-prepare", "--help"}} {
		var out, errOut bytes.Buffer
		code := cli.Run(args, nil, &out, &errOut)
		text := out.String() + errOut.String()
		if len(args) < 3 {
			for _, command := range []string{"review-continue", "audit-prepare"} {
				if !strings.Contains(text, command) {
					t.Fatalf("%v omitted %s: %s", args, command, text)
				}
			}
		} else if code != 0 || !strings.Contains(text, "--root") || !strings.Contains(text, "original") {
			t.Fatalf("%v help did not describe original recovery: %d %s", args, code, text)
		}
	}
}
