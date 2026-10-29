package integration

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/processtree"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// guardIgnoredCleanup is an observation, not a lock against external writers.
// The workspace lock serializes cooperating framework operations only. A file
// created or replaced after the final inventory can still race Git's recursive
// worktree removal; callers must not describe this as atomic preservation.
func guardIgnoredCleanup(ctx context.Context, root string, known map[string]string) error {
	return guardIgnoredCleanupWithPrivateArea(ctx, root, known, nil)
}

func guardIgnoredCleanupWithPrivateArea(ctx context.Context, root string, known map[string]string, area *WorkerPrivateAreaProjection) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"ls-files", "--others", "--ignored", "--exclude-standard", "-z"}
	raw, err := defaultRunner.Run(ctx, root, args...)
	if err != nil {
		return fmt.Errorf("ignored cleanup inventory unavailable; preserve Worker: %w", err)
	}
	if len(raw) > cleanupInventoryLimit {
		return fmt.Errorf("ignored cleanup inventory exceeds %d bytes; preserve Worker", cleanupInventoryLimit)
	}
	readBytes, entries := 0, 0
	for _, rel := range strings.Split(raw, "\x00") {
		if rel == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		if entries > 10000 || readBytes > 256<<20 {
			return fmt.Errorf("ignored cleanup proof budget exceeded; preserve Worker")
		}
		if area != nil && strings.HasPrefix(rel, area.path+"/") {
			allowed, areaErr := area.Allows(rel)
			if areaErr != nil {
				return fmt.Errorf("inspect original Worker private area %q: %w", rel, areaErr)
			}
			if !allowed {
				return fmt.Errorf("preserve product or unrelated ignored Worker file %q", rel)
			}
		}
		want := known[rel]
		if want == "" {
			allowed, areaErr := area.Allows(rel)
			if areaErr != nil {
				return fmt.Errorf("inspect original Worker private area %q: %w", rel, areaErr)
			}
			if allowed {
				continue
			}
			return fmt.Errorf("preserve unknown ignored Worker file %q", rel)
		}
		// Rooted reading rejects symlinks at every component, nonregular files,
		// replacement while opening, and oversized/unreadable content.
		data, err := workpacket.ReadFile(root, rel, 64<<20)
		readBytes += len(data)
		if readBytes > 256<<20 {
			return fmt.Errorf("ignored cleanup proof exceeds 256 MiB; preserve Worker")
		}
		if err != nil || workpacket.Digest(data) != want {
			return fmt.Errorf("preserve changed or nonregular ignored Worker file %q (read: %v)", rel, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

const cleanupInventoryLimit = 1 << 20

type cleanupInventoryBuffer struct {
	buffer bytes.Buffer
	cancel context.CancelFunc
}

func (b *cleanupInventoryBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > cleanupInventoryLimit {
		b.cancel()
		return 0, fmt.Errorf("ignored inventory exceeds %d bytes", cleanupInventoryLimit)
	}
	return b.buffer.Write(p)
}

func boundedIgnoredInventory(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	output := &cleanupInventoryBuffer{cancel: cancel}
	cmd.Stdout = output
	if err := processtree.Run(cmd); err != nil {
		return "", fmt.Errorf("bounded ignored inventory failed (limit %d bytes): %w", cleanupInventoryLimit, err)
	}
	return output.buffer.String(), nil
}

func removeGuardedWorktree(ctx context.Context, root, path string, known map[string]string) error {
	if err := guardIgnoredCleanup(ctx, path, known); err != nil {
		return err
	}
	return removeWorktree(ctx, root, path)
}
