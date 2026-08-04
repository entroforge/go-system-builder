package verification_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/verification"
)

func validRoundEvidence(id, responsibility string, round int) map[string]any {
	return map[string]any{
		"id":                  id,
		"kind":                "delivery_review",
		"path":                "docs/reports/review/REV-X.md",
		"sha256":              "abc",
		"status":              "valid",
		"baseline_generation": float64(1),
		"review_round":        float64(round),
		"produced_by":         []any{"agent-x"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   responsibility,
		"scope_refs":          []any{},
	}
}

func targetedReverificationEvidence(id, bugID string, round int) map[string]any {
	return map[string]any{
		"id":                  id,
		"kind":                "targeted_reverification",
		"path":                "docs/reports/bugs/" + bugID + ".md#reverify",
		"sha256":              "def",
		"status":              "valid",
		"baseline_generation": float64(1),
		"review_round":        float64(round),
		"produced_by":         []any{"agent-x"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "VER-REQ",
		"scope_refs":          []any{bugID},
	}
}

func stateWithRound(round int, evidence []map[string]any, bugs []map[string]any, teams []map[string]any) map[string]any {
	evArr := make([]any, 0, len(evidence))
	for _, e := range evidence {
		evArr = append(evArr, e)
	}
	bugArr := make([]any, 0, len(bugs))
	for _, b := range bugs {
		bugArr = append(bugArr, b)
	}
	teamArr := make([]any, 0, len(teams))
	for _, t := range teams {
		teamArr = append(teamArr, t)
	}
	return map[string]any{
		"review":   map[string]any{"round": float64(round)},
		"evidence": evArr,
		"entities": map[string]any{
			"bugs":  bugArr,
			"teams": teamArr,
		},
	}
}

func TestCleanRoundPassesWhenAllGuardsSatisfied(t *testing.T) {
	teams := []map[string]any{
		{"kind": "delivery_verifier", "responsibility_ids": []any{"VER-REQ"}},
		{"kind": "qa", "responsibility_ids": []any{"QA-QUALITY"}},
		{"kind": "e2e_browser", "responsibility_ids": []any{"E2E-USER-FLOW"}},
	}
	state := stateWithRound(1, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 1),
		validRoundEvidence("ev-2", "QA-QUALITY", 1),
		validRoundEvidence("ev-3", "E2E-USER-FLOW", 1),
	}, nil, teams)
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("expected clean round to pass, got reasons: %v", result.Reasons)
	}
}

func TestCleanRoundFailsWithoutE2EBrowserEvidence(t *testing.T) {
	teams := []map[string]any{
		{"kind": "delivery_verifier", "responsibility_ids": []any{"VER-REQ"}},
		{"kind": "qa", "responsibility_ids": []any{"QA-QUALITY"}},
		{"kind": "e2e_browser", "responsibility_ids": []any{"E2E-USER-FLOW"}},
	}
	state := stateWithRound(1, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 1),
		validRoundEvidence("ev-2", "QA-QUALITY", 1),
	}, nil, teams)
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail without E2E browser evidence")
	}
}

func TestCleanRoundFailsWithoutE2EBrowserWorkgroup(t *testing.T) {
	teams := []map[string]any{
		{"kind": "delivery_verifier", "responsibility_ids": []any{"VER-REQ"}},
		{"kind": "qa", "responsibility_ids": []any{"QA-QUALITY"}},
	}
	state := stateWithRound(1, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 1),
		validRoundEvidence("ev-2", "QA-QUALITY", 1),
	}, nil, teams)
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail without an E2E browser workgroup")
	}
}

func TestCleanRoundFailsOnMixedReviewRounds(t *testing.T) {
	state := stateWithRound(2, []map[string]any{
		validRoundEvidence("ev-current", "VER-REQ", 2),
		validRoundEvidence("ev-old", "QA-QUALITY", 1),
	}, nil, nil)
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail when evidence mixes rounds")
	}
	found := false
	for _, check := range result.Checks {
		if check.Name == "same_review_round" && !check.Passed {
			found = true
		}
	}
	if !found {
		t.Error("expected same_review_round check to fail")
	}
}

// BUG-039-36: OrganicSpine retains S6 building completion evidence at round 1
// after TR-006 bumps review.round to 2. same_review_round must only consider
// clean-round / verification-phase evidence, not historical agent_completion.
func TestCleanRoundIgnoresPriorRoundBuildingEvidence(t *testing.T) {
	teams := []map[string]any{
		{"kind": "delivery_verifier", "responsibility_ids": []any{"VER-REQ"}},
		{"kind": "qa", "responsibility_ids": []any{"QA-QUALITY"}},
		{"kind": "e2e_browser", "responsibility_ids": []any{"E2E-USER-FLOW"}},
	}
	building := map[string]any{
		"id":                  "ev-completion-1",
		"kind":                "agent_completion",
		"path":                "evidence/ev-completion-1.json",
		"sha256":              "abc",
		"status":              "valid",
		"baseline_generation": float64(1),
		"review_round":        float64(1),
		"produced_by":         []any{"builder-1"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "BUILD-WORK-PACKAGE",
		"scope_refs":          []any{},
	}
	state := stateWithRound(2, []map[string]any{
		building,
		validRoundEvidence("ev-1", "VER-REQ", 2),
		validRoundEvidence("ev-2", "QA-QUALITY", 2),
		validRoundEvidence("ev-3", "E2E-USER-FLOW", 2),
	}, nil, teams)
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("expected clean round to pass with prior-round building evidence, got: %v", result.Reasons)
	}
	for _, check := range result.Checks {
		if check.Name == "same_review_round" && !check.Passed {
			t.Fatalf("same_review_round must ignore building evidence: %s", check.Detail)
		}
	}
}

func TestCleanRoundFailsOnInvalidatedEvidence(t *testing.T) {
	ev := validRoundEvidence("ev-invalid", "VER-REQ", 1)
	ev["status"] = "invalid"
	state := stateWithRound(1, []map[string]any{ev}, nil, nil)
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail with invalidated evidence")
	}
}

func TestCleanRoundFailsOnOpenBlockingBug(t *testing.T) {
	bug := map[string]any{
		"id":       "BUG-001",
		"state":    "accepted",
		"severity": "P0",
	}
	state := stateWithRound(1, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 1),
	}, []map[string]any{bug}, nil)
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail with open blocking bug")
	}
}

func TestCleanRoundFailsOnMissingResponsibilityEvidence(t *testing.T) {
	team := map[string]any{
		"responsibility_ids": []any{"VER-REQ", "QA-QUALITY"},
	}
	state := stateWithRound(1, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 1),
	}, nil, []map[string]any{team})
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail when a responsibility lacks PASS evidence")
	}
}

func TestCleanRoundFailsWithoutRegisteredResponsibilities(t *testing.T) {
	state := stateWithRound(1, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 1),
	}, nil, nil)
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail when no review responsibilities are registered")
	}
}

func TestCleanRoundFailsAtRoundZero(t *testing.T) {
	team := map[string]any{
		"responsibility_ids": []any{"VER-REQ"},
	}
	state := stateWithRound(0, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 0),
	}, nil, []map[string]any{team})
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail before review round 1")
	}
}

func TestCleanRoundFailsWhenClosedBlockingBugLacksTargetedReverification(t *testing.T) {
	bug := map[string]any{
		"id":       "BUG-001",
		"state":    "closed",
		"severity": "P0",
	}
	team := map[string]any{
		"kind":               "delivery_verifier",
		"responsibility_ids": []any{"VER-REQ"},
	}
	state := stateWithRound(1, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 1),
	}, []map[string]any{bug}, []map[string]any{team})
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("expected clean round to fail without targeted re-verification evidence for closed BUG")
	}
}

func TestCleanRoundPassesWhenBlockingBugClosedWithTargetedReverification(t *testing.T) {
	bug := map[string]any{
		"id":       "BUG-001",
		"state":    "closed",
		"severity": "P0",
	}
	teams := []map[string]any{
		{"kind": "delivery_verifier", "responsibility_ids": []any{"VER-REQ"}},
		{"kind": "qa", "responsibility_ids": []any{"QA-QUALITY"}},
		{"kind": "e2e_browser", "responsibility_ids": []any{"E2E-USER-FLOW"}},
	}
	state := stateWithRound(1, []map[string]any{
		validRoundEvidence("ev-1", "VER-REQ", 1),
		validRoundEvidence("ev-2", "QA-QUALITY", 1),
		validRoundEvidence("ev-3", "E2E-USER-FLOW", 1),
		targetedReverificationEvidence("reverify-BUG-001-attempt-1", "BUG-001", 1),
	}, []map[string]any{bug}, teams)
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("expected clean round to pass with closed bug, got: %v", result.Reasons)
	}
}
