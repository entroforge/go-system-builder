package runtime_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestHumanReleaseTransitionIDMapsOnlyKnownDispositions(t *testing.T) {
	tests := []struct {
		disposition string
		want        string
	}{
		{disposition: "approve", want: "TR-025"},
		{disposition: "defer", want: "TR-026"},
		{disposition: "reject_defect", want: "TR-027"},
		{disposition: "reject_acceptance", want: "TR-028"},
		{disposition: "reject_release_audit", want: "TR-029"},
		{disposition: "abort", want: "TR-030"},
	}

	for _, tt := range tests {
		t.Run(tt.disposition, func(t *testing.T) {
			got, err := runtime.HumanReleaseTransitionID(tt.disposition)
			if err != nil {
				t.Fatalf("HumanReleaseTransitionID(%q) error = %v", tt.disposition, err)
			}
			if got != tt.want {
				t.Fatalf("HumanReleaseTransitionID(%q) = %q, want %q", tt.disposition, got, tt.want)
			}
		})
	}
	for _, disposition := range []string{"", " ", "release_authorized", "unknown"} {
		t.Run("reject_"+strings.ReplaceAll(disposition, " ", "space"), func(t *testing.T) {
			if _, err := runtime.HumanReleaseTransitionID(disposition); err == nil {
				t.Fatalf("HumanReleaseTransitionID(%q) accepted an invalid disposition", disposition)
			}
		})
	}
}

func TestRolloverRejectsAwaitingHumanReleaseWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	prepareRolloverSourcePair(t, statePath)

	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	archiveRoot := filepath.Join(dir, "archive")
	_, err := testWriter(statePath, journalPath).Rollover(
		freshInactiveState(t),
		archiveRoot,
		runtime.RolloverApproval{ApprovedBy: "release-owner", EvidenceID: "ev-approval"},
		time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC),
	)
	if err == nil || !strings.Contains(err.Error(), "terminal state") {
		t.Fatalf("Rollover error = %v, want awaiting_human_release rejection", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("rejecting S11 rollover changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("rejecting S11 rollover changed journal")
	}
	if _, statErr := os.Stat(archiveRoot); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejecting S11 rollover changed archive, stat error = %v", statErr)
	}
}
