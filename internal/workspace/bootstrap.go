package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type BootstrapReceipt struct {
	Version      int                        `json:"version"`
	RuntimeID    string                     `json:"runtime_id"`
	AssignmentID string                     `json:"assignment_id"`
	Generation   int                        `json:"execution_generation"`
	Files        map[string]json.RawMessage `json:"files"`
}

func decodeOriginalBootstrapReceipt(data []byte, receipt *BootstrapReceipt) error {
	return json.Unmarshal(data, receipt)
}

// Bootstrap installs executable and declarative assets only. It never copies
// settings permissions, credentials, Runtime, journal, or evidence. A receipt
// is published last; mismatching existing files require explicit recovery.
func (b *ExecutionRegistry) Bootstrap(e Execution, executable string) error {
	var roleOverride *GeneratedRole
	submissionPaths := legacyWorkerSubmissionPaths()
	if HasDispatchContext(e) {
		context, err := b.ReadDispatchContext(e)
		if err != nil {
			return err
		}
		roleOverride = context.GeneratedRole
		submissionPaths, err = context.WorkerSubmissionPaths()
		if err != nil {
			return err
		}
	}
	r := BootstrapReceipt{Version: 1, RuntimeID: e.RuntimeID, AssignmentID: e.AssignmentID, Generation: e.Generation, Files: map[string]json.RawMessage{}}
	name := "loop-harness"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	inheritedTargets := map[string]bool{
		".claude/bin/" + name:   true,
		".claude/settings.json": true,
	}
	if roleOverride != nil {
		inheritedTargets[filepath.ToSlash(filepath.Clean(roleOverride.Target))] = true
	}
	install := func(rel string, data []byte, mode os.FileMode) error {
		dest := filepath.Join(e.Path, filepath.FromSlash(rel))
		physical, err := CanonicalProspective(dest)
		if err != nil || physical != dest {
			return fmt.Errorf("bootstrap destination contains symlinks: %s", rel)
		}
		// This is a record of installed paths, not a byte identity or cleanup
		// authorization. Existing historical digest values remain readable.
		r.Files[rel] = json.RawMessage("null")
		if info, statErr := os.Lstat(dest); statErr == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("preserve changed Worker bootstrap asset %s; existing destination is not a regular file", rel)
			}
			old, err := os.ReadFile(dest)
			if err != nil {
				return err
			}
			if bytes.Equal(old, data) {
				return nil
			}
			if inheritedTargets[filepath.ToSlash(rel)] {
				replace, proofErr := inheritedBootstrapAssetIsUnchanged(e, rel, old, info)
				if proofErr != nil {
					return fmt.Errorf("preserve changed Worker bootstrap asset %s; restore the approved asset before retry: cannot prove inherited Git bytes: %w", rel, proofErr)
				}
				if replace {
					return publishBootstrapFile(dest, data, mode)
				}
			}
			return fmt.Errorf("preserve changed Worker bootstrap asset %s; restore the approved asset before retry", rel)
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		return publishBootstrapFile(dest, data, mode)
	}
	if strings.HasPrefix(submissionPaths.Answer, ".loop-submissions") {
		// These five local drafts are control inputs, not product changes.
		// Ignore only the frozen names so normal non-forced Git cleanup can
		// retire an accepted execution; unexpected files remain visible.
		lines := []string{"/.gitignore"}
		for _, draft := range append(submissionPaths.ControlPaths(), submissionPaths.Answer) {
			lines = append(lines, "/"+filepath.Base(draft))
		}
		if err := install(filepath.ToSlash(filepath.Join(filepath.Dir(submissionPaths.Answer), ".gitignore")), []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	if err = install(".claude/bin/"+name, data, 0700); err != nil {
		return err
	}
	// Claude's command Hooks use its project directory; the native executable
	// performs authenticated control-root resolution without changing cwd.
	hooks := map[string]any{}
	for _, event := range []string{"PreToolUse", "SessionStart", "SubagentStart", "SubagentStop", "TeammateIdle", "Stop", "PostToolUse", "PostToolUseFailure", "ConfigChange", "PreCompact"} {
		command := `"$CLAUDE_PROJECT_DIR"/.claude/bin/` + name + ` hook --event ` + event + ` --root "$CLAUDE_PROJECT_DIR"`
		hooks[event] = []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 10}}}}
	}
	data, err = json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
	if err != nil {
		return err
	}
	if err = install(".claude/settings.json", data, 0600); err != nil {
		return err
	}
	for _, dir := range []string{"agents", "skills"} {
		source := filepath.Join(b.MainRoot, ".claude", dir)
		if _, err := os.Stat(source); err != nil {
			return fmt.Errorf("Worker bootstrap needs installed %s assets: %w", dir, err)
		}
		err := filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("bootstrap asset is not a regular file: %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			target := filepath.ToSlash(filepath.Join(".claude", dir, rel))
			if roleOverride != nil && target == roleOverride.Target {
				data, err = workpacket.ReadFile(b.MainRoot, roleOverride.Source, 1<<20)
				if err != nil {
					return fmt.Errorf("generated role cannot be installed: %w", err)
				}
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			return install(filepath.ToSlash(filepath.Join(".claude", dir, rel)), data, info.Mode().Perm()&0700)
		})
		if err != nil {
			return err
		}
	}
	// Frozen input documents are read-only execution context, not a second
	// Runtime. Keep their relative names under a distinct input directory.
	if err := b.ValidateInputs(e); err != nil {
		return err
	}
	for rel := range e.Inputs {
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("invalid frozen input path: %s", rel)
		}
		source := filepath.Join(b.MainRoot, filepath.FromSlash(rel))
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return fmt.Errorf("frozen input must be a regular document no larger than 8 MiB: %s", rel)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err = install(filepath.ToSlash(filepath.Join(".claude/inputs", rel)), data, 0400); err != nil {
			return err
		}
	}
	if HasDispatchContext(e) {
		c, err := b.ReadDispatchContext(e)
		if err != nil {
			return err
		}
		data, err := workpacket.ReadFile(b.MainRoot, DispatchContextSource(e), 2<<20)
		if err != nil {
			return err
		}
		if err := install(DispatchContextPath, data, 0400); err != nil {
			return err
		}
		data, err = schema.ReadAsset("agent-message.schema.json")
		if err != nil {
			return err
		}
		if err := install(PlanSchemaPath, data, 0400); err != nil {
			return err
		}
		if c.PlanDraft != nil {
			data, err = json.MarshalIndent(c.PlanDraft, "", "  ")
			if err != nil {
				return err
			}
			if _, err := workpacket.Publish(e.Path, submissionPaths.Plan, append(data, '\n')); err != nil {
				return err
			}
		}
		if c.ResultDraft != nil {
			data, err = json.MarshalIndent(c.ResultDraft, "", "  ")
			if err != nil {
				return err
			}
			// The submission is an editable owner artifact, not a bootstrap
			// asset. Exclusive publication preserves an author's answer on retry.
			if _, err := workpacket.PublishBounded(e.Path, c.ResultPath, append(data, '\n'), 2<<20); err != nil {
				return err
			}
		}
		if canonicalS6BuilderCommitDraft(e, c) {
			data, err = json.MarshalIndent(CommitRequest{Message: "", Paths: []string{}}, "", "  ")
			if err != nil {
				return err
			}
			if _, err := workpacket.Publish(e.Path, submissionPaths.Commit, append(data, '\n')); err != nil {
				return fmt.Errorf("publish canonical S6 commit request draft: %w", err)
			}
		}
	}
	if e.Purpose == DocumentReviewPurpose {
		snapshot, err := loopruntime.NewStore(filepath.Join(b.MainRoot, ".claude/loop-state.json"), filepath.Join(b.MainRoot, ".claude/loop-events.jsonl")).Snapshot()
		if err != nil {
			return err
		}
		p, err := DocumentReviewPacket(snapshot.State, e)
		if err != nil {
			return err
		}
		for _, ref := range p.Inputs {
			if _, present := e.Inputs[ref.Path]; !present {
				return fmt.Errorf("document review bootstrap lacks original subject %s", ref.Path)
			}
		}
		v, err := workpacket.BindExecution(p, e.Path, e.Generation, e.AgentID, submissionPaths.Answer)
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		if err := install(workpacket.WorkerViewPath, append(data, '\n'), 0400); err != nil {
			return err
		}
		draft, err := json.MarshalIndent(p.Draft, "", "  ")
		if err != nil {
			return err
		}
		if _, err := workpacket.Publish(e.Path, v.AuthoringPath, append(draft, '\n')); err != nil {
			return err
		}
	}
	data, err = json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return publishBootstrapFile(filepath.Join(e.Path, ".claude/loop-bootstrap.json"), data, 0600)
}

// inheritedBootstrapAssetIsUnchanged permits updating an installed control
// target only while preparing its original checkout, when Git reports that
// exact tracked path has no staged or worktree changes. No historical object
// ID or byte digest is consulted; unknown or modified files remain untouched.
func inheritedBootstrapAssetIsUnchanged(e Execution, rel string, _ []byte, _ fs.FileInfo) (bool, error) {
	if e.Status != "preparing" || !filepath.IsLocal(rel) || rel == "." || strings.ContainsAny(rel, "\\\x00") || filepath.ToSlash(rel) != rel {
		return false, nil
	}
	if _, err := os.Lstat(filepath.Join(e.Path, ".claude/loop-bootstrap.json")); err == nil || !os.IsNotExist(err) {
		return false, nil
	}
	tracked, err := bootstrapGitOutput(e.Path, 4096, "ls-files", "-z", "--", rel)
	if err != nil || string(tracked) != rel+"\x00" {
		return false, err
	}
	status, err := bootstrapGitOutput(e.Path, 4096, "status", "--porcelain=v1", "-z", "--", rel)
	if err != nil {
		return false, err
	}
	return len(status) == 0, nil
}

func bootstrapGitPathClean(root string, args ...string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := bootstrapGitCommand(ctx, root, args...)
	var stdout, stderr bootstrapGitBuffer
	stdout.limit, stderr.limit = 4096, 4096
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil {
		return true, nil
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
}

func bootstrapGitOutput(root string, limit int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := bootstrapGitCommand(ctx, root, args...)
	var stdout, stderr bootstrapGitBuffer
	stdout.limit, stderr.limit = limit, 4096
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

func bootstrapGitCommand(ctx context.Context, root string, args ...string) *exec.Cmd {
	fullArgs := append([]string{"-C", root, "--no-pager", "--literal-pathspecs", "-c", "core.fsmonitor=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_PAGER=cat")
	cmd.WaitDelay = time.Second
	return cmd
}

type bootstrapGitBuffer struct {
	bytes.Buffer
	limit int
}

func (b *bootstrapGitBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("Git output exceeds %d bytes", b.limit)
	}
	return b.Buffer.Write(p)
}

func publishBootstrapFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".bootstrap-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (b *ExecutionRegistry) ValidateBootstrap(e Execution) error {
	data, err := os.ReadFile(filepath.Join(e.Path, ".claude/loop-bootstrap.json"))
	if err != nil {
		return fmt.Errorf("Worker bootstrap receipt unavailable: %w", err)
	}

	var r BootstrapReceipt
	if err = decodeOriginalBootstrapReceipt(data, &r); err != nil {
		return err
	}
	if r.Version != 1 || r.RuntimeID != e.RuntimeID || r.AssignmentID != e.AssignmentID || r.Generation != e.Generation || len(r.Files) == 0 {
		return fmt.Errorf("Worker bootstrap receipt identity mismatch")
	}
	for rel := range r.Files {
		path := filepath.Join(e.Path, filepath.FromSlash(rel))
		physical, err := Canonical(path)
		back, relErr := filepath.Rel(e.Path, path)
		if err != nil || relErr != nil || !filepath.IsLocal(back) || physical != path {
			return fmt.Errorf("invalid bootstrap asset path %s", rel)
		}
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	return nil
}
