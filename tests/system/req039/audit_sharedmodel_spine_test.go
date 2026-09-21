package req039_test

// This is the Hook/CLI-driven companion to the lower-level shared-model
// action audit. It drives the formal planning spine through S3, S4, S5 and
// S6, while checking that model registration and document-review subjects use
// the same committed Git source.

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func copySharedModelSpineFixture(t *testing.T, root string, workerOnlyRef bool) {
	t.Helper()
	src := filepath.Join(repoRoot(t), "docs", "examples", "shared-model", "project")
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}

	// The bound REQ is REQ-039, so the formal S3 selector must use an
	// explicitly bound index rather than filename fallback or an unrelated
	// CONTRACTS-001 batch.
	if err := os.Remove(filepath.Join(root, "docs", "dev", "contracts", "CONTRACTS-001.md")); err != nil {
		t.Fatal(err)
	}
	modelRef := "orders.schema.json"
	if workerOnlyRef {
		modelRef = "worker-only.json"
	}
	for _, name := range []string{"FE-001.md", "BE-001.md", "SYNC-001.md"} {
		path := filepath.Join(root, "docs", "dev", "contracts", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.ReplaceAll(string(data), "orders.schema.json", modelRef))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if workerOnlyRef {
		// This root-local file is intentionally created only after the first
		// state commit; it is an untracked disk artifact, not a real worker
		// worktree. The formal Git view must reject it until the model is
		// committed.
		return
	}
	writeWorkerOnlySchema(t, root)
}

func writeWorkerOnlySchema(t *testing.T, root string) {
	t.Helper()
	data := []byte(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$defs": {
    "request": {"type":"object","required":["order_id"],"additionalProperties":false,"properties":{"order_id":{"type":"string","minLength":1}}},
    "response": {"type":"object","required":["state"],"additionalProperties":false,"properties":{"state":{"enum":["cancelled","completed"]}}}
  }
}
`)
	if err := os.WriteFile(filepath.Join(root, "docs", "architecture", "data-model", "worker-only.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func installSharedContractIndex(t *testing.T, root string, state map[string]any) {
	t.Helper()
	// Seed the ordinary planning contract/evidence so the rest of the spine
	// has the same prerequisites as the existing organic Hook test. Replace
	// only the index with the shared-model contract batch under audit.
	req039fixtures.WritePlanningContractPass(t, root, state)
	index := `# Shared cancellation contracts

> Status: locked
> REQ: REQ-039
> Shared model policy: json-schema-v1

## Shared model baseline

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| cancelOrder | request | [request](../../architecture/data-model/worker-only.json#/$defs/request) | [FE](FE-001.md) [BE](BE-001.md) [SYNC](SYNC-001.md) | [valid](../../architecture/data-model/request.json) | [structural negative](../../architecture/data-model/invalid-request.json) |
| cancelOrder | response-200 | [response](../../architecture/data-model/worker-only.json#/$defs/response) | [FE](FE-001.md) [BE](BE-001.md) [SYNC](SYNC-001.md) | [valid](../../architecture/data-model/response.json) | N/A |

## Coverage

| REQ source_ref | Rule / acceptance | Contract clause | Verification |
|:---|:---|:---|:---|
| REQ-039/FR-001 | client validates the request | FE-001 §1 | TASK-039-01 §3 |
| REQ-039/FR-001 | service validates and persists cancellation | BE-001 §1 | TASK-039-01 §3 |
| REQ-039/FR-001 | API returns the cancellation result | SYNC-001 §1 | TASK-039-01 §3 |
| REQ-039/FR-001 | cancellation state is persisted | BE-039 §1 | TASK-039-01 §3 |
`
	if err := os.WriteFile(filepath.Join(root, "docs", "dev", "contracts", "CONTRACTS-039.md"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSharedPlanningTaskPass(t *testing.T, root string, state map[string]any) {
	t.Helper()
	req039fixtures.EnsureStateRoot(state, root)
	taskPath := "docs/dev/tasks/TASK-039-01-loop-definition.md"
	data := []byte("# TASK-039-01\n\n> Status: complete\n> Version: v1.0.2\n> Primary contract: BE-039\n\n" +
		"## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n" +
		"| FE-001 | §1 |\n| BE-001 | §1 |\n| SYNC-001 | §1 |\n| BE-039 | §1 |\n\n" +
		"## 7. Closing Contract\n\n```text\nassert FE-001 §1 == satisfied\nassert BE-001 §1 == satisfied\nassert SYNC-001 §1 == satisfied\nassert BE-039 §1 == satisfied\n```\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, taskPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskPath), data, 0o644); err != nil {
		t.Fatal(err)
	}
	envelope := req039fixtures.EvidenceEnvelope(state, "ev-tasks", "planning_task", "task-planner-1", "Task Planner", "pass", map[string]any{
		"review_round": 1,
		"subject_refs": []any{map[string]any{"path": taskPath, "version": "v1.0.2", "sha256": req039fixtures.Sha256Hex(data)}},
	})
	entry := req039fixtures.WriteEvidenceEnvelope(t, root, state, "ev-tasks", "planning_task", "task-planner-1", "Task Planner", envelope, []any{taskPath})
	req039fixtures.AppendEvidence(state, entry)
}

func sharedModelDocumentsFromState(state map[string]any) []map[string]any {
	var out []map[string]any
	documents, _ := state["documents"].([]any)
	for _, raw := range documents {
		doc, _ := raw.(map[string]any)
		if doc == nil {
			continue
		}
		id, _ := doc["id"].(string)
		if strings.HasPrefix(id, "shared-model:") {
			out = append(out, doc)
		}
	}
	return out
}

func assertSharedModelDocuments(t *testing.T, state map[string]any, wantPaths ...string) {
	t.Helper()
	seen := map[string]map[string]any{}
	for _, doc := range sharedModelDocumentsFromState(state) {
		path, _ := doc["path"].(string)
		seen[path] = doc
		if doc["kind"] != "design" || doc["status"] != "locked" {
			t.Fatalf("shared model document is not a locked design subject: %#v", doc)
		}
		sha, _ := doc["sha256"].(string)
		if len(sha) != 64 {
			t.Fatalf("shared model document has invalid content hash: %#v", doc)
		}
	}
	for _, path := range wantPaths {
		if _, ok := seen[path]; !ok {
			t.Fatalf("runtime documents[] lacks shared model subject %s; got %v", path, seen)
		}
	}
}

func assertReviewSubjectsContain(t *testing.T, root string, path string) {
	t.Helper()
	for _, name := range []string{"ev-dv-spec.json", "ev-dv-task.json"} {
		data, err := os.ReadFile(filepath.Join(root, "evidence", name))
		if err != nil {
			t.Fatal(err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		found := false
		subjects, _ := envelope["subject_refs"].([]any)
		for _, raw := range subjects {
			subject, _ := raw.(map[string]any)
			if subject["path"] == path {
				found = true
				if subject["sha256"] == "" {
					t.Fatalf("%s subject %s has no hash", name, path)
				}
			}
		}
		if !found {
			t.Fatalf("%s does not sign shared model subject %s: %#v", name, path, subjects)
		}
	}
}

// TestAuditSharedModelHookSpineCommittedSource drives the actual Hook/CLI
// path across S3→S4→S5→S6. The first Hook sees a worker-only schema only on
// disk and must leave planning at S3; committing that schema permits the same
// Hook to register and advance the model batch.
func TestAuditSharedModelHookSpineCommittedSource(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "contracts", 1)
	copySharedModelSpineFixture(t, root, true)
	installSharedContractIndex(t, root, state)
	writeSystemState(t, root, state)
	writeWorkerOnlySchema(t, root)

	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse",
		req039fixtures.PreToolUseBody("shared-model-uncommitted", "Edit", map[string]any{
			"file_path": "docs/dev/contracts/CONTRACTS-039.md",
		}))
	if code != 0 && code != 2 {
		t.Fatalf("uncommitted model Hook exited unexpectedly: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	qg := req039fixtures.ParseHookQualityGate(t, stdout)
	blockedState := req039fixtures.ReadState(t, root)
	if lc, phase := req039fixtures.Lifecycle(blockedState); lc != "planning" || phase != "contracts" {
		t.Fatalf("uncommitted worker schema advanced lifecycle: %s.%s qg=%v stderr=%s", lc, phase, qg, stderr)
	}
	if len(sharedModelDocumentsFromState(blockedState)) != 0 {
		t.Fatalf("uncommitted worker schema was registered into runtime documents: %#v", blockedState["documents"])
	}
	if committed, _ := qg["transition_committed"].(bool); committed {
		t.Fatalf("uncommitted worker schema committed a transition: qg=%v", qg)
	}

	runGitIn(t, root, "add", "docs/architecture/data-model/worker-only.json")
	runGitIn(t, root, "commit", "-qm", "commit shared schema before formal planning")
	state = req039fixtures.RequireLifecycleTransition(t, runner, root, "shared-model-s3", "Edit",
		map[string]any{"file_path": "docs/dev/contracts/CONTRACTS-039.md"},
		"PTR-PLAN-02", "planning", "tasks", "AUDIT-SHARED-S3")
	assertSharedModelDocuments(t, state,
		"docs/architecture/data-model/worker-only.json",
		"docs/architecture/data-model/request.json",
		"docs/architecture/data-model/invalid-request.json",
		"docs/architecture/data-model/response.json",
	)

	state = req039fixtures.ReadState(t, root)
	writeSharedPlanningTaskPass(t, root, state)
	writeSystemState(t, root, state)
	var hookOutput string
	code, hookOutput, stderr = runHookWithRunner(t, runner, root, "PreToolUse",
		req039fixtures.PreToolUseBody("shared-model-s4", "Edit",
			map[string]any{"file_path": "docs/dev/tasks/TASK-039-01-loop-definition.md"}))
	if code != 0 && code != 2 {
		t.Fatalf("S4 Hook failed: code=%d stderr=%s", code, stderr)
	}
	qg = req039fixtures.ParseHookQualityGate(t, hookOutput)
	state = req039fixtures.ReadState(t, root)
	if lc, phase := req039fixtures.Lifecycle(state); lc != "document_verification" || phase != "" || req039fixtures.LastTransitionID(state) != "TR-002" {
		t.Fatalf("S4 Hook did not commit TR-002: lifecycle=%s.%s last=%s qg=%v output=%s stderr=%s", lc, phase, req039fixtures.LastTransitionID(state), qg, hookOutput, stderr)
	}
	assertSharedModelDocuments(t, state, "docs/architecture/data-model/worker-only.json")

	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteDocumentVerificationPassEvidence(t, root, state, "shared-dv-spec", "shared-dv-task")
	writeSystemState(t, root, state)
	for _, modelPath := range []string{
		"docs/architecture/data-model/worker-only.json",
		"docs/architecture/data-model/request.json",
		"docs/architecture/data-model/invalid-request.json",
		"docs/architecture/data-model/response.json",
	} {
		assertReviewSubjectsContain(t, root, modelPath)
	}
	state = req039fixtures.RequireLifecycleTransition(t, runner, root, "shared-model-s5", "Edit",
		map[string]any{"file_path": "docs/dev/contracts/CONTRACTS-039.md"},
		"TR-003", "building", "", "AUDIT-SHARED-S5")
	assertSharedModelDocuments(t, state,
		"docs/architecture/data-model/worker-only.json",
		"docs/architecture/data-model/request.json",
		"docs/architecture/data-model/invalid-request.json",
		"docs/architecture/data-model/response.json",
	)

	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("Hook spine invoked manual transition CLI %d times", runner.ManualTransitionCalls)
	}
}
