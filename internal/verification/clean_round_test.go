package verification_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/verification"
)

// cleanState builds a state carrying the S7 ReviewPlan projection: plan
// pointer, claim dispositions, evidence and finding/BUG entities.
type cleanStateSpec struct {
	round           int
	planStatus      string
	claims          map[string]map[string]any // claim_id -> {disposition, applicability, result_id}
	evidence        []map[string]any
	findings        []map[string]any
	bugs            []map[string]any
	planRoundOffset int // make the plan belong to a different round
}

func cleanState(spec cleanStateSpec) map[string]any {
	evidence := make([]any, 0, len(spec.evidence))
	for _, entry := range spec.evidence {
		evidence = append(evidence, entry)
	}
	findings := make([]any, 0, len(spec.findings))
	for _, row := range spec.findings {
		findings = append(findings, row)
	}
	bugs := make([]any, 0, len(spec.bugs))
	for _, row := range spec.bugs {
		bugs = append(bugs, row)
	}
	state := map[string]any{
		"review":   map[string]any{"round": spec.round, "clean_round": nil},
		"evidence": evidence,
		"entities": map[string]any{"bugs": bugs, "findings": findings},
	}
	if spec.planStatus != "" {
		planRound := spec.round + spec.planRoundOffset
		claims := map[string]any{}
		for claimID, row := range spec.claims {
			claims[claimID] = map[string]any{
				"lens":          "qa",
				"applicability": valueOr(row, "applicability", "required"),
				"disposition":   valueOr(row, "disposition", "planned"),
				"assignment_id": "assignment-qa-1",
				"result_id":     row["result_id"],
				"finding_ids":   []any{},
			}
		}
		state["review"] = map[string]any{
			"round":       spec.round,
			"clean_round": nil,
			"plan": map[string]any{
				"plan_id": "review-plan-t", "path": ".claude/review/plans/review-plan-t.json",
				"sha256": "aaaa", "revision": 1, "review_round": planRound,
				"status": spec.planStatus, "e2e_coverage_state": "regression_available",
				"submitted_at": "2026-01-01T00:00:00Z",
			},
			"claims":            claims,
			"assignments":       map[string]any{},
			"observation_batch": nil,
		}
	}
	return state
}

func valueOr(row map[string]any, key, fallback string) string {
	if value, ok := row[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func passClaim(resultID string) map[string]any {
	return map[string]any{"disposition": "pass", "result_id": resultID}
}

func reviewEvidence(id, kind string, round int, status string) map[string]any {
	return map[string]any{
		"id": id, "kind": kind, "path": "evidence/" + id + ".json",
		"sha256": "abc", "status": status, "baseline_generation": float64(1),
		"review_round": float64(round), "produced_by": []any{"agent-x"},
		"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": "QA", "scope_refs": []any{},
	}
}

func targetedReverificationEvidence(id, bugID string, round int) map[string]any {
	return map[string]any{
		"id": id, "kind": "targeted_reverification", "path": "docs/reports/bugs/" + bugID + ".md#reverify",
		"sha256": "def", "status": "valid", "baseline_generation": float64(1),
		"review_round": float64(round), "produced_by": []any{"agent-x"},
		"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": "VER-REQ", "scope_refs": []any{bugID},
	}
}

// A clean round = plan closed clean + every required Claim consumed pass +
// no findings + no invalid current-round evidence + no blocking BUGs + the
// machine snapshot registered (L3-S7 §10.1).
func TestCleanRoundPassesWhenAllClaimsConsumed(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims: map[string]map[string]any{
			"claim-dv-1":  passClaim("ev-r1"),
			"claim-qa-1":  passClaim("ev-r2"),
			"claim-e2e-1": passClaim("ev-r3"),
		},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("ev-r2", "review_result", 1, "valid"),
			reviewEvidence("ev-r3", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("expected clean round to pass, got reasons: %v", result.Reasons)
	}
}

func TestCleanRoundFailsWithoutPlan(t *testing.T) {
	state := cleanState(cleanStateSpec{round: 1})
	result := verification.EvaluateCleanRound(state)
	if result.Passed {
		t.Fatal("clean round without a ReviewPlan must fail")
	}
	assertCheckFailed(t, result, "review_plan_clean")
}

func TestCleanRoundFailsWhenPlanNotClean(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "observation_sealed",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "review_plan_clean")
}

func TestCleanRoundFailsWhenClaimNotConsumed(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims: map[string]map[string]any{
			"claim-dv-1": passClaim("ev-r1"),
			"claim-qa-1": {"disposition": "running"},
		},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "all_required_claims_pass")
}

func TestCleanRoundFailsWithCurrentRoundFinding(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
		findings: []map[string]any{{
			"finding_id": "finding-1", "review_round": float64(1), "severity": "P1",
		}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "no_findings_current_round")
}

func TestCleanRoundFailsOnInvalidatedCurrentRoundEvidence(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("ev-stale", "review_result", 1, "invalid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "no_invalidated_pass_evidence")
}

// BUG-039-36 spirit: prior-round review evidence must not pollute the
// current round's evaluation.
func TestCleanRoundIgnoresPriorRoundReviewEvidence(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      2,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r2")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"), // prior round
			reviewEvidence("ev-r2", "review_result", 2, "valid"), // current
			reviewEvidence("clean-round-r2", "clean_round", 2, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("prior-round evidence must not pollute the clean round: %v", result.Reasons)
	}
}

func TestCleanRoundFailsOnOpenBlockingBug(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
		bugs: []map[string]any{{"id": "BUG-001", "severity": "P0", "state": "accepted"}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "no_open_blocking_bugs")
}

// RC-02 (L3-S7 §10.1): blocking is the explicit business judgment, never a
// severity synonym. A P1 BUG with blocking=true must block the clean round
// exactly like a P0 — severity alone cannot launder a business blocker into
// a clean round.
func TestCleanRoundFailsOnOpenP1BusinessBlockingBug(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
		bugs: []map[string]any{{
			"id": "BUG-002", "severity": "P1", "blocking": true, "state": "accepted",
		}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "no_open_blocking_bugs")
}

// The blocking flag also survives a string-typed round trip (recovery/import
// paths re-serialize flags as text).
func TestCleanRoundFailsOnOpenP2BlockingBugWithStringFlag(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
		bugs: []map[string]any{{
			"id": "BUG-003", "severity": "P2", "blocking": "true", "state": "fixing",
		}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "no_open_blocking_bugs")
}

// A closed P1 blocking BUG owes the same targeted re-verification as a P0.
func TestCleanRoundFailsWhenClosedP1BlockingBugLacksReverification(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
		bugs: []map[string]any{{
			"id": "BUG-002", "severity": "P1", "blocking": true, "state": "closed",
		}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "no_open_blocking_bugs")
}

// A non-P0 BUG without the blocking marker stays non-blocking: ordinary
// defects drain through the finding path without stopping the round.
func TestCleanRoundPassesWithNonBlockingP1Bug(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      2,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r2")},
		evidence: []map[string]any{
			reviewEvidence("ev-r2", "review_result", 2, "valid"),
			reviewEvidence("clean-round-r2", "clean_round", 2, "valid"),
		},
		bugs: []map[string]any{{
			"id": "BUG-004", "severity": "P1", "blocking": false, "state": "closed",
		}},
	})
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("non-blocking P1 BUG must not stop the clean round: %v", result.Reasons)
	}
}

func TestCleanRoundFailsWhenClosedBlockingBugLacksTargetedReverification(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
		bugs: []map[string]any{{"id": "BUG-001", "severity": "P0", "state": "closed"}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "no_open_blocking_bugs")
}

func TestCleanRoundPassesWhenBlockingBugClosedWithTargetedReverification(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      2,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r2")},
		evidence: []map[string]any{
			reviewEvidence("ev-r2", "review_result", 2, "valid"),
			reviewEvidence("clean-round-r2", "clean_round", 2, "valid"),
			targetedReverificationEvidence("ev-reverify", "BUG-001", 2),
		},
		bugs: []map[string]any{{"id": "BUG-001", "severity": "P0", "state": "closed"}},
	})
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("expected clean round with closed+reverified bug, got: %v", result.Reasons)
	}
}

func TestCleanRoundFailsAtRoundZero(t *testing.T) {
	state := cleanState(cleanStateSpec{round: 0})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "review_round_started")
}

func TestCleanRoundFailsWithoutSnapshotEvidence(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "clean_round_snapshot_registered")
}

// A stale plan (belonging to an older round) cannot close the current round.
func TestCleanRoundFailsWithStalePlan(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:           2,
		planStatus:      "clean",
		planRoundOffset: -1,
		claims:          map[string]map[string]any{"claim-qa-1": passClaim("ev-r2")},
		evidence: []map[string]any{
			reviewEvidence("ev-r2", "review_result", 2, "valid"),
			reviewEvidence("clean-round-r2", "clean_round", 2, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "review_plan_clean")
}

// N/A claims with their plan-level disposition never block the clean round.
func TestCleanRoundPassesWithNotApplicableClaims(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims: map[string]map[string]any{
			"claim-qa-1":  passClaim("ev-r1"),
			"claim-e2e-1": {"disposition": "not_applicable", "applicability": "not_applicable"},
		},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("N/A claims must not block the clean round: %v", result.Reasons)
	}
}

func assertCheckFailed(t *testing.T, result verification.Result, name string) {
	t.Helper()
	for _, check := range result.Checks {
		if check.Name == name && !check.Passed {
			return
		}
	}
	t.Fatalf("check %s did not fail; reasons: %v", name, result.Reasons)
}
