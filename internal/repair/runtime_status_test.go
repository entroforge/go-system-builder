package repair

import (
	"strings"
	"testing"
)

func TestRepairResultStatusStopsOnAnyNonPassResult(t *testing.T) {
	status, action := repairResultNextState(false, false, []string{"repair-assignment-unit-2"})
	if status != "blocked" {
		t.Fatalf("non-pass result must block the batch before all assignments report, got %q", status)
	}
	if !strings.Contains(action, "S8") || !strings.Contains(action, "blocker") {
		t.Fatalf("blocked result must expose the recovery route, got %q", action)
	}
}

func TestTargetedFailureNextActionNamesExecutableS8Reentry(t *testing.T) {
	action := targetedFailureNextAction("investigation-case-1", "fail_same_cause", ArtifactRef{Path: ".claude/review/repair/reverification/reverify-1.json"})
	if !strings.Contains(action, "runtime investigation route") || !strings.Contains(action, "--route investigate_more") || !strings.Contains(action, "--reassessment-evidence") || !strings.Contains(action, "reverify-1.json") {
		t.Fatalf("targeted failure must name the executable S8 re-entry command, got %q", action)
	}
	blocked := targetedFailureNextAction("investigation-case-1", "blocked", ArtifactRef{Path: "reverify-1.json"})
	if strings.Contains(blocked, "runtime investigation route") {
		t.Fatalf("blocked verification should first resolve its local blocker, got %q", blocked)
	}
}
