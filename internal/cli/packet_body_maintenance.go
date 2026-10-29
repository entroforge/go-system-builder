package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// This is framework storage maintenance, never an author submission step.
func runPacketBodyMaintenance(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime compact-packets", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	writer := runtime.NewWriter(resolveRootPath(abs, *statePath), resolveRootPath(abs, *journalPath), abs, semantic.RuntimeCandidateValidator{}).WithContext(runtime.WithPacketBodyRoot(context.Background(), abs))
	snapshot, err := writer.ExternalizePacketBodies()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime compact-packets", err))
		return 1
	}
	fmt.Fprintf(stdout, "packet storage maintained at Runtime revision %d; original responsibilities, answers and reception records preserved\n", snapshot.Revision)
	return 0
}
