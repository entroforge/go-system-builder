package investigation_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
)

func TestConsumeCaseRouteReturnsNoRepairCaseToFreshS7Round(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
		Route:                "s7_no_change",
		RouteReason:          "the finding is not a product defect and requires no repair",
	})
	if err != nil {
		t.Fatalf("UpdateCaseRoute() error = %v", err)
	}

	snapshot, err = investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: snapshot.Revision,
		CaseID:           "investigation-case-observation-batch-r1",
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
	if review["round"] != float64(2) && review["round"] != 2 {
		t.Fatalf("review round = %v, want 2", review["round"])
	}
}

func TestConsumeCaseRouteReturnsSpecificationRouteToPlanning(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision: 1, ExpectedCaseRevision: 1, ExpectedCaseSHA256: pointer["sha256"].(string),
		CaseID: "investigation-case-observation-batch-r1", Route: "s2_spec_rework", RouteReason: "the locked specification is inconsistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: snapshot.Revision, CaseID: "investigation-case-observation-batch-r1", Actor: "orchestrator",
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
	pointer := investigationPointer(t, fixture)
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision: 1, ExpectedCaseRevision: 1, ExpectedCaseSHA256: pointer["sha256"].(string),
		CaseID: "investigation-case-observation-batch-r1", Route: "human_req_change", RouteReason: "the locked requirement is wrong",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: snapshot.Revision, CaseID: "investigation-case-observation-batch-r1", Actor: "orchestrator",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime pause") {
		t.Fatalf("human requirement route must return the pause recovery verb, got %v", err)
	}
}
