// Package verification evaluates the machine CleanRound.
//
// Post-S7-remediation (L3-S7 §10): a clean round is no longer a set of
// per-lens aggregate PASS envelopes. It is a strict conjunction computed
// over the current ReviewPlan's exact Claim set:
//
//   - a ReviewPlan is registered for the current round and closed clean;
//   - every required Claim has a consumed pass Result (N/A Claims carry a
//     plan-level disposition with source and rationale);
//   - no Finding exists for the current round;
//   - no current-round review evidence has been invalidated;
//   - no business-blocking BUG is open (and closed blockers carry targeted
//     re-verification);
//   - the machine CleanRound snapshot is registered as evidence.
//
// The function is pure over the runtime state, so the transition engine,
// the Quality Gate evaluator and the CLI all share one implementation.
package verification

import (
	"fmt"
	"sort"

	"github.com/entroforge/go-system-builder/internal/impact"
	"github.com/entroforge/go-system-builder/internal/review"
)

// Result is the outcome of a clean-round evaluation.
type Result struct {
	Passed      bool          `json:"passed"`
	ReviewRound int           `json:"review_round"`
	Checks      []CheckResult `json:"checks"`
	Reasons     []string      `json:"reasons,omitempty"`
}

// CheckResult records the outcome of one guard check.
type CheckResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// EvaluateCleanRound inspects the runtime state and decides whether the
// current review round is a valid machine CleanRound (L3-S7 §10.1). The
// state is the decoded loop-state.json object. The function never mutates
// state.
func EvaluateCleanRound(state map[string]any) Result {
	currentRound := readReviewRound(state)
	result := Result{ReviewRound: currentRound}

	checkActiveRound := CheckResult{Name: "review_round_started", Passed: currentRound >= 1}
	if !checkActiveRound.Passed {
		checkActiveRound.Detail = "clean round requires review round >= 1"
	}
	result.Checks = append(result.Checks, checkActiveRound)

	// Check 2: the ReviewPlan for this round exists and closed clean. The
	// consumer writes status=clean only when the final required Claim lands
	// without findings, so a missing/wrong-status plan is not cleanable.
	checkPlan := CheckResult{Name: "review_plan_clean", Passed: false}
	ptr := review.PlanPointerFromState(state)
	switch {
	case ptr == nil:
		checkPlan.Detail = "no ReviewPlan registered for this round"
	case ptr.ReviewRound != currentRound:
		checkPlan.Detail = fmt.Sprintf("ReviewPlan belongs to round %d, current round is %d", ptr.ReviewRound, currentRound)
	case ptr.Status != "clean":
		checkPlan.Detail = fmt.Sprintf("ReviewPlan status is %s, not clean", ptr.Status)
	default:
		checkPlan.Passed = true
	}
	result.Checks = append(result.Checks, checkPlan)

	// Check 3: all_required_claims_pass — the exact-set evaluation. Every
	// required Claim needs disposition=pass with a consumed Result; N/A
	// Claims keep their plan-level disposition.
	checkClaims := CheckResult{Name: "all_required_claims_pass", Passed: true}
	dispositions := review.Dispositions(state)
	if len(dispositions) == 0 {
		checkClaims.Passed = false
		checkClaims.Detail = "no claim dispositions registered"
	} else {
		var unproven []string
		for claimID, disp := range dispositions {
			if disp.Applicability == "not_applicable" {
				if disp.Disposition != "not_applicable" {
					unproven = append(unproven, claimID+"(n/a-disposition="+disp.Disposition+")")
				}
				continue
			}
			if disp.Disposition != "pass" || disp.ResultID == "" {
				unproven = append(unproven, claimID+"("+disp.Disposition+")")
			}
		}
		if len(unproven) > 0 {
			sort.Strings(unproven)
			checkClaims.Passed = false
			checkClaims.Detail = fmt.Sprintf("claims without a consumed pass: %v", unproven)
		}
	}
	result.Checks = append(result.Checks, checkClaims)

	// Check 4: no_findings_current_round — a confirmed Finding forecloses the
	// clean path for the round regardless of later pass Results.
	checkFindings := CheckResult{Name: "no_findings_current_round", Passed: true}
	if findings := review.RoundFindings(state); len(findings) > 0 {
		ids := make([]string, 0, len(findings))
		for _, row := range findings {
			if id, ok := row["finding_id"].(string); ok {
				ids = append(ids, id)
			}
		}
		checkFindings.Passed = false
		checkFindings.Detail = fmt.Sprintf("current-round findings exist: %v", ids)
	}
	result.Checks = append(result.Checks, checkFindings)

	// Check 5: no_invalidated_pass_evidence — no current-round review
	// evidence (result/finding/batch/clean snapshot) may be invalid.
	checkInvalid := CheckResult{Name: "no_invalidated_pass_evidence", Passed: true}
	for _, entry := range evidenceEntries(state) {
		if readInt(entry["review_round"]) != currentRound {
			continue
		}
		kind, _ := entry["kind"].(string)
		switch kind {
		case "review_result", "finding", "observation_batch", "clean_round":
		default:
			continue
		}
		if status, _ := entry["status"].(string); status == "invalid" {
			checkInvalid.Passed = false
			id, _ := entry["id"].(string)
			checkInvalid.Detail = fmt.Sprintf("evidence %s is invalid but belongs to the current round", id)
			break
		}
	}
	result.Checks = append(result.Checks, checkInvalid)

	// Check 6: no_open_blocking_bugs (unchanged semantics; L3-S7 §10.1.9-10).
	checkBugs := CheckResult{Name: "no_open_blocking_bugs", Passed: true}
	if openBugs := openBlockingBugs(state); len(openBugs) > 0 {
		checkBugs.Passed = false
		checkBugs.Detail = fmt.Sprintf("open blocking bugs: %v", openBugs)
	} else if missing := closedBugsMissingTargetedEvidence(state, currentRound); len(missing) > 0 {
		checkBugs.Passed = false
		checkBugs.Detail = fmt.Sprintf("closed blocking bugs without targeted re-verification: %v", missing)
	}
	result.Checks = append(result.Checks, checkBugs)

	// Check 7: the machine CleanRound snapshot exists as current-round
	// evidence. TR-009 binds this record; an agent hand-written aggregate is
	// not a substitute (L3-S7 §10.2).
	checkSnapshot := CheckResult{Name: "clean_round_snapshot_registered", Passed: false}
	for _, entry := range evidenceEntries(state) {
		kind, _ := entry["kind"].(string)
		if kind != "clean_round" || readInt(entry["review_round"]) != currentRound {
			continue
		}
		if status, _ := entry["status"].(string); status == "valid" {
			checkSnapshot.Passed = true
			break
		}
	}
	if !checkSnapshot.Passed {
		checkSnapshot.Detail = "no valid clean_round evidence for the current round"
	}
	result.Checks = append(result.Checks, checkSnapshot)

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

// openBlockingBugs returns the IDs of business-blocking BUG entities (P0, or
// any severity with blocking=true) in a blocking state.
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

// isBlockingBug decides whether a BUG entity blocks the clean round
// (RC-02, L3-S7 §10.1): blocking is the explicit business judgment, never a
// severity synonym. A BUG blocks when
//
//   - severity is P0 (implicit blocking=true, backward compatible), or
//   - the blocking field reads true (bool, or the string "true" from
//     hand-recovered/re-imported state).
//
// A non-P0 BUG with blocking=true blocks exactly like a P0; a non-P0 BUG
// without the marker does not. The stored entity is map[string]any, so the
// flag is read defensively (bool / string forms) rather than via a typed
// struct — the same pattern as bug_lifecycle.readBugInt.
func isBlockingBug(bug map[string]any) bool {
	if severity, _ := bug["severity"].(string); severity == "P0" {
		return true
	}
	return readBugBool(bug["blocking"])
}

// readBugBool parses the blocking flag from the decoded runtime entity.
// Accepts bool (normal JSON decode), and the string "true"/"false" for
// recovery/import paths that re-serialize flags as text. Anything else is
// not a blocking claim.
func readBugBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
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
	reviewMap, ok := state["review"].(map[string]any)
	if !ok {
		return 0
	}
	return readInt(reviewMap["round"])
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
