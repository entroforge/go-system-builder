package missingtokens

import (
	"fmt"
	"sort"
	"strings"
)

// missingTokenLegend maps GATE-BUILDER-BATCH-READY missing tokens to their
// meaning and the next executable action (L3-S6 §9.3 — error messages are
// process orchestration). The legend is shared by the Hook's not_ready
// recovery packet and `explain TR-006` so the agent reads one vocabulary
// wherever the tokens appear.
type missingTokenRule struct {
	prefix   string // token equality when exact, otherwise prefix match
	exact    bool
	meaning  string
	nextStep string
}

var builderBatchMissingTokenRules = []missingTokenRule{
	{
		prefix:   "batch:execution_batch_empty",
		exact:    true,
		meaning:  "no task documents are registered at the current generation — the TR-003 execution batch is missing",
		nextStep: "check whether TR-003 committed; run `loop-harness runtime reconcile` if the state is inconsistent",
	},
	{
		prefix:   "evidence:completion_report",
		exact:    true,
		meaning:  "no qualified completion envelope exists at all",
		nextStep: "run `runtime task-complete` for each unfinished TASK in the batch",
	},
	{
		prefix:   "evidence:completion_report:",
		meaning:  "that TASK has no Builder Result envelope",
		nextStep: "run `runtime task-complete` for the named TASK",
	},
	{
		prefix:   "checks:",
		meaning:  "the completion envelope records a check that is not `pass`",
		nextStep: "fix the failure and re-run `runtime task-complete`; the newer envelope supersedes the older one",
	},
	{
		prefix:   "scope_deviations:",
		meaning:  "the completion envelope declares an unapproved write-scope deviation",
		nextStep: "revise the assignment scope (new manifest row) or fix the implementation to stay inside `write_paths`",
	},
	{
		prefix:   "integration_checkpoint:",
		meaning:  "that TASK has no durable worktree integration checkpoint at `verified` or beyond",
		nextStep: "run `runtime task-integrate --assignment-id <id>` for that assignment (or re-trigger the Builder's SubagentStop)",
	},
}

// reviewRoundMissingTokenRules maps the S7 exit gates' missing tokens to
// their meaning and next action (L3-S7 §10/§3.7). The tokens are produced by
// the qualitygate evaluator's ReviewPlan exact-set checks.
var reviewRoundMissingTokenRules = []missingTokenRule{
	{
		prefix:   "cleanround:review_round_started",
		exact:    true,
		meaning:  "no review round is open",
		nextStep: "enter S7 via TR-006 (or TR-012 after repair) before anything else",
	},
	{
		prefix:   "cleanround:review_plan_clean",
		exact:    true,
		meaning:  "the ReviewPlan is missing, stale, or not closed clean",
		nextStep: "run `loop-harness s7 status`; register the plan via `runtime review-plan` or finish consuming results",
	},
	{
		prefix:   "cleanround:all_required_claims_pass",
		exact:    true,
		meaning:  "some required Claims lack a consumed pass Result",
		nextStep: "run `loop-harness s7 status` for the pending Claims, then `runtime review-result submit` for each assignment",
	},
	{
		prefix:   "cleanround:no_findings_current_round",
		exact:    true,
		meaning:  "current-round Findings foreclose the clean path",
		nextStep: "the round must go to S8 via TR-008 with the sealed ObservationBatch; clean is unreachable this round",
	},
	{
		prefix:   "cleanround:no_invalidated_pass_evidence",
		exact:    true,
		meaning:  "a current-round review evidence entry is invalid",
		nextStep: "inspect the invalidated evidence; a drifted baseline makes the round stale — start a new round",
	},
	{
		prefix:   "cleanround:no_open_blocking_bugs",
		exact:    true,
		meaning:  "an open blocking BUG (or a closed one missing targeted re-verification) blocks the clean round",
		nextStep: "route through S8/S9 and re-enter via TR-012",
	},
	{
		prefix:   "cleanround:clean_round_snapshot_registered",
		exact:    true,
		meaning:  "the machine CleanRound snapshot is not registered",
		nextStep: "the round consumer writes it inside the final `runtime review-result submit`; check `loop-harness s7 status`",
	},
	{
		prefix:   "batch:review_plan_missing",
		exact:    true,
		meaning:  "no ReviewPlan is registered for this round",
		nextStep: "register one via `runtime review-plan --file <plan.json>`",
	},
	{
		prefix:   "batch:plan_status=",
		meaning:  "the ReviewPlan has not reached observation_sealed",
		nextStep: "consume the remaining ReviewResults; the batch seals automatically when the final required Claim lands (P0 seals immediately)",
	},
	{
		prefix:   "batch:observation_batch_not_sealed",
		exact:    true,
		meaning:  "no sealed ObservationBatch exists for the current round",
		nextStep: "complete the required Claims; the round consumer seals the batch in the final submit transaction",
	},
	{
		prefix:   "batch:finding_set:",
		meaning:  "the sealed batch's finding set diverges from the current-round Finding entities",
		nextStep: "do not edit state by hand; run `runtime reconcile` and re-seal via a corrected result submit",
	},
	{
		prefix:   "batch:unobserved_claim:",
		meaning:  "an ordinary batch sealed while a required Claim was never observed",
		nextStep: "only a P0 immediate-stop batch may carry safety gaps; submit the missing Claim results first",
	},
	{
		prefix:   "evidence:observation_batch_record",
		exact:    true,
		meaning:  "no sealed ObservationBatch evidence is bound",
		nextStep: "TR-008 binds `observation_batch_record`; the sealed batch registers it automatically",
	},
	{
		prefix:   "evidence:clean_round_record",
		exact:    true,
		meaning:  "no machine CleanRound evidence is bound",
		nextStep: "TR-009 binds `clean_round_record`; the round consumer registers it when the final Claim passes",
	},
	{
		prefix:   "evidence:review_result_record",
		exact:    true,
		meaning:  "no ReviewResult with the pause verdict is registered for this round — normal while discovery is healthy; this gate only fires on a pause verdict",
		nextStep: "nothing to do for ordinary work; only when a Reviewer concludes the REQ/release must change, submit with verdict req_change_required / release_blocked via `runtime review-result submit`",
	},
}

// RenderMissingTokenLegend returns a compact legend block for the missing
// tokens of one gate evaluation. Empty when nothing matches (unknown tokens
// are listed verbatim with a generic fallback line so silence never hides a
// new token shape).
func RenderMissingTokenLegend(gateID string, missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	var rules []missingTokenRule
	rules, hasRules := gateRules(gateID)
	if !hasRules {
		return ""
	}
	var b strings.Builder
	b.WriteString("MISSING TOKENS:\n")
	covered := make(map[string]bool)
	for _, token := range missing {
		rule, ok := matchMissingTokenRule(rules, token)
		if !ok {
			continue
		}
		if covered[rule.prefix] {
			continue
		}
		covered[rule.prefix] = true
		fmt.Fprintf(&b, "- %s — %s. Next: %s.\n", legendTokenLabel(rule, token), rule.meaning, rule.nextStep)
	}
	unknown := unknownTokens(rules, missing)
	if len(unknown) > 0 {
		sort.Strings(unknown)
		fmt.Fprintf(&b, "- %s — no legend entry; inspect the gate implementation or run `loop-harness explain %s`.\n",
			strings.Join(unknown, ", "), gateID)
	}
	return strings.TrimRight(b.String(), "\n")
}

func matchMissingTokenRule(rules []missingTokenRule, token string) (missingTokenRule, bool) {
	for _, rule := range rules {
		if rule.exact {
			if token == rule.prefix {
				return rule, true
			}
			continue
		}
		if strings.HasPrefix(token, rule.prefix) {
			return rule, true
		}
	}
	return missingTokenRule{}, false
}

// RenderGateTokenLegend prints the full token table for one gate — used by
// `explain <transition>` where no evaluation exists yet, so the agent can
// read the vocabulary before the first not_ready packet.
func RenderGateTokenLegend(gateID string) string {
	rules, ok := gateRules(gateID)
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "GATE MISSING-TOKEN LEGEND (%s):\n", gateID)
	for _, rule := range rules {
		fmt.Fprintf(&b, "- %s — %s. Next: %s.\n", legendTokenLabel(rule, rule.prefix), rule.meaning, rule.nextStep)
	}
	return strings.TrimRight(b.String(), "\n")
}

func gateRules(gateID string) ([]missingTokenRule, bool) {
	switch gateID {
	case "GATE-BUILDER-BATCH-READY":
		return builderBatchMissingTokenRules, true
	case "GATE-VERIFY-CLEAN-ROUND-PASSED", "GATE-VERIFY-BLOCKING-FINDING",
		"GATE-VERIFY-REQ-CHANGE-REQUIRED", "GATE-VERIFY-RELEASE-BLOCKED":
		return reviewRoundMissingTokenRules, true
	}
	return nil, false
}

// legendTokenLabel keeps one line per rule family: the bare prefix for the
// per-TASK families (the specific TASK rides in the evaluation's token
// list) and the exact token otherwise.
func legendTokenLabel(rule missingTokenRule, token string) string {
	if rule.exact {
		return "`" + token + "`"
	}
	return "`" + rule.prefix + "<id>`"
}

func unknownTokens(rules []missingTokenRule, missing []string) []string {
	var unknown []string
	for _, token := range missing {
		if _, ok := matchMissingTokenRule(rules, token); !ok {
			unknown = append(unknown, token)
		}
	}
	return unknown
}
