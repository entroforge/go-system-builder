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
		Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{{Path: "internal/profile-input.txt", SHA256: digest(depBytes)}},
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
		ContextSource{Kind: "manifest_document", Ref: record.Task.Ref, ID: record.Task.ID, Version: record.Task.Version, SHA256: record.Task.SHA256},
		ContextSource{Kind: "frozen_input", Ref: "docs/context/decision.md", SHA256: digest([]byte("same content as a second source ref\n"))},
		ContextSource{Kind: "check_profile", Ref: profileRef, ID: profile.ID, Version: profile.Version, SHA256: profile.Digest()},
		ContextSource{Kind: "check_profile_dependency", Ref: "internal/profile-input.txt", SHA256: digest(depBytes)},
		ContextSource{Kind: "embedded_context_profile", Ref: profileSourceRef, SHA256: digest(profileSourceBytes)},
		ContextSource{Kind: "embedded_schema", Ref: profileSchemaRef, SHA256: digest(profileSchemaBytes)},
	)
	contextBytes := []byte("same content as a second source ref\n")
	for i := range record.ContextSources {
		if record.ContextSources[i].Kind == "frozen_input" {
			record.ContextSources[i].SHA256 = digest(contextBytes)
		}
	}
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
	if taskRow.Path != docRow.Path || taskRow.SHA256 != docRow.SHA256 {
		t.Fatal("same source bytes under two semantic kinds did not share one content-addressed artifact")
	}

	encoded, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("assignment-record.schema.json", encoded); err != nil {
		t.Fatalf("schema rejected captured source snapshots: %v", err)
	}
}

func TestCaptureSourceSnapshotsPreservesLegacyRecordJSONAndRejectsDrift(t *testing.T) {
	legacy := recordTestRecord()
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "source_snapshots") || strings.Contains(string(encoded), "revision_issuance") {
		t.Fatalf("legacy Record acquired optional fields: %s", encoded)
	}
	legacyDigest, err := legacy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	decoded := Record{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	decodedDigest, err := decoded.Digest()
	if err != nil || decodedDigest != legacyDigest {
		t.Fatalf("optional fields changed legacy digest: before %s, after %s, err %v", legacyDigest, decodedDigest, err)
	}

	record, sources := snapshotFixtureRecord(t)
	if _, _, err := CaptureSourceSnapshots(record, func(ref string) ([]byte, error) {
		data := sources[ref]
		if ref == record.AgentDefinition.Ref {
			return []byte("drifted role source"), nil
		}
		return data, nil
	}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed historical source error = %v, want source drift rejection", err)
	}
}

func TestCaptureSourceSnapshotsRejectsConflictingRefsAndBounds(t *testing.T) {
	record, sources := snapshotFixtureRecord(t)
	conflict := record
	conflict.ContextSources = append([]ContextSource(nil), record.ContextSources...)
	conflict.ContextSources = append(conflict.ContextSources, ContextSource{
		Kind: "frozen_input", Ref: record.Task.Ref, SHA256: strings.Repeat("d", 64),
	})
	conflict.ContextSources, _ = canonicalContextSources(conflict.ContextSources)
	if _, _, err := CaptureSourceSnapshots(conflict, func(ref string) ([]byte, error) { return sources[ref], nil }); err == nil {
		t.Fatal("same ref with conflicting digests was accepted")
	}

	oversized, oversizedSources := snapshotFixtureRecord(t)
	large := make([]byte, maxSourceSnapshotBytes+1)
	large[0] = 'x'
	for i := range oversized.ContextSources {
		if oversized.ContextSources[i].Kind == "task" {
			oversized.ContextSources[i].SHA256 = digest(large)
		}
	}
	oversized.Task.SHA256 = digest(large)
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
		tooMany.ContextSources = append(tooMany.ContextSources, ContextSource{Kind: "frozen_input", Ref: ref, SHA256: digest(data)})
	}
	tooMany.ContextSources, _ = canonicalContextSources(tooMany.ContextSources)
	if _, _, err := CaptureSourceSnapshots(tooMany, func(ref string) ([]byte, error) { return totalSources[ref], nil }); err == nil || !strings.Contains(err.Error(), "bundle exceeds") {
		t.Fatalf("oversized bundle error = %v, want total bound rejection", err)
	}
}

func TestCaptureSourceSnapshotsRequiresProfileDependenciesInContextSources(t *testing.T) {
	record, sources := snapshotFixtureRecord(t)
	depBytes := []byte("dependency")
	profile := checkexec.Profile{
		ID: "profile-unit", Version: "1.0.0", CheckIndex: 0, Command: "go test ./...",
		Location: "worker", TimeoutSeconds: 30, EnvironmentPolicy: "declared_only",
		Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{{Path: "internal/profile-input.txt", SHA256: digest(depBytes)}},
		Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never",
	}
	record.RequiredChecks = []string{"go test ./..."}
	record.CheckProfiles = []checkexec.Profile{profile}
	record.ContextSources = append(record.ContextSources, ContextSource{
		Kind: "check_profile", Ref: record.Manifest.Ref + "#assignments/" + record.AssignmentID + "/check_profiles/" + profile.ID,
		ID: profile.ID, Version: profile.Version, SHA256: profile.Digest(),
	})
	record.ContextSources, _ = canonicalContextSources(record.ContextSources)
	sources["internal/profile-input.txt"] = depBytes
	if _, _, err := CaptureSourceSnapshots(record, func(ref string) ([]byte, error) { return sources[ref], nil }); err == nil || !strings.Contains(err.Error(), "dependency") {
		t.Fatalf("missing profile dependency context ref error = %v", err)
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
		IntentKey: "intent-2026-10-14-001", RequestDigest: strings.Repeat("d", 64),
		PreviousRecord: previous.Reference(), PreviousRecordSHA256: mustDigestRecord(t, previous),
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
		source.SHA256 = digest(data)
		switch source.Kind {
		case "manifest":
			record.Manifest.SHA256 = source.SHA256
		case "task":
			record.Task.SHA256 = source.SHA256
		case "agent_definition":
			record.AgentDefinition.SHA256 = source.SHA256
		}
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

func mustDigestRecord(t *testing.T, record Record) string {
	t.Helper()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
