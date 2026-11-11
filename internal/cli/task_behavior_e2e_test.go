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
	// Blueprint L3-S5-document-verification.md:102: the Hook does not register
	// or check manifest and material digests; the professional author and
	// Main judge the actual material and its effect on the review, so the
	// system does not claim to detect a same-version external byte change.
	// What must still hold is that the projection only ever reports the
	// declared current CASE references and never invents one.
	declared := semantic.ProjectTaskBehavior(root, fileview.Disk{Root: root}, []semantic.TaskContextSummary{summary})
	if len(declared) != 1 || declared[0].CaseRef != "CASE-ALLOW" {
		t.Fatalf("projection invented CASE references: %+v", declared)
	}
	if declared[0].Status == "source_example" && declared[0].Example == nil {
		t.Fatal("standard CASE lost its example")
	}
	if _, err := scenario.ProjectBehaviorWithFiles(root, "../escape", fileview.Disk{Root: root}); err == nil {
		t.Fatal("invalid module accepted")
	}
}

func TestS5FrozenBehaviorSourcesRemainUsableWithoutDigestGate(t *testing.T) {
	f := newHistoricalStageAnswerFixture(t, func(root string, state map[string]any) {
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
		original, readErr := os.ReadFile(filepath.Join(f.root, source.Path))
		if err != nil || readErr != nil || !bytes.Equal(data, original) {
			t.Fatalf("Worker lacks actual behavior source %s: %v %v", source.Path, err, readErr)
		}
	}
	// The review-check consumer reads the original frozen analysis. Older
	// machine values must not block it or alter professional same-named data.
	analysisPath := filepath.Join(f.root, filepath.FromSlash(p.TaskAnalysis.Path))
	originalAnalysis, err := os.ReadFile(analysisPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		value   any
		present bool
	}{
		{"missing", nil, false}, {"null", nil, true}, {"string", "legacy", true},
		{"number", 7, true}, {"object", map[string]any{"old": true}, true},
	} {
		t.Run("original_analysis_"+tc.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(originalAnalysis, &document); err != nil {
				t.Fatal(err)
			}
			row := document["behavior_examples"].([]any)[0].(map[string]any)
			example := row["example"].(map[string]any)
			example["inputs"].(map[string]any)["sha256"] = map[string]any{"business": "preserved"}
			source := example["sources"].([]any)[0].(map[string]any)
			if tc.present {
				source["sha256"] = tc.value
			} else {
				delete(source, "sha256")
			}
			authored, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(analysisPath, authored, 0600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := cli.Run([]string{"runtime", "workspace", "review-check", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
			var result semantic.TaskCheckResult
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || len(result.BehaviorExamples) == 0 {
				t.Fatalf("original review-check failed: code=%d err=%v stderr=%s", code, err, errOut.String())
			}
			value, ok := result.BehaviorExamples[0].Example.Inputs["sha256"].(map[string]any)
			if !ok || value["business"] != "preserved" {
				t.Fatalf("professional same-named input changed: %#v", result.BehaviorExamples[0].Example.Inputs)
			}
			after, err := os.ReadFile(analysisPath)
			if err != nil || !bytes.Equal(after, authored) {
				t.Fatalf("original analysis changed: %v", err)
			}
		})
	}
	if err := os.WriteFile(analysisPath, originalAnalysis, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := cli.Run([]string{"runtime", "workspace", "review-check", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
	initialCode := code
	var result semantic.TaskCheckResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("review-check: %d %v %s", code, err, errOut.String())
	}
	if !reflect.DeepEqual(result.BehaviorExamples, behavior) {
		t.Fatal("review-check refreshed frozen behavior")
	}
	f.startFrozenReviewWorker(t, e)
	changed := filepath.Join(f.root, "docs/design/prototypes/investor-workbench/scenario-model.json")
	data, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(changed, append(data, '\n'), 0600)
	out.Reset()
	errOut.Reset()
	code = cli.Run([]string{"runtime", "workspace", "review-check", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
	var afterEdit semantic.TaskCheckResult
	if err := json.Unmarshal(out.Bytes(), &afterEdit); err != nil || code != initialCode || !reflect.DeepEqual(afterEdit.Problems, result.Problems) || !reflect.DeepEqual(afterEdit.BehaviorExamples, behavior) {
		t.Fatalf("format-only source edit changed the original review: %d vs %d, err=%v stderr=%s stdout=%s", code, initialCode, err, errOut.String(), out.String())
	}
	r := saveStageHook(t, f.root, e.Path, e.AgentID, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	if !r.Accepted || !r.WorkComplete {
		t.Fatal("original reviewer did not naturally receive the answer after source edit", r)
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
