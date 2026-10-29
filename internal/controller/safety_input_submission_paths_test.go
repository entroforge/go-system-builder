package controller

import (
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestBuildSafetyInputPreservesTrustedWorkerAuthoringPaths(t *testing.T) {
	snapshot := runtime.Snapshot{State: map[string]any{
		"runtime_id": "loop-REQ-1",
		"lifecycle":  map[string]any{"state": "document_verification", "phase": "review"},
		"baseline":   map[string]any{"generation": float64(3)},
	}}
	request := policy.RuntimeContext{
		PacketMode:            true,
		WorkerSubmissionPaths: []string{".loop-submissions/plan.json", ".loop-submissions/completion.json"},
		WorkPacketAnswerPaths: []string{".loop-submissions/stage-answer.json"},
	}
	input := buildSafetyInput(ControlRequest{Root: t.TempDir(), Runtime: request}, snapshot, nil)
	if !input.Runtime.PacketMode {
		t.Fatal("packet mode was dropped from the controller safety projection")
	}
	if !reflect.DeepEqual(input.Runtime.WorkerSubmissionPaths, request.WorkerSubmissionPaths) {
		t.Fatalf("worker submission paths were dropped: got %v want %v", input.Runtime.WorkerSubmissionPaths, request.WorkerSubmissionPaths)
	}
	if !reflect.DeepEqual(input.Runtime.WorkPacketAnswerPaths, request.WorkPacketAnswerPaths) {
		t.Fatalf("packet answer paths were dropped: got %v want %v", input.Runtime.WorkPacketAnswerPaths, request.WorkPacketAnswerPaths)
	}
}
