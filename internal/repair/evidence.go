package repair

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func CreateChangeImpact(root string, request ChangeImpactRequest) (ChangeImpact, ArtifactRef, error) {
	if strings.TrimSpace(request.ImpactID) == "" {
		return ChangeImpact{}, ArtifactRef{}, errors.New("impact record identity is required")
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
		Decisions: append([]ImpactDecision{}, request.Decisions...), EscalationLevel: request.EscalationLevel, IntegrationNotes: request.IntegrationNotes,
		InvalidatedEvidenceIDs: sortedStrings(request.InvalidatedEvidenceIDs), SupersededEvidenceIDs: sortedStrings(request.SupersededEvidenceIDs),
		RetainedEvidenceIDs: sortedStrings(request.RetainedEvidenceIDs), RequiredReverificationIDs: sortedStrings(request.RequiredReverificationIDs),
		AnalyzedBy: request.AnalyzedBy, AnalyzedAt: nowOr(request.AnalyzedAt),
	}
	if impact.EscalationLevel == "" {
		impact.EscalationLevel = "assignment"
	}
	path := responsibilityArtifactPath("change-impact", request.ImpactID)
	ref, err := writeUniqueArtifact(root, path, "review-evidence.schema.json", impact)
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
	return createTargetedReverification(root, request, responsibilityArtifactPath("reverification", request.ReverificationID))
}

func createTargetedReverification(root string, request TargetedReverificationRequest, relativePath string) (TargetedReverification, ArtifactRef, error) {
	if strings.TrimSpace(request.ReverificationID) == "" {
		return TargetedReverification{}, ArtifactRef{}, errors.New("reverification record identity is required")
	}
	// Independence concerns the actual implementer/integrator responsibility,
	// not assignment spelling or a label supplied in this professional report.
	reverification := TargetedReverification{
		SchemaVersion: "1.0.0", RecordType: "targeted_reverification", ReverificationID: request.ReverificationID,
		RuntimeID: request.RuntimeID, BugID: request.BugID, CaseID: request.CaseID, BaselineGeneration: request.BaselineGeneration,
		OriginalAssignmentID: request.OriginalAssignmentID, PerformingAssignmentID: request.PerformingAssignmentID,
		ContinuityReason: request.ContinuityReason, ImpactID: request.ImpactID, AssertionResults: append([]AssertionResult{}, request.AssertionResults...),
		ScopeCompliance: request.ScopeCompliance, Result: request.Result, FailureClass: request.FailureClass, PerformedAt: nowOr(request.PerformedAt),
	}
	if strings.TrimSpace(reverification.Result) == "" {
		return TargetedReverification{}, ArtifactRef{}, errors.New("Targeted result is not yet expressed; preserve the draft and hand the unfinished work to Main")
	}
	ref, err := writeUniqueArtifact(root, relativePath, "review-evidence.schema.json", reverification)
	return reverification, ref, err
}

// writeUniqueArtifact publishes a candidate without claiming an existing
// file. The caller commits the returned path in its original Runtime Writer;
// an accepted replay is resolved from that recorded reference, not the bytes
// of an unregistered candidate.
func writeUniqueArtifact(root, relativePath, schemaName string, document any) (ArtifactRef, error) {
	return writeUniqueArtifactPrepared(root, relativePath, schemaName, func(string) any { return document })
}

// writeUniqueArtifactPrepared selects the actual path before serializing the
// document. The caller can record that path in the same original Writer fact.
func writeUniqueArtifactPrepared(root, relativePath, schemaName string, prepare func(string) any) (ArtifactRef, error) {
	identity := base64.RawURLEncoding.EncodeToString([]byte(relativePath))
	for attempt := 0; ; attempt++ {
		path := relativePath
		if attempt > 0 {
			path = responsibilityArtifactPath(fmt.Sprintf("artifact-alternative-%d", attempt), identity)
		}
		data, err := canonicalJSON(prepare(path))
		if err != nil {
			return ArtifactRef{}, err
		}
		if len(data) > 20<<20 {
			return ArtifactRef{}, fmt.Errorf("artifact exceeds 20 MiB after final serialization")
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
			return ArtifactRef{}, err
		}
		created, err := workpacket.PublishNewBounded(root, path, data, 20<<20)
		if err != nil {
			return ArtifactRef{}, err
		}
		if created {
			return fileRef(path, data), nil
		}
		parent, _, err := workpacket.OpenParent(root, artifactRoot+"/.artifact-parent", false)
		if err != nil {
			return ArtifactRef{}, fmt.Errorf("open artifact parent: %w", err)
		}
		if err := parent.Close(); err != nil {
			return ArtifactRef{}, err
		}
	}
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
	return value, nil
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
