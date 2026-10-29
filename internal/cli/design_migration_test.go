package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/designfoundation"
)

func TestDesignMigrationIsReadOnlyAndRejectsEveryWriteIntent(t *testing.T) {
	root := t.TempDir()
	legacy := "# Project Design Foundation\n> 状态：published\n> 版本：v1.0.0\n\n| Legacy ID | Unknown original column |\n|---|---|\n| existing-choice | 保留原设计与原 ID |\n"
	path := filepath.Join(root, "docs/design/DESIGN.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	before := dryRunTree(t, root)
	for _, tc := range []struct {
		name  string
		flags []string
		write bool
	}{
		{"default preview", nil, false},
		{"explicit preview", []string{"--dry-run"}, false},
		{"false write flag", []string{"--write=false"}, false},
		{"write", []string{"--write"}, true},
		{"write true", []string{"--write=true"}, true},
		{"disable preview", []string{"--dry-run=false"}, true},
		{"write with preview", []string{"--write", "--dry-run=true"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"design-foundation", "migrate", "--root", root, "--to", "contract-v1"}, tc.flags...)
			var out, diagnostic bytes.Buffer
			code := cli.Run(args, nil, &out, &diagnostic)
			if tc.write && (code == 0 || !strings.Contains(diagnostic.String(), "unsupported")) {
				t.Fatalf("unsupported migration write returned success or no useful refusal: code=%d stderr=%s", code, &diagnostic)
			}
			if !tc.write && code != 0 {
				t.Fatalf("read-only migration preview failed: %d %s", code, &diagnostic)
			}
			if !reflect.DeepEqual(before, dryRunTree(t, root)) {
				t.Fatal("migration preview/refusal changed files or generated debt/IDs")
			}
			var plan designfoundation.MigratePlan
			if err := json.Unmarshal(out.Bytes(), &plan); err != nil || !plan.DryRun || plan.Mode != "legacy-v1.0" {
				t.Fatalf("migration output is not an accurate read-only plan: %+v %v", plan, err)
			}
			for _, file := range plan.Files {
				if file.Path != "docs/design/DESIGN.md" {
					continue
				}
				mapping := strings.Join(append(file.ExpectedChanges, file.PreservedTextNote), "\n")
				if !strings.Contains(mapping, "docs/design/DESIGN-template.md") || !strings.Contains(mapping, "constraints") || !strings.Contains(mapping, "binding") {
					t.Fatalf("original file mapping still requires guessing the template/table/columns: %s", mapping)
				}
				return
			}
			t.Fatal("preview omitted its actual legacy source")
		})
	}
}
