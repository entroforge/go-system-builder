package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/checkexec"
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
	if e.CheckLocation == "main" {
		return "", fmt.Errorf("this execution selected Main checks; report committed work and use Main integration, never run an unsandboxed Worker check")
	}
	if p, ok := ProfileAt(e, index); ok {
		ctx = checkexec.WithProfile(ctx, p)
		ctx = checkexec.WithBinding(ctx, checkexec.Binding{RuntimeID: e.RuntimeID, Generation: e.BaselineGeneration, AssignmentID: e.AssignmentID, Owner: e.AgentID, ExecutionGeneration: e.Generation, Responsibility: "owned_check"})
	}
	ctx, cancel := checkexec.CheckTimeout(ctx)
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
	if _, profiled := ProfileAt(e, index); profiled && e.BootstrapSHA256 != "" {
		if err := b.ValidateBootstrap(e); err != nil {
			return "", err
		}
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
	var observation *checkexec.Observation
	var reuseDecision *checkexec.ReuseDecision
	if p, ok := ProfileAt(e, index); ok {
		observed, err := checkexec.ObserveProfile(ctx, scratch, p)
		if err != nil {
			return "", err
		}
		permissions := b.observeCheckPermissions(ctx, e, scratch)
		observed.Permissions = &permissions
		observed.PermissionDigest = permissions.Digest()
		observation = &observed
		guard := checkexec.CurrentReuseGuard(ctx)
		decision := checkexec.DecideReuse(filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks"), checkexec.ReuseRequest{Profile: p, Observation: observed, Binding: CheckBinding(e), Checkout: e.Path, Fresh: guard.Fresh, NewHypothesis: guard.NewHypothesis}, e.ToolCheckReceipts)
		reuseDecision = &decision
		if decision.Action == "reuse" {
			if err := b.ValidateInputs(e); err != nil {
				return "", err
			}
			fmt.Fprintln(output, "Worker check reused original accepted receipt:", decision.ReceiptRef)
			return decision.ReceiptRef, nil
		}
	}
	dir := filepath.Join(b.MainRoot, ".claude/evidence/workspace-checks")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", checkexec.New("diagnostic_io", "environment owner", e.Checks[index], err)
	}
	dir, err = os.MkdirTemp(dir, e.AssignmentID+"-")
	if err != nil {
		return "", checkexec.New("diagnostic_io", "environment owner", e.Checks[index], err)
	}
	log, err := os.OpenFile(filepath.Join(dir, "output.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", checkexec.New("diagnostic_io", "environment owner", e.Checks[index], err)
	}
	started := time.Now().UTC()
	err = runIsolatedCheck(ctx, b.MainRoot, b.CommonDir, e.Path, scratch, e.Checks[index], io.MultiWriter(output, log))
	closeErr := log.Close()
	if p, ok := ProfileAt(e, index); ok && err == nil {
		after, afterErr := checkexec.ObserveProfile(ctx, scratch, p)
		permissions := b.observeCheckPermissions(ctx, e, scratch)
		after.PermissionDigest = permissions.Digest()
		if afterErr != nil || after.SubjectDigest != observation.SubjectDigest || after.EnvironmentDigest != observation.EnvironmentDigest || after.RunnerDigest != observation.RunnerDigest || after.PermissionDigest != observation.PermissionDigest {
			err = checkexec.New("input_drift", "environment owner", e.Checks[index], fmt.Errorf("profile inputs/environment/runner changed during check"))
		}
	}
	if err == nil {
		err = closeErr
	}
	status := "pass"
	reason := ""
	if err != nil {
		status = "fail"
		reason = err.Error()
	}
	failure := checkexec.Classify(ctx, "environment owner", e.Checks[index], err)
	if closeErr != nil {
		failure = checkexec.New("diagnostic_io", "environment owner", e.Checks[index], closeErr)
	}
	path := filepath.Join(dir, "receipt.json")
	if failure != nil {
		failure.ReceiptRef = path
		failure.LogRef = filepath.Join(dir, "output.log")
	}
	binding, _ := checkexec.CurrentBinding(ctx)
	receipt := map[string]any{"reuse_decision": reuseDecision, "check_binding": binding, "profile_observation": observation, "failure": failure, "version": 1, "runtime_id": e.RuntimeID, "assignment_id": e.AssignmentID, "execution_generation": e.Generation, "baseline_generation": e.BaselineGeneration, "check_index": index, "command": e.Checks[index], "snapshot_sha256": hash, "started_at": started, "elapsed_ms": time.Since(started).Milliseconds(), "status": status, "error": reason, "output": filepath.Join(dir, "output.log"), "disposable_copy": true}
	data, _ := json.MarshalIndent(receipt, "", "  ")
	if observation != nil {
		receipt["version"] = 2
		sealed, sealErr := checkexec.SealReceipt(receipt, filepath.Join(dir, "output.log"))
		if sealErr != nil {
			return "", checkexec.New("diagnostic_io", "environment owner", e.Checks[index], sealErr)
		}
		data = sealed
	}
	if writeErr := publishBootstrapFile(path, data, 0600); writeErr != nil {
		return "", checkexec.New("diagnostic_io", "environment owner", e.Checks[index], writeErr)
	}
	if captureErr := checkexec.CaptureReceipt(ctx, path, e.Path, data); captureErr != nil {
		return path, checkexec.New("diagnostic_io", "environment owner", e.Checks[index], captureErr)
	}
	if failure != nil {
		failure.ReceiptRef = path
		failure.LogRef = filepath.Join(dir, "output.log")
		return path, failure
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
