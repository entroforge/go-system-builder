package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// The real S7 issuer leaves one Reviewer in flight when another Finding
// enters S8. The Case route returns to S7 without erasing that original work.
func TestS8NoChangeRoutesToOriginalS7QuestionnaireAndKeepsInFlightReviewer(t *testing.T) {
	root, _ := minimalAnswerInvestigator(t)
	before := stageSnapshot(t, root)
	index, err := runtime.ProjectIssuedReviewResultPacketIndex(before.State, "")
	if err != nil || len(index) != 2 {
		t.Fatalf("issued original reviewer responsibilities: %v, %v", index, err)
	}
	var lateID string
	for _, row := range index {
		if row.Reception == nil || row.Reception["receipt"] == nil {
			lateID = row.ID
		}
	}
	if lateID == "" {
		t.Fatal("fixture did not retain an in-flight original Reviewer")
	}
	late, found, err := runtime.FindReviewResultPacketContext(runtime.WithPacketBodyRoot(context.Background(), root), before.State, lateID)
	if err != nil || !found {
		t.Fatalf("read the original late responsibility: found=%v err=%v", found, err)
	}
	pointer := before.State["review"].(map[string]any)["investigation"].(map[string]any)
	caseID := pointer["case_id"].(string)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	routed, err := investigation.UpdateCaseRoute(root, statePath, journalPath, investigation.RouteRequest{
		ExpectedRevision: before.Revision, CaseID: caseID, Route: "s7_no_change", RouteReason: "The current observation does not establish a product defect; continue the remaining original review.",
	})
	if err != nil {
		t.Fatalf("route the actual Case: %v", err)
	}
	returned, err := investigation.ConsumeCaseRoute(root, statePath, journalPath, investigation.ConsumeRouteRequest{ExpectedRevision: routed.Revision, CaseID: caseID, Actor: "Main"})
	if err != nil {
		t.Fatalf("consume the actual Case route: %v", err)
	}
	entry := returned.State["review"].(map[string]any)["round_entry"].(map[string]any)
	last := returned.State["last_transition"].(map[string]any)
	wantID := reviewmeta.NewPlanID(returned.State["runtime_id"].(string), last["event_id"].(string))
	if entry["plan_id"] != wantID || entry["round"] != nil {
		t.Fatalf("wrong original route identity: %v want %s", entry, wantID)
	}
	question := review.PlanQuestionnairePath(root, returned.State)
	if question == "" {
		t.Fatal("route did not provide the new original ReviewPlan path")
	}
	out := entryQuestionnaireHook(t, root, "PreToolUse", question)
	if _, err := os.Stat(question); err != nil || !strings.Contains(out, question) {
		t.Fatalf("original Main event did not prepare and show ReviewPlan %s: %v; %s", question, err, out)
	}
	// The old Reviewer is still the original author even after this Case route.
	s7ListenerAnswer(t, root, late)
	payload := s7ListenerPayload(t, "opaque-late-after-s8-route", "Review original "+filepath.Join(root, late.ViewPath))
	out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", payload)
	receipt := minimalAnswerReviewReceipt(t, root, late)
	if !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("in-flight original Reviewer lost after S8 return: %+v; %s %s", receipt, out, diagnostic)
	}
	after := stageSnapshot(t, root)
	if got := after.State["review"].(map[string]any)["round_entry"].(map[string]any)["plan_id"]; got != wantID {
		t.Fatalf("late result replaced the new original plan identity: %v", got)
	}
	s7ListenerEvent(t, root, "PreToolUse", payload)
	if replay := stageSnapshot(t, root); replay.Revision != after.Revision {
		t.Fatalf("replayed original late result changed revision: %d -> %d", after.Revision, replay.Revision)
	}
}
