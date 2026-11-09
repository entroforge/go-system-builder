package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
)

// The platform Hook is an ordinary-built process with its own watchdog. The
// test process can independently run with -race without changing that budget.
func s9BlockerPublicHook(t *testing.T, binary, root string, input policy.Input) (string, string) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic := factory.RunHook(t, binary, root, input.Event, raw)
	if code != 0 {
		t.Fatalf("public %s: %d %s %s", input.Event, code, out, diagnostic)
	}
	if input.Event == "PreToolUse" || input.Event == "PostToolUse" {
		buffer := bytes.NewBufferString(out)
		nativeS9ListenerFeedback(t, buffer)
	}
	return out, diagnostic
}

func TestNativeS9BlockerAndResolutionSurviveUnknownEvidenceRoot(t *testing.T) {
	binary := factory.BuildHarness(t)
	root, worker, plan, input, answer := nativeS9WorkerFixture(t)
	resultPath := nativeS9TestResultPath(t, root, plan)
	answer.Verdict, answer.ResidualRisks = "blocked", []string{"dependent service is temporarily unavailable"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, resultPath), answer); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(root, ".claude", "evidence", "s9")
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupied, []byte("unknown evidence root"), 0600); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	publicBlockerOut, publicBlockerDiagnostic := s9BlockerPublicHook(t, binary, root, input)
	snapshot := repairDispatchSnapshot(t, root)
	packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), root), snapshot.State)
	if err != nil || len(packets) != 1 {
		t.Fatalf("current issued result: %v", err)
	}
	blocked, found, err := runtime.VerifiedRepairResultBlockerReceipt(root, snapshot.State, packets[0].ID, plan.Binding.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("accepted blocker: %+v %v; hook out=%s diagnostic=%s evidence=%v", blocked, err, publicBlockerOut, publicBlockerDiagnostic, snapshot.State["evidence"])
	}
	path, err := workpacket.RepairResultResolutionAuthoringPath(packets[0])
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	readRepairLeadJSON(t, filepath.Join(root, path), &draft)
	mapFieldCLI(draft, "s9_resolution")["rationale"] = "The dependency is available; continue the same responsibility."
	if err := persistRepairLeadQuestionnaire(filepath.Join(root, path), draft); err != nil {
		t.Fatal(err)
	}
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
	before := repairDispatchRuntimeBytes(t, root)
	failedSave := repairLeadSaveInput(root, filepath.Join(root, path), "Main")
	failedSave.ToolResponse = map[string]any{"is_error": true}
	s9BlockerPublicHook(t, binary, root, failedSave)
	repairDispatchAssertUnchanged(t, root, before)
	publicOut, publicDiagnostic := s9BlockerPublicHook(t, binary, root, repairLeadSaveInput(root, filepath.Join(root, path), "Main"))
	afterHook := repairDispatchSnapshot(t, root)
	resolved, found, err := runtime.VerifiedRepairResultResolutionReceipt(root, afterHook.State, packets[0].ID, plan.Binding.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("public Hook original resolution: %+v %v; %s %s", resolved, err, publicOut, publicDiagnostic)
	}
	replay, err := service.SaveRepairResultResolutionFrom(t.Context(), filepath.Join(root, path), nil)
	if err != nil || !replay.Accepted {
		t.Fatalf("original resolution replay: %+v %v", replay, err)
	}
	if got, err := os.ReadFile(occupied); err != nil || string(got) != "unknown evidence root" {
		t.Fatalf("unknown evidence root changed: %q %v", got, err)
	}
}

func TestNativeS9ResumedSecondBlockerUsesCurrentResponsibility(t *testing.T) {
	t.Run("public-hook", func(t *testing.T) {
		runS9ResumedSecondBlocker(t, factory.BuildHarness(t), false)
	})
	t.Run("in-process-writer", func(t *testing.T) {
		runS9ResumedSecondBlocker(t, "", true)
	})
}

// Each path starts with its own original fixture and accepts the second
// blocker for the first time. The in-process path retains Writer race
// coverage without making the instrumented test binary a platform Hook.
func runS9ResumedSecondBlocker(t *testing.T, binary string, inProcess bool) {
	t.Helper()
	root, worker, plan, input, answer := nativeS9WorkerFixture(t)
	var occupied string
	if inProcess {
		occupied = filepath.Join(root, ".claude", "evidence", "s9")
		if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(occupied, []byte("unknown evidence root"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	resultPath := nativeS9TestResultPath(t, root, plan)
	answer.Verdict, answer.ResidualRisks = "blocked", []string{"first dependency unavailable"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, resultPath), answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
		t.Fatalf("first blocker: %d %s", code, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	first, err := runtime.CurrentRepairResultPackets(root, snapshot.State, plan.Binding.OwnerAgentID)
	if err != nil || len(first) != 1 {
		t.Fatalf("first issued Result: %v", err)
	}
	resolution, err := workpacket.RepairResultResolutionAuthoringPath(first[0])
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	readRepairLeadJSON(t, filepath.Join(root, resolution), &draft)
	mapFieldCLI(draft, "s9_resolution")["rationale"] = "The first dependency is available; continue."
	if err := persistRepairLeadQuestionnaire(filepath.Join(root, resolution), draft); err != nil {
		t.Fatal(err)
	}
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
	if receipt, err := service.SaveRepairResultResolutionFrom(t.Context(), filepath.Join(root, resolution), nil); err != nil || !receipt.Accepted {
		t.Fatalf("original resolution: %+v %v", receipt, err)
	}
	answer.ResidualRisks = []string{"a second dependency became unavailable"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, resultPath), answer); err != nil {
		t.Fatal(err)
	}
	input.AgentID = "new-native-owner-after-resolution"
	diagnostic.Reset()
	var publicOut, publicDiagnostic string
	if inProcess {
		var out, diagnostic bytes.Buffer
		if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
			t.Fatalf("second blocker Writer: %t %d %s %s", handled, code, &out, &diagnostic)
		}
		publicOut, publicDiagnostic = out.String(), diagnostic.String()
	} else {
		publicOut, publicDiagnostic = s9BlockerPublicHook(t, binary, root, input)
	}
	snapshot = repairDispatchSnapshot(t, root)
	current, err := runtime.CurrentRepairResultPackets(root, snapshot.State, plan.Binding.OwnerAgentID)
	if err != nil || len(current) != 1 || current[0].Binding.Resume == nil || current[0].Binding.Resume.PredecessorPacketID != first[0].ID {
		t.Fatalf("current resumed Result: %+v %v; %s %s", current, err, publicOut, publicDiagnostic)
	}
	receipt, found, err := runtime.VerifiedRepairResultBlockerReceipt(root, snapshot.State, current[0].ID, plan.Binding.OwnerAgentID)
	if err != nil || !found || receipt.Artifact.PacketID != current[0].ID {
		t.Fatalf("second original blocker: %+v %v", receipt, err)
	}
	agent := contextAgent(snapshot.State, plan.Binding.OwnerAgentID)
	if agent["blocker_resolved_ref"] != nil && agent["blocker_resolved_ref"] != "" {
		t.Fatal("previous resolution remained active for the new blocker")
	}
	if agent["work_blocked_ref"] != receipt.Artifact.MessageRef {
		t.Fatal("new original blocker was not recorded")
	}
	if _, found, err := runtime.VerifiedRepairResultBlockerReceipt(root, snapshot.State, first[0].ID, plan.Binding.OwnerAgentID); err != nil || !found {
		t.Fatalf("first original blocker was not retained: %t %v", found, err)
	}
	if _, found, err := runtime.VerifiedRepairResultResolutionReceipt(root, snapshot.State, first[0].ID, plan.Binding.OwnerAgentID); err != nil || !found {
		t.Fatalf("first original resolution was not retained: %t %v", found, err)
	}
	if inProcess {
		if got, err := os.ReadFile(occupied); err != nil || string(got) != "unknown evidence root" {
			t.Fatalf("unknown evidence root changed: %q %v", got, err)
		}
	}
}

func TestNativeS9ContractConflictWithoutPrivatePlanKeepsHumanDecisionBoundary(t *testing.T) {
	root, worker, plan, input, answer := nativeS9WorkerFixture(t)
	planPath, resultPath := nativeS9TestAuthorPaths(t, root, plan)
	if err := os.Remove(filepath.Join(worker, planPath)); err != nil {
		t.Fatal(err)
	}
	answer.Verdict, answer.ResidualRisks = "contract_conflict", []string{"approved scope cannot resolve the observed boundary"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, resultPath), answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
		t.Fatalf("contract conflict receipt: %d %s", code, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), root), snapshot.State)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original conflict packet: %v", err)
	}
	blocker, found, err := runtime.VerifiedRepairResultBlockerReceipt(root, snapshot.State, packets[0].ID, plan.Binding.OwnerAgentID)
	if err != nil || !found || !strings.Contains(string(blocker.Artifact.SubmittedAnswer), "contract_conflict") {
		t.Fatalf("original conflict was not received: %+v %v", blocker, err)
	}
	resolution, err := workpacket.RepairResultResolutionAuthoringPath(packets[0])
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = repair.PrepareIssuedRepairResultResolution(t.Context(), root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repair.PrepareIssuedRepairResultResolutionRequest{ExpectedRevision: snapshot.Revision, PacketID: packets[0].ID, OwnerAgentID: plan.Binding.OwnerAgentID, AuthoringPath: resolution})
	if err == nil || !strings.Contains(err.Error(), "contract_conflict") {
		t.Fatalf("contract conflict accepted an ordinary retry: %v", err)
	}
}

func TestNativeS9BlockedResultWithoutPrivatePlanIsReceived(t *testing.T) {
	root, worker, plan, input, answer := nativeS9WorkerFixture(t)
	planPath, resultPath := nativeS9TestAuthorPaths(t, root, plan)
	if err := os.Remove(filepath.Join(worker, planPath)); err != nil {
		t.Fatal(err)
	}
	answer.Verdict, answer.ResidualRisks = "blocked", []string{"the required dependency is unavailable"}
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, resultPath), answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	s9ReferencePublicHook(t, root, input)
	snapshot := repairDispatchSnapshot(t, root)
	packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), root), snapshot.State)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original blocked packet: %v", err)
	}
	blocker, found, err := runtime.VerifiedRepairResultBlockerReceipt(root, snapshot.State, packets[0].ID, plan.Binding.OwnerAgentID)
	if err != nil || !found || !strings.Contains(string(blocker.Artifact.SubmittedAnswer), "the required dependency is unavailable") {
		t.Fatalf("original blocker and professional reason not received: %+v %v", blocker, err)
	}
}
