package repair_test

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestNewRepairPlanUsesPendingAndPreservesLegacySchemaBoundary(t *testing.T) {
	root := t.TempDir()
	contract, _ := writeRuntimeContract(t, root)
	_, session, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contract, SessionID: "repair-session-unified", RuntimeID: "loop-unified",
		ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, ref, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contract, Session: session, PlanID: "repair-plan-unified", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != "1.1.0" || len(plan.Assignments) == 0 || plan.Assignments[0].Status != "pending" {
		t.Fatalf("new plan initial status: %+v", plan)
	}
	if _, err := repair.ValidateRepairPlan(root, ref); err != nil {
		t.Fatal(err)
	}
	validator := schema.NewEmbeddedValidator()
	for _, tc := range []struct {
		version, status string
		valid           bool
	}{
		{"1.1.0", "pending", true}, {"1.1.0", "queued", false},
		{"1.0.0", "queued", true}, {"1.0.0", "pending", false},
	} {
		plan.SchemaVersion, plan.Assignments[0].Status = tc.version, tc.status
		data, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		err = validator.ValidateBytes("repair-plan.schema.json", data)
		if (err == nil) != tc.valid {
			t.Fatalf("version=%s status=%s valid=%v: %v", tc.version, tc.status, tc.valid, err)
		}
	}
}
