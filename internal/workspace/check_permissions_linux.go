package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func (b *ExecutionRegistry) observeCheckPermissions(ctx context.Context, e Execution, scratch string) checkexec.PermissionObservation {
	out := checkexec.PermissionObservation{Version: "1.0.0", Runner: "worker_bubblewrap_seccomp_v1", Platform: runtime.GOOS + "/" + runtime.GOARCH, UID: os.Geteuid(), GID: os.Getegid(), MainRoot: b.MainRoot, CommonDir: b.CommonDir, WorkerRoot: e.Path, CWD: e.Path}
	fail := func(err error) checkexec.PermissionObservation {
		out.Available = false
		out.Diagnostic = err.Error()
		return out
	}
	var err error
	out.Groups, err = os.Getgroups()
	if err != nil {
		return fail(err)
	}
	sort.Ints(out.Groups)
	kernel, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return fail(err)
	}
	out.Kernel = strings.TrimSpace(string(kernel))
	if _, err := exec.LookPath("bwrap"); err != nil {
		return fail(err)
	}
	if _, err := checkSocketProgram(); err != nil {
		return fail(err)
	}
	p, ok := checkexec.CurrentProfile(ctx)
	if !ok || p.EnvironmentPolicy != "declared_only" {
		return fail(fmt.Errorf("inherited environment does not close isolated runner inputs"))
	}
	if _, err := p.CommandEnvironment(); err != nil {
		return fail(err)
	}
	var diagnostic bytes.Buffer
	if err := runIsolatedCheck(ctx, b.MainRoot, b.CommonDir, e.Path, scratch, "true", &diagnostic); err != nil {
		message := strings.TrimSpace(diagnostic.String())
		if len(message) > 2048 {
			message = message[:2048]
		}
		return fail(fmt.Errorf("original isolation probe: %w: %s", err, message))
	}
	out.Available = true
	return out
}
