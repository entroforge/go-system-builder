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

var planningMissingTokenRules = []missingTokenRule{
	{prefix: "evidence:planning_design_record", exact: true, meaning: "the S2 design questionnaire has not been received", nextStep: "complete the design/self-review and fill architecture_path plus conclusion at the exact questionnaire path shown in this Hook, then save"},
	{prefix: "evidence:planning_contract_record", exact: true, meaning: "the S3 contract questionnaire has not been received", nextStep: "complete the contract work and professional answers at the exact questionnaire path shown in this Hook, then save"},
	{prefix: "evidence:planning_task_record", exact: true, meaning: "the S4 task-planning questionnaire has not been received", nextStep: "complete the task split and professional answers at the exact questionnaire path shown in this Hook, then save"},
}

var builderBatchMissingTokenRules = []missingTokenRule{
	{
		prefix:   "batch:execution_batch_empty",
		exact:    true,
		meaning:  "no task documents are registered at the current generation — the TR-003 execution batch is missing",
		nextStep: "Main must finish or restore the stage's registered TASK batch; do not invent task completion",
	},
	{
		prefix:   "evidence:completion_report",
		exact:    true,
		meaning:  "one or more reviewed TASKs have no qualified Builder Result; this does not prove a Builder has been prepared or started",
		nextStep: "Main follows the current TASK progress: prepare an eligible unprepared TASK with s6 prepare, dispatch or resume its exact native brief, or resolve the actual returned blocker; integrate only a qualified received delivery",
	},
	{
		prefix:   "evidence:completion_report:",
		meaning:  "the named TASK has no qualified Builder Result; its preparation and native execution must be determined from actual progress",
		nextStep: "Main prepares this TASK with s6 prepare if eligible and unprepared, otherwise dispatches or resumes its original brief or resolves the actual returned blocker; an absent Result is not proof of a running Builder",
	},
	{
		prefix:   "checks:",
		meaning:  "the completion envelope records a check that is not `pass`",
		nextStep: "resolve the reported check failure; Main assigns any required rework, then save the current Result questionnaire with the actual check outcome",
	},
	{
		prefix:   "scope_deviations:",
		meaning:  "the completion envelope declares an unapproved write-scope deviation",
		nextStep: "revise the assignment scope (new manifest row) or fix the implementation to stay inside `write_paths`",
	},
	{
		prefix:   "integration_checkpoint:",
		meaning:  "that TASK has no durable worktree integration checkpoint at `verified` or beyond",
		nextStep: "Main follows the named Assignment's exact action in the current progress context: resolve unfinished Builder work through its original rework path, or integrate eligible delivery and run required Main checks",
	},
}

// S7 exit guidance follows the original reviewer conclusions.
var reviewRoundMissingTokenRules = []missingTokenRule{
	{prefix: "review:necessary_responsibilities_unfinished_or_finding", exact: true, meaning: "necessary review responsibilities are unfinished or an accepted Finding exists", nextStep: "inspect the original conclusions and let the natural event receive pending answers"},
	{prefix: "review:finding_missing", exact: true, meaning: "no accepted Finding is available for S8", nextStep: "inspect the original reviewer conclusions"},
	{prefix: "evidence:review_result_record", exact: true, meaning: "the pause conclusion has not been received", nextStep: "save the original reviewer answer with the actual pause verdict"},
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
		fmt.Fprintf(&b, "- %s — no guidance entry; Main must resolve this stage requirement and provide its concrete work and questionnaire path.\n",
			strings.Join(unknown, ", "))
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
	case "GATE-PLANNING-DESIGN-COMPLETE", "GATE-PLANNING-CONTRACTS-COMPLETE", "GATE-PLANNING-TASKS-COMPLETE":
		return planningMissingTokenRules, true
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
