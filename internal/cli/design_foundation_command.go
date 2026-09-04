package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/entroforge/go-system-builder/internal/designfoundation"
)

func runDesignFoundation(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "design-foundation requires <check|emit-css|export-portable|migrate>")
		return 2
	}
	switch args[0] {
	case "check":
		return runDesignFoundationCheck(args[1:], stdout, stderr)
	case "emit-css":
		return runDesignFoundationEmitCSS(args[1:], stdout, stderr)
	case "export-portable":
		return runDesignFoundationExport(args[1:], stdout, stderr)
	case "migrate":
		return runDesignFoundationMigrate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "design-foundation: unknown subcommand %q; expected check, emit-css, export-portable or migrate\n", args[0])
		return 2
	}
}

func runDesignFoundationCheck(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("design-foundation check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "design-foundation check")
	root := flags.String("root", ".", "repository root")
	strict := flags.Bool("strict", false, "exit 1 when advisory warnings exist")
	jsonOut := flags.Bool("json", false, "emit JSON report")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	report, err := designfoundation.Check(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("design-foundation check", err))
		return 1
	}
	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		printDesignFoundationReport(stdout, report)
	}
	if *strict && len(report.Warnings()) > 0 {
		return 1
	}
	return 0
}

func printDesignFoundationReport(w io.Writer, report designfoundation.Report) {
	if len(report.Findings) == 0 {
		fmt.Fprintln(w, "design-foundation check: no findings (advisory)")
		return
	}
	fmt.Fprintf(w, "design-foundation check: %d finding(s) (advisory; aesthetic quality is not judged)\n", len(report.Findings))
	for _, f := range report.Findings {
		loc := f.Path
		if loc == "" {
			loc = "."
		}
		fmt.Fprintf(w, "  [%s] %s: %s — %s\n", f.Severity, f.Code, loc, f.Detail)
	}
	if len(report.Warnings()) > 0 {
		fmt.Fprintln(w, "next: missing Foundation → skills/design-foundation F0–F6; unregistered hex → packages/design-tokens/tokens.json; repeats → docs/design/decisions/ (CP-*/EX-*, legacy docs/design/components/ still recognized)")
	}
}

func runDesignFoundationEmitCSS(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("design-foundation emit-css", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "design-foundation emit-css")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	path, err := designfoundation.EmitCSSFile(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("design-foundation emit-css", err))
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	return 0
}

func runDesignFoundationExport(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("design-foundation export-portable", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "design-foundation export-portable")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	path, err := designfoundation.ExportPortable(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("design-foundation export-portable", err))
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s (derived snapshot, not authority)\n", path)
	return 0
}

func runDesignFoundationMigrate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("design-foundation migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "design-foundation migrate")
	root := flags.String("root", ".", "repository root")
	to := flags.String("to", "contract-v1", "migration target (only contract-v1)")
	write := flags.Bool("write", false, "write markers (default is dry-run)")
	dryRun := flags.Bool("dry-run", true, "preview without writing")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	// --write overrides dry-run
	isDryRun := *dryRun
	for _, a := range args {
		if a == "--write" {
			isDryRun = false
			break
		}
	}
	if *write {
		isDryRun = false
	}
	if *to != "contract-v1" {
		fmt.Fprintln(stderr, "design-foundation migrate: only --to contract-v1 is supported")
		return 2
	}
	plan, err := designfoundation.PlanMigrate(*root, *to, isDryRun)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("design-foundation migrate", err))
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(plan)
	if !isDryRun {
		if written, werr := designfoundation.WriteMigrate(*root, plan); werr == nil && len(written) > 0 {
			for _, line := range written {
				fmt.Fprintln(stderr, line)
			}
		}
	}
	return 0
}

