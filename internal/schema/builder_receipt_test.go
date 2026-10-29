package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestBuilderReceiptClosedContract(t *testing.T) {
	base := map[string]any{"schema_version": "1.0.0", "runtime_id": "runtime", "baseline_generation": 1, "observed_revision": 1, "owner": "builder", "assignment_id": "assignment", "task_id": "TASK-001", "delivery_recorded": true, "accepted": true, "work_complete": true, "integrated": false, "responsibility_status": "complete", "checks_source": "original_report", "domain_refs": []string{"report.json"}, "own_pending": []string{}, "waiting_on": []any{map[string]any{"owner": "main", "reason": "integration_pending", "ref": "checkpoint.json"}}}
	for _, name := range []string{"valid", "unknown authority", "executed checks", "pending complete", "integrated incomplete", "peer wait"} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var value map[string]any
			json.Unmarshal(data, &value)
			switch name {
			case "unknown authority":
				value["permission_grant"] = true
			case "executed checks":
				value["checks_source"] = "executed"
			case "pending complete":
				value["own_pending"] = []string{"check failed"}
			case "integrated incomplete":
				value["integrated"] = true
				value["work_complete"] = false
			case "peer wait":
				value["waiting_on"] = []any{map[string]any{"owner": "peer", "reason": "integration_pending", "ref": "checkpoint.json"}}
			}
			data, _ = json.Marshal(value)
			err := schema.NewEmbeddedValidator().ValidateBytes("builder-receipt.schema.json", data)
			if (err == nil) != (name == "valid") {
				t.Fatalf("schema accepted=%v: %v", err == nil, err)
			}
		})
	}
}

func TestBuilderReceiptAcceptsWorkspaceWorkerBootstrapBinding(t *testing.T) {
	base := map[string]any{
		"schema_version": "1.0.0", "runtime_id": "loop-1", "baseline_generation": 1,
		"observed_revision": 1, "owner": "builder", "assignment_id": "assignment-1",
		"task_id": "TASK-001", "delivery_recorded": true, "accepted": true,
		"work_complete": false, "integrated": false, "responsibility_status": "accepted_incomplete",
		"checks_source": "original_report", "domain_refs": []string{"report.json"},
		"own_pending": []string{}, "waiting_on": []any{},
		"worker_check_facts": []any{map[string]any{
			"index": 0, "command": "go test ./pkg", "profile_digest": strings.Repeat("a", 64),
			"status": "unavailable", "execution_kind": "workspace",
			"assignment_record": map[string]any{
				"runtime_id": "loop-1", "baseline_generation": 1,
				"assignment_id": "assignment-1", "assignment_revision": 1,
			},
			"assignment_record_sha256": strings.Repeat("b", 64), "runtime_id": "loop-1",
			"baseline_generation": 1, "owner_agent_id": "builder", "execution_generation": 0,
			"bootstrap_sha256":               strings.Repeat("c", 64),
			"current_source_snapshot_sha256": strings.Repeat("d", 64),
		}},
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("builder-receipt.schema.json", data); err != nil {
		t.Fatalf("schema rejected the optional captured workspace bootstrap digest: %v", err)
	}
}
