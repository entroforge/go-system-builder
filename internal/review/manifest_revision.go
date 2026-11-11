package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// ManifestPlanRevision decodes only the existing internal manifest identity.
// A legacy unversioned identity refers to the original Plan revision.
func ManifestPlanRevision(planID, assignmentID, manifestID string) (int, bool) {
	base := ManifestIDForPlan(planID, assignmentID)
	if base == "" {
		return 0, false
	}
	if manifestID == base {
		return 1, true
	}
	if !strings.HasPrefix(manifestID, base+".p") {
		return 0, false
	}
	revision, err := strconv.Atoi(strings.TrimPrefix(manifestID, base+".p"))
	return revision, err == nil && revision >= 2 && manifestID == fmt.Sprintf("%s.p%d", base, revision)
}

// ManifestAssignmentUnchangedSince compares only this original responsibility
// and its declared read/write inputs. Missing prior content is unknown, so the
// caller preserves the old draft and may prepare a separate current draft.
// Historical machine digest values are ignored, never calculated or checked.
func ManifestAssignmentUnchangedSince(root string, state map[string]any, assignmentID string, revision int) (bool, error) {
	current, ptr, err := LoadPlan(root, state)
	if err != nil {
		return false, err
	}
	if revision < 1 || revision > ptr.Revision {
		return false, nil
	}
	if revision == ptr.Revision {
		return true, nil
	}
	name := ptr.PlanID
	if revision > 1 {
		name = fmt.Sprintf("%s-r%d", name, revision)
	}
	rel := boundedReviewArtifactPath([]string{".claude", "review", "plans"}, name)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var previous Plan
	if err := json.Unmarshal(data, &previous); err != nil {
		return false, nil
	}
	if previous.ReviewPlanID != current.ReviewPlanID || previous.BaselineGeneration != current.BaselineGeneration {
		return false, nil
	}
	oldAssignment := findPlanAssignment(&previous, assignmentID)
	newAssignment := findPlanAssignment(current, assignmentID)
	oldContent, oldErr := planAssignmentContent(oldAssignment)
	newContent, newErr := planAssignmentContent(newAssignment)
	if oldErr != nil || newErr != nil || oldContent != newContent {
		return false, nil
	}
	oldClaims := map[string]Claim{}
	newClaims := map[string]Claim{}
	for _, claim := range previous.Claims {
		oldClaims[claim.ClaimID] = claim
	}
	for _, claim := range current.Claims {
		newClaims[claim.ClaimID] = claim
	}
	for _, id := range newAssignment.ClaimIDs {
		old, oldOK := oldClaims[id]
		next, newOK := newClaims[id]
		if !oldOK || !newOK || !sameClaimContent(old, next) {
			return false, nil
		}
	}
	if workspaceValue(previous.VerificationArtifactWorkspace) != workspaceValue(current.VerificationArtifactWorkspace) {
		return false, nil
	}
	oldSubjects := append([]FrozenSubject(nil), previous.FrozenSubjects...)
	newSubjects := append([]FrozenSubject(nil), current.FrozenSubjects...)
	return reflect.DeepEqual(oldSubjects, newSubjects), nil
}
