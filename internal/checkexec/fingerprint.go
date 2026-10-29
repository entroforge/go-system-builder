package checkexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/schema"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Observation struct {
	Permissions       *PermissionObservation `json:"permissions,omitempty"`
	PermissionDigest  string                 `json:"permission_digest,omitempty"`
	ProfileID         string                 `json:"profile_id"`
	ProfileVersion    string                 `json:"profile_version"`
	ProfileDigest     string                 `json:"profile_digest"`
	SubjectDigest     string                 `json:"subject_digest"`
	RunnerDigest      string                 `json:"runner_digest"`
	EnvironmentDigest string                 `json:"environment_digest"`
	DependencyDigest  string                 `json:"dependency_digest"`
	FixtureDigest     string                 `json:"fixture_digest"`
	Environment       []EnvFact              `json:"environment"`
	VerificationScope string                 `json:"verification_scope"`
	ReuseEligible     bool                   `json:"reuse_eligible"`
}

func ObserveProfile(ctx context.Context, root string, p Profile) (Observation, error) {
	out := Observation{ProfileID: p.ID, ProfileVersion: p.Version, ProfileDigest: p.Digest(), VerificationScope: "whole checked tree, declared dependency/fixture digests, actual runner and approved environment facts; reuse not enabled"}
	facts, err := p.ActualEnvironment()
	if err != nil {
		return out, err
	}
	out.Environment = facts
	data, _ := json.Marshal(facts)
	out.EnvironmentDigest = Hash(data)
	out.SubjectDigest, err = TreeDigest(ctx, root)
	if err != nil {
		return out, err
	}
	collect := func(refs []FileRef) (string, error) {
		for _, ref := range refs {
			data, err := ReadProfileFile(root, ref.Path)
			if err != nil || Hash(data) != ref.SHA256 {
				return "", fmt.Errorf("profile dependency/fixture changed: %s", ref.Path)
			}
		}
		data, _ := json.Marshal(refs)
		return Hash(data), nil
	}
	out.DependencyDigest, err = collect(p.Dependencies)
	if err != nil {
		return out, err
	}
	out.FixtureDigest, err = collect(p.Fixtures)
	if err != nil {
		return out, err
	}
	runner := map[string]string{"platform": runtime.GOOS + "/" + runtime.GOARCH, "go_runtime": runtime.Version()}
	for _, name := range []string{"sh", entranceOrEmpty(p.Command)} {
		if name == "" {
			continue
		}
		path := name
		if strings.Contains(path, "/") && !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		resolved, err := exec.LookPath(path)
		if err != nil {
			return out, err
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return out, err
		}
		identity := resolved
		if filepath.IsAbs(resolved) {
			// A local executable is copied into a fresh scratch directory for
			// each invocation. Its identity is its checked-tree coordinate and
			// actual bytes; the random copy directory is not an input.
			if relative, relErr := filepath.Rel(root, resolved); relErr == nil && filepath.IsLocal(relative) {
				identity = "checked-tree/" + filepath.ToSlash(relative)
			}
		}
		runner[name] = identity + "#" + Hash(data)
	}
	data, _ = json.Marshal(runner)
	out.RunnerDigest = Hash(data)
	// Collection alone is not original domain acceptance/authenticated reuse.
	out.ReuseEligible = false
	data, _ = json.Marshal(out)
	if err := schema.NewEmbeddedValidator().ValidateBytes("check-observation.schema.json", data); err != nil {
		return out, err
	}
	return out, nil
}
func entranceOrEmpty(command string) string {
	tool, ok := LiteralEntrance(command)
	if !ok || builtins[tool] {
		return ""
	}
	return tool
}
func ReadProfileFile(root, path string) ([]byte, error) {
	if !safeProfilePath(path) {
		return nil, fmt.Errorf("unsafe profile input")
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	actual, err := filepath.EvalSymlinks(filepath.Join(base, path))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, actual)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("profile input escapes source root")
	}
	info, err := os.Stat(actual)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return nil, fmt.Errorf("profile input is not a bounded regular file")
	}
	return os.ReadFile(actual)
}
func TreeDigest(ctx context.Context, root string) (string, error) {
	h := sha256.New()
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" || rel == ".claude" || rel == ".worktrees" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%q %d %d\n", filepath.ToSlash(rel), info.Mode(), info.Size())
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%q\n", target)
			actual, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			base, err := filepath.EvalSymlinks(root)
			if err != nil {
				return err
			}
			outside, err := filepath.Rel(base, actual)
			if err != nil || outside == ".." || strings.HasPrefix(outside, ".."+string(filepath.Separator)) {
				return fmt.Errorf("profile tree symlink escapes root: %s", rel)
			}
			if st, err := os.Stat(actual); err != nil || !st.Mode().IsRegular() {
				return fmt.Errorf("profile tree symlink is not a file: %s", rel)
			}
			data, err := os.ReadFile(actual)
			if err != nil {
				return err
			}
			total += int64(len(data))
			h.Write(data)
		} else {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("profile tree contains special file: %s", rel)
			}
			total += info.Size()
			if total > 512<<20 {
				return fmt.Errorf("profile tree exceeds 512 MiB")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write(data)
		}
		if total > 512<<20 {
			return fmt.Errorf("profile tree exceeds 512 MiB")
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}
