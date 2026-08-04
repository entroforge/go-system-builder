package transition_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// TestResumeRejectsBaselineDrift verifies that TR-019 (resume) rejects when a
// document fingerprint changed while paused.
func TestResumeRejectsBaselineDrift(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)

	// Create a REQ file with a known hash.
	reqDir := filepath.Join(root, "docs", "requirements")
	os.MkdirAll(reqDir, 0o755)
	reqPath := filepath.Join(reqDir, "REQ-099.md")
	os.WriteFile(reqPath, []byte("# REQ-099\nStatus: locked\n"), 0o644)

	// Pause from verification.delivery with a fingerprint for the REQ.
	state := stateAtVerificationMap(5)
	state["lifecycle"] = map[string]any{"state": "paused", "phase": nil, "phase_revision": float64(2)}
	state["pause"] = map[string]any{
		"from_state":               "verification",
		"from_phase":               "delivery",
		"phase_revision":           float64(1),
		"baseline_generation":      float64(1),
		"review_round":             float64(1),
		"entity_snapshot_revision": float64(0),
		"reason":                   "test",
		"required_human_action":    "test",
		"document_fingerprints": []any{
			map[string]any{
				"path":    "docs/requirements/REQ-099.md",
				"version": "1.0.0",
				"sha256":  "deadbeef0000000000000000000000000000000000000000000000000000ffff",
			},
		},
		"committed_idempotency_keys": []string{},
		"paused_at":                  "2026-01-01T00:00:00Z",
	}
	registerFixtureEvidence(t, root, state, map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"pause_record":          "docs/reports/human/pause-record.md",
	})
	writeFullState(t, root, state)

	// TR-019 should fail because the REQ file hash does not match the recorded fingerprint.
	err := applyT(t, root, "TR-019", 5, "user", map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"pause_record":          "docs/reports/human/pause-record.md",
	})
	if err == nil {
		t.Fatal("TR-019 should reject when document fingerprint drifted")
	}
}

// TestResumePassesWhenBaselinesUnchanged verifies that TR-019 succeeds when
// document fingerprints match.
func TestResumePassesWhenBaselinesUnchanged(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)

	// Create a REQ file and compute its real hash.
	reqDir := filepath.Join(root, "docs", "requirements")
	os.MkdirAll(reqDir, 0o755)
	reqContent := []byte("# REQ-099\nStatus: locked\n")
	os.WriteFile(filepath.Join(reqDir, "REQ-099.md"), reqContent, 0o644)

	// Compute the real hash.
	realSHA := sha256Hex(reqContent)

	state := stateAtVerificationMap(5)
	state["lifecycle"] = map[string]any{"state": "paused", "phase": nil, "phase_revision": float64(2)}
	state["pause"] = map[string]any{
		"from_state":               "verification",
		"from_phase":               "delivery",
		"phase_revision":           float64(1),
		"baseline_generation":      float64(1),
		"review_round":             float64(1),
		"entity_snapshot_revision": float64(0),
		"reason":                   "test",
		"required_human_action":    "test",
		"document_fingerprints": []any{
			map[string]any{
				"path":    "docs/requirements/REQ-099.md",
				"version": "1.0.0",
				"sha256":  realSHA,
			},
		},
		"committed_idempotency_keys": []string{},
		"paused_at":                  "2026-01-01T00:00:00Z",
	}
	registerFixtureEvidence(t, root, state, map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"pause_record":          "docs/reports/human/pause-record.md",
	})
	writeFullState(t, root, state)

	err := applyT(t, root, "TR-019", 5, "user", map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"pause_record":          "docs/reports/human/pause-record.md",
	})
	if err != nil {
		t.Fatalf("TR-019 should succeed when fingerprints match: %v", err)
	}
}
