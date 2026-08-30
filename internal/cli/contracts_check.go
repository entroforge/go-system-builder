package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/entroforge/go-system-builder/internal/semantic"
)

// runContracts exposes S3's mechanical close: token reconciliation, clause
// cells, and the fingerprint column against disk. The core lives in
// internal/semantic so doctor / validate --all run the same check.
func runContracts(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintln(stderr, "contracts requires <check>")
		return 2
	}
	flags := flag.NewFlagSet("contracts check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "contracts check")
	root := flags.String("root", ".", "repository root")
	asJSON := flags.Bool("json", false, "machine-readable output")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	result, err := semantic.ContractsCheck(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("contracts check", err))
		return 1
	}
	if *asJSON {
		return encodeJSON(stdout, result)
	}
	if len(result.Problems) > 0 {
		for _, problem := range result.Problems {
			fmt.Fprintf(stderr, "  %s\n", problem)
		}
		fmt.Fprintf(stderr, "contracts check: %d problem(s) across %d contract(s) — fix the flagged cells and rerun\n", len(result.Problems), result.Contracts)
		return 1
	}
	fmt.Fprintf(stdout, "contracts check: %d contract(s), %d token ref(s), %d clause cell(s), %d fingerprint(s) — all reconciled\n",
		result.Contracts, result.TokenRefs, result.Clauses, result.Fingerprints)
	return 0
}
