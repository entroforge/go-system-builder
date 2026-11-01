package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestSubmissionReceiptAcceptsOriginalResponsibilityIdentityWithoutDigest(t *testing.T) {
	receipt := map[string]any{
		"packet_id": "packet-s5-existing-responsibility", "accepted": true,
		"responsibility_status": "accepted", "work_complete": true,
		"current_stage": "S5", "domain_refs": []string{"report/original.json"},
		"waiting_on": []any{}, "next_action": "continue", "field_errors": []any{},
		"submission_key": "submission:packet-s5-existing-responsibility",
		"status":         "done",
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", data); err != nil {
		t.Fatal(err)
	}
}
