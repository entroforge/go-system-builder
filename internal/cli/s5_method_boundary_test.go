package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Follow the installed method's native and frozen-recovery paths against real
// issued artifacts. The execution view is a wrapper, not a second packet shape.
func TestS5MethodPathsResolveIssuedNativeAndFrozenMaterials(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	readObject := func(path string) map[string]any {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err != nil {
			t.Fatalf("read object %s: %v", path, err)
		}
		return obj
	}
	stringField := func(obj map[string]any, key string) string {
		t.Helper()
		value, ok := obj[key].(string)
		if !ok || value == "" {
			t.Fatalf("missing path %s in %#v", key, obj)
		}
		return value
	}
	resolveReadSet := func(root string, packet map[string]any) map[string][]byte {
		t.Helper()
		draft, ok := packet["draft"].(map[string]any)
		if !ok {
			t.Fatal("packet has no draft object")
		}
		items, ok := draft["read"].([]any)
		if !ok || len(items) == 0 {
			t.Fatal("draft.read has no issued sources")
		}
		resolved := make(map[string][]byte)
		for _, raw := range items {
			ref := stringField(raw.(map[string]any), "path")
			data, err := os.ReadFile(filepath.Join(root, ref))
			if err != nil {
				t.Fatalf("resolve documented read path %s: %v", ref, err)
			}
			resolved[ref] = data
		}
		analysis, ok := packet["task_analysis"].(map[string]any)
		if !ok {
			t.Fatal("packet has no task_analysis reference")
		}
		path := stringField(analysis, "path")
		artifact := readObject(filepath.Join(root, path))
		for _, field := range []string{"structural_check", "task_contexts", "granularity_advice"} {
			if _, present := artifact[field]; !present {
				t.Fatalf("separate analysis lacks %s", field)
			}
			if _, inline := analysis[field]; inline {
				t.Fatalf("analysis content unexpectedly in reference: %s", field)
			}
		}
		if got := workpacket.Digest(resolved[path]); got != stringField(analysis, "sha256") {
			t.Fatal("read list did not resolve exact analysis bytes")
		}
		return resolved
	}

	method, err := os.ReadFile(filepath.Join(f.root, ".claude/skills/document-verification/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"`draft.read`", "`answer_path`", "`task_analysis.path`", "`packet.draft.read`", "`packet.task_analysis.path`", "`authoring_path`"} {
		if !strings.Contains(string(method), field) {
			t.Fatalf("installed method omits the field used below: %s", field)
		}
	}
	native := readObject(filepath.Join(f.root, p.ViewPath))
	original := resolveReadSet(f.root, native)
	answer := readObject(filepath.Join(f.root, stringField(native, "answer_path")))
	if _, ok := answer["answer"].(map[string]any); !ok {
		t.Fatal("native answer path is not an authorable questionnaire")
	}

	execution := f.prepare(t, p)
	view := readObject(filepath.Join(execution.Path, workpacket.WorkerViewPath))
	wrapped, ok := view["packet"].(map[string]any)
	if !ok {
		t.Fatal("frozen execution view does not wrap its original packet")
	}
	// Recovery must still work after the mutable Main files disappear.
	for ref := range original {
		if err := os.Remove(filepath.Join(f.root, ref)); err != nil {
			t.Fatal(err)
		}
	}
	frozenRoot := filepath.Join(execution.Path, stringField(view, "frozen_input_root"))
	frozen := resolveReadSet(frozenRoot, wrapped)
	for ref, data := range original {
		if !bytes.Equal(data, frozen[ref]) {
			t.Fatalf("frozen recovery changed original source %s", ref)
		}
	}
	answer = readObject(filepath.Join(execution.Path, stringField(view, "authoring_path")))
	if _, ok := answer["answer"].(map[string]any); !ok {
		t.Fatal("frozen authoring_path is not an authorable questionnaire")
	}
}
