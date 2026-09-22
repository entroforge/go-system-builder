package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObservationPreservesTreesAndDisablesGitHelpers(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	os.MkdirAll(filepath.Join(b.MainRoot, ".claude/agents"), 0700)
	os.MkdirAll(filepath.Join(b.MainRoot, ".claude/skills"), 0700)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Bootstrap(e, executable); err != nil {
		t.Fatal(err)
	}
	e.BootstrapSHA256, err = BootstrapDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "executed")
	helper := filepath.Join(t.TempDir(), "helper")
	os.WriteFile(helper, []byte("#!/bin/sh\necho bad > '"+marker+"'\n"), 0700)
	for _, key := range []string{"core.fsmonitor", "core.pager", "diff.external", "diff.custom.textconv"} {
		if _, err = Git(ctx, e.Path, "config", key, helper); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GIT_EXTERNAL_DIFF", helper)
	t.Setenv("GIT_PAGER", helper)
	// Real Claude Bash supplies GIT_EDITOR. Accept it without invoking the
	// editor, including during identity probes before the sanitized Git call.
	t.Setenv("GIT_EDITOR", helper)
	before, err := os.ReadFile(filepath.Join(b.CommonDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	workerGit, err := Git(ctx, e.Path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatal(err)
	}
	workerIndex, err := os.ReadFile(filepath.Join(workerGit, "index"))
	if err != nil {
		t.Fatal(err)
	}
	workerHead, err := os.ReadFile(filepath.Join(workerGit, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("observable modification\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, view := range observationViews {
		var out bytes.Buffer
		if err = b.Observe(ctx, e, view, &out); err != nil {
			t.Fatalf("%s: %v %s", view, err, out.String())
		}
		if (view == "status" || view == "diff") && !strings.Contains(out.String(), "input.md") {
			t.Fatalf("%s missing changed file: %s", view, out.String())
		}
		if view == "cwd" && strings.TrimSpace(out.String()) != e.Path {
			t.Fatal("wrong cwd")
		}
		if view == "log" && out.Len() == 0 {
			t.Fatal("missing history")
		}
	}
	if got, _ := os.ReadFile(filepath.Join(workerGit, "index")); !bytes.Equal(got, workerIndex) {
		t.Fatal("Worker index changed")
	}
	if got, _ := os.ReadFile(filepath.Join(workerGit, "HEAD")); !bytes.Equal(got, workerHead) {
		t.Fatal("Worker HEAD changed")
	}
	after, err := os.ReadFile(filepath.Join(b.CommonDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Main index changed")
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("Git helper executed")
	}
	for _, view := range []string{"log --output=bad", "diff; touch bad", "../status", "status > bad"} {
		if err = b.Observe(ctx, e, view, &bytes.Buffer{}); err == nil {
			t.Fatal("accepted arbitrary args")
		}
	}
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_TRACE", "GIT_CONFIG_COUNT"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, marker)
			if err := b.Observe(ctx, e, "status", &bytes.Buffer{}); err == nil {
				t.Fatalf("accepted inherited %s", key)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("inherited Git override wrote file")
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = b.Observe(cancelled, e, "log", &bytes.Buffer{}); err == nil {
		t.Fatal("cancelled observation ran")
	}
	e.Status = "retired"
	if err = b.Observe(ctx, e, "cwd", &bytes.Buffer{}); err == nil {
		t.Fatal("retired execution observed")
	}
}
func TestCheckPreflightExplicitMainAndSizeLimit(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	e.Checks = []string{"project-test"}
	e.CheckLocation = "main"
	if err := b.PreflightChecks(ctx, e, e.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunCheck(ctx, e, 0, &bytes.Buffer{}); err == nil {
		t.Fatal("Main check downgraded to Worker")
	}
	e.CheckLocation = "worker"
	f, err := os.Create(filepath.Join(e.Path, "oversize"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(513 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = b.PreflightChecks(ctx, e, e.Path); err == nil || !strings.Contains(err.Error(), "512 MiB") {
		t.Fatalf("size: %v", err)
	}
}

func TestObservationOutputIsBounded(t *testing.T) {
	var out bytes.Buffer
	w := observationOutput{writer: &out, remaining: 4}
	if n, err := w.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatal(n, err)
	}
	if _, err := w.Write([]byte("5")); err == nil || out.String() != "1234" {
		t.Fatal("output limit failed")
	}
}
