package transition

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDesignRegistrationBatchMatchesOriginalActionMembership(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs/architecture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"ARCHITECTURE-current.md":     "> Status: locked\n> REQ: REQ-039\n",
		"ARCHITECTURE-legacy.md":      "> Status: locked\n",
		"ARCHITECTURE-alternative.md": "> Status: draft\n> REQ: REQ-039\n",
		"ARCHITECTURE-other.md":       "> Status: draft\n> REQ: REQ-040\n",
		"ARCHITECTURE-cancelled.md":   "> Status: cancelled\n> REQ: REQ-039\n",
		"ARCHITECTURE-template.md":    "> Status: draft\n",
		"ARCHITECTURE-stub.md":        "# No declared status\n",
		"README.md":                   "> Status: draft\n",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{"bound_req": map[string]any{"id": "REQ-039"}, "baseline": map[string]any{"generation": 1}}
	batch, err := DesignRegistrationBatch(root, state)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, doc := range batch {
		got = append(got, doc.ID)
		if doc.ID == "ARCHITECTURE-legacy" && !doc.LegacyOwner {
			t.Fatal("legacy membership must be explained")
		}
	}
	want := []string{"ARCHITECTURE-alternative", "ARCHITECTURE-current", "ARCHITECTURE-legacy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batch = %v, want %v", got, want)
	}
	if batch[0].Status != "draft" {
		t.Fatal("projection hid the original outlet's draft gap")
	}
	if _, err := registerDocumentsFromDisk(root, state, &ActionContext{}, "docs/architecture", []string{"ARCHITECTURE-"}, "design", "locked"); err == nil || !strings.Contains(err.Error(), batch[0].Path) {
		t.Fatalf("original action did not report the projected gap: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ARCHITECTURE-alternative.md"), []byte("> Status: locked\n> REQ: REQ-039\n"), 0600); err != nil {
		t.Fatal(err)
	}
	n, err := registerDocumentsFromDisk(root, state, &ActionContext{}, "docs/architecture", []string{"ARCHITECTURE-"}, "design", "locked")
	if err != nil || n != len(batch) {
		t.Fatalf("registration = %d, %v", n, err)
	}
	for i, raw := range state["documents"].([]any) {
		if raw.(map[string]any)["path"] != batch[i].Path {
			t.Fatalf("registered membership differs from projection: %v", state["documents"])
		}
	}
}
