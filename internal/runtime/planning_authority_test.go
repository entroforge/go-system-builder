package runtime_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestDesignAuthorityAssociatesCurrentEntryWithoutSourceProof(t *testing.T) {
	root := t.TempDir()
	ref := "docs/requirements/REQ-001.md"
	bytes := []byte("# REQ-001\n> Status: locked\n> Version: v1.0.0\n")
	os.MkdirAll(filepath.Join(root, "docs/requirements"), 0700)
	os.WriteFile(filepath.Join(root, ref), bytes, 0600)
	state := map[string]any{"schema_version": "1.2.0", "runtime_id": "loop-design", "configuration": map[string]any{"optimization": map[string]any{"packet": true}}, "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "design", "phase_revision": 2}, "bound_req": map[string]any{"id": "REQ-001", "path": ref, "version": "v1.0.0", "sha256": workpacket.Digest(bytes), "status": "locked", "workspace": map[string]any{"project_root": root}}}
	p, err := runtime.CurrentDesignPacket(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateDesignPacket(root, state, p, fileview.Disk{Root: root}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateDesignPacket(t.TempDir(), state, p, nil); err != nil {
		t.Fatalf("source location became an authoring qualification: %v", err)
	}
	os.WriteFile(filepath.Join(root, ref), []byte("drifted"), 0600)
	if err := runtime.ValidateDesignPacket(root, state, p, fileview.Disk{Root: root}); err != nil {
		t.Fatalf("current responsibility was rejected by source fingerprint: %v", err)
	}
	state["lifecycle"].(map[string]any)["phase_revision"] = 3
	if err := runtime.ValidateDesignPacket(root, state, p, nil); err == nil {
		t.Fatal("old entry associated with the current responsibility")
	}
	state["lifecycle"].(map[string]any)["phase"] = "contracts"
	if _, err := runtime.CurrentDesignPacket(state); err == nil {
		t.Fatal("S2 responsibility fabricated outside design")
	}
}
