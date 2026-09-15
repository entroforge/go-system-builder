package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEvidenceBindingRepairBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any, map[string]any, string)
		want   bool
	}{
		{"misbound", nil, true},
		{"correct ID", func(s, e map[string]any, r string) { e["id"] = "real" }, false},
		{"invalid", func(s, e map[string]any, r string) { e["status"] = "invalid" }, false},
		{"old generation", func(s, e map[string]any, r string) { e["baseline_generation"] = 0 }, false},
		{"wrong kind", func(s, e map[string]any, r string) { e["kind"] = "acceptance" }, false},
		{"paused", func(s, e map[string]any, r string) { s["pause"] = map[string]any{} }, false},
		{"building", func(s, e map[string]any, r string) { s["lifecycle"] = map[string]any{"state": "building"} }, false},
		{"S7 began", func(s, e map[string]any, r string) { s["review"] = map[string]any{"round": 1} }, false},
		{"artifact changed", func(s, e map[string]any, r string) { os.WriteFile(filepath.Join(r, "review.json"), []byte("{}"), 0644) }, false},
		{"REQ changed", func(s, e map[string]any, r string) { os.WriteFile(filepath.Join(r, "req.md"), []byte("changed"), 0644) }, false},
		{"missing", func(s, e map[string]any, r string) { os.Remove(filepath.Join(r, "review.json")) }, false},
		{"duplicate", func(s, e map[string]any, r string) { s["evidence"] = []any{e, e} }, false},
		{"escape", func(s, e map[string]any, r string) { e["path"] = "../review.json" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(p string, b []byte) string {
				if err := os.WriteFile(filepath.Join(root, p), b, 0644); err != nil {
					t.Fatal(err)
				}
				return scopeHash(b)
			}
			env, _ := json.Marshal(map[string]any{"evidence_id": "real", "kind": "document_review", "runtime_id": "loop-test", "baseline_generation": 1})
			e := map[string]any{"id": "wrong", "path": "review.json", "sha256": write("review.json", env), "kind": "document_review", "status": "valid", "baseline_generation": 1}
			s := map[string]any{"runtime_id": "loop-test", "revision": 89, "lifecycle": map[string]any{"state": "document_verification"}, "baseline": map[string]any{"generation": 1}, "bound_req": map[string]any{"status": "locked", "path": "req.md", "sha256": write("req.md", []byte("locked"))}, "evidence": []any{e}}
			if tc.change != nil {
				tc.change(s, e, root)
			}
			p, err := inspectEvidenceBinding(root, s, "wrong")
			if (err == nil) != tc.want {
				t.Fatalf("plan=%+v err=%v", p, err)
			}
			if err == nil && (p.ID != "wrong" || p.EnvelopeID != "real" || len(p.Files) != 2) {
				t.Fatalf("bad plan: %+v", p)
			}
		})
	}
}
