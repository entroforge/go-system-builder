package cli

import (
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// ExportTestWorkspaceOwnedReceipt exposes the existing package-private
// Writer to external-package tests; it is not compiled into production.
func ExportTestWorkspaceOwnedReceipt(root string, execution workspace.Execution, index int, ref checkexec.ToolReceipt) error {
	return acceptWorkspaceOwnedCheckReceipt(root, execution, index, ref)
}
