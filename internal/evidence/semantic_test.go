package evidence_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/evidence"
)

func TestRuntimeReferencesKeepOriginalAttestation(t *testing.T) {
	for _, form := range []string{"ev-observation", "runtime:ev-observation"} {
		for _, fault := range []string{"", "missing", "invalid", "invalidated", "generation", "kind", "round", "sha", "file", "self"} {
			t.Run(form+"/"+fault, func(t *testing.T) {
				root := t.TempDir()
				body := []byte("original observation\n")
				digest := sha256.Sum256(body)
				path := filepath.Join(root, "observation.txt")
				if err := os.WriteFile(path, body, 0600); err != nil {
					t.Fatal(err)
				}
				row := map[string]any{"id": "ev-observation", "status": "valid", "invalidated_by": nil, "baseline_generation": 1, "review_round": 2, "kind": "qa_review", "path": "observation.txt", "sha256": hex.EncodeToString(digest[:])}
				state := map[string]any{"baseline": map[string]any{"generation": 1}, "evidence": []any{row}}
				opts := evidence.RefsOptions{Root: root, RequireKinds: []string{"qa_review"}, RequireReviewRound: 2}
				switch fault {
				case "missing":
					state["evidence"] = []any{}
				case "invalid":
					row["status"] = "invalid"
				case "invalidated":
					row["invalidated_by"] = "impact-1"
				case "generation":
					row["baseline_generation"] = 2
				case "kind":
					row["kind"] = "human_decision"
				case "round":
					row["review_round"] = 1
				case "sha":
					if err := os.WriteFile(path, []byte("drift"), 0600); err != nil {
						t.Fatal(err)
					}
				case "file":
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				case "self":
					opts.ForbidSelfID = "ev-observation"
				}
				before, _ := json.Marshal(state)
				err := evidence.ValidateRefs(state, []string{form}, opts)
				if (err == nil) != (fault == "") {
					t.Fatalf("attestation fault=%q: %v", fault, err)
				}
				after, _ := json.Marshal(state)
				if string(before) != string(after) {
					t.Fatal("reference resolution mutated evidence or registered an alias")
				}
			})
		}
	}
}

func TestRuntimeReferencePrefixCannotBypassIndexOrPreferAlias(t *testing.T) {
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "evidence": []any{
		map[string]any{"id": "ev-observation", "status": "invalid", "baseline_generation": 1},
		map[string]any{"id": "runtime:ev-observation", "status": "valid", "baseline_generation": 1},
	}}
	for _, ref := range []string{"runtime:", "runtime: ", "runtime:missing", "runtime:test://unregistered", "runtime:ev-observation"} {
		if err := evidence.ValidateRefs(state, []string{ref}, evidence.RefsOptions{}); err == nil {
			t.Fatalf("unqualified runtime ref accepted: %q", ref)
		}
	}
	if err := evidence.ValidateRefs(state, []string{"test://existing-execution"}, evidence.RefsOptions{}); err != nil {
		t.Fatalf("original execution anchor changed: %v", err)
	}
	// The prefix is decoded once. A historical prefixed ID can still be
	// addressed explicitly, as in S7; it never shadows the canonical ID.
	if err := evidence.ValidateRefs(state, []string{"runtime:runtime:ev-observation"}, evidence.RefsOptions{}); err != nil {
		t.Fatalf("exact historical evidence ID cannot be addressed: %v", err)
	}
}
