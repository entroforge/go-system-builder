// Command loop-harness is the entry point for the Loop Engineering Harness.
// It delegates to internal/cli.Run, which dispatches the subcommands
// (init, validate, doctor, hook, dry-run, runtime, team, impact,
// verification, release-graph).
package main

import (
	"os"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
