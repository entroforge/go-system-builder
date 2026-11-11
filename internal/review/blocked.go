package review

import "fmt"

func hasOwnedBlocked(result *Result, claimIDs []string) bool {
	owned := make(map[string]bool, len(claimIDs))
	for _, id := range claimIDs {
		owned[id] = true
	}
	for _, blocked := range result.BlockedClaims {
		if owned[blocked.ClaimID] {
			return true
		}
	}
	return false
}

// blockedClaimsOrEmpty keeps the persisted envelope field a stable array.
func blockedClaimsOrEmpty(result *Result) []BlockedClaim {
	if result.BlockedClaims == nil {
		return []BlockedClaim{}
	}
	return result.BlockedClaims
}

// applyBlockedDispositions writes the derived disposition rows. The claim row
// keeps the projection's identity binding (result_id + blocking finding_ids);
// the full source fields (failed_precondition, evidence_refs,
// after_repair_required) are preserved in the persisted result envelope and
// in the sealed ObservationBatch's claim_coverage_summary.blocked_claims.
func applyBlockedDispositions(claims map[string]any, assignment *PlanAssignment, result *Result) error {
	owned := make(map[string]bool, len(assignment.ClaimIDs))
	for _, id := range assignment.ClaimIDs {
		owned[id] = true
	}
	for _, blocked := range result.BlockedClaims {
		if !owned[blocked.ClaimID] {
			continue
		}
		claimRow, _ := claims[blocked.ClaimID].(map[string]any)
		if claimRow == nil {
			return fmt.Errorf("claim %s is missing from the runtime projection", blocked.ClaimID)
		}
		if claimRow["applicability"] == "not_applicable" {
			return fmt.Errorf("claim %s is not_applicable in the plan; N/A is a plan disposition, never a blocked projection (L3-S7 §9.3)", blocked.ClaimID)
		}
		claimRow["disposition"] = "blocked"
		claimRow["result_id"] = result.ResultID
		ids := make([]any, 0, len(blocked.BlockingFindingIDs))
		for _, id := range blocked.BlockingFindingIDs {
			ids = append(ids, id)
		}
		claimRow["finding_ids"] = ids
	}
	return nil
}

// projectBlockedDispositions mirrors applyBlockedDispositions onto the
// pre-transaction projection the round consumer reasons over.
func projectBlockedDispositions(dispositions map[string]ClaimDisposition, assignment *PlanAssignment, result *Result) {
	owned := make(map[string]bool, len(assignment.ClaimIDs))
	for _, id := range assignment.ClaimIDs {
		owned[id] = true
	}
	for _, blocked := range result.BlockedClaims {
		if !owned[blocked.ClaimID] {
			continue
		}
		disp := dispositions[blocked.ClaimID]
		disp.Disposition = "blocked"
		disp.ResultID = result.ResultID
		disp.FindingIDs = append([]string{}, blocked.BlockingFindingIDs...)
		dispositions[blocked.ClaimID] = disp
	}
}
