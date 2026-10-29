//go:build !windows

package targetedauthoring

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalOriginalRechecksConcurrentPublication(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"regular", "staged", "symlink", "stage-after-status", "ignore-after-status", "symlink-after-status"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			originalGit(t, root, "init", "-q")
			originalGit(t, root, "config", "user.email", "fixture@example.test")
			originalGit(t, root, "config", "user.name", "fixture")
			originalGit(t, root, "config", "core.excludesFile", os.DevNull)
			originalGit(t, root, "commit", "--allow-empty", "-qm", "base")
			rel := ".loop-submissions/s7/packet/owner/answer.json"
			target := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			script := `#!/bin/sh
if [ "$3" = status ] && [ ! -e "$GSB_ORIGINAL_RACE_MARK" ]; then
  case "$GSB_ORIGINAL_RACE_MODE" in
    regular) printf 'peer original' > "$GSB_ORIGINAL_RACE_TARGET" ;;
    staged) printf 'peer original' > "$GSB_ORIGINAL_RACE_TARGET"; "$GSB_ORIGINAL_RACE_GIT" -C "$GSB_ORIGINAL_RACE_ROOT" add -- "$GSB_ORIGINAL_RACE_REL" ;;
    symlink) ln -s "$GSB_ORIGINAL_RACE_ROOT" "$GSB_ORIGINAL_RACE_TARGET" ;;
    *-after-status)
      printf 'peer original' > "$GSB_ORIGINAL_RACE_TARGET"
      "$GSB_ORIGINAL_RACE_GIT" "$@" > "$GSB_ORIGINAL_RACE_MARK.status"
      case "$GSB_ORIGINAL_RACE_MODE" in
        stage-after-status) "$GSB_ORIGINAL_RACE_GIT" -C "$GSB_ORIGINAL_RACE_ROOT" add -- "$GSB_ORIGINAL_RACE_REL" ;;
        ignore-after-status) printf '.loop-submissions/\n' > "$GSB_ORIGINAL_RACE_ROOT/.gitignore" ;;
        symlink-after-status) rm "$GSB_ORIGINAL_RACE_TARGET"; ln -s "$GSB_ORIGINAL_RACE_ROOT" "$GSB_ORIGINAL_RACE_TARGET" ;;
      esac
      : > "$GSB_ORIGINAL_RACE_MARK"
      cat "$GSB_ORIGINAL_RACE_MARK.status"
      exit 0 ;;
  esac
  : > "$GSB_ORIGINAL_RACE_MARK"
fi
exec "$GSB_ORIGINAL_RACE_GIT" "$@"
`
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			mark := filepath.Join(t.TempDir(), "published")
			t.Setenv("GSB_ORIGINAL_RACE_GIT", realGit)
			t.Setenv("GSB_ORIGINAL_RACE_ROOT", root)
			t.Setenv("GSB_ORIGINAL_RACE_REL", rel)
			t.Setenv("GSB_ORIGINAL_RACE_TARGET", target)
			t.Setenv("GSB_ORIGINAL_RACE_MARK", mark)
			t.Setenv("GSB_ORIGINAL_RACE_MODE", mode)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			err := CheckNormalOriginal(context.Background(), root, rel)
			if _, statErr := os.Stat(mark); statErr != nil {
				t.Fatalf("status window did not execute: %v", statErr)
			}
			if mode == "regular" {
				if err != nil {
					t.Fatalf("peer-published ordinary original: %v", err)
				}
				data, readErr := os.ReadFile(target)
				if readErr != nil || string(data) != "peer original" {
					t.Fatalf("peer original changed: %q %v", data, readErr)
				}
			} else {
				want := map[string]string{
					"staged":               "conflicting Git status",
					"symlink":              "symlink or non-regular",
					"stage-after-status":   "conflicts with HEAD or index product",
					"ignore-after-status":  "is ignored by Git",
					"symlink-after-status": "symlink or non-regular",
				}[mode]
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("%s original was not rejected for %q: %v", mode, want, err)
				}
			}
		})
	}
}
