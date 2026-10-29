package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "optimization-eval <list|run> [--root <factory> --output <new-runs-parent> --case <G08-ID|all> --mode <mechanism|real_host>]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("optimization-eval", flag.ContinueOnError)
	root := fs.String("root", ".", "factory Git source; actual dirty source is frozen")
	output := fs.String("output", ".local/optimization-evals", "parent for unique preserved run directories")
	caseID := fs.String("case", "all", "exact case ID or all twelve")
	mode := fs.String("mode", "mechanism", "mechanism or real_host (not_run without real adapter)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	catalog, err := loadCatalog()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if os.Args[1] == "list" {
		encoder.Encode(catalog)
		return
	}
	if os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "unknown evaluation command")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	report, err := runEvaluation(ctx, *root, *output, *mode, *caseID, catalog)
	encoder.Encode(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, c := range report.Cases {
		if c.Status != "pass" {
			os.Exit(1)
		}
	}
}
