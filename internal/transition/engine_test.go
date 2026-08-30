package transition_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestApplyStartsLockedREQAndProducesSchemaValidRuntime(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	reqPath := "internal/transition/testdata/locked-req.md"
	reqHash := fileHash(t, filepath.Join(root, reqPath))

	next, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-001",
		ExpectedRevision: 0,
		Actor:            "user",
		Evidence: map[string]string{
			"req_lock_record":           "docs/requirements/REQ-002.md@0000000000000000000000000000000000000000000000000000000000000000",
			"loop_authorization_record": "user:/loop REQ-002",
		},
		REQ: &transition.LockedREQ{
			ID:         "REQ-002",
			Path:       reqPath,
			Version:    "v1.0.0",
			SHA256:     reqHash,
			ApprovedBy: "user",
			ApprovedAt: "2026-06-22T00:00:00Z",
		},
		OccurredAt: time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != 0 {
		t.Fatalf("expected new bound runtime revision 0, got %d", next.Revision)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeBytes(root, data); err != nil {
		t.Fatalf("transition produced invalid runtime: %v", err)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal) != 0 {
		t.Fatalf("bound runtime journal must start empty, got %q", journal)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	lifecycle := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "planning" || lifecycle["phase"] != "design" {
		t.Fatalf("unexpected lifecycle: %#v", lifecycle)
	}
	if state["runtime_id"] != "loop-REQ-002" {
		t.Fatalf("unexpected runtime id: %v", state["runtime_id"])
	}
	bound := state["bound_req"].(map[string]any)
	metadata := bound["metadata"].(map[string]any)
	if metadata["ui_impact"] != "none" {
		t.Fatalf("bound REQ must carry ui_impact, got %#v", bound)
	}
	if state["authorization"].(map[string]any)["command"] != "loop-harness req bind" {
		t.Fatalf("binding authorization must not masquerade as Claude /loop")
	}
}

func TestApplyRejectsMissingEvidenceWithoutMutation(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	before, _ := os.ReadFile(statePath)

	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-001",
		ExpectedRevision: 0,
		Actor:            "user",
		Evidence:         map[string]string{},
	})
	if err == nil {
		t.Fatal("expected missing evidence rejection")
	}
	after, _ := os.ReadFile(statePath)
	if string(before) != string(after) {
		t.Fatal("rejected transition mutated runtime")
	}
}

func TestApplyAdvancesPlanningPhaseAndRejectsIllegalTopLevelJump(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	seedPlanningArtifacts(t, statePath)
	reqPath := "internal/transition/testdata/locked-req.md"
	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-001",
		ExpectedRevision: 0,
		Actor:            "user",
		Evidence: map[string]string{
			"req_lock_record":           "docs/requirements/REQ-002.md@0000000000000000000000000000000000000000000000000000000000000000",
			"loop_authorization_record": "user:/loop REQ-002",
		},
		REQ: &transition.LockedREQ{
			ID: "REQ-002", Path: reqPath, Version: "v1.0.0",
			SHA256:     fileHash(t, filepath.Join(root, reqPath)),
			ApprovedBy: "user", ApprovedAt: "2026-06-22T00:00:00Z",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "PTR-PLAN-01", ExpectedRevision: 0, Actor: "hook_controller",
	}); err != nil {
		t.Fatalf("PTR-PLAN-01 must advance planning.design: %v", err)
	}
	if _, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "PTR-PLAN-02", ExpectedRevision: 1, Actor: "hook_controller",
	}); err != nil {
		t.Fatalf("PTR-PLAN-02 must advance planning.contracts: %v", err)
	}
	// TR-002 advances from formal planning.tasks to document_verification
	// once the seeded CONTRACTS/TASKS artifacts exist with the required status
	// values.
	next, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-002",
		ExpectedRevision: 2,
		Actor:            "orchestrator",
		Evidence:         map[string]string{},
	})
	if err != nil {
		t.Fatalf("TR-002 must pass when CONTRACTS/TASKS are seeded: %v", err)
	}
	lifecycle := next.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "document_verification" {
		t.Fatalf("expected document_verification state, got %#v", lifecycle)
	}

	if _, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-006",
		ExpectedRevision: 3,
		Actor:            "orchestrator",
		Evidence: map[string]string{
			"builder_report_record": "builder-report",
			"team_manifest_record":  "team-manifest",
		},
	}); err == nil {
		t.Fatal("expected illegal top-level jump rejection")
	}
}

func TestApplyPlanningCompleteAcceptsEnglishStatusFields(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	startLockedREQ(t, root, statePath, journalPath)
	seedPlanningArtifactsLang(t, statePath, true)
	advancePlanningToTasks(t, root, statePath, journalPath)

	if _, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "TR-002", ExpectedRevision: 2, Actor: "orchestrator", Evidence: map[string]string{},
	}); err != nil {
		t.Fatalf("TR-002 must accept the English Status fields used by TASK templates: %v", err)
	}
}

func TestApplyRejectsGlobalTransitionFromUndeclaredSource(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	refs := addRuntimeEvidence(t, root, statePath, 0, nil, "human_decision_record", "pause_record")
	before, _ := os.ReadFile(statePath)
	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "GTR-001", ExpectedRevision: 0, Actor: "user", Evidence: refs,
	})
	if err == nil || !strings.Contains(err.Error(), "source state") {
		t.Fatalf("illegal global transition must fail on source state, got %v", err)
	}
	after, _ := os.ReadFile(statePath)
	if string(before) != string(after) {
		t.Fatal("rejected global transition mutated runtime")
	}
}

func TestApplyRejectsEvidenceNotRegisteredInRuntime(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	startLockedREQ(t, root, statePath, journalPath)
	advancePlanningToTasks(t, root, statePath, journalPath)
	// TR-002 has empty required_evidence (BUG-PLANNING-SUBSTATE).
	// The semantic equivalent of the old PTR-PLAN-01 evidence-rejection test
	// is verifying that TR-002 still rejects planning→document_verification
	// when the planning_complete guard finds no CONTRACTS-*.md with
	// status=locked AND no TASK-*.md with status=complete.
	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "TR-002", ExpectedRevision: 2, Actor: "orchestrator",
		Evidence: map[string]string{},
	})
	if err == nil || !strings.Contains(err.Error(), "planning not complete") {
		t.Fatalf("TR-002 must fail when no CONTRACTS/TASKS exist: %v", err)
	}
}

func TestApplyDispatchesRegisteredGuardAndAction(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	startLockedREQ(t, root, statePath, journalPath)
	advancePlanningToTasks(t, root, statePath, journalPath)
	guardCalled := false
	// BUG-PLANNING-SUBSTATE: planning_complete is the direct-check
	// guard on TR-002. Override its registry slot here to verify dispatch
	// without depending on the on-disk CONTRACTS/TASKS artifacts. The action
	// dispatch contract is covered by the existing verification→acceptance
	// tests; TR-002's actions are empty after the planning sub-state collapse,
	// so this test focuses on the guard half of the dispatch contract.
	transition.RegisterGuard("planning_complete", func(state map[string]any, evidence map[string]string) error {
		guardCalled = true
		return nil
	})
	transition.RegisterGuard("tasks_checked", func(state map[string]any, evidence map[string]string) error {
		return nil
	})
	defer transition.InitGuardRegistry()
	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "TR-002", ExpectedRevision: 2, Actor: "orchestrator", Evidence: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !guardCalled {
		t.Fatalf("registry guard dispatch missing: guard=%v", guardCalled)
	}
}

func TestApplyRejectsEvidenceWithIncompatibleKind(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	startLockedREQ(t, root, statePath, journalPath)
	advancePlanningToTasks(t, root, statePath, journalPath)
	// BUG-PLANNING-SUBSTATE: the old PTR-PLAN-02 incompatibility
	// check is moot because TR-002 has empty required_evidence. Replace the
	// assertion with the new direct-check failure mode: TR-002 reports the
	// offending file (not "incompatible kind") when at least one of
	// CONTRACTS / TASKS is missing or has the wrong status.
	tempRoot := filepath.Dir(statePath)
	contractsDir := filepath.Join(tempRoot, "docs", "contracts")
	tasksDir := filepath.Join(tempRoot, "docs", "tasks")
	if err := os.MkdirAll(contractsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contractsDir, "CONTRACTS-draft.md"),
		[]byte("# CONTRACTS-draft\n\n> 状态：draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "TR-002", ExpectedRevision: 2, Actor: "orchestrator", Evidence: map[string]string{},
	})
	if err == nil || !strings.Contains(err.Error(), "no complete TASK document") {
		t.Fatalf("TR-002 must fail while the planning batch is incomplete (a locked contract now exists via the advance fixture, so the gap is the missing complete TASK): %v", err)
	}
}

func startLockedREQ(t *testing.T, root, statePath, journalPath string) {
	t.Helper()
	reqPath := "internal/transition/testdata/locked-req.md"
	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "TR-001", ExpectedRevision: 0, Actor: "user",
		Evidence: map[string]string{"req_lock_record": "REQ-002#lock", "loop_authorization_record": "user:/loop REQ-002"},
		REQ:      &transition.LockedREQ{ID: "REQ-002", Path: reqPath, Version: "v1.0.0", SHA256: fileHash(t, filepath.Join(root, reqPath)), ApprovedBy: "user", ApprovedAt: "2026-06-22T00:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func advancePlanningToTasks(t *testing.T, root, statePath, journalPath string) {
	t.Helper()
	tempRoot := filepath.Dir(statePath)
	// PTR-PLAN-01's register_design_documents demands a locked architecture
	// document on disk.
	archDir := filepath.Join(tempRoot, "docs", "design", "architecture")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archDir, "ARCHITECTURE-test.md"),
		[]byte("# ARCHITECTURE-test\n\n> 状态：locked\n> 版本：v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// PTR-PLAN-02's contracts_checked guard demands at least one real
	// contract on disk (the contractless-stage floor).
	contractsDir := filepath.Join(tempRoot, "docs", "contracts")
	if err := os.MkdirAll(contractsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contractsDir, "BE-test.md"),
		[]byte("# BE-test\n\n> 状态：locked\n> 版本：v1.0.0\n\n### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §1 | — |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for revision, id := range []string{"PTR-PLAN-01", "PTR-PLAN-02"} {
		if _, err := transition.Apply(root, statePath, journalPath, transition.Request{
			TransitionID: id, ExpectedRevision: revision, Actor: "hook_controller",
		}); err != nil {
			t.Fatalf("%s must advance formal planning: %v", id, err)
		}
	}
}

func addRuntimeEvidence(t *testing.T, root, statePath string, generation int, reviewRound any, kinds ...string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	path := "internal/transition/testdata/locked-req.md"
	hash := fileHash(t, filepath.Join(root, path))
	items, _ := state["evidence"].([]any)
	refs := make(map[string]string, len(kinds))
	for i, requiredKind := range kinds {
		id := fmt.Sprintf("evidence-%s-%d", strings.ReplaceAll(requiredKind, "_", "-"), i)
		items = append(items, map[string]any{
			"id": id, "kind": "human_decision", "path": path, "sha256": hash,
			"status": "valid", "baseline_generation": float64(generation), "review_round": reviewRound,
			"produced_by": []any{"user"}, "invalidated_by": nil, "invalidation_rule": nil,
			"invalidation_reason": nil, "responsibility_id": nil, "scope_refs": []any{},
		})
		refs[requiredKind] = id
	}
	state["evidence"] = items
	encoded, _ := json.MarshalIndent(state, "", " ")
	if err := os.WriteFile(statePath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return refs
}

func copyInactiveRuntime(t *testing.T, root string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-inactive"
	state["revision"] = float64(0)
	state["authorization"] = map[string]any{
		"mode": "none", "command": nil, "actor": nil, "occurred_at": nil,
	}
	state["bound_req"] = nil
	state["baseline"] = map[string]any{"generation": float64(0), "captured_at": nil}
	state["review"] = map[string]any{"round": float64(0), "clean_round": nil}
	state["documents"] = []any{}
	state["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}}
	state["evidence"] = []any{}
	state["blockers"] = []any{}
	state["pause"] = nil
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil}
	state["last_transition"] = nil
	state["lifecycle"] = map[string]any{"state": "inactive", "phase": nil, "phase_revision": float64(0)}
	// BUG-PLANNING-SUBSTATE: state["root"] lets the planning_complete
	// direct-check guard find CONTRACTS/TASKS under the temp root instead
	// of falling back to "." (which is the test's working directory and
	// contains no planning artifacts).
	state["root"] = dir
	data, err = json.MarshalIndent(state, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return statePath, journalPath
}

// seedPlanningArtifacts creates the minimum planning surface both TR-002
// guards require: a locked CONTRACTS index whose 需求覆盖矩阵 declares the
// clause universe, a locked contract file, and a complete TASK declaring
// coverage + closing contract (L3-S4 v4.0.1 — the batch-quality guard
// consumes structure, not just Status lines). The temp root is derived from
// statePath (see copyInactiveRuntime which writes state["root"]).
func seedPlanningArtifacts(t *testing.T, statePath string) {
	seedPlanningArtifactsLang(t, statePath, false)
}

func seedPlanningArtifactsLang(t *testing.T, statePath string, english bool) {
	t.Helper()
	root := filepath.Dir(statePath)
	contractsDir := filepath.Join(root, "docs", "contracts")
	tasksDir := filepath.Join(root, "docs", "tasks")
	archDir := filepath.Join(root, "docs", "design", "architecture")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archStatus := "> 状态：locked"
	if english {
		archStatus = "> Status: locked"
	}
	if err := os.WriteFile(filepath.Join(archDir, "ARCHITECTURE-test.md"),
		[]byte("# ARCHITECTURE-test\n\n"+archStatus+"\n> 版本：v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(contractsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	status := func(value string) string {
		if english {
			return "> Status: " + value
		}
		return "> 状态：" + value
	}
	index := "# CONTRACTS-test\n\n" + status("locked") + "\n> 版本：v1.0.0\n\n" +
		"## 需求覆盖矩阵\n\n" +
		"| REQ source_ref | Rule → CASE | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n" +
		"|:--|:--|:--|:--|:--|\n" +
		"| REQ-test | — | — | BE-TEST §1 | — |\n"
	if err := os.WriteFile(filepath.Join(contractsDir, "CONTRACTS-test.md"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contractsDir, "BE-TEST.md"),
		[]byte("# BE-TEST\n\n"+status("locked")+"\n> 版本：v1.0.0\n\n### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §1 | — |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := "# TASK-test\n\n" + status("complete") + "\n> Version: v1.0.0\n> Primary contract: BE-TEST\n\n" +
		"## 3. Delivered Clauses\n\n" +
		"| Contract | Delivered clauses |\n|:--|:--|\n| BE-TEST | §1 |\n\n" +
		"## 7. Closing Contract\n\n```text\nassert BE-TEST §1 == satisfied\n```\n"
	if err := os.WriteFile(filepath.Join(tasksDir, "TASK-test.md"), []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return transition.SHA256(data)
}

// TestParseUIImpactRejectsDriftedReflection verifies that a §C
// reflection that disagrees with the top anchor field must be refused
// (a drifted echo would silently route a changed REQ through the none path).
func TestParseUIImpactRejectsDriftedReflection(t *testing.T) {
	base := "> 状态：locked\n> 版本：v1.0.0\n"
	// The template's real §C reflection is a table row — pin THAT format
	// (a colon-form-only parser would be an unsafe fallback).
	drifted := base + "> UI impact：none\n\n# 内容\n\n## §C 具体需求\n\n| 字段 | 内容 |\n|:--|:--|\n| UI impact（引自顶部） | changed（顶部 blockquote 是唯一被解析的位置，本节只回显） |\n"
	if _, err := transition.ParseUIImpactForTest(drifted); err == nil || !strings.Contains(err.Error(), "inconsistent") {
		t.Fatalf("drifted template-row reflection must be refused, got %v", err)
	}
	aligned := base + "> UI impact：changed\n\n# 内容\n\n## §C 具体需求\n\n| 字段 | 内容 |\n|:--|:--|\n| UI impact（引自顶部） | changed（回显） |\n"
	value, err := transition.ParseUIImpactForTest(aligned)
	if err != nil || value != "changed" {
		t.Fatalf("aligned template-row reflection must pass, got %q %v", value, err)
	}
	// The untouched template placeholder row must not read as a mismatch.
	placeholder := base + "> UI impact：none\n\n# 内容\n\n## §C 具体需求\n\n| 字段 | 内容 |\n|:--|:--|\n| UI impact（引自顶部） | none / changed / unknown（顶部 blockquote 是唯一被解析的位置，本节只回显，不独立声明） |\n"
	top, err := transition.ParseUIImpactForTest(placeholder)
	if err != nil || top != "none" || !strings.HasPrefix(top, "none") {
		t.Fatalf("legal top value with placeholder row must pass, got %q %v", top, err)
	}
}
