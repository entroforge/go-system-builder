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
		fmt.Fprintln(stderr, "design-foundation requires <check|emit-css|export-portable>")
		return 2
	}
	switch args[0] {
	case "check":
		return runDesignFoundationCheck(args[1:], stdout, stderr)
	case "emit-css":
		return runDesignFoundationEmitCSS(args[1:], stdout, stderr)
	case "export-portable":
		return runDesignFoundationExport(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "design-foundation: unknown subcommand %q; expected check, emit-css or export-portable\n", args[0])
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
		fmt.Fprintln(w, "next: missing Foundation → skills/design-foundation F0–F6; unregistered hex → packages/design-tokens/tokens.json; repeats → docs/design/components/COMPONENT-PROPOSAL-template.md")
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
