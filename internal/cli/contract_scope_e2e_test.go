package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/scenario"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS3OriginalHookCannotBorrowForeignBatchAndS4KeepsExactDelivery(t *testing.T) {
	root, design := newDesignFixture(t, "none", func(root string) {
		writeContractSource(t, root, "docs/requirements/REQ-039-loop-control-plane.md", "# REQ-039\n> Status: locked\n> Version: v2.0.0\n> UI impact: none\n\n## §C 当前需求\n| ID | 描述 | 指向 |\n|---|---|---|\n| FR-001 | Service filing decision | S-001 |\n| AC-001 | Service preserves allow/reject outcomes | FR-001 |\n")
		// Existing complete module fixture, adapted to a service before binding.
		source := writeScenarioFixture(t, "contract-service")
		dir := "docs/design/prototypes/contract-service"
		data, err := os.ReadFile(filepath.Join(source, dir, "scenario-model.json"))
		if err != nil {
			t.Fatal(err)
		}
		var model map[string]any
		if err := json.Unmarshal(data, &model); err != nil {
			t.Fatal(err)
		}
		for _, raw := range model["rules"].([]any) {
			rule := raw.(map[string]any)
			rule["source_refs"] = []string{"REQ-039/FR-001"}
			for _, raw := range rule["branches"].([]any) {
				branch := raw.(map[string]any)
				branch["browser_required"] = false
				branch["flow_refs"] = []string{"F-001"}
			}
		}
		writeScenarioJSONFile(t, filepath.Join(source, dir, "scenario-model.json"), model)
		matrixData, err := os.ReadFile(filepath.Join(source, dir, "cross-matrix.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, dir, "cross-matrix.json"), []byte(strings.ReplaceAll(string(matrixData), "REQ-001", "REQ-039/FR-001")), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := scenario.GenerateModule(source, "contract-service"); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(filepath.Join(source, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(source, dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			content := string(data)
			if entry.Name() == "stories.md" {
				content = strings.ReplaceAll(content, "REQ-001", "REQ-039")
			}
			writeContractSource(t, root, dir+"/"+entry.Name(), content)
		}
	})
	root, p := contractAnswerFromDesignFixture(t, root, design, func(root string) {
		writeContractSource(t, root, "docs/requirements/REQ-040.md", "# REQ-040\n| FR-040 | Historical service | S-001 |\n")
		writeContractSource(t, root, "docs/dev/contracts/CONTRACTS-040.md", "# History\n> 需求: REQ-040\n> Status: locked\n> Version: v1.0.0\n> Shared model policy: none\n> Shared model reason: No shared model is defined by this fixture.\n\n## 合同清单\n| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |\n|---|---|---|---|---|---|\n| BE-040 | 后端 | Main | locked | v1.0.0 | N/A |\n")
		writeContractSource(t, root, "docs/dev/contracts/BE-040.md", "# Foreign contract\n> Status: locked\n> Version: v1.0.0\n\nCASE-ALLOW CASE-REJECT FR-040\n")
		indexPath := "docs/dev/contracts/CONTRACTS-039.md"
		data, err := os.ReadFile(filepath.Join(root, indexPath))
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, indexPath, string(data)+"| REQ-039/FR-001 | — | BE-039 §1 | — |\n")
		for _, rel := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
			data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
			if err != nil {
				t.Fatal(err)
			}
			writeContractSource(t, root, rel, string(data))
		}
	})
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	if _, diagnostic, code := runCLI(t, root, "contracts", "check", "--root", root); code != 1 || !strings.Contains(diagnostic, "reverse closure: CASE-") {
		t.Fatalf("public diagnostic borrowed foreign coverage: %d %s", code, diagnostic)
	}
	callHook := func(path string) string {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "native-main-contract-scope-test", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, path)}})
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic)
		if code != 0 {
			t.Fatalf("Main Hook failed: %d %s %s", code, out.String(), diagnostic.String())
		}
		return out.String() + diagnostic.String()
	}
	before := stageSnapshot(t, root)
	out := callHook(p.AnswerPath)
	after := stageSnapshot(t, root)
	if after.Revision != before.Revision || !strings.Contains(out, "reverse closure: CASE-") {
		t.Fatalf("foreign batch filled missing current coverage: %s", out)
	}
	if _, found, err := runtime.FindSubmission(after.State, p.ID); err != nil || found {
		t.Fatalf("bad current batch was received: %v %v", found, err)
	}
	// Correct the actual contract, keeping the same original questionnaire.
	path := "docs/dev/contracts/BE-039.md"
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, path, string(data)+"\nCurrent CASE-ALLOW and CASE-REJECT are required by FR-001.\n")
	fixtures.CommitCurrentFixture(t, root)
	if output, diagnostic, code := runCLI(t, root, "contracts", "check", "--root", root, "--json"); code != 0 || !strings.Contains(output, `"contracts":2`) {
		t.Fatalf("public diagnostic lost current batch: %d %s %s", code, output, diagnostic)
	}
	out = callHook(path)
	after = stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["phase"] != "contracts" || strings.Contains(out, "SUBMISSION RECEIPT") {
		t.Fatalf("material save submitted an unchanged, unreceived answer: %s", out)
	}
	if _, found, err := runtime.FindSubmission(after.State, p.ID); err != nil || found {
		t.Fatalf("material save received the old answer: %t %v", found, err)
	}
	// The rejected report remains editable at its original path. Explicitly
	// saving that answer after the professional fix is the submission boundary.
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	out = callHook(p.AnswerPath)
	after = stageSnapshot(t, root)
	if life := after.State["lifecycle"].(map[string]any); life["phase"] != "tasks" {
		t.Fatalf("corrected original source did not continue S3: %v %s", life, out)
	}
	var next workpacket.TaskPlanningPacket
	for _, raw := range after.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] == "s4-task-planning-v1" {
			data, err := os.ReadFile(filepath.Join(root, row["view_path"].(string)))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &next); err != nil {
				t.Fatal(err)
			}
		}
	}
	if next.ID == "" {
		t.Fatalf("original S4 handoff was not prepared: %s", out)
	}
	for _, ref := range next.Inputs.Contracts {
		if strings.Contains(ref.Path, "040") {
			t.Fatalf("foreign contract entered S4's accepted batch: %+v", ref)
		}
	}
	if len(next.Inputs.Contracts) != 2 {
		t.Fatalf("S4 lost the exact original delivery: %+v", next.Inputs.Contracts)
	}
}
