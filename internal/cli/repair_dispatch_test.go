package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/repair"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestRuntimeRepairDispatchBindsBuilderToAssignment(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	writeDispatchFile(t, root, "docs/agent-protocol.md", "# protocol\n")
	writeDispatchFile(t, root, "agents/backend-builder.md", "# backend builder\n")

	contractBytes := []byte(`{
  "schema_version":"1.0.0","repair_contract_id":"repair-contract-dispatch","case_id":"investigation-case-dispatch","revision":1,"status":"approved",
  "source_finding_ids":["finding-dispatch"],"root_cause_statement":"two authorities drift","violated_invariant":"one authority",
  "causal_model_ref":"case://investigation-case-dispatch/model","architecture_intent":"restore one authority",
  "repair_units":[{"id":"unit-api","description":"restore api authority","scope":["internal/api"],"assertion_ids":["symptom-1"]}],
  "prospective_scope":["internal/api"],"forbidden_scope":["docs/requirements"],"symptom_assertions":["api displays"],
  "root_invariant_assertions":["one authority"],"detection_gap_assertions":["contract catches drift"],
  "stop_escalation_conditions":["scope expands"],"approved_by":"human","approved_at":"2026-08-26T00:00:00Z",
  "approval_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}
`)
	contractRel := ".claude/review/investigation/contracts/repair-contract-dispatch.json"
	contractPath := filepath.Join(root, filepath.FromSlash(contractRel))
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, contractBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	contractRef := repair.ContractRef{Path: contractRel, SHA256: sha256HexDispatch(contractBytes)}
	session, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-dispatch", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{
		Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-dispatch", CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	state := req039fixtures.BaseState(t, root, "bug_resolution", "planning", 4)
	review := state["review"].(map[string]any)
	review["repair"] = map[string]any{
		"session_id": session.SessionID, "case_id": "investigation-case-dispatch", "contract_id": "repair-contract-dispatch",
		"contract_ref": contractRel, "contract_sha256": contractRef.SHA256, "path": sessionRef.Path, "sha256": sessionRef.SHA256,
		"revision": 1, "status": "planning", "plan_ref": planRef.Path, "plan_sha256": planRef.SHA256,
		"plan_report_refs": []any{}, "result_refs": []any{}, "next_action": "dispatch a Builder", "updated_at": "2026-08-26T00:00:00Z",
	}
	// Keep the fixture's runtime identity aligned with the generated session.
	state["runtime_id"] = "loop-req039-ct"
	req039fixtures.WriteState(t, root, state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "repair", "dispatch", "--root", root, "--assignment-id", plan.Assignments[0].AssignmentID, "--agent-id", "builder-dispatch-1"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dispatch code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "generic PLAN_REPORT") || !strings.Contains(stderr.String(), "domain") {
		t.Fatalf("dispatch must expose both plan-report layers: %s", stderr.String())
	}
	var response map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["assignment_id"] != plan.Assignments[0].AssignmentID || response["agent_id"] != "builder-dispatch-1" {
		t.Fatalf("dispatch response = %#v", response)
	}
	updated := req039fixtures.ReadState(t, root)
	updatedReview := updated["review"].(map[string]any)
	updatedRepair := updatedReview["repair"].(map[string]any)
	owners := updatedRepair["assignment_owners"].(map[string]any)
	if owners[plan.Assignments[0].AssignmentID] != "builder-dispatch-1" {
		t.Fatalf("S9 owner projection = %#v", owners)
	}
	entities := updated["entities"].(map[string]any)
	if !containsDispatchEntity(entities["teams"], response["workgroup_id"].(string)) || !containsDispatchEntity(entities["agents"], "builder-dispatch-1") || !containsDispatchEntity(entities["tasks"], response["task_id"].(string)) {
		t.Fatalf("dispatch did not register Team/Agent/Task: %#v", entities)
	}
}

func writeDispatchFile(t *testing.T, root, rel, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha256HexDispatch(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func containsDispatchEntity(raw any, id string) bool {
	items, _ := raw.([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		if row["id"] == id {
			return true
		}
	}
	return false
}
