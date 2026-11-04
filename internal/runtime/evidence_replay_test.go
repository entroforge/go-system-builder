package runtime_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// A repeated record of the same evidence id keeps the domain's own refusal.
// The event id names the evidence identity, so the journal already holds it
// when the Runtime already holds the evidence row; the domain answer must not
// be replaced by the journal's key collision, and the repeat must write
// neither a Runtime fact nor a journal fact.
func TestRecordEvidenceRepeatedIdentityKeepsDomainRefusal(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	if err := os.WriteFile(filepath.Join(root, "REV-001.md"), []byte(`{"schema_version":"1.0.0","evidence_id":"EV-REPLAY-001","kind":"document_review","runtime_id":"loop-REQ-002-example","baseline_generation":1,"producer_agent_id":"document-verifier","producer_responsibility":"DV-TRUTH-AUDIT"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	request := runtime.EvidenceRequest{
		ExpectedRevision: -1,
		ID:               "EV-REPLAY-001",
		Kind:             "document_review",
		Path:             "REV-001.md",
		ProducedBy:       []string{"document-verifier"},
		ResponsibilityID: "DV-TRUTH-AUDIT",
		Validator:        testCandidateValidator(),
	}
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err != nil {
		t.Fatalf("RecordEvidence failed: %v", err)
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RecordEvidence(root, statePath, journalPath, request)
	if err == nil || !strings.Contains(err.Error(), "evidence EV-REPLAY-001 is already registered") {
		t.Fatalf("repeated evidence id did not keep the domain refusal: %v", err)
	}
	if strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("repeated evidence id answered with the journal key collision: %v", err)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateBefore) != string(stateAfter) || string(journalBefore) != string(journalAfter) {
		t.Fatal("repeated evidence id wrote a new Runtime fact")
	}
}

// An already accepted submission is a replay of the committed reception, not
// a failure: the caller sees no error and the Runtime and the journal stay
// exactly as the accepted reception left them.
func TestRecordEvidenceAcceptedContractSubmissionReplaysWithoutWriting(t *testing.T) {
	root, statePath, journalPath, request := evidenceContractReception(t, semantic.RuntimeCandidateValidator{})
	first, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("original reception failed: %v", err)
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("accepted submission did not replay: %v", err)
	}
	if replay.Revision != first.Revision {
		t.Fatalf("replay moved the revision: got %d, want %d", replay.Revision, first.Revision)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateBefore) != string(stateAfter) || string(journalBefore) != string(journalAfter) {
		t.Fatal("replay of an accepted submission wrote a new Runtime fact")
	}
}

// Two callers that receive the same submission at the same instant keep one
// writer and one replay: both succeed, the Runtime holds one reception row and
// the journal holds one event for it. The judgment runs under the store lock,
// so the loser of the race is judged against the committed reception row
// instead of a snapshot taken before the winner committed.
func TestRecordEvidenceConcurrentContractReceptionKeepsOneWriterAndOneReplay(t *testing.T) {
	gate := newEvidenceReceptionGate(semantic.RuntimeCandidateValidator{})
	root, statePath, journalPath, request := evidenceContractReception(t, gate)
	// The second caller must read its Runtime snapshot after the first caller
	// committed: that is the interleaving the reception has to survive, and it
	// is the one a caller with a stale snapshot would have got wrong.
	gate.arm()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			defer gate.release()
			<-start
			_, err := runtime.RecordEvidence(root, statePath, journalPath, request)
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent reception of the same submission: %v", err)
		}
	}
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if rows := evidenceReplayRows(snapshot.State, request.ID); rows != 1 {
		t.Fatalf("evidence rows for %s = %d, want 1", request.ID, rows)
	}
	rows, _ := snapshot.State["entities"].(map[string]any)["submissions"].([]any)
	if len(rows) != 1 {
		t.Fatalf("submission rows = %d, want 1", len(rows))
	}
	events := evidenceReplayJournalEvents(t, journalPath)
	prefix := fmt.Sprintf("evt-evidence-%s", request.ID)
	written := 0
	for id := range events {
		if strings.HasPrefix(id, prefix) {
			written += events[id]
		}
	}
	if written != 1 {
		t.Fatalf("journal events for the reception = %v, want exactly one", events)
	}
}

// A repeated contract reception whose professional answer differs is a
// conflict, not a replay. The judgment has to tell the two apart: the replay
// is the same professional answer received again, the conflict is a different
// answer claiming the same reception.
func TestRecordEvidenceRepeatedContractSubmissionWithChangedAnswerConflicts(t *testing.T) {
	root, statePath, journalPath, request := evidenceContractReception(t, semantic.RuntimeCandidateValidator{})
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err != nil {
		t.Fatalf("original reception failed: %v", err)
	}
	var draft workpacket.ContractDraft
	if err := json.Unmarshal(request.Submission.AnswerJSON, &draft); err != nil {
		t.Fatal(err)
	}
	draft.Answer.Applicability["FE"] = workpacket.ContractApplicability{Disposition: "required", Rationale: "A different professional statement of the same view."}
	changed, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	conflict := request
	submission := *request.Submission
	submission.AnswerJSON = changed
	conflict.Submission = &submission
	_, err = runtime.RecordEvidence(root, statePath, journalPath, conflict)
	if !errors.Is(err, runtime.ErrSubmissionConflict) {
		t.Fatalf("repeated reception with a different answer answered with %v, want ErrSubmissionConflict", err)
	}
}

// A stale repeat answers with the store's stale-revision error. The domain
// judgment and the revision CAS have one order, and the journal's event-id
// collision is never one of the answers.
func TestRecordEvidenceStaleRepeatAnswersWithStaleRevision(t *testing.T) {
	root, statePath, journalPath, request := evidenceContractReception(t, semantic.RuntimeCandidateValidator{})
	first, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("original reception failed: %v", err)
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	stale := request
	stale.ExpectedRevision = first.Revision - 1
	_, err = runtime.RecordEvidence(root, statePath, journalPath, stale)
	if !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("stale repeat answered with %v, want ErrStaleRevision", err)
	}
	if strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("stale repeat answered with the journal key collision: %v", err)
	}
	if strings.Contains(err.Error(), "is already registered") {
		t.Fatalf("stale repeat answered with the domain refusal: %v", err)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateBefore) != string(stateAfter) || string(journalBefore) != string(journalAfter) {
		t.Fatal("stale repeat wrote a new Runtime fact")
	}
}

// evidenceContractReception builds the original S3 contract responsibility
// and the reception request that hands the professional answer to the Writer.
// The S3 contract reception is the original submission kind whose professional
// reception happens inside the atomic Apply, so it is the path a concurrent
// reception has to keep working.
func evidenceContractReception(t *testing.T, candidate runtime.CandidateValidator) (string, string, string, runtime.EvidenceRequest) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{".claude/evidence", "docs/requirements", "docs/architecture", "docs/design/decisions", "docs/dev/contracts", "docs/dev/tasks", "docs/rules"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	reqPath := "docs/requirements/REQ-001.md"
	designPath := "docs/architecture/ARCHITECTURE-001.md"
	conditionalPath := "docs/design/decisions/ADR-001.md"
	indexPath := "docs/dev/contracts/CONTRACTS-001.md"
	contractPath := "docs/dev/contracts/FE-001.md"
	writeBytes(t, root, reqPath, []byte("# REQ-001\n> Status: locked\n> Version: v1.0.0\n"))
	writeBytes(t, root, designPath, []byte("# Architecture\n> Status: locked\n> Version: v1.0.0\n"))
	writeBytes(t, root, conditionalPath, []byte("# ADR-001\n> Status: accepted\n> Version: v1.0.0\n"))
	writeBytes(t, root, indexPath, []byte("# CONTRACTS-001\n> Requirement: REQ-001\n> Status: locked\n> Version: v1.0.0\n\n## 合同清单\n| Contract ID | Type | Name | Status | Version | Notes |\n|:---|:---|:---|:---|:---|:---|\n| FE-001 | frontend | ui | locked | v1.0.0 | entry |\n\n## 需求覆盖矩阵\n| Contract | Clause | Purpose |\n|:---|:---|:---|\n| FE-001 §1 | user view | visible |\n"))
	writeBytes(t, root, contractPath, []byte("# FE-001\n> Status: locked\n> Version: v1.0.0\n\n| 本合同条款 | 承诺 |\n|---|---|\n| §1 | The user view is visible. |\n"))
	writeBytes(t, root, "docs/dev/tasks/TASK-template.md", []byte("# Task template\n"))
	writeBytes(t, root, "docs/rules/dispatch-plan.md", []byte("# Dispatch plan rules\n"))

	reqRef := workpacket.Reference{ID: "REQ-001", Path: reqPath, Version: "v1.0.0"}
	designRef := workpacket.Reference{ID: "ARCHITECTURE-001", Path: designPath, Version: "v1.0.0"}
	packet, err := workpacket.BuildContracts(workpacket.PlanningBinding{RuntimeID: "loop-REQ-001", Generation: 1, StageEntry: 2, Stage: "S3", Owner: "Main", Requirement: reqRef}, workpacket.ContractInputs{
		Designs:     []workpacket.Reference{designRef},
		Sources:     []workpacket.Subject{{Path: conditionalPath, Version: "v1.0.0"}},
		Directories: map[string][]string{"docs/design/decisions": {"ADR-001.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := workpacket.ContractAnswer{
		Conclusion: stringPtr("pass"), IndexPath: stringPtr(indexPath), ContractPaths: []string{contractPath},
		Applicability: map[string]workpacket.ContractApplicability{
			"FE":   {Disposition: "required", Rationale: "The REQ includes a user-facing view."},
			"BE":   {Disposition: "not_applicable", Rationale: "This REQ has no backend behavior."},
			"SYNC": {Disposition: "not_applicable", Rationale: "This REQ has no shared integration behavior."},
		},
	}
	artifacts := []workpacket.Reference{
		{ID: "CONTRACTS-001", Path: indexPath, Version: "v1.0.0"},
		{ID: "FE-001", Path: contractPath, Version: "v1.0.0"},
	}
	evidence, err := workpacket.ContractEvidence(packet, "E-S3-REPLAY", artifacts, answer, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	evidenceData, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidencePath := ".claude/evidence/E-S3-REPLAY.json"
	writeBytes(t, root, evidencePath, evidenceData)

	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"], state["runtime_id"], state["revision"] = "1.2.0", packet.Binding.RuntimeID, 0
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": packet.Binding.StageEntry}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	bound := packet.Binding.Requirement
	state["bound_req"] = map[string]any{"id": bound.ID, "path": bound.Path, "version": bound.Version, "status": "locked", "approved_by": "user", "approved_at": "2026-06-18T00:00:00Z", "metadata": map[string]any{"ui_impact": "none"}, "workspace": map[string]any{"project_root": root, "dev_branch": "test-development", "release_upstream": "main"}}
	state["documents"] = []any{
		map[string]any{"id": designRef.ID, "kind": "design", "path": designRef.Path, "version": designRef.Version, "status": "locked", "generation": 1},
		map[string]any{"id": "CONTRACTS-001", "kind": "contract", "path": indexPath, "version": "v1.0.0", "status": "locked", "generation": 1},
		map[string]any{"id": "FE-001", "kind": "contract", "path": contractPath, "version": "v1.0.0", "status": "locked", "generation": 1},
	}
	entities := state["entities"].(map[string]any)
	entities["work_packets"], entities["submissions"] = []any{}, []any{}
	state["evidence"] = []any{}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, root, "docs/control/loop-definition.json", definition)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	draft := packet.Draft
	draft.Answer = answer
	answerJSON, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	files := fileview.Disk{Root: root}
	if _, err := runtime.RecordContractPacket(root, statePath, journalPath, packet, files, candidate); err != nil {
		t.Fatalf("original S3 packet Writer: %v", err)
	}
	paths := append([]string{*answer.IndexPath}, answer.ContractPaths...)
	request := runtime.EvidenceRequest{
		ExpectedRevision: -1,
		ID:               "E-S3-REPLAY",
		Kind:             "planning_contract",
		Path:             evidencePath,
		ProducedBy:       []string{"Main"},
		ResponsibilityID: "Orchestrator",
		Validator:        candidate,
		Submission: &runtime.SubmissionRequest{PacketID: packet.ID, Owner: "Main", AnswerJSON: answerJSON, Files: files,
			Authorize: func(root string, _ map[string]any) error {
				checked, err := semantic.ContractsCheckBatch(root, files, packet.Binding.Requirement, paths)
				if err != nil {
					return err
				}
				if checked.Contracts == 0 || len(checked.Problems) != 0 {
					return fmt.Errorf("original contract check: %+v", checked)
				}
				return nil
			}},
	}
	return root, statePath, journalPath, request
}

// evidenceReceptionGate holds every caller that arrives after the first one
// until one reception has committed. A caller reads the Runtime snapshot
// before it enters the store, and the store's judgment is made against the
// state it reads under its own lock. Holding the second caller before that
// read reproduces the interleaving a concurrent reception has to survive: the
// second caller observes the committed reception of the first one instead of a
// snapshot taken before it committed.
type evidenceReceptionGate struct {
	base runtime.CandidateValidator

	mu        sync.Mutex
	armed     bool
	arrivals  int
	committed chan struct{}
	once      sync.Once
}

func newEvidenceReceptionGate(base runtime.CandidateValidator) *evidenceReceptionGate {
	return &evidenceReceptionGate{base: base, committed: make(chan struct{})}
}

// arm starts holding every caller that arrives after the first one.
func (g *evidenceReceptionGate) arm() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.armed = true
	g.arrivals = 0
}

// release reports that one reception has committed.
func (g *evidenceReceptionGate) release() {
	g.once.Do(func() { close(g.committed) })
}

func (g *evidenceReceptionGate) ValidateCandidate(root string, state map[string]any) error {
	err := g.base.ValidateCandidate(root, state)
	// The Writer's constructor probe runs before the caller reads the Runtime
	// snapshot. It is the one place where a caller can be held until the
	// other writer has committed.
	if root == "" {
		g.mu.Lock()
		armed := g.armed
		if armed {
			g.arrivals++
		}
		wait := armed && g.arrivals > 1
		g.mu.Unlock()
		if wait {
			<-g.committed
		}
	}
	return err
}

// evidenceReplayRows counts the Runtime evidence rows that carry one
// evidence identity.
func evidenceReplayRows(state map[string]any, id string) int {
	items, _ := state["evidence"].([]any)
	count := 0
	for _, raw := range items {
		row, _ := raw.(map[string]any)
		if row["id"] == id {
			count++
		}
	}
	return count
}

// evidenceReplayJournalEvents counts the journal events by their event id.
func evidenceReplayJournalEvents(t *testing.T, journalPath string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if id, ok := event["event_id"].(string); ok {
			counts[id]++
		}
	}
	return counts
}
