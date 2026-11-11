package investigation_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// Validate the public receipt wire before projecting it into a test type.
func assertSharedSubmissionReceiptWire(t *testing.T, receiptBytes []byte) {
	t.Helper()
	if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", receiptBytes); err != nil {
		t.Fatalf("public submission receipt differs from the shared schema: %v\n%s", err, receiptBytes)
	}
}
