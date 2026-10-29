package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9NaturalAcceptedReplayDoesNotConsumeOrWriteTyped(t *testing.T) {
	root, worker, plan, in, _ := nativeS9WorkerFixture(t)
	in.Event, in.CWD = "PreToolUse", worker
	var diagnostic bytes.Buffer
	snap := repairDispatchSnapshot(t, root)
	if snap.State["workspace"] != nil {
		t.Fatal("first natural reception must begin without an execution")
	}
	if err := receiveNativeS9BuilderEvent(root, snap.State, in, plan, in.LastAssistantMessage, &diagnostic, nil); err != nil {
		t.Fatalf("first reception without an execution: %v %s", err, &diagnostic)
	}
	snap = repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairResultPacketsContext(t.Context(), root, snap.State, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("received Result packet: %d %v", len(packets), err)
	}
	typed := filepath.Join(worker, packets[0].AnswerPath)
	stamp := time.Unix(1000000000, 0)
	if err := os.Chtimes(typed, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(typed)
	if err != nil {
		t.Fatal(err)
	}
	beforeTyped, err := os.ReadFile(typed)
	if err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		in.Event = event
		capture := newNativeS9AttemptCapture(t.Context(), root, snap, plan.ID, plan.Binding.OwnerAgentID, "Builder")
		if err := receiveNativeS9BuilderEvent(root, snap.State, in, plan, in.LastAssistantMessage, &diagnostic, capture); err != nil {
			t.Fatalf("%s replay: %v %s", event, err, &diagnostic)
		}
		if capture.consumer || capture.value.Attempt.Stage != "selected" || capture.value.Attempt.TypedInputRef != "" {
			t.Fatalf("accepted replay entered adaptation/consumer: %+v", capture.value.Attempt)
		}
		afterInfo, err := os.Stat(typed)
		if err != nil || !os.SameFile(beforeInfo, afterInfo) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
			t.Fatalf("accepted replay wrote typed file: %v", err)
		}
		after, err := os.ReadFile(typed)
		if err != nil || !bytes.Equal(beforeTyped, after) {
			t.Fatalf("accepted replay changed typed bytes: %v", err)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
	// The receipt cannot authorize newly edited private originals, even when
	// the edit makes the flat answer look like an unfinished draft.
	planPath, resultPath, err := nativeS9AuthorPaths(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{planPath, resultPath} {
		path := filepath.Join(worker, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"verdict":""}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := receiveNativeS9BuilderEvent(root, snap.State, in, plan, in.LastAssistantMessage, &diagnostic, nil); err == nil || !strings.Contains(err.Error(), "bytes changed") {
			t.Fatalf("accepted changed private original was hidden: %s: %v", rel, err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
	// Candidate acceptance cannot authorize recovery of an absent worktree.
	// Natural events cannot prove its live source; the explicit recovery still
	// requires the original verified cleanup checkpoint.
	repairLeadGit(t, root, "worktree", "remove", "--force", worker)
	if err := receiveNativeS9BuilderEvent(root, snap.State, in, plan, in.LastAssistantMessage, &diagnostic, nil); err != nativeS9ProgressPending {
		t.Fatalf("missing natural source was claimed: %v", err)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if handled, err := recoverNativeS9TypedResult(t.Context(), service, snap.State, plan, worker, true, false, &diagnostic, nil); !handled || err == nil {
		t.Fatalf("Candidate alone authorized a missing worktree: handled=%t err=%v", handled, err)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat(t *testing.T) {
	for _, event := range []string{"PreToolUse", "SubagentStop"} {
		for _, missing := range []bool{false, true} {
			t.Run(event+"/"+map[bool]string{false: "changed", true: "missing"}[missing], func(t *testing.T) {
				root, worker, plan, in, professional := nativeS9LegacyWorkerFixture(t)
				service, err := submission.New(root)
				if err != nil {
					t.Fatal(err)
				}
				var diagnostic bytes.Buffer
				if err := importNativeS9PlanReport(t.Context(), root, worker, plan, service, &diagnostic); err != nil {
					t.Fatal(err)
				}
				if r, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !r.Accepted {
					t.Fatalf("Plan: %+v %v", r, err)
				}
				if err := recordNativeS9Source(t.Context(), root, worker, repairLeadGit(t, root, "rev-parse", "HEAD"), plan); err != nil {
					t.Fatal(err)
				}
				packets, err := service.IssueRepairResults(t.Context(), plan.Binding.OwnerAgentID)
				if err != nil || len(packets) != 1 {
					t.Fatalf("Result issue: %d %v", len(packets), err)
				}
				draft := packets[0].Draft
				draft.Answer, _ = json.Marshal(professional)
				typed := filepath.Join(worker, packets[0].AnswerPath)
				if err := persistRepairLeadQuestionnaire(typed, draft); err != nil {
					t.Fatal(err)
				}
				saved, err := os.ReadFile(typed)
				if err != nil {
					t.Fatal(err)
				}
				_, result, err := nativeS9AuthorPaths(root, plan)
				if err != nil {
					t.Fatal(err)
				}
				flat := filepath.Join(worker, result)
				if missing {
					err = os.Remove(flat)
				} else {
					err = os.WriteFile(flat, []byte(`{"verdict":""}`), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				in.Event, in.CWD = event, worker
				nativeS9SplitChild(t, &in)
				if diagnostic := nativeS9SplitPublicEvent(t, root, in); strings.Contains(diagnostic, "reception incomplete") {
					t.Fatalf("saved typed recovery: %s", diagnostic)
				}
				after, err := os.ReadFile(typed)
				if err != nil || !bytes.Equal(saved, after) {
					t.Fatalf("original typed input replaced: %v", err)
				}
				if r, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packets[0].ID); err != nil || !r.Accepted || r.ResponsibilityStatus != "candidate_accepted" {
					t.Fatalf("original typed consumer not resumed: %+v %v", r, err)
				}
			})
		}
	}
}

// Exercise the actual JSON Hook entry and child task association, rather than
// selecting a packet by role or calling the adapter with an invented receipt.
func nativeS9SplitChild(t *testing.T, in *policy.Input) {
	t.Helper()
	in.AgentTranscriptPath = filepath.Join(t.TempDir(), "agent.jsonl")
	row, err := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": in.AgentID, "message": map[string]any{"role": "user", "content": in.LastAssistantMessage}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in.AgentTranscriptPath, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func nativeS9SplitPublicEvent(t *testing.T, root string, in policy.Input) string {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"hook", "--root", root, "--event", in.Event}, bytes.NewReader(raw), &out, &diagnostic); code != 0 {
		t.Fatalf("public %s: %d %s %s", in.Event, code, &out, &diagnostic)
	}
	return diagnostic.String()
}

func TestNativeS9PublicAuthorCorrectsRejectedResult(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			root, worker, plan, in, professional := nativeS9WorkerFixture(t)
			_, result, err := nativeS9AuthorPaths(root, plan)
			if err != nil {
				t.Fatal(err)
			}
			flat := filepath.Join(worker, result)
			body, err := json.Marshal(professional)
			if err != nil {
				t.Fatal(err)
			}
			var invalid map[string]any
			if err := json.Unmarshal(body, &invalid); err != nil {
				t.Fatal(err)
			}
			// null decodes as an empty Go string, but the original professional
			// validator must reject it before creating or staging a Candidate.
			invalid["migration_ref"] = nil
			if err := persistRepairLeadQuestionnaire(flat, invalid); err != nil {
				t.Fatal(err)
			}
			in.Event, in.CWD = event, worker
			nativeS9SplitChild(t, &in)
			if diagnostic := nativeS9SplitPublicEvent(t, root, in); !strings.Contains(diagnostic, "reception incomplete") || !strings.Contains(diagnostic, "migration_ref") {
				t.Fatalf("original professional rejection was not delivered: %s", diagnostic)
			}
			partial := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(partial.State)
			if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef != "" {
				t.Fatalf("rejected answer acquired a Candidate: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json.commit-pending.json")); !os.IsNotExist(err) {
				t.Fatalf("professional rejection unexpectedly fixed a pending pair: %v", err)
			}
			packets, err := runtime.CurrentRepairResultPacketsContext(t.Context(), root, partial.State, plan.Binding.OwnerAgentID)
			if err != nil || len(packets) != 1 {
				t.Fatalf("original Result issue missing: %v", err)
			}
			typed, err := os.ReadFile(filepath.Join(worker, packets[0].AnswerPath))
			if err != nil || nativeS9TypedInputSaved(packets[0], typed, root) {
				t.Fatalf("first pre-Candidate rejection did not restore its issued draft: %v", err)
			}

			professional.ResidualRisks = []string{"actual corrected professional observation"}
			if err := persistRepairLeadQuestionnaire(flat, professional); err != nil {
				t.Fatal(err)
			}
			if diagnostic := nativeS9SplitPublicEvent(t, root, in); strings.Contains(diagnostic, "reception incomplete") {
				t.Fatalf("corrected original was not received: %s", diagnostic)
			}
			accepted := repairDispatchSnapshot(t, root)
			registry, err = workspace.Decode(accepted.State)
			if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef == "" {
				t.Fatalf("corrected original did not create its Candidate: %v", err)
			}
			candidateBytes, err := os.ReadFile(filepath.Join(root, registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef))
			if err != nil {
				t.Fatal(err)
			}
			var candidate repair.WorkspaceCandidate
			if err := json.Unmarshal(candidateBytes, &candidate); err != nil || candidate.Provenance == nil {
				t.Fatalf("original Candidate provenance unavailable: %v", err)
			}
			var submitted struct {
				MigrationRef string   `json:"migration_ref"`
				Risks        []string `json:"residual_risks"`
			}
			if err := json.Unmarshal(candidate.Provenance.SubmittedAnswer, &submitted); err != nil || submitted.MigrationRef != professional.MigrationRef || len(submitted.Risks) != 1 || submitted.Risks[0] != professional.ResidualRisks[0] {
				t.Fatalf("Candidate did not preserve the corrected professional original: %s (%v)", candidate.Provenance.SubmittedAnswer, err)
			}
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packets[0].ID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
				t.Fatalf("corrected original lacks its original Candidate receipt: %+v %v", receipt, err)
			}
		})
	}
}

func TestNativeS9PublicCorrectsHistoricalUnstagedResult(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			root, worker, plan, in, professional := nativeS9WorkerFixture(t)
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			var diagnostic bytes.Buffer
			if err := importNativeS9PlanReport(t.Context(), root, worker, plan, service, &diagnostic); err != nil {
				t.Fatal(err)
			}
			if r, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !r.Accepted {
				t.Fatalf("original Plan: %+v %v", r, err)
			}
			if err := recordNativeS9Source(t.Context(), root, worker, repairLeadGit(t, root, "rev-parse", "HEAD"), plan); err != nil {
				t.Fatal(err)
			}
			packets, err := service.IssueRepairResults(t.Context(), plan.Binding.OwnerAgentID)
			if err != nil || len(packets) != 1 {
				t.Fatalf("original Result issue: %v", err)
			}
			// Model an older implementation's already-saved, unaccepted typed
			// input whose referenced evidence never reached Candidate staging.
			old := professional
			old.EvidenceRefs = []string{"path:logs/historical-missing.txt"}
			draft := packets[0].Draft
			draft.Answer, _ = json.Marshal(old)
			typed := filepath.Join(worker, packets[0].AnswerPath)
			if err := persistRepairLeadQuestionnaire(typed, draft); err != nil {
				t.Fatal(err)
			}
			oldTyped, err := os.ReadFile(typed)
			if err != nil {
				t.Fatal(err)
			}
			_, result, err := nativeS9AuthorPaths(root, plan)
			if err != nil {
				t.Fatal(err)
			}
			flat := filepath.Join(worker, result)
			professional.ResidualRisks = []string{"corrected historical observation"}
			if err := persistRepairLeadQuestionnaire(flat, professional); err != nil {
				t.Fatal(err)
			}
			// An explicit Main technical recovery must not adopt the newer flat.
			var out bytes.Buffer
			if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diagnostic); code == 0 {
				t.Fatalf("technical retry adopted the later original: %s %s", &out, &diagnostic)
			}
			if raw, err := os.ReadFile(typed); err != nil || !bytes.Equal(raw, oldTyped) {
				t.Fatalf("technical retry changed the saved input: %v", err)
			}
			in.Event, in.CWD = event, worker
			nativeS9SplitChild(t, &in)
			if message := nativeS9SplitPublicEvent(t, root, in); strings.Contains(message, "reception incomplete") {
				t.Fatalf("actual author's corrected Result was not received: %s", message)
			}
			snap := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snap.State)
			if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef == "" {
				t.Fatalf("corrected historical Result lacks a Candidate: %v", err)
			}
			candidateBytes, err := os.ReadFile(filepath.Join(root, registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef))
			if err != nil {
				t.Fatal(err)
			}
			var candidate repair.WorkspaceCandidate
			if err := json.Unmarshal(candidateBytes, &candidate); err != nil || candidate.Provenance == nil {
				t.Fatalf("Candidate provenance unavailable: %v", err)
			}
			var actual struct {
				Evidence []string `json:"evidence_refs"`
				Risks    []string `json:"residual_risks"`
			}
			if err := json.Unmarshal(candidate.Provenance.SubmittedAnswer, &actual); err != nil || len(actual.Risks) != 1 || actual.Risks[0] != professional.ResidualRisks[0] || len(actual.Evidence) != 1 || actual.Evidence[0] != professional.EvidenceRefs[0] {
				t.Fatalf("Candidate did not use the actual corrected source: %s (%v)", candidate.Provenance.SubmittedAnswer, err)
			}
			if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packets[0].ID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
				t.Fatalf("corrected historical source lacks original receipt: %+v %v", receipt, err)
			}
		})
	}
}

func TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns(t *testing.T) {
	root, worker, plan, in, professional := nativeS9LegacyWorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := importNativeS9PlanReport(t.Context(), root, worker, plan, service, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if r, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !r.Accepted {
		t.Fatalf("original Plan: %+v %v", r, err)
	}
	if err := recordNativeS9Source(t.Context(), root, worker, repairLeadGit(t, root, "rev-parse", "HEAD"), plan); err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueRepairResults(t.Context(), plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Result issue: %v", err)
	}
	_, result, err := nativeS9AuthorPaths(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	flat := filepath.Join(worker, result)
	professional.EvidenceRefs = []string{"path:logs/restored-source.txt"}
	old := packets[0].Draft
	old.Answer, _ = json.Marshal(professional)
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, packets[0].AnswerPath), old); err != nil {
		t.Fatal(err)
	}
	if err := persistRepairLeadQuestionnaire(flat, professional); err != nil {
		t.Fatal(err)
	}
	in.Event, in.CWD = "PreToolUse", worker
	nativeS9SplitChild(t, &in)
	if message := nativeS9SplitPublicEvent(t, root, in); !strings.Contains(message, "reception incomplete") || !strings.Contains(message, "restored-source.txt") {
		t.Fatalf("old input did not actually fail on its missing source: %s", message)
	}
	partial := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(partial.State)
	if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef != "" {
		t.Fatalf("source rejection acquired a Candidate: %v", err)
	}
	// Both the old and new answers are now individually acceptable. The actual
	// author's changed original must win when no Candidate was ever staged.
	writeRepairLeadFile(t, worker, "logs/restored-source.txt", "original evidence source restored\n")
	professional.ResidualRisks = []string{"new observation after source restoration"}
	if err := persistRepairLeadQuestionnaire(flat, professional); err != nil {
		t.Fatal(err)
	}
	if message := nativeS9SplitPublicEvent(t, root, in); strings.Contains(message, "reception incomplete") {
		t.Fatalf("corrected original was not received: %s", message)
	}
	accepted := repairDispatchSnapshot(t, root)
	registry, err = workspace.Decode(accepted.State)
	if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("corrected original has no Candidate: %v", err)
	}
	candidateBytes, err := os.ReadFile(filepath.Join(root, registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef))
	if err != nil {
		t.Fatal(err)
	}
	var candidate repair.WorkspaceCandidate
	if err := json.Unmarshal(candidateBytes, &candidate); err != nil || candidate.Provenance == nil {
		t.Fatalf("original Candidate provenance: %v", err)
	}
	var actual struct {
		Risks []string `json:"residual_risks"`
	}
	if err := json.Unmarshal(candidate.Provenance.SubmittedAnswer, &actual); err != nil || len(actual.Risks) != 1 || actual.Risks[0] != professional.ResidualRisks[0] {
		t.Fatalf("old typed input displaced the author's actual correction: %s (%v)", candidate.Provenance.SubmittedAnswer, err)
	}
	if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packets[0].ID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
		t.Fatalf("corrected original lacks original receipt: %+v %v", receipt, err)
	}
}

func TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceDisappears(t *testing.T) {
	root, worker, plan, packet, previousState, previousJournal, _, _ := nativeS9OriginalCandidateBoundary(t)
	accepted := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(accepted.State)
	if err != nil || registry == nil {
		t.Fatalf("original Candidate execution: %v", err)
	}
	candidateRef := registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef
	candidate := filepath.Join(root, candidateRef)
	candidateBytes, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	// Retain the actual immutable Candidate while returning Runtime to its
	// original pre-CAS state; no pending marker has been published yet.
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), previousState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-events.jsonl"), previousJournal, 0600); err != nil {
		t.Fatal(err)
	}
	typed := filepath.Join(worker, packet.AnswerPath)
	typedBytes, err := os.ReadFile(typed)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := nativeS9AuthorPaths(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	flat := filepath.Join(worker, result)
	flatBytes, err := os.ReadFile(flat)
	if err != nil {
		t.Fatal(err)
	}
	var later map[string]any
	if err := json.Unmarshal(flatBytes, &later); err != nil {
		t.Fatal(err)
	}
	later["residual_risks"] = []string{"a later professional edit"}
	if err := persistRepairLeadQuestionnaire(flat, later); err != nil {
		t.Fatal(err)
	}
	// The old Candidate was staged when this source existed. Losing it later
	// makes the current old-input retry fail before it can rebuild that Candidate.
	if err := os.Remove(filepath.Join(worker, "internal/api/one.go")); err != nil {
		t.Fatal(err)
	}
	brief, _, _ := nativeS9Paths(plan)
	in := policy.Input{AgentID: "native-original", CWD: worker, LastAssistantMessage: filepath.Join(root, brief) + "\n" + flat}
	nativeS9SplitChild(t, &in)
	before := repairDispatchRuntimeBytes(t, root)
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		in.Event = event
		if diagnostic := nativeS9SplitPublicEvent(t, root, in); !strings.Contains(diagnostic, "reception incomplete") || !strings.Contains(diagnostic, candidateRef) {
			t.Fatalf("%s hid the original unavailable source: %s", event, diagnostic)
		}
		if raw, err := os.ReadFile(typed); err != nil || !bytes.Equal(raw, typedBytes) {
			t.Fatalf("%s overwrote original staged typed input: %v", event, err)
		}
		if raw, err := os.ReadFile(candidate); err != nil || !bytes.Equal(raw, candidateBytes) {
			t.Fatalf("%s replaced the original staged Candidate: %v", event, err)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
}

func TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidate(t *testing.T) {
	root, worker, plan, packet, previousState, previousJournal, _, _ := nativeS9OriginalCandidateBoundary(t)
	accepted := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(accepted.State)
	if err != nil || registry == nil {
		t.Fatalf("original Candidate execution: %v", err)
	}
	candidateRef := registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef
	candidate := filepath.Join(root, candidateRef)
	candidateBytes, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), previousState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-events.jsonl"), previousJournal, 0600); err != nil {
		t.Fatal(err)
	}
	typed := filepath.Join(worker, packet.AnswerPath)
	if err := os.Remove(typed); err != nil {
		t.Fatal(err)
	}
	_, result, err := nativeS9AuthorPaths(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	flat := filepath.Join(worker, result)
	flatBytes, err := os.ReadFile(flat)
	if err != nil {
		t.Fatal(err)
	}
	var later map[string]any
	if err := json.Unmarshal(flatBytes, &later); err != nil {
		t.Fatal(err)
	}
	later["residual_risks"] = []string{"later input cannot replace a lost original typed attempt"}
	if err := persistRepairLeadQuestionnaire(flat, later); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	var out, diagnostic bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diagnostic); code == 0 {
		t.Fatalf("technical recovery fabricated the missing staged typed input: %s %s", &out, &diagnostic)
	}
	brief, _, _ := nativeS9Paths(plan)
	in := policy.Input{AgentID: "native-original", CWD: worker, LastAssistantMessage: filepath.Join(root, brief) + "\n" + flat}
	nativeS9SplitChild(t, &in)
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		in.Event = event
		if message := nativeS9SplitPublicEvent(t, root, in); !strings.Contains(message, "reception incomplete") || !strings.Contains(message, candidateRef) {
			t.Fatalf("%s did not explain the missing staged input: %s", event, message)
		}
		if _, err := os.Stat(typed); !os.IsNotExist(err) {
			t.Fatalf("%s fabricated the missing staged typed input: %v", event, err)
		}
		if raw, err := os.ReadFile(candidate); err != nil || !bytes.Equal(raw, candidateBytes) {
			t.Fatalf("%s changed the original staged Candidate: %v", event, err)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
}

func TestNativeS9StagedCandidateVerification(t *testing.T) {
	root, _, plan, packet, _, _, _, _ := nativeS9OriginalCandidateBoundary(t)
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("original execution: %v", err)
	}
	execution := registry.Executions[plan.Binding.ManifestAssignmentID]
	ref := execution.DeliveryRef
	path := filepath.Join(root, ref)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repair.FindNativeS9StagedCandidate(t.Context(), root, packet, execution); err != nil || got != ref {
		t.Fatalf("original Candidate was not identified: %s %v", got, err)
	}
	other := packet
	other.ID = "packet-" + strings.Repeat("0", 64)
	if other.ID == packet.ID {
		other.ID = "packet-" + strings.Repeat("1", 64)
	}
	if got, err := repair.FindNativeS9StagedCandidate(t.Context(), root, other, execution); err != nil || got != "" {
		t.Fatalf("another packet acquired this Candidate: %s %v", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, err := repair.FindNativeS9StagedCandidate(t.Context(), root, packet, execution); err != nil || got != "" {
		t.Fatalf("empty Candidate directory was not empty: %s %v", got, err)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), original...), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := repair.FindNativeS9StagedCandidate(t.Context(), root, packet, execution); err == nil || got != "" {
		t.Fatalf("Candidate with a changed hash was treated as verified: %s %v", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var invalid map[string]any
	if err := json.Unmarshal(original, &invalid); err != nil {
		t.Fatal(err)
	}
	delete(invalid, "version")
	invalidBytes, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	invalidPath := filepath.Join(filepath.Dir(path), workpacket.Digest(invalidBytes)+".json")
	if err := os.WriteFile(invalidPath, invalidBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := repair.FindNativeS9StagedCandidate(t.Context(), root, packet, execution); err == nil || got != "" {
		t.Fatalf("schema-invalid original Candidate was treated as verified: %s %v", got, err)
	}
	if err := os.Remove(invalidPath); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 257; i++ {
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), fmt.Sprintf("unexpected-%03d", i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := repair.FindNativeS9StagedCandidate(t.Context(), root, packet, execution); err == nil || got != "" {
		t.Fatalf("an exhausted directory budget was treated as no staged Candidate: %s %v", got, err)
	}
}

func TestNativeS9PublicStopAndNaturalReplayPreserveAcceptedSources(t *testing.T) {
	for _, contract := range []string{"C", "D"} {
		t.Run(contract, func(t *testing.T) {
			fixture := nativeS9WorkerFixture
			if contract == "C" {
				fixture = nativeS9LegacyWorkerFixture
			}
			root, worker, plan, in, _ := fixture(t)
			if contract == "C" {
				repairLeadGit(t, worker, "reset", "--hard", repairLeadGit(t, root, "rev-parse", "HEAD"))
			}
			nativeS9SplitChild(t, &in)
			in.Event = "SubagentStop"
			if repairDispatchSnapshot(t, root).State["workspace"] != nil {
				t.Fatal("first Stop must capture source through its original path")
			}
			if diagnostic := nativeS9SplitPublicEvent(t, root, in); strings.Contains(diagnostic, "reception incomplete") {
				t.Fatalf("first Stop: %s", diagnostic)
			}
			snap := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snap.State)
			if err != nil || registry == nil {
				t.Fatalf("original execution missing: %v", err)
			}
			execution := registry.Executions[plan.Binding.ManifestAssignmentID]
			packets, err := runtime.CurrentRepairResultPacketsContext(t.Context(), root, snap.State, plan.Binding.OwnerAgentID)
			if err != nil || len(packets) != 1 || execution.DeliveryRef == "" {
				t.Fatalf("original Candidate missing: %d %v %+v", len(packets), err, execution)
			}
			typed := filepath.Join(worker, packets[0].AnswerPath)
			var retained *repair.NativeS9Source
			if contract == "C" {
				retained, err = nativeS9RetainedSource(t.Context(), root, snap.State, plan, worker)
				if err != nil || retained == nil || retained.Actor != in.AgentID {
					t.Fatalf("first Stop did not capture original actor/source: %+v %v", retained, err)
				}
				typed = filepath.Join(root, repair.NativeS9TypedInputPath(retained, packets[0]))
			}
			// Removing only the already consumed typed wrapper makes an accidental
			// ReadRepairResultInput/flat adaptation observable on the next event.
			if err := os.Remove(typed); err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			for _, event := range []string{"SubagentStop", "PreToolUse", "PostToolUse"} {
				in.Event, in.CWD = event, worker
				if diagnostic := nativeS9SplitPublicEvent(t, root, in); strings.Contains(diagnostic, "reception incomplete") {
					t.Fatalf("accepted %s: %s", event, diagnostic)
				}
				if _, err := os.Stat(typed); !os.IsNotExist(err) {
					t.Fatalf("accepted %s published typed: %v", event, err)
				}
				repairDispatchAssertUnchanged(t, root, before)
			}
			// Exercise the same accepted C/D checks through public events after
			// each independent corruption; successful receipt is not a bypass.
			_, result, err := nativeS9AuthorPaths(root, plan)
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{filepath.Join(worker, result)}
			if retained != nil {
				paths = append(paths, filepath.Join(worker, retained.PlanPath), filepath.Join(root, repair.NativeS9SourceReference(execution).Path))
				if len(retained.Evidence) == 0 {
					t.Fatal("retained C lacks real evidence")
				}
				paths = append(paths, filepath.Join(root, retained.Evidence[0].Path))
			}
			for _, path := range paths {
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(append([]byte(nil), original...), '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				for _, event := range []string{"SubagentStop", "PreToolUse", "PostToolUse"} {
					in.Event = event
					if diagnostic := nativeS9SplitPublicEvent(t, root, in); !strings.Contains(diagnostic, "reception incomplete") {
						t.Fatalf("changed %s accepted at %s: %s", path, event, diagnostic)
					}
					repairDispatchAssertUnchanged(t, root, before)
				}
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if retained != nil {
				repairLeadGit(t, root, "update-ref", "-d", retained.GitRef)
				in.Event = "SubagentStop"
				if diagnostic := nativeS9SplitPublicEvent(t, root, in); !strings.Contains(diagnostic, "reception incomplete") || !strings.Contains(diagnostic, "Git reference") {
					t.Fatalf("accepted C lost Git source: %s", diagnostic)
				}
				repairLeadGit(t, root, "update-ref", retained.GitRef, retained.Head)
			}
			// C has its original retained nochange capability; D Candidate alone
			// cannot claim that an absent live source was verified and reclaimed.
			repairLeadGit(t, root, "worktree", "remove", "--force", worker)
			in.Event = "SubagentStop"
			diagnostic := nativeS9SplitPublicEvent(t, root, in)
			if strings.Contains(diagnostic, "reception incomplete") != (contract == "D") {
				t.Fatalf("missing %s source projected wrong fact: %s", contract, diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}
