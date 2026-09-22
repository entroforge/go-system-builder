// Package workspace owns the explicit REQ development and release destinations.
package workspace

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"os/exec"
	"path/filepath"
	"strings"
)

type Binding struct {
	ProjectRoot     string `json:"project_root"`
	DevBranch       string `json:"dev_branch"`
	ReleaseUpstream string `json:"release_upstream"`
	BoundCommit     string `json:"bound_commit"`
}

func Bind(root, dev, upstream string) (Binding, error) {
	b := Binding{}
	if dev == "" || upstream == "" {
		return b, fmt.Errorf("req bind requires explicit --dev-branch and --release-upstream; neither has a default")
	}
	dev = strings.TrimPrefix(dev, "refs/heads/")
	if e := exec.Command("git", "check-ref-format", "refs/heads/"+dev).Run(); e != nil {
		return b, fmt.Errorf("invalid development branch %q", dev)
	}
	if strings.HasPrefix(upstream, "-") || strings.ContainsAny(upstream, " \t\n") {
		return b, fmt.Errorf("invalid release upstream %q", upstream)
	}
	ref := upstream
	if !strings.HasPrefix(ref, "refs/") {
		ref = "refs/heads/" + ref
	}
	if e := exec.Command("git", "check-ref-format", ref).Run(); e != nil {
		return b, fmt.Errorf("invalid release upstream %q", upstream)
	}
	commit, e := fileview.Resolve(root, "refs/heads/"+dev)
	if e != nil {
		return b, e
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return b, e
	}
	return Binding{root, dev, upstream, commit}, nil
}
func (b Binding) Map() map[string]any {
	return map[string]any{"project_root": b.ProjectRoot, "dev_branch": b.DevBranch, "release_upstream": b.ReleaseUpstream, "bound_commit": b.BoundCommit}
}

// ValidateCheckout proves that a recorded path is a checkout of the same
// repository, without changing which checkout owns the control plane.
func ValidateCheckout(root, checkout string) error {
	common := func(path string) (string, error) {
		out, err := exec.Command("git", "-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	}
	authority, err := common(root)
	if err != nil {
		return err
	}
	worker, err := common(checkout)
	if err != nil {
		return err
	}
	if authority != worker {
		return fmt.Errorf("worktree belongs to another repository: %s", checkout)
	}
	out, err := exec.Command("git", "-C", checkout, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return err
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return err
	}
	if top != actual {
		return fmt.Errorf("worktree path is not a checkout root: %s", checkout)
	}
	return nil
}
