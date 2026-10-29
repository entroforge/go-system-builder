package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestStageAnswerReceiptRejectsPendingRuntimeWithoutMutation(t *testing.T) {
	root := t.TempDir()
	control := filepath.Join(root, ".claude")
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(control, "loop-state.json")
	journalPath := filepath.Join(control, "loop-events.jsonl")
	markerPath := statePath + ".commit-pending.json"
	for path, data := range map[string][]byte{
		statePath:   []byte("{}\n"),
		journalPath: []byte("old journal bytes\n"),
		markerPath:  []byte("{\"schema_version\":\"1.0.0\",\"pending\":true}\n"),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	markerBefore, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "stage-answer", "receipt",
		"--root", root,
		"--agent", "builder-1",
		"--packet", "packet-receipt-read-only",
	}, nil, &stdout, &stderr)
	if code == 0 || !strings.Contains(strings.ToLower(stderr.String()), "pending") {
		t.Fatalf("receipt query result code=%d stderr=%q stdout=%q, want explicit pending refusal", code, stderr.String(), stdout.String())
	}
	for path, before := range map[string][]byte{
		statePath:   stateBefore,
		journalPath: journalBefore,
		markerPath:  markerBefore,
	} {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("read-only receipt query changed %s", path)
		}
	}
}
