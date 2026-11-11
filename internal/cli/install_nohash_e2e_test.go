package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installReleaseFixture stages the actual allowlisted assets and current
// generated Manual. Its tiny host-binary placeholder is never executed; the
// test exercises the installer in-process, not release or platform acceptance.
func installReleaseFixture(t *testing.T) string {
	t.Helper()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	include, err := os.ReadFile(filepath.Join(repo, "packaging", "include.txt"))
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "release")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(include), "\n") {
		item := strings.TrimSpace(line)
		if item == "" || strings.HasPrefix(item, "#") {
			continue
		}
		if err := copyInstallTree(filepath.Join(repo, filepath.FromSlash(item)), filepath.Join(stage, filepath.FromSlash(item))); err != nil {
			t.Fatalf("stage %s: %v", item, err)
		}
	}
	for _, pair := range [][2]string{{"packaging/README.installed.md", "docs/README.md"}, {"packaging/DOCUMENT-MAP.installed.md", "docs/DOCUMENT-MAP.md"}, {"packaging/project.gitattributes", "project.gitattributes"}, {"tools/loop-harness-launcher.sh", ".claude/bin/loop-harness"}} {
		if err := copyInstallTree(filepath.Join(stage, pair[0]), filepath.Join(stage, pair[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(stage, "packaging")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(stage, ".claude/bin/loop-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	binary := "loop-harness-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := os.WriteFile(filepath.Join(stage, ".claude/bin", binary), []byte("fixture host executable; not executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "prelude.md"), []byte("# Getting started\n\nRead [onboarding](docs/guides/getting-started.md).\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "INSTALL.md"), []byte("# Install\n\nRead [installation](docs/guides/install.md).\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := runManual([]string{"--root", stage, "--target", "loop-harness.md"}, &out, &stderr); code != 0 {
		t.Fatalf("generate actual Manual: %d %s", code, stderr.String())
	}
	return stage
}

func TestInstallOriginalEntryIgnoresOldDigestManifest(t *testing.T) {
	source := installReleaseFixture(t)
	// A malformed historical manifest is neither needed nor rewritten. An
	// unrelated extra file does not become a release-admission condition.
	legacyPath := filepath.Join(source, "release-manifest.json")
	legacy := []byte(`{"files":{"docs/control/loop-definition.json":"deliberately-wrong"}}`)
	if err := os.WriteFile(legacyPath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "unrelated-extra.txt"), []byte("not an input"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"old-wrong", "missing"} {
		if mode == "missing" {
			if err := os.Remove(legacyPath); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(t.TempDir(), "project")
		var out, stderr bytes.Buffer
		if code := runInstall([]string{"--source", source, "--root", target}, &out, &stderr); code != 0 {
			t.Fatalf("%s install exit %d: %s", mode, code, stderr.String())
		}
		if _, err := os.Stat(filepath.Join(target, ".claude", "loop-state.json")); err != nil {
			t.Fatalf("actual Runtime missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, ".claude", "framework-installation.json")); !os.IsNotExist(err) {
			t.Fatalf("unexpected digest receipt: %v", err)
		}
		if mode == "old-wrong" {
			got, err := os.ReadFile(legacyPath)
			if err != nil || !bytes.Equal(got, legacy) {
				t.Fatalf("legacy source changed: %v", err)
			}
		}
	}
	if err := os.Remove(filepath.Join(source, "docs/control/loop-definition.json")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "missing-resource-project")
	var out, stderr bytes.Buffer
	if code := runInstall([]string{"--source", source, "--root", target}, &out, &stderr); code == 0 {
		t.Fatal("missing actual Definition accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed install published a target: %v", err)
	}
}
