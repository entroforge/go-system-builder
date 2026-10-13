package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/review"
)

func runS7Lint(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s7 lint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	file := flags.String("file", "", "ReviewPlan draft path (required)")
	authorOnly := flags.Bool("author-only", false, "check schema and plan semantics without reading Runtime or input files")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return flagParseExitCode(err)
	}
	if *file == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "s7 lint requires --file <plan.json>")
		return 2
	}
	data, err := os.ReadFile(resolveRootPath(*root, *file))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var state map[string]any
	if !*authorOnly {
		stateData, err := os.ReadFile(filepath.Join(*root, ".claude/loop-state.json"))
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err == nil {
			if err = json.Unmarshal(stateData, &state); err != nil || state == nil {
				fmt.Fprintf(stderr, "invalid Runtime snapshot: %v\n", err)
				return 1
			}
		}
	}
	report := review.PreflightPlan(*root, data, state)
	if code := encodeJSON(stdout, report); code != 0 {
		return code
	}
	if !report.ArtifactValid {
		return 1
	}
	return 0
}
