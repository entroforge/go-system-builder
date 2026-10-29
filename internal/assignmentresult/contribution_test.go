package assignmentresult

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestContributionPreservesInvalidatedDeliveryWithoutCurrentPASS(t *testing.T) {
	for _, rule := range []string{"unscoped_evidence", "scope_overlap", "change_impact_invalid"} {
		t.Run(rule, func(t *testing.T) {
			state, record, ref, files := acceptedFixture(t)
			row := state["evidence"].([]any)[0].(map[string]any)
			row["status"], row["invalidated_by"], row["invalidation_rule"], row["invalidation_reason"] = "invalid", "impact-original", rule, "original repair changed product scope"
			before, _ := json.Marshal(state)
			read := func(p string) ([]byte, error) {
				b, ok := files[p]
				if !ok {
					return nil, fmt.Errorf("missing %s", p)
				}
				return b, nil
			}
			if _, err := LoadAccepted(state, record, ref, read); err == nil {
				t.Fatal("invalid evidence granted current acceptance")
			}
			if _, err := AcceptedHistory(state, record, read); err == nil {
				t.Fatal("invalid history granted current acceptance")
			}
			result, err := LoadContribution(state, record, ref, read)
			if err != nil || result.Conclusion != "submitted" {
				t.Fatalf("historical contribution: %v", err)
			}
			history, err := ContributionHistory(state, record, read)
			if err != nil || len(history) != 1 || history[0].EvidenceID != result.EvidenceID {
				t.Fatalf("historical history: %v", err)
			}
			after, _ := json.Marshal(state)
			if string(before) != string(after) {
				t.Fatal("history reader rewrote evidence state")
			}
			files[result.ResolvedEvidenceRefs[0].Path] = []byte("tampered original source")
			if _, err := LoadContribution(state, record, ref, read); err == nil {
				t.Fatal("invalidated history bypassed immutable source validation")
			}
		})
	}
}

func TestContributionRejectsUnprovenInvalidationOrRegistry(t *testing.T) {
	for _, test := range []string{"missing by", "missing reason", "other rule", "assignment revision", "superseded", "missing registry", "changed result", "duplicate path"} {
		t.Run(test, func(t *testing.T) {
			state, record, ref, files := acceptedFixture(t)
			row := state["evidence"].([]any)[0].(map[string]any)
			row["status"], row["invalidated_by"], row["invalidation_rule"], row["invalidation_reason"] = "invalid", "impact-original", "unscoped_evidence", "scope changed"
			switch test {
			case "missing by":
				delete(row, "invalidated_by")
			case "missing reason":
				delete(row, "invalidation_reason")
			case "other rule":
				row["invalidation_rule"] = "manual_override"
			case "assignment revision":
				row["invalidation_rule"] = "assignment_revision_issued"
			case "superseded":
				row["status"] = "superseded"
			case "missing registry":
				state["entities"].(map[string]any)["assignments"] = []any{}
			case "changed result":
				files[ref] = append(files[ref], byte(' '))
			case "duplicate path":
				state["evidence"] = append(state["evidence"].([]any), row)
			}
			read := func(p string) ([]byte, error) { return files[p], nil }
			if _, err := LoadContribution(state, record, ref, read); err == nil {
				t.Fatal("unproven contribution accepted")
			}
		})
	}
}
