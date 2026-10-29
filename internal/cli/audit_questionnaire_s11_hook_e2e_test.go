package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestS11QuestionnaireHookDeliversVerifiableS10PackageAndKeepsDecisionHuman(t *testing.T) {
	root, acceptancePath := s10QuestionnaireHookRoot(t)
	releasePath := prepareS10QuestionnaireReleaseAudit(t, root, acceptancePath)
	fillS10QuestionnaireAnswer(t, releasePath, "release_audit", "approved")

	request := map[string]any{
		"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write",
		"tool_input":    map[string]any{"file_path": releasePath},
		"tool_response": map[string]any{"file_path": releasePath},
	}
	// Existing optional professional audit rows must survive reception: an
	// explicitly unfinished observation cannot be erased by a display summary.
	answer := readS10QuestionnaireState(t, releasePath)
	answer["audit_areas"] = []any{map[string]any{
		"id": "verification_evidence", "conclusion": "unknown", "owner": "Main", "evidence_refs": []any{},
	}}
	pending, err := json.MarshalIndent(answer, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releasePath, pending, 0o644); err != nil {
		t.Fatal(err)
	}
	out := callS10QuestionnaireHook(t, root, request)
	pendingState := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
	if pendingState["lifecycle"].(map[string]any)["state"] != "release_audit" {
		t.Fatalf("reported unknown audit observation advanced to S11: %s", out)
	}
	preservedAnswer := readS10QuestionnaireState(t, releasePath)
	if preservedAnswer["audit_areas"].([]any)[0].(map[string]any)["conclusion"] != "unknown" {
		t.Fatal("receiver overwrote the actual unfinished audit observation")
	}
	answer["audit_areas"].([]any)[0].(map[string]any)["conclusion"] = "pass"
	completed, err := json.MarshalIndent(answer, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releasePath, completed, 0o644); err != nil {
		t.Fatal(err)
	}
	out = callS10QuestionnaireHook(t, root, request)
	if !strings.Contains(out, "recorded at") {
		t.Fatalf("release-audit save did not use the existing S10 consumer: %s", out)
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "awaiting_human_release" {
		t.Fatalf("release audit save did not complete its original TR-017: %#v; Hook=%s", state["lifecycle"], out)
	}
	acceptanceEvidencePath := s10QuestionnaireEvidencePath(t, root, state, "acceptance")
	releaseEvidencePath := s10QuestionnaireEvidencePath(t, root, state, "release_audit")
	decisionFiles, err := filepath.Glob(filepath.Join(root, ".claude", "decisions", "hd-s11-*.json"))
	if err != nil || len(decisionFiles) != 1 {
		t.Fatalf("TR017 did not prepare exactly its current application: %v %v", decisionFiles, err)
	}
	decisionPath := decisionFiles[0]
	decisionID := strings.TrimSuffix(filepath.Base(decisionPath), ".json")
	preparedBeforePre, err := os.ReadFile(decisionPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeS11State, _ := os.ReadFile(statePath)
	beforeS11Journal, _ := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	// TR017 already prepared the application; ordinary S11 Pre only displays it.
	request = map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"}
	out += callS10QuestionnaireHook(t, root, request)
	preparedAfterPre, _ := os.ReadFile(decisionPath)
	if !bytes.Equal(preparedBeforePre, preparedAfterPre) {
		t.Fatal("S11 Pre rewrote its stage-entry application")
	}
	for _, exactPath := range []string{acceptancePath, releasePath, strings.TrimSuffix(acceptancePath, ".json") + ".md", strings.TrimSuffix(releasePath, ".json") + ".md", decisionPath} {
		if !strings.Contains(out, exactPath) {
			t.Errorf("S11 Hook omitted exact current package/application path %s: %s", exactPath, out)
		}
	}
	for _, file := range []string{acceptanceEvidencePath, releaseEvidencePath} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("original recorded evidence is missing: %s %v", file, err)
		}
	}
	if _, err := os.Stat(decisionPath); err != nil {
		t.Fatalf("TR017 did not prepare the exact human application: %v", err)
	}

	if err := os.Remove(decisionPath); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"Stop", "TeammateIdle"} {
		payload := map[string]any{"hook_event_name": event, "cwd": root}
		if event == "TeammateIdle" {
			payload["teammate_name"] = "release-reviewer"
		}
		_, _, code := invokeS10QuestionnaireHook(t, root, payload)
		if code != 0 && code != 2 {
			t.Fatalf("%s Hook failed unexpectedly with code %d", event, code)
		}
		if _, err := os.Stat(decisionPath); !os.IsNotExist(err) {
			t.Fatalf("%s created the S11 human application: %v", event, err)
		}
	}

	afterS11State, _ := os.ReadFile(statePath)
	afterS11Journal, _ := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	if !bytes.Equal(beforeS11State, afterS11State) || !bytes.Equal(beforeS11Journal, afterS11Journal) {
		t.Fatal("S11 ordinary Pre/Stop/Idle mutated Runtime or journal")
	}
	request["hook_event_name"] = "PreToolUse"
	out = callS10QuestionnaireHook(t, root, request)
	if !strings.Contains(out, decisionPath) {
		t.Fatalf("read-only Pre omitted its exact missing application: %s", out)
	}
	if _, err := os.Stat(decisionPath); !os.IsNotExist(err) {
		t.Fatal("ordinary Pre recreated a missing application")
	}
	agentAttempt, err := json.Marshal(map[string]any{"decision_id": decisionID, "disposition": "", "reason": "No human choice has been given; no default approval."})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisionPath, agentAttempt, 0o644); err != nil {
		t.Fatal(err)
	}
	out = callS10QuestionnaireHook(t, root, request)
	if !strings.Contains(out, decisionPath) {
		t.Fatalf("repeat prepare omitted the existing application path: %s", out)
	}
	preserved, err := os.ReadFile(decisionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(preserved, agentAttempt) {
		t.Fatalf("repeat prepare overwrote the human-editable application: %q", preserved)
	}

	unregisteredRequest := map[string]any{
		"hook_event_name": "PostToolUse", "cwd": root, "agent_id": "unregistered-reviewer", "tool_name": "Write",
		"tool_input":    map[string]any{"file_path": decisionPath},
		"tool_response": map[string]any{"file_path": decisionPath},
	}
	stateBeforeActor, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	journalBeforeActor, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNoDecision := func(saved []byte) {
		t.Helper()
		stateAfter, stateErr := os.ReadFile(statePath)
		journalAfter, journalErr := os.ReadFile(journalPath)
		answerAfter, answerErr := os.ReadFile(decisionPath)
		if stateErr != nil || journalErr != nil || answerErr != nil || !bytes.Equal(stateBeforeActor, stateAfter) || !bytes.Equal(journalBeforeActor, journalAfter) || !bytes.Equal(saved, answerAfter) {
			t.Fatalf("event changed Runtime, journal or saved decision: state=%v journal=%v answer=%v", stateErr, journalErr, answerErr)
		}
	}
	// An unregistered actor is not routed to the Main questionnaire receiver.
	// This routing does not authenticate the identity behind a Main event.
	out = callS10QuestionnaireHook(t, root, unregisteredRequest)
	if strings.Contains(out, "TR-028") || strings.Contains(out, "Recorded and consumed") {
		t.Fatalf("unregistered actor received the empty draft: %s", out)
	}
	assertNoDecision(agentAttempt)

	humanChoice, err := json.Marshal(map[string]any{"disposition": "reject_acceptance", "reason": "Synthetic human request: reconsider the acceptance interpretation."})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisionPath, humanChoice, 0o644); err != nil {
		t.Fatal(err)
	}
	out = callS10QuestionnaireHook(t, root, unregisteredRequest)
	if strings.Contains(out, "TR-028") || strings.Contains(out, "Recorded and consumed") {
		t.Fatalf("unregistered actor received the non-empty draft: %s", out)
	}
	assertNoDecision(humanChoice)

	// Restore the empty draft before Main saves it. Only a subsequent explicit
	// synthetic human choice is carried through the original decision exit.
	if err := os.WriteFile(decisionPath, agentAttempt, 0o644); err != nil {
		t.Fatal(err)
	}
	request = map[string]any{
		"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write",
		"tool_input":    map[string]any{"file_path": decisionPath},
		"tool_response": map[string]any{"file_path": decisionPath},
	}
	out = callS10QuestionnaireHook(t, root, request)
	if !strings.Contains(out, "Draft only") {
		t.Fatalf("Main's empty-choice draft lacked feedback: %s", out)
	}
	assertNoDecision(agentAttempt)
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "awaiting_human_release" {
		t.Fatalf("empty-choice draft ended the human gateway: %#v", state["lifecycle"])
	}
	for _, entry := range state["evidence"].([]any) {
		row, _ := entry.(map[string]any)
		if row != nil && row["kind"] == "human_decision" {
			t.Fatalf("empty-choice draft registered human_decision evidence: %#v", row)
		}
	}
	if err := os.WriteFile(decisionPath, humanChoice, 0o644); err != nil {
		t.Fatal(err)
	}
	out = callS10QuestionnaireHook(t, root, request)
	if !strings.Contains(out, "TR-028") {
		t.Fatalf("saved explicit choice did not use its fixed original exit: %s", out)
	}
	returned := readS10QuestionnaireState(t, acceptancePath)
	if (returned["outcome"] != nil && returned["outcome"] != "") || returned["coverage_inventory"].([]any)[0].(map[string]any)["disposition"] != "pass" {
		t.Fatal("return lost observations or retained old final outcome")
	}
	if outcome := readS10QuestionnaireState(t, releasePath)["outcome"]; outcome != nil && outcome != "" {
		t.Fatal("acceptance rejection retained downstream final audit outcome")
	}
	refs := returned["coverage_inventory"].([]any)[0].(map[string]any)["source_refs"].([]any)
	foundDecision := false
	for _, ref := range refs {
		if ref == decisionPath {
			foundDecision = true
		}
	}
	if !foundDecision {
		t.Fatal("rework omitted the actual human decision material")
	}
	returned["coverage_inventory"].([]any)[0].(map[string]any)["oracle"] = "Synthetic author supplement retained across decision replay."
	supplement, _ := json.MarshalIndent(returned, "", "  ")
	if err := os.WriteFile(acceptancePath, supplement, 0o644); err != nil {
		t.Fatal(err)
	}
	callS10QuestionnaireHook(t, root, request)
	afterReplay, _ := os.ReadFile(acceptancePath)
	if !bytes.Equal(supplement, afterReplay) {
		t.Fatal("same consumed decision reset the author's new answer")
	}
	fillS10QuestionnaireAnswer(t, acceptancePath, "acceptance", "pass")
	callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": acceptancePath}})
	if readS10QuestionnaireState(t, statePath)["lifecycle"].(map[string]any)["state"] != "release_audit" {
		t.Fatal("corrected acceptance did not take its normal save exit")
	}
	// Keep the already reported audit observations; only the professional
	// author supplies the new final decision after the human-requested rework.
	auditAnswer := readS10QuestionnaireState(t, releasePath)
	auditAnswer["outcome"] = "approved"
	auditBytes, _ := json.MarshalIndent(auditAnswer, "", "  ")
	if err := os.WriteFile(releasePath, auditBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": releasePath}})
	if readS10QuestionnaireState(t, statePath)["lifecycle"].(map[string]any)["state"] != "awaiting_human_release" {
		t.Fatal("corrected audit did not return to the human gateway")
	}
	newDecisions, _ := filepath.Glob(filepath.Join(root, ".claude", "decisions", "hd-s11-*.json"))
	if len(newDecisions) != 2 {
		t.Fatalf("same-round reentry reused the consumed application: %v", newDecisions)
	}
	var currentDecision string
	for _, candidate := range newDecisions {
		if candidate != decisionPath {
			currentDecision = candidate
		}
	}
	approval, err := json.Marshal(map[string]any{"disposition": "approve", "reason": "Synthetic explicit human approval after the corrected review."})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentDecision, approval, 0o644); err != nil {
		t.Fatal(err)
	}
	out = callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Bash", "tool_input": map[string]any{"command": "script saved the current supplied application"}, "tool_response": map[string]any{}})
	finalState := readS10QuestionnaireState(t, statePath)
	if finalState["lifecycle"].(map[string]any)["state"] != "release_authorized" || !strings.Contains(out, "TR-025") {
		t.Fatalf("normal script save did not automatically record the explicit synthetic human choice: %s", out)
	}

}

func TestS10RoutedQuestionnaireOutcomesDoNotDirectlyAdvanceTheirCursor(t *testing.T) {
	t.Run("acceptance review_required", func(t *testing.T) {
		root, acceptancePath := s10QuestionnaireHookRoot(t)
		callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
		fillS10QuestionnaireAnswer(t, acceptancePath, "acceptance", "review_required")
		request := map[string]any{
			"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write",
			"tool_input":    map[string]any{"file_path": acceptancePath},
			"tool_response": map[string]any{"file_path": acceptancePath},
		}
		callS10QuestionnaireHook(t, root, request)
		state := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
		if lifecycle := state["lifecycle"].(map[string]any)["state"]; lifecycle == "release_audit" || lifecycle == "awaiting_human_release" {
			t.Fatalf("review_required save directly advanced the lifecycle: %#v", state["lifecycle"])
		}
		if !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") {
			t.Fatal("review_required professional answer did not reach the original acceptance evidence consumer")
		}
		callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
		state = readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
		lifecycle := state["lifecycle"].(map[string]any)["state"]
		if lifecycle == "release_audit" || lifecycle == "awaiting_human_release" || lifecycle == "release_authorized" {
			t.Fatalf("review_required outcome advanced onto the release path: %v", lifecycle)
		}
	})

	t.Run("release audit blocked", func(t *testing.T) {
		root, acceptancePath := s10QuestionnaireHookRoot(t)
		releasePath := prepareS10QuestionnaireReleaseAudit(t, root, acceptancePath)
		fillS10QuestionnaireAnswer(t, releasePath, "release_audit", "blocked")
		request := map[string]any{
			"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write",
			"tool_input":    map[string]any{"file_path": releasePath},
			"tool_response": map[string]any{"file_path": releasePath},
		}
		callS10QuestionnaireHook(t, root, request)
		state := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
		if lifecycle := state["lifecycle"].(map[string]any)["state"]; lifecycle == "awaiting_human_release" || lifecycle == "release_authorized" {
			t.Fatalf("blocked release-audit save directly advanced the lifecycle: %#v", state["lifecycle"])
		}
		if !hasS10QuestionnaireEvidence(root, state, "release_audit", "blocked") {
			t.Fatal("blocked professional answer did not reach the original release-audit evidence consumer")
		}
		callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
		state = readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
		lifecycle := state["lifecycle"].(map[string]any)["state"]
		if lifecycle == "awaiting_human_release" || lifecycle == "release_authorized" {
			t.Fatalf("blocked release-audit outcome advanced to the human release path: %v", lifecycle)
		}
	})
}

func prepareS10QuestionnaireReleaseAudit(t *testing.T, root, acceptancePath string) string {
	t.Helper()
	out := callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
	if !strings.Contains(out, acceptancePath) {
		t.Fatalf("acceptance Hook omitted its exact answer path: %s", out)
	}
	fillS10QuestionnaireAnswer(t, acceptancePath, "acceptance", "pass")
	out = callS10QuestionnaireHook(t, root, map[string]any{
		"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write",
		"tool_input":    map[string]any{"file_path": acceptancePath},
		"tool_response": map[string]any{"file_path": acceptancePath},
	})
	state := readS10QuestionnaireState(t, filepath.Join(root, ".claude", "loop-state.json"))
	if state["lifecycle"].(map[string]any)["state"] != "release_audit" {
		t.Fatalf("Controller did not advance the genuinely registered acceptance package: %#v; Hook=%s", state["lifecycle"], out)
	}
	releasePath := filepath.Join(root, "s10", "loop-test", "g1", "r1", "release_audit-manifest.json")
	if !strings.Contains(out, releasePath) {
		t.Fatalf("release-audit Hook omitted its exact answer path: %s", out)
	}
	return releasePath
}

func s10QuestionnaireEvidencePath(t *testing.T, root string, state map[string]any, kind string) string {
	t.Helper()
	rows := state["evidence"].([]any)
	for i := len(rows) - 1; i >= 0; i-- {
		raw := rows[i]
		entry, _ := raw.(map[string]any)
		if entry != nil && entry["kind"] == kind {
			relative, _ := entry["path"].(string)
			if relative == "" {
				t.Fatalf("%s evidence has no registered artifact path", kind)
			}
			return filepath.Join(root, filepath.FromSlash(relative))
		}
	}
	t.Fatalf("no %s package evidence is registered", kind)
	return ""
}

func hasS10QuestionnaireEvidence(root string, state map[string]any, kind, conclusion string) bool {
	for _, raw := range state["evidence"].([]any) {
		entry, _ := raw.(map[string]any)
		if entry == nil || entry["kind"] != kind {
			continue
		}
		path, _ := entry["path"].(string)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		var envelope map[string]any
		if json.Unmarshal(data, &envelope) == nil && envelope["conclusion"] == conclusion {
			return true
		}
	}
	return false
}

func invokeS10QuestionnaireHook(t *testing.T, root string, payload map[string]any) (string, string, int) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	event, _ := payload["hook_event_name"].(string)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", event, "--root", root}, bytes.NewReader(data), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}
