package e2erun

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"time"
)

func runIsolated(ctx context.Context, staging string, command []string, output, events io.Writer) (int, error) {
	binary := filepath.Join(staging, "isolator")
	args := []string{"--unshare-all", "--ro-bind", filepath.Join(staging, "system"), "/", "--die-with-parent", "--new-session", "--clearenv", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run", "--dir", "/opt", "--dir", "/etc", "--setenv", "HOME", "/tmp", "--setenv", "PATH", "/opt:/usr/bin:/bin", "--setenv", "LANG", "C.UTF-8", "--setenv", "NODE_PATH", "/opt/modules", "--setenv", "PLAYWRIGHT_BROWSERS_PATH", "/opt/browsers", "--setenv", "CI", "1"}
	for _, mount := range []struct{ name, target string }{{"work", "/work"}, {"adapter", "/adapter"}, {"node", "/opt/node"}, {"modules", "/opt/modules"}, {"browsers", "/opt/browsers"}} {
		args = append(args, "--ro-bind", filepath.Join(staging, mount.name), mount.target)
	}
	args = append(args, "--chdir", "/work", "--")
	args = append(args, command...)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = []string{"LANG=C.UTF-8", "TZ=UTC"}
	cmd.Stdout, cmd.Stderr = events, output
	cmd.WaitDelay = time.Second
	runErr := cmd.Run()
	if cmd.ProcessState == nil {
		return -1, runErr
	}
	if ctx.Err() != nil {
		return cmd.ProcessState.ExitCode(), ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		return exit.ExitCode(), nil
	}
	return cmd.ProcessState.ExitCode(), runErr
}
