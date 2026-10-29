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
)

func TestS9VerifierRegistrationWaitsForTargetedReadiness(t *testing.T) {
	root, _, plan, division := repairDispatchFixture(t)
	assignmentID := plan.Assignments[0].AssignmentID
	repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: assignmentID, RoleFamily: "backend-builder"}})
	current := repairDispatchSnapshot(t, root)
	path, err := repairLeadQuestionnairePath(root, current.State, "independent-verifier-dispatch.json")
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, path, []repairVerifierDispatchChoice{{AssignmentID: assignmentID, IndependentVerification: true}})
	var output bytes.Buffer
	if handled, code := runSavedRepairVerifierDispatch(root, repairLeadSaveInput(root, path, "Main"), &output, &bytes.Buffer{}); !handled || code != 0 {
		t.Fatalf("registration: %t %d %s", handled, code, &output)
	}
	if !strings.Contains(output.String(), "wait for the original Final integration and ChangeImpact") || strings.Contains(output.String(), "spawn/resume") {
		t.Fatalf("premature dispatch: %s", &output)
	}
	registered := repairDispatchSnapshot(t, root)
	verifierID := repair.VerificationAssignmentID(assignmentID)
	owner := stringMapCLI(repairPointerCLI(registered.State)["assignment_owners"])[verifierID]
	target, err := repairTargetedQuestionnairePath(root, registered.State, verifierID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("early registration prepared Targeted: %v", err)
	}
	for _, status := range []string{"planning", "reproducing"} {
		repairPointerCLI(registered.State)["status"] = status
		if got := stageRepairVerifierDispatchContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, registered); strings.Contains(got, "spawn/resume") || !strings.Contains(got, "wait for the original Final") {
			t.Fatalf("%s readiness: %s", status, got)
		}
	}
	repairPointerCLI(registered.State)["status"] = "targeted_reverification"
	for _, directory := range []bool{false, true} {
		if directory {
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
		}
		got := repairVerifierDispatchContext(root, registered.State, verifierID, owner)
		if strings.Contains(got, "spawn/resume") || !strings.Contains(got, path) || !strings.Contains(got, "save the registered division again") {
			t.Fatalf("missing/nonregular answer recovery: %s", got)
		}
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := repairVerifierDispatchContext(root, registered.State, verifierID, owner)
	if !strings.Contains(got, "spawn/resume") || !strings.Contains(got, target) {
		t.Fatalf("ready responsibility missing launch: %s", got)
	}
}

// Called with the real accepted TR-012 successor from the original-consumer
// integration test, so recovery exercises completeRepairExit's real replay.
func assertS9SeedProjectionReadOnlyAndCompletionRepair(t *testing.T, root string, snapshot runtime.Snapshot) {
	t.Helper()
	question := reviewPlanQuestionnairePath(root, snapshot.State)
	guide := reviewPlanGuidePath(question)
	t.Run("missing seed plan projection stays read only", func(t *testing.T) {
		data, err := json.Marshal(snapshot.State)
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		delete(mapFieldCLI(state, "review"), "plan")
		original, err := os.ReadFile(question)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(question); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.WriteFile(question, original, 0644); err != nil {
				t.Error(err)
			}
		}()
		before := repairDispatchRuntimeBytes(t, root)
		message := stageReviewPlanQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, runtime.Snapshot{State: state})
		if !strings.Contains(message, repairExitRecovery(root)) {
			t.Fatalf("missing original projection recovery: %s", message)
		}
		if _, err := os.Stat(question); !os.IsNotExist(err) {
			t.Fatalf("missing plan projection manufactured a generic draft: %v", err)
		}
		if draft, notes := reviewPlanDraftForEntry(root, state, integerValue(mapFieldCLI(state, "review")["round"])); draft != nil || !strings.Contains(strings.Join(notes, " "), "original S9 seed/ReviewPlan projection") {
			t.Fatalf("lost original projection gap: draft=%+v notes=%v", draft, notes)
		}
		repairDispatchAssertUnchanged(t, root, before)
	})
	for _, missing := range []string{question, guide} {
		original, err := os.ReadFile(missing)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(missing); err != nil {
			t.Fatal(err)
		}
		beforeState, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
		beforeJournal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
		var status bytes.Buffer
		if code := runS7Status(root, &status, false); code != 0 {
			t.Fatalf("status: %d %s", code, &status)
		}
		messages := []string{status.String(), stageReviewPlanQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, snapshot)}
		ordinaryPost, _ := prepareReviewPlanQuestionnaireContext(root, policy.Input{Event: "PostToolUse", CWD: root}, snapshot)
		messages = append(messages, ordinaryPost, stageReviewPlanQuestionnaireContext(root, policy.Input{Event: "PostToolUse", CWD: root}, snapshot))
		guidance := policy.Guidance{}
		applyS7RecoveryProjection(root, snapshot.State, &guidance)
		messages = append(messages, guidance.Action)
		for _, message := range messages {
			if !strings.Contains(message, repairExitRecovery(root)) || !strings.Contains(message, missing) {
				t.Fatalf("missing exact original recovery for %s: %s", missing, message)
			}
		}
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatalf("ordinary projection prepared %s: %v", missing, err)
		}
		afterState, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
		afterJournal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
		if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
			t.Fatal("ordinary S9-seed projection wrote Runtime")
		}
		if message, err := completeRepairExit(root); err != nil {
			t.Fatalf("explicit original completion: %s %v", message, err)
		}
		restored, err := os.ReadFile(missing)
		if err != nil || !bytes.Equal(original, restored) {
			t.Fatalf("explicit completion failed to restore original %s: %v", missing, err)
		}
		if after := repairDispatchSnapshot(t, root); after.Revision != snapshot.Revision {
			t.Fatal("preparation replay advanced Runtime")
		}
	}
}

func assertS9RegisteredVerifierSaveRestoresTarget(t *testing.T, root, division, target string, snapshot runtime.Snapshot) {
	t.Helper()
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if handled, code := runSavedRepairVerifierDispatch(root, repairLeadSaveInput(root, division, "Main"), &output, &bytes.Buffer{}); !handled || code != 0 {
		t.Fatalf("registered division recovery: %t %d %s", handled, code, &output)
	}
	restored, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(original, restored) {
		t.Fatalf("division recovery did not restore original Targeted: %v %s", err, &output)
	}
	if !strings.Contains(output.String(), "Existing original verification dispatch reused without a Runtime write") || !strings.Contains(output.String(), "spawn/resume") || !strings.Contains(output.String(), target) {
		t.Fatalf("reused registration did not become dispatchable: %s", &output)
	}
	if after := repairDispatchSnapshot(t, root); after.Revision != snapshot.Revision {
		t.Fatal("registered division recovery wrote Runtime")
	}
}
