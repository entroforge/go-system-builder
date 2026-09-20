package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

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
func (b *Binding) Bootstrap(e Execution, executable string) error {
	r := BootstrapReceipt{Version: 1, RuntimeID: e.RuntimeID, AssignmentID: e.AssignmentID, Generation: e.Generation, Files: map[string]string{}}
	install := func(rel string, data []byte, mode os.FileMode) error {
		dest := filepath.Join(e.Path, filepath.FromSlash(rel))
		physical, err := CanonicalProspective(dest)
		if err != nil || physical != dest {
			return fmt.Errorf("bootstrap destination contains symlinks: %s", rel)
		}
		h := sha256.Sum256(data)
		r.Files[rel] = hex.EncodeToString(h[:])
		if old, err := os.ReadFile(dest); err == nil {
			oldHash := sha256.Sum256(old)
			if oldHash != h {
				return fmt.Errorf("preserve changed Worker bootstrap asset %s; restore the approved asset before retry", rel)
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		return publishBootstrapFile(dest, data, mode)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	name := "loop-harness"
	if runtime.GOOS == "windows" {
		name += ".exe"
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
	data, err = json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return publishBootstrapFile(filepath.Join(e.Path, ".claude/loop-bootstrap.json"), data, 0600)
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

func (b *Binding) ValidateBootstrap(e Execution) error {
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
