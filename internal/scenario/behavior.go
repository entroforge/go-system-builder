package scenario

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

type BehaviorSource struct {
	Path string `json:"path"`
}

type BehaviorExample struct {
	Case    Case             `json:"case"`
	Inputs  map[string]any   `json:"inputs"`
	Fixture Fixture          `json:"fixture"`
	Sources []BehaviorSource `json:"sources"`
}

// ProjectBehaviorWithFiles uses the actual readable scenario cases and source
// materials. It executes no scenario, test or browser and writes no file.
func ProjectBehaviorWithFiles(root, module string, files fileview.Reader) (map[string]BehaviorExample, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	recorded := &behaviorReader{Reader: files, root: root, paths: map[string]struct{}{}}
	files = recorded
	source, err := loadSourcePackageWithFiles(root, module, files)
	if err != nil {
		return nil, err
	}
	outputs, err := buildOutputs(source)
	if err != nil {
		return nil, err
	}
	casesPath := filepath.Join(root, prototypeRoot, module, casesFile)
	coveragePath := filepath.Join(root, prototypeRoot, module, coverageFile)
	casesBytes, err := files.ReadFile(casesPath)
	if err != nil {
		return nil, err
	}
	coverageBytes, err := files.ReadFile(coveragePath)
	if err != nil {
		return nil, err
	}
	actual, err := validateOutputData(casesBytes, coverageBytes, outputs)
	if err != nil {
		return nil, err
	}
	sources := []BehaviorSource{}
	for path := range recorded.paths {
		sources = append(sources, BehaviorSource{Path: path})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	values := map[string]map[string]any{}
	for _, fact := range source.model.Facts {
		values[fact.ID] = map[string]any{}
		for _, partition := range fact.Partitions {
			values[fact.ID][partition.ID] = partition.Value
		}
	}
	fixtures := map[string]Fixture{}
	for _, fixture := range source.fixtures.Fixtures {
		fixtures[fixture.ID] = fixture
	}
	result := map[string]BehaviorExample{}
	for _, c := range actual.Cases {
		inputs := map[string]any{}
		for fact, partition := range c.Witness {
			value, ok := values[fact][partition]
			if !ok {
				return nil, fmt.Errorf("CASE witness has no original partition value")
			}
			inputs[fact] = value
		}
		result[c.ID] = BehaviorExample{Case: c, Inputs: inputs, Fixture: fixtures[c.FixtureID], Sources: append([]BehaviorSource{}, sources...)}
	}
	return result, nil
}

type behaviorReader struct {
	fileview.Reader
	root  string
	paths map[string]struct{}
}

func (r *behaviorReader) ReadFile(path string) ([]byte, error) {
	data, err := r.Reader.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.root, path)
	}
	rel, err := filepath.Rel(r.root, path)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	r.paths[rel] = struct{}{}
	return data, nil
}
