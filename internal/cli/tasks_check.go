package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/entroforge/go-system-builder/internal/semantic"
)

// runTasks exposes S4's mechanical close: batch completeness, closing
// contracts, bidirectional clause coverage against the CONTRACTS index, and
// DAG acyclicity. The core lives in internal/semantic so the tasks_checked
// guard and validate --all run the same check.
func runTasks(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintln(stderr, "tasks requires <check>")
		return 2
	}
	flags := flag.NewFlagSet("tasks check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "tasks check")
	root := flags.String("root", ".", "repository root")
	asJSON := flags.Bool("json", false, "machine-readable output")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	result, err := semantic.TasksCheck(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("tasks check", err))
		return 1
	}
	if *asJSON {
		return encodeJSON(stdout, result)
	}
	if len(result.Problems) > 0 {
		for _, problem := range result.Problems {
			fmt.Fprintf(stderr, "  %s\n", problem)
		}
		fmt.Fprintf(stderr, "tasks check: %d problem(s) across %d task(s) — fix the flagged items and rerun\n", len(result.Problems), result.Tasks)
		return 1
	}
	for _, load := range result.ReferenceLoads {
		fmt.Fprintf(stdout, "  load: %s\n", load)
	}
	fmt.Fprintf(stdout, "tasks check: %d task(s) (%d cancelled), clauses %d/%d covered, dependencies acyclic — batch ready\n",
		result.Tasks, result.Cancelled, result.ClausesCovered, result.ClausesTotal)
	return 0
}
