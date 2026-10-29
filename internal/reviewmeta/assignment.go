package reviewmeta

// PlanAssignment is the exact JSON projection stored in a ReviewPlan. Keep
// the field order and tags aligned with the original review.PlanAssignment:
// its json.Marshal bytes are the durable Assignment digest source.
//
// This DTO is read-only metadata. It does not authorize dispatch or change a
// ReviewPlan.
type PlanAssignment struct {
	AssignmentID       string   `json:"assignment_id"`
	Lens               string   `json:"lens"`
	ClaimIDs           []string `json:"claim_ids"`
	FocusKeys          []string `json:"focus_keys,omitempty"`
	NonOverlapBoundary string   `json:"non_overlap_boundary"`
	ExecutionWave      string   `json:"execution_wave"`
	ResourceLocks      []string `json:"resource_locks,omitempty"`
	BehaviorSurface    string   `json:"behavior_surface,omitempty"`
}
