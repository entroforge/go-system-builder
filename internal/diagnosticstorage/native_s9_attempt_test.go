package diagnosticstorage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeS9RecognizesOnlyExactOldNewAndTemporaryShapes(t *testing.T) {
	root := t.TempDir()
	dir := ".claude/diagnostics/native-s9/"
	old := `{"scope":"` + strings.Repeat("a", 64) + `","runtime":"original","message":"specific failure","files":{}}`
	attempt := `{"kind":"Builder","responsibility_ref":"original-plan","owner":"original-owner","stage":"plan-accepted","input_ref":"","typed_input_ref":"","failure_kind":"deadline","observed_at":"2026-10-26T00:00:00Z"}`
	current := strings.TrimSuffix(old, "}") + `,"attempt":` + attempt + `}`
	for i, tc := range []struct {
		data string
		want bool
	}{
		{old, true}, {current, true},
		{strings.TrimSuffix(current, "}") + `,"consumer_failure":{"message":"original count mismatch","files":{},"attempt":` + attempt + `}}`, true},
		{strings.TrimSuffix(current, "}") + `,"consumer_failure":{"message":"original count mismatch","files":{},"attempt":` + attempt + `,"product":true}}`, false},
		{strings.TrimSuffix(current, "}") + `,"consumer_failure":{"message":"original count mismatch","message":"duplicate","files":{},"attempt":` + attempt + `}}`, false},
		{strings.Replace(current, `"failure_kind":"deadline"`, `"failure_kind":"author"`, 1), true},
		{strings.Replace(current, `"failure_kind":"deadline"`, `"failure_kind":"conflict"`, 1), true},
		{strings.Replace(current, `"failure_kind":"deadline"`, `"failure_kind":"successor"`, 1), true},
		{strings.Replace(current, `"kind":"Builder"`, `"kind":"Builder","kind":"Targeted"`, 1), false},
		{strings.Replace(current, `"input_ref":"",`, "", 1), false},
		{strings.Replace(current, `"stage":"plan-accepted"`, `"stage":"plan-accepted","product":"source"`, 1), false},
		{strings.Replace(current, `"failure_kind":"deadline"`, `"failure_kind":"guessed-author-error"`, 1), false},
	} {
		path := dir + fmt.Sprintf("%064x", i+1) + ".json"
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		if got := Recognizes(t.Context(), root, path); got != tc.want {
			t.Errorf("shape %d: %v", i, got)
		}
	}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{".diagnostic-" + strings.Repeat("a", 32) + ".tmp", true},
		{".diagnostic-unknown.tmp", false}, {"neighbor.json", false}, {".publish.lock", false},
	} {
		path := dir + tc.name
		if err := os.WriteFile(filepath.Join(root, path), []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
		if got := Recognizes(t.Context(), root, path); got != tc.want {
			t.Errorf("temporary %s: %v", tc.name, got)
		}
	}
}

func TestNativeS9PublishLockRecognitionIsExact(t *testing.T) {
	root := t.TempDir()
	ref := ".claude/diagnostics/native-s9/.publish.lock"
	path := filepath.Join(root, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if !Recognizes(t.Context(), root, ref) {
		t.Fatal("exact empty publish lock not recognized")
	}
	if err := os.WriteFile(path, []byte("product"), 0600); err != nil {
		t.Fatal(err)
	}
	if Recognizes(t.Context(), root, ref) {
		t.Fatal("product content hidden as lock")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere"), path); err != nil {
		t.Fatal(err)
	}
	if Recognizes(t.Context(), root, ref) {
		t.Fatal("symlink hidden as lock")
	}
}
