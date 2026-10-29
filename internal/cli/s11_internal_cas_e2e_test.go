package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestS11OriginalCASAndMainRecoveryPreserveHumanChoice(t *testing.T) {
	root, acceptance := s10QuestionnaireHookRoot(t)
	release := prepareS10QuestionnaireReleaseAudit(t, root, acceptance)
	fillS10QuestionnaireAnswer(t, release, "release_audit", "approved")
	callS10QuestionnaireHook(t, root, map[string]any{
		"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write",
		"tool_input": map[string]any{"file_path": release}, "tool_response": map[string]any{"file_path": release},
	})
	before := stageSnapshot(t, root)
	if before.State["lifecycle"].(map[string]any)["state"] != "awaiting_human_release" {
		t.Fatal("fixture did not reach the original S11 gateway")
	}
	paths, err := filepath.Glob(filepath.Join(root, ".claude/decisions/hd-s11-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("original application paths=%v err=%v", paths, err)
	}
	// Synthetic explicit choice in a disposable repository; no real human
	// authorization, deployment or product work is performed by this test.
	const reason = "Synthetic human choice: authorize this exact release."
	stageJSON(t, root, strings.TrimPrefix(paths[0], root+string(filepath.Separator)), map[string]any{"disposition": "approve", "reason": reason})
	answerBefore, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	_, err = writer.UpdateCurrent(runtime.Mutation{
		EventID: "evt-s11-unrelated-observation", TransitionID: "MILESTONE-REFRESH", Event: "milestone_refreshed",
		Actor: "Main", IdempotencyKey: "s11-unrelated-observation", RetainLastTransition: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	stateAfterObservation, _ := os.ReadFile(statePath)
	journalAfterObservation, _ := os.ReadFile(journalPath)
	args := []string{"runtime", "human-decision", "--root", root, "--actor", "Main", "--disposition", "approve", "--reason", reason}
	var out, diagnostic bytes.Buffer
	stale := append(append([]string(nil), args...), "--expected-revision", strconv.Itoa(before.Revision))
	if code := cli.Run(stale, nil, &out, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "stale runtime revision") || !strings.Contains(diagnostic.String(), "Main") || strings.Contains(diagnostic.String(), "runtime reconcile") {
		t.Fatalf("original CAS or truthful ownership changed: code=%d stderr=%s stdout=%s", code, &diagnostic, &out)
	}
	stateAfterReject, _ := os.ReadFile(statePath)
	journalAfterReject, _ := os.ReadFile(journalPath)
	answerAfterReject, _ := os.ReadFile(paths[0])
	if !bytes.Equal(stateAfterObservation, stateAfterReject) || !bytes.Equal(journalAfterObservation, journalAfterReject) || !bytes.Equal(answerBefore, answerAfterReject) {
		t.Fatal("rejected stale request changed the decision, Runtime or journal")
	}
	// Main checks the current application, then resumes the existing command
	// with the same human choice. The author never rewrites or resaves it.
	out.Reset()
	diagnostic.Reset()
	if code := cli.Run(args, nil, &out, &diagnostic); code != 0 {
		t.Fatalf("Main could not continue the original current application: code=%d stderr=%s stdout=%s", code, &diagnostic, &out)
	}
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["state"] != "release_authorized" || after.Revision != before.Revision+2 {
		t.Fatalf("wrong decision transition: revision=%d/%d lifecycle=%v", after.Revision, before.Revision, after.State["lifecycle"])
	}
	var decisions int
	for _, raw := range after.State["evidence"].([]any) {
		e := raw.(map[string]any)
		if e["kind"] == "human_decision" {
			decisions++
			if e["consumed_by"] != "TR-025" || e["id"] != strings.TrimSuffix(filepath.Base(paths[0]), ".json") {
				t.Fatalf("recovery consumed another decision: %v", e)
			}
		}
	}
	if decisions != 1 {
		t.Fatalf("expected one original decision, got %d", decisions)
	}
	stageCLI(t, args...)
	if stageSnapshot(t, root).Revision != after.Revision {
		t.Fatal("original decision replay committed again")
	}
}
