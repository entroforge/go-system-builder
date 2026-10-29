package workspace

import "testing"

func TestDispatchContextWorkerSubmissionPathsAreFrozenExactSets(t *testing.T) {
	legacy, err := (DispatchContext{}).WorkerSubmissionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Plan != LegacyPlanDraftPath || legacy.Answer != LegacyAnswerDraftPath || legacy.Completion != LegacyResultDraftPath || legacy.Commit != LegacyCommitRequestPath || legacy.RepairResult != LegacyRepairResultDraftPath {
		t.Fatalf("missing PlanPath must select the legacy exact path set: %#v", legacy)
	}

	current, err := (DispatchContext{
		PlanPath:      PlanDraftPath,
		WorkPacketRef: ".claude/inputs/work-packet.json",
		AnswerPath:    AnswerDraftPath,
		ResultPath:    ResultDraftPath,
	}).WorkerSubmissionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if current.Plan != PlanDraftPath || current.Answer != AnswerDraftPath || current.Completion != ResultDraftPath || current.Commit != CommitRequestPath || current.RepairResult != RepairResultDraftPath {
		t.Fatalf("new frozen context must select the loop-local exact path set: %#v", current)
	}
	review, err := (DispatchContext{Version: "1.0.0", WorkPacketRef: ".claude/inputs/work-packet.json", AnswerPath: AnswerDraftPath}).WorkerSubmissionPaths()
	if err != nil {
		t.Fatalf("resolve questionnaire-only S5 paths: %v", err)
	}
	if review.Plan != "" || review.Answer != AnswerDraftPath {
		t.Fatalf("S5 questionnaire context must expose its answer path without a generic PLAN path: %#v", review)
	}
	for _, path := range []string{PlanDraftPath, AnswerDraftPath, ResultDraftPath, CommitRequestPath, RepairResultDraftPath} {
		if !current.Contains(path) {
			t.Errorf("expected exact lifecycle path %q to be recognized", path)
		}
	}
	if current.Contains(".loop-submissions/extra.json") || current.Contains(".loop-submissions/nested/plan.json") {
		t.Fatal("unlisted .loop-submissions files must remain outside the lifecycle path set")
	}
	for _, path := range current.ControlPaths() {
		if path == AnswerDraftPath {
			t.Fatal("S5 answer must retain its separate packet authorization")
		}
	}
}

func TestDispatchContextRejectsUnknownOrMismatchedSubmissionPaths(t *testing.T) {
	for _, c := range []DispatchContext{
		{PlanPath: ".loop-submissions/other.json"},
		{PlanPath: PlanDraftPath, WorkPacketRef: ".claude/inputs/work-packet.json", AnswerPath: LegacyAnswerDraftPath},
		{PlanPath: PlanDraftPath, ResultPath: LegacyResultDraftPath},
	} {
		if _, err := c.WorkerSubmissionPaths(); err == nil {
			t.Errorf("expected invalid frozen path tuple to fail: %#v", c)
		}
	}
}

func TestWorkerSubmissionStatusExcludesOnlyFixedLoopDrafts(t *testing.T) {
	want := map[string]bool{
		":(exclude).loop-submissions/plan.json":          true,
		":(exclude).loop-submissions/stage-answer.json":  true,
		":(exclude).loop-submissions/completion.json":    true,
		":(exclude).loop-submissions/commit.json":        true,
		":(exclude).loop-submissions/repair-result.json": true,
	}
	for _, pattern := range WorkerSubmissionStatusExcludes() {
		delete(want, pattern)
		if pattern == ":(exclude).loop-submissions/**" {
			t.Fatal("a directory-wide .loop-submissions exclusion would hide arbitrary files")
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing exact local status exclusions: %#v", want)
	}
}
