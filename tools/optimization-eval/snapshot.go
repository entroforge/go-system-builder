package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type sourceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
}
type sourceManifest struct {
	Revision string       `json:"revision"`
	Dirty    bool         `json:"dirty"`
	Digest   string       `json:"digest"`
	Files    []sourceFile `json:"files"`
}

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func sourceAllowed(ref string) bool {
	if !safeRelative(ref) {
		return false
	}
	root := strings.Split(ref, "/")[0]
	switch root {
	case "cmd", "internal", "tests", "tools", "agents", "skills", "docs", "packaging":
		return true
	case "go.mod", "go.sum", "settings.json", "AGENTS-template.md", "loop-template.md", "prelude.md", "INSTALL.md", "README.md", "LICENSE":
		return true
	}
	return false
}

func frozenSnapshot(root, destination string) (sourceManifest, error) {
	m := sourceManifest{Files: []sourceFile{}}
	git := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		return cmd.Output()
	}
	revision, err := git("rev-parse", "HEAD")
	if err != nil {
		return m, err
	}
	m.Revision = strings.TrimSpace(string(revision))
	status, err := git("status", "--porcelain")
	if err != nil {
		return m, err
	}
	m.Dirty = len(status) > 0
	listed, err := git("ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return m, err
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, ref := range strings.Split(string(listed), "\x00") {
		if sourceAllowed(ref) && !seen[ref] {
			paths = append(paths, ref)
			seen[ref] = true
		}
	}
	sort.Strings(paths)
	if len(paths) > 20000 {
		return m, fmt.Errorf("source snapshot exceeds 20000 files")
	}
	var total int64
	for _, ref := range paths {
		original := filepath.Join(root, filepath.FromSlash(ref))
		before, err := os.Lstat(original)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return m, err
		}
		if !before.Mode().IsRegular() {
			return m, fmt.Errorf("source must be a regular file: %s", ref)
		}
		resolved, err := filepath.EvalSymlinks(original)
		if err != nil {
			return m, err
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel != filepath.FromSlash(ref) {
			return m, fmt.Errorf("source traverses a linked path: %s", ref)
		}
		total += before.Size()
		if before.Size() > 32<<20 || total > 256<<20 {
			return m, fmt.Errorf("source snapshot exceeds bounded size")
		}
		data, err := os.ReadFile(original)
		if err != nil {
			return m, err
		}
		after, err := os.Lstat(original)
		if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return m, fmt.Errorf("source changed while freezing: %s", ref)
		}
		out := filepath.Join(destination, filepath.FromSlash(ref))
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return m, err
		}
		if err := os.WriteFile(out, data, before.Mode().Perm()); err != nil {
			return m, err
		}
		m.Files = append(m.Files, sourceFile{ref, hash(data), uint32(before.Mode().Perm()), int64(len(data))})
	}
	if !seen["go.mod"] || !seen["docs/control/loop-definition.json"] {
		return m, fmt.Errorf("expected factory source files missing")
	}
	data, _ := json.Marshal(m.Files)
	m.Digest = hash(data)
	for _, args := range [][]string{{"init", "-b", "eval-source"}, {"config", "user.name", "Optimization mechanism fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "."}, {"commit", "-q", "-m", "Frozen optimization evaluation source"}} {
		cmd := exec.Command("git", append([]string{"-C", destination}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return m, fmt.Errorf("initialize isolated source Git: %v %s", err, output)
		}
	}
	return m, nil
}

func verifySnapshot(root string, m sourceManifest) error {
	known := map[string]bool{}
	for _, file := range m.Files {
		known[file.Path] = true
		p := filepath.Join(root, filepath.FromSlash(file.Path))
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || uint32(info.Mode().Perm()) != file.Mode {
			return fmt.Errorf("frozen source changed: %s", file.Path)
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		f.Close()
		if copyErr != nil || hex.EncodeToString(h.Sum(nil)) != file.SHA256 {
			return fmt.Errorf("frozen source changed: %s", file.Path)
		}
	}
	return filepath.WalkDir(root, func(p string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		ref := filepath.ToSlash(rel)
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && sourceAllowed(ref) && !known[ref] {
			return fmt.Errorf("unexpected new frozen source: %s", ref)
		}
		return nil
	})
}
