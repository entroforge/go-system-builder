package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/transition"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestStageAnswerNamedRevisionHoldsOriginalGateUntilNewAnswer(t *testing.T) {
	for _, verdict := range []string{"pass", "fix_required", "req_change_required"} {
		t.Run(verdict, func(t *testing.T) {
			f := newStageAnswerFixture(t)
			old, peer := f.packets[0], f.packets[1]
			// Ordinary S5 answers and named revisions use the current issued
			// professional packets without a separate generic PLAN step.
			first := saveStageHook(t, f.root, f.root, old.Binding.Owner, old.AnswerPath, completedDraft(old, "pass"))
			if !first.WorkComplete || first.SubmissionKey == "" {
				t.Fatalf("original receipt has no named revision key: %+v", first)
			}
			s, err := submission.New(f.root)
			if err != nil {
				t.Fatal(err)
			}
			issuedJSON := stageCLI(t, "runtime", "stage-answer", "revise", "--root", f.root, "--previous", first.SubmissionKey, "--reason", "Recheck the concrete dependency finding before S5 advances")
			var next workpacket.Packet
			if err := json.Unmarshal([]byte(issuedJSON), &next); err != nil {
				t.Fatal(err)
			}
			issued := stageSnapshot(t, f.root)
			replay, err := s.Revise(context.Background(), first.SubmissionKey, next.Binding.RevisionReason)
			if err != nil || replay.ID != next.ID || stageSnapshot(t, f.root).Revision != issued.Revision {
				t.Fatalf("revision issuance was not idempotent: %v", err)
			}
			// Simulate loss of post-commit derived files before any professional
			// answer exists. Replaying restores projections, not a new issuance.
			if verdict == "pass" {
				for _, path := range []string{next.ViewPath, next.AnswerPath} {
					if err := os.Remove(filepath.Join(f.root, path)); err != nil {
						t.Fatal(err)
					}
				}
				if restored, err := s.Revise(context.Background(), first.SubmissionKey, next.Binding.RevisionReason); err != nil || restored.ID != next.ID || stageSnapshot(t, f.root).Revision != issued.Revision {
					t.Fatalf("revision projection recovery changed the durable issuance: %v", err)
				}
				for _, path := range []string{next.ViewPath, next.AnswerPath} {
					if _, err := os.Stat(filepath.Join(f.root, path)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := s.Revise(context.Background(), first.SubmissionKey, "A different concrete observation"); !errors.Is(err, runtime.ErrRevisionConflict) {
				t.Fatalf("revision branched: %v", err)
			}
			historical, err := s.ReceiptPacket(old.Binding.Owner, old.ID)
			if err != nil || !historical.Accepted || historical.WorkComplete || historical.ResponsibilityStatus != "replaced" || submission.CompletedOwner(f.root, old.Binding.Owner) {
				t.Fatalf("old completion authorized the new attempt: %+v %v", historical, err)
			}
			if _, err := s.Save(context.Background(), old.Binding.Owner, old.AnswerPath); !errors.Is(err, runtime.ErrSubmissionSuperseded) {
				t.Fatalf("old answer replay allowed after issuance: %v", err)
			}
			beforeDraft := stageSnapshot(t, f.root)
			if r, err := s.Save(context.Background(), next.Binding.Owner, next.AnswerPath); err == nil || r.WorkComplete || stageSnapshot(t, f.root).Revision != beforeDraft.Revision {
				t.Fatal("empty revision draft occupied the new attempt")
			}
			r := saveStageHook(t, f.root, f.root, peer.Binding.Owner, peer.AnswerPath, completedDraft(peer, "pass"))
			if !r.WorkComplete || stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"] != "document_verification" {
				t.Fatal("peer PASS consumed old PASS while revision was pending")
			}
			peerRecord, _, _ := runtime.FindSubmission(stageSnapshot(t, f.root).State, peer.ID)
			manualBefore := stageSnapshot(t, f.root).Revision
			if _, err := transition.Apply(f.root, s.StatePath, s.JournalPath, transition.Request{TransitionID: "TR-003", ExpectedRevision: -1, Actor: "orchestrator", Evidence: map[string]string{"document_review_record": peerRecord.EvidenceID}, Files: s.Files}); err == nil || stageSnapshot(t, f.root).Revision != manualBefore {
				t.Fatal("manual transition bypassed the pending named revision")
			}
			// Runtime supplies the current exact path; old authoring paths cannot
			// replace the new packet.
			r = saveStageHook(t, f.root, f.root, next.Binding.Owner, next.AnswerPath, completedDraft(next, verdict))
			if !r.WorkComplete || !r.Accepted {
				t.Fatalf("new answer rejected: %+v", r)
			}
			want := map[string]string{"pass": "building", "fix_required": "planning", "req_change_required": "document_verification"}[verdict]
			final := stageSnapshot(t, f.root)
			if final.State["lifecycle"].(map[string]any)["state"] != want {
				t.Fatalf("new verdict skipped original route: %v", final.State["lifecycle"])
			}
			oldRecord, _, _ := runtime.FindSubmission(final.State, old.ID)
			newRecord, _, _ := runtime.FindSubmission(final.State, next.ID)
			if oldRecord.SupersededBy != newRecord.Key || len(final.State["entities"].(map[string]any)["submissions"].([]any)) != 3 {
				t.Fatal("domain replacement lost reception history")
			}
			before := final.Revision
			r = saveStageHook(t, f.root, f.root, next.Binding.Owner, next.AnswerPath, completedDraft(next, verdict))
			if !r.WorkComplete || stageSnapshot(t, f.root).Revision != before {
				t.Fatal("new answer replay repeated reception/transition")
			}
		})
	}
}

func TestIndependentNamedRevisionRetainsOldSourceAndRequiresNewExecution(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	oldExecution := f.prepare(t, p)
	f.begin(t, p, &oldExecution)
	first := saveStageHook(t, f.root, oldExecution.Path, p.Binding.Owner, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	if !first.WorkComplete {
		t.Fatalf("original source not received: %+v", first)
	}
	oldAnswer, err := os.ReadFile(filepath.Join(oldExecution.Path, workpacket.WorkerAnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	s, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, f.root)
	var workerOut, workerErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "revise", "--root", oldExecution.Path, "--previous", first.SubmissionKey, "--reason", "Recheck a concrete observation"}, nil, &workerOut, &workerErr); code == 0 || stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("Worker root routing granted Main revision authority")
	}
	// Production launch holds exactly this lease while a real child runs.
	lease, err := filelock.Acquire(context.Background(), filepath.Join(f.root, ".claude/workspace-launch", oldExecution.RuntimeID, "g1-"+oldExecution.AssignmentID+"-e1.lock"))
	if err != nil {
		t.Fatal(err)
	}
	_, revisionErr := s.Revise(context.Background(), first.SubmissionKey, "Recheck a specific frozen document observation")
	lease()
	if revisionErr == nil || stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("running Worker was retired or failed issuance changed runtime")
	}
	next, err := s.Revise(context.Background(), first.SubmissionKey, "Recheck a specific frozen document observation")
	if err != nil {
		t.Fatal(err)
	}
	if !next.Binding.ExecutionRequired {
		t.Fatal("independent revision did not freeze its source requirement")
	}
	b, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	if _, active := b.Executions[p.Binding.AssignmentID]; active || len(b.History) != 1 || b.History[0].Status != "retired" || b.History[0].WorkPacketID != p.ID {
		t.Fatal("previous source not atomically retained as retired history")
	}
	if _, err := os.Stat(filepath.Join(f.root, next.AnswerPath)); !os.IsNotExist(err) {
		t.Fatal("independent revision published a Main answer fallback")
	}
	if _, err := s.Save(context.Background(), next.Binding.Owner, next.AnswerPath); err == nil {
		t.Fatal("independent revision accepted shared Main source")
	}
	fresh := f.prepare(t, next)
	if fresh.Path == oldExecution.Path || fresh.Generation <= oldExecution.Generation || fresh.WorkPacketID != next.ID || fresh.Purpose != workspace.DocumentReviewPurpose {
		t.Fatal("revision reused the old immutable execution")
	}
	if !reflect.DeepEqual(fresh.WritePaths, []string{}) || !reflect.DeepEqual(fresh.Checks, []string{}) {
		t.Fatal("revision broadened product write/check scope")
	}
	if r, err := s.Save(context.Background(), p.Binding.Owner, filepath.Join(oldExecution.Path, workpacket.WorkerAnswerPath)); err == nil || r.WorkComplete {
		t.Fatal("retired source delivered the new attempt")
	}
	if submission.CompletedOwner(f.root, p.Binding.Owner) {
		t.Fatal("old reception ended pending new responsibility")
	}
	if code, msg := stageHookCode(t, f.root, fresh.Path, fresh.AgentID, "Stop", ""); code != 2 {
		t.Fatalf("new unreceived Worker Stop: %d %s", code, msg)
	}
	r := saveStageHook(t, f.root, fresh.Path, fresh.AgentID, workpacket.WorkerAnswerPath, completedDraft(next, "pass"))
	if !r.WorkComplete || !r.Accepted {
		t.Fatalf("fresh revision reception: %+v", r)
	}
	unchanged, err := os.ReadFile(filepath.Join(oldExecution.Path, workpacket.WorkerAnswerPath))
	if err != nil || string(unchanged) != string(oldAnswer) {
		t.Fatal("revision overwrote previous answer files")
	}
	if err := b.ValidateBootstrap(oldExecution); err != nil {
		t.Fatalf("revision modified old bootstrap: %v", err)
	}
}

// Pause only after the real gate has evaluated PASS, without holding a Writer.
// Issuance must also defeat that stale evaluation in transition.Apply's lock.
type revisionPausedEvaluator struct {
	engine    qualitygate.Evaluator
	evaluated chan struct{}
	resume    chan struct{}
	once      sync.Once
}

func (e *revisionPausedEvaluator) Evaluate(ctx context.Context, input qualitygate.Input) (qualitygate.Evaluation, error) {
	result, err := e.engine.Evaluate(ctx, input)
	if input.GateID == "GATE-DOCUMENT-PASS" {
		e.once.Do(func() { close(e.evaluated) })
		select {
		case <-e.resume:
		case <-ctx.Done():
			return qualitygate.Evaluation{}, ctx.Err()
		}
	}
	return result, err
}

func TestNamedRevisionDefeatsAnAlreadyEvaluatedPeerPass(t *testing.T) {
	f := newStageAnswerFixture(t)
	old, peer := f.packets[0], f.packets[1]
	f.begin(t, old, nil)
	f.begin(t, peer, nil)
	first := saveStageHook(t, f.root, f.root, old.Binding.Owner, old.AnswerPath, completedDraft(old, "pass"))
	s, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	answer := completedDraft(peer, "pass")
	artifactPath := ".claude/evidence/peer-before-followup.json"
	artifactID := "ev-peer-before-followup"
	stageJSON(t, f.root, artifactPath, workpacket.DocumentReview(peer, artifactID, answer.Answer, time.Now().UTC()))
	domain, err := runtime.RecordEvidence(f.root, s.StatePath, s.JournalPath, runtime.EvidenceRequest{ExpectedRevision: -1, ID: artifactID, Kind: "document_review", Path: artifactPath, ProducedBy: []string{peer.Binding.Owner}, ResponsibilityID: peer.Binding.Responsibility, Validator: s.Validator, Submission: &runtime.SubmissionRequest{PacketID: peer.ID, Owner: peer.Binding.Owner, AnswerJSON: mustStageJSON(t, answer), Files: s.Files, Authorize: func(root string, state map[string]any) error {
		return assignment.ValidateWorkingAuthorization(root, state, peer.Binding.Owner)
	}}})
	if err != nil {
		t.Fatal(err)
	}
	record, ok, err := runtime.FindSubmission(domain.State, peer.ID)
	if err != nil || !ok {
		t.Fatal("peer domain reception missing")
	}
	catalog, err := transition.LoadCatalog(f.root)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	evaluator := &revisionPausedEvaluator{engine: qualitygate.NewEvaluator(registry), evaluated: make(chan struct{}), resume: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan controller.ControlResult, 1)
	go func() {
		result, err := controller.RunControlCycle(ctx, controller.ControlRequest{Root: f.root, CWD: f.root, Event: "PostToolUse", ToolName: "Write", AgentID: peer.Binding.Owner, SubmissionFollowupID: record.FollowupID, Files: s.Files, GateEvaluator: evaluator})
		if err != nil {
			result.Error = err.Error()
		}
		done <- result
	}()
	select {
	case <-evaluator.evaluated:
	case <-ctx.Done():
		t.Fatal("real PASS gate did not reach the controlled commit window")
	}
	next, revisionErr := s.Revise(ctx, first.SubmissionKey, "Recheck the specific finding after peer gate evaluation")
	close(evaluator.resume)
	result := <-done
	if revisionErr != nil {
		t.Fatal(revisionErr)
	}
	if next.Binding.Attempt != 2 || result.QualityGate.TransitionCommitted || stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatalf("stale gate crossed revision suspension: %+v", result.QualityGate)
	}
}
