package fileview

import (
	"fmt"
	"os"
	"path/filepath"
)

type Reader interface {
	ReadFile(string) ([]byte, error)
	ReadDir(string) ([]os.DirEntry, error)
}

// Disk is for explicitly disk-based authoring commands and test fixtures.
// Production stage evaluation uses New with its upstream rules.
type Disk struct{ Root string }

func (d Disk) ReadFile(p string) ([]byte, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(d.Root, p)
	}
	return os.ReadFile(p)
}
func (d Disk) ReadDir(p string) ([]os.DirEntry, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(d.Root, p)
	}
	return os.ReadDir(p)
}
func DevelopmentRef(state map[string]any) (string, error) {
	bound, _ := state["bound_req"].(map[string]any)
	w, _ := bound["workspace"].(map[string]any)
	ref, _ := w["dev_branch"].(string)
	if ref == "" {
		return "", fmt.Errorf("REQ development branch is not bound; run req workspace --dev-branch <branch> --release-upstream <upstream> (no default branch)")
	}
	return ref, nil
}

// ValidateAuthority prevents a copied runtime in a worker checkout from
// becoming a second writer. A linked checkout may itself be the bound root.
func ValidateAuthority(root string, state map[string]any) error {
	bound, _ := state["bound_req"].(map[string]any)
	binding, _ := bound["workspace"].(map[string]any)
	declared, _ := binding["project_root"].(string)
	if declared == "" {
		return fmt.Errorf("REQ authority root is not bound")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	declaredAbs, err := filepath.Abs(declared)
	if err != nil {
		return err
	}
	if canonicalRoot(abs) != canonicalRoot(declaredAbs) {
		return fmt.Errorf("use the REQ authority root %s; current checkout %s is not its control plane", declared, root)
	}
	return nil
}
