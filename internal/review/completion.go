package review

import "github.com/entroforge/go-system-builder/internal/workstate"

// RequiredResponsibilitiesComplete reads only the current plan projection and
// already accepted conclusions. It does not certify coverage or evidence.
func RequiredResponsibilitiesComplete(state map[string]any) bool {
	r, _ := state["review"].(map[string]any)
	rows, _ := r["assignments"].(map[string]any)
	claims, _ := r["claims"].(map[string]any)
	if len(claims) == 0 {
		return false
	}
	allNotApplicable := true
	// An expressed required Claim with no Assignment is unfinished planning,
	// not an implicit passing responsibility.
	for _, raw := range claims {
		claim, _ := raw.(map[string]any)
		if claim["applicability"] != "not_applicable" {
			allNotApplicable = false
		}
		if claim["applicability"] == "required" && claim["assignment_id"] == "" {
			return false
		}
	}
	hasResponsibility := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		// A declared Assignment (or already issued responsibility) remains
		// actual work even if the Claim classification was left unspecified.
		// Only explicit N/A Claims are skipped.
		owner, _ := row["agent_id"].(string)
		required := owner != ""
		for _, id := range stringSliceValue(row["claim_ids"]) {
			claim, _ := claims[id].(map[string]any)
			if claim["applicability"] != "not_applicable" {
				required = true
			}
		}
		if !required {
			continue
		}
		hasResponsibility = true
		if workstate.ReviewAssignmentActivity(row) != "consumed" {
			return false
		}
		verdict, _ := row["result_verdict"].(string)
		if verdict != "" {
			if verdict != "pass" {
				return false
			}
			continue
		}
		if receipt, _ := row["result_receipt"].(map[string]any); receipt != nil {
			continue
		}
		// Historical receiptless records retain their original dispositions.
		for _, id := range stringSliceValue(row["claim_ids"]) {
			claim, _ := claims[id].(map[string]any)
			if claim["applicability"] == "required" && claim["disposition"] != "pass" {
				return false
			}
		}
	}
	return hasResponsibility || allNotApplicable
}

// ResultWorkComplete distinguishes an accepted handoff from unfinished work.
// The existing assignment activity is authoritative for new receptions.
func ResultWorkComplete(state map[string]any, assignmentID string) bool {
	row := resultPacketAssignmentRow(state, assignmentID)
	activity := workstate.ReviewAssignmentActivity(row)
	return (activity == "consumed" || activity == "result_submitted") && row["result_verdict"] != "blocked"
}
