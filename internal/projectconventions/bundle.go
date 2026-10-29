package projectconventions

import (
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/schema"
)

type Bundle struct {
	ResolverVersion string       `json:"resolver_version"`
	TaskConventions []Projection `json:"task_conventions"`
}

// Sources verifies immutable projection bindings without resolving current
// choices or granting permissions. Each task path comes from original authority.
func (b *Bundle) Sources(tasks map[string]bool) (map[string]string, error) {
	result := map[string]string{}
	if b == nil {
		return result, nil
	}
	if b.ResolverVersion != Version {
		return nil, fmt.Errorf("unknown frozen conventions resolver version")
	}
	for _, row := range b.TaskConventions {
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("task-conventions.schema.json", data); err != nil {
			return nil, err
		}
		if row.ResolverVersion != b.ResolverVersion || !tasks[row.TaskRef] {
			return nil, fmt.Errorf("convention projection belongs to another registered TASK")
		}
		for _, source := range row.Sources {
			if source.Status != "readable" {
				continue
			}
			if !safePath(source.Path, false) || !digestPattern.MatchString(source.SHA256) {
				return nil, fmt.Errorf("frozen convention source is unsafe")
			}
			if old, ok := result[source.Path]; ok && old != source.SHA256 {
				return nil, fmt.Errorf("convention source hashes conflict")
			}
			result[source.Path] = source.SHA256
		}
	}
	return result, nil
}
func (b *Bundle) ValidateInputs(inputs map[string]string) error {
	if b == nil {
		return nil
	}
	tasks := map[string]bool{}
	for _, row := range b.TaskConventions {
		if inputs[row.TaskRef] != "" {
			tasks[row.TaskRef] = true
		}
	}
	sources, err := b.Sources(tasks)
	if err != nil {
		return err
	}
	for path, hash := range sources {
		if inputs[path] != hash {
			return fmt.Errorf("frozen context lacks convention source %s", path)
		}
	}
	return nil
}

// CommonScope handles canonical prefixes and the common terminal /** form.
// Other wildcard intersections remain unresolved instead of broadening them.
func CommonScope(task, assignment []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, a := range task {
		for _, b := range assignment {
			aa, bb := a, b
			if len(aa) > 3 && aa[len(aa)-3:] == "/**" {
				aa = aa[:len(aa)-3]
			}
			if len(bb) > 3 && bb[len(bb)-3:] == "/**" {
				bb = bb[:len(bb)-3]
			}
			if !safePath(aa, true) || !safePath(bb, true) || !overlaps(aa, bb) {
				continue
			}
			chosen := aa
			if aa == "." || len(bb) > len(aa) {
				chosen = bb
			}
			if !seen[chosen] {
				seen[chosen] = true
				result = append(result, chosen)
			}
		}
	}
	return result
}
