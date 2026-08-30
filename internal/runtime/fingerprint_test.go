package runtime

import "testing"

func TestComputeTriple(t *testing.T) {
	state := map[string]any{
		"documents": []any{
			map[string]any{"path": "a/b.go", "sha256": "abc"},
			map[string]any{"path": "c/d.go", "sha256": "def"},
		},
		"evidence": []any{
			map[string]any{"path": "ev/1.json", "sha256": "111"},
		},
		"bound_req": map[string]any{"path": "docs/req.json", "sha256": "999"},
	}
	triple := ComputeTriple(state)
	if triple.StateHash == "" || triple.EvidenceHash == "" || triple.BaselineHash == "" {
		t.Fatalf("ComputeTriple returned empty hashes: %+v", triple)
	}
	// Empty state should produce deterministic empty-input hashes.
	empty := ComputeTriple(map[string]any{})
	if empty.StateHash == "" || empty.BaselineHash == "" {
		t.Fatalf("ComputeTriple empty state should produce non-empty hashes")
	}
	// Malformed rows (missing sha) must be skipped without poisoning.
	state2 := map[string]any{
		"documents": []any{
			map[string]any{"path": "a/b.go", "sha256": ""},
			map[string]any{"path": "c/d.go", "sha256": "def"},
		},
	}
	triple2 := ComputeTriple(state2)
	if triple2.StateHash == triple.StateHash {
		t.Fatalf("malformed row should affect hash")
	}
	// Sorting: order should not matter.
	stateA := map[string]any{
		"documents": []any{
			map[string]any{"path": "b", "sha256": "2"},
			map[string]any{"path": "a", "sha256": "1"},
		},
	}
	stateB := map[string]any{
		"documents": []any{
			map[string]any{"path": "a", "sha256": "1"},
			map[string]any{"path": "b", "sha256": "2"},
		},
	}
	if ComputeTriple(stateA).StateHash != ComputeTriple(stateB).StateHash {
		t.Fatalf("ComputeTriple should be order-independent")
	}
}

func TestComputeRevisionPair(t *testing.T) {
	state := map[string]any{
		"revision": float64(5),
		"baseline": map[string]any{"generation": float64(3)},
	}
	rev, gen := ComputeRevisionPair(state)
	if rev != 5 || gen != 3 {
		t.Fatalf("ComputeRevisionPair got %d/%d want 5/3", rev, gen)
	}
	// Missing revision should be 0, not error.
	rev2, gen2 := ComputeRevisionPair(map[string]any{})
	if rev2 != 0 || gen2 != 0 {
		t.Fatalf("missing revision should be 0/0 got %d/%d", rev2, gen2)
	}
	// Revision 0 present should still be 0 (distinguished from missing only by existence, value same).
	rev3, _ := ComputeRevisionPair(map[string]any{"revision": float64(0)})
	if rev3 != 0 {
		t.Fatalf("revision 0 should be 0 got %d", rev3)
	}
}
