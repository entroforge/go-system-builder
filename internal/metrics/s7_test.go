package metrics

import (
	"reflect"
	"testing"
)

func TestS7SubmitPhaseBatchPreservesSequentialSamples(t *testing.T) {
	sequential, batch := t.TempDir(), t.TempDir()
	samples := map[string]int64{"result_consumption": 25, "findings": 3, "clean": -4, "": 2}
	for phase, duration := range samples {
		if err := RecordS7SubmitPhase(sequential, phase, duration); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecordS7SubmitPhases(batch, samples); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7SubmitPhase(batch, "findings", 7); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7SubmitPhase(sequential, "findings", 7); err != nil {
		t.Fatal(err)
	}
	want, err := NewStore(sequential).Read()
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(batch).Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want.S7SubmitPhases, got.S7SubmitPhases) || len(got.S7ResultSubmits) != 0 {
		t.Fatalf("batch changed samples or business counters: want=%v got=%v submits=%v", want.S7SubmitPhases, got.S7SubmitPhases, got.S7ResultSubmits)
	}
}

func TestS7ResultSubmitOutcomesRoundTrip(t *testing.T) {
	root := t.TempDir()
	for _, outcome := range []string{"accepted", "rejected", "accepted"} {
		if err := RecordS7ResultSubmit(root, outcome); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.S7ResultSubmits["accepted"] != 2 || snap.S7ResultSubmits["rejected"] != 1 {
		t.Fatalf("submit outcomes wrong: %+v", snap.S7ResultSubmits)
	}
}
