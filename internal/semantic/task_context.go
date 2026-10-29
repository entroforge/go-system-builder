package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
)

const TaskContextAnalysisVersion = "1.0.0"
const taskContextParseLimit = 1 << 20

type TaskReadingReference struct {
	Source           string `json:"source"`
	Path             string `json:"path,omitempty"`
	Anchor           string `json:"anchor,omitempty"`
	Mode             string `json:"mode"`
	Purpose          string `json:"purpose,omitempty"`
	Clauses          string `json:"clauses,omitempty"`
	ConditionUnknown bool   `json:"condition_unknown"`
	Status           string `json:"status"`
	Selection        string `json:"selection,omitempty"`
	Bytes            *int   `json:"bytes"`
	SHA256           string `json:"sha256,omitempty"`
	Reason           string `json:"reason,omitempty"`
}
type TaskCheckEntrance struct {
	Command string `json:"command"`
	Source  string `json:"source"`
}
type TaskContextSummary struct {
	TaskID               string                 `json:"task_id"`
	TaskRef              string                 `json:"task_ref"`
	Reading              []TaskReadingReference `json:"reading"`
	BytesByMode          map[string]int         `json:"bytes_by_mode"`
	TotalUniqueBytes     int                    `json:"total_unique_bytes"`
	Writes               []string               `json:"writes"`
	Modules              []string               `json:"modules"`
	Dependencies         []string               `json:"dependencies"`
	Clauses              []string               `json:"clauses"`
	CheckEntrances       []TaskCheckEntrance    `json:"check_entrances"`
	UnresolvedAssertions []string               `json:"unresolved_assertions"`
}
type TaskGranularityAdvisory struct {
	Kind        string   `json:"kind"`
	TaskIDs     []string `json:"task_ids"`
	SourceRefs  []string `json:"source_refs"`
	Paths       []string `json:"paths"`
	Observation string   `json:"observation"`
	Question    string   `json:"question"`
}
type contextFile struct {
	body           []byte
	status, reason string
}
type byteSpan struct{ start, end int }

var contextLink = regexp.MustCompile(`\[[^\]]*\]\(([^\s)]+)\)`)
var explicitHeadingAnchor = regexp.MustCompile(`(?:\{#([^}]+)\}|<a\s+(?:id|name)=["']([^"']+)["'][^>]*>\s*</a>)`)

func analyzeTaskContexts(root string, files fileview.Reader, tasks []*taskDocument) []TaskContextSummary {
	cache := map[string]contextFile{}
	for _, t := range tasks {
		cache[t.rel] = contextFile{body: []byte(t.content), status: "readable"}
		if len(t.content) > taskContextParseLimit {
			cache[t.rel] = contextFile{status: "parse_limit_exceeded", reason: "TASK source exceeds the 1 MiB referenced-input analysis limit"}
		}
	}
	read := func(p string) contextFile {
		if f, ok := cache[p]; ok {
			return f
		}
		underlying := files
		for {
			scoped, ok := underlying.(planningFiles)
			if !ok {
				break
			}
			underlying = scoped.Reader
		}
		committed := false
		if sourced, ok := underlying.(interface{ Source(string) (string, error) }); ok {
			source, err := sourced.Source(p)
			committed = err == nil && source == "git_tree"
		}
		if physical, err := filepath.EvalSymlinks(filepath.Join(root, p)); !committed && err == nil {
			physicalRoot, rootErr := filepath.EvalSymlinks(root)
			if rootErr == nil {
				rel, relErr := filepath.Rel(physicalRoot, physical)
				if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					f := contextFile{status: "unresolved", reason: "reading input resolves outside the repository"}
					cache[p] = f
					return f
				}
			}
		}
		f := contextFile{status: "unreadable", reason: "declared input is unavailable through the original file-source view"}
		if _, err := files.ReadDir(filepath.Join(root, p)); err == nil {
			f.status = "directory_unexpanded"
			f.reason = "directory entries are not expanded into a repository reading set"
		} else if data, err := files.ReadFile(filepath.Join(root, p)); err == nil {
			f.body = data
			f.status = "readable"
			f.reason = ""
		}
		if len(f.body) > taskContextParseLimit {
			f.body = nil
			f.status = "parse_limit_exceeded"
			f.reason = "input exceeds the 1 MiB analysis limit; underlying Reader I/O is not measured here"
		}
		cache[p] = f
		return f
	}
	var result []TaskContextSummary
	for _, t := range tasks {
		if t.status == "cancelled" {
			continue
		}
		s := TaskContextSummary{TaskID: t.id, TaskRef: t.rel, Reading: []TaskReadingReference{}, BytesByMode: map[string]int{"required": 0, "conditional": 0, "optional": 0, "legacy_unspecified": 0}, Writes: append([]string{}, t.writePaths...), Modules: []string{}, Dependencies: append([]string{}, t.deps...), Clauses: append([]string{}, t.clauses...), CheckEntrances: []TaskCheckEntrance{}, UnresolvedAssertions: []string{}}
		all := map[string][]byteSpan{}
		byMode := map[string]map[string][]byteSpan{}
		rows := sectionTable(t.content, "Document Manifest")
		for i, row := range rows {
			if len(row) < 4 || row[3] == "Path" {
				continue
			}
			r := TaskReadingReference{Source: fmt.Sprintf("%s#document-manifest:row-%d", t.rel, i+1), Mode: "legacy_unspecified", Status: "unresolved"}
			if len(row) > 4 {
				r.Clauses = row[4]
			}
			if len(row) > 5 {
				r.Purpose = row[5]
			}
			if len(row) > 6 {
				r.Mode = strings.Trim(row[6], "` ")
			}
			r.ConditionUnknown = r.Mode == "conditional"
			if _, ok := s.BytesByMode[r.Mode]; !ok {
				r.Reason = "unknown reading Mode"
				s.Reading = append(s.Reading, r)
				continue
			}
			links := contextLink.FindAllStringSubmatch(row[3], -1)
			var err error
			if len(links) == 1 {
				r.Path, r.Anchor, err = sharedmodel.Resolve(root, t.rel, links[0][1])
			} else if len(links) > 1 {
				err = fmt.Errorf("expected one linked reading input")
			} else {
				r.Path = strings.Trim(row[3], "` ")
				r.Path, r.Anchor, _ = strings.Cut(r.Path, "#")
				if filepath.IsAbs(r.Path) || strings.Contains(r.Path, ":") || strings.Contains(r.Path, "\\") || r.Path == "" {
					err = fmt.Errorf("expected repository-relative input")
				} else {
					r.Path = filepath.ToSlash(filepath.Clean(r.Path))
					if r.Path == ".." || strings.HasPrefix(r.Path, "../") {
						err = fmt.Errorf("reading input escapes repository")
					}
				}
			}
			if err != nil || strings.Contains(r.Path, "{") {
				r.Reason = "unresolved reading reference"
				if err != nil {
					r.Reason = err.Error()
				}
				s.Reading = append(s.Reading, r)
				continue
			}
			f := read(r.Path)
			r.Status, r.Reason = f.status, f.reason
			if f.status == "readable" {
				digest := sha256.Sum256(f.body)
				r.SHA256 = hex.EncodeToString(digest[:])
				span, selection, reason := readingSpan(f.body, r.Path, r.Anchor)
				r.Selection, r.Reason = selection, reason
				n := span.end - span.start
				r.Bytes = &n
				all[r.Path] = append(all[r.Path], span)
				if byMode[r.Mode] == nil {
					byMode[r.Mode] = map[string][]byteSpan{}
				}
				byMode[r.Mode][r.Path] = append(byMode[r.Mode][r.Path], span)
			}
			s.Reading = append(s.Reading, r)
		}
		for _, spans := range all {
			s.TotalUniqueBytes += unionSpanBytes(spans)
		}
		for mode, paths := range byMode {
			for _, spans := range paths {
				s.BytesByMode[mode] += unionSpanBytes(spans)
			}
		}
		modules := map[string]bool{}
		for _, p := range s.Writes {
			parts := strings.Split(strings.Trim(p, "/"), "/")
			if len(parts) > 1 && strings.Contains(parts[len(parts)-1], ".") {
				parts = parts[:len(parts)-1]
			}
			if len(parts) > 2 {
				parts = parts[:2]
			}
			modules[strings.Join(parts, "/")] = true
		}
		for m := range modules {
			s.Modules = append(s.Modules, m)
		}
		sort.Strings(s.Modules)
		closing := closingSection(t.content)
		for _, line := range strings.Split(closing, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "assert ") {
				continue
			}
			assertion := strings.TrimSpace(strings.TrimPrefix(line, "assert "))
			command, tail, ok := strings.Cut(assertion, " == ")
			command = strings.Trim(command, "` ")
			if ok && tail == "pass" && !strings.ContainsAny(command, "{}") && knownCheckEntrance(command) {
				s.CheckEntrances = append(s.CheckEntrances, TaskCheckEntrance{Command: command, Source: t.rel + "#closing-contract"})
			} else {
				s.UnresolvedAssertions = append(s.UnresolvedAssertions, line)
			}
		}
		result = append(result, s)
	}
	return result
}

func knownCheckEntrance(command string) bool {
	for _, p := range []string{"go test", "go vet", "go build", "npm test", "npm run", "pnpm test", "pnpm run", "yarn test", "yarn run", "pytest", "cargo test", "cargo check", "make test", "make lint", "make check"} {
		if command == p || strings.HasPrefix(command, p+" ") {
			return true
		}
	}
	return false
}
func closingSection(body string) string {
	active := false
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			if active {
				break
			}
			active = strings.Contains(line, "Closing Contract")
			continue
		}
		if active {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
func readingSpan(body []byte, p, anchor string) (byteSpan, string, string) {
	whole := byteSpan{0, len(body)}
	if anchor == "" {
		return whole, "whole_file", ""
	}
	if !strings.HasSuffix(p, ".md") {
		return whole, "whole_file", "non-Markdown anchor retained; complete file counted conservatively"
	}
	type heading struct {
		start, level int
		slug         string
		aliases      []string
	}
	var headings []heading
	offset := 0
	fence := ""
	var pendingAliases []string
	pendingStart := 0
	for _, line := range strings.SplitAfter(string(body), "\n") {
		text := strings.TrimSpace(line)
		marker := ""
		if strings.HasPrefix(text, "```") {
			marker = "```"
		} else if strings.HasPrefix(text, "~~~") {
			marker = "~~~"
		}
		if marker != "" {
			pendingAliases = nil
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			offset += len(line)
			continue
		}
		if fence == "" && text != "" && explicitHeadingAnchor.MatchString(text) && strings.TrimSpace(explicitHeadingAnchor.ReplaceAllString(text, "")) == "" {
			if len(pendingAliases) == 0 {
				pendingStart = offset
			}
			for _, a := range explicitHeadingAnchor.FindAllStringSubmatch(text, -1) {
				pendingAliases = append(pendingAliases, a[1], a[2])
			}
			offset += len(line)
			continue
		}
		if fence == "" && strings.HasPrefix(text, "#") {
			level := len(text) - len(strings.TrimLeft(text, "#"))
			if level <= 6 && len(text) > level && text[level] == ' ' {
				title := strings.TrimSpace(text[level:])
				h := heading{start: offset, level: level}
				if len(pendingAliases) > 0 {
					h.start = pendingStart
					h.aliases = append(h.aliases, pendingAliases...)
				}
				for _, a := range explicitHeadingAnchor.FindAllStringSubmatch(title, -1) {
					h.aliases = append(h.aliases, a[1], a[2])
				}
				title = explicitHeadingAnchor.ReplaceAllString(title, "")
				h.slug = strings.Map(func(r rune) rune {
					if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
						return unicode.ToLower(r)
					}
					if r == ' ' {
						return '-'
					}
					return -1
				}, strings.TrimSpace(title))
				headings = append(headings, h)
			}
		}
		if text != "" {
			pendingAliases = nil
		}
		offset += len(line)
	}
	match := -1
	for i, h := range headings {
		found := h.slug == anchor
		for _, a := range h.aliases {
			found = found || a != "" && a == anchor
		}
		if found {
			if match >= 0 {
				return whole, "whole_file", "ambiguous Markdown anchor; complete file counted conservatively"
			}
			match = i
		}
	}
	if match < 0 {
		return whole, "whole_file", "Markdown anchor could not be located as one heading; complete file counted conservatively"
	}
	end := len(body)
	for _, h := range headings[match+1:] {
		if h.level <= headings[match].level {
			end = h.start
			break
		}
	}
	return byteSpan{headings[match].start, end}, "heading_section", ""
}
func unionSpanBytes(spans []byteSpan) int {
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	total, end := 0, -1
	for _, s := range spans {
		start := s.start
		if start < end {
			start = end
		}
		if s.end > start {
			total += s.end - start
		}
		if s.end > end {
			end = s.end
		}
	}
	return total
}

// AnalyzeTaskGranularity only poses source-backed questions. No advice is a
// gate, size threshold, execution result or edit to the approved batch.
func AnalyzeTaskGranularity(summaries []TaskContextSummary) []TaskGranularityAdvisory {
	result := []TaskGranularityAdvisory{}
	for i, a := range summaries {
		for _, b := range summaries[i+1:] {
			shared := []string{}
			overlap := false
			for _, x := range a.Writes {
				for _, y := range b.Writes {
					overlap = overlap || WriteOverlap(x, y)
					if x == y && !strings.ContainsAny(x, "*?[") && strings.Contains(filepath.Base(x), ".") {
						shared = append(shared, x)
					}
				}
			}
			ids, refs := []string{a.TaskID, b.TaskID}, []string{a.TaskRef + "#scope", b.TaskRef + "#scope"}
			if len(shared) > 0 {
				result = append(result, TaskGranularityAdvisory{Kind: "same_file_writes", TaskIDs: ids, SourceRefs: refs, Paths: uniqueContextStrings(shared), Observation: "Both TASKs declare the same literal file write.", Question: "What approved dependency or ownership boundary prevents conflicting changes; should the shared change be delivered once?"})
			}
			ar, br := requiredContextRefs(a), requiredContextRefs(b)
			if overlap && len(ar) > 0 && strings.Join(ar, "\n") == strings.Join(br, "\n") {
				result = append(result, TaskGranularityAdvisory{Kind: "shared_read_write_context", TaskIDs: ids, SourceRefs: []string{a.TaskRef + "#document-manifest", b.TaskRef + "#document-manifest"}, Paths: ar, Observation: "The complete declared required reading sets are identical and write scopes overlap.", Question: "Is the separation needed for independent acceptance, or can shared implementation/context be delivered once without losing clause coverage?"})
			}
		}
	}
	return result
}
func requiredContextRefs(s TaskContextSummary) []string {
	var refs []string
	for _, r := range s.Reading {
		if r.Mode == "required" && r.Status == "readable" {
			ref := r.Path
			if r.Anchor != "" {
				ref += "#" + r.Anchor
			}
			refs = append(refs, ref)
		}
	}
	return uniqueContextStrings(refs)
}
func uniqueContextStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.Strings(result)
	return result
}
