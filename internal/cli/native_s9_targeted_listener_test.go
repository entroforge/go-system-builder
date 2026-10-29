package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

// Uses the existing dispatch fixture and original Plan/Result/Impact consumers;
// all professional observations are explicitly synthetic storage fixture data.
func nativeS9TargetedListenerFixture(t *testing.T) (string, string, repair.TargetedReverificationRequest) {
	t.Helper()
	root, snapshot, plan, _ := repairDispatchFixture(t)
	snapshot.State["schema_version"] = "1.2.0"
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	snapshot = repairVerifierDispatchBuilder(t, root, plan.Assignments[0].AssignmentID, "source-guard-builder")
	division := repairVerifierDispatchPaths(root, snapshot.State)[0]
	repairVerifierDispatchSave(t, root, division, []repairVerifierDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, IndependentVerification: true}})
	snapshot = repairDispatchSnapshot(t, root)
	pointer := repairPointerCLI(snapshot.State)
	original := plan.Assignments[0].AssignmentID
	performing := repair.VerificationAssignmentID(original)
	owner := stringMapCLI(pointer["assignment_owners"])[performing]
	shared, err := repair.UsesSharedTargetedAuthoring(root, snapshot.State, original, performing, owner)
	if err != nil || !shared {
		t.Fatalf("original registered shared Targeted contract: %t %v", shared, err)
	}
	units := []repair.RepairUnitResult{}
	for _, id := range plan.Assignments[0].UnitIDs {
		units = append(units, repair.RepairUnitResult{UnitID: id, Status: "pass", EvidenceRefs: []string{"test://synthetic-source-guard-repair"}})
	}
	_, finalRef, err := repair.SubmitRepairResult(root, repair.RepairResultRequest{
		Contract: repair.ContractRef{Path: stringValue(pointer["contract_ref"]), SHA256: stringValue(pointer["contract_sha256"])},
		Session:  repair.ArtifactRef{Path: stringValue(pointer["path"]), SHA256: stringValue(pointer["sha256"])},
		Plan:     repair.ArtifactRef{Path: stringValue(pointer["plan_ref"]), SHA256: stringValue(pointer["plan_sha256"])},
		ResultID: "repair-result-targeted-source-guard", ProducerAgentID: "source-guard-builder", AssignmentID: original,
		UnitResults: units, Result: "pass", ChangedArtifacts: []repair.ChangedArtifact{},
		BeforeFixChecks: []repair.RepairCheck{{Name: "synthetic pre-fix observation", Command: "test -f internal/api/one.go", Result: "fail", EvidenceRefs: []string{"test://synthetic-source-guard-red"}}},
		Checks:          []repair.RepairCheck{{Name: "synthetic post-fix observation", Command: "test -f internal/api/one.go", Result: "pass", EvidenceRefs: []string{"test://synthetic-source-guard-green"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pointer["result_refs"] = []any{map[string]any{"path": finalRef.Path, "sha256": finalRef.SHA256}}
	// Synthetic Impact establishes the current independent observation slots;
	// TASK/manifest source authority above comes from the actual dispatch path.
	impact := repair.ChangeImpactRequest{ImpactID: "impact-targeted-source-guard", RuntimeID: stringValue(snapshot.State["runtime_id"]), ReqID: "REQ-039", BaselineGeneration: 1,
		SourceCaseIDs: []string{stringValue(pointer["case_id"])}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts:          []repair.ArtifactRef{{ID: "changed-one", Path: "internal/api/one.go", SHA256: strings.Repeat("a", 64)}},
		Decisions:                 []repair.ImpactDecision{{SourceID: "changed-one", TargetID: "root-1", Relation: "invalidates", RuleID: "IM-ROOT", Decision: "reverify", Scope: []string{"internal/api/one.go"}, Rationale: "synthetic source observation", RecoveryEvidence: []string{"test://source-guard"}}},
		RequiredReverificationIDs: []string{"reverify-source-guard"}, AnalyzedBy: "Main"}
	_, impactRef, err := repair.CreateChangeImpact(root, impact)
	if err != nil {
		t.Fatal(err)
	}
	pointer["impact_ref"], pointer["impact_sha256"], pointer["status"] = impactRef.Path, impactRef.SHA256, "targeted_reverification"
	pointer["required_reverification_ids"] = []any{"reverify-source-guard"}
	mapFieldCLI(snapshot.State, "lifecycle")["phase"] = "targeted_reverification"
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	selections, err := repairTargetedSelections(root, snapshot.State)
	if err != nil || len(selections) != 1 {
		t.Fatalf("original exact Targeted selection: %+v %v", selections, err)
	}
	path, draft, err := prepareRepairTargetedQuestionnaire(root, snapshot.State, selections[0])
	if err != nil {
		t.Fatal(err)
	}
	draft.Result, draft.FailureClass = "blocked", "blocked"
	for i := range draft.AssertionResults {
		draft.AssertionResults[i].Result = "blocked"
		draft.AssertionResults[i].EvidenceRefs = []string{"test://synthetic-observed-environment-blocker"}
	}
	writeRepairLeadJSON(t, path, draft)
	return root, path, draft
}

func TestNativeS9TargetedListenerActualPlatformPreAndPost(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		t.Run(event, func(t *testing.T) {
			root, path, complete := nativeS9TargetedListenerFixture(t)
			actor := "opaque-targeted-child"
			parent := filepath.Join(t.TempDir(), "real-platform-session.jsonl")
			child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+actor+".jsonl")
			writeTask := func(file, id string, side bool) {
				t.Helper()
				row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": side, "agentId": id, "message": map[string]any{"role": "user", "content": "Perform your independent verification using this exact original questionnaire " + path}})
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, append(row, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeTask(parent, actor, false) // Real ordinary payload points here, not at the child.
			lastContext := ""
			invoke := func(event, cwd, id string) string {
				t.Helper()
				payload := map[string]any{"hook_event_name": event, "session_id": "real-platform-session", "agent_id": id, "agent_type": "general-purpose", "cwd": cwd, "transcript_path": parent, "tool_name": "Read", "tool_input": map[string]any{"file_path": "internal/api/one.go"}}
				if event == "SubagentStop" {
					payload["agent_transcript_path"], payload["last_assistant_message"] = child, path
				}
				raw, _ := json.Marshal(payload)
				var out, diagnostic bytes.Buffer
				if code := Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(raw), &out, &diagnostic); code != 0 {
					t.Fatalf("public %s: %d %s %s", event, code, &out, &diagnostic)
				}
				lastContext = nativeS9ListenerFeedback(t, &out)
				return diagnostic.String()
			}
			assertNoReception := func(before [][]byte) {
				t.Helper()
				repairDispatchAssertUnchanged(t, root, before)
				snapshot := repairDispatchSnapshot(t, root)
				if refs := repairArtifactRefs(repairPointerCLI(snapshot.State), "targeted_reverification_artifacts", "", ""); len(refs) != 0 {
					t.Fatalf("unreceived input gained original observation: %+v", refs)
				}
			}
			before := repairDispatchRuntimeBytes(t, root)
			// The parent session may quote the right original. A missing actual child
			// and another child's transcript metadata establish no author authority.
			invoke(event, root, actor)
			assertNoReception(before)
			writeTask(child, "different-native-child", true)
			invoke(event, root, actor)
			assertNoReception(before)
			writeTask(child, actor, true)
			unfinished := complete
			unfinished.Result, unfinished.FailureClass = "", ""
			writeRepairLeadJSON(t, path, unfinished)
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			invoke(event, root, actor)
			assertNoReception(before)
			if lastContext != "" {
				t.Fatalf("unfinished Targeted generated feedback: %s", lastContext)
			}
			if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, saved) {
				t.Fatal("unfinished original was changed", err)
			}
			if err := os.WriteFile(path, []byte(`{"result":`), 0600); err != nil {
				t.Fatal(err)
			}
			invoke(event, root, actor)
			assertNoReception(before)
			invalid := complete
			invalid.Result = "unsupported-verdict"
			writeRepairLeadJSON(t, path, invalid)
			invoke(event, root, actor)
			assertNoReception(before)
			if !strings.Contains(lastContext, "Original consumer diagnostic") || !strings.Contains(strings.ToLower(lastContext), "result") || strings.Contains(lastContext, "were accepted") {
				t.Fatalf("Targeted author error missing or falsely accepted: %s", lastContext)
			}
			writeRepairLeadJSON(t, path, complete)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Shared original responsibility cannot be borrowed from another checkout,
			// even when the actual child task contains that exact Main questionnaire.
			isolated := filepath.Join(t.TempDir(), "wrong-targeted-source")
			repairLeadGit(t, root, "worktree", "add", "-b", "targeted-listener-wrong-source", isolated, "HEAD")
			if diagnostic := invoke(event, isolated, actor); !strings.Contains(diagnostic, "no source binding for actual native worktree") {
				t.Fatalf("wrong source was not rejected: %s", diagnostic)
			}
			assertNoReception(before)
			if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, original) {
				t.Fatal("wrong-source attempt changed original", err)
			}
			if diagnostic := invoke(event, root, actor); !strings.Contains(diagnostic, "committed through the original Runtime Writer") {
				t.Fatalf("actual native %s did not reach original Targeted consumer: %s", event, diagnostic)
			}
			if !strings.Contains(lastContext, "Targeted observations") || !strings.Contains(lastContext, "were accepted") || !strings.Contains(lastContext, path) || !strings.Contains(lastContext, "Main owns") || strings.Contains(lastContext, "runtime stage-answer") {
				t.Fatalf("Targeted accepted context missing or assigns Main commands: %s", lastContext)
			}
			snapshot := repairDispatchSnapshot(t, root)
			refs := repairArtifactRefs(repairPointerCLI(snapshot.State), "targeted_reverification_artifacts", "", "")
			if len(refs) != 1 {
				t.Fatalf("original targeted observation missing: %+v", refs)
			}
			accepted := repairDispatchRuntimeBytes(t, root)
			invoke("SubagentStop", root, actor)
			repairDispatchAssertUnchanged(t, root, accepted)
			invoke(event, root, actor)
			repairDispatchAssertUnchanged(t, root, accepted)
			if lastContext != "" {
				t.Fatalf("accepted Targeted replay repeated Worker context: %s", lastContext)
			}
		})
	}
}

func TestNativeS9TargetedListenerStopFallbackAfterUnfinishedSave(t *testing.T) {
	root, path, complete := nativeS9TargetedListenerFixture(t)
	actor := "opaque-targeted-fallback"
	parent := filepath.Join(t.TempDir(), "fallback-session.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+actor+".jsonl")
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": actor, "message": map[string]any{"role": "user", "content": "Independent Targeted original " + path}})
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	unfinished := complete
	unfinished.Result = ""
	writeRepairLeadJSON(t, path, unfinished)
	invoke := func(event string) string {
		t.Helper()
		payload := map[string]any{"hook_event_name": event, "agent_id": actor, "agent_type": "general-purpose", "cwd": root, "session_id": "fallback-session", "transcript_path": parent, "tool_name": "Read"}
		if event == "SubagentStop" {
			payload["agent_transcript_path"], payload["last_assistant_message"] = child, path
		}
		raw, _ := json.Marshal(payload)
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(raw), &out, &diagnostic); code != 0 {
			t.Fatalf("fallback %s: %d %s %s", event, code, &out, &diagnostic)
		}
		return diagnostic.String()
	}
	before := repairDispatchRuntimeBytes(t, root)
	invoke("PreToolUse")
	repairDispatchAssertUnchanged(t, root, before)
	writeRepairLeadJSON(t, path, complete)
	if diagnostic := invoke("SubagentStop"); !strings.Contains(diagnostic, "committed through the original Runtime Writer") {
		t.Fatalf("Stop fallback did not receive original: %s", diagnostic)
	}
	accepted := repairDispatchRuntimeBytes(t, root)
	invoke("SubagentStop")
	repairDispatchAssertUnchanged(t, root, accepted)
}
