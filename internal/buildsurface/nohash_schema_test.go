package buildsurface

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestOriginalMainProjectionDoesNotRequireDigestsOrGitObjects(t *testing.T) {
	data, err := schema.ReadAsset("review-plan.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	plan["product_baseline"] = map[string]any{
		"contract": Contract, "runtime_id": "runtime-1", "baseline_generation": 1,
		"project_root": "/project", "development_branch": "develop",
		"tasks":         []any{map[string]any{"id": "TASK-1", "path": "docs/TASK-1.md", "version": "1"}},
		"contributions": []any{map[string]any{"assignment_record": map[string]any{"runtime_id": "runtime-1", "baseline_generation": 1, "assignment_id": "a1", "assignment_revision": 1}, "result_ref": "results/a.json", "checkpoint_ref": "checkpoints/a.json"}},
		"paths":         []any{map[string]any{"path": "src/example.go"}},
	}
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", data); err != nil {
		t.Fatal(err)
	}
	subjects, err := FrozenSubjects(&Snapshot{Tasks: []Task{{Path: "docs/TASK-1.md"}}, Paths: []Path{{Path: "src/example.go"}}})
	if err != nil || len(subjects) != 2 {
		t.Fatalf("original paths must remain usable: %#v %v", subjects, err)
	}
	want := map[string]string{"docs/TASK-1.md": "task", "src/example.go": "changed_surface"}
	for _, subject := range subjects {
		if want[subject.Path] != subject.Kind {
			t.Fatalf("original subject path or kind changed: %#v", subject)
		}
		delete(want, subject.Path)
	}
	if len(want) != 0 {
		t.Fatalf("original subjects missing: %#v", want)
	}
}
