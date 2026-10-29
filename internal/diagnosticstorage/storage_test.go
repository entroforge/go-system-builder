package diagnosticstorage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayStorageKeepsUnknownNeighborsAndSymlinksVisible(t *testing.T) {
	root := t.TempDir()
	dir := ".claude/diagnostics/planning/"
	valid := `{"message":"original interruption","at":"2026-10-25T12:00:00Z"}`
	for _, tc := range []struct {
		name, content string
		want          bool
	}{
		{strings.Repeat("a", 64) + ".json", valid, true},
		{strings.Repeat("b", 64) + ".json", `{"message":"x","at":"2026-10-25T12:00:00Z","product":true}`, false},
		{strings.Repeat("c", 64) + ".json", `{"message":"x","message":"y","at":"2026-10-25T12:00:00Z"}`, false},
		{strings.Repeat("d", 64) + ".json", `{"message":"x","at":"invalid"}`, false},
		{"product.go", valid, false},
		{"unknown.json", valid, false},
		{".diagnostic-" + strings.Repeat("a", 32) + ".tmp", "partial publication", true},
		{".diagnostic-unknown.tmp", "partial", false},
	} {
		path := dir + tc.name
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		if got := Recognizes(t.Context(), root, path); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
	link := dir + strings.Repeat("e", 64) + ".json"
	if err := os.Symlink(filepath.Join(root, dir+strings.Repeat("a", 64)+".json"), filepath.Join(root, link)); err == nil && Recognizes(t.Context(), root, link) {
		t.Fatal("symlink classified as display storage")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if Recognizes(ctx, root, dir+strings.Repeat("a", 64)+".json") {
		t.Fatal("canceled observation classified storage")
	}
}
