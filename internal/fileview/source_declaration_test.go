package fileview

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSourceDeclarationsNeedNoGitCheckout(t *testing.T) {
	root := t.TempDir()
	rules := []Rule{{Path: ".", Source: "git_tree"}, {Path: "docs/./evidence/", Source: "disk"}}
	original := append([]Rule(nil), rules...)
	for _, tc := range []struct{ path, source string }{
		{"docs/dev/contracts/BE-001.md", "git_tree"},
		{filepath.Join(root, "docs/evidence/report.json"), "disk"},
		{"docs/evidence-other/report.json", "git_tree"},
	} {
		source, err := SourceForRules(root, tc.path, rules)
		if err != nil || source != tc.source {
			t.Fatalf("declaration for %s: %q, %v", tc.path, source, err)
		}
	}
	if !reflect.DeepEqual(rules, original) {
		t.Fatal("declaration projection changed caller rules")
	}
	for _, path := range []string{"../escape.md", filepath.Join(root, "..", "escape.md")} {
		if _, err := SourceForRules(root, path, rules); err == nil {
			t.Fatalf("outside authority path was accepted: %s", path)
		}
	}
	for _, invalid := range [][]Rule{
		nil,
		{{Path: ".", Source: "unknown"}},
		{{Path: "docs", Source: "disk"}, {Path: "docs/./", Source: "git_tree"}},
		{{Path: "../outside", Source: "disk"}},
	} {
		if _, err := SourceForRules(root, "docs/report.md", invalid); err == nil {
			t.Fatalf("invalid declarations accepted: %#v", invalid)
		}
	}
}
