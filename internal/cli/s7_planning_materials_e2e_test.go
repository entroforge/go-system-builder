package cli_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS7GuideCarriesRealIntegratedAssignmentRevisions(t *testing.T) {
	root, _, first := s6HandoffFixture(t, true)
	firstOutput := stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", first.AssignmentID, "--agent-id", first.OwnerAgentID)
	if !strings.Contains(firstOutput, "completion_ack recorded") {
		t.Fatalf("first actual integration not acknowledged: %s", firstOutput)
	}
	firstCheckpoint := assignmentrecord.IntegrationCheckpointPath(root, first.Reference())
	manifestBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(first.Manifest.Ref)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	const intent = "guide-r2"
	owner := "native-" + first.Task.ID + "-" + hex.EncodeToString([]byte(intent))
	manifest["manifest_id"] = "team-manifest-guide-r2"
	manifest["version"] = "v1.0.1"
	manifest["platform_team_id"] = "platform-guide-r2"
	manifest["workgroup_id"] = "workgroup-guide-r2"
	manifest["assignments"].([]any)[0].(map[string]any)["agent_id"] = owner
	manifestRef := ".claude/workgroups/guide-r2.json"
	stageJSON(t, root, manifestRef, manifest)
	previous, _ := json.Marshal(first.Reference())
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef,
		"--task-id", first.Task.ID, "--task", first.Task.Ref, "--revise-assignment", first.AssignmentID,
		"--revision-intent-key", intent, "--expected-record-ref", string(previous))
	second, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, owner)
	if err != nil || !found || second.Revision != 2 {
		t.Fatalf("original revision Writer: %+v %v", second, err)
	}
	stageCLI(t, "s6", "prepare", "--root", root, "--task", second.Task.ID, "--role", second.RoleFamily,
		"--revise-assignment", second.AssignmentID, "--revision-intent-key", intent)
	secondSource := filepath.Join(t.TempDir(), "second-worker")
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", "native/guide-r2", secondSource, "HEAD"); err != nil {
		t.Fatal(err)
	}
	brief, _, result := assignmentrecord.NativePaths(second)
	if code, out := nativeEvent(t, root, secondSource, "SubagentStart", filepath.Join(root, brief), false); code != 0 {
		t.Fatalf("r2 native start: %d %s", code, out)
	}
	if err := os.WriteFile(filepath.Join(secondSource, "src/pkg/proof.txt"), []byte("second actual contribution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), secondSource, "add", "src/pkg/proof.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), secondSource, "commit", "-m", "second contribution"); err != nil {
		t.Fatal(err)
	}
	nativeAnswer(t, secondSource, second)
	if code, out := nativeEvent(t, root, secondSource, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(secondSource, result), false); code != 0 {
		t.Fatalf("r2 original reception: %d %s", code, out)
	}
	secondOutput := stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", second.AssignmentID, "--agent-id", owner)
	if !strings.Contains(secondOutput, "completion_ack recorded") {
		t.Fatalf("second actual integration not acknowledged: %s", secondOutput)
	}
	// The remaining actual TASK keeps the batch open until its own delivery.
	stageCLI(t, "s6", "prepare", "--root", root, "--task", "TASK-039-02", "--role", "backend-builder")
	third, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-TASK-039-02")
	if err != nil || !found {
		t.Fatalf("second TASK registration: %+v %v", third, err)
	}
	thirdSource := filepath.Join(t.TempDir(), "third-worker")
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", "native/guide-third", thirdSource, "HEAD"); err != nil {
		t.Fatal(err)
	}
	thirdBrief, _, thirdResult := assignmentrecord.NativePaths(third)
	if code, out := nativeEvent(t, root, thirdSource, "SubagentStart", filepath.Join(root, thirdBrief), false); code != 0 {
		t.Fatalf("second TASK native start: %d %s", code, out)
	}
	if err := os.WriteFile(filepath.Join(thirdSource, "src/pkg/another.txt"), []byte("another actual contribution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), thirdSource, "add", "src/pkg/another.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), thirdSource, "commit", "-m", "another contribution"); err != nil {
		t.Fatal(err)
	}
	nativeAnswer(t, thirdSource, third)
	if code, out := nativeEvent(t, root, thirdSource, "SubagentStop", filepath.Join(root, thirdBrief)+"\n"+filepath.Join(thirdSource, thirdResult), false); code != 0 {
		t.Fatalf("second TASK original reception: %d %s", code, out)
	}
	final := stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", third.AssignmentID, "--agent-id", third.OwnerAgentID)
	if !strings.Contains(final, "TR-006") {
		t.Fatalf("actual S6 batch did not enter S7: %s", final)
	}
	path := review.PlanQuestionnairePath(root, stageSnapshot(t, root).State)
	body, err := os.ReadFile(strings.TrimSuffix(path, ".json") + ".md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{first.AssignmentID + " r1", second.AssignmentID + " r2", firstCheckpoint, assignmentrecord.IntegrationCheckpointPath(root, second.Reference()), "src/pkg/proof.txt", "Main's own changes and deleted paths"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("guide omitted actual r1/r2 material %q: %s", want, body)
		}
	}
}

func TestS7GuideUsesOriginalS6MaterialsAndMissingHistoryDoesNotBlockPlan(t *testing.T) {
	root, _, record := s6HandoffFixture(t)
	output := stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", record.AssignmentID, "--agent-id", record.OwnerAgentID)
	if !strings.Contains(output, "TR-006") {
		t.Fatalf("original integration did not enter S7: %s", output)
	}
	snapshot := stageSnapshot(t, root)
	path := review.PlanQuestionnairePath(root, snapshot.State)
	guide := strings.TrimSuffix(path, ".json") + ".md"
	body, err := os.ReadFile(guide)
	if err != nil {
		t.Fatalf("original S7 guide unavailable: %v", err)
	}
	checkpoint := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
	var resultRef string
	for _, raw := range snapshot.State["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "completion_report" && row["responsibility_id"] == "BUILD-WORK-PACKAGE" {
			resultRef, _ = row["path"].(string)
		}
	}
	if resultRef == "" {
		t.Fatal("original accepted S6 Result is unavailable")
	}
	for _, want := range []string{record.Task.Ref, record.AssignmentID, "r1", resultRef, checkpoint, "src/pkg/proof.txt", "Main's own changes and deleted paths"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("S7 guide omitted actual material %q: %s", want, body)
		}
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(resultRef))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(guide); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "s7", "draft", "--root", root, "--out", path)
	body, err = os.ReadFile(guide)
	if err != nil || !strings.Contains(string(body), resultRef) || !strings.Contains(string(body), "remain unknown") || strings.Contains(string(body), "registration will reject") {
		t.Fatalf("missing historical Result was not reported as unknown: %v %s", err, body)
	}
	authored := minimalReviewPlan(t, t.TempDir(), "review-plan-available-materials")
	professionalBytes, err := os.ReadFile(authored)
	if err != nil {
		t.Fatal(err)
	}
	draftBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var draft, professional map[string]any
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(professionalBytes, &professional); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"claims", "assignments", "e2e_coverage_state"} {
		draft[field] = professional[field]
	}
	plan, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, plan, 0600); err != nil {
		t.Fatal(err)
	}
	reception := entryQuestionnaireHook(t, root, "PostToolUse", path)
	if !strings.Contains(reception, "review-plan: registered") {
		t.Fatalf("missing historical material blocked original Plan reception: %s", reception)
	}
}
