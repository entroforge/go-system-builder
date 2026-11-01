package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestS7DraftBaselineFollowsActualExclusiveCreator(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude", "answer", "review-plan.json")
	inputs := [][]byte{[]byte(`{"claims":[{"target":"first"}]}`), []byte(`{"claims":[{"target":"second"}]}`)}
	var wg sync.WaitGroup
	errs := make([]error, len(inputs))
	for i := range inputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = prepareInitialReviewPlanQuestionnaire(context.Background(), root, path, "# Guide\n", inputs[i])
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	baseline, found := readS7DraftBaseline(reviewPlanGuidePath(path))
	if !found || !bytes.Equal(bytes.TrimSpace(actual), bytes.TrimSpace(baseline)) {
		t.Fatalf("published baseline does not match creator's original: %q vs %q", actual, baseline)
	}
}

func TestS7ExistingOriginalNeverReceivesInventedBaseline(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude", "answer", "review-plan.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	authored := []byte(`{"claims":[{"target":"already authored"}]}`)
	if err := os.WriteFile(path, authored, 0600); err != nil {
		t.Fatal(err)
	}
	created, err := prepareInitialReviewPlanQuestionnaire(context.Background(), root, path, "# Guide\n", []byte(`{"claims":[{"target":"generated later"}]}`))
	if err != nil || created {
		t.Fatalf("existing original was replaced: created=%v err=%v", created, err)
	}
	if actual, _ := os.ReadFile(path); !bytes.Equal(actual, authored) {
		t.Fatal("existing authored original changed")
	}
	if _, found := readS7DraftBaseline(reviewPlanGuidePath(path)); found {
		t.Fatal("a new baseline was invented for an existing original")
	}
}
