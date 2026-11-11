package factory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/processtree"
)

// BuildHarness returns a job-provided production entrypoint when
// HOOK_ACCEPTANCE_BINARY is set, or builds one for this test otherwise. A
// provided binary must outlive all tests using it and use the ordinary,
// non-race build mode described below. Each CI job owns its binary; this
// helper does not cache a binary from a test's temporary directory.
//
// The fallback builds the production entrypoint without race instrumentation.
// Platform deadline tests must exercise its process watchdog; calling cli.Run
// in a race test measures a different entrypoint and a different CPU workload.
// Domain concurrency tests continue to run their consumers in the race process.
func BuildHarness(t *testing.T) string {
	t.Helper()
	if binary, provided, err := injectedHarness(); provided {
		if err != nil {
			t.Fatalf("HOOK_ACCEPTANCE_BINARY: %v", err)
		}
		return binary
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate harness sources")
	}
	name := "loop-harness"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/loop-harness")
	command.Dir = filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build production harness: %v\n%s", err, output)
	}
	return binary
}

// InstallHarness installs a production entrypoint into a private test project.
// An injected job binary is copied, never linked; without one, the original
// build-at-destination behavior is retained for local live tests.
func InstallHarness(t *testing.T, destination, repo string) {
	t.Helper()
	if os.Getenv("HOOK_ACCEPTANCE_BINARY") != "" {
		data, err := os.ReadFile(BuildHarness(t))
		if err != nil {
			t.Fatalf("read shared Hook binary: %v", err)
		}
		if err := os.WriteFile(destination, data, 0755); err != nil {
			t.Fatalf("install shared Hook binary: %v", err)
		}
		return
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", destination, "./cmd/loop-harness")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Hook binary: %v %s", err, output)
	}
}

// injectedHarness resolves paths before child processes change their working
// directory and reports an invalid explicit binary instead of silently
// building a different one.
func injectedHarness() (string, bool, error) {
	configured := os.Getenv("HOOK_ACCEPTANCE_BINARY")
	if configured == "" {
		return "", false, nil
	}
	path, err := filepath.Abs(configured)
	if err != nil {
		return "", true, fmt.Errorf("resolve %q: %w", configured, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", true, fmt.Errorf("stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", true, fmt.Errorf("%q is not a regular file", path)
	}
	return path, true, nil
}

// RunHook starts a fresh production process, including RunWithHookBudget. The
// test guard is longer than the platform deadline so it cannot replace the
// production watchdog or turn a missing reception into test success.
func RunHook(t *testing.T, binary, root, event string, payload []byte) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "hook", "--root", root, "--event", event)
	command.Dir = root
	command.Stdin = bytes.NewReader(payload)
	var out, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &out, &diagnostic
	err := processtree.Run(command)
	if ctx.Err() != nil {
		t.Fatalf("production Hook did not exit under its own budget: %v; stdout=%s stderr=%s", ctx.Err(), &out, &diagnostic)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out.String(), diagnostic.String()
	}
	if err != nil {
		t.Fatalf("start production Hook: %v", err)
	}
	return 0, out.String(), diagnostic.String()
}
