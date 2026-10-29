package projectconventions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/schema"
)

type resolver struct {
	root            string
	files           fileview.Reader
	cache           map[string][]byte
	failures        map[string]error
	sources         map[string]Source
	memo            map[string]Source
	registry        Registry
	registryChecked bool
	registryErr     error
}

func (r *resolver) read(path string) ([]byte, error) {
	if data, ok := r.cache[path]; ok {
		r.sources[path] = r.memo[path]
		return data, nil
	}
	if err, ok := r.failures[path]; ok {
		r.sources[path] = r.memo[path]
		return nil, err
	}
	mode := "disk"
	var sourceErr error
	if sourced, ok := r.files.(interface{ Source(string) (string, error) }); ok {
		value, err := sourced.Source(path)
		if err != nil {
			mode = "unresolved"
			sourceErr = err
		} else {
			mode = value
		}
	}
	s := Source{Path: path, SourceMode: mode, Status: "unreadable"}
	fail := func(err error) ([]byte, error) {
		r.failures[path] = err
		r.sources[path] = s
		r.memo[path] = s
		return nil, err
	}
	if sourceErr != nil {
		return fail(sourceErr)
	}
	if !safePath(path, false) {
		return fail(fmt.Errorf("source path is not repository-relative: %s", path))
	}
	if mode != "git_tree" {
		physical, err := filepath.EvalSymlinks(filepath.Join(r.root, path))
		if err == nil {
			base, err := filepath.EvalSymlinks(r.root)
			if err != nil {
				return fail(err)
			}
			rel, err := filepath.Rel(base, physical)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fail(fmt.Errorf("source resolves outside project: %s", path))
			}
		}
	}
	data, err := r.files.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		if entries, listErr := r.files.ReadDir(filepath.Join(r.root, filepath.Dir(path))); listErr == nil {
			absent := true
			for _, entry := range entries {
				if entry.Name() == filepath.Base(path) {
					absent = false
				}
			}
			if absent {
				s.Status = "absent"
			}
		} else if os.IsNotExist(err) && os.IsNotExist(listErr) {
			s.Status = "absent"
		}
		return fail(fmt.Errorf("read %s: %w", path, err))
	}
	if len(data) > 1<<20 {
		s.Status = "parse_limit_exceeded"
		return fail(fmt.Errorf("source exceeds 1 MiB analysis limit: %s", path))
	}
	s.Status = "readable"
	s.SHA256 = digest(data)
	r.sources[path] = s
	r.memo[path] = s
	r.cache[path] = data
	return data, nil
}
func listedRule(index []byte, path string) bool {
	if !strings.HasPrefix(path, "docs/rules/") || strings.Contains(strings.TrimPrefix(path, "docs/rules/"), "/") {
		return false
	}
	file := filepath.Base(path)
	fenced := false
	for _, line := range strings.Split(string(index), "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || !strings.HasPrefix(text, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(text, "|"), "|")
		if len(cells) >= 2 && strings.TrimSpace(cells[1]) == "`"+file+"`" {
			return true
		}
	}
	return false
}
func (r *resolver) prove(ref Reference, approved []Reference) ([]byte, error) {
	if !safePath(ref.Path, false) {
		return nil, fmt.Errorf("source path is not repository-relative: %s", ref.Path)
	}
	switch ref.Kind {
	case "rule":
		if !strings.HasPrefix(ref.Path, "docs/rules/") || strings.Contains(strings.TrimPrefix(ref.Path, "docs/rules/"), "/") {
			return nil, fmt.Errorf("source is outside the original Rules index")
		}
		index, err := r.read(IndexPath)
		if err != nil {
			return nil, err
		}
		if metadata(index, "status") != "locked" || !listedRule(index, ref.Path) {
			return nil, fmt.Errorf("source is not in the original locked Rules index: %s", ref.Path)
		}
	case "architecture", "decision":
		prefix := "docs/architecture/"
		if ref.Kind == "decision" {
			prefix = "docs/design/decisions/"
		}
		matched := false
		for _, a := range approved {
			if a.ID == ref.ID && a.Path == ref.Path && a.Version == ref.Version && a.SHA256 == ref.SHA256 {
				matched = true
			}
		}
		if !strings.HasPrefix(ref.Path, prefix) || !matched || ref.Version == "" {
			return nil, fmt.Errorf("source lacks the exact registered locked document/version proof: %s", ref.Path)
		}
	default:
		return nil, fmt.Errorf("unsupported convention source kind")
	}
	data, err := r.read(ref.Path)
	if err != nil {
		return nil, err
	}
	if !digestPattern.MatchString(ref.SHA256) || digest(data) != ref.SHA256 {
		return nil, fmt.Errorf("source digest changed: %s", ref.Path)
	}
	if metadata(data, "status") != "locked" {
		return nil, fmt.Errorf("source is not locked: %s", ref.Path)
	}
	if ref.Kind == "rule" && metadata(data, "rule_id") != ref.ID {
		return nil, fmt.Errorf("source is not the named original rule: %s", ref.Path)
	}
	if ref.Kind != "rule" && metadata(data, "version") != ref.Version {
		return nil, fmt.Errorf("source version differs from the original registration: %s", ref.Path)
	}
	return section(data, ref.Anchor)
}
func examplePath(path string) bool {
	if !safePath(path, false) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".git" || part == ".claude" || part == "blueprint" || strings.HasPrefix(part, ".env") {
			return false
		}
	}
	return true
}

func matches(scope, targets []string) bool {
	for _, s := range scope {
		if !safePath(s, true) {
			return false
		}
	}
	for _, target := range targets {
		target = taskPrefix(target)
		if !safePath(target, true) {
			continue
		}
		for _, s := range scope {
			if overlaps(s, target) {
				return true
			}
		}
	}
	return false
}
func Resolve(root string, files fileview.Reader, req Request) Projection {
	return ResolveMany(root, files, []Request{req})[0]
}

// ResolveMany shares one bounded read/schema cache across the current TASK set,
// while every output lists only the sources used for that TASK.
func ResolveMany(root string, files fileview.Reader, requests []Request) []Projection {
	root, _ = filepath.Abs(root)
	r := &resolver{root: root, files: files, cache: map[string][]byte{}, failures: map[string]error{}, sources: map[string]Source{}, memo: map[string]Source{}}
	result := []Projection{}
	for _, req := range requests {
		r.sources = map[string]Source{}
		result = append(result, resolve(r, req))
	}
	return result
}
func resolve(r *resolver, req Request) Projection {
	out := Projection{ResolverVersion: Version, TaskID: req.TaskID, TaskRef: req.TaskRef, RegistryStatus: "absent", Sources: []Source{}, Entries: []Relevant{}, Questions: []string{}}

	data, err := r.read(RegistryPath)
	if err != nil {
		out.RegistryStatus = r.sources[RegistryPath].Status
		if out.RegistryStatus != "absent" {
			out.Questions = append(out.Questions, err.Error())
		}
		// Candidate cards are selected only by explicit TASK reading references.
		for _, ref := range req.Reading {
			topic := ""
			switch ref.Path {
			case "docs/rules/api-design.md":
				topic = "api"
			case "docs/rules/error-handling.md":
				topic = "error-handling"
			}
			if topic == "" {
				continue
			}
			body, err := r.read(ref.Path)
			if err != nil {
				out.Questions = append(out.Questions, err.Error())
				continue
			}
			scope := []string{}
			for _, s := range req.Scope {
				s = taskPrefix(s)
				if safePath(s, true) {
					scope = append(scope, s)
				}
			}
			ref.Kind = "rule"
			ref.ID = metadata(body, "rule_id")
			if ref.ID == "" {
				out.Questions = append(out.Questions, "Explicit rule has no original rule_id: "+ref.Path)
				continue
			}
			ref.SHA256 = digest(body)
			entry := Entry{ID: "candidate-" + topic, Topic: topic, Scope: scope, Default: "", SourceRefs: []Reference{ref}, ExampleRefs: []Reference{}, Exceptions: []string{}, Status: "candidate"}
			question := "Architect: confirm the exact applicable original rule choice and its exceptions for this TASK in the original professional material; this candidate does not establish a current default."
			if _, err := r.prove(ref, req.ApprovedRefs); err != nil {
				question += " " + err.Error()
			}
			out.Entries = append(out.Entries, Relevant{Entry: entry, Status: "candidate", Questions: []string{question}})
		}
	} else if err := r.loadRegistry(data); err != nil {
		out.RegistryStatus = "invalid"
		out.Questions = append(out.Questions, "Convention registry schema: "+err.Error())
	} else {
		out.RegistryStatus = "present"
		registry := r.registry
		if err := r.registryErr; err != nil {
			out.RegistryStatus = "invalid"
			out.Questions = append(out.Questions, err.Error())
		} else {
			counts := map[string]int{}
			for _, entry := range registry.Entries {
				counts[entry.ID]++
				validScope := true
				for _, path := range entry.Scope {
					if !safePath(path, true) {
						validScope = false
					}
				}
				if !validScope {
					out.Questions = append(out.Questions, "Convention scope must be a canonical repository path prefix: "+entry.ID)
					continue
				}
			}
			for _, entry := range registry.Entries {
				if !matches(entry.Scope, req.Scope) {
					continue
				}
				row := Relevant{Entry: entry, Status: entry.Status, Questions: []string{}}
				proven := true
				quoted := false
				for _, ref := range entry.SourceRefs {
					selected, err := r.prove(ref, req.ApprovedRefs)
					if err != nil {
						proven = false
						row.Questions = append(row.Questions, err.Error())
					} else if strings.TrimSpace(entry.Default) != "" && strings.Contains(string(selected), entry.Default) {
						quoted = true
					}
				}
				examplesValid := true
				for _, ref := range entry.ExampleRefs {
					if !examplePath(ref.Path) {
						examplesValid = false
						row.Questions = append(row.Questions, "Example is not an implementation/test path: "+ref.Path)
						continue
					}
					if len(req.ApprovedRefs) > 0 {
						mode := "disk"
						if sourced, ok := r.files.(interface{ Source(string) (string, error) }); ok {
							value, err := sourced.Source(ref.Path)
							if err == nil {
								mode = value
							}
						}
						if mode != "git_tree" {
							examplesValid = false
							row.Questions = append(row.Questions, "Formal example is not from the bound committed development view: "+ref.Path)
							continue
						}
					}
					body, err := r.read(ref.Path)
					if err != nil || digest(body) != ref.SHA256 {
						examplesValid = false
						row.Questions = append(row.Questions, "Example unavailable or changed: "+ref.Path)
					}
				}
				if entry.Status == "approved" {
					row.Status = "candidate"
					if proven && quoted && examplesValid {
						row.Status = "declared_default"
					} else if !examplesValid {
						row.Status = "stale"
					}
					if !quoted {
						row.Questions = append(row.Questions, "The declared default is not present in a proved source section; resolve the original decision instead of approving the registry's claim.")
					}
				}
				if counts[entry.ID] > 1 {
					row.Status = "conflict"
					row.Questions = append(row.Questions, "Duplicate convention ID; resolve all original entries without selecting a winner.")
				}
				if entry.Status == "migration" || entry.Status == "superseded" {
					row.Questions = append(row.Questions, "This convention is not a current default; confirm the original migration or replacement responsibility.")
				}
				out.Entries = append(out.Entries, row)
			}
		}
	}
	for i := range out.Entries {
		for j := i + 1; j < len(out.Entries); j++ {
			a, b := &out.Entries[i], &out.Entries[j]
			if a.Entry.Topic != b.Entry.Topic || a.Entry.Default == b.Entry.Default || a.Entry.Status != "approved" || b.Entry.Status != "approved" {
				continue
			}
			if matches(a.Entry.Scope, b.Entry.Scope) {
				a.Status = "conflict"
				b.Status = "conflict"
				a.Questions = append(a.Questions, "Overlapping current default conflicts with "+b.Entry.ID)
				b.Questions = append(b.Questions, "Overlapping current default conflicts with "+a.Entry.ID)
			}
		}
	}
	for _, s := range r.sources {
		out.Sources = append(out.Sources, s)
	}
	sort.Slice(out.Sources, func(i, j int) bool { return out.Sources[i].Path < out.Sources[j].Path })
	sort.SliceStable(out.Entries, func(i, j int) bool {
		if out.Entries[i].Entry.Topic != out.Entries[j].Entry.Topic {
			return out.Entries[i].Entry.Topic < out.Entries[j].Entry.Topic
		}
		return out.Entries[i].Entry.ID < out.Entries[j].Entry.ID
	})
	return out
}

func (r *resolver) loadRegistry(data []byte) error {
	if !r.registryChecked {
		r.registryChecked = true
		r.registryErr = schema.NewEmbeddedValidator().ValidateBytes("project-conventions.schema.json", data)
		if r.registryErr == nil {
			r.registryErr = json.Unmarshal(data, &r.registry)
		}
	}
	return r.registryErr
}
