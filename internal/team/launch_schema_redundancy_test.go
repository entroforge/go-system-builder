package team

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// TestLaunchDocumentOrderMatchesSchema enforces BUG-002 B3: any documents[]
// slice that the readback-request schema accepts must also be accepted by
// launch.validateDocumentOrder, and vice versa. The two checks encode the same
// bottom-up-order rule independently; drift between them would be a silent bug.
//
// The schema enforces the rule via allOf if/then on the documents prefixItems
// (see internal/schema/assets/readback-request.schema.json). launch.go enforces
// it via validateDocumentOrder. If one drifts, the other must drift in lock
// step — this test makes that contract executable.
func TestLaunchDocumentOrderMatchesSchema(t *testing.T) {
	root := filepath.Join("..", "..")
	validator := schema.NewValidator(root)

	type docCase struct {
		name              string
		bugID             *string
		documents         []DocumentReference
		expectPass        bool
		onlyOrderRelevant bool
	}

	sha := func(seed byte) string {
		// 64-char hex pad; the schema requires ^[a-f0-9]{64}$.
		out := make([]byte, 64)
		for i := range out {
			out[i] = '0' + (seed % 9)
		}
		return string(out)
	}

	bug := "BUG-100"

	cases := []docCase{
		{
			name: "standard task,contract,req order passes both",
			documents: []DocumentReference{
				{ID: "T", Kind: "task", Path: "t.md", Version: "v1", SHA256: sha(1), ReadOrder: 1},
				{ID: "C", Kind: "contract", Path: "c.md", Version: "v1", SHA256: sha(2), ReadOrder: 2},
				{ID: "R", Kind: "req", Path: "r.md", Version: "v1", SHA256: sha(3), ReadOrder: 3},
			},
			expectPass: true,
		},
		{
			name:  "repair bug,task,contract,req order passes both",
			bugID: &bug,
			documents: []DocumentReference{
				{ID: "B", Kind: "bug", Path: "b.md", Version: "v1", SHA256: sha(1), ReadOrder: 1},
				{ID: "T", Kind: "task", Path: "t.md", Version: "v1", SHA256: sha(2), ReadOrder: 2},
				{ID: "C", Kind: "contract", Path: "c.md", Version: "v1", SHA256: sha(3), ReadOrder: 3},
				{ID: "R", Kind: "req", Path: "r.md", Version: "v1", SHA256: sha(4), ReadOrder: 4},
			},
			expectPass: true,
		},
		{
			name: "req,task,contract fails both (wrong order)",
			documents: []DocumentReference{
				{ID: "R", Kind: "req", Path: "r.md", Version: "v1", SHA256: sha(1), ReadOrder: 1},
				{ID: "T", Kind: "task", Path: "t.md", Version: "v1", SHA256: sha(2), ReadOrder: 2},
				{ID: "C", Kind: "contract", Path: "c.md", Version: "v1", SHA256: sha(3), ReadOrder: 3},
			},
			expectPass: false,
		},
		{
			name: "task,req,contract fails both (middle swapped)",
			documents: []DocumentReference{
				{ID: "T", Kind: "task", Path: "t.md", Version: "v1", SHA256: sha(1), ReadOrder: 1},
				{ID: "R", Kind: "req", Path: "r.md", Version: "v1", SHA256: sha(2), ReadOrder: 2},
				{ID: "C", Kind: "contract", Path: "c.md", Version: "v1", SHA256: sha(3), ReadOrder: 3},
			},
			expectPass: false,
		},
		{
			name: "two documents fails both (under-min)",
			documents: []DocumentReference{
				{ID: "T", Kind: "task", Path: "t.md", Version: "v1", SHA256: sha(1), ReadOrder: 1},
				{ID: "C", Kind: "contract", Path: "c.md", Version: "v1", SHA256: sha(2), ReadOrder: 2},
			},
			expectPass: false,
		},
		{
			name: "extra design doc after the prefix passes both",
			documents: []DocumentReference{
				{ID: "T", Kind: "task", Path: "t.md", Version: "v1", SHA256: sha(1), ReadOrder: 1},
				{ID: "C", Kind: "contract", Path: "c.md", Version: "v1", SHA256: sha(2), ReadOrder: 2},
				{ID: "R", Kind: "req", Path: "r.md", Version: "v1", SHA256: sha(3), ReadOrder: 3},
				{ID: "D", Kind: "design", Path: "d.md", Version: "v1", SHA256: sha(4), ReadOrder: 4},
			},
			expectPass: true,
		},
		{
			// read_order not strictly increasing on the trailing tail; launch
			// rejects this but the schema's prefixItems only inspect the first
			// three positions. Mark onlyOrderRelevant=false so the equivalence
			// check skips this case (the schema cannot encode strict-increasing
			// on the tail — that is launch-only by design, not drift).
			name: "tail non-monotonic read_order is launch-only (skipped from equivalence)",
			documents: []DocumentReference{
				{ID: "T", Kind: "task", Path: "t.md", Version: "v1", SHA256: sha(1), ReadOrder: 1},
				{ID: "C", Kind: "contract", Path: "c.md", Version: "v1", SHA256: sha(2), ReadOrder: 2},
				{ID: "R", Kind: "req", Path: "r.md", Version: "v1", SHA256: sha(3), ReadOrder: 3},
				{ID: "D", Kind: "design", Path: "d.md", Version: "v1", SHA256: sha(4), ReadOrder: 2},
			},
			expectPass:        false,
			onlyOrderRelevant: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			launchErr := validateDocumentOrder(tc.documents, tc.bugID != nil)
			schemaErr := validateReadbackSchema(t, validator, tc.bugID, tc.documents)

			launchPass := launchErr == nil
			schemaPass := schemaErr == nil

			if tc.onlyOrderRelevant {
				// Cases where the schema cannot encode the same rule (e.g.
				// strict-increasing on the tail). We still assert the launch
				// decision matches the expected pass/fail; the schema is
				// informational here.
				if launchPass != tc.expectPass {
					t.Fatalf("launch: expected pass=%v got pass=%v err=%v",
						tc.expectPass, launchPass, launchErr)
				}
				return
			}

			if launchPass != tc.expectPass {
				t.Fatalf("launch: expected pass=%v got pass=%v err=%v",
					tc.expectPass, launchPass, launchErr)
			}
			if schemaPass != tc.expectPass {
				t.Fatalf("schema: expected pass=%v got pass=%v err=%v",
					tc.expectPass, schemaPass, schemaErr)
			}
			// The core BUG-002 B3 contract: the two validators agree.
			if launchPass != schemaPass {
				t.Fatalf("BUG-002 B3 drift: launchPass=%v schemaPass=%v (launchErr=%v schemaErr=%v)",
					launchPass, schemaPass, launchErr, schemaErr)
			}
		})
	}
}

// validateReadbackSchema builds a minimal readback_request envelope around the
// documents slice and runs it through the same schema validator that production
// uses. Returns nil on success, non-nil on schema rejection.
func validateReadbackSchema(t *testing.T, v *schema.Validator, bugID *string, documents []DocumentReference) error {
	t.Helper()
	envelope := map[string]any{
		"schema_version":       "1.0.0",
		"message_type":         "readback_request",
		"documents":            toInterfaceDocuments(documents),
		"skills":               []map[string]any{{"name": "two-phase-activation"}},
		"scope":                map[string]any{"responsibility": "x"},
		"closing_contract_ref": "TASK-001#closing-contract",
		"readback_fields":      []string{"objective"},
	}
	if bugID != nil {
		envelope["bug_id"] = *bugID
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return v.ValidateBytes("readback-request.schema.json", data)
}

// toInterfaceDocuments rebuilds each DocumentReference as a generic map so the
// schema validator sees the exact field names (id, kind, path, version, sha256,
// read_order) that production JSON would carry.
func toInterfaceDocuments(docs []DocumentReference) []map[string]any {
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		out = append(out, map[string]any{
			"id":         d.ID,
			"kind":       d.Kind,
			"path":       d.Path,
			"version":    d.Version,
			"sha256":     d.SHA256,
			"read_order": d.ReadOrder,
		})
	}
	return out
}

// TestValidateDocumentOrderDirectly exercises launch.validateDocumentOrder with
// edge cases that the schema cannot speak to (under-min length, repair-mode
// length). This locks the launch side of the BUG-002 B3 contract independently
// of the schema.
func TestValidateDocumentOrderDirectly(t *testing.T) {
	sha := func(seed byte) string {
		out := make([]byte, 64)
		for i := range out {
			out[i] = '0' + (seed % 9)
		}
		return string(out)
	}
	doc := func(kind string, order int, seed byte) DocumentReference {
		return DocumentReference{
			ID: kind, Kind: kind, Path: kind + ".md", Version: "v1",
			SHA256: sha(seed), ReadOrder: order,
		}
	}

	t.Run("empty is rejected", func(t *testing.T) {
		if err := validateDocumentOrder(nil, false); err == nil {
			t.Fatal("expected rejection for empty slice")
		}
	})
	t.Run("repair-mode needs four documents", func(t *testing.T) {
		three := []DocumentReference{doc("bug", 1, 1), doc("task", 2, 2), doc("contract", 3, 3)}
		if err := validateDocumentOrder(three, true); err == nil {
			t.Fatal("expected rejection: repair mode requires four docs")
		}
		four := append(three, doc("req", 4, 4))
		if err := validateDocumentOrder(four, true); err != nil {
			t.Fatalf("expected pass for four-doc repair order: %v", err)
		}
	})
	t.Run("non-monotonic tail is rejected", func(t *testing.T) {
		docs := []DocumentReference{
			doc("task", 1, 1), doc("contract", 2, 2), doc("req", 3, 3),
			{ID: "D", Kind: "design", Path: "d.md", Version: "v1", SHA256: strings.Repeat("0", 64), ReadOrder: 2},
		}
		if err := validateDocumentOrder(docs, false); err == nil {
			t.Fatal("expected rejection for non-monotonic tail read_order")
		}
	})
}
