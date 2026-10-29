//go:build !linux

package workspace

import (
	"context"
	"runtime"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func observeNativeCheckPermissions(_ context.Context, root, common, checkout, _ string, _ NativeOwnedCheckRequest) checkexec.PermissionObservation {
	return checkexec.PermissionObservation{
		Version:    "1.0.0",
		Runner:     "worker_bubblewrap_seccomp_v1",
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		MainRoot:   root,
		CommonDir:  common,
		WorkerRoot: checkout,
		CWD:        checkout,
		Available:  false,
		Diagnostic: "original native isolated Worker runner is unavailable on this platform",
	}
}
