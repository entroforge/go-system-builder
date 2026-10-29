package productbaseline

import (
	"encoding/json"
	"strings"
	"testing"
)

func validStart() S6MainStart {
	start, err := Seal(S6MainStart{
		RuntimeID:               "loop-REQ-001",
		BaselineGeneration:      3,
		ProjectRoot:             "/workspace/project",
		DevelopmentBranch:       "main",
		StartCommit:             strings.Repeat("a", 40),
		TransitionEventID:       "evt-tr-003-r9",
		TransitionAfterRevision: 9,
		TransitionSequence:      8,
		TaskSet: []Task{
			{TaskID: "TASK-002", Path: "docs/dev/tasks/TASK-002.md", Version: "v1.0.0", SHA256: strings.Repeat("b", 64)},
			{TaskID: "TASK-001", Path: "docs/dev/tasks/TASK-001.md", Version: "unversioned", SHA256: strings.Repeat("c", 64)},
		},
	})
	if err != nil {
		panic(err)
	}
	return start
}

func TestSealSortsTaskSetAndValidatesBothDigests(t *testing.T) {
	start := validStart()
	if start.TaskSet[0].TaskID != "TASK-001" || start.TaskSet[1].TaskID != "TASK-002" {
		t.Fatalf("task_set was not normalized: %+v", start.TaskSet)
	}
	if err := start.Validate(); err != nil {
		t.Fatal(err)
	}
	start.TaskSet[0].Version = "v9"
	if err := start.Validate(); err == nil || !strings.Contains(err.Error(), "task_set_sha256") {
		t.Fatalf("changed task snapshot was accepted: %v", err)
	}
}

func TestReadS6MainStartsRejectsUnknownFieldsAndDuplicateGeneration(t *testing.T) {
	start := validStart()
	encoded, err := json.Marshal([]S6MainStart{start, start})
	if err != nil {
		t.Fatal(err)
	}
	var rows []any
	if err := json.Unmarshal(encoded, &rows); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"baseline": map[string]any{"generation": 3, "s6_main_starts": rows}}
	if _, err := ReadS6MainStarts(state); err == nil || !strings.Contains(err.Error(), "unique increasing generations") {
		t.Fatalf("duplicate generation accepted: %v", err)
	}
	rows = []any{map[string]any{"unexpected": true}}
	state["baseline"].(map[string]any)["s6_main_starts"] = rows
	if _, err := ReadS6MainStarts(state); err == nil {
		t.Fatal("unknown fields accepted")
	}
}

func TestAppendS6MainStartPreservesPriorGenerationsAndRejectsCapacity(t *testing.T) {
	state := map[string]any{"baseline": map[string]any{"generation": 1}}
	first := validStart()
	first.BaselineGeneration = 1
	first.TransitionEventID = "evt-tr-003-r9"
	first, _ = Seal(first)
	if err := AppendS6MainStart(state, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.BaselineGeneration = 2
	second.TransitionEventID = "evt-tr-003-r10"
	second.TransitionAfterRevision++
	second.TransitionSequence++
	second.StartCommit = strings.Repeat("d", 40)
	second, _ = Seal(second)
	if err := AppendS6MainStart(state, second); err != nil {
		t.Fatal(err)
	}
	starts, err := ReadS6MainStarts(state)
	if err != nil || len(starts) != 2 || starts[0].StartCommit != first.StartCommit || starts[1].StartCommit != second.StartCommit {
		t.Fatalf("starts=%+v err=%v", starts, err)
	}
	baseline := state["baseline"].(map[string]any)
	tooMany := make([]S6MainStart, MaxS6MainStarts)
	for i := range tooMany {
		row := first
		row.BaselineGeneration = i + 1
		row.TransitionEventID = "evt-tr-003-capacity-" + string(rune('a'+i%26))
		row.TransitionAfterRevision = i + 1
		row.TransitionSequence = i + 1
		tooMany[i], _ = Seal(row)
	}
	tooManyBytes, _ := json.Marshal(tooMany)
	var tooManyRaw []any
	_ = json.Unmarshal(tooManyBytes, &tooManyRaw)
	baseline["s6_main_starts"] = tooManyRaw
	if err := AppendS6MainStart(state, second); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("capacity exhaustion accepted: %v", err)
	}
}
