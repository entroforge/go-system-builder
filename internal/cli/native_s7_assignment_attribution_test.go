package cli

import (
	"fmt"
	"strings"
	"testing"
)

// Regression coverage for the S7 reception deadlock observed in a real
// simulation: Main's dispatch prose carried a several-hundred-character
// registered answer path that model transcription corrupted by a character,
// while the machine-minted assignment id survived verbatim. Attribution must
// select the responsibility from that id when no protocol identity parses,
// and must stay fail-closed when the prose names several assignments.
func TestNativeS7AssignmentNamedBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		assignment string
		want       bool
	}{
		{"empty assignment", "unrelated text", "", false},
		{"exact id in prose", "You are the independent S7 Reviewer for assignment-qa-testability-oracle (lens: qa).", "assignment-qa-testability-oracle", true},
		{"id after sentence separator", "first line.\nassignment-qa-testability-oracle is yours.", "assignment-qa-testability-oracle", true},
		{"id at text start", "assignment-qa-testability-oracle: review the claim.", "assignment-qa-testability-oracle", true},
		{"id at text end", "review for assignment-qa-testability-oracle", "assignment-qa-testability-oracle", true},
		{"different assignment", "review for assignment-qa-maintainability", "assignment-qa-testability-oracle", false},
		{"prefix inside longer id", "review for assignment-qa-testability-oracle-2", "assignment-qa-testability-oracle", false},
		{"suffix inside longer id", "review for other-assignment-qa-testability-oracle", "assignment-qa-testability-oracle", false},
		{"id as inner path component", "Review `/project/.claude/work-packets/g1/S7/assignment-qa-testability-oracle/view.json`.", "assignment-qa-testability-oracle", false},
		{"id as final path component", "see /project/packets/assignment-qa-testability-oracle", "assignment-qa-testability-oracle", false},
		{"id starting a path", "assignment-qa-testability-oracle/view.json is the path", "assignment-qa-testability-oracle", false},
		{"id absent", "no identifier here at all", "assignment-qa-testability-oracle", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nativeS7AssignmentNamed(tt.text, tt.assignment); got != tt.want {
				t.Fatalf("nativeS7AssignmentNamed() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The real failure shape: free-form dispatch prose whose registered path
// reference was corrupted mid-string, with no protocol header, pair list or
// assigned-owner field anywhere. Only the assignment id survives.
func nativeS7CorruptedDispatchText(assignment string) string {
	var b strings.Builder
	b.WriteString("You are the independent S7 Reviewer for " + assignment + " (lens: qa, focus testability-oracle) in the isolated simulation project.\n\n")
	b.WriteString("USER-AUTHORIZED SIMULATION SCOPE (relayed from Main; governs your work).\n\n")
	b.WriteString("View: /project/.claude/work-packets/loop-REQ-001/g1/S7/review-plan-12-bG9vcC1SRVEtMDAx/p/packet-MTI6bG9vcC1SRVEtMDAxMTAyOnJldmlldy1wbGFuLTEy/WJHOXZjQzFTUlZFdE1EQXgtWlh\n")
	b.WriteString("- Answer (fill and save here): /project/.loop-submissions/s7/p/packet-MTI6bG9vcC1SRVEtMDAxMTAyOnJldmlldy1wbGFuLTEy/WJHOXZjQzFTUlZFdE1EQXgtWlhXFZWS1oxRldUVVpNVkVSV1pG/answer.json\n\n")
	b.WriteString("PROCEDURE: 1) Read the view.json; follow its own instruction. 2) Run the real checks and record actual outcomes. 3) Fill ONLY the answer fields the view defines. 4) Save at the exact path. 5) Return the answer path and verdict.\n")
	return b.String()
}

func TestNativeS7CandidateMatchByAssignmentIDSurvivesCorruptedPaths(t *testing.T) {
	const mine = "assignment-qa-testability-oracle"
	text := nativeS7CorruptedDispatchText(mine)
	facts := nativeReviewFacts(text)
	if facts.multiple || len(facts.s7) != 0 || len(facts.s8) != 0 || len(facts.owners) != 0 {
		t.Fatalf("free-form dispatch must extract empty protocol facts, got %+v", facts)
	}
	candidates := []nativeReviewCandidate{
		{stage: "S7", owner: "owner-" + mine, assignment: mine, claims: []string{"claim-qa-testability-oracle-6"}, refs: []string{
			"/project/.loop-submissions/s7/p/packet-registered-1/registered/answer.json",
			"/project/.claude/work-packets/loop-REQ-001/g1/S7/plan/assignment-qa-testability-oracle/p/registered/view.json",
		}},
		{stage: "S7", owner: "owner-other", assignment: "assignment-qa-maintainability", claims: []string{"claim-qa-maintainability-5"}, refs: []string{
			"/project/.loop-submissions/s7/p/packet-registered-2/registered/answer.json",
		}},
	}
	selected, unknown := nativeReviewSelect(facts, text, "/project", candidates)
	if unknown || len(selected) != 1 || selected[0].assignment != mine {
		t.Fatalf("assignment id in prose must select its own responsibility (selected=%d unknown=%v)", len(selected), unknown)
	}
}

// A dispatch that already carries another stage's explicit task coordinates
// (an S8 investigator's own question/answer paths) must keep attributing to
// that stage even though its background JSON names an S7 assignment id: the
// assignment-name rescue is subordinate to an explicit selection and must
// never steal or conflict with it.
func TestNativeS7AssignmentNameYieldsToExplicitS8Selection(t *testing.T) {
	const s7Assignment = "assignment-qa-testability-oracle"
	const s8Assignment = "assignment-hypothesis-investigation-1"
	text := fmt.Sprintf("S8 Investigator owner-investigator assignment=%s: Main carries exact question /project/.claude/submissions/s8/g1/source/q.json and private questionnaire answer /project/.loop-submissions/s8/answer.json. Compare background {\"assignment_id\":%q,\"claim_id\":\"claim-qa-testability-oracle-6\"}.", s8Assignment, s7Assignment)
	facts := nativeReviewFacts(text)
	candidates := []nativeReviewCandidate{
		{stage: "S7", owner: "owner-" + s7Assignment, assignment: s7Assignment, claims: []string{"claim-qa-testability-oracle-6"}, refs: []string{
			"/project/.loop-submissions/s7/p/packet-registered-1/registered/answer.json",
		}},
		{stage: "S8", owner: "owner-investigator", hypothesis: "hypothesis-investigation-1", assignment: s8Assignment, refs: []string{
			"/project/.claude/submissions/s8/g1/source/q.json",
			"/project/.loop-submissions/s8/answer.json",
		}},
	}
	selected, unknown := nativeReviewSelect(facts, text, "/project", candidates)
	if unknown || len(selected) != 1 || selected[0].stage != "S8" {
		t.Fatalf("explicit S8 selection must win over a background S7 assignment mention (selected=%d unknown=%v)", len(selected), unknown)
	}
}

func TestNativeS7SelectionStaysFailClosedWhenProseNamesTwoAssignments(t *testing.T) {
	const first = "assignment-qa-testability-oracle"
	const second = "assignment-qa-maintainability"
	text := "single actor: handle " + first + " and then " + second + " answers at /project/.loop-submissions/s7/a/answer.json and /project/.loop-submissions/s7/b/answer.json"
	facts := nativeReviewFacts(text)
	candidates := []nativeReviewCandidate{
		{stage: "S7", owner: "owner-a", assignment: first, claims: []string{"claim-a"}, refs: []string{"/project/.loop-submissions/s7/a/answer.json"}},
		{stage: "S7", owner: "owner-b", assignment: second, claims: []string{"claim-b"}, refs: []string{"/project/.loop-submissions/s7/b/answer.json"}},
	}
	selected, unknown := nativeReviewSelect(facts, text, "/project", candidates)
	if len(selected) != 0 || !unknown {
		t.Fatalf("prose naming two assignments must stay unresolved (selected=%d unknown=%v)", len(selected), unknown)
	}
}
