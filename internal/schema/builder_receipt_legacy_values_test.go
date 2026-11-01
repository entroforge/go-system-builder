package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestBuilderReceiptIgnoresArbitraryLegacyMachineValues(t *testing.T) {
	for name, old := range map[string]any{"null": nil, "number": 7, "object": map[string]any{"old": true}} {
		t.Run(name, func(t *testing.T) {
			value := map[string]any{"schema_version": "1.0.0", "runtime_id": "loop-1", "baseline_generation": 1, "observed_revision": 3, "owner": "builder", "assignment_id": "assignment-1", "task_id": "TASK-1", "delivery_recorded": true, "accepted": true, "work_complete": true, "integrated": true, "responsibility_status": "complete", "checks_source": "main_checkpoint", "main_checkpoint_ref": "checkpoint.json", "main_check_receipts": []any{map[string]any{"check_index": 0, "command": "true", "status": "pass", "receipt_sha256": old, "tested_head": old}}, "resolved_evidence_refs": []any{map[string]any{"ref": "original", "path": "proof.txt", "sha256": old}}, "domain_refs": []string{"result.json", "checkpoint.json"}, "own_pending": []string{}, "waiting_on": []any{}, "answer_digest": old, "main_checkpoint_sha256": old}
			data, _ := json.Marshal(value)
			if err := schema.NewEmbeddedValidator().ValidateBytes("builder-receipt.schema.json", data); err != nil {
				t.Fatal(err)
			}
			value["owner"] = 7
			data, _ = json.Marshal(value)
			if err := schema.NewEmbeddedValidator().ValidateBytes("builder-receipt.schema.json", data); err == nil {
				t.Fatal("invalid actual owner was accepted")
			}
		})
	}
}
