package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestActionsCatalogShowsCanonicalReviewRepairLoop(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"actions"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("actions failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{
		"Canonical Agent action catalog",
		"s7 draft",
		"runtime review-plan --file <plan.json>",
		"runtime review-result submit",
		"runtime investigation hypothesis register",
		"runtime repair plan-report submit",
		"runtime repair changeset compute --session-id <session>",
		"runtime repair handoff commit",
		"return to a fresh S7 round",
		"compatibility: runtime register-workgroup",
		"do not call runtime transition",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("action catalog missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestPrimaryCommandHelpIsActionable(t *testing.T) {
	for _, args := range [][]string{
		{"s7", "--help"},
		{"s7", "status", "--help"},
		{"s10", "--help"},
		{"runtime", "--help"},
		{"runtime", "repair", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := cli.Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("%v help failed: code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage:") || strings.Contains(stderr.String(), "requires") {
			t.Fatalf("%v help is not a clean actionable response: stdout=%s stderr=%s", args, stdout.String(), stderr.String())
		}
	}
}
