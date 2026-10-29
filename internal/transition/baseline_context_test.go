package transition

import (
	"context"
	"errors"
	"os"
	"testing"
)

type canceledBaselineEvidenceRead struct {
	body   []byte
	cancel context.CancelFunc
}

func (r canceledBaselineEvidenceRead) ReadFile(string) ([]byte, error) {
	r.cancel()
	return r.body, nil
}

func (r canceledBaselineEvidenceRead) ReadDir(string) ([]os.DirEntry, error) {
	return nil, errors.New("unexpected directory read in canceled evidence test")
}

func TestTransitionBaselineValidationPreservesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := map[string]any{"runtime_id": "loop-source", "baseline": map[string]any{"generation": 1}, "entities": map[string]any{"assignments": []any{map[string]any{"runtime_id": "loop-source", "baseline_generation": 1}}}}
	if err := validateRequest(t.TempDir(), state, TransitionSpec{ID: "TR-006"}, Request{Context: ctx, Actor: "orchestrator"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("TR-006 baseline detached request context: %v", err)
	}
}

func TestCurrentEvidenceBaselineStopsAfterCanceledSourceRead(t *testing.T) {
	body := []byte(`{"original":"completion"}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "evidence": []any{map[string]any{"id": "result", "kind": "completion_report", "status": "valid", "baseline_generation": 1, "path": "result.json", "sha256": SHA256(body)}}}
	err := validateCurrentEvidenceWithFiles(t.TempDir(), state, "builder_report_record", "result", canceledBaselineEvidenceRead{body, cancel}, ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("source cancellation became canonical-baseline refusal: %v", err)
	}
}
