package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/entroforge/go-system-builder/internal/doclinks"
	"github.com/entroforge/go-system-builder/internal/releasegraph"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// install publishes a fully validated fresh installation by directory rename.
// It never overlays an existing project or rewrites an active Runtime.
func runInstall(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourceArg := flags.String("source", "", "extracted release root")
	rootArg := flags.String("root", "", "new or empty target directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *sourceArg == "" || *rootArg == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: loop-harness install --source <extracted-release> --root <empty-target>")
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "install:", err); return 1 }
	source, err := filepath.Abs(*sourceArg)
	if err != nil {
		return fail(err)
	}
	root, err := filepath.Abs(*rootArg)
	if err != nil {
		return fail(err)
	}
	if rel, err := filepath.Rel(source, root); err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return fail(fmt.Errorf("target must be outside the release source"))
	}
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fail(fmt.Errorf("target must be a real empty directory"))
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return fail(err)
		}
		if len(entries) != 0 {
			return fail(fmt.Errorf("target is not empty; existing projects and active Runtime upgrades are not supported; keep the matching previous release"))
		}
	} else if !os.IsNotExist(err) {
		return fail(err)
	}
	if err := verifyInstallManifest(source); err != nil {
		return fail(err)
	}
	if err := releasegraph.ValidateStagedRelease(source); err != nil {
		return fail(err)
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fail(err)
	}
	stage, err := os.MkdirTemp(parent, ".loop-install-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(stage) // Only the directory created by this invocation.
	entries := [][2]string{
		{"docs", "docs"}, {"skills", ".claude/skills"}, {"agents", ".claude/agents"},
		{".claude/bin", ".claude/bin"}, {"AGENTS-template.md", "AGENTS.md"},
		{"loop-template.md", ".claude/loop.md"}, {"settings.json", ".claude/settings.json"},
		{"loop-harness.md", ".claude/bin/loop-harness.md"}, {"tools", "tools"},
		{"project.gitattributes", ".gitattributes"},
	}
	for _, entry := range entries {
		if err := copyInstallTree(filepath.Join(source, entry[0]), filepath.Join(stage, entry[1])); err != nil {
			return fail(err)
		}
	}
	binary := "loop-harness-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if info, err := os.Stat(filepath.Join(stage, ".claude/bin", binary)); err != nil || !info.Mode().IsRegular() {
		return fail(fmt.Errorf("unsupported or incomplete host binary %s", binary))
	}
	for _, launcher := range [][2]string{{"loop-harness-launcher.sh", "loop-harness"}, {"loop-harness-launcher.ps1", "loop-harness.ps1"}} {
		if err := copyInstallTree(filepath.Join(source, "tools", launcher[0]), filepath.Join(stage, ".claude/bin", launcher[1])); err != nil {
			return fail(err)
		}
	}
	if err := os.Chmod(filepath.Join(stage, ".claude/bin/loop-harness"), 0755); err != nil {
		return fail(err)
	}
	if err := copyInstallTree(filepath.Join(stage, "docs/project-map-template.md"), filepath.Join(stage, "docs/project-map.md")); err != nil {
		return fail(err)
	}
	// Resolve installed Markdown links from their original source positions.
	toInstalled := func(rel string) string {
		for _, entry := range entries {
			if rel == entry[0] {
				return entry[1]
			}
			if strings.HasPrefix(rel, entry[0]+"/") {
				return entry[1] + strings.TrimPrefix(rel, entry[0])
			}
		}
		return rel
	}
	if err := filepath.Walk(stage, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(stage, path)
		rel = filepath.ToSlash(rel)
		old := rel
		for _, entry := range entries {
			if rel == entry[1] {
				old = entry[0]
				break
			}
			if strings.HasPrefix(rel, entry[1]+"/") {
				old = entry[0] + strings.TrimPrefix(rel, entry[1])
				break
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, doclinks.Relocate(data, old, rel, toInstalled), info.Mode().Perm())
	}); err != nil {
		return fail(err)
	}
	if code := runInit([]string{"--root", stage}, io.Discard, stderr); code != 0 {
		return code
	}
	if err := releasegraph.ValidateInstalledProject(stage); err != nil {
		return fail(err)
	}
	if err := semantic.ValidateRepository(stage); err != nil {
		return fail(err)
	}
	if err := semantic.ValidateManualAgreement(stage); err != nil {
		return fail(err)
	}
	if err := recordInstalledManifest(source, stage); err != nil {
		return fail(err)
	}
	// Remove only an empty target; a concurrent writer makes this fail safely.
	if err := os.Remove(root); err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	if err := os.Rename(stage, root); err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, "installed layout v2 at", root)
	return 0
}

func copyInstallTree(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("install refuses symlink %s", source)
	}
	if info.IsDir() {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyInstallTree(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("install refuses special file %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
