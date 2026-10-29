package factory

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/processtree"
)

// BuildHarness builds the production entrypoint without race instrumentation.
// Platform deadline tests must exercise its process watchdog; calling cli.Run
// in a race test measures a different entrypoint and a different CPU workload.
// Domain concurrency tests continue to run their consumers in the race process.
func BuildHarness(t *testing.T) string {
	t.Helper()
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
