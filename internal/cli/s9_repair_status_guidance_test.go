package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS9PlanningGuidanceUsesOriginalDivisionAndIssuedOwner(t *testing.T) {
	root, plan, _, _, division := repairExecutionPlanningFixture(t)
	firstID, secondID := plan.Assignments[0].AssignmentID, plan.Assignments[1].AssignmentID
	status := func() (runtime.Snapshot, string, map[string]any) {
		t.Helper()
		snapshot := repairDispatchSnapshot(t, root)
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"runtime", "repair", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("public repair status code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		var response map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		board := mapFieldCLI(response, "board")
		next := stringValue(response["next"])
		if next != stringValue(board["next_action"]) {
			t.Fatalf("status and board disagree: %q vs %q", next, board["next_action"])
		}
		guidance := buildGuidance(root, snapshot.State, "SessionStart", policy.Input{Event: "SessionStart", AgentID: "Main"})
		if guidance.Action != next {
			t.Fatalf("Hook Next and repair status disagree: %q vs %q", guidance.Action, next)
		}
		missing := stringSliceAny(board["missing_reports"])
		if len(missing) != 2 {
			t.Fatalf("missing_reports must continue to count both unreceived Plans: %#v", board["missing_reports"])
		}
		return snapshot, next, response
	}

	_, next, _ := status()
	for _, want := range []string{division, "Main selects and saves", "unselected", "PreToolUse", "PostToolUse", "Stop/SubagentStop"} {
		if !strings.Contains(next, want) {
			t.Fatalf("unselected original division lacks %q: %s", want, next)
		}
	}
	if strings.Contains(next, "No current original PlanReport is indexed") || strings.Contains(next, "workspace prepare") {
		t.Fatalf("ordinary unselected owner was described as missing preparation: %s", next)
	}

	rows := []repairDispatchChoice{{AssignmentID: firstID, RoleFamily: "backend-builder"}, {AssignmentID: secondID, RoleFamily: "backend-builder"}}
	repairDispatchSave(t, root, division, rows[:1])
	snapshot, next, _ := status()
	packets, err := runtime.CurrentRepairPlanReportPackets(root, snapshot.State, "")
	if err != nil || len(packets) != 1 {
		t.Fatalf("one actual original PlanReport must be issued: %v %v", packets, err)
	}
	brief, _, _ := nativeS9Paths(packets[0])
	for _, want := range []string{firstID, packets[0].Binding.OwnerAgentID, filepath.Join(root, brief), "initial native Agent/worktree dispatch", "same actor", "unselected"} {
		if !strings.Contains(next, want) {
			t.Fatalf("selected/issued guidance lacks %q: %s", want, next)
		}
	}
	if strings.Contains(next, "No current original PlanReport is indexed") || strings.Contains(next, "workspace prepare") {
		t.Fatalf("issued original was described as absent or as legacy startup: %s", next)
	}

	// The passive original dispatch can register the other owner without
	// issuing its PlanReport. This is an actual selected-but-unissued boundary,
	// not an inference from the missing_reports counter.
	if err := persistRepairLeadQuestionnaire(division, rows); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if handled, code := runObservedRepairDispatch(root, repairLeadSaveInput(root, division, "Main"), &stdout, &stderr); !handled || code != 0 {
		t.Fatalf("passive original dispatch: handled=%t code=%d stdout=%s stderr=%s", handled, code, stdout.String(), stderr.String())
	}
	snapshot, next, _ = status()
	owner := stringMapCLI(repairPointerCLI(snapshot.State)["assignment_owners"])[secondID]
	if owner == "" || !strings.Contains(next, "No current original PlanReport is indexed for "+secondID) || !strings.Contains(next, owner) || !strings.Contains(next, division) {
		t.Fatalf("selected but genuinely unissued original not reported: %s", next)
	}
}

func TestS7AndS9CatalogNextUseNaturalReception(t *testing.T) {
	for _, tc := range []struct{ state, phase string }{
		{"verification", "running"}, {"bug_resolution", "planning"}, {"bug_resolution", "reproducing"},
		{"bug_resolution", "fixing"}, {"bug_resolution", "targeted_reverification"},
	} {
		_, _, next := projectNext(tc.state, tc.phase, t.TempDir())
		for _, want := range []string{"PreToolUse", "PostToolUse", "Stop/SubagentStop"} {
			if !strings.Contains(next, want) {
				t.Fatalf("%s/%s omits natural reception %q: %s", tc.state, tc.phase, want, next)
			}
		}
		if strings.Contains(next, "return its exact reference") || strings.Contains(next, "for SubagentStop reception") {
			t.Fatalf("%s/%s requires extra return or Stop-only reception: %s", tc.state, tc.phase, next)
		}
	}
}
