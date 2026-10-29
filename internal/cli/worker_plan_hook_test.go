package cli_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorkerQuestionnairePreToolIsObserverAndSaveUsesOriginalConsumer(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	e := f.prepare(t, p)
	registry, _, err := workspace.Load(f.root)
	if err != nil || registry == nil {
		t.Fatalf("load registered execution: registry=%v err=%v", registry, err)
	}
	paths, err := registry.WorkerSubmissionPaths(e)
	if err != nil {
		t.Fatalf("resolve frozen submission paths: %v", err)
	}
	if paths.Plan != "" || paths.Answer != workspace.AnswerDraftPath {
		t.Fatalf("S5 must expose the current answer path without a generic PLAN: %+v", paths)
	}
	answerPath := filepath.Join(e.Path, paths.Answer)
	before := stageSnapshot(t, f.root)
	beforeLifecycle := before.State["lifecycle"]

	for name, path := range map[string]string{
		"current_questionnaire": answerPath,
		"product_file":          filepath.Join(e.Path, "product.go"),
		"unknown_path":          filepath.Join(e.Path, "notes", "unregistered.md"),
		"other_assignment":      filepath.Join(f.root, filepath.FromSlash(f.packets[1].AnswerPath)),
	} {
		t.Run(name, func(t *testing.T) {
			code, output := stageHookCode(t, f.root, e.Path, e.AgentID, "PreToolUse", path)
			if code != 0 || strings.Contains(output, `"permissionDecision":"deny"`) {
				t.Fatalf("PreToolUse observer denied or failed for %s: code=%d output=%s", path, code, output)
			}
			after := stageSnapshot(t, f.root)
			if !reflect.DeepEqual(beforeLifecycle, after.State["lifecycle"]) {
				t.Fatalf("tool observation alone advanced the graph: before=%v after=%v", beforeLifecycle, after.State["lifecycle"])
			}
		})
	}

	receipt := saveStageHook(t, f.root, e.Path, e.AgentID, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	if !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("exact S5 professional answer did not reach the original consumer without generic PLAN: %+v", receipt)
	}
}
