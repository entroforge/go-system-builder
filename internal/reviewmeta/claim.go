package reviewmeta

// FrozenSubject is the exact JSON DTO stored in a ReviewPlan. Field order and
// tags match review.FrozenSubject so source projection uses the same
// json.Marshal behavior as the original Review writer, including omitempty.
type FrozenSubject struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind,omitempty"`
}

// Claim is the exact JSON DTO stored in a ReviewPlan. Field order and tags
// match review.Claim so optional empty values are normalized by the same Go
// serializer used when the original packet was built.
type Claim struct {
	ClaimID          string   `json:"claim_id"`
	Lens             string   `json:"lens"`
	Target           string   `json:"target"`
	Assertion        string   `json:"assertion"`
	Oracle           string   `json:"oracle"`
	Method           string   `json:"method"`
	RequiredEvidence []string `json:"required_evidence,omitempty"`
	Applicability    string   `json:"applicability"`
	NARationale      string   `json:"na_rationale,omitempty"`
	NAChecklistID    string   `json:"na_checklist_id,omitempty"`
	SourceRefs       []string `json:"source_refs"`
	FocusKey         string   `json:"focus_key,omitempty"`
	DependsOn        []string `json:"depends_on,omitempty"`
	ResourceLocks    []string `json:"resource_locks,omitempty"`
}
