package runtime_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func s4OriginalWriterFixture(t *testing.T) (string, runtime.Snapshot, workpacket.TaskPlanningPacket, runtime.EvidenceRequest) {
	t.Helper()
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	issued, err := runtime.RecordTaskPlanningPacket(root, statePath, journalPath, packet, files, semantic.RuntimeCandidateValidator{})
	if err != nil {
		t.Fatalf("original S4 packet Writer: %v", err)
	}
	indexPath, taskPath := "docs/dev/tasks/index-REQ-001.md", "docs/dev/tasks/TASK-REQ-001-01.md"
	writeTaskPlanningFile(t, root, taskPath, "# TASK-REQ-001-01: Scope\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n> Dispatch policy: waves-v1\n> Primary contract: FE-001\n\n## Delivered Clauses\n\n| Contract | Delivered clauses |\n|---|---|\n| FE-001 | §1 |\n\n## Scope\n\n| Type | Paths |\n|---|---|\n| prospective write paths | `web/ui` |\n")
	writeTaskPlanningFile(t, root, indexPath, "# Dispatch plan\n> REQ: REQ-001\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n- [ ] [TASK](TASK-REQ-001-01.md)\n")
	answer := workpacket.TaskPlanningAnswer{Conclusion: stringPtr("pass"), IndexPath: stringPtr(indexPath), TaskPaths: []string{taskPath}}

	artifacts, err := runtime.TaskPlanningArtifacts(root, issued.State, packet, answer, files)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := workpacket.TaskPlanningEvidence(packet, "E-S4-001", artifacts, answer, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	draft := packet.Draft
	draft.Answer = answer
	answerJSON, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	req := runtime.EvidenceRequest{ExpectedRevision: -1, ID: "E-S4-001", Kind: "planning_task", Path: ".claude/evidence/E-S4-001.json", ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: semantic.RuntimeCandidateValidator{}, Submission: &runtime.SubmissionRequest{PacketID: packet.ID, Owner: "Main", AnswerJSON: answerJSON, EvidenceJSON: data, Files: files, Authorize: func(string, map[string]any) error { t.Error("S4 Writer must not re-authorize sources"); return nil }}}
	return root, issued, packet, req
}

func s4WriterRead(t *testing.T, root, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestS4ExplicitEmptyEvidenceRejectsBeforePublicationAndPreservesPair(t *testing.T) {
	root, _, _, request := s4OriginalWriterFixture(t)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	// A valid unknown report would be accepted by the legacy nil entry. The explicit
	// empty bytes must reach envelope validation instead of reading this file.
	placeholder := append([]byte(nil), request.Submission.EvidenceJSON...)
	writeBytes(t, root, request.Path, placeholder)
	beforeState, beforeJournal := s4WriterRead(t, root, ".claude/loop-state.json"), s4WriterRead(t, root, ".claude/loop-events.jsonl")
	request.Submission.EvidenceJSON = []byte{}
	_, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("empty report should reach original JSON envelope validation: %v", err)
	}
	if !bytes.Equal(beforeState, s4WriterRead(t, root, ".claude/loop-state.json")) || !bytes.Equal(beforeJournal, s4WriterRead(t, root, ".claude/loop-events.jsonl")) {
		t.Fatal("empty report changed original Runtime/journal pair")
	}
	if !bytes.Equal(placeholder, s4WriterRead(t, root, request.Path)) {
		t.Fatal("empty report changed unknown placeholder")
	}
	if _, err := os.Stat(statePath + ".commit-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty report left pending: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, ".claude/evidence-alt-*")); err != nil || len(matches) != 0 {
		t.Fatalf("empty report published fallback: %v %v", matches, err)
	}
	// Confirm that the same original answer/report can still be received through
	// the historical external-report entry after the rejected empty attempt.
	request.Submission.EvidenceJSON = nil
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err != nil {
		t.Fatalf("nil external report entry: %v", err)
	}
}

func TestS4OriginalWriterConcurrentSameAndDifferentAnswers(t *testing.T) {
	for _, different := range []bool{false, true} {
		name := "same"
		if different {
			name = "different"
		}
		t.Run(name, func(t *testing.T) {
			root, issued, packet, first := s4OriginalWriterFixture(t)
			second := first
			frozen := *first.Submission
			second.Submission = &frozen
			second.Path = ".claude/evidence/other-candidate.json"
			var envelope map[string]any
			if err := json.Unmarshal(first.Submission.EvidenceJSON, &envelope); err != nil {
				t.Fatal(err)
			}
			envelope["created_at"] = "2026-11-10T01:02:03.123456789Z"
			if different {
				var draft workpacket.TaskPlanningDraft
				if err := json.Unmarshal(second.Submission.AnswerJSON, &draft); err != nil {
					t.Fatal(err)
				}
				alternative := "docs/dev/tasks/TASK-REQ-001-02.md"
				writeBytes(t, root, alternative, []byte("> Status: cancelled\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n"))
				draft.Answer.TaskPaths = append(append([]string(nil), draft.Answer.TaskPaths...), alternative)
				artifacts, err := runtime.TaskPlanningArtifacts(root, issued.State, packet, draft.Answer, second.Submission.Files)
				if err != nil {
					t.Fatal(err)
				}
				report, err := workpacket.TaskPlanningEvidence(packet, first.ID, artifacts, draft.Answer, time.Date(2026, 11, 10, 1, 2, 3, 123456789, time.UTC))
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(encoded, &envelope); err != nil {
					t.Fatal(err)
				}
				second.Submission.AnswerJSON, err = json.Marshal(draft)
				if err != nil {
					t.Fatal(err)
				}
			}
			var err error
			second.Submission.EvidenceJSON, err = json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
			start := make(chan struct{})
			results := make(chan error, 2)
			var group sync.WaitGroup
			for _, req := range []runtime.EvidenceRequest{first, second} {
				group.Add(1)
				go func(req runtime.EvidenceRequest) {
					defer group.Done()
					<-start
					_, err := runtime.RecordEvidence(root, statePath, journalPath, req)
					results <- err
				}(req)
			}
			close(start)
			group.Wait()
			close(results)
			conflicts := 0
			for err := range results {
				if errors.Is(err, runtime.ErrSubmissionConflict) {
					conflicts++
				} else if err != nil {
					t.Fatalf("original Writer reception: %v", err)
				}
			}
			wantConflicts := 0
			if different {
				wantConflicts = 1
			}
			if conflicts != wantConflicts {
				t.Fatalf("conflicts=%d want=%d", conflicts, wantConflicts)
			}
			latest, err := runtime.NewStore(statePath, journalPath).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			receipts := 0
			for _, raw := range latest.State["entities"].(map[string]any)["submissions"].([]any) {
				if raw.(map[string]any)["packet_id"] == packet.ID {
					receipts++
				}
			}
			evidenceCount := 0
			actualPath := ""
			for _, raw := range latest.State["evidence"].([]any) {
				row := raw.(map[string]any)
				if row["id"] == first.ID {
					evidenceCount++
					actualPath, _ = row["path"].(string)
				}
			}
			if receipts != 1 || evidenceCount != 1 || latest.Revision != issued.Revision+1 {
				t.Fatalf("reception duplicated: receipts=%d evidence=%d revision=%d", receipts, evidenceCount, latest.Revision)
			}
			record, found, err := runtime.FindSubmission(latest.State, packet.ID)
			if err != nil || !found || record.FollowupID != "followup:"+packet.ID || record.ControlStatus != "pending" {
				t.Fatalf("original followup: %+v found=%v err=%v", record, found, err)
			}
			journal := s4WriterRead(t, root, ".claude/loop-events.jsonl")
			if bytes.Count(journal, []byte(`"event_id":"evt-evidence-E-S4-001"`)) != 1 {
				t.Fatalf("reception journal count: %s", journal)
			}
			if _, err := runtime.ReadTaskPlanningReport(root, actualPath); err != nil {
				t.Fatalf("registered winner: %v", err)
			}
			for _, candidate := range []string{first.Path, second.Path} {
				if candidate != actualPath {
					if _, err := os.Stat(filepath.Join(root, candidate)); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("loser published %q: %v", candidate, err)
					}
				}
			}
		})
	}
}

func TestS4WriterPendingRecoveryKeepsPublishedWinner(t *testing.T) {
	root, issued, packet, request := s4OriginalWriterFixture(t)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	beforeJournal := s4WriterRead(t, root, ".claude/loop-events.jsonl")
	if err := os.Chmod(journalPath, 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(journalPath, 0644) })
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err == nil {
		t.Fatal("expected original journal append failure")
	}
	if _, err := os.Stat(statePath + ".commit-pending.json"); err != nil {
		t.Fatalf("original pending marker missing: %v", err)
	}
	if !bytes.Equal(beforeJournal, s4WriterRead(t, root, ".claude/loop-events.jsonl")) {
		t.Fatal("failed append changed journal")
	}
	// The original Writer already installed the candidate state before append.
	// Retain its actual evidence path, not a guessed preferred path.
	var committed map[string]any
	if err := json.Unmarshal(s4WriterRead(t, root, ".claude/loop-state.json"), &committed); err != nil {
		t.Fatal(err)
	}
	record, found, err := runtime.FindSubmission(committed, packet.ID)
	if err != nil || !found {
		t.Fatalf("pending receipt: %+v %v %v", record, found, err)
	}
	winnerPath := ""
	for _, raw := range committed["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == request.ID {
			winnerPath, _ = row["path"].(string)
		}
	}
	report, err := runtime.ReadTaskPlanningReport(root, winnerPath)
	if err != nil || !bytes.Equal(report, request.Submission.EvidenceJSON) {
		t.Fatalf("pending published winner: %v", err)
	}
	if err := os.Chmod(journalPath, 0644); err != nil {
		t.Fatal(err)
	}
	recovered, err := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).Snapshot()
	if err != nil {
		t.Fatalf("original pending recovery: %v", err)
	}
	if recovered.Revision != issued.Revision+1 {
		t.Fatalf("recovery revision=%d", recovered.Revision)
	}
	recoveredRecord, found, err := runtime.FindSubmission(recovered.State, packet.ID)
	if err != nil || !found || recoveredRecord != record {
		t.Fatalf("recovery changed receipt: %+v %v", recoveredRecord, err)
	}
	if _, err := os.Stat(statePath + ".commit-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending remains after recovery: %v", err)
	}
	afterState, afterJournal := s4WriterRead(t, root, ".claude/loop-state.json"), s4WriterRead(t, root, ".claude/loop-events.jsonl")
	if bytes.Count(afterJournal, []byte(`"event_id":"evt-evidence-E-S4-001"`)) != 1 {
		t.Fatal("recovery did not append exactly one reception event")
	}
	request.Path = ".claude/evidence/retry-candidate.json"
	replay, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if err != nil || replay.Revision != recovered.Revision {
		t.Fatalf("post-recovery replay: revision=%d err=%v", replay.Revision, err)
	}
	if !bytes.Equal(afterState, s4WriterRead(t, root, ".claude/loop-state.json")) || !bytes.Equal(afterJournal, s4WriterRead(t, root, ".claude/loop-events.jsonl")) {
		t.Fatal("post-recovery replay changed original pair")
	}
	if got, err := runtime.ReadTaskPlanningReport(root, winnerPath); err != nil || !bytes.Equal(got, report) {
		t.Fatalf("recovery changed published winner: %v", err)
	}
}
