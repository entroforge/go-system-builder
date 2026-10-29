package workspace

import (
	"context"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/checkexec"
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
	location := e.CheckLocation
	if location == "" {
		location = "worker"
	}
	if err := checkexec.ValidateProfiles(e.CheckProfiles, e.Checks, location); err != nil {
		return err
	}
	for _, p := range e.CheckProfiles {
		if _, err := p.ActualEnvironment(); err != nil {
			return checkexec.New("capability_missing", "environment owner", p.Command, err)
		}
		for _, ref := range append(append([]checkexec.FileRef{}, p.Dependencies...), p.Fixtures...) {
			data, err := checkexec.ReadProfileFile(source, ref.Path)
			if err != nil || checkexec.Hash(data) != ref.SHA256 {
				return checkexec.New("capability_missing", "environment owner", p.Command, fmt.Errorf("profile dependency/fixture is unavailable or changed: %s", ref.Path))
			}
		}
	}
	if e.CheckLocation == "main" {
		if err := checkexec.MainCapabilities(ctx, source, e.Checks).Error(); err != nil {
			return err
		}
		return b.PreflightOwnedChecks(ctx, e, source)
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
	return b.PreflightOwnedChecks(ctx, e, source)
}

// PreflightOwnedChecks observes only the separate Record-frozen WorkerChecks.
// It never moves a Main RequiredCheck into the Worker collection.
func (b *ExecutionRegistry) PreflightOwnedChecks(ctx context.Context, e Execution, source string) error {
	if len(e.WorkerChecks) == 0 {
		return nil
	}
	commands := make([]string, len(e.WorkerChecks))
	profiles := make([]checkexec.Profile, len(e.WorkerChecks))
	for index, check := range e.WorkerChecks {
		if check.Index != index || check.Profile.CheckIndex != index || check.Command != check.Profile.Command || check.Profile.Location != "worker" {
			return fmt.Errorf("frozen WorkerCheck %d is malformed", index)
		}
		commands[index], profiles[index] = check.Command, check.Profile
	}
	if err := checkexec.ValidateProfiles(profiles, commands, "worker"); err != nil {
		return err
	}
	for _, profile := range profiles {
		if _, err := profile.ActualEnvironment(); err != nil {
			return checkexec.New("capability_missing", "environment owner", profile.Command, err)
		}
		for _, ref := range append(append([]checkexec.FileRef{}, profile.Dependencies...), profile.Fixtures...) {
			data, err := checkexec.ReadProfileFile(source, ref.Path)
			if err != nil || checkexec.Hash(data) != ref.SHA256 {
				return checkexec.New("capability_missing", "environment owner", profile.Command, fmt.Errorf("WorkerCheck profile dependency/fixture is unavailable or changed: %s", ref.Path))
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	scratch, err := os.MkdirTemp("", "loop-owned-check-preflight-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	if err := runIsolatedCheck(ctx, b.MainRoot, b.CommonDir, source, scratch, "true", io.Discard); err != nil {
		return fmt.Errorf("Record-frozen WorkerCheck capability unavailable: %w; Worker checks require Bubblewrap and seccomp, with no unsandboxed fallback", err)
	}
	return nil
}
