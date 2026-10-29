package assignmentrecord

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
)

const (
	maxSourceSnapshotBytes      = 16 << 20
	maxSourceSnapshotBundleSize = 64 << 20
)

type sourceSnapshotClaim struct {
	ref  string
	kind string
	sha  string
}

// CaptureSourceSnapshots reads and hashes the original permission sources
// named by a validated Record. It performs no filesystem writes. Returned
// bytes are keyed by their content-addressed Runtime evidence paths for the
// owning Writer to publish within its mutation and pending-cleanup boundary.
//
// CheckProfile fragments are covered by the exact manifest snapshot. They are
// canonicalized values embedded in that file, not independent repository
// files, so this function does not serialize a derived Profile in place of its
// original manifest bytes.
//
// The supplied reader must resolve gsb-embedded refs through
// assignmentinputs.Read and ordinary refs through the caller's verified source reader.
func CaptureSourceSnapshots(record Record, read func(string) ([]byte, error)) (Record, map[string][]byte, error) {
	if read == nil {
		return Record{}, nil, fmt.Errorf("Assignment source snapshot reader is required")
	}
	if record.SourceSnapshots != nil {
		return Record{}, nil, fmt.Errorf("Assignment Record already has source snapshots")
	}
	if err := ValidateRecord(record); err != nil {
		return Record{}, nil, fmt.Errorf("validate Assignment Record before source capture: %w", err)
	}
	if err := validateSnapshotContextCoverage(record); err != nil {
		return Record{}, nil, err
	}
	claims, err := sourceSnapshotClaims(record)
	if err != nil {
		return Record{}, nil, err
	}
	if len(claims) == 0 {
		return Record{}, nil, fmt.Errorf("Assignment Record has no source bytes to snapshot")
	}

	// Read a physical source only once even when the Record names it under
	// multiple semantic source kinds. The ref-to-digest check in claim
	// collection guarantees those reads have one expected byte identity.
	dataByRef := make(map[string][]byte, len(claims))
	shaByRef := make(map[string]string, len(claims))
	claimsByRef := make(map[string][]sourceSnapshotClaim, len(claims))
	for _, claim := range claims {
		claimsByRef[claim.ref] = append(claimsByRef[claim.ref], claim)
		shaByRef[claim.ref] = claim.sha
	}
	refs := make([]string, 0, len(claimsByRef))
	for ref := range claimsByRef {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	var total int64
	for _, ref := range refs {
		data, err := read(ref)
		if err != nil {
			return Record{}, nil, fmt.Errorf("read Assignment source %s: %w", ref, err)
		}
		if len(data) > maxSourceSnapshotBytes {
			return Record{}, nil, fmt.Errorf("Assignment source %s exceeds %d bytes", ref, maxSourceSnapshotBytes)
		}
		if int64(len(data)) > maxSourceSnapshotBundleSize-total {
			return Record{}, nil, fmt.Errorf("Assignment source snapshot bundle exceeds %d bytes", maxSourceSnapshotBundleSize)
		}
		if got := digest(data); got != shaByRef[ref] {
			return Record{}, nil, fmt.Errorf("Assignment source %s changed: got sha256 %s, want %s", ref, got, shaByRef[ref])
		}
		total += int64(len(data))
		dataByRef[ref] = append([]byte(nil), data...)
	}

	snapshots := make([]SourceSnapshot, 0, len(claims))
	artifacts := make(map[string][]byte, len(refs))
	for _, claim := range claims {
		data := dataByRef[claim.ref]
		sha := digest(data)
		artifactPath := sourceSnapshotPath(record, sha)
		if previous, exists := artifacts[artifactPath]; exists {
			if !bytes.Equal(previous, data) {
				return Record{}, nil, fmt.Errorf("content-addressed Assignment source path collision at %s", artifactPath)
			}
		} else {
			artifacts[artifactPath] = append([]byte(nil), data...)
		}
		snapshots = append(snapshots, SourceSnapshot{
			SourceRef:    claim.ref,
			SourceKind:   claim.kind,
			SourceSHA256: claim.sha,
			Path:         artifactPath,
			SHA256:       sha,
		})
	}
	sortSourceSnapshots(snapshots)

	updated := record
	updated.SourceSnapshots = snapshots
	if err := ValidateRecord(updated); err != nil {
		return Record{}, nil, fmt.Errorf("validate captured Assignment source snapshots: %w", err)
	}
	return updated, artifacts, nil
}

func sourceSnapshotPath(record Record, sha string) string {
	return path.Join(
		".claude", "evidence", record.RuntimeID,
		fmt.Sprintf("g%d", record.BaselineGeneration), "assignments", record.AssignmentID,
		"records", fmt.Sprintf("r%d", record.Revision), "sources", sha+".bin",
	)
}

func sourceSnapshotClaims(record Record) ([]sourceSnapshotClaim, error) {
	byIdentity := map[string]sourceSnapshotClaim{}
	shaByRef := map[string]string{}
	add := func(ref, kind, sha string) error {
		if strings.TrimSpace(ref) == "" || strings.TrimSpace(kind) == "" {
			return fmt.Errorf("Assignment source snapshot claim has an empty ref or kind")
		}
		if err := validateSHA(sha); err != nil {
			return fmt.Errorf("Assignment source %s: %w", ref, err)
		}
		if previous, ok := shaByRef[ref]; ok && previous != sha {
			return fmt.Errorf("Assignment source %s is claimed with conflicting SHA-256 digests", ref)
		}
		shaByRef[ref] = sha
		claim := sourceSnapshotClaim{ref: ref, kind: kind, sha: sha}
		key := ref + "\x00" + kind + "\x00" + sha
		byIdentity[key] = claim
		return nil
	}
	for _, source := range []ContextSource{record.Task, record.Manifest, record.AgentDefinition} {
		if err := add(source.Ref, source.Kind, source.SHA256); err != nil {
			return nil, err
		}
	}
	for _, source := range record.ContextSources {
		if source.Kind == "check_profile" {
			// The containing manifest is captured as exact source bytes.
			continue
		}
		if err := add(source.Ref, source.Kind, source.SHA256); err != nil {
			return nil, err
		}
	}
	claims := make([]sourceSnapshotClaim, 0, len(byIdentity))
	for _, claim := range byIdentity {
		claims = append(claims, claim)
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].ref != claims[j].ref {
			return claims[i].ref < claims[j].ref
		}
		if claims[i].kind != claims[j].kind {
			return claims[i].kind < claims[j].kind
		}
		return claims[i].sha < claims[j].sha
	})
	return claims, nil
}

// validateSnapshotContextCoverage ensures permission-bearing source refs that
// are structured outside ContextSources still have their exact source rows.
// Historical Records without snapshots retain their original validation
// behavior; new capture and Records that carry snapshots run this stricter
// coverage check.
func validateSnapshotContextCoverage(record Record) error {
	for _, skillID := range record.SkillRefs {
		count := 0
		for _, source := range record.ContextSources {
			if source.Kind == "authorized_skill" && source.ID == skillID {
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("Assignment Skill %s must have exactly one frozen source ref", skillID)
		}
	}
	for _, profile := range record.CheckProfiles {
		profileRef := record.Manifest.Ref + "#assignments/" + record.AssignmentID + "/check_profiles/" + profile.ID
		if !hasExactContextSource(record.ContextSources, ContextSource{
			Kind: "check_profile", Ref: profileRef, ID: profile.ID, Version: profile.Version, SHA256: profile.Digest(),
		}) {
			return fmt.Errorf("Assignment CheckProfile %s is missing its frozen manifest source binding", profile.ID)
		}
		for _, dependency := range profile.Dependencies {
			if !hasExactContextSource(record.ContextSources, ContextSource{Kind: "check_profile_dependency", Ref: dependency.Path, SHA256: dependency.SHA256}) {
				return fmt.Errorf("Assignment CheckProfile %s dependency %s is missing its frozen source ref", profile.ID, dependency.Path)
			}
		}
		for _, fixture := range profile.Fixtures {
			if !hasExactContextSource(record.ContextSources, ContextSource{Kind: "check_profile_fixture", Ref: fixture.Path, SHA256: fixture.SHA256}) {
				return fmt.Errorf("Assignment CheckProfile %s fixture %s is missing its frozen source ref", profile.ID, fixture.Path)
			}
		}
	}
	if record.ClosingContract != nil {
		for _, check := range record.ClosingContract.WorkerChecks {
			for _, dependency := range check.Profile.Dependencies {
				if !hasExactContextSource(record.ContextSources, ContextSource{Kind: "worker_check_profile_dependency", Ref: dependency.Path, ID: check.Profile.ID, SHA256: dependency.SHA256}) {
					return fmt.Errorf("Worker CheckProfile %s dependency %s is missing its frozen source ref", check.Profile.ID, dependency.Path)
				}
			}
			for _, fixture := range check.Profile.Fixtures {
				if !hasExactContextSource(record.ContextSources, ContextSource{Kind: "worker_check_profile_fixture", Ref: fixture.Path, ID: check.Profile.ID, SHA256: fixture.SHA256}) {
					return fmt.Errorf("Worker CheckProfile %s fixture %s is missing its frozen source ref", check.Profile.ID, fixture.Path)
				}
			}
		}
	}
	return nil
}

func hasExactContextSource(sources []ContextSource, expected ContextSource) bool {
	for _, source := range sources {
		if source == expected {
			return true
		}
	}
	return false
}

func validateSourceSnapshots(record Record) error {
	if len(record.SourceSnapshots) == 0 {
		return fmt.Errorf("Assignment Record source_snapshots must not be empty when present")
	}
	claims, err := sourceSnapshotClaims(record)
	if err != nil {
		return err
	}
	if len(record.SourceSnapshots) != len(claims) {
		return fmt.Errorf("Assignment Record source snapshot rows do not cover its exact source refs")
	}
	canonical := append([]SourceSnapshot(nil), record.SourceSnapshots...)
	sortSourceSnapshots(canonical)
	for i, claim := range claims {
		snapshot := canonical[i]
		if snapshot.SourceRef != claim.ref || snapshot.SourceKind != claim.kind || snapshot.SourceSHA256 != claim.sha {
			return fmt.Errorf("Assignment Record source snapshot metadata differs from source claim %s", claim.ref)
		}
		if err := validateSHA(snapshot.SHA256); err != nil {
			return fmt.Errorf("Assignment source snapshot %s: %w", snapshot.Path, err)
		}
		if snapshot.SHA256 != claim.sha || snapshot.Path != sourceSnapshotPath(record, snapshot.SHA256) {
			return fmt.Errorf("Assignment source snapshot path/digest is not content-addressed for %s", claim.ref)
		}
	}
	for i := range canonical {
		if canonical[i] != record.SourceSnapshots[i] {
			return fmt.Errorf("Assignment Record source snapshots are not canonical")
		}
	}
	return nil
}

func sortSourceSnapshots(snapshots []SourceSnapshot) {
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].SourceRef != snapshots[j].SourceRef {
			return snapshots[i].SourceRef < snapshots[j].SourceRef
		}
		if snapshots[i].SourceKind != snapshots[j].SourceKind {
			return snapshots[i].SourceKind < snapshots[j].SourceKind
		}
		return snapshots[i].SourceSHA256 < snapshots[j].SourceSHA256
	})
}

func validateRevisionIssuance(record Record, issuance RevisionIssuance) error {
	if record.Revision < 2 {
		return fmt.Errorf("Assignment revision_issuance is only valid for revision 2 or later")
	}
	if issuance.IntentKey == "" || len(issuance.IntentKey) > 1024 || strings.TrimSpace(issuance.IntentKey) != issuance.IntentKey {
		return fmt.Errorf("Assignment revision intent_key must be non-empty, trimmed, and at most 1024 bytes")
	}
	for _, r := range issuance.IntentKey {
		if unicode.IsControl(r) {
			return fmt.Errorf("Assignment revision intent_key contains a control character")
		}
	}
	if err := validateSHA(issuance.RequestDigest); err != nil {
		return fmt.Errorf("Assignment revision request_digest: %w", err)
	}
	if err := validateSHA(issuance.PreviousRecordSHA256); err != nil {
		return fmt.Errorf("Assignment revision previous_record_sha256: %w", err)
	}
	previous := issuance.PreviousRecord
	if !recordRuntimeIDRE.MatchString(previous.RuntimeID) || previous.BaselineGeneration < 1 ||
		!recordAssignmentIDRE.MatchString(previous.AssignmentID) || previous.Revision < 1 {
		return fmt.Errorf("Assignment revision previous_record reference is invalid")
	}
	if previous.RuntimeID != record.RuntimeID || previous.BaselineGeneration != record.BaselineGeneration ||
		previous.AssignmentID != record.AssignmentID || previous.Revision != record.Revision-1 {
		return fmt.Errorf("Assignment revision previous_record must be the immediately preceding revision in the same namespace")
	}
	return nil
}
