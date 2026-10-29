package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
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
	if err := parseWorkspaceFlags(flags, args); err != nil {
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
		result["next"] = fmt.Sprintf("budget recorded; the original pending %s consumer must continue through its actual save/recovery outlet; ordinary PreToolUse reads do not retry it or receive an old answer", pending)
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

func s7BudgetGateRequired(state map[string]any, roots ...string) bool {
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
	if maxRounds < 1 || round < maxRounds {
		return false
	}
	if stateName != "acceptance" {
		return true
	}
	// Merely finishing the final allowed clean round is not a request for
	// another round. Acceptance may still pass normally. Read only its latest
	// existing current-scope receipt, without rerunning gates or Git chains.
	if len(roots) == 0 || roots[0] == "" {
		return false
	}
	needed, _ := s7AcceptedReviewRequiresNewRound(roots[0], state)
	return needed
}

func s7AcceptedReviewRequiresNewRound(root string, state map[string]any) (bool, error) {
	round := integerValue(nestedStateValue(state, "review", "round"))
	rows := stateEvidence(state)
	for i := len(rows) - 1; i >= 0; i-- {
		row, _ := rows[i].(map[string]any)
		if row["kind"] != "acceptance" || row["status"] != "valid" || row["invalidated_by"] != nil ||
			integerValue(row["baseline_generation"]) != integerValue(nestedStateValue(state, "baseline", "generation")) || integerValue(row["review_round"]) != round {
			continue
		}
		data, err := workpacket.ReadFile(root, stringValue(row["path"]), 20<<20)
		if err != nil || workpacket.Digest(data) != row["sha256"] {
			return false, fmt.Errorf("original accepted acceptance source %s is missing or changed; Main/system restores its exact receipt before a budget decision: %v", stringValue(row["path"]), err)
		}
		var accepted struct {
			RuntimeID  string `json:"runtime_id"`
			Conclusion string `json:"conclusion"`
			Generation int    `json:"baseline_generation"`
			Round      int    `json:"review_round"`
		}
		if json.Unmarshal(data, &accepted) != nil || accepted.RuntimeID != state["runtime_id"] || accepted.Generation != integerValue(nestedStateValue(state, "baseline", "generation")) || accepted.Round != round {
			return false, fmt.Errorf("original accepted acceptance source %s has different Runtime/generation/round bindings; Main/system restores its original source", stringValue(row["path"]))
		}
		return accepted.Conclusion == "review_required", nil
	}
	return false, nil
}

func s7MaxRounds(state map[string]any) int {
	configuration, _ := state["configuration"].(map[string]any)
	repair, _ := configuration["repair"].(map[string]any)
	return integerValue(repair["max_full_review_rounds"])
}

func applyS7BudgetGateway(next *nextProjection, state map[string]any, roots ...string) {
	if !s7BudgetGateRequired(state, roots...) {
		return
	}
	next.Stage = "S7"
	next.ProtocolRef = "docs/control/agent-protocol.md#s7"
	next.Objective = "obtain the human decision for an exhausted S7 full-review budget"
	next.Action = "read the exact prepared S7 budget decision application; after the human actually chooses, Main fills only decision, the genuinely increased maximum when applicable, and reason, then saves that same application"
	if len(roots) > 0 {
		if path, _, err := s7BudgetDecisionApplicationForState(roots[0], state); err == nil {
			next.Read = append(next.Read, path)
		}
	}
	next.PrimarySkill = PrimarySkillS7
	next.Missing = []string{"s7_budget_decision"}
	next.DoneWhen = []string{"the decision is recorded in Runtime evidence", "increase_budget updates max_full_review_rounds or return_to_governance routes to planning"}
	next.HumanRequired = true
}
