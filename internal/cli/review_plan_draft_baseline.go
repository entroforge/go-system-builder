package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
)

const s7DraftBaselineStart = "\n<!-- gsb-s7-original-plan-v1\n"
const s7DraftBaselineEnd = "\n-->\n"

// The normal Runtime process lock serializes competing framework preparations
// without introducing another lock artifact. O_EXCL remains authoritative
// against authors or other processes that do not participate in that lock.
func prepareInitialReviewPlanQuestionnaire(ctx context.Context, root, path, guide string, draft []byte) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	lockPath := filepath.Join(root, ".claude", "loop-state.json.lock.process")
	release, err := filelock.Acquire(ctx, lockPath)
	if err != nil {
		return false, err
	}
	defer release()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, writeErr := file.Write(draft)
	closeErr := file.Close()
	if writeErr != nil {
		return true, writeErr
	}
	if closeErr != nil {
		return true, closeErr
	}
	baseline, err := review.PlanObservationSnapshot(draft)
	if err != nil {
		return true, fmt.Errorf("prepare original S7 observation baseline: %w", err)
	}
	// JSON encoding escapes HTML delimiter characters, so user content cannot
	// terminate this existing guide's machine section. Existing guides are
	// never replaced, including guides authored concurrently by someone else.
	contents := guide + s7DraftBaselineStart + string(baseline) + s7DraftBaselineEnd
	if err := prepareQuestionnaireText(reviewPlanGuidePath(path), contents); err != nil {
		return true, err
	}
	return true, nil
}

func readS7DraftBaseline(guide string) ([]byte, bool) {
	data, err := os.ReadFile(guide)
	if err != nil {
		return nil, false
	}
	start := bytes.LastIndex(data, []byte(s7DraftBaselineStart))
	if start < 0 {
		return nil, false
	}
	data = data[start+len(s7DraftBaselineStart):]
	end := bytes.Index(data, []byte(s7DraftBaselineEnd))
	if end < 0 {
		return nil, false
	}
	baseline, err := review.PlanObservationSnapshot(data[:end])
	if err != nil {
		return nil, false
	}
	return baseline, true
}

// If preparation was interrupted after the authoring file appeared, recreate
// only ordinary guidance, never an unprovable original-content baseline.
func prepareS7GuideIfMissing(ctx context.Context, root, path, guide string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath := filepath.Join(root, ".claude", "loop-state.json.lock.process")
	release, err := filelock.Acquire(ctx, lockPath)
	if err != nil {
		return err
	}
	defer release()
	return prepareQuestionnaireText(reviewPlanGuidePath(path), guide)
}

func explicitS7OriginalWrite(request policy.Input, expected string) bool {
	switch request.ToolName {
	case "Write", "Edit", "MultiEdit":
	default:
		return false
	}
	actual, ok := savedQuestionnairePath(request)
	return ok && filepath.Clean(actual) == filepath.Clean(expected)
}

// A missing or damaged baseline is unknown and must not become a new gate.
func s7InitialDraftObservation(request policy.Input, path string, state map[string]any, input []byte) (unchanged, unknown bool) {
	if currentReviewPlanRegistered(state) || explicitS7OriginalWrite(request, path) {
		return false, false
	}
	baseline, found := readS7DraftBaseline(reviewPlanGuidePath(path))
	if !found {
		return false, true
	}
	same, err := review.SamePlanObservation(input, baseline)
	return err == nil && same, err != nil
}
