package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func originalS3EvidencePath(t *testing.T, state map[string]any, packetID string) string {
	t.Helper()
	record, found, err := runtime.FindSubmission(state, packetID)
	if err != nil || !found {
		t.Fatalf("original submission: %+v %v", record, err)
	}
	for _, raw := range state["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == record.EvidenceID {
			return row["path"].(string)
		}
	}
	t.Fatal("registered original evidence path missing")
	return ""
}

func TestS3OriginalEvidenceWriterPreservesOccupiedFileOrDirectory(t *testing.T) {
	for _, occupied := range []string{"file", "directory"} {
		t.Run(occupied, func(t *testing.T) {
			root, p := newContractAnswerFixture(t)
			stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
			preferred := filepath.Join(root, ".claude", "evidence", "submissions", p.ID, "answer.json")
			if err := os.MkdirAll(filepath.Dir(preferred), 0700); err != nil {
				t.Fatal(err)
			}
			unknown := preferred
			if occupied == "directory" {
				if err := os.Mkdir(preferred, 0700); err != nil {
					t.Fatal(err)
				}
				unknown = filepath.Join(preferred, "author-owned.txt")
			}
			if err := os.WriteFile(unknown, []byte("unknown author-owned data"), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := s.SaveContracts(context.Background(), p.AnswerPath)
			if err != nil || !receipt.Accepted {
				t.Fatalf("original S3 receipt: %+v %v", receipt, err)
			}
			if data, err := os.ReadFile(unknown); err != nil || string(data) != "unknown author-owned data" {
				t.Fatalf("unknown file changed: %q %v", data, err)
			}
			snap := stageSnapshot(t, root)
			actual := originalS3EvidencePath(t, snap.State, p.ID)
			if filepath.Clean(filepath.Join(root, actual)) == filepath.Clean(preferred) {
				t.Fatal("occupied path was claimed as the report")
			}
			data, err := os.ReadFile(filepath.Join(root, actual))
			if err != nil {
				t.Fatal(err)
			}
			answer, err := runtime.ContractAnswerFromEvidence(data)
			if err != nil || answer.IndexPath == nil || !strings.Contains(*answer.IndexPath, "CONTRACTS-039") {
				t.Fatalf("actual issued professional answer: %+v %v", answer, err)
			}
			if snap.State["lifecycle"].(map[string]any)["phase"] != "tasks" {
				t.Fatalf("S4 not reached: %+v; receipt=%+v", snap.State["lifecycle"], receipt)
			}
			replayed, err := s.SaveContracts(context.Background(), p.AnswerPath)
			if err != nil || !replayed.Accepted || stageSnapshot(t, root).Revision != snap.Revision {
				t.Fatalf("same original replay: %+v %v", replayed, err)
			}
		})
	}
}

func TestS3OriginalEvidenceWriterDoesNotClaimIdenticalUnknownFile(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	draft := completedContractDraft(p)
	stageJSON(t, root, p.AnswerPath, draft)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	snap := stageSnapshot(t, root)
	artifacts, err := runtime.ContractArtifacts(root, snap.State, p, draft.Answer, s.Files)
	if err != nil {
		t.Fatal(err)
	}
	id := "ev-submission-" + p.ID
	envelope, err := workpacket.ContractEvidence(p, id, artifacts, draft.Answer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	preferred := filepath.ToSlash(filepath.Join(".claude", "evidence", "submissions", p.ID, "answer.json"))
	absolute := filepath.Join(root, filepath.FromSlash(preferred))
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	answerJSON, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	next, err := runtime.RecordEvidence(root, s.StatePath, s.JournalPath, runtime.EvidenceRequest{ExpectedRevision: -1, ID: id, Kind: "planning_contract", Path: preferred, ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: s.Validator, Submission: &runtime.SubmissionRequest{PacketID: p.ID, Owner: "Main", AnswerJSON: answerJSON, EvidenceJSON: encoded, Files: s.Files, Authorize: func(root string, state map[string]any) error {
		paths := append([]string{*draft.Answer.IndexPath}, draft.Answer.ContractPaths...)
		result, err := semantic.ContractsCheckBatch(root, s.Files, p.Binding.Requirement, paths)
		if err != nil {
			return err
		}
		if result.Contracts == 0 || len(result.Problems) != 0 {
			t.Fatalf("original contract check: %+v", result)
		}
		return nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	actual := originalS3EvidencePath(t, next.State, p.ID)
	if actual == preferred {
		t.Fatal("unregistered identical content was claimed")
	}
	got, err := os.ReadFile(absolute)
	if err != nil || string(got) != string(encoded) {
		t.Fatalf("unknown identical file changed: %v", err)
	}
}

func TestS3ConcurrentOriginalReceptionKeepsOneWriterAndReplay(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	services := make([]*submission.Service, 2)
	for i := range services {
		var err error
		services[i], err = submission.New(root)
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, len(services))
	for _, service := range services {
		wg.Add(1)
		go func(s *submission.Service) {
			defer wg.Done()
			<-start
			receipt, err := s.SaveContracts(context.Background(), p.AnswerPath)
			if err != nil {
				errs <- err
			} else if !receipt.Accepted {
				errs <- context.Canceled
			}
		}(service)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent original reception: %v", err)
	}
	if t.Failed() {
		return
	}
	snap := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil || !found || record.EvidenceID == "" {
		t.Fatalf("original receipt: %+v %v", record, err)
	}
	count := 0
	for _, raw := range snap.State["evidence"].([]any) {
		if raw.(map[string]any)["id"] == record.EvidenceID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("original evidence rows=%d", count)
	}
	replayed, err := services[0].SaveContracts(context.Background(), p.AnswerPath)
	if err != nil || !replayed.Accepted || stageSnapshot(t, root).Revision != snap.Revision {
		t.Fatalf("same original replay: %+v %v", replayed, err)
	}
}

func TestS3PendingContinuesWithoutPrivateAnswerAndReceiptKeepsAcceptedFact(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	accepted, err := s.SaveContracts(ctx, p.AnswerPath)
	if err != nil || !accepted.Accepted || len(accepted.WaitingOn) == 0 {
		t.Fatalf("original pending receipt: %+v %v", accepted, err)
	}
	before := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(before.State, p.ID)
	if err != nil || !found || record.ControlStatus != "pending" {
		t.Fatalf("original pending: %+v %v", record, err)
	}
	if err := os.Remove(filepath.Join(root, p.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	continued, err := s.SaveContracts(context.Background(), p.AnswerPath)
	if err != nil || !continued.Accepted {
		t.Fatalf("pending continuation required original answer again: %+v %v", continued, err)
	}
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["phase"] != "tasks" {
		t.Fatalf("original followup did not reach S4: %+v", after.State["lifecycle"])
	}
	path := filepath.Join(root, originalS3EvidencePath(t, after.State, p.ID))
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	// Receipt is the already-committed original fact. Missing professional
	// material remains a separate issue for any successor that needs it.
	receipt, err := s.ContractReceipt("", p.ID)
	if err != nil || !receipt.Accepted || stageSnapshot(t, root).Revision != after.Revision {
		t.Fatalf("already accepted receipt depended on missing private/report bytes: %+v %v", receipt, err)
	}
}
