package workpacket

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTaskAnalysisReadsOriginalBehaviorSourcesWithoutHistoricalSHA(t *testing.T) {
	for _, tc := range []struct {
		name    string
		old     any
		include bool
	}{
		{"missing", nil, false}, {"null", nil, true}, {"old string", "not-a-digest", true},
		{"number", 73, true}, {"object", map[string]any{"old": true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			p, err := BuildS5(testBinding(), []Reference{{ID: "TASK-001", Path: "docs/dev/tasks/TASK-001.md", Version: "v1"}})
			if err != nil {
				t.Fatal(err)
			}
			source := map[string]any{"path": "docs/design/prototypes/example/model.json"}
			input := map[string]any{"id": "TASK-001", "path": "docs/dev/tasks/TASK-001.md", "version": "v1"}
			reading := map[string]any{"source": "contract", "path": "docs/dev/contracts/BE-001.md", "mode": "required", "condition_unknown": false, "status": "readable", "bytes": 8}
			convention := map[string]any{"path": "docs/rules/convention.md", "source_mode": "disk", "status": "readable"}
			if tc.include {
				source["sha256"], input["sha256"] = tc.old, tc.old
				reading["sha256"], convention["sha256"] = tc.old, tc.old
			}
			example := map[string]any{
				"case": map[string]any{"id": "CASE-1", "rule_id": "R-1", "branch_id": "B-1", "title": "Case", "polarity": "positive", "required": true,
					"witness": map[string]string{"fact": "value"}, "oracle": map[string]any{"visible": []string{"seen"}, "terminal_state": "done", "persisted_effects": []string{}, "forbidden_side_effects": []string{"unexpected"}},
					"fixture_id": "F-1", "story_refs": []string{"story"}, "flow_refs": []string{"flow"}, "browser_required": false},
				"inputs":  map[string]any{"business": map[string]any{"sha256": "professional-value"}},
				"fixture": map[string]any{"id": "F-1", "persona": "reader", "synthetic": true, "setup": []string{"setup"}, "cleanup": []string{"cleanup"}},
				"sources": []any{source},
			}
			artifact := map[string]any{
				"schema_version": "1.0.0", "parser_version": "1.0.0", "inputs": []any{input},
				"task_contexts":      []any{map[string]any{"task_id": "TASK-001", "task_ref": "docs/dev/tasks/TASK-001.md", "reading": []any{reading}, "bytes_by_mode": map[string]int{"required": 8, "conditional": 0, "optional": 0, "legacy_unspecified": 0}, "total_unique_bytes": 8, "writes": []string{}, "modules": []string{}, "dependencies": []string{}, "clauses": []string{}, "check_entrances": []any{}, "unresolved_assertions": []string{}}},
				"granularity_advice": []any{}, "conventions_resolver_version": "1.0.0", "task_conventions": []any{map[string]any{"resolver_version": "1.0.0", "task_id": "TASK-001", "task_ref": "docs/dev/tasks/TASK-001.md", "registry_status": "present", "sources": []any{convention}, "entries": []any{}, "questions": []string{}}},
				"behavior_projection_version": "1.0.0", "behavior_examples": []any{map[string]any{"task_id": "TASK-001", "task_ref": "docs/dev/tasks/TASK-001.md", "case_ref": "CASE-1", "source": "case", "status": "source_example", "condition_unknown": false, "example": example}},
			}
			data, err := json.Marshal(artifact)
			if err != nil {
				t.Fatal(err)
			}
			path := ".claude/work-packets/analysis/original.json"
			abs := filepath.Join(root, path)
			if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, data, 0600); err != nil {
				t.Fatal(err)
			}
			p, err = BindTaskAnalysis(p, &TaskAnalysisRef{Path: path, ParserVersion: "1.0.0"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadTaskAnalysis(root, p)
			if err != nil {
				t.Fatalf("original task analysis: %v", err)
			}
			inputs, err := TaskAnalysisInputs(root, p)
			if err != nil || inputs["docs/design/prototypes/example/model.json"] != "" {
				t.Fatalf("original source path: %v %v", inputs, err)
			}
			if _, ok := inputs["docs/rules/convention.md"]; !ok {
				t.Fatal("original convention source path missing")
			}
			if _, ok := inputs["docs/dev/contracts/BE-001.md"]; !ok {
				t.Fatal("original task reading path missing")
			}
			var projections []struct {
				Example struct {
					Inputs map[string]any `json:"inputs"`
				} `json:"example"`
			}
			if err := json.Unmarshal(got.BehaviorExamples, &projections); err != nil {
				t.Fatalf("review-check projection: %v", err)
			}
			if projections[0].Example.Inputs["business"].(map[string]any)["sha256"] != "professional-value" {
				t.Fatal("professional input was altered")
			}
			after, err := os.ReadFile(abs)
			if err != nil || !reflect.DeepEqual(after, data) {
				t.Fatalf("original artifact changed: %v", err)
			}
		})
	}
}
