package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectRejectsUnsafeOrUnusableExplicitREQ(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name      string
		reqPath   string
		status    string
		version   string
		wantErr   error
		wantField string
	}{
		{
			name:      "path outside repository",
			reqPath:   filepath.Join(root, "..", "REQ-OUTSIDE.md"),
			status:    "locked",
			version:   "v1.0.0",
			wantErr:   ErrPathOutsideRepository,
			wantField: "path",
		},
		{
			name:      "filename is not REQ prefixed",
			reqPath:   "docs/requirements/not-a-req.md",
			status:    "locked",
			version:   "v1.0.0",
			wantErr:   ErrREQFilename,
			wantField: "filename",
		},
		{
			name:      "status is not locked",
			reqPath:   "docs/requirements/REQ-001.md",
			status:    "draft",
			version:   "v1.0.0",
			wantErr:   ErrREQNotLocked,
			wantField: "status",
		},
		{
			name:      "version is missing",
			reqPath:   "docs/requirements/REQ-002.md",
			status:    "locked",
			version:   "",
			wantErr:   ErrREQVersionMissing,
			wantField: "version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !filepath.IsAbs(tt.reqPath) {
				writeRecoveryREQ(t, root, tt.reqPath, tt.status, tt.version)
			}
			_, err := Inspect(root, tt.reqPath)
			if err == nil {
				t.Fatal("Inspect returned nil error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Inspect error = %v, want errors.Is(..., %v)", err, tt.wantErr)
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Inspect error = %v, want *ValidationError", err)
			}
			if validationErr.Field != tt.wantField {
				t.Fatalf("validation field = %q, want %q", validationErr.Field, tt.wantField)
			}
		})
	}
}

func TestInspectAcceptsLockedREQAndDamagedBOMRuntime(t *testing.T) {
	root := t.TempDir()
	reqPath := "docs/requirements/REQ-003.md"
	writeRecoveryREQ(t, root, reqPath, "locked", "v1.2.3")
	writeRecoveryFile(t, root, ".claude/loop-state.json", append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"broken":`)...))

	inventory, err := Inspect(root, reqPath)
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if inventory.REQ.ID != "REQ-003" || inventory.REQ.Status != "locked" || inventory.REQ.Version != "v1.2.3" {
		t.Fatalf("unexpected REQ binding: %#v", inventory.REQ)
	}
	if inventory.REQ.Path != reqPath {
		t.Fatalf("REQ path = %q, want %q", inventory.REQ.Path, reqPath)
	}
	if inventory.Root == "" || !filepath.IsAbs(inventory.Root) {
		t.Fatalf("inventory root = %q, want absolute inspection root", inventory.Root)
	}
	state, ok := inventoryInputByPath(inventory, ".claude/loop-state.json")
	if !ok {
		t.Fatal("damaged runtime state was not inventoried")
	}
	wantSHA := sha256HexForRecoveryTest(append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"broken":`)...))
	if state.SHA256 != wantSHA {
		t.Fatalf("state sha256 = %q, want %q", state.SHA256, wantSHA)
	}
}

func TestInspectUsesCanonicalREQIDFromDescriptiveFilename(t *testing.T) {
	root := t.TempDir()
	reqPath := "docs/requirements/REQ-039-loop-control-plane.md"
	writeRecoveryREQ(t, root, reqPath, "locked", "v2.0.0")

	inventory, err := Inspect(root, reqPath)
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if inventory.REQ.ID != "REQ-039" {
		t.Fatalf("REQ id = %q, want canonical REQ-039", inventory.REQ.ID)
	}
}

func TestInspectInventoryRecordsRepositoryRelativePathsAndSHA256(t *testing.T) {
	root := t.TempDir()
	writeRecoveryREQ(t, root, "docs/requirements/REQ-010.md", "locked", "v1.0.0")
	inputs := map[string][]byte{
		".claude/loop-state.json":      []byte("{not-json"),
		".claude/loop-events.jsonl":    []byte("event\n"),
		"docs/design/ARCH-010.md":      []byte("design"),
		".claude/evidence/ev-010.json": []byte(`{"id":"ev-010"}`),
	}
	for path, data := range inputs {
		writeRecoveryFile(t, root, path, data)
	}

	inventory, err := Inspect(root, "docs/requirements/REQ-010.md")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	for path, data := range inputs {
		item, ok := inventoryInputByPath(inventory, path)
		if !ok {
			t.Fatalf("inventory missing input %q", path)
		}
		if filepath.IsAbs(item.Path) || strings.HasPrefix(item.Path, "../") || item.Path == ".." {
			t.Fatalf("inventory path %q is not repository-relative", item.Path)
		}
		if item.SHA256 != sha256HexForRecoveryTest(data) {
			t.Fatalf("input %q sha256 = %q, want %q", path, item.SHA256, sha256HexForRecoveryTest(data))
		}
	}
}

func TestInspectInventoriesDefinitionAndPolicyUsedByRecoverySeed(t *testing.T) {
	root := t.TempDir()
	writeRecoveryREQ(t, root, "docs/requirements/REQ-011.md", "locked", "v1.0.0")
	writeRecoveryFile(t, root, "docs/loop-definition.json", []byte(`{"schema_version":"1.1.0"}`))
	writeRecoveryFile(t, root, "docs/hook-policy.json", []byte(`{"version":"v1","mode":"audit"}`))

	inventory, err := Inspect(root, "docs/requirements/REQ-011.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		if _, ok := inventoryInputByPath(inventory, path); !ok {
			t.Fatalf("recovery seed input %q missing from inventory", path)
		}
	}
}

func writeRecoveryREQ(t *testing.T, root, path, status, version string) {
	t.Helper()
	content := "# 需求：REQ-TEST\n\n> 状态：" + status + "\n"
	if version != "" {
		content += "> 版本：" + version + "\n"
	}
	writeRecoveryFile(t, root, path, []byte(content))
}

func writeRecoveryFile(t *testing.T, root, path string, data []byte) {
	t.Helper()
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(root, filepath.FromSlash(path))
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullPath, err)
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}

func inventoryInputByPath(inventory Inventory, path string) (InventoryInput, bool) {
	for _, input := range inventory.Inputs {
		if input.Path == path {
			return input, true
		}
	}
	return InventoryInput{}, false
}

func sha256HexForRecoveryTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
