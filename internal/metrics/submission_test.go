package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSubmissionSaveBoundedVocabularyAndConsumers(t *testing.T) {
	root := t.TempDir()
	for _, outcome := range []string{"interrupted", "read_error", "incomplete", "complete_rejected", "received_pass", "received_fix_required", "received_req_change_required"} {
		if err := ObserveSubmissionSave(root, outcome, 7); err != nil {
			t.Fatal(err)
		}
	}
	if err := ObserveSubmissionSave(root, "owner-secret/path", 7); err == nil {
		t.Fatal("unbounded label accepted")
	}
	if err := ObserveSubmissionSave(root, "incomplete", -1); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.SubmissionSaves) != 7 || s.SubmissionSaves["incomplete"] != 2 || s.SubmissionSaveDuration["incomplete"] != (DurationStats{Count: 2, SumMS: 7}) {
		t.Fatalf("incorrect invocation totals: %+v", s)
	}
	for _, format := range []func(string) (string, error){FormatDoctor, FormatHealth} {
		text, err := format(root)
		if err != nil || !strings.Contains(text, "first-submit rate unavailable") || !strings.Contains(text, `outcome="received_req_change_required"`) || !strings.Contains(text, metricSubmissionSaveDuration) {
			t.Fatalf("missing consumer: %s / %v", text, err)
		}
	}
	if bad, err := HealthDegraded(root); err != nil || bad {
		t.Fatalf("save outcomes changed health authorization: %v %v", bad, err)
	}
	var malformed Snapshot = emptySnapshot()
	if err := applyObservation(&malformed, observation{Kind: "submission_save", Label: "unexpected"}, 1); err == nil {
		t.Fatal("unknown spool label accepted")
	}
}

func TestSubmissionSaveConcurrentCompactionAndInterruptedDeletion(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 80; j++ {
				if err := ObserveSubmissionSave(root, "received_pass", 3); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	if err := CompactObservations(root); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	for i := 0; i < 3; i++ {
		if err := CompactObservations(root); err != nil {
			t.Fatal(err)
		}
		s, err := NewStore(root).Read()
		if err != nil || s.SubmissionSaves["received_pass"] != 320 || s.SubmissionSaveDuration["received_pass"] != (DurationStats{320, 960}) {
			t.Fatalf("compaction lost/doubled counter or duration: %+v %v", s, err)
		}
	}
	// Exercise a summary committed before its consumed source was deleted.
	other := t.TempDir()
	if err := ObserveSubmissionSave(other, "received_fix_required", 11); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(other, ".claude/hook-metrics")
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("missing original observation: %v %v", files, err)
	}
	key, _ := json.Marshal([]string{"submission_save", "received_fix_required"})
	summary := observationSummary{Version: 1, Totals: map[string]countedObservation{string(key): {Observation: observation{Kind: "submission_save", Label: "received_fix_required", Duration: 11}, Count: 1}}, Consumed: map[string]bool{files[0].Name(): true}}
	if err := writeObservationSummary(dir, summary); err != nil {
		t.Fatal(err)
	}
	if err := ObserveSubmissionSave(other, "received_fix_required", 13); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		s, err := NewStore(other).Read()
		if err != nil || s.SubmissionSaves["received_fix_required"] != 2 || s.SubmissionSaveDuration["received_fix_required"] != (DurationStats{2, 24}) {
			t.Fatalf("interrupted deletion changed totals: %+v %v", s, err)
		}
	}
}

func TestSubmissionFamilyBudgetIncludesOmittedFields(t *testing.T) {
	typ := reflect.TypeOf(Snapshot{})
	count := 0
	for i := 0; i < typ.NumField(); i++ {
		if strings.HasPrefix(typ.Field(i).Tag.Get("json"), "loop_") {
			count++
		}
	}
	if count != maxTrackedFamilies {
		t.Fatalf("declared families %d, documented budget %d", count, maxTrackedFamilies)
	}
}
