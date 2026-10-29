package productbaseline

import (
	"fmt"
	"strings"
	"testing"
)

func TestReviewedLargeTaskSetKeepsEveryTask(t *testing.T) {
	start := validStart()
	start.TaskSet = nil
	for i := 0; i < 160; i++ {
		id := fmt.Sprintf("TASK-%03d", i+1)
		start.TaskSet = append(start.TaskSet, Task{TaskID: id, Path: "docs/dev/tasks/" + id + ".md", Version: "v1.0.0", SHA256: strings.Repeat("a", 64)})
	}
	sealed, err := Seal(start)
	if err != nil || len(sealed.TaskSet) != 160 {
		t.Fatalf("large reviewed set lost: %+v %v", sealed, err)
	}
	if err := sealed.Validate(); err != nil {
		t.Fatal(err)
	}
	sealed.TaskSet = append(sealed.TaskSet, sealed.TaskSet[0])
	if _, err := Seal(sealed); err == nil {
		t.Fatal("duplicate task accepted")
	}
}
