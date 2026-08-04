package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitRunner abstracts the subset of git we need. The default implementation
// shells out to the system git; tests inject a fakeRunner so they do not
// depend on git being installed.
type gitRunner interface {
	// Run executes `git -C root <args>` and returns stdout. If the command
	// exits non-zero, the returned error wraps *exec.ExitError and the
	// captured stderr is included in the message.
	Run(ctx context.Context, root string, args ...string) (string, error)
	// RunStdin is like Run but writes the given stdin to the process. Used
	// for `git merge-tree` style inputs that expect a heredoc.
	RunStdin(ctx context.Context, stdin string, root string, args ...string) (string, error)
}

// execRunner is the production gitRunner. It uses os/exec and respects
// ctx cancellation.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, root string, args ...string) (string, error) {
	return runGit(ctx, root, nil, args...)
}

func (execRunner) RunStdin(ctx context.Context, stdin string, root string, args ...string) (string, error) {
	return runGit(ctx, root, strings.NewReader(stdin), args...)
}

func runGit(ctx context.Context, root string, stdin *strings.Reader, args ...string) (string, error) {
	fullArgs := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// defaultRunner returns the process-wide git runner. Tests substitute this
// via withRunner.
var defaultRunner gitRunner = execRunner{}

// withRunner temporarily swaps the package-level git runner. Tests use it
// to inject a fakeRunner; the swap is reverted by the returned func.
func withRunner(r gitRunner) func() {
	previous := defaultRunner
	defaultRunner = r
	return func() { defaultRunner = previous }
}

// acquireFileLock takes an exclusive O_EXCL create lock at lockPath and
// returns a release func. It is used by the checkpoint CAS to serialise
// concurrent writers without depending on the system flock syscall. The
// helper retries for up to 2 seconds and gives up if it cannot acquire
// the lock in that window — callers must treat a returned error as
// "lock contention, retryable" per BUG-039-05 §4.1.
func acquireFileLock(lockPath string) (func(), error) {
	if dir := filepath.Dir(lockPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir lock dir: %w", err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("close lock: %w", closeErr)
			}
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("lock timeout: %s", lockPath)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
