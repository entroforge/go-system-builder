package assignmentrecord

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestCaptureSourceSnapshotsCapturesExactBytesAndEmbeddedInputs(t *testing.T) {
	record, sources := snapshotFixtureRecord(t)
	depBytes := []byte("profile dependency bytes\n")
	profile := checkexec.Profile{
		ID: "profile-unit", Version: "1.0.0", CheckIndex: 0, Command: "go test ./...",
		Location: "worker", TimeoutSeconds: 30, EnvironmentPolicy: "declared_only",
		Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{{Path: "internal/profile-input.txt"}},
		Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never",
	}
	record.RequiredChecks = []string{"go test ./..."}
	record.CheckProfiles = []checkexec.Profile{profile}
	profileRef := record.Manifest.Ref + "#assignments/" + record.AssignmentID + "/check_profiles/" + profile.ID
	sources["internal/profile-input.txt"] = depBytes
	sources[profileRef] = nil // The Profile fragment is covered by manifest bytes, not read as a file.

	profileSourceRef := "gsb-embedded/agent-context-profiles.json"
	profileSourceBytes := []byte("embedded context profile bytes\n")
	profileSchemaRef := "gsb-embedded/schema/agent-message.schema.json"
	profileSchemaBytes := []byte("embedded schema bytes\n")
	record.ContextSources = append(record.ContextSources,
		ContextSource{Kind: "manifest_document", Ref: record.Task.Ref, ID: record.Task.ID, Version: record.Task.Version},
		ContextSource{Kind: "frozen_input", Ref: "docs/context/decision.md"},
		ContextSource{Kind: "check_profile", Ref: profileRef, ID: profile.ID, Version: profile.Version},
		ContextSource{Kind: "check_profile_dependency", Ref: "internal/profile-input.txt"},
		ContextSource{Kind: "embedded_context_profile", Ref: profileSourceRef},
		ContextSource{Kind: "embedded_schema", Ref: profileSchemaRef},
	)
	contextBytes := []byte("same content as a second source ref\n")
	sources["docs/context/decision.md"] = contextBytes
	sources[profileSourceRef] = profileSourceBytes
	sources[profileSchemaRef] = profileSchemaBytes
	var err error
	record.ContextSources, err = canonicalContextSources(record.ContextSources)
	if err != nil {
		t.Fatal(err)
	}

	calls := map[string]int{}
	updated, artifacts, err := CaptureSourceSnapshots(record, func(ref string) ([]byte, error) {
		calls[ref]++
		if strings.Contains(ref, "#") {
			return nil, fmt.Errorf("CheckProfile fragment unexpectedly passed to read callback: %s", ref)
		}
		data, ok := sources[ref]
		if !ok {
			return nil, fmt.Errorf("unexpected source ref %s", ref)
		}
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SourceSnapshots == nil || len(updated.SourceSnapshots) == 0 {
		t.Fatal("CaptureSourceSnapshots returned no source snapshot rows")
	}
	if calls[record.Task.Ref] != 1 {
		t.Fatalf("repeated source ref was read %d times, want one", calls[record.Task.Ref])
	}
	if _, ok := calls[profileRef]; ok {
		t.Fatal("embedded CheckProfile fragment was incorrectly read as a project file")
	}
	if len(artifacts) == 0 {
		t.Fatal("CaptureSourceSnapshots returned no publishable source bytes")
	}

	manifestSnapshot := findSnapshot(t, updated.SourceSnapshots, record.Manifest.Ref, "manifest")
	if got := artifacts[manifestSnapshot.Path]; string(got) != string(sources[record.Manifest.Ref]) {
		t.Fatal("manifest snapshot did not preserve the exact original bytes")
	}
	if !strings.HasPrefix(manifestSnapshot.Path, ".claude/evidence/loop-s6-record/g3/assignments/assignment-s6-record/records/r1/sources/") {
		t.Fatalf("snapshot path is outside the Assignment revision evidence prefix: %s", manifestSnapshot.Path)
	}
	if got := findSnapshot(t, updated.SourceSnapshots, profileSourceRef, "embedded_context_profile"); artifacts[got.Path] == nil {
		t.Fatalf("embedded context profile bytes were not captured at %s", got.Path)
	}
	if got := findSnapshot(t, updated.SourceSnapshots, profileSchemaRef, "embedded_schema"); artifacts[got.Path] == nil {
		t.Fatalf("embedded schema bytes were not captured at %s", got.Path)
	}
	for _, snapshot := range updated.SourceSnapshots {
		if snapshot.SourceRef == profileRef {
			t.Fatal("embedded CheckProfile fragment must be covered by exact manifest bytes, not a synthetic source file")
		}
	}
	taskRow := findSnapshot(t, updated.SourceSnapshots, record.Task.Ref, "task")
	docRow := findSnapshot(t, updated.SourceSnapshots, record.Task.Ref, "manifest_document")
	if taskRow.Path != docRow.Path {
		t.Fatal("same source under two semantic kinds did not share the original artifact")
	}

	encoded, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("assignment-record.schema.json", encoded); err != nil {
		t.Fatalf("schema rejected captured source snapshots: %v", err)
	}
}

func TestCaptureSourceSnapshotsPreservesRecordAndCapturesActualSource(t *testing.T) {
	legacy := recordTestRecord()
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "source_snapshots") || strings.Contains(string(encoded), "revision_issuance") {
		t.Fatalf("legacy Record acquired optional fields: %s", encoded)
	}
	decoded := Record{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Reference() != legacy.Reference() {
		t.Fatalf("legacy assignment responsibility changed: %+v", decoded.Reference())
	}

	record, sources := snapshotFixtureRecord(t)
	updated, artifacts, err := CaptureSourceSnapshots(record, func(ref string) ([]byte, error) {
		if ref == record.AgentDefinition.Ref {
			return []byte("current role source"), nil
		}
		return sources[ref], nil
	})
	if err != nil {
		t.Fatalf("actual role source was rejected: %v", err)
	}
	row := findSnapshot(t, updated.SourceSnapshots, record.AgentDefinition.Ref, "agent_definition")
	if string(artifacts[row.Path]) != "current role source" {
		t.Fatalf("captured wrong role source: %#v", row)
	}

}

func TestCaptureSourceSnapshotsEnforcesActualSizeBounds(t *testing.T) {
	oversized, oversizedSources := snapshotFixtureRecord(t)
	large := make([]byte, maxSourceSnapshotBytes+1)
	large[0] = 'x'
	oversizedSources[oversized.Task.Ref] = large
	if _, _, err := CaptureSourceSnapshots(oversized, func(ref string) ([]byte, error) { return oversizedSources[ref], nil }); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized source error = %v, want per-source bound rejection", err)
	}

	tooMany, totalSources := snapshotFixtureRecord(t)
	largeUnique := make([]byte, (maxSourceSnapshotBundleSize/5)+1)
	refs := []string{"docs/frozen/one", "docs/frozen/two", "docs/frozen/three", "docs/frozen/four", "docs/frozen/five"}
	for i, ref := range refs {
		data := append([]byte(nil), largeUnique...)
		data[0] = byte('a' + i)
		totalSources[ref] = data
		tooMany.ContextSources = append(tooMany.ContextSources, ContextSource{Kind: "frozen_input", Ref: ref})
	}
	tooMany.ContextSources, _ = canonicalContextSources(tooMany.ContextSources)
	if _, _, err := CaptureSourceSnapshots(tooMany, func(ref string) ([]byte, error) { return totalSources[ref], nil }); err == nil || !strings.Contains(err.Error(), "bundle exceeds") {
		t.Fatalf("oversized bundle error = %v, want total bound rejection", err)
	}
}

func TestCaptureSourceSnapshotsDoesNotInventProfileDependencyRegistration(t *testing.T) {
	record, sources := snapshotFixtureRecord(t)
	depBytes := []byte("dependency")
	profile := checkexec.Profile{
		ID: "profile-unit", Version: "1.0.0", CheckIndex: 0, Command: "go test ./...",
		Location: "worker", TimeoutSeconds: 30, EnvironmentPolicy: "declared_only",
		Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{{Path: "internal/profile-input.txt"}},
		Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never",
	}
	record.RequiredChecks = []string{"go test ./..."}
	record.CheckProfiles = []checkexec.Profile{profile}
	record.ContextSources = append(record.ContextSources, ContextSource{
		Kind: "check_profile", Ref: record.Manifest.Ref + "#assignments/" + record.AssignmentID + "/check_profiles/" + profile.ID,
		ID: profile.ID, Version: profile.Version,
	})
	record.ContextSources, _ = canonicalContextSources(record.ContextSources)
	sources["internal/profile-input.txt"] = depBytes
	if _, _, err := CaptureSourceSnapshots(record, func(ref string) ([]byte, error) { return sources[ref], nil }); err != nil {
		t.Fatalf("profile dependency without extra registration blocked original sources: %v", err)
	}
}

func TestRevisionIssuanceIsOptionalButStrictWhenPresent(t *testing.T) {
	previous := recordTestRecord()
	second := previous
	second.Revision = 2
	second.RegistrationRevision++
	if err := ValidateRecord(second); err != nil {
		t.Fatalf("historical r2 without an issuance receipt must remain valid: %v", err)
	}
	second.RevisionIssuance = &RevisionIssuance{
		IntentKey:      "intent-2026-10-14-001",
		PreviousRecord: previous.Reference(),
	}
	if err := ValidateRecord(second); err != nil {
		t.Fatalf("valid r2 issuance receipt rejected: %v", err)
	}
	encoded, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("assignment-record.schema.json", encoded); err != nil {
		t.Fatalf("schema rejected valid revision issuance: %v", err)
	}

	bad := second
	bad.Revision = 1
	if err := ValidateRecord(bad); err == nil {
		t.Fatal("r1 with revision issuance was accepted")
	}
	bad = second
	copyIssuance := *second.RevisionIssuance
	copyIssuance.PreviousRecord.Revision = 0
	bad.RevisionIssuance = &copyIssuance
	if err := ValidateRecord(bad); err == nil {
		t.Fatal("issuance receipt with a non-immediate previous revision was accepted")
	}
	bad = second
	copyIssuance = *second.RevisionIssuance
	copyIssuance.PreviousRecord.BaselineGeneration++
	bad.RevisionIssuance = &copyIssuance
	if err := ValidateRecord(bad); err == nil {
		t.Fatal("issuance receipt from a different namespace was accepted")
	}
	bad = second
	copyIssuance = *second.RevisionIssuance
	copyIssuance.IntentKey = strings.Repeat("x", 1025)
	bad.RevisionIssuance = &copyIssuance
	if err := ValidateRecord(bad); err == nil {
		t.Fatal("oversized revision intent key was accepted")
	}
}

func snapshotFixtureRecord(t *testing.T) (Record, map[string][]byte) {
	t.Helper()
	record := recordTestRecord()
	dataByRef := map[string][]byte{}
	for i := range record.ContextSources {
		source := &record.ContextSources[i]
		data := []byte("original source bytes for " + source.Kind + "\n")
		dataByRef[source.Ref] = data
	}
	var err error
	record.ContextSources, err = canonicalContextSources(record.ContextSources)
	if err != nil {
		t.Fatal(err)
	}
	return record, dataByRef
}

func findSnapshot(t *testing.T, snapshots []SourceSnapshot, ref, kind string) SourceSnapshot {
	t.Helper()
	for _, snapshot := range snapshots {
		if snapshot.SourceRef == ref && snapshot.SourceKind == kind {
			return snapshot
		}
	}
	t.Fatalf("missing source snapshot %s (%s)", ref, kind)
	return SourceSnapshot{}
}

func TestCaptureSourceSnapshotsUsesOriginalResponsibilityNotDigest(t *testing.T) {
	record, sources := snapshotFixtureRecord(t)
	updated, artifacts, err := CaptureSourceSnapshots(record, func(ref string) ([]byte, error) {
		data, ok := sources[ref]
		if !ok {
			return nil, fmt.Errorf("missing actual source %s", ref)
		}
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range updated.SourceSnapshots {
		if got, ok := artifacts[snapshot.Path]; !ok || string(got) != string(sources[snapshot.SourceRef]) {
			t.Fatalf("snapshot does not preserve original source %s", snapshot.SourceRef)
		}
	}
}
