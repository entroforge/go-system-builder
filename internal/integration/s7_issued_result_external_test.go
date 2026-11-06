package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
)

// This is a complete original S5 -> TR-003 -> S6 Main -> TR-006 -> S7
// RegisterPlan -> reviewer Register -> packet issue/submit chain.
// The S5 evidence and the answer strings below are mechanism fixtures: they
// prove the original writers, guards, receipts, and CleanRound consumer run;
// they do not claim that a real model performed the professional review.
func TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound(t *testing.T) {
	runS7IssuedResultPacketChain(t, false)
}

func TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound(t *testing.T) {
	runS7IssuedResultPacketChain(t, true)
}

// An unrelated sibling is neither owned nor a reason to refuse this answer.
func TestS7IssuedResultPreservesUnissuedSibling(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003)
	registered := registerCanonicalPlanForPacketTest(t, fixture)
	plan, _, err := review.LoadPlan(fixture.root, registered.State)
	if err != nil {
		t.Fatal(err)
	}
	owners := dispatchS7ReviewerAssignments(t, fixture, plan)
	packets, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, "")
	if err != nil || len(packets) == 0 {
		t.Fatalf("issue original packet: %d %v", len(packets), err)
	}
	packet := packets[0]
	owner := owners[packet.Binding.AssignmentID]
	answer := filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath))
	writeJSONFixture(t, answer, s7MechanismAnswer(t, fixture, packet))
	sibling := filepath.Join(filepath.Dir(answer), "extra.json")
	contents := []byte(`{"unissued":true}`)
	if err := os.WriteFile(sibling, contents, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, review.IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owner, SourceRoot: fixture.root})
	if err != nil {
		t.Fatalf("original reviewer answer was blocked: %v", err)
	}
	if _, found, err := review.IssuedResultReceipt(fixture.root, accepted.State, packet.ID, owner); err != nil || !found {
		t.Fatalf("original receipt missing: %v", err)
	}
	if got, err := os.ReadFile(sibling); err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("unissued sibling changed: %q %v", got, err)
	}
}

func runS7IssuedResultPacketChain(t *testing.T, coldHooks bool) {
	t.Helper()
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003)
	if coldHooks {
		fixture.hookBinary = factory.BuildHarness(t)
	}
	registered := registerCanonicalPlanForPacketTest(t, fixture)
	plan, _, err := review.LoadPlan(fixture.root, registered.State)
	if err != nil {
		t.Fatal(err)
	}
	owners := dispatchS7ReviewerAssignments(t, fixture, plan, "static")
	packets, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, "")
	if err != nil || len(packets) != len(owners) || len(packets) == 0 {
		t.Fatalf("original static packets: %d, owners %d, %v", len(packets), len(owners), err)
	}
	staticCount := len(packets)
	var first workpacket.ReviewResultPacket
	var firstOwner string
	for i := 0; i < len(packets); i++ {
		packet := packets[i]
		owner := owners[packet.Binding.AssignmentID]
		if owner == "" || packet.Binding.OwnerAgentID != owner {
			t.Fatalf("packet has wrong original owner: %+v", packet.Binding)
		}
		if i == 0 {
			first, firstOwner = packet, owner
			assertS7IssuedNegativeCases(t, fixture, packet, owner, readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath).Revision)
		}
		writeJSONFixture(t, filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath)), s7MechanismAnswer(t, fixture, packet))
		accepted, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, review.IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owner, SourceRoot: fixture.root})
		if err != nil {
			t.Fatalf("original Reviewer answer: %v", err)
		}
		if receipt, found, err := review.IssuedResultReceipt(fixture.root, accepted.State, packet.ID, owner); err != nil || !found || receipt.PacketID != packet.ID || receipt.ProducerAgentID != owner {
			t.Fatalf("original receipt missing: %+v %v", receipt, err)
		}
		if i+1 == staticCount {
			behavior := dispatchS7ReviewerAssignments(t, fixture, plan, "behavior")
			for assignment, owner := range behavior {
				owners[assignment] = owner
			}
			if len(behavior) > 0 {
				issued, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, "")
				if err != nil || len(issued) != len(behavior) {
					t.Fatalf("behavior issuance: %d %v", len(issued), err)
				}
				packets = append(packets, issued...)
			}
		}
	}
	if len(packets) != len(plan.Assignments) {
		t.Fatalf("issued %d for %d responsibilities", len(packets), len(plan.Assignments))
	}
	final := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if ptr := review.PlanPointerFromState(final.State); ptr == nil || ptr.Status != "clean" {
		t.Fatalf("actual conclusions did not reach clean: %+v", ptr)
	}
	original, found, err := review.IssuedResultReceipt(fixture.root, final.State, first.ID, firstOwner)
	if err != nil || !found {
		t.Fatalf("original receipt unavailable: %v", err)
	}
	promoted := runS7StopHookMode(t, fixture, first, firstOwner, coldHooks)
	if life := promoted.State["lifecycle"].(map[string]any); life["state"] != "acceptance" {
		t.Fatalf("original Hook did not reach S10: %+v", life)
	}
	retained, found, err := review.IssuedResultReceipt(fixture.root, promoted.State, first.ID, firstOwner)
	if err != nil || !found || !reflect.DeepEqual(original, retained) {
		t.Fatalf("original receipt changed at handoff: %+v %v", retained, err)
	}
	replayed := runS7StopHookMode(t, fixture, first, firstOwner, coldHooks)
	if replayed.Revision != promoted.Revision {
		t.Fatalf("replay changed Runtime: %d -> %d", promoted.Revision, replayed.Revision)
	}
	// The reverse case in the same chain: an actor that is not the registered
	// Reviewer states the exact registered answer path and carries no sidechain
	// original task. It must not bind the responsibility, must not leak that
	// answer path or its owner into any channel, and must not move Runtime.
	// L4-workflow-convergence.md:191 — the actor's own final statement cannot by
	// itself make the Hook receive for another author; :189 — the person's event
	// selects from the same sidechain's real original task.
	beforeUnbound := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	code, unboundOut, unboundDiagnostic := invokeS7StopHookWithoutOwnTask(t, fixture, first, firstOwner, coldHooks)
	if code != 0 {
		t.Fatalf("S7 Stop without an original task was blocked: %d %s %s", code, unboundOut, unboundDiagnostic)
	}
	unboundAnswer := filepath.Join(fixture.root, filepath.FromSlash(first.AnswerPath))
	for _, forbidden := range []string{"received by the original consumer", unboundAnswer, firstOwner} {
		if strings.Contains(unboundOut+unboundDiagnostic, forbidden) {
			t.Fatalf("S7 Stop without an original task bound the responsibility (%q): %s %s", forbidden, unboundOut, unboundDiagnostic)
		}
	}
	if after := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath); after.Revision != beforeUnbound.Revision {
		t.Fatalf("S7 Stop without an original task changed Runtime: %d -> %d", beforeUnbound.Revision, after.Revision)
	}
}

func enablePacketModeAndApplyTR003(t *testing.T, root, statePath, journalPath string) {
	t.Helper()
	// Reviewer source assets are part of the physical Main fixture from its
	// original TR-003 baseline onward. This keeps later registration from
	// looking like product drift while still using the normal installed asset
	// authority for the dispatch records.
	for _, role := range []string{"delivery-verifier", "document-verifier", "e2e-tester", "qa", "test-builder"} {
		copyFixtureFile(t, filepath.Join(sourceRepositoryRoot(t), "agents", role+".md"), filepath.Join(root, ".claude", "agents", role+".md"))
	}
	gitFixture(t, root, "add", ".claude/agents")
	gitFixture(t, root, "commit", "-m", "Install S7 reviewer role sources before TR-003")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	configuration, _ := state["configuration"].(map[string]any)
	if configuration == nil {
		configuration = map[string]any{}
		state["configuration"] = configuration
	}
	configuration["optimization"] = map[string]any{"packet": true}
	writeJSONFixture(t, statePath, state)
	seedCanonicalS5AndApplyTR003(t, root, statePath, journalPath)
}

func registerCanonicalPlanForPacketTest(t *testing.T, fixture *canonicalMainFixture) loopruntime.Snapshot {
	t.Helper()
	ctx := context.Background()
	inspection := fixture.inspect(t, "")
	if !inspection.Ready {
		t.Fatalf("original Main Inspect before S7 = blockers %v", inspection.Blockers)
	}
	if integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig()); err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("original Main Integrate before S7 = state %q err %v", integrated.Checkpoint.State, err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	catalogue, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := fileview.New(fixture.root, "refs/heads/main", catalogue.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(ctx, qualitygate.Input{
		Root: fixture.root, Snapshot: current, Files: files, GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006",
	})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("original TR-006 Gate: status=%q missing=%v conflicts=%v err=%v", gate.Status, gate.Missing, gate.Conflicts, err)
	}
	started, err := transition.Apply(fixture.root, fixture.statePath, fixture.journalPath, transition.Request{
		Files: files, TransitionID: "TR-006", ExpectedRevision: current.Revision,
		ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator",
		Evidence: map[string]string{"builder_report_record": canonicalEvidenceIDForPath(t, current.State, fixture.result.ResultRef)},
		GateID:   gate.GateID, ProducerResponsibility: "delivery_orchestrator",
	})
	if err != nil {
		t.Fatalf("original TR-006 Writer: %v", err)
	}
	draft, notes := review.DraftPlanForRoot(fixture.root, started.State)
	if draft == nil {
		t.Fatalf("original ReviewPlan draft unavailable: notes=%v", notes)
	}
	// This fixture declares the actual command-visible product marker scenario;
	// it supplies no browser inventory and does not claim real model review.
	authorCanonicalMechanismBehavior(draft, "e2e-workspace/s7-packet-fixture")
	for i := range draft.Claims {
		claim := &draft.Claims[i]
		if !strings.Contains(claim.Oracle, "TODO(planner)") {
			continue
		}
		if claim.Lens == "delivery" {
			claim.Oracle = fmt.Sprintf("Trace the locked %s acceptance obligations through the accepted implementation and verified Main change at %s.", canonicalTaskID, canonicalSourceFile)
		} else {
			claim.Oracle = fmt.Sprintf("Inspect the frozen Main change at %s against this Claim and record the concrete supporting or refuting behavior.", canonicalSourceFile)
		}
	}
	planPath := filepath.Join(fixture.root, ".claude", "evidence", "review-plan-s7-packet-fixture.json")
	writeJSONFixture(t, planPath, draft)
	registered, err := review.RegisterPlan(fixture.root, fixture.statePath, fixture.journalPath, review.PlanRequest{
		ExpectedRevision: started.Revision, PlanPath: planPath, OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("original S7 RegisterPlan: %v", err)
	}
	return registered
}

func dispatchS7ReviewerAssignments(t *testing.T, fixture *canonicalMainFixture, plan *review.Plan, waves ...string) map[string]string {
	t.Helper()
	wave := "static"
	if len(waves) != 0 {
		wave = waves[0]
	}
	owners := make(map[string]string, len(plan.Assignments))
	for _, planned := range plan.Assignments {
		if planned.ExecutionWave != wave {
			continue
		}
		manifest, _, err := review.DraftManifest(fixture.root, readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath).State, planned.AssignmentID)
		if err != nil {
			t.Fatalf("original reviewer manifest draft for %s: %v", planned.AssignmentID, err)
		}
		owner := "agent-s7-packet-" + strings.TrimPrefix(planned.AssignmentID, "assignment-")
		owners[planned.AssignmentID] = owner
		manifest.Assignments[0].AgentID = owner
		manifestPath := filepath.Join(fixture.root, ".claude", "workgroups", "manifest-s7-packet-"+strings.TrimPrefix(planned.AssignmentID, "assignment-")+".json")
		writeJSONFixture(t, manifestPath, manifest)

		definition := manifest.Assignments[0].AgentDefinitionRef
		definitionBase := filepath.Base(definition)
		manifest.Assignments[0].AgentDefinitionRef = filepath.ToSlash(filepath.Join(".claude", "agents", definitionBase))

		before := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
		registered, err := assignment.Register(fixture.root, fixture.statePath, fixture.journalPath, assignment.Request{
			ExpectedRevision: before.Revision, ManifestPath: manifestPath,
			TaskID: canonicalTaskID, TaskPath: filepath.Join(fixture.root, filepath.FromSlash(canonicalTask)),
			OccurredAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("original reviewer workgroup Register for %s: %v", planned.AssignmentID, err)
		}
		row := registered.State["review"].(map[string]any)["assignments"].(map[string]any)[planned.AssignmentID].(map[string]any)
		if row["status"] != "dispatched" || row["agent_id"] != owner {
			t.Fatalf("original workgroup did not dispatch exact Plan Assignment %s: %#v", planned.AssignmentID, row)
		}
	}
	return owners
}

func assertS7IssuedNegativeCases(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string, expectedRevision int) {
	t.Helper()
	valid := s7MechanismAnswer(t, fixture, packet)
	answerPath := filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath))
	writeJSONFixture(t, answerPath, valid)
	otherRoot := t.TempDir()
	for _, tc := range []struct {
		name    string
		request review.IssuedSubmitRequest
	}{
		{name: "wrong owner", request: review.IssuedSubmitRequest{ExpectedRevision: expectedRevision, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: "agent-s7-packet-wrong", SourceRoot: fixture.root}},
		{name: "wrong source checkout", request: review.IssuedSubmitRequest{ExpectedRevision: expectedRevision, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owner, SourceRoot: otherRoot}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, tc.request); err == nil {
				t.Fatal("invalid identity/source reached the original Result Writer")
			}
			if got := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath).Revision; got != expectedRevision {
				t.Fatalf("rejected %s changed Runtime revision to %d", tc.name, got)
			}
		})
	}

	// The old untyped direct submit API still routes to the original consumer,
	// which rejects a direct Result while this Assignment has an issued packet.
	var professional map[string]any
	if err := json.Unmarshal(valid.Answer, &professional); err != nil {
		t.Fatal(err)
	}
	direct := map[string]any{
		"schema_version": "1.0.0", "result_id": packet.ResultID, "assignment_id": packet.Binding.AssignmentID,
		"assignment_revision": packet.Binding.ReviewPlanRevision, "review_plan_id": packet.Binding.ReviewPlanID,
		"baseline_generation": packet.Binding.BaselineGeneration,
		"producer_agent_id":   owner,
		"claim_results":       professional["claim_results"],
		"blocked_claims":      professional["blocked_claims"], "checks": professional["checks"],
		"findings": professional["findings"], "deviations": professional["deviations"],
		"site_lost": professional["site_lost"], "verdict": professional["verdict"],
	}
	directPath := filepath.Join(fixture.root, ".claude", "evidence", "direct-s7-bypass.json")
	writeJSONFixture(t, directPath, direct)
	if _, err := review.SubmitResult(fixture.root, fixture.statePath, fixture.journalPath, review.SubmitRequest{
		ExpectedRevision: expectedRevision, AssignmentID: packet.Binding.AssignmentID, ResultPath: directPath,
	}); err == nil {
		t.Fatal("direct SubmitResult bypassed its active frozen S7 packet")
	}
	if got := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath).Revision; got != expectedRevision {
		t.Fatalf("direct-submit bypass attempt changed Runtime revision to %d", got)
	}
	writeJSONFixture(t, answerPath, valid)
}

func s7MechanismAnswer(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket) workpacket.ReviewResultDraft {
	t.Helper()
	var answer map[string]any
	if err := json.Unmarshal(packet.Draft.Answer, &answer); err != nil {
		t.Fatal(err)
	}
	claimRows, _ := answer["claim_results"].([]any)
	if len(claimRows) != len(packet.Binding.ClaimIDs) {
		t.Fatalf("draft has %d claim answers for %d frozen Claims", len(claimRows), len(packet.Binding.ClaimIDs))
	}
	evidenceRel := filepath.ToSlash(filepath.Join(".claude", "evidence", "s7-mechanism", packet.Binding.AssignmentID+".txt"))
	evidenceBytes := []byte("mechanism fixture only; this source exercises the original S7 receipt and CleanRound machinery, not a real professional review\n")
	writeFixtureFile(t, fixture.root, evidenceRel, evidenceBytes)
	evidenceRef := "path:" + evidenceRel
	for _, raw := range claimRows {
		row, _ := raw.(map[string]any)
		row["conclusion"] = "pass"
		row["observed"] = "mechanism fixture observed the registered Claim/receipt path; not a product-review assertion"
		row["evidence_refs"] = []string{evidenceRef}
	}
	answer["verdict"] = "pass"
	answerBytes, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	valid := packet.Draft
	valid.Answer = answerBytes
	return valid
}

func runS7StopHook(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string) loopruntime.Snapshot {
	return runS7StopHookMode(t, fixture, packet, owner, false)
}

func runS7StopHookMode(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string, cold bool) loopruntime.Snapshot {
	t.Helper()
	code, stdoutText, stderrText := invokeS7StopHookMode(t, fixture, packet, owner, cold)
	if code != 0 {
		t.Fatalf("original S7 SubagentStop hook exit=%d stdout=%s stderr=%s", code, stdoutText, stderrText)
	}
	snapshot, snapshotErr := loopruntime.NewStore(fixture.statePath, fixture.journalPath).WithContext(t.Context()).Snapshot()
	if snapshotErr != nil {
		detail := map[string]any{"cold_process": cold, "packet_id": packet.ID, "owner": owner, "snapshot_error": snapshotErr.Error(), "exit_code": code, "stdout": stdoutText, "stderr": stderrText}
		for name, path := range map[string]string{"pending": fixture.statePath + ".commit-pending.json", "events": fixture.journalPath, "state": fixture.statePath} {
			file, err := os.Open(path)
			if err != nil {
				detail[name+"_error"] = err.Error()
				continue
			}
			info, statErr := file.Stat()
			data, readErr := io.ReadAll(io.LimitReader(file, 4<<20))
			tail := data
			if statErr == nil {
				detail[name+"_size"] = info.Size()
				detail[name+"_artifact_prefix_truncated"] = info.Size() > int64(len(data))
				size := min(info.Size(), int64(8192))
				tail = make([]byte, int(size))
				if _, err := file.ReadAt(tail, info.Size()-size); err != nil && err != io.EOF {
					detail[name+"_tail_error"] = err.Error()
				}
			} else {
				detail[name+"_stat_error"] = statErr.Error()
			}
			_ = file.Close()
			if readErr != nil {
				detail[name+"_error"] = readErr.Error()
			}
			if directory := os.Getenv("GSB_S7_FAILURE_DIR"); directory != "" {
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Logf("failure evidence directory: %v", err)
				} else {
					_ = os.WriteFile(filepath.Join(directory, name+".json"), data, 0600)
				}
			}
			if len(tail) > 8192 {
				tail = tail[len(tail)-8192:]
			}
			detail[name+"_tail"] = string(tail)
		}
		encoded, _ := json.MarshalIndent(detail, "", "  ")
		if directory := os.Getenv("GSB_S7_FAILURE_DIR"); directory != "" {
			_ = os.WriteFile(filepath.Join(directory, "hook-failure.json"), encoded, 0600)
		}
		t.Fatalf("original public S7 handoff left an unreadable durable pair; do not hide it by automatic test recovery: %s", encoded)
	}
	if _, found, err := review.IssuedResultReceipt(fixture.root, snapshot.State, packet.ID, owner); err != nil || !found {
		t.Fatalf("original hook did not persist an exact S7 receipt for %s: found=%t err=%v stdout=%s stderr=%s", packet.ID, found, err, stdoutText, stderrText)
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] == "verification" && !strings.Contains(stderrText, "original consumer") {
		t.Fatalf("active S7 Stop did not report original reception: stdout=%s stderr=%s", stdoutText, stderrText)
	}
	// After TR-009, a late Reviewer Stop only projects the new global stage.
	// The exact durable receipt above, not a repeated reception phrase, proves
	// the old work was accepted. Callers separately verify replay has no writes.
	return snapshot
}

func invokeS7StopHook(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string) (int, string, string) {
	return invokeS7StopHookMode(t, fixture, packet, owner, false)
}

// s7ReviewerOriginalDispatch writes the platform sidechain transcript of the
// original Reviewer. Its first user row is the real first dispatch the platform
// handed to this sidechain, naming the registered answer path where the
// Reviewer must save its own professional Result.
//
// The official SubagentStop payload carries the actor's own sidechain
// transcript, and the original responsibility is selected from that
// sidechain's real original task.
// L4-workflow-convergence.md:189 — "本人事件从同一平台子会话的真实原任务选择";
// :185 — "原 Reviewer 的真实首派发可直接引用已发行原 view／answer".
func s7ReviewerOriginalDispatch(t *testing.T, root, agentID, answerPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "subagents", "agent-"+agentID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	row, _ := json.Marshal(map[string]any{
		"type": "user", "isSidechain": true, "agentId": agentID,
		"message": map[string]any{"role": "user", "content": "Review the assigned S7 plan and save your professional Result at " + answerPath},
	})
	if err := os.WriteFile(path, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func invokeS7StopHookMode(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string, cold bool) (int, string, string) {
	t.Helper()
	answerPath := filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath))
	// This is the Reviewer's own natural Stop: the actor is the registered
	// Reviewer, and the payload carries that sidechain's original dispatch.
	// The original consumer binds the responsibility from that original task.
	payload, err := json.Marshal(map[string]any{
		"hook_event_name":        "SubagentStop",
		"last_assistant_message": answerPath,
		"agent_id":               owner,
		"agent_type":             "reviewer",
		"agent_transcript_path":  s7ReviewerOriginalDispatch(t, fixture.root, owner, answerPath),
		"cwd":                    filepath.Join(fixture.root, "not-main"), "session_id": "unrelated-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	return runS7StopPayload(t, fixture, payload, cold)
}

// invokeS7StopHookWithoutOwnTask is the reverse case: an actor that is not the
// registered Reviewer states the exact registered answer path as its final
// message and carries no sidechain original task. That statement names another
// author's output, so it must not make the Hook receive for that author.
// L4-workflow-convergence.md:191 — "消息、工具输入／输出或本人末次陈述中即使含
// 另一责任准确路径，也不能单独改变此子会话的 owner，或促使 Hook 代另一作者接收".
func invokeS7StopHookWithoutOwnTask(t *testing.T, fixture *canonicalMainFixture, packet workpacket.ReviewResultPacket, owner string, cold bool) (int, string, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"hook_event_name":        "SubagentStop",
		"last_assistant_message": filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath)),
		"agent_id":               "unrelated-hook-actor",
		"cwd":                    filepath.Join(fixture.root, "not-main"), "session_id": "unrelated-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	return runS7StopPayload(t, fixture, payload, cold)
}

func runS7StopPayload(t *testing.T, fixture *canonicalMainFixture, payload []byte, cold bool) (int, string, string) {
	t.Helper()
	if cold {
		return factory.RunHook(t, fixture.hookBinary, fixture.root, "SubagentStop", payload)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "SubagentStop"}, bytes.NewReader(payload), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// parseS7HookReceipt retains the legacy wire-schema compatibility fixture.
// Native SubagentStop reports reception through the original durable receipt.
func parseS7HookReceipt(t *testing.T, stdout string) (bool, string) {
	t.Helper()
	var envelope struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode PostToolUse hook envelope: %v (%s)", err, stdout)
	}
	const prefix = "S7 SUBMISSION RECEIPT: "
	context := envelope.HookSpecificOutput.AdditionalContext
	if !strings.Contains(context, prefix) {
		t.Fatalf("PostToolUse hook has no S7 receipt context: %s", stdout)
	}
	start := strings.Index(context, prefix) + len(prefix)
	receiptBytes := []byte(strings.TrimSpace(context[start:]))
	if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", receiptBytes); err != nil {
		t.Fatalf("public S7 receipt differs from the shared schema: %v\n%s", err, receiptBytes)
	}
	var receipt struct {
		Accepted bool   `json:"accepted"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatalf("decode S7 hook receipt: %v (%s)", err, context[start:])
	}
	return receipt.Accepted, receipt.Error
}

func TestParseS7HookReceiptDecodesBothJSONLayers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accepted bool
		errText  string
	}{
		{name: "accepted", accepted: true},
		{name: "conflict", accepted: false, errText: "submission key conflicts with the originally accepted professional answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := submission.Receipt{
				PacketID: "packet-" + strings.Repeat("a", 64), Accepted: tc.accepted, Error: tc.errText,
				ResponsibilityStatus: "not_accepted", CurrentStage: "S7", NextAction: "Review the original receipt.",
				DomainRefs: []string{}, WaitingOn: []submission.Waiting{}, FieldErrors: []workpacket.FieldError{},
			}
			if tc.accepted {
				value.ResponsibilityStatus, value.WorkComplete = "accepted", true
				value.SubmissionKey = "review-result-" + strings.Repeat("a", 64)
				value.DomainRefs = []string{"review-result.json"}
			}
			receiptBytes, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			outerBytes, err := json.Marshal(map[string]any{
				"hookSpecificOutput": map[string]any{
					"additionalContext": "S7 SUBMISSION RECEIPT: " + string(receiptBytes),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			accepted, gotError := parseS7HookReceipt(t, string(outerBytes))
			if accepted != tc.accepted || gotError != tc.errText {
				t.Fatalf("parsed S7 receipt accepted=%t error=%q, want accepted=%t error=%q", accepted, gotError, tc.accepted, tc.errText)
			}
		})
	}
}

func authorCanonicalMechanismBehavior(plan *review.Plan, workspace string) {
	plan.E2ECoverageState = "cold_start"
	plan.VerificationArtifactWorkspace = &workspace
	for i := range plan.Assignments {
		if plan.Assignments[i].Lens == "e2e" {
			plan.Assignments[i].BehaviorSurface = "non_browser"
		}
	}
	for i := range plan.Claims {
		claim := &plan.Claims[i]
		if claim.Lens != "e2e" {
			continue
		}
		claim.Target = canonicalSourceFile + " through the non-browser marker-check command"
		claim.Assertion = "The accepted Main product exposes the exact main-check-marker line to a read-only command without changing the file"
		claim.Oracle = "grep -qx 'main-check-marker' " + canonicalSourceFile + " exits 0 for the accepted product marker; a missing or altered marker exits nonzero, and the product bytes remain unchanged"
		claim.Method = "Run the declared grep command against the frozen Main product checkout; record its actual exit status and compare the file bytes before and after. No browser execution or model review is inferred by this mechanism fixture."
	}
}
