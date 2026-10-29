package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
)

type pendingIntegration = integration.PendingIntegration

// Pending work is derived from durable completion reports and checkpoints;
// it never introduces a second queue whose authority could drift from Runtime.
func runWorkspacePending(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace pending", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "Main control root")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	loaded, err := hookctx.LoadFull(*root, "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rows, err := pendingIntegrations(*root, loaded)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err = json.NewEncoder(stdout).Encode(rows); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func pendingIntegrations(root string, loaded *hookctx.LoadedContext) ([]pendingIntegration, error) {
	return integration.Pending(root, loaded)
}
