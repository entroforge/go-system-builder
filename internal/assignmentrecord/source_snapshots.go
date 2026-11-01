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
}

// CaptureSourceSnapshots retains actual source bytes under this Assignment's
// own revision and a deterministic source ordinal. It performs no hashing;
// the Writer publishes the bytes and owns their cleanup.
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
	claims, err := sourceSnapshotClaims(record)
	if err != nil {
		return Record{}, nil, err
	}
	if len(claims) == 0 {
		return Record{}, nil, fmt.Errorf("Assignment Record has no source bytes to snapshot")
	}

	// Read a physical source only once even when the Record names it under
	// multiple semantic source kinds. The existing source identity lets both claims share this read.
	dataByRef := make(map[string][]byte, len(claims))
	claimsByRef := make(map[string][]sourceSnapshotClaim, len(claims))
	for _, claim := range claims {
		claimsByRef[claim.ref] = append(claimsByRef[claim.ref], claim)
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
		total += int64(len(data))
		dataByRef[ref] = append([]byte(nil), data...)
	}

	snapshots := make([]SourceSnapshot, 0, len(claims))
	artifacts := make(map[string][]byte, len(refs))
	paths := make(map[string]string, len(refs))
	for index, ref := range refs {
		paths[ref] = sourceSnapshotPath(record, fmt.Sprintf("source-%d", index+1))
	}
	for _, claim := range claims {
		data := dataByRef[claim.ref]
		artifactPath := paths[claim.ref]
		if previous, exists := artifacts[artifactPath]; exists {
			if !bytes.Equal(previous, data) {
				return Record{}, nil, fmt.Errorf("Assignment source path collision at %s", artifactPath)
			}
		} else {
			artifacts[artifactPath] = append([]byte(nil), data...)
		}
		snapshots = append(snapshots, SourceSnapshot{
			SourceRef:  claim.ref,
			SourceKind: claim.kind,
			Path:       artifactPath,
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

func sourceSnapshotPath(record Record, ordinal string) string {
	return path.Join(
		".claude", "evidence", record.RuntimeID,
		fmt.Sprintf("g%d", record.BaselineGeneration), "assignments", record.AssignmentID,
		"records", fmt.Sprintf("r%d", record.Revision), "sources", ordinal+".bin",
	)
}

func sourceSnapshotClaims(record Record) ([]sourceSnapshotClaim, error) {
	byIdentity := map[string]sourceSnapshotClaim{}
	add := func(ref, kind string) error {
		if strings.TrimSpace(ref) == "" || strings.TrimSpace(kind) == "" {
			return fmt.Errorf("Assignment source snapshot claim has an empty ref or kind")
		}
		claim := sourceSnapshotClaim{ref: ref, kind: kind}
		key := ref + "\x00" + kind
		byIdentity[key] = claim
		return nil
	}
	for _, source := range []ContextSource{record.Task, record.Manifest, record.AgentDefinition} {
		if err := add(source.Ref, source.Kind); err != nil {
			return nil, err
		}
	}
	for _, source := range record.ContextSources {
		if source.Kind == "check_profile" {
			// The containing manifest is captured as exact source bytes.
			continue
		}
		if err := add(source.Ref, source.Kind); err != nil {
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
		return false
	})
	return claims, nil
}

func sortSourceSnapshots(snapshots []SourceSnapshot) {
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].SourceRef != snapshots[j].SourceRef {
			return snapshots[i].SourceRef < snapshots[j].SourceRef
		}
		if snapshots[i].SourceKind != snapshots[j].SourceKind {
			return snapshots[i].SourceKind < snapshots[j].SourceKind
		}
		return false
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
