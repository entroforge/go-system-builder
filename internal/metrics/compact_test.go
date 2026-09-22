package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCompactionPreservesLegacyAndConcurrentObservations(t *testing.T) {
	root := t.TempDir()
	if err := RecordCASConflict(root); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if err := ObserveIntegrationDuration(root, "ok", 3); err != nil {
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
		if err != nil {
			t.Fatal(err)
		}
		if s.CASConflicts != 1 || s.IntegrationDuration["ok"].Count != 400 || s.IntegrationDuration["ok"].SumMS != 1200 {
			t.Fatalf("bad totals: %+v", s)
		}
	}
	files, err := os.ReadDir(filepath.Join(root, ".claude/hook-metrics"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("spool not reclaimed: %v", files)
	}
}

func TestCompactionResumesAfterSummaryCommitBeforeDeletion(t *testing.T) {
	root := t.TempDir()
	if err := ObserveCASConflict(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".claude/hook-metrics")
	files, _ := os.ReadDir(dir)
	key, _ := json.Marshal([]string{"cas", ""})
	summary := observationSummary{Version: 1, Totals: map[string]countedObservation{string(key): {Observation: observation{Kind: "cas"}, Count: 1}}, Consumed: map[string]bool{files[0].Name(): true}}
	if err := writeObservationSummary(dir, summary); err != nil {
		t.Fatal(err)
	}
	if err := ObserveCASConflict(root); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := NewStore(root).Read()
		if err != nil {
			t.Fatal(err)
		}
		if s.CASConflicts != 2 {
			t.Fatalf("double count after interrupted deletion: %d", s.CASConflicts)
		}
	}
}
