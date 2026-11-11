package cli

import "testing"

func TestNativeS8TaskIdentityUsesExplicitOriginalResponsibility(t *testing.T) {
	const owner = "investigator-case-1"
	const hypothesis = "hypothesis-case-1"
	for _, tc := range []struct {
		name, text      string
		match, conflict bool
	}{
		{"explicit", "Investigate `hypothesis-case-1` (owner `investigator-case-1`) at a mistyped/path", true, false},
		{"structured dispatch", `Assignment: {"owner_agent_id":"investigator-case-1","hypothesis_id":"hypothesis-case-1"}`, true, false},
		{"relative owner is not labelled", "investigator-case-1 investigates hypothesis-case-1", false, false},
		{"other owner", "Investigate `hypothesis-case-1` (owner `investigator-case-2`)", false, true},
		{"other hypothesis", "Investigate `hypothesis-case-2` (owner `investigator-case-1`)", false, true},
		{"multiple explicit dispatches", "Investigate `hypothesis-case-1` (owner `investigator-case-1`) and investigate `hypothesis-case-2` (owner `investigator-case-2`)", false, true},
		{"other context is not a dispatch", "Investigate `hypothesis-case-1` (owner `investigator-case-1`); compare hypothesis-case-2 and owner `investigator-case-2` as context.", true, false},
		{"assignment is not hypothesis", "owner `investigator-case-1` assignment-hypothesis-case-1", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			match, conflict := nativeS8TaskIdentity(tc.text, owner, hypothesis)
			if match != tc.match || conflict != tc.conflict {
				t.Fatalf("match=%v conflict=%v", match, conflict)
			}
		})
	}
	if !nativeS8TaskPath("read `.claude/submissions/x/answer.json`", "/repo", "/repo/.claude/submissions/x/answer.json") {
		t.Fatal("registered relative path was not recognized")
	}
}
