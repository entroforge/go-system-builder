package repair

import (
	"fmt"
	"path/filepath"
	"time"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// artifactSink is per invocation. Domain builders remain the only source of
// validated bytes; no global writer switch or alternate authority tree exists.
type artifactSink func(root, relative, schemaName string, document any) (ArtifactRef, error)

type preparedArtifacts struct {
	items []runtimepkg.ImmutableArtifact
}

type preparedPlanReport struct {
	draft   PlanReportRequest
	report  PlanReport
	outputs []runtimepkg.ImmutableArtifact
}

// SubmitPlanReportDraftToRuntime prepares and consumes a report as one durable
// Runtime operation. The older ref consumer remains available for already
// published historical artifacts; CLI submission uses this composed producer.
func submitPlanReportDraftToRuntime(root, statePath, journalPath string, request RuntimeRequest, draft PlanReportRequest) (runtimepkg.Snapshot, PlanReport, ArtifactRef, error) {
	if draft.OccurredAt.IsZero() {
		draft.OccurredAt = time.Now().UTC()
	}
	prepared := &preparedArtifacts{}
	report, ref, err := createPlanReport(root, draft, prepared.prepare)
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, ArtifactRef{}, err
	}
	snapshot, consumed, err := SubmitRepairPlanReportToRuntime(root, statePath, journalPath, SubmitPlanReportRequest{
		RuntimeRequest: request, Report: ref,
		prepared: &preparedPlanReport{draft: draft, report: report, outputs: prepared.items},
	})
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, ArtifactRef{}, err
	}
	return snapshot, consumed, ref, nil
}

func (p *preparedArtifacts) prepare(root, relative, schemaName string, document any) (ArtifactRef, error) {
	data, err := canonicalJSON(document)
	if err != nil {
		return ArtifactRef{}, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
		return ArtifactRef{}, fmt.Errorf("validate %s: %w", relative, err)
	}
	if _, err := repositoryPath(root, relative); err != nil {
		return ArtifactRef{}, err
	}
	relative = filepath.ToSlash(filepath.Clean(relative))
	for _, a := range p.items {
		if a.Path == relative {
			return ArtifactRef{}, fmt.Errorf("duplicate prepared artifact %s", relative)
		}
	}
	p.items = append(p.items, runtimepkg.ImmutableArtifact{Path: relative, Data: data})
	return fileRef(relative, data), nil
}

// Domain IDs stay human-readable; physical identity also binds Runtime and
// Session. Content has its own full SHA in the ref and is never overwritten.
func scopedRepairPath(kind, id, runtimeID, sessionID string) string {
	scope, _ := canonicalJSON([]string{runtimeID, sessionID})
	return artifactRoot + "/" + kind + "/scope-" + sha256Bytes(scope) + "/" + id + ".json"
}
