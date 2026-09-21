package semantic

import (
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
)

// DispatchPlan is a reviewed static plan. Dependencies remain TASK-owned.
type DispatchPlan struct {
	Path     string         `json:"path"`
	REQ      string         `json:"req"`
	Revision string         `json:"revision"`
	Content  []byte         `json:"-"`
	Tasks    []DispatchTask `json:"tasks"`
	Problems []string       `json:"problems,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`
}
type DispatchTask struct {
	ID           string   `json:"id"`
	Path         string   `json:"path"`
	Wave         int      `json:"wave"`
	Dependencies []string `json:"dependencies"`
	Writes       []string `json:"writes"`
	Resources    []string `json:"resources,omitempty"`
	Before       []string `json:"resource_predecessors,omitempty"`
}

var reqToken = regexp.MustCompile(`\bREQ-[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*\b`)
var waveHeading = regexp.MustCompile(`^## W([1-9][0-9]*)\b`)
var planItem = regexp.MustCompile(`^- \[([ xX])\] \[([^\]]+)\]\(([^)]+)\)\s*$`)

func MarkdownField(s string, keys ...string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, key := range keys {
			if strings.HasPrefix(line, key+":") {
				return strings.TrimSpace(strings.TrimPrefix(line, key+":"))
			}
		}
	}
	return ""
}
func HasREQ(s, req string) bool {
	for _, id := range reqToken.FindAllString(MarkdownField(s, "Source REQ refs", "REQ", "需求", "Requirement"), -1) {
		if id == req {
			return true
		}
	}
	return false
}

// ScopedPlanningFiles filters discovery, never individual reads. Unknown TASK
// ownership is retained so explicit waves-v1 plans diagnose it, not hide it.
func ScopedPlanningFiles(root string, files fileview.Reader, req string) fileview.Reader {
	if req == "" {
		return files
	}
	return planningFiles{files, root, req}
}

type planningFiles struct {
	fileview.Reader
	root, req string
}

func (f planningFiles) ReadDir(p string) ([]os.DirEntry, error) {
	entries, err := f.Reader.ReadDir(p)
	if err != nil {
		return nil, err
	}
	rel := p
	if filepath.IsAbs(p) {
		rel, _ = filepath.Rel(f.root, p)
	}
	rel = filepath.ToSlash(rel)
	if rel != projectlayout.Tasks && rel != projectlayout.Contracts {
		return entries, nil
	}
	out := []os.DirEntry{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.Contains(e.Name(), "template") {
			out = append(out, e)
			continue
		}
		b, er := f.Reader.ReadFile(filepath.Join(p, e.Name()))
		if er != nil {
			return nil, er
		}
		declared := reqToken.FindAllString(MarkdownField(string(b), "Source REQ refs", "REQ", "需求", "Requirement"), -1)
		if len(declared) == 0 || HasREQ(string(b), f.req) {
			out = append(out, e)
		}
	}
	return out, nil
}
func (f planningFiles) Source(p string) (string, error) {
	if s, ok := f.Reader.(interface{ Source(string) (string, error) }); ok {
		return s.Source(p)
	}
	return "git_tree", nil // explicit authoring reader
}

// LoadDispatchPlan validates the selected REQ's plan and membership against
// TASK declarations (not only against the plan's own links).
func LoadDispatchPlan(root string, files fileview.Reader, req string) (*DispatchPlan, error) {
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p := &DispatchPlan{REQ: req}
	entries, err := files.ReadDir(filepath.Join(root, projectlayout.Tasks))
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return nil, err
	}
	selected := []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "index-") || !strings.HasSuffix(e.Name(), ".md") || strings.Contains(e.Name(), "template") {
			continue
		}
		path := filepath.Join(projectlayout.Tasks, e.Name())
		b, er := files.ReadFile(filepath.Join(root, path))
		if er != nil {
			return nil, er
		}
		if req != "" && !HasREQ(string(b), req) {
			continue
		}
		if MarkdownField(string(b), "Dispatch policy") == "" {
			continue
		}
		selected = append(selected, path)
	}
	if len(selected) > 1 {
		p.Problems = append(p.Problems, "multiple dispatch plans: select one REQ/unique index")
		return p, nil
	}
	if len(selected) == 1 {
		p.Path = filepath.ToSlash(selected[0])
		p.Content, err = files.ReadFile(filepath.Join(root, p.Path))
		if err != nil {
			return nil, err
		}
		s := string(p.Content)
		declared := MarkdownField(s, "REQ", "需求")
		if req == "" {
			p.REQ = declared
			req = declared
		}
		if !reqToken.MatchString(req) || declared != req {
			p.Problems = append(p.Problems, "dispatch plan requires one exact REQ")
		}
		if MarkdownField(s, "Dispatch policy") != "waves-v1" {
			p.Problems = append(p.Problems, "unsupported Dispatch policy (expected waves-v1)")
		}
		if MarkdownField(s, "Status") != "complete" {
			p.Problems = append(p.Problems, "dispatch plan Status must be complete")
		}
		p.Revision = MarkdownField(s, "Revision")
		if n, e := strconv.Atoi(p.Revision); e != nil || n < 1 {
			p.Problems = append(p.Problems, "dispatch plan Revision must be a positive integer")
		}
		if r, ok := files.(interface{ Source(string) (string, error) }); ok {
			src, e := r.Source(p.Path)
			if e != nil || src != "git_tree" {
				p.Problems = append(p.Problems, "dispatch plan must use committed git_tree input")
			}
		}
	}
	scoped := ScopedPlanningFiles(root, files, req)
	tasks, err := loadTaskDocumentsWithFiles(root, scoped)
	if err != nil {
		return nil, err
	}
	required := false
	byID := map[string]*taskDocument{}
	for _, t := range tasks {
		b, e := files.ReadFile(filepath.Join(root, t.rel))
		if e != nil {
			return nil, e
		}
		if MarkdownField(string(b), "Dispatch policy") != "" {
			required = true
		}
		if policy := MarkdownField(string(b), "Dispatch policy"); policy != "" && policy != "waves-v1" {
			p.Problems = append(p.Problems, "unsupported TASK Dispatch policy: "+t.id)
		}
		byID[t.id] = t
	}
	if req != "" {
		b, e := files.ReadFile(filepath.Join(root, projectlayout.Requirements, req+".md"))
		if e == nil && MarkdownField(string(b), "Dispatch policy") != "" {
			required = true
		}
	}
	if p.Path == "" {
		if required {
			p.Problems = append(p.Problems, "waves-v1 batch requires an overall dispatch plan")
		} else {
			p.Warnings = append(p.Warnings, "legacy: no reviewed dispatch plan; new planning batches must adopt waves-v1")
		}
		return p, nil
	}
	s := regexp.MustCompile("(?ms)^```.*?^```[^\\n]*$").ReplaceAllString(string(p.Content), "")
	wave := 0
	last := 0
	seen := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if m := waveHeading.FindStringSubmatch(line); m != nil {
			wave, _ = strconv.Atoi(m[1])
			if wave != last+1 {
				p.Problems = append(p.Problems, "waves must be contiguous and increasing from W1")
			}
			last = wave
			continue
		}
		if strings.HasPrefix(line, "## ") {
			wave = 0
		}
		if !strings.HasPrefix(line, "- [") {
			continue
		}
		m := planItem.FindStringSubmatch(line)
		if m == nil || wave == 0 {
			p.Problems = append(p.Problems, "TASK checklist entry must be one Markdown file link under a W<n> heading")
			continue
		}
		path, fragment, e := sharedmodel.Resolve(root, p.Path, m[3])
		if e != nil || fragment != "" {
			p.Problems = append(p.Problems, "invalid TASK link: "+m[3])
			continue
		}
		id := strings.TrimSuffix(filepath.Base(path), ".md")
		t := byID[id]
		if t == nil || t.rel != path {
			p.Problems = append(p.Problems, "unknown or cross-REQ TASK link: "+m[3])
			continue
		}
		if m[1] != " " {
			p.Problems = append(p.Problems, "frozen plan checkboxes must stay unchecked: "+id)
		}
		if seen[id] {
			p.Problems = append(p.Problems, "duplicate planned TASK: "+id)
		}
		seen[id] = true
		if t.status != "complete" {
			p.Problems = append(p.Problems, "planned TASK must be complete: "+id)
		}
		b, _ := files.ReadFile(filepath.Join(root, t.rel))
		if !HasREQ(string(b), req) {
			p.Problems = append(p.Problems, "TASK must explicitly bind plan REQ: "+id)
		}
		if r, ok := files.(interface{ Source(string) (string, error) }); ok {
			src, e := r.Source(t.rel)
			if e != nil || src != "git_tree" {
				p.Problems = append(p.Problems, "TASK must use git_tree: "+id)
			}
		}
		dt := DispatchTask{ID: id, Path: t.rel, Wave: wave, Dependencies: t.deps, Writes: t.writePaths}
		for _, write := range dt.Writes {
			clean := filepath.ToSlash(filepath.Clean(write))
			if filepath.IsAbs(write) || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsAny(write, "*?{[") {
				p.Problems = append(p.Problems, "waves-v1 requires concrete repository-relative write paths: "+id+" "+write)
			}
		}

		if len(dt.Writes) == 0 {
			p.Problems = append(p.Problems, "TASK needs concrete prospective write paths: "+id)
		}
		for _, row := range sectionTable(string(b), "Resources") {
			if isBlankTableRow(row) || isTableHeader(row, "Resource", "Purpose") {
				continue
			}
			if len(row) != 2 || !strings.HasPrefix(row[0], "resource:") || strings.TrimSpace(strings.TrimPrefix(row[0], "resource:")) == "" {
				p.Problems = append(p.Problems, "invalid TASK resource declaration: "+id+" | "+strings.Join(row, " | "))
				continue
			}
			dt.Resources = append(dt.Resources, row[0])
		}
		p.Tasks = append(p.Tasks, dt)
	}
	for _, t := range tasks {
		if t.status != "cancelled" && !seen[t.id] {
			p.Problems = append(p.Problems, "TASK missing from dispatch plan (declare REQ for unrelated legacy tasks): "+t.id)
		}
	}
	index := map[string]int{}
	for i, t := range p.Tasks {
		index[t.ID] = i
	}
	for _, row := range sectionTable(s, "Resource order") {
		if isBlankTableRow(row) || isTableHeader(row, "Before", "After", "Resource", "Reason") {
			continue
		}
		if len(row) != 4 || !strings.HasPrefix(row[0], "TASK-") || !strings.HasPrefix(row[1], "TASK-") {
			p.Problems = append(p.Problems, "invalid resource order: "+strings.Join(row, " | "))
			continue
		}
		a, oka := index[row[0]]
		b, okb := index[row[1]]
		if !oka || !okb || strings.TrimSpace(row[2]) == "" || strings.TrimSpace(row[3]) == "" {
			p.Problems = append(p.Problems, "invalid resource order: "+strings.Join(row, " | "))
			continue
		}
		p.Tasks[b].Before = append(p.Tasks[b].Before, p.Tasks[a].ID)
	}
	deps := map[string][]string{}
	for _, t := range p.Tasks {
		deps[t.ID] = append(append([]string{}, t.Dependencies...), t.Before...)
		for _, d := range deps[t.ID] {
			j, ok := index[d]
			if !ok {
				p.Problems = append(p.Problems, t.ID+" missing predecessor "+d)
			} else if p.Tasks[j].Wave >= t.Wave {
				p.Problems = append(p.Problems, t.ID+" predecessor must be in earlier wave: "+d)
			}
		}
	}
	if cycle := findTaskCycle(deps); cycle != "" {
		p.Problems = append(p.Problems, "dispatch dependency/resource cycle: "+cycle)
	}
	for i, a := range p.Tasks {
		for _, b := range p.Tasks[i+1:] {
			if a.Wave == b.Wave && DispatchConflict(a, b) {
				p.Warnings = append(p.Warnings, "same-wave write/resource overlap: "+a.ID+" / "+b.ID+"; S5 must resolve scope or ordering; runtime will serialize unresolved overlapping scopes")
			}
		}
	}
	if len(p.Tasks) == 0 {
		p.Problems = append(p.Problems, "dispatch plan must contain tasks")
	}
	sort.Strings(p.Problems)
	return p, nil
}

func isBlankTableRow(row []string) bool {
	if len(row) == 0 {
		return true
	}
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func isTableHeader(row []string, columns ...string) bool {
	if len(row) != len(columns) {
		return false
	}
	for i, column := range columns {
		if !strings.EqualFold(strings.TrimSpace(row[i]), column) {
			return false
		}
	}
	return true
}

func DispatchConflict(a, b DispatchTask) bool {
	for _, x := range a.Resources {
		for _, y := range b.Resources {
			if x == y {
				return true
			}
		}
	}
	for _, x := range a.Writes {
		for _, y := range b.Writes {
			if WriteOverlap(x, y) {
				return true
			}
		}
	}
	return false
}
func WriteOverlap(a, b string) bool {
	normalize := func(p string) string {
		p = strings.Trim(strings.TrimSpace(p), "`")
		if i := strings.IndexAny(p, "*?["); i >= 0 {
			p = p[:i]
		}
		return strings.TrimSuffix(filepath.ToSlash(filepath.Clean(p)), "/")
	}
	a, b = normalize(a), normalize(b)
	return a == "." || b == "." || a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

type DispatchFact struct{ State, Reason string }
type DispatchRow struct {
	Task   DispatchTask `json:"task"`
	State  string       `json:"state"`
	Reason string       `json:"reason,omitempty"`
}

// NextDispatch is deterministic and wave-barrier-free. capacity is available
// slots (not total slots). Facts and results are projections, not mutations.
func NextDispatch(p *DispatchPlan, facts map[string]DispatchFact, capacity int) ([]DispatchRow, []string) {
	rows := []DispatchRow{}
	next := []string{}
	busy := []DispatchTask{}
	for _, t := range p.Tasks {
		f := facts[t.ID]
		if f.State != "" && f.State != "integrated" {
			busy = append(busy, t)
		}
	}
	tasks := append([]DispatchTask{}, p.Tasks...)
	score := func(id string) int {
		n := 0
		for _, t := range tasks {
			for _, d := range append(append([]string{}, t.Dependencies...), t.Before...) {
				if d == id {
					n++
				}
			}
		}
		return n
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := score(tasks[i].ID), score(tasks[j].ID)
		if a != b {
			return a > b
		}
		return tasks[i].ID < tasks[j].ID
	})
	for _, t := range tasks {
		row := DispatchRow{Task: t, State: "ready"}
		if f := facts[t.ID]; f.State != "" {
			row.State = f.State
			row.Reason = f.Reason
			rows = append(rows, row)
			continue
		}
		for _, d := range append(append([]string{}, t.Dependencies...), t.Before...) {
			if facts[d].State != "integrated" {
				row.State = "waiting"
				row.Reason = "await verified integration: " + d
				break
			}
		}
		if row.State == "ready" {
			for _, b := range busy {
				if DispatchConflict(t, b) {
					row.State = "waiting"
					row.Reason = "write/resource conflict: " + b.ID
					break
				}
			}
		}
		if row.State == "ready" {
			if capacity <= 0 {
				row.State = "queued"
				row.Reason = "available capacity exhausted"
			} else {
				next = append(next, t.ID)
				busy = append(busy, t)
				capacity--
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Task.Wave != rows[j].Task.Wave {
			return rows[i].Task.Wave < rows[j].Task.Wave
		}
		return rows[i].Task.ID < rows[j].Task.ID
	})
	return rows, next
}
