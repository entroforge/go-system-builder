package recovery

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInspectPlanAndImportUseOriginalFactsWithoutDigests(t *testing.T) {
	root := t.TempDir()
	path := "docs/requirements/REQ-052.md"
	writeRecoveryREQ(t, root, path, "locked", "v1.0.3")
	inventory, err := Inspect(root, path)
	if err != nil {
		t.Fatal(err)
	}
	// Unrelated historical properties remain harmless input, without being
	// reintroduced into the current plan or treated as evidence.
	encoded, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	object["req"].(map[string]any)["sha256"] = "unrelated historical value"
	encoded, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &inventory); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(inventory)
	if err != nil {
		t.Fatalf("old digest must not qualify the plan: %v", err)
	}
	imported, err := Import(root, plan)
	if err != nil {
		t.Fatalf("old digest must not qualify the original input: %v", err)
	}
	data, err := json.Marshal(struct {
		Plan   Plan         `json:"plan"`
		Import ImportResult `json:"import"`
	}{plan, imported})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"sha256"`) || strings.Contains(string(data), `"plan_sha256"`) {
		t.Fatalf("new plan/import copied a historical digest: %s", data)
	}
	if plan.REQ.Path != path || len(imported.Documents) == 0 {
		t.Fatalf("original input was not retained: %#v %#v", plan.REQ, imported.Documents)
	}
}
