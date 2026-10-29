package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeInterruptedReceptionPreservesAnswerAndNamesOriginalRetry(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		err := fmt.Errorf("original writer: %w", cause)
		var diagnostic bytes.Buffer
		if code := nativeS9HandoffFailure("original-worker", "/original/result.json", err, &diagnostic); code != 0 {
			t.Fatal("interrupted return was blocked")
		}
		for _, want := range []string{"not a professional content rejection", "If already accepted", "original consumer owns", "does not establish completion", "/original/result.json"} {
			if !strings.Contains(diagnostic.String(), want) {
				t.Fatalf("missing %q: %s", want, &diagnostic)
			}
		}
		if strings.Contains(diagnostic.String(), "Consumer rejection:") {
			t.Fatal("timeout labeled content rejection")
		}
		for _, forbidden := range []string{"resume the same native Agent", "save the unchanged file again", "change the artifact id", "resume the original professional responsibility", "restore missing or stale sources"} {
			if strings.Contains(diagnostic.String(), forbidden) {
				t.Fatalf("timeout assigned transaction recovery to author: %s", &diagnostic)
			}
		}
	}
	if nativeReceptionInterrupted(errors.New("author omitted required assertion")) {
		t.Fatal("author error treated as interruption")
	}
}

func TestReviewPlanRecoveryHelpAndJSONFilePath(t *testing.T) {
	for _, verb := range []string{"revive", "revise"} {
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"runtime", "review-plan", verb, "--help"}, nil, &out, &diagnostic); code != 0 {
			t.Fatalf("help: %d %s", code, &diagnostic)
		}
		for _, want := range []string{"JSON file", "--source-ref", "--affected-surface", "does not restore files", "never use blanket reset/clean/restore"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("help missing %s: %s", want, &out)
			}
		}
	}
	root := t.TempDir()
	file := filepath.Join(root, "notes.md")
	if err := os.WriteFile(file, []byte("# not a ReviewPlan\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"runtime", "review-plan", "revise", "--root", root, "--file", file, "--source-ref", "finding-example", "--affected-surface", "internal/example"}, nil, &out, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "must contain JSON") || !strings.Contains(diagnostic.String(), file) {
		t.Fatalf("Markdown file misdiagnosed as inline JSON: %d %s", code, &diagnostic)
	}
}
