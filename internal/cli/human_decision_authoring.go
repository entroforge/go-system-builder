package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func runHumanDecisionAuthoring(args []string, stdout, stderr io.Writer) int {
	operation := args[0]
	flags := flag.NewFlagSet("runtime human-decision "+operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root; Runtime is read without recovery or locking")
	var file, actor, out *string
	if operation == "lint" {
		file = flags.String("file", "", "filled human decision JSON (required)")
		actor = flags.String("actor", "", "execution role to validate, independent of the human approver (required)")
	} else {
		out = flags.String("out", "", "write a new draft file exclusively (default stdout)")
	}
	if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
		return flagParseExitCode(err)
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	if operation == "lint" && (*file == "" || *actor == "") {
		fmt.Fprintln(stderr, "human-decision lint requires --file and --actor")
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, ".claude/loop-state.json"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil || state == nil {
		fmt.Fprintf(stderr, "invalid Runtime snapshot: %v\n", err)
		return 1
	}
	if operation == "lint" {
		data, err = os.ReadFile(resolveRootPath(*root, *file))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		report := transition.PreflightHumanReleaseDecision(*root, state, data, *actor)
		if code := encodeJSON(stdout, report); code != 0 {
			return code
		}
		if !report.ArtifactValid {
			return 1
		}
		return 0
	}
	draft := transition.HumanDecisionDraft(state)
	if *out == "" {
		return encodeJSON(stdout, draft)
	}
	data, err = json.MarshalIndent(draft, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	path := resolveRootPath(*root, *out)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, writeErr := f.Write(append(data, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		fmt.Fprintln(stderr, writeErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintln(stderr, closeErr)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}
