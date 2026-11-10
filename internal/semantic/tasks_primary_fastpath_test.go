package semantic

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func TestTaskPrimaryFieldFastPathSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{"absent", "> Status: cancelled\n", ""},
		{"normal", "> Primary contract: BE-001\n", "BE-001"},
		{"lowercase", "> primary contract: BE-001\n", ""},
		{"uppercase", "> PRIMARY CONTRACT: BE-001\n", ""},
		{"CRLF", "> Primary contract:\tBE-001\r\n", "BE-001"},
		{"literal in body", "Primary contract: BE-001\n", ""},
		{"indented header", " > Primary contract: BE-001\n", ""},
		{"first match", "> Primary contract: BE-001\n> Primary contract: FE-002\n", "BE-001"},
		{"whitespace across lines", ">\nPrimary contract:\nBE-001\n", "BE-001"},
		{"fenced header", "```\n> Primary contract: BE-001\n```\n", "BE-001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "docs/dev/tasks")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "TASK-001.md"), []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			tasks, err := loadTaskDocumentsWithFiles(root, fileview.Disk{Root: root})
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 1 {
				t.Fatalf("loaded %d tasks, want 1", len(tasks))
			}
			if got := tasks[0].contract; got != tc.want {
				t.Fatalf("contract = %q, want %q", got, tc.want)
			}
		})
	}
}
