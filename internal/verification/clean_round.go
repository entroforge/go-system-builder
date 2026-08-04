// Package verification evaluates the clean-round gate.
//
// A clean round is the precondition for acceptance (TR-009) and release audit
// (TR-015). Per docs/loop-definition.json the
// PTR-VERIFY-04 transition requires all four of:
//
//   - same_review_round
//   - all_required_dimensions_passed
//   - no_invalidated_pass_evidence
//   - no_open_blocking_bugs
//
// This package implements that evaluation as a pure function over the runtime
// state, so the same logic is usable by the transition engine, the Hook policy
// engine, and the `loop-harness verification clean-round` CLI.
package verification

import (
	"fmt"

	"github.com/entroforge/go-system-builder/internal/impact"
)

// Result is the outcome of a clean-round evaluation.
type Result struct {
	Passed      bool          `json:"passed"`
	ReviewRound int           `json:"review_round"`
	Checks      []CheckResult `json:"checks"`
	Reasons     []string      `json:"reasons,omitempty"`
}

// CheckResult records the outcome of one of the four guard checks.
type CheckResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// EvaluateCleanRound inspects the runtime state and decides whether the current
// review round satisfies all four clean-round guards. The state is the decoded
// loop-state.json object. The function never mutates state.
func EvaluateCleanRound(state map[string]any) Result {
	currentRound := readReviewRound(state)
	result := Result{ReviewRound: currentRound}

	checkActiveRound := CheckResult{Name: "review_round_started", Passed: currentRound >= 1}
	if !checkActiveRound.Passed {
		checkActiveRound.Detail = "clean round requires review round >= 1"
	}
	result.Checks = append(result.Checks, checkActiveRound)

	// Check 1: same_review_round — every clean-round-relevant (verification-
	// phase) evidence item under consideration must belong to the current
	// review round. Historical building/planning evidence from earlier rounds
	// is not under consideration (BUG-039-36).
	checkRound := CheckResult{Name: "same_review_round", Passed: true}
	for _, entry := range evidenceEntries(state) {
		if !isCleanRoundRelevantEvidence(entry) {
			continue
		}
		evRound := readInt(entry["review_round"])
		if evRound != 0 && evRound != currentRound {
			checkRound.Passed = false
			id, _ := entry["id"].(string)
			checkRound.Detail = fmt.Sprintf(
				"evidence %s belongs to round %d, current round is %d",
				id, evRound, currentRound)
			break
		}
	}
	result.Checks = append(result.Checks, checkRound)

	// Check 2: all_required_dimensions_passed — every responsibility listed in
	// any registered team manifest must have a corresponding PASS evidence
	// entry in the current round.
	checkDims := CheckResult{Name: "all_required_dimensions_passed", Passed: true}
	if missingKinds := missingReviewWorkgroups(state); len(missingKinds) > 0 {
		checkDims.Passed = false
		checkDims.Detail = fmt.Sprintf("missing review workgroups: %v", missingKinds)
		result.Checks = append(result.Checks, checkDims)
	} else {
		required := collectRequiredResponsibilities(state)
		if len(required) == 0 {
			checkDims.Passed = false
			checkDims.Detail = "no registered review responsibilities"
		} else if missing := missingResponsibilityEvidence(state, currentRound, required); len(missing) > 0 {
			checkDims.Passed = false
			checkDims.Detail = fmt.Sprintf(
				"responsibilities without PASS evidence: %v", missing)
		}
		result.Checks = append(result.Checks, checkDims)
	}

	// Check 3: no_invalidated_pass_evidence — no evidence with status invalid
	// may be referenced as a PASS in the current round. We approximate this by
	// checking that no evidence in the current round has status invalid. A
	// stricter check would walk impact.ComputeImpact, but the runtime already
	// carries the validity status on each entry.
	checkInvalid := CheckResult{Name: "no_invalidated_pass_evidence", Passed: true}
	for _, entry := range evidenceEntries(state) {
		if readInt(entry["review_round"]) != currentRound {
			continue
		}
		if status, _ := entry["status"].(string); status == "invalid" {
			checkInvalid.Passed = false
			id, _ := entry["id"].(string)
			checkInvalid.Detail = fmt.Sprintf(
				"evidence %s is invalid but belongs to the current round", id)
			break
		}
	}
	result.Checks = append(result.Checks, checkInvalid)

	// Check 4: no_open_blocking_bugs — no BUG entity may be in a state that
	// blocks the round (accepted / assigned / fixing / retesting).
	checkBugs := CheckResult{Name: "no_open_blocking_bugs", Passed: true}
	openBugs := openBlockingBugs(state)
	if len(openBugs) > 0 {
		checkBugs.Passed = false
		checkBugs.Detail = fmt.Sprintf("open blocking bugs: %v", openBugs)
	} else if missing := closedBugsMissingTargetedEvidence(state, currentRound); len(missing) > 0 {
		checkBugs.Passed = false
		checkBugs.Detail = fmt.Sprintf("closed blocking bugs without targeted re-verification: %v", missing)
	}
	result.Checks = append(result.Checks, checkBugs)

	// Aggregate. The round passes only when every check passed.
	result.Passed = true
	for _, check := range result.Checks {
		if !check.Passed {
			result.Passed = false
			result.Reasons = append(result.Reasons,
				fmt.Sprintf("%s: %s", check.Name, check.Detail))
		}
	}
	return result
}

// isCleanRoundRelevantEvidence reports whether an evidence entry is under
// consideration for same_review_round. Building/planning/document evidence
// from earlier lifecycle phases must not veto a verification clean round.
func isCleanRoundRelevantEvidence(entry map[string]any) bool {
	kind, _ := entry["kind"].(string)
	switch kind {
	case "delivery_review", "qa_review", "e2e_review", "clean_round",
		"angle_declaration", "team_manifest", "targeted_reverification":
		return true
	case "":
		// Untyped evidence with a responsibility_id remains under consideration
		// (legacy fixtures / SM-014 mixed-round cases).
		id, _ := entry["responsibility_id"].(string)
		return id != ""
	default:
		return false
	}
}

func missingReviewWorkgroups(state map[string]any) []string {
	required := map[string]bool{
		"delivery_verifier": false,
		"qa":                false,
		"e2e_browser":       false,
	}
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return []string{"delivery_verifier", "qa", "e2e_browser"}
	}
	teams, ok := entities["teams"].([]any)
	if !ok {
		return []string{"delivery_verifier", "qa", "e2e_browser"}
	}
	for _, raw := range teams {
		team, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := team["kind"].(string)
		if _, ok := required[kind]; ok {
			required[kind] = true
		}
	}
	var missing []string
	for _, kind := range []string{"delivery_verifier", "qa", "e2e_browser"} {
		if !required[kind] {
			missing = append(missing, kind)
		}
	}
	return missing
}

// evidenceEntries returns the evidence array as a slice of maps.
func evidenceEntries(state map[string]any) []map[string]any {
	raw, ok := state["evidence"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

// missingResponsibilityEvidence collects responsibility IDs declared by team
// manifests that have no valid PASS evidence in the current round.
func missingResponsibilityEvidence(state map[string]any, currentRound int, required []string) []string {
	covered := map[string]bool{}
	for _, entry := range evidenceEntries(state) {
		if readInt(entry["review_round"]) != currentRound {
			continue
		}
		if status, _ := entry["status"].(string); status != "valid" {
			continue
		}
		if id, _ := entry["responsibility_id"].(string); id != "" {
			covered[id] = true
		}
	}
	var missing []string
	for _, id := range required {
		if !covered[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

// collectRequiredResponsibilities walks the registered team manifests in the
// runtime and returns the union of their responsibility IDs.
func collectRequiredResponsibilities(state map[string]any) []string {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return nil
	}
	teams, ok := entities["teams"].([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var ordered []string
	for _, raw := range teams {
		team, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if values, ok := team["responsibility_ids"].([]any); ok {
			for _, value := range values {
				id, _ := value.(string)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				ordered = append(ordered, id)
			}
			continue
		}
		if manifest, ok := team["manifest_ref"].(map[string]any); ok {
			assignments, _ := manifest["assignments"].([]any)
			for _, assignment := range assignments {
				a, ok := assignment.(map[string]any)
				if !ok {
					continue
				}
				id, _ := a["responsibility_id"].(string)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				ordered = append(ordered, id)
			}
		}
	}
	return ordered
}

// openBlockingBugs returns the IDs of P0 BUG entities in a blocking state.
func openBlockingBugs(state map[string]any) []string {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return nil
	}
	bugs, ok := entities["bugs"].([]any)
	if !ok {
		return nil
	}
	blockingStates := map[string]bool{
		"accepted":         true,
		"assigned":         true,
		"fixing":           true,
		"retesting":        true,
		"investigating":    true,
		"pending_approval": true,
	}
	var open []string
	for _, raw := range bugs {
		bug, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if !isBlockingBug(bug) {
			continue
		}
		stateName, _ := bug["state"].(string)
		if blockingStates[stateName] {
			if id, _ := bug["id"].(string); id != "" {
				open = append(open, id)
			}
		}
	}
	return open
}

func closedBugsMissingTargetedEvidence(state map[string]any, currentRound int) []string {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return nil
	}
	bugs, ok := entities["bugs"].([]any)
	if !ok {
		return nil
	}
	var missing []string
	for _, raw := range bugs {
		bug, ok := raw.(map[string]any)
		if !ok || !isBlockingBug(bug) {
			continue
		}
		if stateName, _ := bug["state"].(string); stateName != "closed" {
			continue
		}
		id, _ := bug["id"].(string)
		if id != "" && !hasTargetedReverificationEvidence(state, currentRound, id) {
			missing = append(missing, id)
		}
	}
	return missing
}

func isBlockingBug(bug map[string]any) bool {
	severity, _ := bug["severity"].(string)
	return severity == "P0"
}

func hasTargetedReverificationEvidence(state map[string]any, currentRound int, bugID string) bool {
	for _, entry := range evidenceEntries(state) {
		if readInt(entry["review_round"]) != currentRound {
			continue
		}
		if kind, _ := entry["kind"].(string); kind != "targeted_reverification" {
			continue
		}
		if status, _ := entry["status"].(string); status != "valid" {
			continue
		}
		for _, raw := range readStringSlice(entry["scope_refs"]) {
			if raw == bugID {
				return true
			}
		}
		if path, _ := entry["path"].(string); containsBugID(path, bugID) {
			return true
		}
	}
	return false
}

func readStringSlice(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, _ := item.(string); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func containsBugID(text, bugID string) bool {
	if bugID == "" {
		return false
	}
	for i := 0; i+len(bugID) <= len(text); i++ {
		if text[i:i+len(bugID)] == bugID {
			return true
		}
	}
	return false
}

func readReviewRound(state map[string]any) int {
	review, ok := state["review"].(map[string]any)
	if !ok {
		return 0
	}
	return readInt(review["round"])
}

func readInt(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// AffectedByChange is a convenience helper for the Hook policy engine: it
// reports whether any currently-valid evidence would be invalidated by the
// supplied changed paths. Hook predicates use this to detect
// impact_analysis_pending.
func AffectedByChange(state map[string]any, changedPaths []string) bool {
	impacts := impact.ComputeImpact(state, changedPaths)
	for _, item := range impacts {
		if !item.AlreadyInvalid {
			return true
		}
	}
	return false
}
