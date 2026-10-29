package repair

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func CreateChangeImpact(root string, request ChangeImpactRequest) (ChangeImpact, ArtifactRef, error) {
	if strings.TrimSpace(request.ImpactID) == "" {
		return ChangeImpact{}, ArtifactRef{}, errors.New("impact record identity is required")
	}
	if (len(request.ChangedArtifacts) > 0 && len(request.ChangeTypes) == 0) || len(request.Decisions) == 0 {
		return ChangeImpact{}, ArtifactRef{}, errors.New("record professional impact decisions and actual change types for any changed artifacts")
	}
	for _, decision := range request.Decisions {
		if strings.TrimSpace(decision.Rationale) == "" {
			return ChangeImpact{}, ArtifactRef{}, errors.New("impact decisions require the actual professional reason")
		}
	}
	changed := append([]ArtifactRef{}, request.ChangedArtifacts...)
	for i := range changed {
		if changed[i].ID == "" {
			changed[i].ID = "changed-" + strings.NewReplacer("/", "-", "\\", "-").Replace(changed[i].Path)
		}
	}
	impact := ChangeImpact{
		SchemaVersion: "1.0.0", RecordType: "change_impact", ImpactID: request.ImpactID,
		RuntimeID: request.RuntimeID, ReqID: request.ReqID, BaselineGeneration: request.BaselineGeneration,
		SourceBugIDs: sortedStrings(request.SourceBugIDs), SourceCaseIDs: sortedStrings(request.SourceCaseIDs), ChangeTypes: sortedStrings(request.ChangeTypes), ChangedArtifacts: changed,
		Decisions: append([]ImpactDecision(nil), request.Decisions...), EscalationLevel: request.EscalationLevel,
		InvalidatedEvidenceIDs: sortedStrings(request.InvalidatedEvidenceIDs), SupersededEvidenceIDs: sortedStrings(request.SupersededEvidenceIDs),
		RetainedEvidenceIDs: sortedStrings(request.RetainedEvidenceIDs), RequiredReverificationIDs: sortedStrings(request.RequiredReverificationIDs),
		AnalyzedBy: request.AnalyzedBy, AnalyzedAt: nowOr(request.AnalyzedAt),
	}
	if impact.EscalationLevel == "" {
		impact.EscalationLevel = "assignment"
	}
	path := filepath.Join(artifactRoot, "change-impact", request.ImpactID+".json")
	var ref ArtifactRef
	var err error
	if request.AnalyzedAt.IsZero() {
		ref, err = writeImmutable(root, path, "review-evidence.schema.json", impact)
	} else {
		ref, err = writeImmutableOrIdenticalReplay(root, path, "review-evidence.schema.json", impact)
	}
	return impact, ref, err
}

func ValidateChangeImpact(root string, ref ArtifactRef) (ChangeImpact, error) {
	var impact ChangeImpact
	data, err := workpacket.ReadFile(root, ref.Path, 20<<20)
	if err != nil {
		return ChangeImpact{}, err
	}
	if err := json.Unmarshal(data, &impact); err != nil {
		return ChangeImpact{}, err
	}
	if impact.RecordType != "change_impact" {
		return ChangeImpact{}, fmt.Errorf("artifact %s is %q, not change_impact", ref.Path, impact.RecordType)
	}
	return impact, nil
}

func CreateTargetedReverification(root string, request TargetedReverificationRequest) (TargetedReverification, ArtifactRef, error) {
	return createTargetedReverification(root, request, filepath.Join(artifactRoot, "reverification", request.ReverificationID+".json"), !request.PerformedAt.IsZero())
}

// CreateTargetedReverificationForResume writes another immutable attempt for
// the same logical reverification obligation after its recorded blocker was
// resolved. The resume timestamp selects the storage path; it is not added to
// the artifact schema or the required-ID set.
func CreateTargetedReverificationForResume(root string, request TargetedReverificationRequest, blockerResolvedAt time.Time) (TargetedReverification, ArtifactRef, error) {
	if blockerResolvedAt.IsZero() {
		return TargetedReverification{}, ArtifactRef{}, errors.New("resumed targeted reverification requires the recorded blocker_resolved_at timestamp")
	}
	if request.PerformedAt.IsZero() {
		request.PerformedAt = blockerResolvedAt
	}
	resumeKey := blockerResolvedAt.UTC().Format("20060102T150405.000000000Z")
	name := fmt.Sprintf("%s-resume-%s.json", request.ReverificationID, resumeKey)
	relative := filepath.Join(artifactRoot, "reverification", name)
	return createTargetedReverification(root, request, relative, true)
}

func createTargetedReverification(root string, request TargetedReverificationRequest, relativePath string, allowIdenticalReplay bool) (TargetedReverification, ArtifactRef, error) {
	if strings.TrimSpace(request.ReverificationID) == "" {
		return TargetedReverification{}, ArtifactRef{}, errors.New("reverification record identity is required")
	}
	// Independence concerns the actual implementer/integrator responsibility,
	// not assignment spelling or a label supplied in this professional report.
	reverification := TargetedReverification{
		SchemaVersion: "1.0.0", RecordType: "targeted_reverification", ReverificationID: request.ReverificationID,
		RuntimeID: request.RuntimeID, BugID: request.BugID, CaseID: request.CaseID, BaselineGeneration: request.BaselineGeneration,
		OriginalAssignmentID: request.OriginalAssignmentID, PerformingAssignmentID: request.PerformingAssignmentID,
		ContinuityReason: request.ContinuityReason, ImpactID: request.ImpactID, AssertionResults: append([]AssertionResult(nil), request.AssertionResults...),
		ScopeCompliance: request.ScopeCompliance, Result: request.Result, FailureClass: request.FailureClass, PerformedAt: nowOr(request.PerformedAt),
	}
	if reverification.Result == "" {
		reverification.Result = "blocked"
	}
	if reverification.Result != "pass" && reverification.FailureClass == "" {
		switch reverification.Result {
		case "blocked":
			reverification.FailureClass = "blocked"
		case "scope_changed":
			reverification.FailureClass = "scope_changed"
		default:
			reverification.FailureClass = "fail_same_cause"
		}
	}
	if err := validateTargetedPassEvidence(reverification); err != nil {
		return TargetedReverification{}, ArtifactRef{}, err
	}
	var ref ArtifactRef
	var err error
	if allowIdenticalReplay {
		ref, err = writeImmutableOrIdenticalReplay(root, relativePath, "review-evidence.schema.json", reverification)
	} else {
		ref, err = writeImmutable(root, relativePath, "review-evidence.schema.json", reverification)
	}
	return reverification, ref, err
}

func writeImmutableOrIdenticalReplay(root, relativePath, schemaName string, document any) (ArtifactRef, error) {
	ref, err := writeImmutable(root, relativePath, schemaName, document)
	if err == nil {
		return ref, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return ArtifactRef{}, err
	}
	want, encodeErr := canonicalJSON(document)
	if encodeErr != nil {
		return ArtifactRef{}, encodeErr
	}
	existing, readErr := workpacket.ReadFile(root, filepath.ToSlash(relativePath), 20<<20)
	if readErr != nil || !bytes.Equal(existing, want) {
		return ArtifactRef{}, err
	}
	if schemaErr := schema.NewEmbeddedValidator().ValidateBytes(schemaName, existing); schemaErr != nil {
		return ArtifactRef{}, err
	}
	return fileRef(filepath.ToSlash(relativePath), existing), nil
}

func ValidateTargetedReverification(root string, ref ArtifactRef) (TargetedReverification, error) {
	var value TargetedReverification
	data, err := workpacket.ReadFile(root, ref.Path, 20<<20)
	if err != nil {
		return TargetedReverification{}, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return TargetedReverification{}, err
	}
	if value.RecordType != "targeted_reverification" {
		return TargetedReverification{}, fmt.Errorf("artifact %s is %q, not targeted_reverification", ref.Path, value.RecordType)
	}
	if err := validateTargetedPassEvidence(value); err != nil {
		return TargetedReverification{}, err
	}
	return value, nil
}

func validateTargetedPassEvidence(value TargetedReverification) error {
	if value.Result != "pass" {
		return nil
	}
	if len(value.AssertionResults) == 0 {
		return errors.New("pass report requires actual assertion observations")
	}
	for _, assertion := range value.AssertionResults {
		if assertion.Result != "pass" {
			return fmt.Errorf("pass report contains unfinished assertion %q", assertion.AssertionID)
		}
		hasEvidence := false
		for _, ref := range assertion.EvidenceRefs {
			if strings.TrimSpace(ref) != "" {
				hasEvidence = true
				break
			}
		}
		if !hasEvidence {
			return fmt.Errorf("pass assertion %q needs its actual observation or output path", assertion.AssertionID)
		}
	}
	return nil
}

func sortedStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
