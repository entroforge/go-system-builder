package integration

import (
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

// VerifyCapturedChecks verifies new tool records belonging to this original
// checkpoint. Legacy receipt paths cannot become new reuse provenance.
func VerifyCapturedChecks(checkpointPath string, cp Checkpoint) error {
	paths := map[string]bool{}
	for _, path := range cp.CheckReceipts {
		paths[path] = true
	}
	seen := map[string]bool{}
	for _, ref := range cp.ToolCheckReceipts {
		if !paths[ref.Path] || seen[ref.Path] || ref.Binding.AssignmentID != cp.AssignmentID || ref.Binding.Generation != cp.BaselineGeneration || ref.Binding.Responsibility != "main_required_integration" {
			return fmt.Errorf("captured check differs from original integration checkpoint")
		}
		seen[ref.Path] = true
		if _, err := checkexec.VerifyToolReceipt(filepath.Join(filepath.Dir(checkpointPath), "checks"), ref); err != nil {
			return err
		}
	}
	return nil
}
