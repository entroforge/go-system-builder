package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// FingerprintTriple is the RC-10 Step B convergence target of the eight
// sha256 fingerprint families catalogued in L4-runtime-control-plane §3. The
// families collapse onto three aggregate hashes over one shared recipe —
// "sha256 over the sorted `path:sha256` line list, joined by newlines", the
// same aggregation SubjectDigest (internal/review/plan.go) and WorkspaceDigest
// (internal/review/workspace.go) already use:
//
//   - StateHash     aggregates the registered `documents[]` rows
//     (the registered-document fingerprint family);
//   - EvidenceHash  aggregates the `evidence[]` rows (the evidence-chain
//     family);
//   - BaselineHash  aggregates documents + evidence + `bound_req` together —
//     the freeze surface a pause/seal/handoff checkpoint must pin.
//
// The triple is a derived projection: it never replaces the per-family
// hashes stored in state (existing 8-family validation is unchanged); it
// gives new consumers one canonical number triple to re-verify instead of
// re-deriving eight family rules. A row missing either `path` or `sha256`
// is skipped — an unregistered reference contributes nothing rather than
// poisoning the aggregate.
//
// RC-17 decision (audit "cut-or-complete", resolved as COMPLETE, not
// deprecate): the eight sha256 fingerprint families stay — they are the
// per-surface validation anchors consumed by transition gates and recovery,
// and migrating every consumer is a per-consumer transaction out of RC-17
// scope (see L4-runtime-control-plane §3 convergence note). ComputeTriple /
// ComputeRevisionPair are therefore retained as the observation layer on top
// of the families, with these standing rules:
//   - new consumers must consume the triple / pair, never mint a ninth
//     family;
//   - the triple is derived, never persisted into runtime state — it lives
//     only in housekeeping output (FingerprintResult.Triple) and diagnostics;
//   - the family layer remains the sole enforcement surface: gate checks
//     that need a fingerprint compare a stored family hash, not the triple.
type FingerprintTriple struct {
	StateHash    string `json:"state_hash"`
	EvidenceHash string `json:"evidence_hash"`
	BaselineHash string `json:"baseline_hash"`
}

// StateRevision and EvidenceGeneration alias the two axes of the RC-10 Step B
// revision convergence (L4-runtime-control-plane §3: revision collapses to
// `state_revision + evidence_generation`). They are aliases, not distinct
// types, so existing int-typed call sites compose without conversion.
type (
	StateRevision      = int
	EvidenceGeneration = int
)

// ComputeTriple derives the RC-10 Step B fingerprint triple from a runtime
// state map. It is a pure, total function: a missing or empty collection
// aggregates to the empty-input sha256 (the same convention WorkspaceDigest
// uses for an absent workspace), and malformed rows are skipped.
func ComputeTriple(state map[string]any) FingerprintTriple {
	documentLines := fingerprintReferenceLines(state["documents"])
	evidenceLines := fingerprintReferenceLines(state["evidence"])

	baseline := append([]string{}, documentLines...)
	baseline = append(baseline, evidenceLines...)
	baseline = append(baseline, fingerprintReferenceLines(state["bound_req"])...)
	sort.Strings(baseline)

	return FingerprintTriple{
		StateHash:    fingerprintAggregate(documentLines),
		EvidenceHash: fingerprintAggregate(evidenceLines),
		BaselineHash: fingerprintAggregate(baseline),
	}
}

// ComputeRevisionPair derives the RC-10 Step B revision binary from a runtime
// state map: the state CAS revision and the evidence generation
// (`baseline.generation`, 0 when the baseline is uncaptured or absent).
// Absent vs present-but-zero is distinguished by key existence — both decode
// to 0 (audit R-L7: a freshly bound runtime legitimately holds revision 0, so
// the value cannot be used alone to signal "no revision recorded"; callers
// needing that distinction must check key presence on the source state, since
// the pair's int return cannot carry it).
func ComputeRevisionPair(state map[string]any) (StateRevision, EvidenceGeneration) {
	revision := 0
	if _, ok := state["revision"]; ok {
		revision = fingerprintTolerantInt(state["revision"])
	}
	generation := 0
	if baseline, ok := state["baseline"].(map[string]any); ok {
		generation = fingerprintTolerantInt(baseline["generation"])
	}
	return revision, generation
}

// fingerprintAggregate hashes the sorted `path:sha256` line list. An empty
// list hashes the empty input, matching WorkspaceDigest's cold-start
// baseline convention.
func fingerprintAggregate(lines []string) string {
	sorted := append([]string{}, lines...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:])
}

// fingerprintReferenceLines extracts `path:sha256` lines from either an
// array of reference rows (`documents[]`, `evidence[]`) or a single
// reference object (`bound_req`). Rows missing path or sha256 are skipped.
func fingerprintReferenceLines(value any) []string {
	switch rows := value.(type) {
	case []any:
		lines := make([]string, 0, len(rows))
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if line, ok := fingerprintRowLine(row); ok {
				lines = append(lines, line)
			}
		}
		return lines
	case map[string]any:
		if line, ok := fingerprintRowLine(rows); ok {
			return []string{line}
		}
		return nil
	default:
		return nil
	}
}

func fingerprintRowLine(row map[string]any) (string, bool) {
	path, _ := row["path"].(string)
	sha, _ := row["sha256"].(string)
	if strings.TrimSpace(path) == "" || strings.TrimSpace(sha) == "" {
		return "", false
	}
	return path + ":" + sha, true
}

// fingerprintTolerantInt reads a numeric JSON field without failing: JSON
// decoding produces float64 (or json.Number under UseNumber), and a missing
// or malformed value reads as zero. This mirrors integerField's type
// coverage without its error contract, because the triple/pair are derived
// projections that must stay total.
func fingerprintTolerantInt(value any) int {
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
