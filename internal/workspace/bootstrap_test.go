package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func trackedBootstrapFixture(t *testing.T, trackLauncher bool) (*ExecutionRegistry, Execution, context.Context) {
	t.Helper()
	b, ctx := repository(t)
	launcher := filepath.Join(b.MainRoot, filepath.FromSlash(bootstrapLauncherRel()))
	settings := filepath.Join(b.MainRoot, ".claude/settings.json")
	if err := os.MkdirAll(filepath.Dir(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".claude/agents", ".claude/skills"} {
		if err := os.MkdirAll(filepath.Join(b.MainRoot, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(launcher, []byte("inherited launcher bytes\x00\x01"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("{\"hooks\":{\"PreToolUse\":[]}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{".claude/settings.json"}
	if trackLauncher {
		paths = append(paths, bootstrapLauncherRel())
	}
	if _, err := Git(ctx, b.MainRoot, append([]string{"add", "-f", "--"}, paths...)...); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, b.MainRoot, "commit", "-m", "install project bootstrap files"); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"bound_req":  map[string]any{"workspace": Binding{ProjectRoot: b.MainRoot, DevBranch: b.Branch, ReleaseUpstream: "main"}.Map()},
		"runtime_id": "loop-test", "revision": 0, "baseline": map[string]any{"generation": 1},
	}
	saveControlState(t, b.MainRoot, state)
	e, err := b.Plan(ctx, state, "bootstrap-inheritance", "builder", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	if err := b.Materialize(ctx, e); err != nil {
		t.Fatal(err)
	}
	return b, e, ctx
}

func bootstrapLauncherRel() string {
	name := "loop-harness"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.ToSlash(filepath.Join(".claude", "bin", name))
}

func bootstrapExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "native-worker")
	if err := os.WriteFile(path, []byte("registered native executable\x00\xfe"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBootstrapReplacesOnlyGitTrackedInheritedLauncherAndSettings(t *testing.T) {
	b, e, _ := trackedBootstrapFixture(t, true)
	executable := bootstrapExecutable(t)
	if err := b.Bootstrap(e, executable); err != nil {
		t.Fatalf("replace unchanged inherited bootstrap files: %v", err)
	}
	wantLauncher, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	gotLauncher, err := os.ReadFile(filepath.Join(e.Path, filepath.FromSlash(bootstrapLauncherRel())))
	if err != nil || string(gotLauncher) != string(wantLauncher) {
		t.Fatalf("Worker launcher not installed: err=%v bytes=%q", err, gotLauncher)
	}
	wantSettings, err := os.ReadFile(filepath.Join(e.Path, ".claude/settings.json"))
	if err != nil || !strings.Contains(string(wantSettings), filepath.Base(bootstrapLauncherRel())+" hook --event PreToolUse") {
		t.Fatalf("Worker Hook settings not installed: err=%v bytes=%q", err, wantSettings)
	}
	// A receipt-backed retry accepts installed bytes that already match the
	// generated assets, while the inherited replacement exception is closed.
	if err := b.Bootstrap(e, executable); err != nil {
		t.Fatalf("idempotent bootstrap replay: %v", err)
	}
	if err := b.ValidateBootstrap(e); err != nil {
		t.Fatalf("validate replayed bootstrap: %v", err)
	}
}

func TestBootstrapRefusesChangedUntrackedAndSymlinkInheritedTargets(t *testing.T) {
	for _, kind := range []string{"unstaged", "staged", "untracked", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			b, e, ctx := trackedBootstrapFixture(t, kind != "untracked")
			target := filepath.Join(e.Path, filepath.FromSlash(bootstrapLauncherRel()))
			var wantPreserved []byte
			switch kind {
			case "unstaged":
				wantPreserved = []byte("Worker edit")
				if err := os.WriteFile(target, wantPreserved, 0700); err != nil {
					t.Fatal(err)
				}
			case "staged":
				if err := os.WriteFile(target, []byte("staged Worker edit"), 0700); err != nil {
					t.Fatal(err)
				}
				// The project fixture intentionally ignores all of .claude/, so
				// Git requires -f even though this exact path is in BaseCommit.
				if _, err := Git(ctx, e.Path, "add", "-f", "--", bootstrapLauncherRel()); err != nil {
					t.Fatal(err)
				}
				// Keep the worktree bytes equal to BaseCommit while leaving a
				// staged-only change in the index.
				wantPreserved = []byte("inherited launcher bytes\x00\x01")
				if err := os.WriteFile(target, wantPreserved, 0700); err != nil {
					t.Fatal(err)
				}
			case "untracked":
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				wantPreserved = []byte("untracked Worker file")
				if err := os.WriteFile(target, wantPreserved, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if err := b.Bootstrap(e, bootstrapExecutable(t)); err == nil {
				t.Fatal("accepted a changed, untracked or symlink target")
			}
			if kind == "unstaged" || kind == "staged" || kind == "untracked" {
				got, err := os.ReadFile(target)
				if err != nil || !bytes.Equal(got, wantPreserved) {
					t.Fatalf("changed target was overwritten: err=%v bytes=%q", err, got)
				}
			}
			if kind == "symlink" {
				info, err := os.Lstat(target)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("symlink target was replaced: info=%v err=%v", info, err)
				}
			}
		})
	}
}

func TestBootstrapReceiptClosesInheritedReplacementException(t *testing.T) {
	b, e, _ := trackedBootstrapFixture(t, true)
	executable := bootstrapExecutable(t)
	if err := b.Bootstrap(e, executable); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(e.Path, filepath.FromSlash(bootstrapLauncherRel()))
	if err := os.WriteFile(target, []byte("post-receipt Worker edit"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := b.Bootstrap(e, executable); err == nil || !strings.Contains(err.Error(), "preserve changed Worker bootstrap asset") {
		t.Fatalf("receipt-backed retry accepted changed installed asset: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "post-receipt Worker edit" {
		t.Fatalf("post-receipt Worker edit was overwritten: err=%v bytes=%q", err, got)
	}
}

func TestBootstrapResumesAndPreservesChangedAssets(t *testing.T) {
	b, e, _, _ := workerFixture(t)
	for _, path := range []string{"agents/builder.md", "skills/build/SKILL.md"} {
		path = filepath.Join(b.MainRoot, ".claude", path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("trusted asset"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(t.TempDir(), "native")
	if err := os.WriteFile(executable, []byte("native test fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.MainRoot, ".claude/credentials.json"), []byte("do not copy"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := b.Bootstrap(e, executable); err != nil {
			t.Fatal(err)
		}
		if err := b.ValidateBootstrap(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"credentials.json", "loop-state.json", "loop-events.jsonl"} {
		if _, err := os.Stat(filepath.Join(e.Path, ".claude", name)); !os.IsNotExist(err) {
			t.Fatalf("copied control file %s", name)
		}
	}
	asset := filepath.Join(e.Path, ".claude/agents/builder.md")
	if err := os.WriteFile(asset, []byte("local change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.ValidateBootstrap(e); err != nil {
		t.Fatalf("existing asset was rejected by byte authentication: %v", err)
	}
	if err := b.Bootstrap(e, executable); err == nil {
		t.Fatal("overwrote changed asset")
	}
	data, _ := os.ReadFile(asset)
	if string(data) != "local change" {
		t.Fatal("lost Worker data")
	}
}

func TestChangedInputDoesNotRequireContentAuthentication(t *testing.T) {
	b, ctx := repository(t)
	state := map[string]any{"bound_req": map[string]any{"workspace": Binding{ProjectRoot: b.MainRoot, DevBranch: b.Branch, ReleaseUpstream: "main"}.Map()}, "runtime_id": "loop-test", "baseline": map[string]any{"generation": 1}}
	e, err := b.Plan(ctx, state, "assignment", "builder", []string{"input.md"}, nil, []string{"input.md"})
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	if err := os.WriteFile(filepath.Join(b.MainRoot, "input.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Materialize(ctx, e); err != nil {
		t.Fatalf("changed input blocked original workspace preparation: %v", err)
	}
	if _, err := os.Stat(e.Path); err != nil {
		t.Fatalf("workspace was not materialized: %v", err)
	}
}

func TestBootstrapReceiptUsesActualPathsWithArbitraryStoredValues(t *testing.T) {
	b, e, _ := trackedBootstrapFixture(t, true)
	if err := b.Bootstrap(e, bootstrapExecutable(t)); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(e.Path, ".claude", "loop-bootstrap.json")
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	files := receipt["files"].(map[string]any)
	var onePath string
	index := 0
	for path := range files {
		onePath = path
		switch index % 3 {
		case 0:
			files[path] = map[string]any{"arbitrary": true}
		case 1:
			files[path] = []any{1, "value"}
		default:
			files[path] = false
		}
		index++
	}
	updated, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, updated, 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.ValidateBootstrap(e); err != nil {
		t.Fatalf("stored values narrowed existing path validation: %v", err)
	}
	if err := os.Remove(filepath.Join(e.Path, filepath.FromSlash(onePath))); err != nil {
		t.Fatal(err)
	}
	if err := b.ValidateBootstrap(e); err == nil {
		t.Fatal("missing actual installed asset path accepted")
	}
}
