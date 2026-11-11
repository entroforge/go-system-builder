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
		TransitionEventID:       "evt-tr-003-r9",
		TransitionAfterRevision: 9,
		TransitionSequence:      8,
		TaskSet: []Task{
			{TaskID: "TASK-002", Path: "docs/dev/tasks/TASK-002.md", Version: "v1.0.0"},
			{TaskID: "TASK-001", Path: "docs/dev/tasks/TASK-001.md", Version: "unversioned"},
		},
	})
	if err != nil {
		panic(err)
	}
	return start
}

func TestSealSortsTaskSetWithoutPersistingDigestOrGitOID(t *testing.T) {
	start := validStart()
	if start.TaskSet[0].TaskID != "TASK-001" || start.TaskSet[1].TaskID != "TASK-002" {
		t.Fatalf("task_set was not normalized: %+v", start.TaskSet)
	}
	if err := start.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sha256") || strings.Contains(string(data), "start_commit") {
		t.Fatalf("new start contains digest or Git OID: %s", data)
	}

}

func TestReadS6MainStartsIgnoresUnrelatedFieldsAndRejectsDuplicateGeneration(t *testing.T) {
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
	encoded, _ = json.Marshal([]S6MainStart{start})
	if err := json.Unmarshal(encoded, &rows); err != nil {
		t.Fatal(err)
	}
	rows[0].(map[string]any)["unrelated"] = "preserved outside this reader"
	state["baseline"].(map[string]any)["s6_main_starts"] = rows
	if got, err := ReadS6MainStarts(state); err != nil || len(got) != 1 || got[0].TransitionEventID != start.TransitionEventID {
		t.Fatalf("unrelated field blocked the original responsibility: %+v %v", got, err)
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
	second, _ = Seal(second)
	if err := AppendS6MainStart(state, second); err != nil {
		t.Fatal(err)
	}
	starts, err := ReadS6MainStarts(state)
	if err != nil || len(starts) != 2 || starts[0].TransitionEventID != first.TransitionEventID || starts[1].TransitionEventID != second.TransitionEventID {
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
