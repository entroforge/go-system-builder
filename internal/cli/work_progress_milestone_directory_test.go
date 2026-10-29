package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func progressMilestoneDirectoryFixture(t *testing.T, lifecycle string, reads []string) (string, map[string]any) {
	t.Helper()
	root := initFormatRoot(t)
	state, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	life := state["lifecycle"].(map[string]any)
	life["state"] = lifecycle
	life["phase"] = nil
	life["phase_revision"] = 1
	reqPath := "docs/requirements/REQ-progress.md"
	reqBytes := []byte("locked progress fixture requirement\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, reqPath)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, reqPath), reqBytes, 0600); err != nil {
		t.Fatal(err)
	}
	state["bound_req"] = map[string]any{
		"id": "REQ-progress", "path": reqPath, "version": "v1.0.0", "sha256": workpacket.Digest(reqBytes),
	}
	milestone := state["milestone"].(map[string]any)
	milestone["stage"] = map[string]string{"planning": "S2", "document_verification": "S5"}[lifecycle]
	milestone["lifecycle_state"] = lifecycle
	milestone["read"] = stringProgressAny(reads)
	milestone["missing"] = []any{"current original responsibility"}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	return root, state
}

func stringProgressAny(values []string) []any {
	result := make([]any, len(values))
	for i := range values {
		result[i] = values[i]
	}
	return result
}

func writeProgressSource(t *testing.T, root, rel, content string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return full
}

func progressReadInput(root, path, content string) policy.Input {
	return policy.Input{
		CWD:       root,
		ToolInput: map[string]any{"file_path": path, "offset": 1, "limit": 1},
		ToolResponse: map[string]any{"file": map[string]any{
			"filePath": path, "startLine": float64(1), "numLines": float64(1), "content": content,
		}},
	}
}

func TestMainS2S5MilestoneDirectoriesProjectActualReadSources(t *testing.T) {
	for _, tc := range []struct {
		name, lifecycle string
		reads           []string
		actualRel       string
		actualContent   string
	}{
		{
			name: "S2", lifecycle: "planning",
			reads:     []string{"docs/requirements/REQ-progress.md", "docs/architecture/", "docs/design/", "docs/rules/"},
			actualRel: "docs/architecture/ARCHITECTURE-001.md", actualContent: "S2 architecture source\n",
		},
		{
			name: "S5", lifecycle: "document_verification",
			reads:     []string{"docs/requirements/REQ-progress.md", "docs/architecture/", "docs/design/", "docs/dev/contracts/", "docs/dev/tasks/"},
			actualRel: "docs/dev/tasks/TASK-001.md", actualContent: "S5 task source\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, state := progressMilestoneDirectoryFixture(t, tc.lifecycle, tc.reads)
			for _, ref := range tc.reads {
				if strings.HasSuffix(ref, "/") {
					if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(ref)), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			writeProgressSource(t, root, "docs/design/ADR-001.md", "design source\n")
			writeProgressSource(t, root, "docs/rules/dispatch-plan.md", "rules source\n")
			writeProgressSource(t, root, "docs/dev/contracts/BE-001.md", "contract source\n")
			path := writeProgressSource(t, root, tc.actualRel, tc.actualContent)

			facts, _, err := projectWorkProgress(root, policy.Input{CWD: root, SessionID: "main-directory-read"}, runtime.Snapshot{State: state})
			if err != nil || !facts.Known {
				t.Fatalf("directory milestone source projection failed: known=%t err=%v", facts.Known, err)
			}
			if got := facts.Sources[path]; got != workpacket.Digest([]byte(tc.actualContent)) {
				t.Fatalf("projected source SHA=%q; want actual file SHA", got)
			}
			if _, exists := facts.Sources[filepath.Join(root, "docs/architecture")]; exists {
				t.Fatal("directory itself was projected as a regular source file")
			}
			read := actualProgressRead(root, progressReadInput(root, path, tc.actualContent), facts.Sources)
			if read == nil || read.Path != path || read.Digest != workpacket.Digest([]byte(tc.actualContent)) || read.Range != "1:1" {
				t.Fatalf("actual read beneath declared directory was not attributed: %+v", read)
			}

			outside := writeProgressSource(t, root, "docs/unlisted/outside.md", "outside source\n")
			if read := actualProgressRead(root, progressReadInput(root, outside, "outside source\n"), facts.Sources); read != nil {
				t.Fatalf("directory outside milestone read scopes was attributed: %+v", read)
			}
		})
	}
}

func TestMainMilestoneDirectoryProjectionRejectsMissingAndSymlinkDirectories(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		root := initFormatRoot(t)
		sources := map[string]string{}
		remaining := progressSourceProjectionByteLimit
		err := projectMilestoneReadSources(sources, &remaining, root, []any{"docs/missing/"})
		if err == nil || !strings.Contains(err.Error(), "inspect milestone directory source") {
			t.Fatalf("missing declared directory was not reported incomplete: %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root := initFormatRoot(t)
		if err := os.MkdirAll(filepath.Join(root, "docs"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "docs/real"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", filepath.Join(root, "docs/link")); err != nil {
			t.Fatal(err)
		}
		sources := map[string]string{}
		remaining := progressSourceProjectionByteLimit
		err := projectMilestoneReadSources(sources, &remaining, root, []any{"docs/link/"})
		if err == nil || !strings.Contains(err.Error(), "contains a symlink") {
			t.Fatalf("symlink directory was not reported incomplete: %v", err)
		}
	})
}

func TestMainMilestoneDirectoryProjectionKeepsEntryByteAndFormalSHABounds(t *testing.T) {
	t.Run("entry ceiling", func(t *testing.T) {
		root := initFormatRoot(t)
		for i := 0; i < 256; i++ {
			writeProgressSource(t, root, fmt.Sprintf("docs/large/f-%03d.md", i), "x")
		}
		sources := map[string]string{}
		remaining := progressSourceProjectionByteLimit
		err := projectMilestoneReadSources(sources, &remaining, root, []any{"docs/large/"})
		if err == nil || !strings.Contains(err.Error(), "exceeds 256 entries") {
			t.Fatalf("over-entry directory was not reported incomplete: %v", err)
		}
	})
	t.Run("byte ceiling", func(t *testing.T) {
		root := initFormatRoot(t)
		large := make([]byte, progressSourceProjectionByteLimit+1)
		writeProgressSource(t, root, "docs/large/oversized.md", string(large))
		sources := map[string]string{}
		remaining := progressSourceProjectionByteLimit
		err := projectMilestoneReadSources(sources, &remaining, root, []any{"docs/large/"})
		if err == nil || !strings.Contains(err.Error(), "exceeds 4194304 bytes") {
			t.Fatalf("over-budget directory was not reported incomplete: %v", err)
		}
	})
	t.Run("formal packet SHA remains exact", func(t *testing.T) {
		root := initFormatRoot(t)
		writeProgressSource(t, root, "docs/formal/source.md", "changed source\n")
		refs := []workpacket.Reference{{Path: "docs/formal/source.md", SHA256: workpacket.Digest([]byte("issued source\n"))}}
		sources := map[string]string{}
		remaining := progressSourceProjectionByteLimit
		err := projectProgressSourceInputs(sources, &remaining, root, refs)
		if err == nil || err.Error() != "necessary source differs from issued binding" {
			t.Fatalf("formal packet SHA mismatch was relaxed: %v", err)
		}
	})
}

func TestMainS6LockedReadCategoriesResolveRegisteredCurrentFiles(t *testing.T) {
	root, state := progressMilestoneDirectoryFixture(t, "building", []string{"locked contracts", "locked TASKs"})
	state["baseline"].(map[string]any)["generation"] = 1
	contract := "docs/dev/contracts/BE-001.md"
	task := "docs/dev/tasks/TASK-001.md"
	state["documents"] = []any{
		map[string]any{"kind": "task", "path": task, "status": "locked", "generation": 1},
		map[string]any{"kind": "contract", "path": contract, "status": "locked", "generation": 1},
		map[string]any{"kind": "contract", "path": contract, "status": "locked", "generation": 1},
		map[string]any{"kind": "contract", "path": "docs/old.md", "status": "locked", "generation": 0},
		map[string]any{"kind": "task", "path": "docs/draft.md", "status": "draft", "generation": 1},
	}
	actualContract := writeProgressSource(t, root, contract, "locked contract source\n")
	actualTask := writeProgressSource(t, root, task, "locked task source\n")
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	got := resolveBoundREQRead([]string{"locked contracts", "locked TASKs"}, state)
	if len(got) != 2 || got[0] != contract || got[1] != task {
		t.Fatalf("category selected stale/draft/duplicate document paths: %v", got)
	}
	facts, _, err := projectWorkProgress(root, policy.Input{CWD: root, SessionID: "main-s6-category"}, runtime.Snapshot{State: state})
	if err != nil || !facts.Known {
		t.Fatalf("actual S6 files were treated as category filenames: known=%t err=%v", facts.Known, err)
	}
	if facts.Sources[actualContract] != workpacket.Digest([]byte("locked contract source\n")) || facts.Sources[actualTask] != workpacket.Digest([]byte("locked task source\n")) {
		t.Fatalf("category projection omitted actual source hashes: %+v", facts.Sources)
	}
	if err := os.Remove(actualTask); err != nil {
		t.Fatal(err)
	}
	if _, _, err := projectWorkProgress(root, policy.Input{CWD: root, SessionID: "main-s6-missing"}, runtime.Snapshot{State: state}); err == nil {
		t.Fatal("missing actual registered source was hidden")
	}
}

func TestMainMilestoneExpandedReadSourcesStayBounded(t *testing.T) {
	reads := make([]any, 257)
	for i := range reads {
		reads[i] = fmt.Sprintf("docs/task-%d.md", i)
	}
	remaining := progressSourceProjectionByteLimit
	if err := projectMilestoneReadSources(map[string]string{}, &remaining, t.TempDir(), reads); err == nil || !strings.Contains(err.Error(), "exceeds 256 entries") {
		t.Fatalf("expanded read list escaped original bound: %v", err)
	}
	if remaining != progressSourceProjectionByteLimit {
		t.Fatal("over-limit list performed file reads before rejection")
	}
}
