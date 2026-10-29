package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Install the real release launcher and a native CLI binary. The delivered
// action is executed by a fresh process after changing cwd, through Run's
// public routing, flag grammar and original completion consumer.
func repairExitInstalledRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project $(touch R2-EXPANDED) `touch R2-EXPANDED` 'quoted' space")
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("../..", rel))
		if err != nil {
			t.Fatal(err)
		}
		writeRepairLeadFile(t, root, rel, string(data))
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"init", "--root", root}, nil, &out, &diagnostic); code != 0 {
		t.Fatalf("init: %d %s", code, &diagnostic)
	}
	bin := filepath.Join(root, ".claude/bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "loop-harness-"+goruntime.GOOS+"-"+goruntime.GOARCH), "../../cmd/loop-harness")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("native build: %v %s", err, data)
	}
	launcher, err := os.ReadFile("../../tools/loop-harness-launcher.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "loop-harness"), launcher, 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func repairExitPublicStatus(t *testing.T, root string, relative bool) (map[string]any, string) {
	t.Helper()
	args := []string{"runtime", "repair", "status"}
	cwd := root
	if relative {
		cwd = filepath.Dir(root)
		args = append(args, "--root", filepath.Base(root))
	}
	command := exec.Command(filepath.Join(root, ".claude/bin/loop-harness"), args...)
	command.Dir = cwd
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("public status: %v %s", err, output)
	}
	var response map[string]any
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("status JSON: %v %s", err, output)
	}
	next := stringValue(response["next"])
	if board := mapFieldCLI(response, "board"); stringValue(board["next_action"]) != next {
		t.Fatalf("board and top next disagree: %s", output)
	}
	return response, next
}

func repairExitExtractCommand(t *testing.T, message string) string {
	t.Helper()
	for _, marker := range []string{"S9 original completion recovery: `", "resume `"} {
		if _, tail, ok := strings.Cut(message, marker); ok {
			if command, _, ok := strings.Cut(tail, "`."); ok {
				return command
			}
		}
	}
	t.Fatalf("missing command: %s", message)
	return ""
}

func repairExitExecuteElsewhere(t *testing.T, root, command string, success bool) {
	t.Helper()
	cwd := t.TempDir()
	invocation := exec.Command("sh", "-c", command)
	invocation.Dir = cwd
	output, err := invocation.CombinedOutput()
	if (err == nil) != success {
		t.Fatalf("actual recovery success=%t err=%v output=%s command=%s", success, err, output, command)
	}
	for _, dir := range []string{cwd, root} {
		if _, err := os.Stat(filepath.Join(dir, "R2-EXPANDED")); !os.IsNotExist(err) {
			t.Fatalf("shell interpreted literal path at %s: %v", dir, err)
		}
	}
}

func assertRepairExitReadyPublicRecovery(t *testing.T, root string) {
	t.Helper()
	before := repairDispatchRuntimeBytes(t, root)
	for _, relative := range []bool{false, true} {
		_, next := repairExitPublicStatus(t, root, relative)
		command := repairExitExtractCommand(t, next)
		if command != repairExitRecovery(root) {
			t.Fatalf("wrong original root command: %s", command)
		}
		repairExitExecuteElsewhere(t, root, command, false) // Original source blocker remains.
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func assertRepairExitClosedPublicRecovery(t *testing.T, root string, snapshot runtime.Snapshot) {
	t.Helper()
	guide := reviewPlanGuidePath(reviewPlanQuestionnairePath(root, snapshot.State))
	original, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []bool{false, true} {
		if err := os.Remove(guide); err != nil {
			t.Fatal(err)
		}
		before := repairDispatchRuntimeBytes(t, root)
		_, next := repairExitPublicStatus(t, root, relative)
		main := stageReviewPlanQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, snapshot)
		if !strings.Contains(main, repairExitRecovery(root)) || !strings.Contains(main, guide) {
			t.Fatalf("ordinary S7 entry lost original recovery: %s", main)
		}
		repairDispatchAssertUnchanged(t, root, before)
		repairExitExecuteElsewhere(t, root, repairExitExtractCommand(t, next), true)
		restored, err := os.ReadFile(guide)
		if err != nil || !bytes.Equal(original, restored) {
			t.Fatalf("actual original recovery did not restore guide: %v", err)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
}

func TestRepairExitRootRejectsUnprovenCoordinates(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	bound := map[string]any{"project_root": root, "dev_branch": "dev", "release_upstream": "main", "bound_commit": "accepted"}
	state := map[string]any{"bound_req": map[string]any{"workspace": bound}}
	if actual, err := repairExitMainRoot(root, state); err != nil || actual != root {
		t.Fatalf("bound root: %s %v", actual, err)
	}
	for _, candidate := range []string{other, filepath.Join(root, "missing")} {
		if _, err := repairExitMainRoot(candidate, state); err == nil {
			t.Fatalf("unproven root accepted: %s", candidate)
		}
	}
	bound["project_root"] = "relative"
	if _, err := repairExitMainRoot(root, state); err == nil {
		t.Fatal("relative authority accepted")
	}
	bound["project_root"] = root
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-workspace.json"), []byte(`{"control_root":"untrusted"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := repairExitMainRoot(root, state); err == nil {
		t.Fatal("Worker pointer accepted as Main")
	}
	delete(state, "bound_req")
	if _, err := repairExitMainRoot(root, state); err == nil {
		t.Fatal("missing authority accepted")
	}
}

// These are read-only projection negatives derived from real accepted records.
// They test selection PASS versus aggregate status, and registered/current
// entry interpretation; they do not claim the Writer produced these snapshots.
func assertRepairExitAcceptedProjectionCases(t *testing.T, root, input string, snapshot runtime.Snapshot, cases []string) {
	t.Helper()
	statePath := filepath.Join(root, ".claude/loop-state.json")
	originalState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.WriteFile(input, answer, 0644); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(statePath, originalState, 0644); err != nil {
			t.Error(err)
		}
	}()
	for _, kind := range cases {
		t.Run("accepted projection "+kind, func(t *testing.T) {
			var state map[string]any
			if err := json.Unmarshal(originalState, &state); err != nil {
				t.Fatal(err)
			}
			pointer := repairPointerCLI(state)
			switch kind {
			case "partial":
				pointer["status"] = "targeted_reverification"
				pointer["required_reverification_ids"] = append(stringSliceAny(pointer["required_reverification_ids"]), "remaining-independent-obligation")
			case "registered":
				mapFieldCLI(mapFieldCLI(state, "review"), "plan")["revision"] = 2
			case "departed":
				mapFieldCLI(state, "lifecycle")["state"] = "final_audit"
				mapFieldCLI(mapFieldCLI(state, "review"), "round_entry")["transition_id"] = "later-entry"
			}
			writeRepairLeadJSON(t, statePath, state)
			before := repairDispatchRuntimeBytes(t, root)
			var output, diagnostic bytes.Buffer
			if code := Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", input}, nil, &output, &diagnostic); code != 0 {
				t.Fatalf("accepted replay: %d %s %s", code, &output, &diagnostic)
			}
			var result map[string]any
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["input_ref"] != input || stringValue(result["accepted_ref"]) == "" {
				t.Fatalf("lost accepted original coordinates: %s", &output)
			}
			selections, err := repairTargetedSelections(root, state)
			if err != nil {
				t.Fatal(err)
			}
			current := runtime.Snapshot{State: state, Revision: snapshot.Revision}
			summary, err := nativeReturnSummary(root, current, nativeReturnSubject{kind: "Targeted", id: selections[0].performingAssignmentID, ref: input, targeted: &selections[0]})
			if err != nil {
				t.Fatal(err)
			}
			_, next := repairExitPublicStatus(t, root, false)
			for _, message := range []string{stringValue(result["next_action"]), summary, next} {
				if strings.Contains(message, "runtime repair handoff complete") || strings.Contains(message, "Complete and save the original questionnaire") || strings.Contains(message, "and Handoff to the next") {
					t.Fatalf("%s reissued completed/unqualified work: %s", kind, message)
				}
			}
			if kind == "registered" {
				if !strings.Contains(stringValue(result["next_action"]), "already registered") {
					t.Fatalf("lost registered Plan: %s", &output)
				}
				if message := stageReviewPlanQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, current); message != "" {
					t.Fatalf("registered planner got old duty: %s", message)
				}
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}

// The existing schema file-URL consumer rejects control characters in a project
// root. Isolate shell/launcher argv here instead of claiming that domain path
// supports newline roots or changing unrelated schema loading for R2.
func TestRepairExitLauncherPreservesNewlineRoot(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires POSIX shell")
	}
	root := filepath.Join(t.TempDir(), "project $(touch R2-EXPANDED) `touch R2-EXPANDED` 'quoted'\nline")
	bin := filepath.Join(root, ".claude/bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	launcher, err := os.ReadFile("../../tools/loop-harness-launcher.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "loop-harness"), launcher, 0755); err != nil {
		t.Fatal(err)
	}
	native := "loop-harness-" + goruntime.GOOS + "-" + goruntime.GOARCH
	if goruntime.GOOS == "windows" {
		native += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, native), []byte("#!/bin/sh\nprintf '%s\\0' \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"bound_req": map[string]any{"workspace": map[string]any{"project_root": root, "dev_branch": "dev", "release_upstream": "main", "bound_commit": "original"}}}
	main, err := repairExitMainRoot(root, state)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	command := exec.Command("sh", "-c", repairExitRecovery(main))
	command.Dir = cwd
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("launcher argv: %v %s", err, output)
	}
	got := bytes.Split(bytes.TrimSuffix(output, []byte{0}), []byte{0})
	want := []string{"runtime", "repair", "handoff", "complete", "--root", root}
	if len(got) != len(want) {
		t.Fatalf("argv count: got %q want %q", got, want)
	}
	for i, arg := range want {
		if string(got[i]) != arg {
			t.Fatalf("argv[%d]=%q want %q", i, got[i], arg)
		}
	}
	for _, dir := range []string{cwd, root} {
		if _, err := os.Stat(filepath.Join(dir, "R2-EXPANDED")); !os.IsNotExist(err) {
			t.Fatalf("shell interpreted literal root: %s %v", dir, err)
		}
	}
}
