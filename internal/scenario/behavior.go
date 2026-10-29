package scenario

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

type BehaviorSource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type BehaviorExample struct {
	Case    Case             `json:"case"`
	Inputs  map[string]any   `json:"inputs"`
	Fixture Fixture          `json:"fixture"`
	Sources []BehaviorSource `json:"sources"`
}

// ProjectBehaviorWithFiles uses the original generator's source validation and
// output equality. It executes no scenario, test or browser and writes no file.
func ProjectBehaviorWithFiles(root, module string, files fileview.Reader) (map[string]BehaviorExample, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	recorded := &behaviorReader{Reader: files, root: root, hashes: map[string]string{}}
	files = recorded
	source, err := loadSourcePackageWithFiles(root, module, files)
	if err != nil {
		return nil, err
	}
	outputs, err := buildOutputs(source)
	if err != nil {
		return nil, err
	}
	sources := []BehaviorSource{}
	loaded := map[string][]byte{modelFile: source.modelBytes, fixtureFile: source.fixtureBytes, "cross-matrix.json": source.crossMatrixByte, "stories.md": source.stories, "flows.md": source.flows}
	for _, name := range []string{modelFile, fixtureFile, "cross-matrix.json", "stories.md", "flows.md", casesFile, coverageFile} {
		path := filepath.ToSlash(filepath.Join(prototypeRoot, module, name))
		data, err := files.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		if original, ok := loaded[name]; ok && !bytes.Equal(data, original) {
			return nil, fmt.Errorf("scenario source %s changed during behavior projection", name)
		}
		if name == casesFile && !bytes.Equal(data, outputs.casesBytes) || name == coverageFile && !bytes.Equal(data, outputs.coverageBytes) {
			return nil, fmt.Errorf("current generated %s differs from approved scenario sources", name)
		}
	}
	for path, hash := range recorded.hashes {
		sources = append(sources, BehaviorSource{Path: path, SHA256: hash})
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
	for _, c := range outputs.cases {
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
	root   string
	hashes map[string]string
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
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if prior, ok := r.hashes[rel]; ok && prior != hash {
		return nil, fmt.Errorf("scenario source %s changed during behavior projection", rel)
	}
	r.hashes[rel] = hash
	return data, nil
}
