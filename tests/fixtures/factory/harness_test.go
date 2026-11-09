package factory

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallHarnessUsesIndependentPrivateCopiesAndBuildFallback(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", ".."))
	t.Setenv("HOOK_ACCEPTANCE_BINARY", "")
	source := BuildHarness(t)
	t.Setenv("HOOK_ACCEPTANCE_BINARY", source)
	name := "loop-harness"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	install := func() string {
		t.Helper()
		path := filepath.Join(t.TempDir(), ".claude", "bin", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		InstallHarness(t, path, repo)
		return path
	}
	checkVersion := func(path string) {
		t.Helper()
		output, err := exec.CommandContext(t.Context(), path, "version").CombinedOutput()
		if err != nil {
			t.Fatalf("run installed harness: %v: %s", err, output)
		}
		var info struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(output, &info); err != nil || info.Version == "" {
			t.Fatalf("installed harness version: %s: %v", output, err)
		}
		if runtime.GOOS != "windows" {
			stat, err := os.Stat(path)
			if err != nil || stat.Mode().Perm()&0111 == 0 {
				t.Fatalf("installed harness is not executable: %v", err)
			}
		}
	}
	first, second := install(), install()
	for _, path := range []string{source, first, second} {
		checkVersion(path)
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	firstInfo, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(sourceInfo, firstInfo) || os.SameFile(firstInfo, secondInfo) {
		t.Fatal("installed test binaries must be independent copies")
	}
	if err := os.WriteFile(first, []byte("changed by first test"), 0755); err != nil {
		t.Fatal(err)
	}
	checkVersion(source)
	checkVersion(second)
	// An ordinary local test without a job binary still builds at the exact
	// destination used by the live scenario.
	t.Setenv("HOOK_ACCEPTANCE_BINARY", "")
	checkVersion(install())
}

func TestBuildHarnessUsesJobBinaryAcrossTests(t *testing.T) {
	// The job owns the binary, not the first test requesting it. In particular,
	// completing one child test must not invalidate its siblings' entrypoint.
	binary := filepath.Join(t.TempDir(), "job artifacts", "loop-harness")
	if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("job-owned binary"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOOK_ACCEPTANCE_BINARY", binary)
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			if got := BuildHarness(t); got != binary {
				t.Fatalf("harness = %q, want job binary %q", got, binary)
			}
			if _, err := os.Stat(binary); err != nil {
				t.Fatalf("job binary unavailable: %v", err)
			}
		})
	}
}

func TestInjectedHarnessReportsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(*testing.T) string
		want string
	}{
		{"missing", func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }, "stat"},
		{"directory", func(t *testing.T) string { return t.TempDir() }, "not a regular file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOOK_ACCEPTANCE_BINARY", tc.path(t))
			_, provided, err := injectedHarness()
			if !provided || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("provided = %v, error = %v, want %q", provided, err, tc.want)
			}
		})
	}
}
