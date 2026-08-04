package cli

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/entroforge/go-system-builder/internal/e2ecoverage"
)

// runE2ECoverage scores an E2E scenario inventory (fidelity ladder / ready gate).
//
// Usage:
//
//	loop-harness e2e-coverage --inventory <path> [--gate]
func runE2ECoverage(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("e2e-coverage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "e2e-coverage")
	inventoryPath := flags.String("inventory", "", "path to E2E scenario inventory JSON (required)")
	gate := flags.Bool("gate", false, "exit non-zero when E2E ready gate fails (default: warn only)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*inventoryPath) == "" {
		fmt.Fprintln(stderr, "e2e-coverage requires --inventory <path>")
		return 2
	}

	data, err := os.ReadFile(*inventoryPath)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("e2e-coverage", err))
		return 1
	}

	inv, err := e2ecoverage.LoadInventory(*inventoryPath)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("e2e-coverage", err))
		return 1
	}

	report := e2ecoverage.Score(inv)
	report.InventoryPath = *inventoryPath
	report.InventorySHA = fmt.Sprintf("%x", sha256.Sum256(data))

	e2ecoverage.FormatReport(stdout, report)

	if *gate && !report.GatePassed {
		return 1
	}
	return 0
}
