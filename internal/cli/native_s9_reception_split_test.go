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
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Both supported diagnostics describe an incomplete reception; durable receipts
// and Runtime comparisons below remain the acceptance authority.
func nativeS9ReceptionIncomplete(message string) bool {
	return strings.Contains(message, "reception incomplete") ||
		strings.Contains(message, "reception for") && strings.Contains(message, "is incomplete")
}

func TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat(t *testing.T) {
	for _, event := range []string{"PreToolUse", "SubagentStop"} {
		for _, missing := range []bool{false, true} {
			t.Run(event+"/"+map[bool]string{false: "changed", true: "missing"}[missing], func(t *testing.T) {
				root, worker, plan, in, professional := nativeS9WorkerFixture(t)
				service, err := submission.New(root)
				if err != nil {
					t.Fatal(err)
				}
				var diagnostic bytes.Buffer
				if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, plan, service, &diagnostic, nil); err != nil {
					t.Fatal(err)
				}
				if r, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !r.Accepted {
					t.Fatalf("Plan: %+v %v", r, err)
				}
				if err := recordNativeS9OriginalLocation(t.Context(), root, plan, worker); err != nil {
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
				if diagnostic := nativeS9SplitPublicEvent(t, root, in); nativeS9ReceptionIncomplete(diagnostic) {
					t.Fatalf("saved typed recovery: %s", diagnostic)
				}
				after, err := os.ReadFile(typed)
				if err != nil || !bytes.Equal(saved, after) {
					t.Fatalf("original typed input replaced: %v", err)
				}
				// L3-S9-repair.md:134 keeps exactly one author path and
				// L4-hook-acceptance-checklist.md:159 removed the flat/typed
				// adaptation. The saved typed copy is not a second author path, so
				// a changed or missing flat answer leaves the original consumer
				// unaccepted instead of being resumed from that copy.
				if r, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packets[0].ID); err != nil || r.Accepted {
					t.Fatalf("typed copy resumed the original consumer: %+v %v", r, err)
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
			if diagnostic := nativeS9SplitPublicEvent(t, root, in); !nativeS9ReceptionIncomplete(diagnostic) || !strings.Contains(diagnostic, "migration_ref") {
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

			professional.ResidualRisks = []string{"actual corrected professional observation"}
			if err := persistRepairLeadQuestionnaire(flat, professional); err != nil {
				t.Fatal(err)
			}
			if diagnostic := nativeS9SplitPublicEvent(t, root, in); nativeS9ReceptionIncomplete(diagnostic) {
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
			if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, candidate.Provenance.PacketID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
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
			if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, plan, service, &diagnostic, nil); err != nil {
				t.Fatal(err)
			}
			if r, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !r.Accepted {
				t.Fatalf("original Plan: %+v %v", r, err)
			}
			if err := recordNativeS9OriginalLocation(t.Context(), root, plan, worker); err != nil {
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
			if message := nativeS9SplitPublicEvent(t, root, in); nativeS9ReceptionIncomplete(message) {
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
			if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, candidate.Provenance.PacketID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
				t.Fatalf("corrected historical source lacks original receipt: %+v %v", receipt, err)
			}
		})
	}
}

func TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns(t *testing.T) {
	root, worker, plan, in, professional := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, plan, service, &diagnostic, nil); err != nil {
		t.Fatal(err)
	}
	if r, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !r.Accepted {
		t.Fatalf("original Plan: %+v %v", r, err)
	}
	if err := recordNativeS9OriginalLocation(t.Context(), root, plan, worker); err != nil {
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
	// L3-S9-repair.md:236 keeps the reception from re-reading evidence files and
	// L4-hook-acceptance-checklist.md:59/:53 removed the evidence-existence
	// gate, so a missing referenced source no longer makes the reception
	// incomplete and no Candidate is withheld for it.
	if message := nativeS9SplitPublicEvent(t, root, in); nativeS9ReceptionIncomplete(message) {
		t.Fatalf("missing evidence source still refused the original responsibility: %s", message)
	}
	if _, err := os.Stat(filepath.Join(worker, "logs/restored-source.txt")); !os.IsNotExist(err) {
		t.Fatalf("reception fabricated the missing evidence source: %v", err)
	}
	partial := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(partial.State)
	if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("received original responsibility has no Candidate: %v", err)
	}
	// Both the old and new answers are individually acceptable. The later
	// actual edit is a revision or a new attempt on the same original
	// responsibility.
	writeRepairLeadFile(t, worker, "logs/restored-source.txt", "original evidence source restored\n")
	professional.ResidualRisks = []string{"new observation after source restoration"}
	if err := persistRepairLeadQuestionnaire(flat, professional); err != nil {
		t.Fatal(err)
	}
	if message := nativeS9SplitPublicEvent(t, root, in); nativeS9ReceptionIncomplete(message) {
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
		EvidenceRefs []string `json:"evidence_refs"`
		Risks        []string `json:"residual_risks"`
	}
	if err := json.Unmarshal(candidate.Provenance.SubmittedAnswer, &actual); err != nil {
		t.Fatal(err)
	}
	// C02 and L3-S9-repair.md:437 keep a received fact from being silently
	// overwritten. The later actual edit is a revision or a new attempt on the
	// same original responsibility, so the received fact keeps the content that
	// was actually received.
	if len(actual.Risks) != 0 {
		t.Fatalf("a later edit silently overwrote the received fact: %s", candidate.Provenance.SubmittedAnswer)
	}
	if len(actual.EvidenceRefs) != 1 || actual.EvidenceRefs[0] != "path:logs/restored-source.txt" {
		t.Fatalf("received fact lost its original content: %s", candidate.Provenance.SubmittedAnswer)
	}
	if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packets[0].ID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
		t.Fatalf("corrected original lacks original receipt: %+v %v", receipt, err)
	}
}

func TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceDisappears(t *testing.T) {
	root, worker, plan, packet, previousState, previousJournal, _, _ := nativeS9OriginalCandidateBoundary(t)
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
	brief := nativeS9BriefPath(plan)
	in := policy.Input{AgentID: "native-original", CWD: worker, LastAssistantMessage: filepath.Join(root, brief) + "\n" + flat}
	nativeS9SplitChild(t, &in)
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		in.Event = event
		// L3-S9-repair.md:236 keeps the reception from re-reading evidence files
		// and L4-hook-acceptance-checklist.md:59/:53 removed the source
		// existence gate. A disappeared evidence source is no longer a reception
		// qualification, so the original responsibility keeps being received.
		if diagnostic := nativeS9SplitPublicEvent(t, root, in); nativeS9ReceptionIncomplete(diagnostic) {
			t.Fatalf("%s still refused the original responsibility over a disappeared evidence source: %s", event, diagnostic)
		}
		if raw, err := os.ReadFile(typed); err != nil || !bytes.Equal(raw, typedBytes) {
			t.Fatalf("%s overwrote original staged typed input: %v", event, err)
		}
	}
	// The later actual author input stays on the same original responsibility.
	// The staged Candidate now carries that later input instead of the input
	// that existed when it was staged.
	accepted := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(accepted.State)
	if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("received original responsibility has no Candidate: %v", err)
	}
	updated, err := os.ReadFile(filepath.Join(root, registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef))
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Provenance struct {
			SubmittedAnswer json.RawMessage `json:"submitted_answer"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(updated, &recorded); err != nil {
		t.Fatal(err)
	}
	var actual struct {
		ResidualRisks []string `json:"residual_risks"`
	}
	if err := json.Unmarshal(recorded.Provenance.SubmittedAnswer, &actual); err != nil || len(actual.ResidualRisks) != 1 || actual.ResidualRisks[0] != "a later professional edit" {
		t.Fatalf("received Candidate does not carry the later actual author input: %s (%v)", recorded.Provenance.SubmittedAnswer, err)
	}
}

func TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidate(t *testing.T) {
	root, worker, plan, packet, previousState, previousJournal, _, _ := nativeS9OriginalCandidateBoundary(t)
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
	var out, diagnostic bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diagnostic); code == 0 {
		t.Fatalf("technical recovery fabricated the missing staged typed input: %s %s", &out, &diagnostic)
	}
	brief := nativeS9BriefPath(plan)
	in := policy.Input{AgentID: "native-original", CWD: worker, LastAssistantMessage: filepath.Join(root, brief) + "\n" + flat}
	nativeS9SplitChild(t, &in)
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		in.Event = event
		// L3-S9-repair.md:134 keeps exactly one author path and
		// L4-hook-acceptance-checklist.md:159 removed the flat/typed
		// adaptation. The removed typed copy is not an author path, so its
		// absence no longer withholds the original responsibility.
		if message := nativeS9SplitPublicEvent(t, root, in); nativeS9ReceptionIncomplete(message) {
			t.Fatalf("%s still refused the original responsibility over a removed typed copy: %s", event, message)
		}
		if _, err := os.Stat(typed); !os.IsNotExist(err) {
			t.Fatalf("%s fabricated the missing staged typed input: %v", event, err)
		}
	}
	// The later actual author input is received on the same original
	// responsibility, so the recorded Candidate now carries that later input.
	accepted := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(accepted.State)
	if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("received original responsibility has no Candidate: %v", err)
	}
	updated, err := os.ReadFile(filepath.Join(root, registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef))
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Provenance struct {
			SubmittedAnswer json.RawMessage `json:"submitted_answer"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(updated, &recorded); err != nil {
		t.Fatal(err)
	}
	var actual struct {
		ResidualRisks []string `json:"residual_risks"`
	}
	if err := json.Unmarshal(recorded.Provenance.SubmittedAnswer, &actual); err != nil || len(actual.ResidualRisks) != 1 || actual.ResidualRisks[0] != "later input cannot replace a lost original typed attempt" {
		t.Fatalf("received Candidate does not carry the later actual author input: %s (%v)", recorded.Provenance.SubmittedAnswer, err)
	}
}
