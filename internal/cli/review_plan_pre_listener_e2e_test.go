package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/review"
)

func TestMainReviewPlanPreReceivesReadyOriginal(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		for _, identity := range []string{"legacy", "plan"} {
			for _, assets := range []int{1, 2} {
				if (event == "PreToolUse") != (assets == 1) {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%d", event, identity, assets), func(t *testing.T) {
					testMainReviewPlanAssetsOriginal(t, event, identity, assets)
				})
			}
		}
	}
}

func testMainReviewPlanAssetsOriginal(t *testing.T, event, identity string, assets int) {
	root := worktreeProjectRoot(t)
	state := stageSnapshot(t, root).State
	if identity == "plan" {
		state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-questionnaire-save", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
		raw, _ := json.Marshal(state)
		if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := review.PlanQuestionnairePath(root, state)
	context := entryQuestionnaireHook(t, root, "PreToolUse", path)
	if !strings.Contains(context, path) || strings.Contains(context, "s7 draft --out plan.json") {
		t.Fatalf("Main did not receive exact automatic plan questionnaire: %s", context)
	}
	guide := strings.TrimSuffix(path, ".json") + ".md"
	if !strings.Contains(context, guide) {
		t.Fatalf("Main lost planning guidance: %s", context)
	}
	guideBytes, guideErr := os.ReadFile(guide)
	if guideErr != nil || !strings.Contains(string(guideBytes), "candidate is not") || !strings.Contains(string(guideBytes), "verification_artifact_workspace") || !strings.Contains(string(guideBytes), "e2e-workspace/") || !strings.Contains(string(guideBytes), "optional historical source information") {
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
	if assets == 2 {
		// A historical row remains readable beside a new row with no machine fields.
		answer["e2e_assets"] = append(answer["e2e_assets"].([]any), map[string]any{"case_ref": casePath + "#CASE-002", "path": "second.spec.ts", "asset_id": "legacy-asset", "sha256": strings.Repeat("f", 64), "selector_ref": "button[name=Load]", "route_ref": "/second", "environment": "local test browser"})
	}
	if identity == "plan" {
		answer["schema_version"] = "1.1.0"
		delete(answer, "review_round")
	}
	// Reception uses declared professional facts even if selected sources have
	// changed or are absent. The second row never had a CASE or executable file.
	if err := os.Remove(filepath.Join(root, casePath)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("later changed executable"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var result string
	if event == "PreToolUse" {
		result = s7MainNaturalPre(t, root)
	} else {
		result = entryQuestionnaireHook(t, root, event, path)
	}
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
	var registeredState map[string]any
	if err := json.Unmarshal(before, &registeredState); err != nil {
		t.Fatal(err)
	}
	ptr, _ := registeredState["review"].(map[string]any)["plan"].(map[string]any)
	if ptr == nil || ptr["plan_id"] != "review-plan-questionnaire-save" {
		t.Fatalf("original plan was not registered: %s", before)
	}
	accepted, _, err := review.LoadPlan(root, registeredState)
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted.E2EAssets) != assets || accepted.E2EAssets[0].AssetID != "" {
		t.Fatalf("reception synthesized machine fields: %+v", accepted.E2EAssets)
	}
	beforePointer := review.PlanPointerFromState(registeredState)
	if out := s7MainNaturalPre(t, root); strings.Contains(out, "review-plan: revised") {
		t.Fatalf("same original answer was mistaken for a revision: %s", out)
	}
	if afterPointer := review.PlanPointerFromState(stageSnapshot(t, root).State); !reflect.DeepEqual(beforePointer, afterPointer) {
		t.Fatalf("same original answer changed accepted pointer: before=%+v after=%+v", beforePointer, afterPointer)
	}
}

// The listener receives a previous save while observing an unrelated ordinary
// tool; no file_path or synthetic save signal is present on this Pre event.
func s7MainNaturalPre(t *testing.T, root string) string {
	t.Helper()
	input, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Bash", "tool_input": map[string]any{"command": "git status"}})
	var out, diag bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(input), &out, &diag); code != 0 {
		t.Fatalf("Pre tool denied: %d %s %s", code, &out, &diag)
	}
	return out.String()
}

func TestMainReviewPlanPreWhitespaceExpansionMatchesOriginalConsumer(t *testing.T) {
	root := worktreeProjectRoot(t)
	path := filepath.Join(root, ".claude/submissions/loop-REQ-WORKTREE/g1/S7/r1/review-plan.json")
	entryQuestionnaireHook(t, root, "PreToolUse", path)
	authored := minimalReviewPlan(t, t.TempDir(), "review-plan-pre-oversize")
	data, err := os.ReadFile(authored)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, bytes.Repeat([]byte(" "), (20<<20)+1-len(data))...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	out := s7MainNaturalPre(t, root)
	if !strings.Contains(out, "review-plan: registered") {
		t.Fatalf("whitespace expansion changed original plan reception: %s", out)
	}
	if review.PlanPointerFromState(stageSnapshot(t, root).State) == nil {
		t.Fatal("canonicalizable original answer was not registered")
	}
}

func TestMainReviewPlanPreLargeReplayPreservesAcceptedPlan(t *testing.T) {
	root := worktreeProjectRoot(t)
	path := filepath.Join(root, ".claude/submissions/loop-REQ-WORKTREE/g1/S7/r1/review-plan.json")
	entryQuestionnaireHook(t, root, "PreToolUse", path)
	authored := minimalReviewPlan(t, t.TempDir(), "review-plan-pre-large-replay")
	data, err := os.ReadFile(authored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	out := entryQuestionnaireHook(t, root, "PostToolUse", path)
	if !strings.Contains(out, "review-plan: registered") {
		t.Fatalf("original plan not registered: %s", out)
	}
	before := stageSnapshot(t, root)
	beforePlan := review.PlanPointerFromState(before.State)
	if beforePlan == nil {
		t.Fatal("original plan pointer is missing after registration")
	}
	planPath := filepath.Join(root, filepath.FromSlash(beforePlan.Path))
	registeredPlan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read accepted plan: %v", err)
	}
	registrationsBefore := s7ReviewPlanRegistrations(t, root)
	if registrationsBefore != 1 {
		t.Fatalf("expected one original plan registration, got %d", registrationsBefore)
	}
	data = append(data, bytes.Repeat([]byte(" "), (20<<20)+1-len(data))...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	out = s7MainNaturalPre(t, root)
	if strings.Contains(out, "exceeds 20 MiB") || strings.Contains(out, "revision/rework") {
		t.Fatalf("Pre rejected an accepted same-answer replay: %s", out)
	}
	after := stageSnapshot(t, root)
	if afterPlan := review.PlanPointerFromState(after.State); !reflect.DeepEqual(afterPlan, beforePlan) {
		t.Fatalf("same-answer replay changed accepted plan: before=%+v after=%+v", beforePlan, afterPlan)
	}
	if got := s7ReviewPlanRegistrations(t, root); got != registrationsBefore {
		t.Fatalf("same-answer replay added a plan registration: before=%d after=%d", registrationsBefore, got)
	}
	if got, err := os.ReadFile(planPath); err != nil || !bytes.Equal(got, registeredPlan) {
		t.Fatalf("same-answer replay changed accepted plan bytes: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("original answer changed: %v", err)
	}
}

// After the first reception the same original answer remains observable on
// ordinary tools, including saves that did not report a path in their Post.
func TestMainReviewPlanPreReceivesChangedOriginal(t *testing.T) {
	root := worktreeProjectRoot(t)
	path := review.PlanQuestionnairePath(root, stageSnapshot(t, root).State)
	entryQuestionnaireHook(t, root, "PreToolUse", path)
	authored := minimalReviewPlan(t, t.TempDir(), "review-plan-pre-revision")
	data, err := os.ReadFile(authored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if out := entryQuestionnaireHook(t, root, "PostToolUse", path); !strings.Contains(out, "review-plan: registered") {
		t.Fatalf("original plan was not registered: %s", out)
	}
	before := review.PlanPointerFromState(stageSnapshot(t, root).State)
	data = bytes.Replace(data, []byte("REQ covered"), []byte("changed assertion from ordinary save"), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if out := s7MainNaturalPre(t, root); !strings.Contains(out, "review-plan: revised") {
		t.Fatalf("ordinary Pre did not receive the changed answer: %s", out)
	}
	afterState := stageSnapshot(t, root).State
	after := review.PlanPointerFromState(afterState)
	accepted, _, err := review.LoadPlan(root, afterState)
	if err != nil || after == nil || before == nil || after.Revision <= before.Revision || accepted.Claims[0].Assertion != "changed assertion from ordinary save" {
		t.Fatalf("changed professional content was not received: before=%+v after=%+v plan=%+v err=%v", before, after, accepted, err)
	}
	if out := s7MainNaturalPre(t, root); strings.Contains(out, "review-plan: revised") {
		t.Fatalf("same original was revised twice: %s", out)
	}
	if again := review.PlanPointerFromState(stageSnapshot(t, root).State); !reflect.DeepEqual(after, again) {
		t.Fatalf("same-answer observation changed accepted plan: before=%+v after=%+v", after, again)
	}
}

func TestMainReviewPlanPrePreservesReceiptWhenAcceptedContentMissing(t *testing.T) {
	root := worktreeProjectRoot(t)
	path := review.PlanQuestionnairePath(root, stageSnapshot(t, root).State)
	entryQuestionnaireHook(t, root, "PreToolUse", path)
	authored := minimalReviewPlan(t, t.TempDir(), "review-plan-pre-missing-accepted")
	data, err := os.ReadFile(authored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if out := entryQuestionnaireHook(t, root, "PostToolUse", path); !strings.Contains(out, "review-plan: registered") {
		t.Fatalf("original plan was not registered: %s", out)
	}
	before := review.PlanPointerFromState(stageSnapshot(t, root).State)
	if before == nil {
		t.Fatal("missing accepted pointer")
	}
	if err := os.Remove(filepath.Join(root, before.Path)); err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("REQ covered"), []byte("changed assertion"), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	out := s7MainNaturalPre(t, root)
	if strings.Contains(out, "review-plan: revised") || !strings.Contains(strings.ToLower(out), "preserve") {
		t.Fatalf("missing accepted body was not reported as unknown: %s", out)
	}
	if after := review.PlanPointerFromState(stageSnapshot(t, root).State); !reflect.DeepEqual(before, after) {
		t.Fatalf("missing accepted body changed the receipt: before=%+v after=%+v", before, after)
	}
}

// Count original ReviewPlan registrations, not unrelated Runtime updates made
// by the same PreToolUse event (for example milestone refreshes).
func s7ReviewPlanRegistrations(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	if err != nil {
		t.Fatalf("read Runtime journal: %v", err)
	}
	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event struct {
			TransitionID string `json:"transition_id"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode Runtime journal event: %v", err)
		}
		if event.TransitionID == "REVIEW-PLAN" {
			count++
		}
	}
	return count
}

func mustReadPlanAsset(t *testing.T, root, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
