package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type nativeS9VerifiedFinalFixture struct {
	root, state, journal string
	request              repair.SubmitIssuedRepairResultRequest
}

// Prepare through the issued Plan and professional Result, then execute actual
// Main inspection, merge and checks. Stop at the original verified checkpoint
// so concurrent calls enter the real Final Writer for the first time.
func nativeS9VerifiedFinal(t *testing.T) nativeS9VerifiedFinalFixture {
	t.Helper()
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	var diagnostics bytes.Buffer
	if err := receiveNativeS9BuilderFrom(context.Background(), root, repairDispatchSnapshot(t, root).State, plan, worker, false, &diagnostics, nil); err != nil {
		t.Fatalf("original professional Result: %v %s", err, &diagnostics)
	}
	snap := repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairResultPackets(root, snap.State, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original issued Result: %d %v", len(packets), err)
	}
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("original work location: %v", err)
	}
	e := registry.Executions[plan.Binding.ManifestAssignmentID]
	e.Branch, err = workspace.Git(context.Background(), e.Path, "branch", "--show-current")
	if err != nil {
		t.Fatal(err)
	}
	a := hookctx.AssignmentContext{AssignmentID: e.AssignmentID, OwnerAgentID: e.AgentID, TaskID: packets[0].Binding.Sources.TaskID, WorktreePath: e.Path, Branch: e.Branch, TargetBranch: e.TargetBranch}
	inspection, err := integration.Inspect(context.Background(), integration.InspectRequest{Root: root, Assignment: a, TargetBranch: e.TargetBranch, BaselineGeneration: e.BaselineGeneration, RuntimeID: e.RuntimeID}, integration.InspectConfig{SkipCompletionCheck: true})
	if err != nil || !inspection.Ready {
		t.Fatalf("actual Main inspection: %+v %v", inspection, err)
	}
	commands := []string{"test -f internal/api/one.go"}
	merged, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: e.RuntimeID, RequiredChecks: commands, CheckRunner: integration.CommandCheckRunner})
	if err != nil || merged.Checkpoint.State != integration.StateVerified {
		t.Fatalf("actual Main integration: %+v %v", merged.Checkpoint, err)
	}
	e.Checks = commands
	checks, err := verifiedDomainChecks(root, e, merged.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := repairResultMainProofContext(context.Background(), root, e, merged.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	return nativeS9VerifiedFinalFixture{root: root, state: filepath.Join(root, ".claude/loop-state.json"), journal: filepath.Join(root, ".claude/loop-events.jsonl"), request: repair.SubmitIssuedRepairResultRequest{ExpectedRevision: snap.Revision, PacketID: packets[0].ID, OwnerAgentID: e.AgentID, Checkpoint: &merged.Checkpoint, MainChecks: checks, MainProof: proof, OccurredAt: time.Now().UTC()}}
}

func TestS9NativeFinalConcurrentFirstAcceptanceAndReplay(t *testing.T) {
	f := nativeS9VerifiedFinal(t)
	type outcome struct {
		ref repair.ArtifactRef
		err error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, ref, err := repair.SubmitIssuedRepairResult(context.Background(), f.root, f.state, f.journal, f.request)
			results <- outcome{ref, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := ""
	for result := range results {
		if result.err != nil {
			if !errors.Is(result.err, integration.ErrIntegrationBusy) && !errors.Is(result.err, runtime.ErrStaleRevision) {
				t.Fatalf("concurrent Final: %v", result.err)
			}
			continue
		}
		if accepted != "" && accepted != result.ref.Path {
			t.Fatalf("two accepted references: %s %s", accepted, result.ref.Path)
		}
		accepted = result.ref.Path
	}
	if accepted == "" {
		t.Fatal("no original Final Writer committed")
	}
	snapshot := repairDispatchSnapshot(t, f.root)
	receipt, found, err := runtime.VerifiedRepairResultReceipt(f.root, snapshot.State, f.request.PacketID, f.request.OwnerAgentID)
	if err != nil || !found || receipt.ResultRef != accepted {
		t.Fatalf("accepted Final: %+v %t %v", receipt, found, err)
	}
	for i := 0; i < 2; i++ {
		_, _, ref, err := repair.SubmitIssuedRepairResult(context.Background(), f.root, f.state, f.journal, f.request)
		if err != nil || ref.Path != accepted {
			t.Fatalf("Final replay %d: %+v %v", i, ref, err)
		}
	}
	assertNativeFinalEventCount(t, f.journal, 1)
}

// G18 §3.2: the Runtime write count is not a gate on this professional act.
// After an unrelated Runtime move the Final must still complete, and the
// protection lives in the domain facts instead: the already accepted Final of
// this packet (replayed), the issued receipt's event identity (journal dedup)
// and the Worker attempt's own version (revalidated inside Apply).
func TestS9NativeFinalOrphanAfterUnrelatedRuntimeMove(t *testing.T) {
	f := nativeS9VerifiedFinal(t)
	beforeState, err := os.ReadFile(f.state)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(f.journal)
	if err != nil {
		t.Fatal(err)
	}
	_, first, firstRef, err := repair.SubmitIssuedRepairResult(context.Background(), f.root, f.state, f.journal, f.request)
	if err != nil {
		t.Fatalf("first actual Final: %v", err)
	}
	firstPath := filepath.Join(f.root, filepath.FromSlash(firstRef.Path))
	orphan, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	// Restore the exact previous Runtime pair to model interruption before its
	// commit. Preserve the already published file as an unregistered occupant.
	if err := os.WriteFile(f.state, beforeState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.journal, beforeJournal, 0600); err != nil {
		t.Fatal(err)
	}
	writer := runtime.NewWriter(f.state, f.journal, f.root, semantic.RuntimeCandidateValidator{})
	advanced, err := writer.UpdateCurrent(runtime.Mutation{EventID: "evt-s9-concurrent-work", TransitionID: "S9-TEST", Event: "s9_concurrent_work", Actor: "Main", IdempotencyKey: "s9-concurrent-work", RuntimeID: receiptRuntimeID(first), BaselineGeneration: first.Receipt.Provenance.BaselineGeneration, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(map[string]any) error { return nil }})
	if err != nil {
		t.Fatalf("actual concurrent Writer event: %v", err)
	}
	if advanced.Revision == f.request.ExpectedRevision {
		t.Fatalf("concurrent Writer did not move the Runtime write count: %d", advanced.Revision)
	}
	// The caller still names the write count it read before the move. The
	// retired gate would have refused this professional act; the domain proof
	// must accept it and record the Final against the current state.
	_, result, ref, err := repair.SubmitIssuedRepairResult(context.Background(), f.root, f.state, f.journal, f.request)
	if err != nil {
		t.Fatalf("professional Final refused after unrelated Runtime move: %v", err)
	}
	if ref.Path == firstRef.Path || result.Receipt == nil || result.Receipt.ResultRef != ref.Path || result.ResultID != first.ResultID {
		t.Fatalf("post-move Final did not record new actual reference: first=%+v final=%+v", firstRef, ref)
	}
	got, err := os.ReadFile(firstPath)
	if err != nil || !bytes.Equal(got, orphan) {
		t.Fatalf("unregistered file changed: %v", err)
	}
	snapshot := repairDispatchSnapshot(t, f.root)
	receipt, found, err := runtime.VerifiedRepairResultReceipt(f.root, snapshot.State, f.request.PacketID, f.request.OwnerAgentID)
	if err != nil || !found || receipt.ResultRef != ref.Path {
		t.Fatalf("post-move Final not recorded: %+v %t %v", receipt, found, err)
	}
	// The replay stays idempotent even while the caller still names the write
	// count it read before the move: the professional act is delivered once.
	_, _, replay, err := repair.SubmitIssuedRepairResult(context.Background(), f.root, f.state, f.journal, f.request)
	if err != nil || replay.Path != ref.Path {
		t.Fatalf("registered post-move replay: %+v %v", replay, err)
	}
	assertNativeFinalEventCount(t, f.journal, 1)
}

func receiptRuntimeID(result repair.RepairResult) string { return result.Receipt.Provenance.RuntimeID }
func assertNativeFinalEventCount(t *testing.T, path string, expected int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event["transition_id"] == "S9-RESULT-SUBMIT" {
			count++
		}
	}
	if count != expected {
		t.Fatalf("Final Writer committed %d events, want %d", count, expected)
	}
}

func TestS9NativeFinalSealedPendingRecoveryBeforeCleanup(t *testing.T) {
	f := nativeS9VerifiedFinal(t)
	beforeState, err := os.ReadFile(f.state)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(f.journal)
	if err != nil {
		t.Fatal(err)
	}
	_, result, ref, err := repair.SubmitIssuedRepairResult(context.Background(), f.root, f.state, f.journal, f.request)
	if err != nil || result.Receipt == nil {
		t.Fatalf("actual Final: %+v %v", result.Receipt, err)
	}
	afterState, err := os.ReadFile(f.state)
	if err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(f.journal)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(f.root, filepath.FromSlash(ref.Path))
	resultBytes, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	marker := nativeS9OriginalPendingMarker(t, beforeState, afterState, afterJournal)
	if err := os.WriteFile(f.state, beforeState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.journal, beforeJournal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.state+".commit-pending.json", marker, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.NewStore(f.state, f.journal).Snapshot(); !errors.Is(err, runtime.ErrPendingRuntimeOperation) {
		t.Fatalf("read-only pending observation: %v", err)
	}
	recovered, err := runtime.NewWriter(f.state, f.journal, f.root, semantic.RuntimeCandidateValidator{}).Snapshot()
	if err != nil {
		t.Fatalf("original Writer recovery: %v", err)
	}
	accepted, found, err := runtime.VerifiedRepairResultReceipt(f.root, recovered.State, f.request.PacketID, f.request.OwnerAgentID)
	if err != nil || !found || accepted.ResultRef != ref.Path {
		t.Fatalf("recovered original Final: %+v %t %v", accepted, found, err)
	}
	if got, err := os.ReadFile(resultPath); err != nil || !bytes.Equal(got, resultBytes) {
		t.Fatalf("recovery changed original Final: %v", err)
	}
	if _, err := os.Stat(f.state + ".commit-pending.json"); !os.IsNotExist(err) {
		t.Fatalf("pending marker was not retired: %v", err)
	}
	assertNativeFinalEventCount(t, f.journal, 1)
}

func TestS9NativeCandidateConcurrentAcceptanceAndReplay(t *testing.T) {
	root, worker, plan, packet, beforeState, beforeJournal, acceptedState, _ := nativeS9OriginalCandidateBoundary(t)
	var originalState map[string]any
	if err := json.Unmarshal(acceptedState, &originalState); err != nil {
		t.Fatal(err)
	}
	registry, err := workspace.Decode(originalState)
	if err != nil || registry == nil {
		t.Fatalf("accepted original execution: %v", err)
	}
	oldRef := registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef
	oldPath := filepath.Join(root, filepath.FromSlash(oldRef))
	original, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if err := os.WriteFile(statePath, beforeState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, beforeJournal, 0600); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		receipt submission.Receipt
		err     error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			service, err := submission.New(root)
			if err != nil {
				results <- outcome{err: err}
				return
			}
			receipt, err := service.SaveRepairResultFrom(context.Background(), plan.Binding.OwnerAgentID, filepath.Join(worker, packet.AnswerPath), worker, nil)
			results <- outcome{receipt, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for result := range results {
		if result.err == nil && result.receipt.Accepted {
			accepted++
			continue
		}
		if !errors.Is(result.err, integration.ErrIntegrationBusy) && !errors.Is(result.err, runtime.ErrStaleRevision) {
			t.Fatalf("concurrent candidate: %+v %v", result.receipt, result.err)
		}
	}
	if accepted == 0 {
		t.Fatal("neither current receiver accepted candidate")
	}
	snapshot := repairDispatchSnapshot(t, root)
	receipt, found, err := runtime.VerifiedRepairResultCandidateReceipt(root, snapshot.State, packet.ID, plan.Binding.OwnerAgentID)
	if err != nil || !found || receipt.CandidateRef == "" || receipt.CandidateRef == oldRef {
		t.Fatalf("actual candidate Writer: %+v %t %v", receipt, found, err)
	}
	if got, err := os.ReadFile(oldPath); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("unregistered candidate overwritten: %v", err)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.SaveRepairResultFrom(context.Background(), plan.Binding.OwnerAgentID, filepath.Join(worker, packet.AnswerPath), worker, nil)
	if err != nil || !replay.Accepted {
		t.Fatalf("candidate replay: %+v %v", replay, err)
	}
	again, found, err := runtime.VerifiedRepairResultCandidateReceipt(root, repairDispatchSnapshot(t, root).State, packet.ID, plan.Binding.OwnerAgentID)
	if err != nil || !found || again.CandidateRef != receipt.CandidateRef {
		t.Fatalf("candidate replay changed original reference: %+v %v", again, err)
	}
}
