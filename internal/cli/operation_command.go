package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func runRuntimeOperation(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime operation", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository authority root")
	state := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journal := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	id := flags.String("id", "", "operation ID to inspect; no recovery or state/journal writes (required)")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return flagParseExitCode(err)
	}
	if *id == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "runtime operation requires --id <operation-id>")
		return 2
	}
	store := runtime.NewStore(resolveRootPath(*root, *state), resolveRootPath(*root, *journal))
	report, err := store.InspectOperation(*root, *id)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime operation", err))
		return 1
	}
	return encodeJSON(stdout, report)
}
