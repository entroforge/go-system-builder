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
		"S6 Builder implementation",
		"s6 prepare --task <TASK> --role <Builder-role>",
		"SubagentStop calls the original Result consumer",
		"No generic begin, activation or per-tool receipt",
		"continues implementation and actual TASK self-checks",
		"ACK and cleanup",
		"SubagentStart supplies short context",
		"--revise-assignment <id> --revision-intent-key <stable-key>",
		"runtime workspace pending",
		"runtime task-integrate --assignment-id <id>",
		"ReviewPlan questionnaire",
		"s7 manifest-draft --assignment <id>",
		"original ReviewResult consumer",
		"runtime investigation ingest --root <root> --grouping-rationale",
		"runtime investigation hypothesis register",
		"original Case consumer",
		"Contract approval prepares the original RepairSession, RepairPlan",
		"original `runtime repair dispatch`",
		"original PlanReport consumer",
		"no extra path return or submission is required for reception",
		"Hook-provided ChangeImpact answer",
		"TargetedReverification answer",
		"existing handoff consumer",
		"S10: use `s10 status` for read-only progress",
		"original consumer validates it, derives machine metadata",
		"Main faithfully fills disposition and optional reason",
		"fresh complete S7 round",
		"not a second delivery after saving a Hook-provided answer",
		"runtime repair session open`, `runtime repair plan compile`",
		"runtime transition` is legacy/low-level",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("action catalog missing %q:\n%s", want, stdout.String())
		}
	}
	for _, obsoleteNormalFlow := range []string{
		"s7 draft --out <plan.json>",
		"runtime review-plan --file <plan.json>",
		"runtime review-result submit --assignment-id <id> --result <result.json>",
		"runtime investigation hypothesis result` →",
		"runtime repair plan-report submit --file <plan-report.json>",
		"runtime repair result submit --file <result.json>",
		"runtime repair impact create|commit",
		"s10 manifest validate --file <manifest.json>` → register evidence",
	} {
		if strings.Contains(stdout.String(), obsoleteNormalFlow) {
			t.Fatalf("action catalog still presents %q as normal flow:\n%s", obsoleteNormalFlow, stdout.String())
		}
	}
	text := stdout.String()
	for _, section := range []string{"S7 complete verification", "S9 complete repair and return"} {
		start := strings.Index(text, section)
		if start < 0 {
			t.Fatalf("missing section %q: %s", section, text)
		}
		end := strings.Index(text[start+len(section):], "\nS")
		if end < 0 {
			t.Fatalf("missing section %q: %s", section, text)
		}
		body := text[start : start+len(section)+end]
		for _, want := range []string{"PreToolUse", "PostToolUse", "Stop/SubagentStop", "original"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing natural reception %q: %s", section, want, body)
			}
		}
		for _, stale := range []string{"for SubagentStop reception", "SubagentStop receives the private", "then return the exact brief and absolute Result path"} {
			if strings.Contains(body, stale) {
				t.Errorf("%s retains obsolete reception %q: %s", section, stale, body)
			}
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
