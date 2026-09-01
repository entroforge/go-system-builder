package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// runRuntimeS7BudgetDecision is the human gateway for an exhausted S7
// full-review budget. The decision file is registered as human_decision
// evidence and, for increase_budget, updates the limit in the same CAS. A
// return_to_governance decision is immediately routed through GTR-006 so the
// lifecycle does not stop at an orphaned approval record.
func runRuntimeS7BudgetDecision(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime s7-budget-decision", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime s7-budget-decision")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	decisionPath := flags.String("file", "", "human S7 budget decision JSON path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	actor := flags.String("actor", "", "human decision actor")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	missing := make([]string, 0, 2)
	if strings.TrimSpace(*decisionPath) == "" {
		missing = append(missing, "--file")
	}
	if strings.TrimSpace(*actor) == "" {
		missing = append(missing, "--actor")
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "runtime s7-budget-decision requires %s; decision must be increase_budget or return_to_governance\n", strings.Join(missing, ", "))
		return 2
	}

	decisionFile, err := s7DecisionPathRelative(*root, *decisionPath)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime s7-budget-decision", err))
		return 1
	}
	receipt, err := runtime.ApplyS7BudgetDecision(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), runtime.S7BudgetDecisionRequest{
		ExpectedRevision: *expectedRevision,
		DecisionPath:     decisionFile,
		Actor:            strings.TrimSpace(*actor),
		Validator:        semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime s7-budget-decision", err))
		return 1
	}

	result := map[string]any{
		"decision":      receipt.Decision.Decision,
		"evidence_id":   receipt.EvidenceID,
		"evidence_path": receipt.EvidencePath,
		"scope_ref":     receipt.ScopeRef,
		"revision":      receipt.Snapshot.Revision,
	}
	if receipt.Decision.Decision == runtime.S7BudgetDecisionGovernance {
		next, transitionErr := transition.Apply(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), transition.Request{
			TransitionID:     "GTR-006",
			ExpectedRevision: -1,
			Actor:            strings.TrimSpace(*actor),
			Evidence:         map[string]string{"human_decision_record": receipt.EvidenceID},
			OccurredAt:       time.Now().UTC(),
		})
		if transitionErr != nil {
			fmt.Fprintf(stderr, "runtime s7-budget-decision: decision recorded at revision %d but governance handoff is pending: %v; retry `runtime transition --id GTR-006 --actor %s --evidence human_decision_record=%s`\n", receipt.Snapshot.Revision, transitionErr, *actor, receipt.EvidenceID)
			return 1
		}
		result["revision"] = next.Revision
		result["next"] = "planning"
	} else {
		result["new_max_full_review_rounds"] = receipt.Decision.NewMaxFullReviewRounds
		pending := ""
		lifecycle, _ := receipt.Snapshot.State["lifecycle"].(map[string]any)
		stateName, _ := lifecycle["state"].(string)
		if stateName == "bug_resolution" {
			pending = "TR-012"
		} else if stateName == "acceptance" {
			pending = "TR-016"
		}
		result["pending_transition"] = pending
		result["next"] = fmt.Sprintf("the next PreToolUse retries %s automatically; do not revive old agents or hand-push the transition", pending)
	}
	return encodeJSON(stdout, result)
}

func s7DecisionPathRelative(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return path, nil
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("resolve S7 budget decision path: %w", err)
	}
	return relative, nil
}

func s7BudgetGateRequired(state map[string]any) bool {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	stateName, _ := lifecycle["state"].(string)
	phase, _ := lifecycle["phase"].(string)
	if stateName != "acceptance" && !(stateName == "bug_resolution" && phase == "ready_for_full_review") {
		return false
	}
	review, _ := state["review"].(map[string]any)
	round := integerValue(review["round"])
	configuration, _ := state["configuration"].(map[string]any)
	repair, _ := configuration["repair"].(map[string]any)
	maxRounds := integerValue(repair["max_full_review_rounds"])
	return maxRounds > 0 && round >= maxRounds
}

func s7MaxRounds(state map[string]any) int {
	configuration, _ := state["configuration"].(map[string]any)
	repair, _ := configuration["repair"].(map[string]any)
	return integerValue(repair["max_full_review_rounds"])
}

func applyS7BudgetGateway(next *nextProjection, state map[string]any) {
	if !s7BudgetGateRequired(state) {
		return
	}
	next.Stage = "S7"
	next.ProtocolRef = "docs/agent-protocol.md#s7"
	next.Objective = "obtain the human decision for an exhausted S7 full-review budget"
	next.Action = "stop automation and submit `runtime s7-budget-decision --file <decision.json> --actor <user>` with increase_budget or return_to_governance"
	next.PrimarySkill = PrimarySkillS7
	next.Missing = []string{"s7_budget_decision"}
	next.DoneWhen = []string{"the decision is recorded in Runtime evidence", "increase_budget updates max_full_review_rounds or return_to_governance routes to planning"}
	next.HumanRequired = true
}
