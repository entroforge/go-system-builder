package cli

import (
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"io"
)

func runIntegrationStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace integration-status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "actual Main checkout whose original integration lock is observed")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if err := workspace.RequireMain(*root); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return encodeJSON(stdout, integration.ObserveWorkspace(*root))
}
