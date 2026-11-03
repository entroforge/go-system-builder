package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9OriginalFlatConsumeReceivesAndProjectsReplay(t *testing.T) {
	root, worker, p, _, _ := nativeS9WorkerFixture(t)
	_, relative, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	path := filepath.Join(worker, relative)
	var out, diagnostic bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", path}, &out, &diagnostic); code != 0 {
		t.Fatalf("public original-input recovery: %d %s %s", code, &out, &diagnostic)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("did not receive original candidate: %v %s", err, &out)
	}
	before := repairDispatchRuntimeBytes(t, root)
	// System successor recovery never needs the now-consumed private files.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(worker, nativeS9TestPlanPath(t, root, p))); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	diagnostic.Reset()
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", path}, &out, &diagnostic); code != 0 {
		t.Fatalf("accepted replay: %d %s %s", code, &out, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeS9OriginalConsumeDoesNotReimportAcceptedPlan(t *testing.T) {
	root, worker, p, _, _ := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := receiveNativeS9ReadyPlan(context.Background(), root, worker, p, service, &diagnostic, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(context.Background(), p.Binding.OwnerAgentID, filepath.Join(root, p.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("plan receipt: %#v %v", receipt, err)
	}
	if err := os.WriteFile(filepath.Join(worker, nativeS9TestPlanPath(t, root, p)), []byte("new unfinished private Plan"), 0600); err != nil {
		t.Fatal(err)
	}
	_, relative, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, err := consumeRegisteredStageAnswer(context.Background(), service, filepath.Join(worker, relative)); err != nil {
		t.Fatal(err)
	}
}

// A normal return re-enters the original Plan consumer, even when that Plan
// was previously accepted. Damaged accepted artifacts must still stop Result
// reception after removing the observation-only receipt lookup.
func TestNativeS9NormalStopRejectsDamagedAcceptedPlan(t *testing.T) {
	for _, damage := range []string{"malformed-report", "invalid-receipt"} {
		t.Run(damage, func(t *testing.T) {
			root, worker, p, input, _ := nativeS9WorkerFixture(t)
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			var diagnostic bytes.Buffer
			if err := receiveNativeS9ReadyPlan(context.Background(), root, worker, p, service, &diagnostic, nil); err != nil {
				t.Fatal(err)
			}
			receipt, err := service.SaveRepairPlanReportFrom(context.Background(), p.Binding.OwnerAgentID, filepath.Join(root, p.AnswerPath), root, nil)
			if err != nil || !receipt.Accepted || len(receipt.DomainRefs) != 1 {
				t.Fatalf("original Plan acceptance: %#v %v", receipt, err)
			}
			reportPath := filepath.Join(root, receipt.DomainRefs[0])
			damaged := []byte("{broken accepted report")
			if damage == "invalid-receipt" {
				var report map[string]any
				readRepairLeadJSON(t, reportPath, &report)
				report["receipt"].(map[string]any)["work_complete"] = true
				damaged, err = json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(reportPath, damaged, 0600); err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			input.Event = "SubagentStop"
			if err := receiveNativeS9Builder(root, repairDispatchSnapshot(t, root).State, input, p, input.LastAssistantMessage, &diagnostic); err == nil {
				t.Fatal("normal Stop accepted a damaged original Plan and continued Result reception")
			}
			repairDispatchAssertUnchanged(t, root, before)
			retained, err := os.ReadFile(reportPath)
			if err != nil || !bytes.Equal(retained, damaged) {
				t.Fatalf("damaged accepted Plan was replaced instead of rejected: %v", err)
			}
		})
	}
}

func TestNativeS9OriginalConsumeRejectsMainAndWrongPacket(t *testing.T) {
	root, _, p, _, _ := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	_, relative, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	before := repairDispatchRuntimeBytes(t, root)
	for _, path := range []string{filepath.Join(root, relative), filepath.Join(root, ".claude/submissions/native-s9/wrong-plan/result.json")} {
		if _, err := consumeRegisteredStageAnswer(context.Background(), service, path); err == nil {
			t.Fatalf("invalid source accepted: %s", path)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
}

func TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery(t *testing.T) {
	root, worker, p, input, professional := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	_, relative, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, err := consumeRegisteredStageAnswer(context.Background(), service, filepath.Join(worker, relative)); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	professional.ResidualRisks = []string{"explicit new professional content"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, relative), professional); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	input.Event = "SubagentStop"
	err = receiveNativeS9Builder(root, repairDispatchSnapshot(t, root).State, input, p, input.LastAssistantMessage, &diagnostic)
	if !errors.Is(err, runtime.ErrSubmissionConflict) {
		t.Fatalf("new explicit professional content lost original conflict: %v %s", err, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

// This fixture captures a real accepted candidate and its actual Writer event,
// then exposes the same staged/durable boundaries as the Store integrity tests.
// It does not invent a business event or a replacement candidate.
func nativeS9OriginalCandidateBoundary(t *testing.T) (root, worker string, plan workpacket.RepairPlanReportPacket, packet workpacket.RepairResultPacket, previousState, previousJournal, targetState, targetJournal []byte) {
	t.Helper()
	return nativeS9OriginalCandidateBoundaryFor(t, false)
}

func nativeS9OriginalCandidateBoundaryFor(t *testing.T, legacy bool) (root, worker string, plan workpacket.RepairPlanReportPacket, packet workpacket.RepairResultPacket, previousState, previousJournal, targetState, targetJournal []byte) {
	t.Helper()
	var professional workpacket.RepairResultProfessionalAnswer
	if legacy {
		root, worker, plan, _, professional = nativeS9WorkerFixture(t)
	} else {
		root, worker, plan, _, professional = nativeS9WorkerFixture(t)
	}
	ctx := context.Background()
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := receiveNativeS9ReadyPlan(ctx, root, worker, plan, service, &diagnostic, nil); err != nil {
		t.Fatal(err)
	}
	if r, err := service.SaveRepairPlanReportFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !r.Accepted {
		t.Fatalf("plan receipt: %#v %v", r, err)
	}
	if err := recordNativeS9OriginalLocation(ctx, root, plan, worker); err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueRepairResults(ctx, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("packet issue: %d %v", len(packets), err)
	}
	packet = packets[0]
	draft := packet.Draft
	draft.Answer, _ = json.Marshal(professional)
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, packet.AnswerPath), draft); err != nil {
		t.Fatal(err)
	}
	previousState, err = os.ReadFile(service.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	previousJournal, err = os.ReadFile(service.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := service.SaveRepairResultFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(worker, packet.AnswerPath), worker, nil); err != nil || !r.Accepted {
		t.Fatalf("candidate receipt: %#v %v", r, err)
	}
	targetState, err = os.ReadFile(service.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	targetJournal, err = os.ReadFile(service.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat(t *testing.T) {
	for _, stateWritten := range []bool{false, true} {
		name := "before_state_publication"
		if stateWritten {
			name = "after_state_before_journal"
		}
		t.Run(name, func(t *testing.T) {
			root, worker, p, packet, previousState, previousJournal, targetState, targetJournal := nativeS9OriginalCandidateBoundaryFor(t, true)
			statePath := filepath.Join(root, ".claude/loop-state.json")
			journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
			markerBytes := nativeS9OriginalPendingMarker(t, previousState, targetState, targetJournal)
			var marker map[string]any
			if err := json.Unmarshal(markerBytes, &marker); err != nil {
				t.Fatal(err)
			}
			marker["retain_last_transition"] = true
			markerBytes, err := json.MarshalIndent(marker, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			currentState := previousState
			if stateWritten {
				currentState = targetState
			}
			if err := os.WriteFile(statePath, currentState, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, previousJournal, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath+".commit-pending.json", markerBytes, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(worker, packet.AnswerPath)); err != nil {
				t.Fatal(err)
			}
			_, relative, pathErr := nativeS9AuthorPaths(root, p)
			if pathErr != nil {
				t.Fatal(pathErr)
			}
			flatPath := filepath.Join(worker, relative)
			replacement := []byte("later unrelated/unfinished flat bytes")
			if err := os.WriteFile(flatPath, replacement, 0600); err != nil {
				t.Fatal(err)
			}
			// A read-only query reports the boundary and cannot finish the pair.
			if _, err := runtime.NewStore(statePath, journalPath).Snapshot(); !errors.Is(err, runtime.ErrPendingRuntimeOperation) {
				t.Fatalf("missing real pending boundary: %v", err)
			}
			var out, diagnostic bytes.Buffer
			if code := runStageAnswer([]string{"consume", "--root", root, "--path", flatPath}, &out, &diagnostic); code != 0 {
				t.Fatalf("public pending recovery: %d %s %s", code, &out, &diagnostic)
			}
			afterState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			afterJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterState, targetState) || !bytes.Equal(afterJournal, targetJournal) {
				t.Fatal("pending recovery replaced original candidate or added a second business event")
			}
			if _, err := os.Stat(statePath + ".commit-pending.json"); !os.IsNotExist(err) {
				t.Fatalf("original pending marker not retired: %v", err)
			}
			if _, err := os.Stat(filepath.Join(worker, packet.AnswerPath)); !os.IsNotExist(err) {
				t.Fatalf("recovery recreated typed input from changed flat: %v", err)
			}
			if after, err := os.ReadFile(flatPath); err != nil || !bytes.Equal(after, replacement) {
				t.Fatalf("recovery changed later private flat bytes: %v", err)
			}
			beforeReplay := repairDispatchRuntimeBytes(t, root)
			out.Reset()
			diagnostic.Reset()
			if code := runStageAnswer([]string{"consume", "--root", root, "--path", flatPath}, &out, &diagnostic); code != 0 {
				t.Fatalf("pending successor replay: %d %s %s", code, &out, &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, beforeReplay)
		})
	}
}

// The private original changed after the Result was already received. The
// system does not detect that drift by comparing bytes against a registered
// digest (L3-S9 §12.1: the system does not rely on re-reading the old draft
// to discover rewrites; L4-workflow-convergence §281). The already accepted
// fact stays accepted and is not overwritten.
func TestNativeS9PendingRecoveryPreservesAcceptedFactAfterPrivateOriginalChange(t *testing.T) {
	for _, stateWritten := range []bool{false, true} {
		name := "before_state_publication"
		if stateWritten {
			name = "after_state_before_journal"
		}
		t.Run(name, func(t *testing.T) {
			root, worker, plan, packet, previousState, previousJournal, targetState, targetJournal := nativeS9OriginalCandidateBoundary(t)
			accepted := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(accepted.State)
			if err != nil || registry == nil {
				t.Fatalf("original accepted Candidate: %v", err)
			}
			candidateRef := registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef
			candidatePath := filepath.Join(root, candidateRef)
			candidateBytes, err := os.ReadFile(candidatePath)
			if err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(root, ".claude/loop-state.json")
			journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
			markerBytes := nativeS9OriginalPendingMarker(t, previousState, targetState, targetJournal)
			var marker map[string]any
			if err := json.Unmarshal(markerBytes, &marker); err != nil {
				t.Fatal(err)
			}
			marker["retain_last_transition"] = true
			markerBytes, err = json.MarshalIndent(marker, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			currentState := previousState
			if stateWritten {
				currentState = targetState
			}
			if err := os.WriteFile(statePath, currentState, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, previousJournal, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath+".commit-pending.json", markerBytes, 0600); err != nil {
				t.Fatal(err)
			}
			typed := filepath.Join(worker, packet.AnswerPath)
			if err := os.Remove(typed); err != nil {
				t.Fatal(err)
			}
			_, relative, err := nativeS9AuthorPaths(root, plan)
			if err != nil {
				t.Fatal(err)
			}
			flat := filepath.Join(worker, relative)
			original, err := os.ReadFile(flat)
			if err != nil {
				t.Fatal(err)
			}
			changed := []byte("later different private original")
			if err := os.WriteFile(flat, changed, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.NewStore(statePath, journalPath).Snapshot(); !errors.Is(err, runtime.ErrPendingRuntimeOperation) {
				t.Fatalf("missing original pending boundary: %v", err)
			}
			var out, diagnostic bytes.Buffer
			// The changed private copy is not a new receipt. The accepted
			// responsibility continues; the accepted Candidate and its receipt
			// are preserved below and are never replaced by the later bytes.
			if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diagnostic); code != 0 {
				t.Fatalf("changed private original broke the accepted responsibility: %d %s %s", code, &out, &diagnostic)
			}
			afterState, stateErr := os.ReadFile(statePath)
			afterJournal, journalErr := os.ReadFile(journalPath)
			if stateErr != nil || journalErr != nil || !bytes.Equal(afterState, targetState) || !bytes.Equal(afterJournal, targetJournal) {
				t.Fatalf("original Writer pair was not recovered: %v %v", stateErr, journalErr)
			}
			if _, err := os.Stat(statePath + ".commit-pending.json"); !os.IsNotExist(err) {
				t.Fatalf("original pending marker not retired: %v", err)
			}
			if _, err := os.Stat(typed); !os.IsNotExist(err) {
				t.Fatalf("recovery fabricated typed input: %v", err)
			}
			if raw, err := os.ReadFile(flat); err != nil || !bytes.Equal(raw, changed) {
				t.Fatalf("recovery changed later private input: %v", err)
			}
			if raw, err := os.ReadFile(candidatePath); err != nil || !bytes.Equal(raw, candidateBytes) {
				t.Fatalf("recovery replaced original Candidate: %v", err)
			}
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packet.ID); err != nil || !receipt.Accepted || len(receipt.DomainRefs) != 1 || receipt.DomainRefs[0] != candidateRef {
				t.Fatalf("original Candidate receipt lost during source drift: %+v %v", receipt, err)
			}
			if err := os.WriteFile(flat, original, 0600); err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			out.Reset()
			diagnostic.Reset()
			// The replay after the pending recovery receives nothing again: the
			// accepted Candidate and its receipt stay the accepted facts.
			if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diagnostic); code != 0 {
				t.Fatalf("replay after recovery failed: %d %s %s", code, &out, &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
			if _, err := os.Stat(typed); !os.IsNotExist(err) {
				t.Fatalf("receipt projection recreated typed input: %v", err)
			}
			if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packet.ID); err != nil || !receipt.Accepted || len(receipt.DomainRefs) != 1 || receipt.DomainRefs[0] != candidateRef {
				t.Fatalf("replay replaced the original Candidate receipt: %+v %v", receipt, err)
			}
		})
	}
}

func TestNativeS9OriginalConsumeMissingTypedDoesNotOverwriteStagedCandidate(t *testing.T) {
	root, worker, p, packet, previousState, previousJournal, _, _ := nativeS9OriginalCandidateBoundary(t)
	accepted := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(accepted.State)
	if err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(root, registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef)
	staged, err := os.ReadFile(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	// Expose the real staged artifact before its original CAS/pending publication.
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	if err := os.WriteFile(statePath, previousState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, previousJournal, 0600); err != nil {
		t.Fatal(err)
	}
	typedPath := filepath.Join(worker, packet.AnswerPath)
	typedBytes, err := os.ReadFile(typedPath)
	if err != nil {
		t.Fatal(err)
	}
	original := repairDispatchSnapshot(t, root)
	capture := newNativeS9AttemptCapture(context.Background(), root, original, p.ID, p.Binding.OwnerAgentID, "Builder")
	capture.SetTypedInput(typedPath)
	capture.SetStage("typed_result_saved")
	capture.ObserveInput(typedPath, typedBytes)
	if err := capture.Publish(context.Background(), errors.New("controlled candidate reception interruption before pending publication")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(typedPath); err != nil {
		t.Fatal(err)
	}
	_, relative, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	flatPath := filepath.Join(worker, relative)
	replacement := []byte("later different flat input")
	if err := os.WriteFile(flatPath, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	var out, diagnostic bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", flatPath}, &out, &diagnostic); code == 0 {
		t.Fatalf("missing original typed input silently replaced from flat: %s %s", &out, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	out.Reset()
	diagnostic.Reset()
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", flatPath}, &out, &diagnostic); code == 0 {
		t.Fatalf("repeated missing typed recovery lost its saved-input window: %s %s", &out, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if after, err := os.ReadFile(candidatePath); err != nil || !bytes.Equal(after, staged) {
		t.Fatalf("staged immutable candidate replaced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worker, packet.AnswerPath)); !os.IsNotExist(err) {
		t.Fatalf("missing typed bytes fabricated from flat: %v", err)
	}
}

func TestNativeS9OriginalStopUsesNewProfessionalInputAfterSourceGap(t *testing.T) {
	root, worker, p, input, professional := nativeS9WorkerFixture(t)
	_, relative, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	professional.EvidenceRefs = []string{"path:logs/original-missing-check.txt"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, relative), professional); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	// L3-S9-repair.md:236 keeps the reception from re-reading evidence files and
	// L4-hook-acceptance-checklist.md:53/:59 removed the evidence-existence
	// gate, so a referenced source that is not present is not a reception
	// qualification. The author's own conclusion is received on its own path.
	if err := receiveNativeS9Builder(root, repairDispatchSnapshot(t, root).State, input, p, input.LastAssistantMessage, &diagnostic); err != nil {
		t.Fatalf("readable author conclusion refused over a missing evidence source: %v %s", err, &diagnostic)
	}
	if _, err := os.Stat(filepath.Join(worker, "logs/original-missing-check.txt")); !os.IsNotExist(err) {
		t.Fatalf("reception fabricated the missing evidence source: %v", err)
	}
	partial := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(partial.State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("received author conclusion has no Candidate: %v", err)
	}
	// This new actual Stop is a new explicit professional save. Under C02 and
	// L3-S9-repair.md:437 it is a revision or a new attempt on the same
	// original responsibility, and it does not silently overwrite the fact that
	// was actually received.
	professional.EvidenceRefs = []string{"internal/api/one.go"}
	professional.ResidualRisks = []string{"new professional observation after restoring the source gap"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, relative), professional); err != nil {
		t.Fatal(err)
	}
	diagnostic.Reset()
	if err := receiveNativeS9Builder(root, partial.State, input, p, input.LastAssistantMessage, &diagnostic); err != nil {
		t.Fatalf("new Stop swallowed by stale unaccepted typed bytes: %v %s", err, &diagnostic)
	}
	accepted := repairDispatchSnapshot(t, root)
	registry, err = workspace.Decode(accepted.State)
	if err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(root, registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef)
	raw, err := os.ReadFile(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	var candidate struct {
		Provenance struct {
			SubmittedAnswer json.RawMessage `json:"submitted_answer"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(raw, &candidate); err != nil {
		t.Fatal(err)
	}
	var actual workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(candidate.Provenance.SubmittedAnswer, &actual); err != nil {
		t.Fatal(err)
	}
	if len(actual.ResidualRisks) != 0 {
		t.Fatalf("a later edit silently overwrote the received fact: %s", candidate.Provenance.SubmittedAnswer)
	}
	if len(actual.EvidenceRefs) != 1 || actual.EvidenceRefs[0] != "path:logs/original-missing-check.txt" {
		t.Fatalf("received fact lost its original content: %s", candidate.Provenance.SubmittedAnswer)
	}
}

func TestNativeS9OriginalConsumeMissingUnadaptedDraftCanAdaptExplicitFlat(t *testing.T) {
	root, worker, p, packet, previousState, previousJournal, _, _ := nativeS9OriginalCandidateBoundary(t)
	accepted := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(accepted.State)
	if err != nil {
		t.Fatal(err)
	}
	// Retain the original pre-adaptation issuance but remove all later candidate
	// facts/artifacts. There is no successful adaptation observation or pending.
	if err := os.Remove(filepath.Join(root, registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), previousState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-events.jsonl"), previousJournal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(worker, packet.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	_, relative, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	var out, diagnostic bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", filepath.Join(worker, relative)}, &out, &diagnostic); code != 0 {
		t.Fatalf("missing unadapted draft prevented original explicit flat adaptation: %d %s %s", code, &out, &diagnostic)
	}
	accepted = repairDispatchSnapshot(t, root)
	registry, err = workspace.Decode(accepted.State)
	if err != nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("explicit original flat did not reach original consumer: %v", err)
	}
}

// Match the original Store commit-pending wire protocol using a real candidate
// state and its original final journal event, as in Store integrity fixtures.
func nativeS9OriginalPendingMarker(t *testing.T, previousStateBytes, targetStateBytes, targetJournalBytes []byte) []byte {
	t.Helper()
	var previousState, targetState map[string]any
	if err := json.Unmarshal(previousStateBytes, &previousState); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(targetStateBytes, &targetState); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(targetJournalBytes)), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		t.Fatal("target journal has no original committed event")
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil {
		t.Fatal(err)
	}
	marker := map[string]any{
		"schema_version": "1.0.0", "previous_revision": previousState["revision"], "request_id": event["request_id"],
		"idempotency_key": event["idempotency_key"], "retain_last_transition": true,
		"state": targetState, "journal_event": event,
	}
	encoded, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func TestNativeS9OriginalConsumeAcceptedPlanSurvivesMissingDisplayBrief(t *testing.T) {
	root, worker, p, _, _ := nativeS9WorkerFixture(t)
	brief := nativeS9BriefPath(p)
	_, relative, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		t.Fatal(err)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, p, service, &diagnostic, nil); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveRepairPlanReportFrom(t.Context(), p.Binding.OwnerAgentID, filepath.Join(root, p.AnswerPath), root, nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Plan acceptance: %#v %v", receipt, err)
	}
	if err := os.Remove(filepath.Join(root, brief)); err != nil {
		t.Fatal(err)
	}
	beforePackets, err := runtime.CurrentRepairPlanReportPackets(root, repairDispatchSnapshot(t, root).State, "")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", filepath.Join(worker, relative)}, &out, &diagnostic); code != 0 {
		t.Fatalf("display loss swallowed original responsibility: %d %s %s", code, &out, &diagnostic)
	}
	accepted := repairDispatchSnapshot(t, root)
	afterPackets, err := runtime.CurrentRepairPlanReportPackets(root, accepted.State, "")
	if err != nil || len(afterPackets) != len(beforePackets) || len(afterPackets) != 1 || afterPackets[0].ID != p.ID {
		t.Fatalf("recovery replaced original Plan responsibility: %#v %v", afterPackets, err)
	}
	registry, err := workspace.Decode(accepted.State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("same original packet was not received: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, brief)); !os.IsNotExist(err) {
		t.Fatalf("consume rebuilt display preparation instead of receiving original input: %v", err)
	}
}

// A later private edit is a new attempt by the author, not a new receipt of
// the accepted responsibility. The system does not detect that drift by
// comparing bytes against a registered digest (L3-S9 §12.1; L4-workflow-
// convergence §281); the accepted fact stays accepted and is not overwritten,
// and missing accepted originals never require a resubmission.
func TestNativeS9AcceptedDRecoveryKeepsAcceptedFactWhenPrivateOriginalsChangeOrVanish(t *testing.T) {
	root, worker, p, _, _ := nativeS9WorkerFixture(t)
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		t.Fatal(err)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(worker, result)
	if _, err := consumeRegisteredStageAnswer(t.Context(), service, path); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	for _, rel := range []string{plan, result} {
		raw, err := os.ReadFile(filepath.Join(worker, rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(worker, rel), append(raw, ' '), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := consumeRegisteredStageAnswer(t.Context(), service, path); err != nil {
			t.Fatalf("changed accepted original broke the accepted responsibility: %v", err)
		}
		repairDispatchAssertUnchanged(t, root, before)
		accepted := repairDispatchSnapshot(t, root)
		registry, err := workspace.Decode(accepted.State)
		if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" {
			t.Fatalf("the changed private original dropped the accepted delivery: %v", err)
		}
		if err := os.WriteFile(filepath.Join(worker, rel), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{plan, result} {
		if err := os.Remove(filepath.Join(worker, rel)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := consumeRegisteredStageAnswer(t.Context(), service, path); err != nil {
		t.Fatalf("accepted missing originals required resubmission: %v", err)
	}
	repairDispatchAssertUnchanged(t, root, before)
	// Missing accepted author copies differ from losing an unverified source.
	// The accepted fact survives the vanished source: the original exit that
	// does not depend on cleanup is not blocked, and no cleanup, ACK or
	// re-receipt state is fabricated (L4-workflow-convergence §"S6／S9 工作区
	// 清理与恢复": preserving resources does not revoke the accepted facts
	// nor block the original stage exit that does not depend on cleanup).
	repairLeadGit(t, root, "worktree", "remove", "--force", worker)
	if _, err := consumeRegisteredStageAnswer(t.Context(), service, path); err != nil {
		t.Fatalf("the vanished source blocked the accepted responsibility: %v", err)
	}
	repairDispatchAssertUnchanged(t, root, before)
}
