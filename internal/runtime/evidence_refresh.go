package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

var errEvidenceRefreshSuperseded = errors.New("evidence changed during refresh; recompute current bindings")

type fingerprintArtifact struct {
	Path           string `json:"path"`
	PreviousSHA256 string `json:"previous_sha256"`
	SHA256         string `json:"sha256"`
	Data           []byte `json:"data"`
}

func mutableEvidence(entry, state map[string]any, kinds map[string]bool) bool {
	kind, _ := entry["kind"].(string)
	baseline, _ := state["baseline"].(map[string]any)
	review, _ := state["review"].(map[string]any)
	gen, _ := integerField(entry, "baseline_generation")
	currentGen, _ := integerField(baseline, "generation")
	round, _ := integerField(entry, "review_round")
	currentRound, _ := integerField(review, "round")
	return kinds[kind] && entry["status"] == "valid" && gen == currentGen && (round == 0 || round == currentRound)
}

// prepareEvidenceClosure changes only typed references to current mutable
// evidence. Product subjects and archived/frozen containers are never traversed.
func prepareEvidenceClosure(root string, state map[string]any, kinds map[string]bool, read func(string) ([]byte, error), writable func(string) bool) ([]fingerprintArtifact, map[string][]byte, error) {
	allowed := map[string]bool{}
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		entry, ok := raw.(map[string]any)
		if !ok || !mutableEvidence(entry, state, kinds) {
			continue
		}
		path, _ := entry["path"].(string)
		if _, err := safeEvidencePath(root, path); err == nil {
			allowed[filepath.ToSlash(filepath.Clean(path))] = true
		}
	}
	// S10 explicitly types the auxiliary manifest as evidence, rather than
	// inferring mutability from a directory name or arbitrary *_sha256 field.
	for path := range allowed {
		raw, err := read(path)
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		if doc["kind"] == "acceptance" || doc["kind"] == "release_audit" {
			if ref, ok := doc["audit_manifest_path"].(string); ok {
				if _, err := safeEvidencePath(root, ref); err == nil {
					allowed[filepath.ToSlash(filepath.Clean(ref))] = true
				}
			}
		}
	}
	resolved := map[string][]byte{}
	visiting := map[string]bool{}
	var writes []fingerprintArtifact
	var resolve func(string) ([]byte, error)
	resolve = func(path string) ([]byte, error) {
		path = filepath.ToSlash(filepath.Clean(path))
		if data, ok := resolved[path]; ok {
			return data, nil
		}
		if visiting[path] {
			return nil, fmt.Errorf("cyclic evidence fingerprint reference: %s", path)
		}
		data, err := read(path)
		if err != nil {
			return nil, err
		}
		visiting[path] = true
		defer delete(visiting, path)
		var value any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if !writable(path) || !json.Valid(data) || decoder.Decode(&value) != nil {
			resolved[path] = data
			return data, nil
		}
		changed := false
		var visit func(any) error
		visit = func(node any) error {
			switch node := node.(type) {
			case map[string]any:
				for _, pair := range [][2]string{{"path", "sha256"}, {"audit_manifest_path", "audit_manifest_sha256"}} {
					ref, ok := node[pair[0]].(string)
					if !ok || !allowed[filepath.ToSlash(filepath.Clean(ref))] {
						continue
					}
					if _, ok := node[pair[1]].(string); !ok {
						continue
					}
					target, err := resolve(ref)
					if os.IsNotExist(err) {
						continue
					} // absence remains an ordinary semantic missing fact
					if err != nil {
						return err
					}
					sum := sha256Hex(target)
					if node[pair[1]] != sum {
						node[pair[1]] = sum
						changed = true
					}
				}
				keys := make([]string, 0, len(node))
				for key := range node {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					// These fields describe frozen subjects, not evidence cache bindings.
					if key == "subject_refs" || key == "frozen_subjects" || key == "baseline" || key == "bound_req" {
						continue
					}
					if err := visit(node[key]); err != nil {
						return err
					}
				}
			case []any:
				for _, child := range node {
					if err := visit(child); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := visit(value); err != nil {
			return nil, err
		}
		if changed {
			next, err := json.MarshalIndent(value, "", "  ")
			if err != nil {
				return nil, err
			}
			next = append(next, '\n')
			writes = append(writes, fingerprintArtifact{Path: path, PreviousSHA256: sha256Hex(data), SHA256: sha256Hex(next), Data: next})
			data = next
		}
		resolved[path] = data
		return data, nil
	}
	paths := make([]string, 0, len(allowed))
	for p := range allowed {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if _, err := resolve(path); err != nil && !os.IsNotExist(err) {
			return nil, nil, err
		}
	}
	return writes, resolved, nil
}

func applyFingerprintArtifacts(root string, writes []fingerprintArtifact) error {
	for _, write := range writes {
		path, err := safeEvidencePath(root, write.Path)
		if err != nil {
			return err
		}
		if sha256Hex(write.Data) != write.SHA256 {
			return fmt.Errorf("invalid pending evidence fingerprint: %s", write.Path)
		}
		path = filepath.Join(root, filepath.FromSlash(path))
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256Hex(data)
		if sum == write.SHA256 {
			continue
		}
		if sum != write.PreviousSHA256 {
			return fmt.Errorf("%w: %s", errEvidenceRefreshSuperseded, write.Path)
		}
		if err := atomicWriteBytes(path, write.Data, ".evidence-refresh-*"); err != nil {
			return err
		}
	}
	return nil
}
