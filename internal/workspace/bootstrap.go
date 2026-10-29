package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const bootstrapGitBlobLimit = 256 << 20

type BootstrapReceipt struct {
	Version      int               `json:"version"`
	RuntimeID    string            `json:"runtime_id"`
	AssignmentID string            `json:"assignment_id"`
	Generation   int               `json:"execution_generation"`
	Files        map[string]string `json:"files"`
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
	r := BootstrapReceipt{Version: 1, RuntimeID: e.RuntimeID, AssignmentID: e.AssignmentID, Generation: e.Generation, Files: map[string]string{}}
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
		h := sha256.Sum256(data)
		r.Files[rel] = hex.EncodeToString(h[:])
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
	if submissionPaths.Plan == PlanDraftPath {
		// These five local drafts are control inputs, not product changes.
		// Ignore only the frozen names so normal non-forced Git cleanup can
		// retire an accepted execution; unexpected files remain visible.
		lines := []string{"/.gitignore"}
		for _, draft := range append(submissionPaths.ControlPaths(), submissionPaths.Answer) {
			lines = append(lines, "/"+filepath.Base(draft))
		}
		if err := install(".loop-submissions/.gitignore", []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
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
				if err != nil || workpacket.Digest(data) != roleOverride.SHA256 {
					return fmt.Errorf("fixed generated role cannot be installed")
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
			published, err := workpacket.PublishBounded(e.Path, c.ResultPath, append(data, '\n'), 2<<20)
			if err != nil {
				return err
			}
			if err := agentcontext.ValidateResultDraft(c.ResultDraft, published); err != nil {
				return fmt.Errorf("editable Worker Result does not match its frozen Record tuple: %w", err)
			}
		}
		if canonicalS6BuilderCommitDraft(e, c) {
			if e.BaseCommit == "" {
				return fmt.Errorf("canonical S6 commit request requires the registered Worker starting HEAD")
			}
			data, err = json.MarshalIndent(CommitRequest{ExpectedHead: e.BaseCommit, Message: "", Paths: []string{}}, "", "  ")
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
			if e.Inputs[ref.Path] != ref.SHA256 {
				return fmt.Errorf("document review bootstrap lacks frozen subject %s", ref.Path)
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

// inheritedBootstrapAssetIsUnchanged permits the first preparing bootstrap to
// replace only an unchanged regular file inherited from its exact base commit.
// The published receipt closes this exception even when BootstrapSHA256 has
// not yet been copied back into the Runtime projection.
func inheritedBootstrapAssetIsUnchanged(e Execution, rel string, existing []byte, info fs.FileInfo) (bool, error) {
	if e.Status != "preparing" || e.BootstrapSHA256 != "" {
		return false, nil
	}
	if _, err := os.Lstat(filepath.Join(e.Path, ".claude/loop-bootstrap.json")); err == nil || !os.IsNotExist(err) {
		return false, nil
	}
	baseBytes, baseMode, found, err := readBootstrapBaseBlob(e, rel)
	if err != nil || !found {
		return false, err
	}
	if !bytes.Equal(existing, baseBytes) {
		return false, nil
	}
	if runtime.GOOS != "windows" {
		baseExecutable := baseMode == "100755"
		worktreeExecutable := info.Mode().Perm()&0111 != 0
		if baseExecutable != worktreeExecutable {
			return false, nil
		}
	}

	clean, err := bootstrapGitPathClean(e.Path, "diff", "--cached", "--quiet", "--no-ext-diff", "--no-textconv", "--no-renames", "--ignore-submodules=all", e.BaseCommit, "--", rel)
	if err != nil || !clean {
		return false, err
	}
	clean, err = bootstrapGitPathClean(e.Path, "diff", "--quiet", "--no-ext-diff", "--no-textconv", "--no-renames", "--ignore-submodules=all", "--", rel)
	if err != nil || !clean {
		return false, err
	}
	return true, nil
}

func readBootstrapBaseBlob(e Execution, rel string) ([]byte, string, bool, error) {
	if !filepath.IsLocal(rel) || rel == "." || strings.ContainsAny(rel, "\\\x00") || filepath.ToSlash(rel) != rel {
		return nil, "", false, fmt.Errorf("invalid bootstrap Git path")
	}
	objectID, err := hex.DecodeString(e.BaseCommit)
	if err != nil || len(objectID) != 20 && len(objectID) != 32 {
		return nil, "", false, fmt.Errorf("invalid bootstrap base commit")
	}

	tree, err := bootstrapGitOutput(e.Path, 4096, "ls-tree", "-z", "--full-tree", e.BaseCommit, "--", rel)
	if err != nil {
		return nil, "", false, err
	}
	if len(tree) == 0 {
		return nil, "", false, nil
	}
	if tree[len(tree)-1] != 0 {
		return nil, "", false, fmt.Errorf("Git tree entry is not NUL terminated")
	}
	entries := bytes.Split(tree[:len(tree)-1], []byte{0})
	if len(entries) != 1 {
		return nil, "", false, fmt.Errorf("Git tree entry is ambiguous")
	}
	metadata, path, ok := bytes.Cut(entries[0], []byte{'\t'})
	if !ok || string(path) != rel {
		return nil, "", false, fmt.Errorf("Git tree path differs from requested bootstrap path")
	}
	fields := strings.Fields(string(metadata))
	if len(fields) != 3 || fields[1] != "blob" || fields[0] != "100644" && fields[0] != "100755" {
		return nil, "", false, nil
	}
	blobID, err := hex.DecodeString(fields[2])
	if err != nil || len(blobID) != 20 && len(blobID) != 32 {
		return nil, "", false, fmt.Errorf("invalid Git blob identity")
	}
	data, err := bootstrapGitOutput(e.Path, bootstrapGitBlobLimit, "cat-file", "blob", fields[2])
	if err != nil {
		return nil, "", false, err
	}
	return data, fields[0], true, nil
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
	if e.BootstrapSHA256 != "" {
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != e.BootstrapSHA256 {
			return fmt.Errorf("Worker bootstrap receipt differs from Main's recorded digest")
		}
	}
	var r BootstrapReceipt
	if err = json.Unmarshal(data, &r); err != nil {
		return err
	}
	if r.Version != 1 || r.RuntimeID != e.RuntimeID || r.AssignmentID != e.AssignmentID || r.Generation != e.Generation || len(r.Files) == 0 {
		return fmt.Errorf("Worker bootstrap receipt identity mismatch")
	}
	for rel, expected := range r.Files {
		path := filepath.Join(e.Path, filepath.FromSlash(rel))
		physical, err := Canonical(path)
		back, relErr := filepath.Rel(e.Path, path)
		if err != nil || relErr != nil || !filepath.IsLocal(back) || physical != path {
			return fmt.Errorf("invalid bootstrap asset path %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h := sha256.Sum256(data)
		if hex.EncodeToString(h[:]) != expected {
			return fmt.Errorf("Worker bootstrap asset changed: %s", rel)
		}
	}
	return nil
}

func BootstrapDigest(e Execution) (string, error) {
	data, err := os.ReadFile(filepath.Join(e.Path, ".claude/loop-bootstrap.json"))
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}
