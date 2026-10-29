package targetedauthoring

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/processtree"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// CheckNormalOriginal checks a precisely issued ordinary path without changing
// user Git rules or hiding any directory. It is valid before first publication
// and while a later author edit remains the original untracked regular file.
func CheckNormalOriginal(ctx context.Context, root, rel string) error {
	return checkNormalOriginal(ctx, root, rel, true)
}

func checkNormalOriginal(ctx context.Context, root, rel string, retryPublished bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := workpacket.CheckPath(root, rel); err != nil {
		return err
	}
	exists := false
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("ordinary original %s is not a regular file", rel)
		}
		exists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	run := func(args ...string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
		command.WaitDelay = time.Second
		var output bytes.Buffer
		command.Stdout = &output
		err := processtree.Run(command)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return output.Bytes(), err
	}
	for _, args := range [][]string{{"ls-files", "--stage", "-z", "--", rel}, {"ls-tree", "-z", "HEAD", "--", rel}} {
		data, err := run(args...)
		if err != nil {
			return fmt.Errorf("check original Git source %s: %w", rel, err)
		}
		if len(data) != 0 {
			return fmt.Errorf("ordinary original %s conflicts with HEAD or index product", rel)
		}
	}
	if _, err := run("check-ignore", "-q", "--no-index", "--", rel); err == nil {
		return fmt.Errorf("ordinary original %s is ignored by Git; restore its normal source capability without changing user rules", rel)
	} else {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return fmt.Errorf("check original Git ignore %s: %w", rel, err)
		}
	}
	data, err := run("status", "--porcelain=v1", "-z", "--untracked-files=all", "--", rel)
	if err != nil {
		return err
	}
	if exists && string(data) != "?? "+rel+"\x00" {
		return fmt.Errorf("ordinary original %s must remain exactly untracked", rel)
	}
	if !exists && len(data) != 0 {
		// Another publisher may have created this exact original after Lstat.
		// Recheck every source boundary once rather than treating the earlier
		// absence as proof that a now-untracked original conflicts.
		if retryPublished && string(data) == "?? "+rel+"\x00" {
			return checkNormalOriginal(ctx, root, rel, false)
		}
		return fmt.Errorf("ordinary original %s has conflicting Git status", rel)
	}
	return nil
}
