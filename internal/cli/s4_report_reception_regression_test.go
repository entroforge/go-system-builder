package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS4CanceledReceptionPreservesLongTimestampPlaceholderAndRetries(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	draft := completedTaskPlanningDraft(p)
	stageJSON(t, root, p.AnswerPath, draft)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root)
	artifacts, err := runtime.TaskPlanningArtifacts(root, before.State, p, draft.Answer, s.Files)
	if err != nil {
		t.Fatal(err)
	}
	report, err := workpacket.TaskPlanningEvidence(p, "ev-submission-"+p.ID, artifacts, draft.Answer, time.Unix(0, 123456789).UTC())
	if err != nil {
		t.Fatal(err)
	}
	report["created_at"] = "1970-01-01T00:00:00.123456789000000000000000000000000000Z"
	// A deterministic valid old timestamp is longer than the whole-second
	// candidate below. The unknown report is never an accepted receipt.
	old, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	report["created_at"] = "1970-01-01T00:00:00Z"
	short, err := json.MarshalIndent(report, "", "  ")
	if err != nil || len(old) <= len(short) {
		t.Fatalf("timestamp fixture lengths: old=%d short=%d err=%v", len(old), len(short), err)
	}
	preferred := filepath.Join(root, ".claude/evidence/submissions", p.ID, "answer.json")
	if err := os.MkdirAll(filepath.Dir(preferred), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preferred, old, 0600); err != nil {
		t.Fatal(err)
	}
	state, journal := mustStageFile(t, s.StatePath), mustStageFile(t, s.JournalPath)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	validator := s.Validator
	s.Validator = cancelValidatedReception{CandidateValidator: validator, packetID: p.ID, cancel: cancel}
	receipt, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	if !errors.Is(err, context.Canceled) || receipt.Accepted {
		t.Fatalf("canceled reception: %+v %v", receipt, err)
	}
	if !bytes.Equal(state, mustStageFile(t, s.StatePath)) || !bytes.Equal(journal, mustStageFile(t, s.JournalPath)) {
		t.Fatal("cancellation changed the Runtime pair")
	}
	if _, err := os.Stat(s.StatePath + ".commit-pending.json"); !os.IsNotExist(err) {
		t.Fatalf("cancellation left pending commit: %v", err)
	}
	s.Validator = validator
	if receipt, err = s.SaveTaskPlanning(t.Context(), p.AnswerPath); err != nil || !receipt.Accepted {
		t.Fatalf("retry original answer: %+v %v", receipt, err)
	}
	if !bytes.Equal(old, mustStageFile(t, preferred)) {
		t.Fatal("unknown old report was changed")
	}
	snap := stageSnapshot(t, root)
	rows, _ := snap.State["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == "ev-submission-"+p.ID {
			if row["path"] == filepath.ToSlash(strings.TrimPrefix(preferred, root+string(filepath.Separator))) {
				t.Fatal("retry claimed the unregistered report")
			}
			t.Logf("old bytes=%d short candidate bytes=%d registered winner=%s", len(old), len(short), row["path"])
			return
		}
	}
	t.Fatal("retry has no registered report")
}

func TestS4ReplayReadsLargeRegisteredReportAndDistinguishesCorruption(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, p.ID)
	r, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	cancel()
	if err != nil || !r.Accepted {
		t.Fatalf("receive S4: %+v %v", r, err)
	}
	snap := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil || !found {
		t.Fatalf("accepted submission: %v", err)
	}
	var path string
	rows, _ := snap.State["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == record.EvidenceID {
			path, _ = row["path"].(string)
		}
	}
	if path == "" {
		t.Fatal("registered report missing")
	}
	var report map[string]any
	if err := json.Unmarshal(mustStageFile(t, filepath.Join(root, path)), &report); err != nil {
		t.Fatal(err)
	}
	// This extension tests compatible reading of a historical report; it is
	// deliberately not presented as an output of the current generator.
	report["historical_extension"] = strings.Repeat("x", 5<<20)
	stageJSON(t, root, path, report)
	if recovered, err := s.Recover(t.Context(), "Main", 2); err != nil || len(recovered) != 1 || !recovered[0].Accepted {
		t.Fatalf("large report original followup: %+v %v", recovered, err)
	}
	snap = stageSnapshot(t, root)
	record, found, err = runtime.FindSubmission(snap.State, p.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		t.Fatalf("large report did not advance original TR-002: %+v %v", record, err)
	}
	if r, err := s.SaveTaskPlanning(t.Context(), p.AnswerPath); err != nil || !r.Accepted {
		t.Fatalf("large registered report replay: %+v %v", r, err)
	}
	guidance := submission.AcceptedPlanningDeliveryGuidance(root, snap.State, record, "S4")
	if !strings.Contains(guidance, "Recorded accepted delivery:") || !strings.Contains(guidance, "TASK-039-01.md") {
		t.Fatalf("large report delivery guidance: %s", guidance)
	}
	changed := completedTaskPlanningDraft(p)
	changed.Answer.TaskPaths = []string{"docs/dev/tasks/TASK-039-02.md"}
	stageJSON(t, root, p.AnswerPath, changed)
	if r, err := s.SaveTaskPlanning(t.Context(), p.AnswerPath); !errors.Is(err, runtime.ErrSubmissionConflict) || !r.Accepted || r.AttemptStatus != "rejected" {
		t.Fatalf("valid report different answer: %+v %v", r, err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte("{broken report"), 0600); err != nil {
		t.Fatal(err)
	}
	if r, err := s.SaveTaskPlanning(t.Context(), p.AnswerPath); err == nil || errors.Is(err, runtime.ErrSubmissionConflict) || !r.Accepted || r.AttemptStatus != "rejected" {
		t.Fatalf("broken winner mislabeled as answer conflict: %+v %v", r, err)
	}
	if stageSnapshot(t, root).Revision != snap.Revision {
		t.Fatal("replay or rejected attempts changed the Runtime")
	}
}
