package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// TestS4TaskSplitPipelineE2E walks the S4 exit path end to end (L3-S4 v4.0.1):
// tasks check red/green with named problems → TR-002 dual guards (planning
// complete pointing at PTR-PLAN-02, tasks_checked surfacing batch problems at
// guard level) → register_planning_tasks with EMPTY evidence (TR-002's
// required_evidence is empty — an evidence precondition here would deadlock
// the transition) → repair-loop second pass over same-generation entries
// (replace, not stack).
func TestS4TaskSplitPipelineE2E(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"docs/contracts", "docs/tasks", "docs/requirements", "docs/design/architecture", ".claude"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")

	// Locked REQ (UI impact none skips the prototype gate) + clause universe
	// in the CONTRACTS index + a locked contract + a complete two-task batch.
	write("docs/design/architecture/ARCHITECTURE-600.md", "# ARCHITECTURE-600\n\n> 状态：locked\n> 版本：v1.0.0\n")
	write("docs/requirements/REQ-600.md", "# REQ-600\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n\n"+
		"| 编号 | 模块 | 需求 | 服务于 | 优先级 |\n|:--|:--|:--|:--|:--|\n| FR-601 | wb6 | 提交 | A1 | Must |\n")
	write("docs/contracts/BE-600.md", "# BE-600\n\n> 状态：locked\n> 版本：v1.0.0\n\n"+
		"### 需求条款映射\n\n| REQ source_ref | Rule / CASE / Story / PATH | 本合同条款 | 验收标准 |\n|:--|:--|:--|:--|\n"+
		"| REQ-600/FR-601 | — | §1 | 可提交 |\n"+
		"| REQ-600/FR-601 | — | §2 | 拒绝重复 |\n")
	write("docs/contracts/CONTRACTS-600.md", "# CONTRACTS-600\n\n> 状态：locked\n> 版本：v1.0.0\n\n"+
		"## 需求覆盖矩阵\n\n| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:--|:--|:--|:--|\n"+
		"| REQ-600/FR-601 | — | BE-600 §1 | — |\n| REQ-600/FR-601 | — | BE-600 §2 | — |\n")
	write("docs/tasks/TASK-600-01.md", "# TASK-600-01\n\n> Status: complete\n> Version: v1.0.0\n> Primary contract: BE-600\n\n"+
		"## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-600 | §1 |\n\n"+
		"## 7. Closing Contract\n\n```text\nassert BE-600 §1 == satisfied\nassert make test == pass\n```\n\n"+
		"## 8. Dependencies\n\n| Dependency | Required evidence | Status |\n|:--|:--|:--|\n| TASK-600-02 | `ev-01` | pending |\n")
	write("docs/tasks/TASK-600-02.md", "# TASK-600-02\n\n> Status: complete\n> Version: v1.0.0\n> Primary contract: BE-600\n\n"+
		"## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-600 | §2 |\n\n"+
		"## 7. Closing Contract\n\n```text\nassert BE-600 §2 == satisfied\nassert make test == pass\n```\n\n"+
		"## 8. Dependencies\n\n| Dependency | Required evidence | Status |\n|:--|:--|:--|\n| N/A | — | satisfied |\n")

	// --- green: full batch reconciles ---
	out, _, code := run("tasks", "check", "--root", root)
	if code != 0 || !strings.Contains(out, "clauses 2/2 covered") {
		t.Fatalf("green batch must pass: code=%d out=%s", code, out)
	}

	// --- red: each break names its problem (拒绝即指路) ---
	breaks := []struct {
		name, file, mutate, want string
	}{
		{"phantom clause", "docs/tasks/TASK-600-01.md",
			strings.Replace(readFile(t, root, "docs/tasks/TASK-600-01.md"), "| BE-600 | §1 |", "| BE-600 | §1, §9 |", 1),
			"TASK-600-01 declares BE-600 §9"},
		{"uncovered clause cell", "docs/tasks/TASK-600-02.md",
			strings.Replace(readFile(t, root, "docs/tasks/TASK-600-02.md"), "| BE-600 | §2 |", "| BE-600 |  |", 1),
			"BE-600 §2 is not covered"},
		{"missing dependency target", "docs/tasks/TASK-600-01.md",
			strings.Replace(readFile(t, root, "docs/tasks/TASK-600-01.md"), "TASK-600-02 | `ev-01`", "TASK-600-GHOST | `ev-01`", 1),
			"depends on TASK-600-GHOST"},
		{"dependency cycle", "docs/tasks/TASK-600-02.md",
			strings.Replace(readFile(t, root, "docs/tasks/TASK-600-02.md"), "| N/A | — | satisfied |", "| TASK-600-01 | `ev-02` | pending |", 1),
			"dependency cycle: TASK-600-01 -> TASK-600-02 -> TASK-600-01"},
		{"partial batch", "docs/tasks/TASK-600-02.md",
			strings.Replace(readFile(t, root, "docs/tasks/TASK-600-02.md"), "> Status: complete", "> Status: draft", 1),
			`status is "draft"`},
		{"cancelled leaves its clause uncovered", "docs/tasks/TASK-600-02.md",
			strings.Replace(readFile(t, root, "docs/tasks/TASK-600-02.md"), "> Status: complete", "> Status: cancelled", 1),
			"BE-600 §2 is not covered"},
	}
	for _, brk := range breaks {
		original := readFile(t, root, brk.file)
		write(brk.file, brk.mutate)
		_, stderr, code := run("tasks", "check", "--root", root)
		if code == 0 || !strings.Contains(stderr, brk.want) {
			t.Fatalf("[%s] must be named: code=%d stderr=%s", brk.name, code, stderr)
		}
		write(brk.file, original)
	}
	// restore green after mutation loop
	out, _, code = run("tasks", "check", "--root", root)
	if code != 0 {
		t.Fatalf("restored batch must pass again: %s", out)
	}

	// --- planning_complete points at PTR-PLAN-02 when nothing is registered ---
	if _, stderr, code := run("req", "bind", "--root", root, "--approved-by", "bob"); code != 0 {
		t.Fatalf("bind failed: %s", stderr)
	}
	if _, stderr, code := run("runtime", "transition", "--root", root,
		"--id", "PTR-PLAN-01", "--expected-revision", "0", "--actor", "orchestrator"); code != 0 {
		t.Fatalf("PTR-PLAN-01 failed: %s", stderr)
	}
	// Jump the phase to tasks without running PTR-PLAN-02 (the state edit
	// mirrors what recovery/test fixtures do) — the guard must point at the
	// missing registration, not scan filenames.
	state := readJSONMap(t, statePath)
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "tasks", "phase_revision": float64(1)}
	writeJSONMap(t, statePath, state)
	_, stderr, code := run("runtime", "transition", "--root", root,
		"--id", "TR-002", "--expected-revision", "1", "--actor", "orchestrator")
	if code == 0 || !strings.Contains(stderr, "no locked contract registered") || !strings.Contains(stderr, "PTR-PLAN-02") {
		t.Fatalf("planning_complete must point at PTR-PLAN-02: code=%d stderr=%s", code, stderr)
	}
	// return to the organic phase chain and register the contracts
	state = readJSONMap(t, statePath)
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": float64(1)}
	writeJSONMap(t, statePath, state)
	if _, stderr, code = run("runtime", "transition", "--root", root,
		"--id", "PTR-PLAN-02", "--expected-revision", "1", "--actor", "orchestrator"); code != 0 {
		t.Fatalf("PTR-PLAN-02 failed: %s", stderr)
	}

	// --- tasks_checked surfaces at guard level, not just CLI ---
	write("docs/tasks/TASK-600-02.md", strings.Replace(readFile(t, root, "docs/tasks/TASK-600-02.md"), "| BE-600 | §2 |", "| BE-600 | §2, §7 |", 1))
	_, stderr, code = run("runtime", "transition", "--root", root,
		"--id", "TR-002", "--expected-revision", "2", "--actor", "orchestrator")
	if code == 0 || !strings.Contains(stderr, "tasks_checked") || !strings.Contains(stderr, "BE-600 §7") {
		t.Fatalf("tasks_checked must reject at guard level naming the phantom clause: code=%d stderr=%s", code, stderr)
	}
	write("docs/tasks/TASK-600-02.md", strings.Replace(readFile(t, root, "docs/tasks/TASK-600-02.md"), ", §7", "", 1))

	// --- cancelled task: skipped, excluded from coverage aggregation ---
	write("docs/tasks/TASK-600-03.md", "# TASK-600-03\n\n> Status: cancelled\n> Version: v1.0.0\n> Primary contract: BE-600\n\n"+
		"## 7. Closing Contract\n\n```text\nassert nothing == satisfied\n```\n")
	out, _, code = run("tasks", "check", "--root", root)
	if code != 0 || !strings.Contains(out, "1 cancelled") {
		t.Fatalf("cancelled task must be skipped without breaking the batch: code=%d out=%s", code, out)
	}

	// --- TR-002 passes with EMPTY evidence and registers the batch ---
	if _, stderr, code = run("runtime", "transition", "--root", root,
		"--id", "TR-002", "--expected-revision", "2", "--actor", "orchestrator"); code != 0 {
		t.Fatalf("TR-002 must pass with empty evidence (required_evidence is empty): %s", stderr)
	}
	state = readJSONMap(t, statePath)
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "document_verification" {
		t.Fatalf("lifecycle = %#v, want document_verification", lifecycle)
	}
	docs, _ := state["documents"].([]any)
	taskEntries := map[string]map[string]any{}
	for _, raw := range docs {
		doc, _ := raw.(map[string]any)
		if doc != nil && doc["kind"] == "task" {
			id, _ := doc["id"].(string)
			taskEntries[id] = doc
		}
	}
	if len(taskEntries) != 2 {
		t.Fatalf("register_planning_tasks must register the complete batch (cancelled excluded), got %d entries", len(taskEntries))
	}
	for id, entry := range taskEntries {
		if entry["author_agent_id"] != "orchestrator" {
			t.Fatalf("%s author_agent_id = %v, want orchestrator", id, entry["author_agent_id"])
		}
		diskData, _ := os.ReadFile(filepath.Join(root, "docs", "tasks", id+".md"))
		if fmt.Sprintf("%x", sha256.Sum256(diskData)) != entry["sha256"] {
			t.Fatalf("%s registered sha must match disk", id)
		}
	}

	// --- repair loop second pass: same-generation registration replaces ---
	// TR-004 (document_fix_required) is the organic return trigger; firing it
	// needs document_review_record evidence, so mirror its effect with the
	// documented state-edit precedent and re-walk the phase chain.
	state = readJSONMap(t, statePath)
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "design", "phase_revision": float64(1)}
	writeJSONMap(t, statePath, state)
	rev := int(state["revision"].(float64))
	write("docs/tasks/TASK-600-01.md", strings.Replace(readFile(t, root, "docs/tasks/TASK-600-01.md"), "v1.0.0", "v1.1.0", 1))
	for _, step := range []struct {
		id  string
		rev int
	}{
		{"PTR-PLAN-01", rev},
		{"PTR-PLAN-02", rev + 1},
		{"TR-002", rev + 2},
	} {
		if _, stderr, code := run("runtime", "transition", "--root", root,
			"--id", step.id, "--expected-revision", fmt.Sprintf("%d", step.rev), "--actor", "orchestrator"); code != 0 {
			t.Fatalf("%s rework pass failed: %s", step.id, stderr)
		}
	}
	state = readJSONMap(t, statePath)
	docs, _ = state["documents"].([]any)
	count, newSHA := 0, ""
	for _, raw := range docs {
		doc, _ := raw.(map[string]any)
		if doc != nil && doc["kind"] == "task" && doc["id"] == "TASK-600-01" {
			count++
			newSHA, _ = doc["sha256"].(string)
		}
	}
	if count != 1 {
		t.Fatalf("same-generation re-registration must replace not stack, got %d TASK-600-01 entries", count)
	}
	diskData, _ := os.ReadFile(filepath.Join(root, "docs", "tasks", "TASK-600-01.md"))
	if newSHA != fmt.Sprintf("%x", sha256.Sum256(diskData)) {
		t.Fatal("re-registered sha must match the revised disk file")
	}
}

// TestS4TasksCheckEmptyRoot pins the two floor problems: no batch and no
// clause universe must be named, never pass vacuously (the false-green hole).
func TestS4TasksCheckEmptyRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"tasks", "check", "--root", root, "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("empty repo check should exit 0 with problems in JSON envelope, got %d %s", code, stderr.String())
	}
	var result struct {
		Tasks    int      `json:"tasks"`
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Tasks != 0 {
		t.Fatalf("tasks = %d, want 0", result.Tasks)
	}
	joined := strings.Join(result.Problems, "; ")
	if !strings.Contains(joined, "no TASK documents") || !strings.Contains(joined, "clause universe is empty") {
		t.Fatalf("floor problems must be named, got: %s", joined)
	}
}

// TestTasksCheckFlagsNonTaskDependency verifies that a dependency row the
// DAG does not track must be named, not silently dropped.
func TestTasksCheckFlagsNonTaskDependency(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"docs/contracts", "docs/tasks", "docs/requirements"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/contracts/BE-800.md", "# BE-800\n\n> 状态：locked\n> 版本：v1.0.0\n\n### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §1 | — |\n")
	write("docs/contracts/CONTRACTS-800.md", "# CONTRACTS-800\n\n> 状态：locked\n> 版本：v1.0.0\n\n## 需求覆盖矩阵\n\n| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:--|:--|:--|:--|\n| REQ-800/FR-801 | — | BE-800 §1 | — |\n")
	write("docs/tasks/TASK-800-01.md", "# TASK-800-01\n\n> Status: complete\n> Version: v1.0.0\n> Primary contract: BE-800\n\n"+
		"## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-800 | §1 |\n\n"+
		"## 7. Closing Contract\n\n```text\nassert BE-800 §1 == satisfied\n```\n\n"+
		"## 8. Dependencies\n\n| Dependency | Required evidence | Status |\n|:--|:--|:--|\n| assignment-42 | `ev-1` | pending |\n")
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"tasks", "check", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "assignment-42") || !strings.Contains(stderr.String(), "not machine-tracked") {
		t.Fatalf("non-TASK dependency must be named, got: %s", stderr.String())
	}
}

// TestContractsCheckFlagsClauseNumberDrift verifies that an index cell
// citing a §n the target contract never declares must be flagged.
func TestContractsCheckFlagsClauseNumberDrift(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/contracts/BE-810.md", "# BE-810\n\n> 状态：locked\n> 版本：v1.0.0\n\n### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §1 | — |\n")
	write("docs/contracts/CONTRACTS-810.md", "# CONTRACTS-810\n\n> 状态：locked\n> 版本：v1.0.0\n\n## 需求覆盖矩阵\n\n| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:--|:--|:--|:--|\n| REQ-810/FR-811 | — | BE-810 §2 | — |\n")
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"contracts", "check", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "never declares that clause number") {
		t.Fatalf("clause number drift must be flagged, got: %s", stderr.String())
	}
}

// TestContractsCheckClauseNumberPrecision verifies that §1 must not
// satisfy §10 — clause numbers compare as a set, not substrings.
func TestContractsCheckClauseNumberPrecision(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Target declares only §10; the index cites §1 — substring matching
	// used to let this pass.
	write("docs/contracts/BE-820.md", "# BE-820\n\n> 状态：locked\n> 版本：v1.0.0\n\n### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §10 | — |\n")
	write("docs/contracts/CONTRACTS-820.md", "# CONTRACTS-820\n\n> 状态：locked\n> 版本：v1.0.0\n\n## 需求覆盖矩阵\n\n| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:--|:--|:--|:--|\n| REQ-820/FR-821 | — | BE-820 §1 | — |\n")
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"contracts", "check", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "never declares that clause number") {
		t.Fatalf("§1-vs-§10 precision must flag the drift, got: %s", stderr.String())
	}
}

// TestTasksCheckReportsReferenceLoads pins L3-S5 v4.4.1: the context-budget
// figures are informational output (never problems) for the S5 executability
// reviewer.
func TestTasksCheckReportsReferenceLoads(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"docs/contracts", "docs/tasks", "docs/requirements", "docs/design/architecture"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/requirements/REQ-830.md", "# REQ-830\n\n> 状态：locked\n> 版本：v1.0.0\n\n| 编号 | 模块 | 需求 | 服务于 | 优先级 |\n|:--|:--|:--|:--|:--|\n| FR-831 | wb | 提交 | A1 | Must |\n")
	write("docs/contracts/BE-830.md", "# BE-830\n\n> 状态：locked\n> 版本：v1.0.0\n\n### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §1 | — |\n")
	write("docs/contracts/CONTRACTS-830.md", "# CONTRACTS-830\n\n> 状态：locked\n> 版本：v1.0.0\n\n## 需求覆盖矩阵\n\n| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:--|:--|:--|:--|\n| REQ-830/FR-831 | — | BE-830 §1 | — |\n")
	write("docs/tasks/TASK-830-01.md", "# TASK-830-01\n\n> Status: complete\n> Version: v1.0.0\n> Primary contract: BE-830\n\n"+
		"## 2. Document Manifest\n\n| Order | Kind | ID | Path | Clauses |\n|:--|:--|:--|:--|:--|\n| 1 | contract | BE-830 | `docs/contracts/BE-830.md` | §1 |\n| 2 | req | REQ-830 | `docs/requirements/REQ-830.md` | FR |\n\n"+
		"## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-830 | §1 |\n\n"+
		"## 4. Scope\n\n| Type | Paths / Commands |\n|:--|:--|\n| read paths | `docs/contracts/BE-830.md` |\n| prospective write paths | `internal/x/a.go`, `internal/x/b.go` |\n\n"+
		"## 7. Closing Contract\n\n```text\nassert BE-830 §1 == satisfied\n```\n")
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"tasks", "check", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("tasks check failed: %s", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "TASK-830-01: required reading ~") || !strings.Contains(out, "write paths 2") || !strings.Contains(out, "reference only") {
		t.Fatalf("reference load info line missing or wrong: %s", out)
	}
}
