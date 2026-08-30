package missingtokens

import (
	"strings"
	"testing"
)

func TestLegendCoversPresentTokensOnly(t *testing.T) {
	legend := RenderMissingTokenLegend("GATE-BUILDER-BATCH-READY", []string{
		"evidence:completion_report:TASK-039-02",
		"integration_checkpoint:TASK-039-02",
		"integration_checkpoint:TASK-039-03",
	})
	for _, want := range []string{
		"MISSING TOKENS:",
		"`evidence:completion_report:<id>`",
		"run `runtime task-complete` for the named TASK",
		"`integration_checkpoint:<id>`",
		"run `runtime task-integrate --assignment-id <id>`",
	} {
		if !strings.Contains(legend, want) {
			t.Fatalf("legend missing %q:\n%s", want, legend)
		}
	}
	// Two tokens of one family collapse to one line; the absent families
	// (checks / scope_deviations / batch) must not appear.
	if strings.Contains(legend, "scope_deviations") {
		t.Fatalf("absent family leaked into legend:\n%s", legend)
	}
	if got := strings.Count(legend, "integration_checkpoint"); got != 1 {
		t.Fatalf("family repeated %d times, want 1:\n%s", got, legend)
	}
}

func TestLegendNamesUnknownTokens(t *testing.T) {
	legend := RenderMissingTokenLegend("GATE-BUILDER-BATCH-READY", []string{
		"evidence:completion_report:TASK-1",
		"some_future_token",
	})
	if !strings.Contains(legend, "some_future_token") || !strings.Contains(legend, "no legend entry") {
		t.Fatalf("unknown token must be surfaced, got:\n%s", legend)
	}
}

func TestLegendEmptyForOtherGates(t *testing.T) {
	if legend := RenderMissingTokenLegend("GATE-DOCUMENT-PASS", []string{"document:req:locked"}); legend != "" {
		t.Fatalf("non-builder gate must render no legend, got:\n%s", legend)
	}
	if legend := RenderGateTokenLegend("GATE-DOCUMENT-PASS"); legend != "" {
		t.Fatalf("full legend must be gated too, got:\n%s", legend)
	}
}

func TestFullLegendListsEveryFamily(t *testing.T) {
	legend := RenderGateTokenLegend("GATE-BUILDER-BATCH-READY")
	for _, want := range []string{
		"`batch:execution_batch_empty`",
		"`evidence:completion_report`",
		"`evidence:completion_report:<id>`",
		"`checks:<id>`",
		"`scope_deviations:<id>`",
		"`integration_checkpoint:<id>`",
	} {
		if !strings.Contains(legend, want) {
			t.Fatalf("full legend missing %q:\n%s", want, legend)
		}
	}
}
