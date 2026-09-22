package releasegraph

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/doclinks"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
)

// ValidateInstalledProject uses the deployed .claude assets and AGENTS.md;
// ValidateStagedRelease deliberately continues to validate the tar source tree.
func ValidateInstalledProject(root string) error {
	if err := projectlayout.Check(root); err != nil {
		return err
	}
	required := append([]string{"AGENTS.md", ".claude/settings.json", ".claude/loop.md", ".claude/bin/loop-harness.md", "docs/project-map.md"}, RequiredDocumentAssets...)
	for _, rel := range required {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("installed layout: required %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("installed layout: %s is not a file", rel)
		}
	}
	for _, rel := range []string{"blueprint", "docs/framework", "docs/product"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			return fmt.Errorf("installed layout: excluded directory %s must not be installed", rel)
		}
	}
	if err := assertHarnessBinary(root); err != nil {
		return err
	}
	if err := catalog.ValidateSkills(root); err != nil {
		return err
	}
	if err := catalog.ValidateAgents(root); err != nil {
		return err
	}
	return doclinks.Validate(root)
}
