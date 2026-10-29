package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestMainReviewPlanQuestionnaireSaveUsesOriginalRegistrationAndReplay(t *testing.T) {
	root := worktreeProjectRoot(t)
	path := filepath.Join(root, ".claude/submissions/loop-REQ-WORKTREE/g1/S7/r1/review-plan.json")
	context := entryQuestionnaireHook(t, root, "PreToolUse", path)
	if !strings.Contains(context, path) || strings.Contains(context, "s7 draft --out plan.json") {
		t.Fatalf("Main did not receive exact automatic plan questionnaire: %s", context)
	}
	guide := strings.TrimSuffix(path, ".json") + ".md"
	if !strings.Contains(context, guide) {
		t.Fatalf("Main lost planning guidance: %s", context)
	}
	guideBytes, guideErr := os.ReadFile(guide)
	if guideErr != nil || !strings.Contains(string(guideBytes), "candidate is not") || !strings.Contains(string(guideBytes), "verification_artifact_workspace") || !strings.Contains(string(guideBytes), "e2e-workspace/") {
		t.Fatalf("missing planning guidance: %v %s", guideErr, guideBytes)
	}
	draft, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entryQuestionnaireHook(t, root, "PreToolUse", path)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(draft, after) {
		t.Fatal("repeat observation overwrote professional plan draft")
	}
	// Professional answers are deterministic test data; this is not a model review.
	authored := minimalReviewPlan(t, t.TempDir(), "review-plan-questionnaire-save")
	data, err := os.ReadFile(authored)
	if err != nil {
		t.Fatal(err)
	}
	// Select one real reusable asset without hand-copying machine fields.
	casePath := "docs/design/prototypes/example/cases.json"
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(casePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, casePath), []byte(`{"cases":[{"id":"CASE-001"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	assetPath := filepath.Join(root, "example.spec.ts")
	if err := os.WriteFile(assetPath, []byte("original executable script"), 0o644); err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	e2e := answer["claims"].([]any)[2].(map[string]any)
	e2e["applicability"] = "required"
	e2e["source_refs"] = []string{casePath + "#CASE-001"}
	delete(e2e, "na_rationale")
	delete(e2e, "na_checklist_id")
	answer["assignments"] = append(answer["assignments"].([]any), map[string]any{"assignment_id": "assignment-e2e-1", "lens": "e2e", "claim_ids": []string{"claim-e2e-na"}, "execution_wave": "behavior", "non_overlap_boundary": "actual browser journey", "behavior_surface": "browser"})
	answer["e2e_coverage_state"] = "regression_available"
	answer["e2e_assets"] = []any{map[string]any{"case_ref": casePath + "#CASE-001", "path": "example.spec.ts", "selector_ref": "button[name=Save]", "route_ref": "/example", "environment": "local test browser"}}
	for _, source := range []string{casePath, "example.spec.ts"} {
		body, err := os.ReadFile(filepath.Join(root, source))
		if err != nil {
			t.Fatal(err)
		}
		answer["frozen_subjects"] = append(answer["frozen_subjects"].([]any), map[string]any{"path": source, "sha256": fmt.Sprintf("%x", sha256.Sum256(body)), "kind": "product_code"})
	}
	data, err = json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	result := entryQuestionnaireHook(t, root, "PostToolUse", path)
	if !strings.Contains(result, "review-plan-questionnaire-save") || !strings.Contains(result, path) {
		t.Fatalf("save did not return original registration/path: %s", result)
	}
	for _, expected := range []string{"prepares and registers", "manifest-draft", "assignment-e2e-1 waits", "static Claims"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("registered plan did not provide its actual preparation/dependency step %q: %s", expected, result)
		}
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(before, &state); err != nil {
		t.Fatal(err)
	}
	ptr, _ := state["review"].(map[string]any)["plan"].(map[string]any)
	if ptr == nil || ptr["plan_id"] != "review-plan-questionnaire-save" {
		t.Fatalf("original plan was not registered: %s", before)
	}
	if err := os.WriteFile(assetPath, []byte("later changed script"), 0o644); err != nil {
		t.Fatal(err)
	}
	replay := entryQuestionnaireHook(t, root, "PostToolUse", path)
	if !strings.Contains(replay, "already received") {
		t.Fatalf("duplicate was not original receipt replay: %s", replay)
	}
	after, err = os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("replay changed Runtime: %v", err)
	}
	data = bytes.Replace(data, []byte("REQ covered"), []byte("changed professional answer"), 1)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	conflict := entryQuestionnaireHook(t, root, "PostToolUse", path)
	if !strings.Contains(conflict, "named revision/rework") {
		t.Fatalf("changed registered plan was silently accepted: %s", conflict)
	}
	after, _ = os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("changed plan rewrote accepted Runtime")
	}
}

func TestMainReviewPlanQuestionnaireStopDoesNotPrepareAndInvalidSaveReturnsFeedback(t *testing.T) {
	root := worktreeProjectRoot(t)
	path := filepath.Join(root, ".claude/submissions/loop-REQ-WORKTREE/g1/S7/r1/review-plan.json")
	body, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "cwd": root})
	var out, errout bytes.Buffer
	code := cli.Run([]string{"hook", "--root", root, "--event", "Stop"}, bytes.NewReader(body), &out, &errout)
	if code != 0 && code != 2 {
		t.Fatalf("unexpected Stop code %d: %s", code, &errout)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stop created a plan questionnaire: %v", err)
	}
	entryQuestionnaireHook(t, root, "PreToolUse", path)
	before, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	result := entryQuestionnaireHook(t, root, "PostToolUse", path)
	if !strings.Contains(result, "original validation feedback") || !strings.Contains(result, path) {
		t.Fatalf("invalid draft did not return original correction feedback: %s", result)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("invalid draft changed Runtime")
	}
	// A valid answer cannot repair a missing frozen product source.
	validPath := minimalReviewPlan(t, t.TempDir(), "review-plan-source-failure")
	valid, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, valid, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "internal/example/service.go")); err != nil {
		t.Fatal(err)
	}
	result = entryQuestionnaireHook(t, root, "PostToolUse", path)
	if !strings.Contains(result, "Main owns source/environment/Writer recovery") || strings.Contains(result, "Correct the specific") {
		t.Fatalf("source failure was misattributed to the professional answer: %s", result)
	}
	after, _ = os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("source failure changed Runtime")
	}
}
