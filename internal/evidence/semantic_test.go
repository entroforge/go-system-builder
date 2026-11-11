package evidence_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/evidence"
)

func TestReferenceIDKeepsOriginalReferenceMeaning(t *testing.T) {
	for _, tc := range []struct {
		input, id          string
		indexed, wantError bool
	}{
		{"ev-observation", "ev-observation", true, false},
		{"runtime:ev-observation", "ev-observation", true, false},
		{"runtime:runtime:ev-observation", "runtime:ev-observation", true, false},
		{"test://existing-execution", "test://existing-execution", false, false},
		{"runtime:test://unregistered", "test://unregistered", true, false},
		{"runtime:", "", false, true},
		{"runtime: ", "", false, true},
		{"", "", false, true},
		{"  ", "", false, true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			id, indexed, err := evidence.ReferenceID(tc.input)
			if (err != nil) != tc.wantError || id != tc.id || indexed != tc.indexed {
				t.Fatalf("ReferenceID(%q) = %q, %t, %v", tc.input, id, indexed, err)
			}
		})
	}
}
