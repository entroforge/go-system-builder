package assignment_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

func TestRegisterWorkgroupHonorsCanceledHookContextBeforeReadingOrWriting(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	original := []byte("must not be parsed after request cancellation")
	if err := os.WriteFile(statePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := assignment.Register(root, statePath, filepath.Join(root, "events.jsonl"), assignment.Request{Context: ctx, ExpectedRevision: -1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost original hook cancellation: %v", err)
	}
	data, _ := os.ReadFile(statePath)
	if string(data) != string(original) {
		t.Fatal("canceled registration rewrote state")
	}
	if _, err := os.Stat(filepath.Join(root, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("canceled registration created journal: %v", err)
	}
}
