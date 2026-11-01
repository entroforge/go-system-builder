package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
)

// ---------------------------------------------------------------------------
// Plan loading uses the original responsibility and stored Plan path.
// A historical digest is never recomputed or used as a lookup condition.
// ---------------------------------------------------------------------------

// PlanPointer retains the original API for the shared read-only projection.
type PlanPointer = reviewmeta.PlanPointer

// PlanQuestionnairePath names the current Main authoring file. Both the
// listener and product-drift check use this path; it is not a product subject.
func PlanQuestionnairePath(root string, state map[string]any) string {
	id, _ := state["runtime_id"].(string)
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		return ""
	}
	generation := baselineGeneration(state)
	if generation < 1 {
		return ""
	}
	rev, _ := state["review"].(map[string]any)
	identity := reviewmeta.FromReview(rev)
	if identity.PlanID == "" || filepath.Base(identity.PlanID) != identity.PlanID || identity.PlanID == "." || identity.PlanID == ".." {
		return ""
	}
	parts := append([]string{root, ".claude", "submissions"}, boundedReviewIdentity(id)...)
	parts = append(parts, fmt.Sprintf("g%d", generation), "S7")
	parts = append(parts, boundedReviewIdentity(identity.PlanID)...)
	return filepath.Join(append(parts, "review-plan.json")...)

}

// PlanPointerFromState reads the existing Runtime projection without mutation.
func PlanPointerFromState(state map[string]any) *PlanPointer {
	return reviewmeta.FromState(state)
}

// LoadPlan reads the original Plan at its stored path.
func LoadPlan(root string, state map[string]any) (*Plan, *PlanPointer, error) {
	ptr := PlanPointerFromState(state)
	if ptr == nil {
		return nil, nil, fmt.Errorf("no ReviewPlan is registered for the current review round; create one with the supplied original ReviewPlan questionnaire")
	}
	abs := ptr.Path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(ptr.Path))
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("read ReviewPlan %s: %w", ptr.Path, err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, nil, fmt.Errorf("decode ReviewPlan %s: %w", ptr.Path, err)
	}
	if plan.ReviewPlanID != ptr.PlanID {
		return nil, nil, fmt.Errorf("ReviewPlan id mismatch: state pins %s but the file declares %s", ptr.PlanID, plan.ReviewPlanID)
	}
	return &plan, ptr, nil
}

// DependenciesSettled is the dispatch-time half of the Claim DAG gate. A
// dependency is usable only after its owning Assignment has produced a
// terminal Claim disposition. This check reads the actual upstream Result
// before dispatching the dependent Assignment.
func DependenciesSettled(state map[string]any, plan *Plan, assignment *PlanAssignment) error {
	if plan == nil || assignment == nil {
		return fmt.Errorf("dependency gate requires a plan and Assignment")
	}
	claims := make(map[string]Claim, len(plan.Claims))
	for _, claim := range plan.Claims {
		claims[claim.ClaimID] = claim
	}
	dispositions := Dispositions(state)
	for _, claimID := range assignment.ClaimIDs {
		claim, ok := claims[claimID]
		if !ok {
			return fmt.Errorf("assignment %s references unknown claim %s", assignment.AssignmentID, claimID)
		}
		for _, dependencyID := range claim.DependsOn {
			disposition, ok := dispositions[dependencyID]
			if !ok || !isSettledClaimDisposition(disposition.Disposition) {
				current := "missing"
				if ok {
					current = disposition.Disposition
				}
				return fmt.Errorf("assignment %s depends on claim %s, whose disposition is %s; consume the upstream Result before dispatching this Assignment", assignment.AssignmentID, dependencyID, current)
			}
		}
	}
	return nil
}

func isSettledClaimDisposition(disposition string) bool {
	switch disposition {
	case "pass", "finding", "blocked", "not_applicable":
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Disposition projection: state.review.claims is maintained inside the
// review-plan / review-result CAS transactions. These helpers read it.
// ---------------------------------------------------------------------------

// ClaimDisposition is one claim's round-level projection.
type ClaimDisposition struct {
	Lens          string
	Applicability string
	Disposition   string
	AssignmentID  string
	ResultID      string
	FindingIDs    []string
}

// Dispositions reads state.review.claims.
func Dispositions(state map[string]any) map[string]ClaimDisposition {
	out := map[string]ClaimDisposition{}
	review, _ := state["review"].(map[string]any)
	raw, _ := review["claims"].(map[string]any)
	for claimID, value := range raw {
		row, _ := value.(map[string]any)
		if row == nil {
			continue
		}
		disp := ClaimDisposition{
			Lens:          stringField(row["lens"]),
			Applicability: stringField(row["applicability"]),
			Disposition:   stringField(row["disposition"]),
			AssignmentID:  stringField(row["assignment_id"]),
			ResultID:      stringField(row["result_id"]),
		}
		findings, _ := row["finding_ids"].([]any)
		for _, f := range findings {
			if s, ok := f.(string); ok {
				disp.FindingIDs = append(disp.FindingIDs, s)
			}
		}
		out[claimID] = disp
	}
	return out
}

// RoundComplete reports whether every required claim has a final
// disposition (pass / finding / not_applicable / blocked). planned and
// running claims keep the round open.
func RoundComplete(state map[string]any) bool {
	for _, disp := range Dispositions(state) {
		if disp.Applicability != "required" {
			continue
		}
		switch disp.Disposition {
		case "pass", "finding", "blocked":
		default:
			return false
		}
	}
	return true
}

// UndispositionedRequired lists required claims still waiting for a Result.
func UndispositionedRequired(state map[string]any) []string {
	var pending []string
	for claimID, disp := range Dispositions(state) {
		if disp.Applicability != "required" {
			continue
		}
		switch disp.Disposition {
		case "pass", "finding", "blocked":
		default:
			pending = append(pending, claimID)
		}
	}
	sort.Strings(pending)
	return pending
}

// StaticClaimsSettled reports whether every required static-wave claim has a
// final disposition; behavior assignments dispatch only behind this fact
// (L3-S7 §5.2/§5.3).
func StaticClaimsSettled(state map[string]any, plan *Plan) bool {
	waveByClaim := map[string]string{}
	for _, assignment := range plan.Assignments {
		for _, claimID := range assignment.ClaimIDs {
			waveByClaim[claimID] = assignment.ExecutionWave
		}
	}
	dispositions := Dispositions(state)
	for _, claim := range plan.Claims {
		if claim.Applicability != "required" || waveByClaim[claim.ClaimID] != "static" {
			continue
		}
		switch dispositions[claim.ClaimID].Disposition {
		case "pass", "finding", "blocked":
		default:
			return false
		}
	}
	return true
}

// RemainingStaticClaims counts required static-wave claims still awaiting a
// final disposition. It is the explainability number behind the wave gate
// (RC-12): the s7 status board and manifest-draft notes print it so an agent
// can see how far the behavior wave is from dispatch without re-deriving the
// count from raw state.
func RemainingStaticClaims(state map[string]any, plan *Plan) int {
	waveByClaim := map[string]string{}
	for _, assignment := range plan.Assignments {
		for _, claimID := range assignment.ClaimIDs {
			waveByClaim[claimID] = assignment.ExecutionWave
		}
	}
	dispositions := Dispositions(state)
	remaining := 0
	for _, claim := range plan.Claims {
		if claim.Applicability != "required" || waveByClaim[claim.ClaimID] != "static" {
			continue
		}
		switch dispositions[claim.ClaimID].Disposition {
		case "pass", "finding", "blocked":
		default:
			remaining++
		}
	}
	return remaining
}

// RoundFindings lists the finding entity rows for the current review round.
func RoundFindings(state map[string]any) []map[string]any {
	identity := activeReviewIdentity(state)
	var out []map[string]any
	entities, _ := state["entities"].(map[string]any)
	findings, _ := entities["findings"].([]any)
	for _, raw := range findings {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		if reviewmeta.CompareIdentity(rowReviewIdentity(row), identity) == reviewmeta.Same {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return stringField(out[i]["finding_id"]) < stringField(out[j]["finding_id"])
	})
	return out
}

// ---------------------------------------------------------------------------
// small shared helpers (kept local so review does not import qualitygate)
// ---------------------------------------------------------------------------

// evidenceEntries returns the evidence array as raw maps.
func evidenceEntries(state map[string]any) []any {
	entries, _ := state["evidence"].([]any)
	return entries
}

func stringField(value any) string {
	s, _ := value.(string)
	return s
}

func intField(value any) int {
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

func baselineGeneration(state map[string]any) int {
	baseline, _ := state["baseline"].(map[string]any)
	return intField(baseline["generation"])
}
