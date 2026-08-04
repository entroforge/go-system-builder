package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// TestFormatFailureAppendsManualAnchorForGuardErrors locks the manual-anchor
// retest contract for guard failures: every CLI gate-blocking stderr must
// end with ` See .claude/bin/loop-harness.md#<lowercase(rule_id)>.` for any
// error whose message starts with `guard <NAME> failed:` or `guard <NAME>
// is not registered`. Non-guard errors must NOT receive an anchor (they
// would mislead the reader).
func TestFormatFailureAppendsManualAnchorForGuardErrors(t *testing.T) {
	cases := []struct {
		name          string
		cmd           string
		err           error
		wantID        string
		wantHasAnchor bool
	}{
		{
			name:          "guard failure from evidenceBackedGuard stub",
			cmd:           "runtime transition",
			err:           errors.New("guard baseline_fingerprint_captured failed: baseline_fingerprint_captured: resolved evidence context is empty"),
			wantID:        "baseline_fingerprint_captured",
			wantHasAnchor: true,
		},
		{
			name:          "guard unregistered",
			cmd:           "runtime transition",
			err:           errors.New("transition PTR-PLAN-01 guard no_accepted_bugs is not registered"),
			wantID:        "no_accepted_bugs",
			wantHasAnchor: true,
		},
		{
			name:          "uppercase guard name lowercased in anchor",
			cmd:           "req bind",
			err:           errors.New("guard UI_Impact_Resolved failed: bound REQ REQ-001 declares ui_impact=unknown"),
			wantID:        "ui_impact_resolved",
			wantHasAnchor: true,
		},
		{
			name:          "io error gets no anchor",
			cmd:           "req bind",
			err:           errors.New("open docs/requirements/REQ-001.md: no such file or directory"),
			wantID:        "",
			wantHasAnchor: false,
		},
		{
			name:          "json parse error gets no anchor",
			cmd:           "runtime reconcile",
			err:           errors.New("decode runtime: unexpected end of JSON input"),
			wantID:        "",
			wantHasAnchor: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatFailure(tc.cmd, tc.err)
			if !strings.HasPrefix(got, tc.cmd+": ") {
				t.Fatalf("formatFailure must prefix with %q, got %q", tc.cmd+": ", got)
			}
			if tc.wantHasAnchor {
				wantAnchor := "See " + transition.ManualTargetPath() + "#" + tc.wantID + "."
				if !strings.Contains(got, wantAnchor) {
					t.Fatalf("formatFailure must contain %q, got %q", wantAnchor, got)
				}
				if !strings.HasSuffix(got, ".") {
					t.Fatalf("formatFailure should end with period when anchor present, got %q", got)
				}
			} else {
				if strings.Contains(got, "See ") {
					t.Fatalf("non-guard error must not include anchor, got %q", got)
				}
			}
		})
	}
}

// TestExtractRuleIDPicksGuardNameNotBody confirms that a multi-token guard
// error (guard NAME + body containing "guard" elsewhere) still resolves to
// the first guard name.
func TestExtractRuleIDPicksGuardNameNotBody(t *testing.T) {
	msg := "guard req_locked failed: req_locked: guard admin would also fail"
	if id := extractRuleID(msg); id != "req_locked" {
		t.Fatalf("extractRuleID should pick first guard name, got %q", id)
	}
}
