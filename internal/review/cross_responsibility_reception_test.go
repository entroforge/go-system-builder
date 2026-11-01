package review

import (
	"encoding/json"
	"testing"
)

// A Reviewer can report observations about another responsibility without
// signing that Reviewer's work. Exercise the issued answer through the real
// packet consumer and Writer, not only the projection helper.
func TestIssuedResultPreservesCrossResponsibilityObservation(t *testing.T) {
	for _, field := range []string{"claim_results", "blocked_claims", "blocked_observation_on_pass"} {
		t.Run(field, func(t *testing.T) {
			root, statePath, journalPath, packet, request := issuedClaimStopFixture(t)
			answer := claimStopAnswer(t, "P1")
			finding := answer["findings"].([]any)[0].(map[string]any)
			finding["claim_id"] = "claim-dv-1"
			answer["claim_results"] = []any{}
			if field == "claim_results" {
				answer[field] = []any{map[string]any{"claim_id": "claim-dv-1", "conclusion": "fail", "observed": "cross-responsibility observation"}}
			} else {
				answer["blocked_claims"] = []any{map[string]any{"claim_id": "claim-dv-1", "blocking_finding_ids": []any{}, "failed_precondition": map[string]any{"kind": "entry", "detail": "entry unavailable"}, "evidence_refs": []any{}}}
			}
			if field == "blocked_observation_on_pass" {
				answer["findings"] = []any{}
				answer["verdict"] = "pass"
			}
			var before map[string]any
			if err := json.Unmarshal(mustReadTestFile(t, statePath), &before); err != nil {
				t.Fatal(err)
			}
			prior := before["review"].(map[string]any)["claims"].(map[string]any)["claim-dv-1"]
			priorJSON, _ := json.Marshal(prior)
			saveClaimStopAnswer(t, root, packet, answer)
			accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
			if err != nil {
				t.Fatalf("receive original answer: %v", err)
			}
			after := accepted.State["review"].(map[string]any)["claims"].(map[string]any)["claim-dv-1"]
			afterJSON, _ := json.Marshal(after)
			if string(priorJSON) != string(afterJSON) {
				t.Fatalf("other owner's claim changed: before=%s after=%s", priorJSON, afterJSON)
			}
			wantFindings := 1
			if field == "blocked_observation_on_pass" {
				wantFindings = 0
				if !ResultWorkComplete(accepted.State, packet.Binding.AssignmentID) {
					t.Fatal("another owner's obstacle marked this responsibility unfinished")
				}
			}
			if len(RoundFindings(accepted.State)) != wantFindings {
				t.Fatal("cross-responsibility finding was not preserved")
			}
		})
	}
}
