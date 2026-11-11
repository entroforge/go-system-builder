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

func TestUnnumberedS10AndS11ConsumeTheirOriginalSource(t *testing.T) {
	root, acceptancePath := s10QuestionnaireHookRootWithIdentity(t, "review-plan-entry-first")
	releasePath := prepareS10QuestionnaireReleaseAudit(t, root, acceptancePath)
	save := func(path string) string {
		return callS10QuestionnaireHook(t, root, map[string]any{
			"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write",
			"tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{"file_path": path},
		})
	}
	fillS10QuestionnaireAnswer(t, releasePath, "release_audit", "approved")
	out := save(releasePath)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "awaiting_human_release" {
		t.Fatalf("S10 original save failed: %s", out)
	}
	var auditID string
	for _, raw := range state["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] != "acceptance" && row["kind"] != "release_audit" {
			continue
		}
		if row["review_plan_id"] != "review-plan-entry-first" {
			t.Fatalf("lost actual S7 source: %#v", row)
		}
		if _, number := row["review_round"]; number {
			t.Fatalf("new report wrote numeric history: %#v", row)
		}
		if row["kind"] == "release_audit" {
			auditID = row["id"].(string)
		}
		envelope := readS10QuestionnaireState(t, filepath.Join(root, row["path"].(string)))
		if _, number := envelope["review_round"]; number {
			t.Fatalf("new envelope wrote numeric history: %#v", envelope)
		}
	}
	applications, err := filepath.Glob(filepath.Join(root, ".claude", "decisions", "hd-s11-*.json"))
	if err != nil || len(applications) != 1 {
		t.Fatalf("original S11 application missing: %v %v", applications, err)
	}
	application := readS10QuestionnaireState(t, applications[0])
	application["disposition"] = "approve"
	data, _ := json.Marshal(application)
	if err = os.WriteFile(applications[0], data, 0600); err != nil {
		t.Fatal(err)
	}
	out = save(applications[0])
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "release_authorized" {
		t.Fatalf("actual synthetic human choice was not consumed: %s", out)
	}
	decision := readS10QuestionnaireState(t, applications[0])
	if decision["release_audit_id"] != auditID {
		t.Fatalf("human decision lost its actual report source: %#v", decision)
	}
	// A later S7 responsibility at the same baseline cannot inherit that PASS or decision.
	r := state["review"].(map[string]any)
	r["round_entry"].(map[string]any)["plan_id"] = "review-plan-entry-next"
	r["plan"].(map[string]any)["plan_id"] = "review-plan-entry-next"
	r["plan"].(map[string]any)["status"] = "running"
	r["assignments"] = map[string]any{}
	state["lifecycle"].(map[string]any)["state"] = "acceptance"
	data, _ = json.Marshal(state)
	if err = os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	save(acceptancePath)
	save(applications[0])
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"s10", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("status: %s", stderr.String())
	}
	var status map[string]any
	if err = json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["acceptance"].(map[string]any)["state"] != "missing" {
		t.Fatalf("old PASS became current: %s", stdout.String())
	}
	state = readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatalf("old material advanced new responsibility: %#v", state["lifecycle"])
	}
}
