// Package evidence — semantic attestation (RC-14).
//
// ValidateRefs is the unified evidence semantic gate shared by S8 hypothesis/
// result, S9 TargetedReverification, and S10 coverage inventory. It enforces
// content-addressed existence (status, generation, invalidated_by, SHA) and
// optional kind / round / self-proof constraints. Callers pass the runtime
// snapshot state map so this low-level package does not import runtime
// (runtime already imports evidence).
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RefsOptions configures ValidateRefs.
type RefsOptions struct {
	// Root is the repository root for file reads. Empty disables on-disk SHA
	// verification (metadata-only mode).
	Root string
	// RequireKinds is a whitelist of evidence kinds. Empty means any registered
	// kind is accepted.
	RequireKinds []string
	// RequireReviewRound, when >0, requires each referenced evidence entry to
	// carry review_round == this value.
	RequireReviewRound int
	// ForbidSelfID is an evidence id that refs must not cite (self-proof).
	ForbidSelfID string
}

// ValidateRefs validates that every ref in refs is a current, valid, SHA-
// verified runtime evidence entry. The checks are:
//
//  1. entry exists with id == ref
//  2. entry.status == "valid" and entry.invalidated_by == nil
//  3. entry.baseline_generation == state baseline.generation
//  4. entry kind in RequireKinds when non-empty
//  5. entry review_round == RequireReviewRound when >0
//  6. ref != ForbidSelfID
//  7. artifact file exists under Root, is readable, and its sha256 matches
//     entry.sha256 (when Root != "" and entry.path != "")
//
// Refs that look like external execution anchors (contain "://") are treated as execution evidence:
// they are validated for non-emptiness but do not require a runtime evidence
// index entry. This keeps existing tests and command/trace refs working while
// still catching phantom runtime evidence ids that claim to be index entries.
func ValidateRefs(state map[string]any, refs []string, opts RefsOptions) error {
	if len(refs) == 0 {
		return fmt.Errorf("evidence_refs must contain at least one reference")
	}
	// Build index of evidence entries by id.
	rawEvidence, _ := state["evidence"].([]any)
	byID := make(map[string]map[string]any, len(rawEvidence))
	for _, raw := range rawEvidence {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		id, _ := entry["id"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		byID[strings.TrimSpace(id)] = entry
	}
	baselineGen := nestedInt(state, "baseline", "generation")

	kindWhitelist := make(map[string]struct{}, len(opts.RequireKinds))
	for _, k := range opts.RequireKinds {
		k = strings.TrimSpace(k)
		if k != "" {
			kindWhitelist[k] = struct{}{}
		}
	}

	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return fmt.Errorf("evidence_ref %q is empty", ref)
		}
		if ref == strings.TrimSpace(opts.ForbidSelfID) && ref != "" {
			return fmt.Errorf("evidence_ref %q must not self-reference the envelope", ref)
		}
		// Execution-anchor refs (scheme://) are not runtime
		// evidence index entries; they are validated for shape only.
		// File-style `evidence/…` paths are runtime evidence ids and must be
		// validated as index entries (RC-14: phantom `evidence/phantom.json`
		// must be rejected).
		if strings.Contains(ref, "://") {
			continue
		}
		entry, ok := byID[ref]
		if !ok {
			return fmt.Errorf("evidence_ref %q is not a valid current-generation evidence id; register the evidence with `runtime evidence add` or use an execution anchor like test://", ref)
		}
		status, _ := entry["status"].(string)
		if strings.TrimSpace(status) != "valid" {
			return fmt.Errorf("evidence_ref %q status is %q, want valid", ref, status)
		}
		if v := entry["invalidated_by"]; v != nil {
			if str, ok := v.(string); ok {
				if strings.TrimSpace(str) != "" {
					return fmt.Errorf("evidence_ref %q is invalidated by %q", ref, str)
				}
				// Empty string is treated as nil (not invalidated).
			} else {
				return fmt.Errorf("evidence_ref %q is invalidated", ref)
			}
		}
		entryGen := intValue(entry["baseline_generation"])
		if entryGen != baselineGen {
			return fmt.Errorf("evidence_ref %q baseline_generation %d does not match current %d", ref, entryGen, baselineGen)
		}
		kind, _ := entry["kind"].(string)
		if len(kindWhitelist) > 0 {
			if _, ok := kindWhitelist[strings.TrimSpace(kind)]; !ok {
				return fmt.Errorf("evidence_ref %q kind %q is not in required kinds %v", ref, kind, opts.RequireKinds)
			}
		}
		if opts.RequireReviewRound > 0 {
			round := intValue(entry["review_round"])
			if round != opts.RequireReviewRound {
				return fmt.Errorf("evidence_ref %q review_round %d does not match required %d", ref, round, opts.RequireReviewRound)
			}
		}
		// On-disk SHA verification.
		if strings.TrimSpace(opts.Root) != "" {
			path, _ := entry["path"].(string)
			path = strings.TrimSpace(path)
			if path == "" {
				return fmt.Errorf("evidence_ref %q has empty path", ref)
			}
			sha, _ := entry["sha256"].(string)
			sha = strings.TrimSpace(sha)
			if sha == "" {
				return fmt.Errorf("evidence_ref %q has empty sha256", ref)
			}
			// Resolve under root (defensive: paths are already normalized).
			abs := filepath.Join(opts.Root, filepath.FromSlash(path))
			data, err := os.ReadFile(abs)
			if err != nil {
				return fmt.Errorf("evidence_ref %q artifact %q is missing or unreadable: %v", ref, path, err)
			}
			actual := sha256HexBytes(data)
			if actual != sha {
				return fmt.Errorf("evidence_ref %q sha256 drifted: expected %s but disk is %s", ref, sha, actual)
			}
		}
	}
	return nil
}

func nestedInt(state map[string]any, keys ...string) int {
	cur := state
	for i, key := range keys {
		if i == len(keys)-1 {
			return intValue(cur[key])
		}
		next, _ := cur[key].(map[string]any)
		if next == nil {
			return 0
		}
		cur = next
	}
	return 0
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
