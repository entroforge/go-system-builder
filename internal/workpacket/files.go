package workpacket

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// OpenParent pins each directory separately. A symlink to another owner's
// files is invalid even if it remains inside the project. Root's containment
// and the inode checks also protect against directory replacement races.
func OpenParent(root, relative string, create bool) (*os.Root, string, error) {
	if !safeRelative(relative) {
		return nil, "", fmt.Errorf("invalid packet artifact path")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, "", err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts[:len(parts)-1] {
		before, err := r.Lstat(part)
		if errors.Is(err, os.ErrNotExist) && create {
			if err := r.Mkdir(part, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				r.Close()
				return nil, "", err
			}
			before, err = r.Lstat(part)
		}
		if err != nil {
			r.Close()
			return nil, "", err
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			r.Close()
			return nil, "", fmt.Errorf("packet path contains a symlink or non-directory")
		}
		next, err := r.OpenRoot(part)
		if err != nil {
			r.Close()
			return nil, "", err
		}
		after, err := next.Stat(".")
		if err != nil || !os.SameFile(before, after) {
			r.Close()
			next.Close()
			return nil, "", fmt.Errorf("packet path changed while opening")
		}
		if create {
			if err := SyncDirectory(r); err != nil {
				r.Close()
				next.Close()
				return nil, "", err
			}
		}
		r.Close()
		r = next
	}
	return r, parts[len(parts)-1], nil
}

// CheckPath permits a prospective regular file while rejecting symlinks.
func CheckPath(root, relative string) error {
	if !safeRelative(relative) {
		return fmt.Errorf("invalid packet artifact path")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for i := range parts {
		info, err := r.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return fmt.Errorf("packet path contains a symlink or non-regular component")
		}
	}
	return nil
}

func ReadFile(root, relative string, limit int64) ([]byte, error) {
	p, name, err := OpenParent(root, relative, false)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	return readFromParent(p, name, limit)
}

func readFromParent(p *os.Root, name string, limit int64) ([]byte, error) {
	before, err := p.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("packet artifact is not a regular file")
	}
	f, err := p.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, fmt.Errorf("packet artifact changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("packet artifact exceeds %d bytes", limit)
	}
	return data, nil
}

// Publish creates an entire durable file without replacing an existing one.
// If another publisher wins, its complete bytes are returned for validation.
func Publish(root, relative string, data []byte) ([]byte, error) {
	return PublishBounded(root, relative, data, 1<<20)
}

// PublishBounded preserves exclusive publication for larger recovery assets.
// The bound applies to both new bytes and an existing winner.
func PublishBounded(root, relative string, data []byte, limit int64) ([]byte, error) {
	if limit < 0 || int64(len(data)) > limit {
		return nil, fmt.Errorf("artifact exceeds %d bytes", limit)
	}
	p, name, err := OpenParent(root, relative, true)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	if current, err := readFromParent(p, name, limit); err == nil {
		return current, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	tmp := fmt.Sprintf(".packet-%x.tmp", random)
	f, err := p.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer p.Remove(tmp)
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return nil, err
	}
	if err := p.Link(tmp, name); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err := SyncDirectory(p); err != nil {
		return nil, err
	}
	return readFromParent(p, name, limit)
}

// SyncDirectory syncs a pinned directory on POSIX. On Windows, ordinary
// directory handles cannot be flushed; validate the handle while retaining
// regular-file syncs and recovery markers, as in runtime's storage contract.
func SyncDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		info, statErr := dir.Stat()
		if statErr == nil && !info.IsDir() {
			statErr = fmt.Errorf("packet root is not a directory")
		}
		return errors.Join(statErr, dir.Close())
	}
	return errors.Join(dir.Sync(), dir.Close())
}
