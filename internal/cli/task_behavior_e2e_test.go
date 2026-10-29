package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/scenario"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func installBehaviorModule(t *testing.T, root string) {
	t.Helper()
	source := writeScenarioFixture(t, "investor-workbench")
	stageCLI(t, "scenario", "generate", "--root", source, "--module", "investor-workbench")
	dir := "docs/design/prototypes/investor-workbench"
	entries, err := os.ReadDir(filepath.Join(source, dir))
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, dir), 0700)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(source, dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, e.Name()), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTaskBehaviorProjectsOnlyDeclaredCurrentCases(t *testing.T) {
	root := t.TempDir()
	installBehaviorModule(t, root)
	path := "docs/design/prototypes/investor-workbench/cases.json"
	summary := semantic.TaskContextSummary{TaskID: "TASK-001", TaskRef: "docs/dev/tasks/TASK-001.md", Reading: []semantic.TaskReadingReference{{Path: path, Source: "TASK-001#reading", Status: "readable", Clauses: "CASE-ALLOW CASE-REJECT"}}}
	rows := semantic.ProjectTaskBehavior(root, fileview.Disk{Root: root}, []semantic.TaskContextSummary{summary})
	if len(rows) != 2 || rows[0].Status != "source_example" || rows[0].Example.Inputs["fact-investor"] != "institutional" || rows[1].Example.Inputs["fact-investor"] != "individual" || rows[1].Example.Case.Oracle.Rejection == "" {
		t.Fatalf("wrong original behavior projection: %+v", rows)
	}
	if len(rows[0].Example.Sources) != 7 {
		t.Fatal("source closure omitted module truth")
	}
	summary.Reading[0].Mode, summary.Reading[0].ConditionUnknown = "conditional", true
	if row := semantic.ProjectTaskBehavior(root, fileview.Disk{Root: root}, []semantic.TaskContextSummary{summary})[0]; row.Mode != "conditional" || !row.ConditionUnknown {
		t.Fatal("conditional reading became a required example")
	}
	second := summary.Reading[0]
	second.Source, second.Mode, second.ConditionUnknown = "TASK-001#reading-required", "required", false
	summary.Reading = append(summary.Reading, second)
	if repeated := semantic.ProjectTaskBehavior(root, fileview.Disk{Root: root}, []semantic.TaskContextSummary{summary}); len(repeated) != 4 {
		t.Fatal("same CASE in different reading rows lost its source or Mode")
	}
	summary.Reading = summary.Reading[:1]
	summary.Reading[0].Clauses = "CASE-UNKNOWN"
	if row := semantic.ProjectTaskBehavior(root, fileview.Disk{Root: root}, []semantic.TaskContextSummary{summary})[0]; row.Status != "unresolved" || row.Example != nil {
		t.Fatal("unknown CASE became guessed example")
	}
	summary.Reading[0].Clauses = ""
	if row := semantic.ProjectTaskBehavior(root, fileview.Disk{Root: root}, []semantic.TaskContextSummary{summary})[0]; row.Status != "unresolved" {
		t.Fatal("module link assigned every CASE")
	}
	original, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, path), append(original, '\n'), 0600)
	summary.Reading[0].Clauses = "CASE-ALLOW"
	if row := semantic.ProjectTaskBehavior(root, fileview.Disk{Root: root}, []semantic.TaskContextSummary{summary})[0]; row.Status != "unresolved" || row.Example != nil {
		t.Fatal("tampered generated CASE became standard")
	}
	if _, err := scenario.ProjectBehaviorWithFiles(root, "../escape", fileview.Disk{Root: root}); err == nil {
		t.Fatal("invalid module accepted")
	}
}

func TestS5FrozenBehaviorSourcesInstallAndDriftReject(t *testing.T) {
	f := newStageAnswerFixtureMode(t, true, func(root string, state map[string]any) {
		installBehaviorModule(t, root)
		for _, name := range []string{"scenario-model.json", "cross-matrix.json", "stories.md"} {
			path := filepath.Join(root, "docs/design/prototypes/investor-workbench", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), "REQ-001", "REQ-039")), 0600); err != nil {
				t.Fatal(err)
			}
		}
		stageCLI(t, "scenario", "generate", "--root", root, "--module", "investor-workbench")
		for _, raw := range state["documents"].([]any) {
			doc := raw.(map[string]any)
			if doc["kind"] == "req" {
				path := doc["path"].(string)
				body, err := os.ReadFile(filepath.Join(root, path))
				if err != nil {
					t.Fatal(err)
				}
				body = append(body, []byte("\n## Functional Requirements\n| ID | Requirement |\n|---|---|\n| FR-001 | Investor access follows the declared rule |\n")...)
				if err := os.WriteFile(filepath.Join(root, path), body, 0600); err != nil {
					t.Fatal(err)
				}
				doc["sha256"] = workpacket.Digest(body)
				state["bound_req"].(map[string]any)["sha256"] = doc["sha256"]
			}
			if doc["kind"] != "task" {
				continue
			}
			path := doc["path"].(string)
			body, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			body = append(body, []byte("\n## 2. Document Manifest\n| Order | Kind | ID | Path | Clauses | Purpose | Mode |\n|---|---|---|---|---|---|---|\n| 1 | model | current-cases | [Current CASE](../../design/prototypes/investor-workbench/cases.json) | CASE-ALLOW CASE-REJECT | Owned example inputs and oracles | required |\n")...)
			os.WriteFile(filepath.Join(root, path), body, 0600)
			doc["sha256"] = workpacket.Digest(body)
		}
	})
	p := f.packets[0]
	artifact, err := workpacket.ReadTaskAnalysis(f.root, p)
	if err != nil {
		t.Fatal(err)
	}
	var behavior []semantic.TaskBehaviorProjection
	if err := json.Unmarshal(artifact.BehaviorExamples, &behavior); err != nil {
		t.Fatal(err)
	}
	if artifact.BehaviorProjectionVersion != semantic.TaskBehaviorProjectionVersion || len(behavior) != 2 || behavior[0].Example == nil {
		t.Fatalf("frozen behavior absent: %+v", behavior)
	}
	if len(behavior[0].Example.Sources) != 8 {
		t.Fatal("original bound REQ join omitted from frozen sources")
	}
	e := f.prepare(t, p)
	for _, source := range behavior[0].Example.Sources {
		data, err := os.ReadFile(filepath.Join(e.Path, ".claude/inputs", source.Path))
		if err != nil || workpacket.Digest(data) != source.SHA256 {
			t.Fatal("Worker lacks original behavior source", err)
		}
	}
	var out, errOut bytes.Buffer
	code := cli.Run([]string{"runtime", "workspace", "review-check", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
	var result semantic.TaskCheckResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("review-check: %d %v %s", code, err, errOut.String())
	}
	if !reflect.DeepEqual(result.BehaviorExamples, behavior) {
		t.Fatal("review-check refreshed frozen behavior")
	}
	f.begin(t, p, &e)
	changed := filepath.Join(f.root, "docs/design/prototypes/investor-workbench/scenario-model.json")
	data, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(changed, append(data, '\n'), 0600)
	before := stageSnapshot(t, f.root)
	out.Reset()
	errOut.Reset()
	code = cli.Run([]string{"runtime", "workspace", "review-check", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "frozen execution input changed") || !strings.Contains(errOut.String(), "scenario-model.json") {
		t.Fatalf("drifted frozen source remained consumable: %d %s", code, errOut.String())
	}
	stageJSON(t, e.Path, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	out.Reset()
	errOut.Reset()
	code = cli.Run([]string{"runtime", "workspace", "answer", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "frozen execution input changed") {
		t.Fatalf("drifted answer was accepted: %d %s", code, errOut.String())
	}
	if code, msg := stageHookCode(t, f.root, e.Path, e.AgentID, "Stop", ""); code != 2 {
		t.Fatalf("drifted frozen behavior sources allowed: %d %s", code, msg)
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("drifted source validation wrote Runtime")
	}
	if err := os.WriteFile(changed, data, 0600); err != nil {
		t.Fatal(err)
	}
	r := saveStageHook(t, f.root, e.Path, e.AgentID, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	if !r.Accepted || !r.WorkComplete {
		t.Fatal("source restoration did not preserve the original receiver", r)
	}
	output := stageCLI(t, "runtime", "stage-answer", "revise", "--root", f.root, "--previous", r.SubmissionKey, "--reason", "Recheck the original declared CASE responsibility")
	var next workpacket.Packet
	if err := json.Unmarshal([]byte(output), &next); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.TaskAnalysis, p.TaskAnalysis) {
		t.Fatal("named revision refreshed behavior examples")
	}
}
