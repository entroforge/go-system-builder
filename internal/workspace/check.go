package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CheckInvocation has no arbitrary shell arguments. The selected command is
// taken from Main's frozen assignment, not from the caller's payload.
func CheckInvocation(e Execution, index int) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
	return quote(filepath.Join(e.Path, ".claude/bin/loop-harness")) + " runtime workspace check --root " + quote(e.Path) + " --assignment " + quote(e.AssignmentID) + " --agent " + quote(e.AgentID) + " --check " + strconv.Itoa(index)
}

// RunCheck tests a disposable copy. The source Worker, Main and Git metadata
// are read-only in the subprocess; generated test files never enter delivery.
func (b *ExecutionRegistry) RunCheck(ctx context.Context, e Execution, index int, output io.Writer) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if index < 0 || index >= len(e.Checks) || strings.HasPrefix(strings.TrimSpace(e.Checks[index]), "locked:") {
		return "", fmt.Errorf("select a declared executable check index")
	}
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return "", err
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return "", err
	}
	if err := b.ValidateInputs(e); err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "loop-worker-check-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	hash, err := copyCheckTree(ctx, e.Path, scratch)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	dir, err = os.MkdirTemp(dir, e.AssignmentID+"-")
	if err != nil {
		return "", err
	}
	log, err := os.OpenFile(filepath.Join(dir, "output.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	started := time.Now().UTC()
	err = runIsolatedCheck(ctx, b.MainRoot, b.CommonDir, e.Path, scratch, e.Checks[index], io.MultiWriter(output, log))
	closeErr := log.Close()
	if err == nil {
		err = closeErr
	}
	status := "pass"
	reason := ""
	if err != nil {
		status = "fail"
		reason = err.Error()
	}
	receipt := map[string]any{"version": 1, "runtime_id": e.RuntimeID, "assignment_id": e.AssignmentID, "execution_generation": e.Generation, "baseline_generation": e.BaselineGeneration, "check_index": index, "command": e.Checks[index], "snapshot_sha256": hash, "started_at": started, "elapsed_ms": time.Since(started).Milliseconds(), "status": status, "error": reason, "output": filepath.Join(dir, "output.log"), "disposable_copy": true}
	data, _ := json.MarshalIndent(receipt, "", "  ")
	path := filepath.Join(dir, "receipt.json")
	if writeErr := publishBootstrapFile(path, data, 0600); writeErr != nil {
		return "", writeErr
	}
	return path, err
}

func copyCheckTree(ctx context.Context, source, dest string) (string, error) {
	hash := sha256.New()
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
		if rel == ".claude" || rel == ".worktrees" {
			return filepath.SkipDir
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		fmt.Fprintf(hash, "%q %d %d\n", filepath.ToSlash(rel), info.Mode(), info.Size())
		if d.IsDir() {
			return os.Mkdir(target, info.Mode().Perm()|0700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "%q\n", link)
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("check snapshot refuses special file %s", rel)
		}
		total += info.Size()
		if total > 512<<20 {
			return fmt.Errorf("check snapshot exceeds 512 MiB; use Main's project check runner for larger trees")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash.Write(data)
		return os.WriteFile(target, data, info.Mode().Perm()|0600)
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}
