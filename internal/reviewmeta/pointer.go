// Package reviewmeta reads the existing ReviewPlan pointer projection. It owns no state or authority.
package reviewmeta

import "encoding/json"

// PlanPointer is the state.review.plan projection.
type PlanPointer struct {
	PlanID                        string
	Path                          string
	Revision                      int
	Status                        string
	E2ECoverageState              string
	VerificationArtifactWorkspace string
	SubmittedAt                   string
}

// PlanPointerFromState extracts the registered plan pointer, or nil.
func FromState(state map[string]any) *PlanPointer {
	review, _ := state["review"].(map[string]any)
	if review == nil {
		return nil
	}
	raw, _ := review["plan"].(map[string]any)
	if raw == nil {
		return nil
	}
	ptr := &PlanPointer{
		PlanID:           textField(raw["plan_id"]),
		Path:             textField(raw["path"]),
		Revision:         numberField(raw["revision"]),
		Status:           textField(raw["status"]),
		E2ECoverageState: textField(raw["e2e_coverage_state"]),
		SubmittedAt:      textField(raw["submitted_at"]),
	}
	if ws, ok := raw["verification_artifact_workspace"].(string); ok {
		ptr.VerificationArtifactWorkspace = ws
	}
	if ptr.PlanID == "" || ptr.Path == "" {
		return nil
	}
	return ptr
}

func textField(value any) string { s, _ := value.(string); return s }
func numberField(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

// Identity returns the original registered responsibility, including a present legacy number.
func (p PlanPointer) Identity() Identity {
	return Identity{PlanID: p.PlanID}
}
