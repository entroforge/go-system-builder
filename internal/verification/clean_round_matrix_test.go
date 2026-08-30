package verification_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/verification"
)

// ---------------------------------------------------------------------------
// §14.1 CleanRound matrix additions: round isolation against stale/foreign
// evidence and the "reverify alone never substitutes the full Claim set"
// conjunction.
// ---------------------------------------------------------------------------

// 旧轮 invalid evidence 不污染当前 exact set：一份 round-1 的 invalid
// review_result 不得让 round-2 的 clean round 失败。
func TestCleanRoundIgnoresInvalidPriorRoundEvidence(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      2,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r2")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "invalid"), // stale prior round
			reviewEvidence("ev-r2", "review_result", 2, "valid"),
			reviewEvidence("clean-round-r2", "clean_round", 2, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("invalid prior-round evidence must not pollute the current round: %v", result.Reasons)
	}
}

// 无关 evidence 不污染：当前轮里一份 invalid 的 completion_report（非
// review evidence kind）不参与 no_invalidated_pass_evidence 判定。
func TestCleanRoundIgnoresUnrelatedInvalidEvidenceKind(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims:     map[string]map[string]any{"claim-qa-1": passClaim("ev-r1")},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("ev-build-1", "completion_report", 1, "invalid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		t.Fatalf("unrelated invalid evidence kinds must not block the clean round: %v", result.Reasons)
	}
}

// targeted reverify 通过但 full Claims 未跑 —— CleanRound 不通过：闭环 BUG
// 的针对性复验永远不能替代完整 Claim set 的 exact-set 判定。
func TestCleanRoundFailsWhenReverifyPresentButClaimsNotFullyRun(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      2,
		planStatus: "clean",
		claims: map[string]map[string]any{
			"claim-qa-1": passClaim("ev-r2"),
			"claim-qa-2": {"disposition": "planned"}, // full Claim set not run
		},
		evidence: []map[string]any{
			reviewEvidence("ev-r2", "review_result", 2, "valid"),
			reviewEvidence("clean-round-r2", "clean_round", 2, "valid"),
			targetedReverificationEvidence("ev-reverify", "BUG-001", 2),
		},
		bugs: []map[string]any{{"id": "BUG-001", "severity": "P0", "state": "closed"}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "all_required_claims_pass")
}

// valid evidence 但 conclusion 非 pass：claim 的 disposition 是 finding
// （Result 有效但结论是 fail）时 CleanRound 不通过。
func TestCleanRoundFailsWhenConclusionIsNotPass(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims: map[string]map[string]any{
			"claim-qa-1": {"disposition": "finding", "result_id": "ev-r1"},
		},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
		findings: []map[string]any{{
			"finding_id": "finding-1", "review_round": float64(1), "severity": "P1",
		}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "all_required_claims_pass")
	assertCheckFailed(t, result, "no_findings_current_round")
}

// pass disposition without a consumed Result reference is not a pass.
func TestCleanRoundFailsWhenPassClaimLacksResultRef(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims: map[string]map[string]any{
			"claim-qa-1": {"disposition": "pass"}, // no result_id
		},
		evidence: []map[string]any{
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "all_required_claims_pass")
}

// §14.1: blocked 不是 PASS。被 confirmed Finding 阻断的 Claim（disposition
// blocked，即便绑定了有效 Result）不满足 exact-set 的 pass 要求，CleanRound
// 不通过；修复后的新轮仍欠该 Claim 一次真实执行（L3-S7 §3.5/§10.1）。
func TestCleanRoundFailsWhenClaimBlockedByConfirmedFinding(t *testing.T) {
	state := cleanState(cleanStateSpec{
		round:      1,
		planStatus: "clean",
		claims: map[string]map[string]any{
			"claim-qa-1":  passClaim("ev-r1"),
			"claim-e2e-1": {"disposition": "blocked", "result_id": "ev-r2"},
		},
		evidence: []map[string]any{
			reviewEvidence("ev-r1", "review_result", 1, "valid"),
			reviewEvidence("ev-r2", "review_result", 1, "valid"),
			reviewEvidence("clean-round-r1", "clean_round", 1, "valid"),
		},
		findings: []map[string]any{{
			"finding_id": "finding-1", "review_round": float64(1), "severity": "P1",
		}},
	})
	result := verification.EvaluateCleanRound(state)
	assertCheckFailed(t, result, "all_required_claims_pass")
	assertCheckFailed(t, result, "no_findings_current_round")
}
