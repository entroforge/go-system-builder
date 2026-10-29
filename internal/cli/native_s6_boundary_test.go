package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS6BriefSourceReadsStayInOriginalWorktree(t *testing.T) {
	root, source, record := nativeDeliveryFixture(t)
	brief, _, _ := assignmentrecord.NativePaths(record)
	data, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, ref := range record.ReadPaths {
		if !strings.Contains(text, "- Read worktree-relative: "+ref) || strings.Contains(text, "- Read "+filepath.Join(root, ref)) {
			t.Fatalf("input does not unambiguously name its worktree source: %s", ref)
		}
		before, err := os.ReadFile(filepath.Join(source, ref))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ref), append(before, []byte("\nLater Main change, outside this responsibility's original source.\n")...), 0600); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(source, ref))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("Main changed the brief's selected source: %s %v", ref, err)
		}
	}
	if !strings.Contains(text, "worktree-relative path "+record.Task.Ref) || !strings.Contains(text, filepath.Join(root, record.AgentDefinition.Ref)) {
		t.Fatal("TASK source and installed control method roots are not distinguished")
	}
}

func TestNativeS6BriefMinimalBlockerWorksBeforeAndAfterPlan(t *testing.T) {
	root, source, record := nativeDeliveryFixture(t)
	brief, plan, result := assignmentrecord.NativePaths(record)
	resultPath := filepath.Join(source, result)
	text := filepath.Join(root, brief) + "\n" + resultPath
	before := stageSnapshot(t, root)
	coordinatePath := filepath.Join(root, ".claude/assignments", record.AssignmentID+".json")
	coordinates, err := os.ReadFile(coordinatePath)
	if err != nil {
		t.Fatal(err)
	}
	if code, output := nativeEvent(t, root, source, "SubagentStop", text+"\nExternal fixture is unavailable.", false); code != 2 || !strings.Contains(output, resultPath) || !strings.Contains(output, `"disposition":"blocked"`) || !strings.Contains(output, "before or after forming a Plan") {
		t.Fatalf("missing Result does not explain an executable blocker handoff: %d %s", code, output)
	}
	data, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	_, handoff, found := strings.Cut(string(data), "Blocked handoff, before or after forming a Plan:")
	if !found {
		t.Fatal("brief omitted the blocker branch")
	}
	_, shape, found := strings.Cut(handoff, "```json\n")
	if !found {
		t.Fatal("brief omitted the actual blocker shape")
	}
	shape, _, found = strings.Cut(shape, "\n```")
	var answer map[string]any
	if !found || json.Unmarshal([]byte(shape), &answer) != nil {
		t.Fatal("brief's blocked Result cannot be decoded")
	}
	answer["summary"] = "Implementation cannot continue; fixture-based checks remain unfinished."
	answer["risks_blockers"] = []string{"Required external fixture is unavailable."}
	for _, withPlan := range []bool{false, true} {
		var planBefore []byte
		if withPlan {
			nativeAnswer(t, source, record)
			planBefore, err = os.ReadFile(filepath.Join(source, plan))
			if err != nil {
				t.Fatal(err)
			}
		}
		stageJSON(t, source, result, answer)
		resultBefore, _ := os.ReadFile(resultPath)
		if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 0 || !strings.Contains(output, "no completion receipt") || !strings.Contains(output, "Required external fixture is unavailable") {
			t.Fatalf("blocked handoff with Plan=%v: %d %s", withPlan, code, output)
		}
		if stageSnapshot(t, root).Revision != before.Revision {
			t.Fatal("minimal blocker created a reception or stage transition")
		}
		if after, _ := os.ReadFile(coordinatePath); !bytes.Equal(after, coordinates) {
			t.Fatal("minimal blocker changed assignment coordinates")
		}
		if after, _ := os.ReadFile(resultPath); !bytes.Equal(after, resultBefore) {
			t.Fatal("handoff rewrote the private Result")
		}
		if withPlan {
			if after, _ := os.ReadFile(filepath.Join(source, plan)); !bytes.Equal(after, planBefore) {
				t.Fatal("handoff rewrote the existing Plan")
			}
		}
	}
	// A legitimate blocker never waives the original worktree source binding.
	if _, err := workspace.Git(t.Context(), source, "checkout", "--orphan", "native/unrelated-source"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), source, "commit", "--allow-empty", "-m", "unrelated source"); err != nil {
		t.Fatal(err)
	}
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 0 || !strings.Contains(output, "does not contain the declared source commit") || strings.Contains(output, "blocker returned for Main") {
		t.Fatalf("minimal blocker bypassed original source checking: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("wrong-source blocker changed Runtime")
	}
}

func TestNativeS6FullBlockedResultRetainsOriginalValidationAndReceipt(t *testing.T) {
	root, source, record := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, record)
	brief, _, result := assignmentrecord.NativePaths(record)
	path := filepath.Join(source, result)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	answer["risks_blockers"] = []string{"External fixture still unavailable."}
	closing := answer["closing_assessment"].(map[string]any)
	closing["disposition"] = "blocked"
	closing["conditions"].([]any)[0].(map[string]any)["judgment"] = "blocked"
	row := answer["assertion_evidence"].([]any)[0].(map[string]any)
	row["evidence_refs"] = []string{}
	stageJSON(t, source, result, answer)
	text := filepath.Join(root, brief) + "\n" + path
	before := stageSnapshot(t, root).Revision
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 2 || strings.Contains(output, "blocker returned for Main") || !strings.Contains(output, "assertion_evidence") {
		t.Fatalf("blocked disposition bypassed full Result validation: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("invalid full blocked Result changed Runtime")
	}
	row["evidence_refs"] = []string{"src/pkg/proof.txt"}
	stageJSON(t, source, result, answer)
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 0 || strings.Contains(output, "blocker returned for Main") {
		t.Fatalf("valid historical full blocked report lost its original consumer: %d %s", code, output)
	}
	receipt := stageCLI(t, "runtime", "task-receipt", "--root", root, "--assignment-id", record.AssignmentID, "--agent-id", record.OwnerAgentID)
	if !strings.Contains(receipt, `"accepted":true`) || !strings.Contains(receipt, `"work_complete":false`) || !strings.Contains(receipt, `"integrated":false`) {
		t.Fatalf("full blocked receipt changed its acceptance/completion boundary: %s", receipt)
	}
}
