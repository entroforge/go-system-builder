package e2erun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func relative(path string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(path))
	if filepath.IsAbs(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsAny(clean, "\x00\n\r") {
		return "", fmt.Errorf("unsafe relative input %q", path)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".git" || part == ".claude" || part == ".worktrees" || part == "node_modules" {
			return "", fmt.Errorf("control/tool path cannot be a source input: %s", clean)
		}
	}
	return clean, nil
}

// InspectInputs recomputes the exact declared input set without writing a
// workspace. Producers use it for CAS revalidation; additions/deletions under
// a declared directory change the digest just as edited bytes do.
func InspectInputs(ctx context.Context, files fileview.Reader, roots []string) ([]FileDigest, string, error) {
	return snapshot(ctx, files, roots, "")
}

// snapshot reads exactly the declared view, never the working tree as a
// fallback. All source files are subsequently mounted read-only to the runner.
func snapshot(ctx context.Context, files fileview.Reader, roots []string, dest string) ([]FileDigest, string, error) {
	seen := map[string]bool{}
	var inputs []FileDigest
	var total int64
	var copyPath func(string) error
	copyPath = func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := relative(path)
		if err != nil {
			return err
		}
		if seen[path] {
			return nil
		}
		seen[path] = true
		entries, dirErr := files.ReadDir(path)
		if dirErr == nil {
			for _, entry := range entries {
				if entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("source symlink is not an executable snapshot input: %s/%s", path, entry.Name())
				}
				if err := copyPath(path + "/" + entry.Name()); err != nil {
					return err
				}
			}
			return nil
		}
		data, err := files.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read declared input %s: %w", path, err)
		}
		total += int64(len(data))
		if total > 512<<20 {
			return fmt.Errorf("source snapshot exceeds 512 MiB")
		}
		if dest != "" {
			target := filepath.Join(dest, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			if err := os.WriteFile(target, data, 0500); err != nil {
				return err
			}
		}
		inputs = append(inputs, FileDigest{Path: path, SHA256: digest(data)})
		return nil
	}
	for _, root := range roots {
		if err := copyPath(root); err != nil {
			return nil, "", err
		}
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	data, _ := json.Marshal(inputs)
	return inputs, digest(data), nil
}

// CopyTool copies to private staging and hashes the copied bytes. Symlinks
// must resolve inside the declared tool root; no external dependency is read
// implicitly. Receipt hashes therefore describe the bytes actually mounted.
func copyTool(ctx context.Context, tool Tool, dest string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	source, err := filepath.EvalSymlinks(tool.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("tool is not regular: %s", source)
		}
		if info.Size() > 256<<20 {
			return "", fmt.Errorf("tool executable exceeds 256 MiB")
		}
		file, err := os.Open(source)
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(contextReader{ctx, file}, info.Size()+1))
		closeErr := file.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
		if int64(len(data)) != info.Size() {
			return "", fmt.Errorf("tool changed during snapshot: %s", source)
		}
		hash := digest(data)
		if tool.SHA256 != "" && hash != tool.SHA256 {
			return "", fmt.Errorf("tool digest drift: %s", tool.Path)
		}
		return hash, os.WriteFile(dest, data, 0500)
	}
	hash := sha256.New()
	var total int64
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// Directory inode sizes and original directory permissions are not
			// mounted semantics: private copies always use 0700. Hash the
			// normalized tree so identical copies have identical identities.
			fmt.Fprintf(hash, "%q directory\n", filepath.ToSlash(rel))
			return os.MkdirAll(target, 0700)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			inside, err := filepath.Rel(source, resolved)
			if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
				return fmt.Errorf("tool symlink escapes its declared root: %s", path)
			}
			if filepath.IsAbs(link) {
				link, err = filepath.Rel(filepath.Dir(path), resolved)
				if err != nil {
					return err
				}
			}
			fmt.Fprintf(hash, "%q link:%q\n", filepath.ToSlash(rel), link)
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("tool special file: %s", path)
		}
		fmt.Fprintf(hash, "%q file %d %d\n", filepath.ToSlash(rel), info.Mode().Perm(), info.Size())
		total += info.Size()
		if total > 1<<30 {
			return fmt.Errorf("tool snapshot exceeds 1 GiB")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(contextReader{ctx, in}, info.Size()+1))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if n != info.Size() {
			return fmt.Errorf("tool changed during snapshot: %s", path)
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	value := hex.EncodeToString(hash.Sum(nil))
	if tool.SHA256 != "" && value != tool.SHA256 {
		return "", fmt.Errorf("tool digest drift: %s", tool.Path)
	}
	return value, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
