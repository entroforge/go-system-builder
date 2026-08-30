package semantic

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// S4's mechanical close (L3-S4 v4.0.1). Division of labor: the CONTRACTS
// index owns the clause universe; TASK §3 owns coverage declarations; this
// check reconciles both directions plus batch completeness, closing-contract
// presence, and DAG acyclicity. Non-goal: free-text clause semantics.

var (
	taskStatusField  = regexp.MustCompile(`(?m)^>\s*(?:Status|状态)\s*[:：]\s*(.+?)\s*$`)
	taskPrimaryField = regexp.MustCompile(`(?m)^>\s*Primary contract:\s*(\S+)`)
	taskClauseNumber = regexp.MustCompile(`§\s*(\d+)`)
	taskDepReference = regexp.MustCompile(`^TASK-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	taskContractCell = regexp.MustCompile(`^(FE|BE|SYNC)-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
)

type TaskCheckResult struct {
	Tasks          int      `json:"tasks"`
	Cancelled      int      `json:"cancelled"`
	ClausesTotal   int      `json:"clauses_total"`
	ClausesCovered int      `json:"clauses_covered"`
	Problems       []string `json:"problems,omitempty"`
	// ReferenceLoads are informational per-task context-budget figures
	// (required-reading bytes + write-path count) for the S5 executability
	// reviewer — deliberately NOT problems: avoiding subagent compact is a
	// prompt-and-review concern, not a hard gate (L3-S5 v4.4.1).
	ReferenceLoads []string `json:"reference_loads,omitempty"`
}

type taskDocument struct {
	id            string
	rel           string
	status        string
	contract      string
	hasClose      bool
	clauses       []string // expanded "{contract} §{n}"
	deps          []string
	problems      []string // parse-level problems surfaced by TasksCheck
	manifestPaths []string
	writePaths    []string
}

// TasksCheck is S4's exit-side reconciliation. It runs as the tasks_checked
// guard on TR-002 and via `tasks check` for agent self-service.
func TasksCheck(root string) (TaskCheckResult, error) {
	result := TaskCheckResult{Problems: []string{}}
	tasks, err := loadTaskDocuments(root)
	if err != nil {
		return result, err
	}
	result.Tasks = len(tasks)
	if len(tasks) == 0 {
		result.Problems = append(result.Problems, "no TASK documents under docs/tasks — write the batch before checking")
		// Fall through: the clause-universe floor must still be named so an
		// empty repo cannot look half-green.
	}

	// Clause universe: the CONTRACTS index matrix is the single home (L3-S4 v4).
	universe := map[string]bool{}
	indexFiles, _ := filepath.Glob(filepath.Join(root, "docs", "contracts", "CONTRACTS-*.md"))
	for _, indexFile := range indexFiles {
		if strings.Contains(strings.ToLower(filepath.Base(indexFile)), "template") {
			continue
		}
		data, err := os.ReadFile(indexFile)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", indexFile, err)
		}
		for _, cell := range contractClauseCellPattern.FindAllString(string(data), -1) {
			universe[normalizeClauseCell(cell)] = true
		}
	}
	if len(universe) == 0 {
		result.Problems = append(result.Problems, "clause universe is empty — the CONTRACTS index (需求覆盖矩阵, one `{id} §{n}` cell per clause) is missing or has no cells")
	}

	// Contract inventory for primary-contract existence and index coverage.
	contractIDs := map[string]bool{}
	entries, _ := os.ReadDir(filepath.Join(root, "docs", "contracts"))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || strings.Contains(strings.ToLower(name), "template") {
			continue
		}
		id := strings.TrimSuffix(name, ".md")
		if taskContractCell.MatchString(id) {
			contractIDs[id] = true
		}
	}

	// Per-task checks. Coverage aggregates only complete tasks — cancelled
	// declarations drop out so an uncovered clause resurfaces (no silent gap).
	complete := 0
	covered := map[string]bool{}
	byID := map[string]*taskDocument{}
	deps := map[string][]string{}
	for _, task := range tasks {
		byID[task.id] = task
		result.Problems = append(result.Problems, task.problems...)
		if task.status == "cancelled" {
			result.Cancelled++
			continue
		}
		if task.status == "complete" {
			complete++
		} else {
			result.Problems = append(result.Problems, fmt.Sprintf("%s: status is %q — the batch must be fully complete (or cancelled) before TR-002", task.rel, task.status))
		}
		if task.contract == "" {
			result.Problems = append(result.Problems, fmt.Sprintf("%s: missing \"> Primary contract:\" header", task.rel))
		} else if !contractIDs[task.contract] {
			result.Problems = append(result.Problems, fmt.Sprintf("%s: primary contract %s has no file under docs/contracts", task.rel, task.contract))
		}
		if !task.hasClose {
			result.Problems = append(result.Problems, fmt.Sprintf("%s: no Closing Contract block with assert lines", task.rel))
		}
		for _, clause := range task.clauses {
			if len(universe) > 0 && !universe[clause] {
				result.Problems = append(result.Problems, fmt.Sprintf("%s declares %s which is not in the CONTRACTS index universe (phantom clause)", task.id, clause))
				continue
			}
			covered[clause] = true
		}
	}
	if complete == 0 {
		result.Problems = append(result.Problems, "no complete TASK document — planning requires at least one")
	}

	if len(universe) > 0 {
		var uncovered []string
		for clause := range universe {
			if !covered[clause] {
				uncovered = append(uncovered, clause)
			}
		}
		sort.Strings(uncovered)
		for _, clause := range uncovered {
			result.Problems = append(result.Problems, fmt.Sprintf("clause %s is not covered by any TASK §3 declaration", clause))
		}
		// Contract↔index direction: a contract on disk with no cell in the
		// universe starves coverage invisibly (the false-green hole).
		ids := make([]string, 0, len(contractIDs))
		for id := range contractIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			hasCell := false
			for clause := range universe {
				if fields := strings.Fields(clause); len(fields) == 2 && fields[0] == id {
					hasCell = true
					break
				}
			}
			if !hasCell {
				result.Problems = append(result.Problems, fmt.Sprintf("contract %s exists on disk but the CONTRACTS index has no clause cell for it", id))
			}
		}
	}

	// Dependencies: reference existence, dead (cancelled) targets, cycles.
	for _, task := range tasks {
		for _, dep := range task.deps {
			target, ok := byID[dep]
			if !ok {
				result.Problems = append(result.Problems, fmt.Sprintf("%s depends on %s which does not exist under docs/tasks", task.id, dep))
				continue
			}
			if target.status == "cancelled" {
				result.Problems = append(result.Problems, fmt.Sprintf("%s depends on cancelled %s — reassign or drop the dependency", task.id, dep))
				continue
			}
			if task.status != "cancelled" {
				deps[task.id] = append(deps[task.id], dep)
			}
		}
	}
	if cycle := findTaskCycle(deps); cycle != "" {
		result.Problems = append(result.Problems, "dependency cycle: "+cycle)
	}

	result.ClausesTotal = len(universe)
	result.ClausesCovered = len(covered)
	// Informational context-budget figures for the S5 reviewer (no
	// thresholds — the compact-avoidance warning lives in the split prompt
	// and the review checklist, not as a machine gate).
	for _, task := range tasks {
		if task.status == "cancelled" {
			continue
		}
		readBytes := 0
		for _, p := range task.manifestPaths {
			clean := filepath.Clean(p)
			if clean == "" || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(root, clean)); err == nil {
				readBytes += len(data)
			}
		}
		result.ReferenceLoads = append(result.ReferenceLoads,
			fmt.Sprintf("%s: required reading ~%dKB, write paths %d (reference only — avoid subagent compact; split or trim if it feels too big)",
				task.id, readBytes/1024, len(task.writePaths)))
	}
	return result, nil
}

// TaskBatchComplete reports S4 batch readiness only: every TASK-*.md
// declares complete or cancelled and at least one is complete. Quality
// checks (coverage/DAG) live in TasksCheck; the planning_complete guard
// consumes this so state readiness and batch quality stay separable.
func TaskBatchComplete(root string) (complete, cancelled int, problems []string, err error) {
	tasks, err := loadTaskDocuments(root)
	if err != nil {
		return 0, 0, nil, err
	}
	for _, task := range tasks {
		switch task.status {
		case "complete":
			complete++
		case "cancelled":
			cancelled++
		case "":
			problems = append(problems, fmt.Sprintf("%s: no Status field — batch membership must be declared", task.rel))
		default:
			problems = append(problems, fmt.Sprintf("%s: status is %q, want complete (or cancelled)", task.rel, task.status))
		}
	}
	if complete == 0 {
		problems = append(problems, "no complete TASK document under docs/tasks")
	}
	return complete, cancelled, problems, nil
}

func loadTaskDocuments(root string) ([]*taskDocument, error) {
	dir := filepath.Join(root, "docs", "tasks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read docs/tasks: %w", err)
	}
	var tasks []*taskDocument
	for _, entry := range entries {
		name := entry.Name()
		id := strings.TrimSuffix(name, ".md")
		if entry.IsDir() || !strings.HasSuffix(name, ".md") ||
			strings.Contains(strings.ToLower(name), "template") ||
			strings.EqualFold(name, "README.md") ||
			!strings.HasPrefix(id, "TASK-") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read docs/tasks/%s: %w", name, err)
		}
		content := string(data)
		task := &taskDocument{
			id:       id,
			rel:      "docs/tasks/" + name,
			hasClose: strings.Contains(content, "Closing Contract") && strings.Contains(content, "assert "),
		}
		if m := taskStatusField.FindStringSubmatch(content); m != nil {
			task.status = strings.TrimSpace(m[1])
		}
		if m := taskPrimaryField.FindStringSubmatch(content); m != nil {
			task.contract = m[1]
		}
		for _, row := range sectionTable(content, "Delivered Clauses") {
			if len(row) >= 2 && taskContractCell.MatchString(row[0]) {
				for _, m := range taskClauseNumber.FindAllStringSubmatch(row[1], -1) {
					task.clauses = append(task.clauses, row[0]+" §"+m[1])
				}
			}
		}
		for _, row := range sectionTable(content, "Document Manifest") {
			if len(row) >= 4 {
				p := strings.Trim(row[3], "` ")
				if p != "" && p != "{path}" && !strings.HasPrefix(p, ":--") && p != "Path" {
					task.manifestPaths = append(task.manifestPaths, p)
				}
			}
		}
		for _, row := range sectionTable(content, "Scope") {
			if len(row) >= 2 && strings.Contains(strings.ToLower(row[0]), "write") {
				for _, cell := range strings.Split(row[1], ",") {
					p := strings.TrimSpace(strings.Trim(cell, "` "))
					if p != "" && p != "{path}" && !strings.HasPrefix(p, ":--") {
						task.writePaths = append(task.writePaths, p)
					}
				}
			}
		}
		for _, row := range sectionTable(content, "Dependencies") {
			cell := strings.TrimSpace(row[0])
			isMarker := cell == "" || cell == "Dependency" || cell == "TASK-{id}" || cell == "N/A" || cell == "—" || cell == "-" || strings.HasPrefix(cell, ":--")
			if !isMarker {
				if taskDepReference.MatchString(cell) {
					task.deps = append(task.deps, cell)
				} else {
					// A silently-dropped dependency is a declared ordering
					// the DAG never sees: name it.
					task.problems = append(task.problems, fmt.Sprintf("%s: dependency reference %q is not machine-tracked — only TASK-* ids join the DAG; write cross-task ordering as a TASK dependency or move it to the closing contract", task.id, cell))
				}
			}
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// sectionTable returns the markdown table rows of the section whose heading
// contains marker, stopping at the next heading. Rows keep their cells
// trimmed; header/separator rows are filtered by the caller's cell regexes.
func sectionTable(content, marker string) [][]string {
	var rows [][]string
	inSection := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if inSection {
				break
			}
			inSection = strings.Contains(trimmed, marker)
			continue
		}
		if !inSection || !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, cells)
	}
	return rows
}

// findTaskCycle returns the first dependency cycle as "A -> B -> A", or "".
// Three-color DFS modeled on the team-manifest cycle check, extended to
// report the path (L3-S4 v4.0.1).
func findTaskCycle(deps map[string][]string) string {
	const (
		unseen = iota
		visiting
		done
	)
	states := map[string]int{}
	var stack []string
	var visit func(string) string
	visit = func(id string) string {
		if states[id] == visiting {
			for i, frame := range stack {
				if frame == id {
					cycle := append([]string(nil), stack[i:]...)
					return strings.Join(append(cycle, id), " -> ")
				}
			}
			return id + " -> " + id
		}
		if states[id] == done {
			return ""
		}
		states[id] = visiting
		stack = append(stack, id)
		for _, dep := range deps[id] {
			if found := visit(dep); found != "" {
				return found
			}
		}
		stack = stack[:len(stack)-1]
		states[id] = done
		return ""
	}
	ids := make([]string, 0, len(deps))
	for id := range deps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if found := visit(id); found != "" {
			return found
		}
	}
	return ""
}

func normalizeClauseCell(cell string) string {
	parts := strings.Fields(cell)
	if len(parts) != 2 {
		return strings.Join(parts, " ")
	}
	return parts[0] + " §" + strings.TrimPrefix(parts[1], "§")
}
