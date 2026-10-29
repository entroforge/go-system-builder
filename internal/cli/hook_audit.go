package cli

import (
	"context"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/audit"
)

// An original reception may finish inside its reserved completion interval.
// Audit uses the same original deadline rather than the expired work context
// or a fresh local budget. Explicit cancellation remains attached.
func appendHookAudit(ctx context.Context, root string, record any) error {
	completion, cancel := hookCompletionContext(ctx)
	defer cancel()
	return audit.NewOutbox(filepath.Join(root, ".claude", "hook-decisions.jsonl")).AppendContext(completion, record)
}
