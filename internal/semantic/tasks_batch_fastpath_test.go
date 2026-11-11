package semantic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

type batchGuardReader struct {
	fileview.Reader
	dirErr   error
	failPath string
	readErr  error
	reads    []string
}

func (r *batchGuardReader) ReadDir(p string) ([]os.DirEntry, error) {
	if r.dirErr != nil {
		return nil, r.dirErr
	}
	return r.Reader.ReadDir(p)
}
func (r *batchGuardReader) ReadFile(p string) ([]byte, error) {
	r.reads = append(r.reads, p)
	if p == r.failPath {
		return nil, r.readErr
	}
	return r.Reader.ReadFile(p)
}

func batchGuardWrite(t *testing.T, root, path, content string) {
	t.Helper()
	p := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// Reproduce the pre-optimization consumer over the complete loader so counts,
// ordering, nil results and error wrapping are compared to its original inputs.
func fullLoaderBatchResult(root string, files fileview.Reader) (complete, cancelled int, problems []string, err error) {
	tasks, err := loadTaskDocumentsWithFiles(root, files)
	if err != nil {
		return 0, 0, nil, err
	}
	for _, task := range tasks {
		switch task.status {
		case "complete":
			complete++
		case "cancelled":
			cancelled++
		case "":
			problems = append(problems, fmt.Sprintf("%s: no Status field — batch membership must be declared", task.rel))
		default:
			problems = append(problems, fmt.Sprintf("%s: status is %q, want complete (or cancelled)", task.rel, task.status))
		}
	}
	if complete == 0 {
		problems = append(problems, "no complete TASK document under docs/dev/tasks")
	}
	return
}

func assertBatchMatchesFull(t *testing.T, root string, files fileview.Reader) {
	t.Helper()
	wc, wx, wp, we := fullLoaderBatchResult(root, files)
	gc, gx, gp, ge := TaskBatchCompleteWithFiles(root, files)
	errorText := func(e error) string {
		if e == nil {
			return ""
		}
		return e.Error()
	}
	if gc != wc || gx != wx || !reflect.DeepEqual(gp, wp) || errorText(ge) != errorText(we) {
		t.Fatalf("batch=(%d,%d,%v,%v), full=(%d,%d,%v,%v)", gc, gx, gp, ge, wc, wx, wp, we)
	}
}

func TestTaskBatchFastPathMatchesFullLoader(t *testing.T) {
	for _, status := range []string{"", "> Status: complete\n", "> Status: cancelled\n", "> Status: pending\n", "> Status: Complete\n", "> 状态：complete\n", "> Status: complete\r\n", "> Status: \tcomplete \t\n", "> Status: pending\n> Status: complete\n", ">\nStatus:\ncomplete\n", "```\n> Status: cancelled\n```\n"} {
		t.Run(fmt.Sprintf("%q", status), func(t *testing.T) {
			root := t.TempDir()
			batchGuardWrite(t, root, "docs/dev/tasks/TASK-001.md", status)
			assertBatchMatchesFull(t, root, fileview.Disk{Root: root})
		})
	}
	t.Run("ownership and discovery order", func(t *testing.T) {
		root := t.TempDir()
		batchGuardWrite(t, root, ".claude/loop-state.json", `{"bound_req":{"id":"REQ-001"}}`)
		for name, body := range map[string]string{
			"TASK-001.md":          "> Status: pending\n> Source REQ refs: REQ-001\n",
			"TASK-002.md":          "> Status: complete\n> Source REQ refs: REQ-002\n",
			"TASK-003.md":          "> Status: cancelled\n",
			"TASK-004.md":          "> Status: complete\n> Source REQ refs: REQ-001 REQ-002\n",
			"TASK-005.md":          "no status\n",
			"TASK-006-template.md": "> Status: pending\n",
			"README.md":            "> Status: pending\n",
			"other.md":             "> Status: pending\n",
			"TASK-007.txt":         "> Status: pending\n",
		} {
			batchGuardWrite(t, root, "docs/dev/tasks/"+name, body)
		}
		if err := os.MkdirAll(filepath.Join(root, "docs/dev/tasks/TASK-000.md"), 0755); err != nil {
			t.Fatal(err)
		}
		assertBatchMatchesFull(t, root, fileview.Disk{Root: root})
		c, x, p, e := TaskBatchComplete(root)
		if e != nil || c != 1 || x != 1 || len(p) != 2 || !strings.HasPrefix(p[0], "docs/dev/tasks/TASK-001.md:") || !strings.HasPrefix(p[1], "docs/dev/tasks/TASK-005.md:") {
			t.Fatalf("unexpected discovery/status: %d %d %v %v", c, x, p, e)
		}
	})
}

func TestTaskBatchFastPathPreservesErrors(t *testing.T) {
	for _, kind := range []string{"missing directory", "directory error", "task read error", "foreign task read error"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			r := &batchGuardReader{Reader: fileview.Disk{Root: root}}
			sentinel := errors.New("test read failure")
			switch kind {
			case "directory error":
				r.dirErr = sentinel
			case "task read error", "foreign task read error":
				batchGuardWrite(t, root, ".claude/loop-state.json", `{"bound_req":{"id":"REQ-001"}}`)
				batchGuardWrite(t, root, "docs/dev/tasks/TASK-001.md", "> Status: complete\n")
				batchGuardWrite(t, root, "docs/dev/tasks/TASK-002.md", "> Status: complete\n> Source REQ refs: REQ-002\n")
				r.failPath = filepath.Join(root, "docs/dev/tasks/TASK-002.md")
				if kind == "task read error" {
					r.failPath = filepath.Join(root, "docs/dev/tasks/TASK-001.md")
				}
				r.readErr = sentinel
			}
			assertBatchMatchesFull(t, root, r)
			c, x, p, e := TaskBatchCompleteWithFiles(root, r)
			if kind != "missing directory" && (c != 0 || x != 0 || p != nil || !errors.Is(e, sentinel)) {
				t.Fatalf("error result=%d %d %v %v", c, x, p, e)
			}
		})
	}
}

func TestTaskBatchFastPathSkipsOnlyUnusedDetails(t *testing.T) {
	root := t.TempDir()
	batchGuardWrite(t, root, "docs/dev/tasks/TASK-001.md", "> Status: complete\n> Primary contract: BE-001\n> Reading policy: linked-v1\n\n## Delivered Clauses\n| BE-001 | §1 §2 |\n## Dependencies\n| TASK-002 |\n## Document Manifest\n| 1 | Spec | input | [missing](missing.md) | v1 | purpose | required |\n## Closing Contract\nassert ready\n")
	r := &batchGuardReader{Reader: fileview.Disk{Root: root}}
	full, err := loadTaskDocumentsWithFiles(root, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 1 || full[0].contract != "BE-001" || !full[0].hasClose || len(full[0].clauses) != 2 || len(full[0].deps) != 1 || len(full[0].problems) == 0 || len(r.reads) != 2 {
		t.Fatalf("full loader lost detail: %+v reads=%v", full, r.reads)
	}
	r.reads = nil
	c, x, p, e := TaskBatchCompleteWithFiles(root, r)
	if e != nil || c != 1 || x != 0 || p != nil || len(r.reads) != 1 {
		t.Fatalf("batch=%d %d %v %v reads=%v", c, x, p, e, r.reads)
	}
	assertBatchMatchesFull(t, root, r)
}
