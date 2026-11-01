package investigation_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
)

func TestConsumeCaseRouteReturnsNoRepairCaseToOriginalS7Entry(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})

	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		Route:                "s7_no_change",
		RouteReason:          "the finding is not a product defect and requires no repair",
	})
	if err != nil {
		t.Fatalf("UpdateCaseRoute() error = %v", err)
	}

	snapshot, err = investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: snapshot.Revision,
		CaseID:           "investigation-case-review-plan-r1",
		Actor:            "orchestrator",
	})
	if err != nil {
		t.Fatalf("ConsumeCaseRoute() error = %v", err)
	}
	if snapshot.Revision != 3 {
		t.Fatalf("runtime revision = %d, want 3", snapshot.Revision)
	}
	lifecycle := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "verification" || lifecycle["phase"] != "running" {
		t.Fatalf("lifecycle = %#v, want verification.running", lifecycle)
	}
	review := snapshot.State["review"].(map[string]any)
	if review["investigation"] != nil {
		t.Fatalf("consumed no-repair route must clear active investigation pointer: %#v", review["investigation"])
	}
	if _, exists := review["round"]; exists {
		t.Fatalf("route registered an obsolete review number: %#v", review)
	}
	entry, _ := review["round_entry"].(map[string]any)
	last, _ := snapshot.State["last_transition"].(map[string]any)
	want := reviewmeta.NewPlanID(snapshot.State["runtime_id"].(string), last["event_id"].(string))
	if entry["plan_id"] != want || entry["transition_id"] != "S8-ROUTE-CONSUME" {
		t.Fatalf("original S7 entry does not use the committed route event: %#v want plan %s", entry, want)
	}
	if _, exists := entry["round"]; exists {
		t.Fatalf("S7 entry registered an obsolete review number: %#v", entry)
	}
	stateBefore, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: -1, CaseID: "investigation-case-review-plan-r1", Actor: "orchestrator",
	})
	stateAfter, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("repeated route event rewrote the original S7 entry")
	}
}

func TestConsumeCaseRouteReturnsSpecificationRouteToPlanning(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision: 1, ExpectedCaseRevision: 1, CaseID: "investigation-case-review-plan-r1", Route: "s2_spec_rework", RouteReason: "the locked specification is inconsistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: snapshot.Revision, CaseID: "investigation-case-review-plan-r1", Actor: "orchestrator",
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "planning" || lifecycle["phase"] != "design" {
		t.Fatalf("specification route lifecycle = %#v, want planning.design", lifecycle)
	}
	active := snapshot.State["review"].(map[string]any)["investigation"].(map[string]any)
	if active["status"] != "closed" || active["route_consumed_at"] == nil {
		t.Fatalf("specification route was not durably consumed: %#v", active)
	}
}

func TestConsumeCaseRouteKeepsHumanRequirementChangeAtPauseBoundary(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision: 1, ExpectedCaseRevision: 1, CaseID: "investigation-case-review-plan-r1", Route: "human_req_change", RouteReason: "the locked requirement is wrong",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: snapshot.Revision, CaseID: "investigation-case-review-plan-r1", Actor: "orchestrator",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime pause") {
		t.Fatalf("human requirement route must return the pause recovery verb, got %v", err)
	}
}
