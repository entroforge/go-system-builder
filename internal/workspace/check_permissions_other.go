//go:build !linux

package workspace

import (
	"context"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"runtime"
)

func (b *ExecutionRegistry) observeCheckPermissions(ctx context.Context, e Execution, scratch string) checkexec.PermissionObservation {
	return checkexec.PermissionObservation{Version: "1.0.0", Runner: "worker_bubblewrap_seccomp_v1", Platform: runtime.GOOS + "/" + runtime.GOARCH, MainRoot: b.MainRoot, CommonDir: b.CommonDir, WorkerRoot: e.Path, CWD: e.Path, Available: false, Diagnostic: "original isolated Worker runner is unavailable on this platform"}
}
