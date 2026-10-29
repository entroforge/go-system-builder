package buildsurface

import (
	"fmt"
	"sort"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
)

// FrozenSubjects projects the exact TASK sources and complete product surface.
// Deleted subjects pin the snapshot's deletion preimage, not current file bytes.
// The original Snapshot reader/verifier owns Git provenance and deletion proof.
func FrozenSubjects(snapshot *Snapshot) ([]reviewmeta.FrozenSubject, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("canonical product baseline is missing")
	}
	byPath := make(map[string]reviewmeta.FrozenSubject, len(snapshot.Tasks)+len(snapshot.Paths))
	for _, task := range snapshot.Tasks {
		if task.Path == "" || task.SHA256 == "" {
			return nil, fmt.Errorf("canonical product baseline has an incomplete TASK source")
		}
		if previous, exists := byPath[task.Path]; exists && previous.SHA256 != task.SHA256 {
			return nil, fmt.Errorf("canonical product baseline has conflicting frozen bytes for %s", task.Path)
		}
		byPath[task.Path] = reviewmeta.FrozenSubject{Path: task.Path, SHA256: task.SHA256, Kind: "task"}
	}
	for _, path := range snapshot.Paths {
		if path.Path == "" || path.SHA256 == "" {
			return nil, fmt.Errorf("canonical product baseline has an incomplete changed path")
		}
		kind := "changed_surface"
		switch path.Status {
		case "deleted":
			kind = "deleted"
		case "added", "modified":
		default:
			return nil, fmt.Errorf("canonical product baseline path %s has unknown status %q", path.Path, path.Status)
		}
		if previous, exists := byPath[path.Path]; exists {
			if previous.SHA256 != path.SHA256 || (previous.Kind == "task" && kind == "deleted") || (previous.Kind != "task" && previous.Kind != kind) {
				return nil, fmt.Errorf("TASK source and changed surface disagree on frozen bytes or existence for %s", path.Path)
			}
			continue
		}
		byPath[path.Path] = reviewmeta.FrozenSubject{Path: path.Path, SHA256: path.SHA256, Kind: kind}
	}
	out := make([]reviewmeta.FrozenSubject, 0, len(byPath))
	for _, subject := range byPath {
		out = append(out, subject)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
