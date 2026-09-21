package projectlayout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyAuthoritiesRejectedWithoutWrites(t *testing.T) {
	for _, path := range []string{"docs/loop-definition.json", "docs/product/requirements/REQ-001.md", ".claude/loop-state.json"} {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, path)
			os.MkdirAll(filepath.Dir(p), 0755)
			data := []byte(`{"definition":{"path":"docs/loop-definition.json"}}`)
			os.WriteFile(p, data, 0644)
			if err := Check(root); err == nil || !strings.Contains(err.Error(), "layout migration required") {
				t.Fatalf("expected migration rejection: %v", err)
			}
			got, _ := os.ReadFile(p)
			if string(got) != string(data) {
				t.Fatal("check mutated authority")
			}
		})
	}
}
func TestNewLayoutAndHistoricalReferences(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".claude"), 0755)
	os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), []byte(`{"definition":{"path":"docs/control/loop-definition.json"},"baseline":{"generation":2},"documents":[{"path":"docs/contracts/old.md","generation":1}]}`), 0644)
	if err := Check(root); err != nil {
		t.Fatal(err)
	}
}

func TestFlatRequirementsAndMixedProductLayout(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{Definition, Requirements + "/REQ-001.md"} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("new layout"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Check(root); err != nil {
		t.Fatalf("flat requirements rejected: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/product"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err == nil {
		t.Fatal("retired empty product wrapper accepted")
	}
	old := filepath.Join(root, "docs/product/requirements/REQ-001.md")
	if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("legacy"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err == nil || !strings.Contains(err.Error(), "docs/product") {
		t.Fatalf("mixed layout accepted: %v", err)
	}
	got, err := os.ReadFile(old)
	if err != nil || string(got) != "legacy" {
		t.Fatal("legacy input changed", err)
	}
}

func TestRequirementRuntimeReferences(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		reject     bool
	}{
		{"flat bound", `{"bound_req":{"path":"docs/requirements/REQ-001.md"}}`, false},
		{"old bound", `{"bound_req":{"path":"docs/product/requirements/REQ-001.md"}}`, true},
		{"normalized old bound", `{"bound_req":{"path":"docs/requirements/../product/requirements/REQ-001.md"}}`, true},
		{"current document", `{"baseline":{"generation":2},"documents":[{"generation":2,"path":"docs/product/requirements/REQ-001.md"}]}`, true},
		{"historical document", `{"baseline":{"generation":2},"documents":[{"generation":1,"path":"docs/product/requirements/REQ-001.md"}]}`, false},
		{"old control with flat req", `{"definition":{"path":"docs/loop-definition.json"},"bound_req":{"path":"docs/requirements/REQ-001.md"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckRuntime([]byte(tc.data))
			if (err != nil) != tc.reject {
				t.Fatalf("reject=%v error=%v", tc.reject, err)
			}
		})
	}
}
