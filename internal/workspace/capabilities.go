package workspace

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PreflightChecks is side-effect free for Runtime/Git. It tests the actual OS
// isolation path before a Worker/session is published. Main mode is an explicit
// choice of runner, never an unsandboxed fallback inside the Worker.
func (b *ExecutionRegistry) PreflightChecks(ctx context.Context, e Execution, source string) error {
	if e.CheckLocation == "main" {
		return nil
	}
	if e.CheckLocation != "" && e.CheckLocation != "worker" {
		return fmt.Errorf("unknown check location")
	}
	required := false
	for _, cmd := range e.Checks {
		if !strings.HasPrefix(strings.TrimSpace(cmd), "locked:") {
			required = true
		}
	}
	if !required {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var total int64
	err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == ".claude" || rel == ".worktrees" || (rel == ".git" && d.IsDir()) {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Type()&os.ModeSymlink == 0 && !d.Type().IsRegular() {
			return fmt.Errorf("check snapshot refuses special file %s", rel)
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
			if total > 512<<20 {
				return fmt.Errorf("check snapshot exceeds 512 MiB")
			}
		}
		return nil
	})
	if err == nil {
		scratch, e2 := os.MkdirTemp("", "loop-check-preflight-")
		if e2 != nil {
			return e2
		}
		defer os.RemoveAll(scratch)
		err = runIsolatedCheck(ctx, b.MainRoot, b.CommonDir, source, scratch, "true", io.Discard)
	}
	if err != nil {
		return fmt.Errorf("Worker check capability unavailable: %w; install/enable Linux Bubblewrap and user namespaces, or prepare a new execution with --check-location main; Main integration must still run all required checks. Network-dependent checks require Main or preinstalled offline dependencies", err)
	}
	return nil
}
