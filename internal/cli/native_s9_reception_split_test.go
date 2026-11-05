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

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
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
			// A declared professional field must carry the value its own type
			// allows. A number in a string field is a real field error, so the
			// original professional validator must reject it before creating or
			// staging a Candidate. A null optional reference is a different
			// case: the author-side schema allows it, so it is a receivable
			// "not applicable" statement of the author
			// (TestNativeS9PublicAuthorKeepsNullOptionalReference).
			invalid["migration_ref"] = 42
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

// TestNativeS9PublicAuthorKeepsNullOptionalReference is the CLI end-to-end
// counterpart of the optional-reference rule.
//
// A null optional reference is the author's own "not applicable" statement: the
// author-side schema allows it, so the answer is received and stored as it was
// written, and the consumer never rewrites the author's null into an explicit
// empty value
// (internal/schema/assets/stage-answer-s9-result.schema.json:84-85;
// L3-S9-repair.md:198 "不适用时可为空字符串"; L4-workflow-convergence.md:106
// "没有表达判断的空值通常保留草稿"; L4-workflow-convergence.md:154
// "可选字段、字段拼法和枚举不得把已有真实观察变成拒收理由").
func TestNativeS9PublicAuthorKeepsNullOptionalReference(t *testing.T) {
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
	var authored map[string]any
	if err := json.Unmarshal(body, &authored); err != nil {
		t.Fatal(err)
	}
	authored["migration_ref"] = nil
	if err := persistRepairLeadQuestionnaire(flat, authored); err != nil {
		t.Fatal(err)
	}
	in.Event, in.CWD = "SubagentStop", worker
	nativeS9SplitChild(t, &in)
	if diagnostic := nativeS9SplitPublicEvent(t, root, in); nativeS9ReceptionIncomplete(diagnostic) {
		t.Fatalf("a null optional reference refused the original responsibility: %s", diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil || registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("the null optional reference did not create its Candidate: %v", err)
	}
	candidateBytes, err := os.ReadFile(filepath.Join(root, registry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef))
	if err != nil {
		t.Fatal(err)
	}
	var candidate repair.WorkspaceCandidate
	if err := json.Unmarshal(candidateBytes, &candidate); err != nil || candidate.Provenance == nil {
		t.Fatalf("original Candidate provenance: %v", err)
	}
	// The author's own null is stored as it was written.
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(candidate.Provenance.SubmittedAnswer, &stored); err != nil {
		t.Fatal(err)
	}
	raw, ok := stored["migration_ref"]
	if !ok {
		t.Fatalf("the received answer lost the optional reference: %s", candidate.Provenance.SubmittedAnswer)
	}
	if string(raw) != "null" {
		t.Fatalf("the consumer rewrote the author's null optional reference: %s", candidate.Provenance.SubmittedAnswer)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, candidate.Provenance.PacketID); err != nil || !receipt.Accepted || receipt.ResponsibilityStatus != "candidate_accepted" {
		t.Fatalf("the null optional reference lacks its original receipt: %+v %v", receipt, err)
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
			// An explicit Main technical recovery delivers the original as soon
			// as its delivery conditions hold. The flat author path is the one
			// original of this responsibility, so the recovery delivers it, names
			// its exact coordinate, and takes the current author's professional
			// content — never the historical staged copy
			// (L4-hook-platform-wiring.md:369 "Main／系统的具名恢复入口复用同一
			// 最小接收核心，只读当前原卷一次"; L3-S9-repair.md:236 "只读取该作者
			// 该责任的原答卷，单次完成解析").
			var out bytes.Buffer
			if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diagnostic); code != 0 {
				t.Fatalf("technical recovery did not deliver the current original: %s %s", &out, &diagnostic)
			}
			if !strings.Contains(out.String(), flat) {
				t.Fatalf("technical recovery did not name the current original coordinate %s: %s", flat, &out)
			}
			if !strings.Contains(out.String(), "Original S9 Result received") {
				t.Fatalf("technical recovery did not report the received original: %s", &out)
			}
			// The historical staged typed copy is not a second original. The
			// recovery reads the current original once and must not name or
			// deliver the staged copy.
			if strings.Contains(out.String(), typed) {
				t.Fatalf("technical recovery named the historical staged typed copy %s as the received original: %s", typed, &out)
			}
			if raw, err := os.ReadFile(typed); err != nil || !bytes.Equal(raw, oldTyped) {
				t.Fatalf("technical retry changed the saved input: %v", err)
			}
			// The delivered contribution is the current author's real
			// professional content. The historical staged copy's evidence never
			// enters it.
			delivered := repairDispatchSnapshot(t, root)
			deliveredRegistry, err := workspace.Decode(delivered.State)
			if err != nil || deliveredRegistry == nil || deliveredRegistry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef == "" {
				t.Fatalf("technical recovery delivered no Candidate: %v", err)
			}
			deliveredBytes, err := os.ReadFile(filepath.Join(root, deliveredRegistry.Executions[plan.Binding.ManifestAssignmentID].DeliveryRef))
			if err != nil {
				t.Fatal(err)
			}
			var deliveredRecord struct {
				Provenance struct {
					SubmittedAnswer json.RawMessage `json:"submitted_answer"`
				} `json:"provenance"`
			}
			if err := json.Unmarshal(deliveredBytes, &deliveredRecord); err != nil {
				t.Fatal(err)
			}
			var deliveredContent struct {
				EvidenceRefs  []string `json:"evidence_refs"`
				ResidualRisks []string `json:"residual_risks"`
			}
			if err := json.Unmarshal(deliveredRecord.Provenance.SubmittedAnswer, &deliveredContent); err != nil {
				t.Fatal(err)
			}
			if len(deliveredContent.ResidualRisks) != 1 || deliveredContent.ResidualRisks[0] != professional.ResidualRisks[0] {
				t.Fatalf("technical recovery did not deliver the current author's professional content: %s", deliveredRecord.Provenance.SubmittedAnswer)
			}
			for _, ref := range deliveredContent.EvidenceRefs {
				if ref == "path:logs/historical-missing.txt" {
					t.Fatalf("technical recovery adopted the historical staged typed copy: %s", deliveredRecord.Provenance.SubmittedAnswer)
				}
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
	// The retired typed copy is concretely absent at the OS level. Everything
	// below asserts that this absence is a real OS fact and not an unreferenced
	// path: the reception never reads it, never reports it and never
	// reconstructs it.
	if _, statErr := os.Stat(typed); !os.IsNotExist(statErr) {
		t.Fatalf("the staged typed copy is not absent at the OS level: %v", statErr)
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
	// The flat author path is the one original of this responsibility. The
	// retired typed copy is not an author path, so its absence does not
	// withhold the original: the technical recovery delivers the current
	// original and reports the received contribution
	// (L4-hook-acceptance-checklist.md:159 G27 "不再适配 flat／typed 或恢复 C／D
	// 资格"; L4-hook-platform-wiring.md:369 "只读当前原卷一次").
	var out, diagnostic bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diagnostic); code != 0 {
		t.Fatalf("technical recovery did not deliver the current original: %s %s", &out, &diagnostic)
	}
	// (1) The delivery names the current author path — the flat author path is
	// the one original of this responsibility — and never a second original.
	if !strings.Contains(out.String(), flat) {
		t.Fatalf("technical recovery did not name the current original coordinate %s: %s", flat, &out)
	}
	if strings.Contains(out.String(), typed) {
		t.Fatalf("technical recovery named the retired typed copy %s as a second original: %s", typed, &out)
	}
	if !strings.Contains(out.String(), "Original S9 Result received") {
		t.Fatalf("technical recovery did not report the received original: %s", &out)
	}
	// (2) "WithoutDiagnostic": the missing retired typed copy is not a defect and
	// must not produce any blocking or missing-item wording, and the author is
	// never asked to hand the work in again.
	for _, wording := range []string{"typed", "missing", "blocked", "not submitted", "resubmit", "hand in"} {
		if strings.Contains(out.String(), wording) {
			t.Fatalf("technical recovery reported the retired typed copy as a defect (%q): %s", wording, &out)
		}
	}
	// (3) No recovery command is handed out at all. A command that points at
	// the retired typed copy would point at material that does not exist
	// (L4-hook-platform-wiring.md:373 "不输出指向失效输入的假恢复命令").
	for _, command := range []string{"stage-answer consume", "consume --path", "consume --root"} {
		if strings.Contains(out.String(), command) || strings.Contains(diagnostic.String(), command) {
			t.Fatalf("technical recovery handed out a recovery command %q for the retired typed copy: %s %s", command, &out, &diagnostic)
		}
	}
	// (4) The retired typed copy is not adapted back into existence.
	if _, err := os.Stat(typed); !os.IsNotExist(err) {
		t.Fatalf("technical recovery fabricated the missing staged typed input: %v", err)
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
		message := nativeS9SplitPublicEvent(t, root, in)
		if nativeS9ReceptionIncomplete(message) {
			t.Fatalf("%s still refused the original responsibility over a removed typed copy: %s", event, message)
		}
		// The natural events hand out no recovery command either.
		for _, command := range []string{"stage-answer consume", "consume --path"} {
			if strings.Contains(message, command) {
				t.Fatalf("%s handed out a recovery command %q for the retired typed copy: %s", event, command, message)
			}
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

// TestNativeS9MalformedOriginalResultKeepsParseReasonWithAcceptedPlan pins the
// malformed original Result branch.
//
// An already accepted PlanReport receipt is a preserved fact, so the unfinished
// Result keeps both coordinates. The concrete parse reason of the actual file
// travels with them as the original consumer's own diagnostic; the two
// coordinates alone would hide which material failed and why
// (L3-S9-repair.md:238 "反馈实际文件与字段"; :255 "沿原准备恢复必要引导";
// L4-hook-platform-wiring.md:373 "明确缺失与已有事实").
func TestNativeS9MalformedOriginalResultKeepsParseReasonWithAcceptedPlan(t *testing.T) {
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, plan, service, &diagnostic, nil); err != nil {
		t.Fatal(err)
	}
	if r, err := service.SaveRepairPlanReportFrom(t.Context(), plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !r.Accepted {
		t.Fatalf("original Plan receipt: %#v %v", r, err)
	}
	planPath, result, err := nativeS9AuthorPaths(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	flat := filepath.Join(worker, result)
	damaged := []byte(`{"verdict":`)
	if err := os.WriteFile(flat, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diag); code == 0 {
		t.Fatalf("a malformed original Result was accepted as an original document: %s %s", &out, &diag)
	}
	text := out.String() + diag.String()
	// The concrete parse reason of the actual file is the consumer diagnostic.
	parseReason := workpacket.ValidateUniqueJSON(damaged)
	if parseReason == nil {
		t.Fatal("the malformed original is not a parse problem at all")
	}
	if !strings.Contains(text, parseReason.Error()) {
		t.Fatalf("the malformed Result feedback lost the concrete parse reason %q: %s", parseReason, text)
	}
	if !strings.Contains(text, "actual native Result at "+flat) {
		t.Fatalf("the malformed Result feedback did not name the actual file %s: %s", flat, text)
	}
	// The unfinished Result keeps both coordinates of the original
	// responsibility, and the concrete parse reason travels with them.
	var partial *nativeS9PlanAcceptedIncomplete
	structural := receiveNativeS9BuilderFrom(t.Context(), root, repairDispatchSnapshot(t, root).State, plan, worker, true, &bytes.Buffer{}, nil)
	if !errors.As(structural, &partial) {
		t.Fatalf("the unfinished Result did not keep the accepted Plan coordinates: %v", structural)
	}
	if partial.plan != filepath.Join(worker, planPath) || partial.result != flat {
		t.Fatalf("the unfinished Result lost a coordinate: plan=%q result=%q", partial.plan, partial.result)
	}
	if !strings.Contains(structural.Error(), parseReason.Error()) {
		t.Fatalf("the unfinished Result lost the concrete parse reason %q: %v", parseReason, structural)
	}
	// No recovery command points at the damaged material.
	for _, command := range []string{"stage-answer consume", "consume --path"} {
		if strings.Contains(text, command) {
			t.Fatalf("the malformed Result feedback handed out a recovery command %q: %s", command, text)
		}
	}
}

// s9ImpactOwnedPointerFacts are the Runtime repair-pointer fields that the
// original ChangeImpact reception owns and may therefore write. Every other
// registered repair fact belongs to the already received S9 work and must stay
// untouched by a read-only replay.
var s9ImpactOwnedPointerFacts = map[string]bool{
	"impact_ref":                  true,
	"status":                      true,
	"required_reverification_ids": true,
	"updated_at":                  true,
	"next_action":                 true,
}

// repairDispatchAssertOnlyImpactReception replaces the retired whole-Runtime
// byte freeze on the public Main progress replay.
//
// The empty original ChangeImpact template may be received along this very
// event: "空 Impact ... 可沿 Pre、成功 Post 或 Stop 按当前原卷直接接收 ...
// 允许可能提前接收或准备后继" (L4-workflow-convergence.md:110). The Runtime
// therefore does change on this replay — by exactly that one original Impact
// reception and by nothing else.
//
// The replacement is stronger than the retired byte freeze in four ways:
//  1. it names the single permitted Runtime event instead of freezing the whole
//     Runtime byte pair;
//  2. it pins every already received S9 fact — the accepted PlanReport receipt,
//     the accepted Result, the Candidate, the Final, the registered ownership
//     and the original responsibility — as unchanged;
//  3. it reverse-asserts that the replay received no second Impact and
//     advanced the lifecycle no further than the original Impact successor;
//  4. it states the expected reception per scenario, so a replay that silently
//     stops receiving the original Impact is a failure too.
func repairDispatchAssertOnlyImpactReception(t *testing.T, root string, before [][]byte, wantImpactReception bool) {
	t.Helper()
	after := repairDispatchRuntimeBytes(t, root)

	// (1) The only Runtime events this replay may write are the original
	//     Impact reception and the graph milestone that its own successor
	//     produced. No Result, PlanReport, Candidate, Final, work packet,
	//     workspace or handoff event may be written by this replay.
	beforeEvents := repairDispatchJournalEvents(t, before[1])
	afterEvents := repairDispatchJournalEvents(t, after[1])
	if len(afterEvents) < len(beforeEvents) {
		t.Fatalf("read-only replay dropped %d Runtime events", len(beforeEvents)-len(afterEvents))
	}
	added := afterEvents[len(beforeEvents):]
	impacts, transitions := 0, 0
	for _, event := range added {
		if stringValue(event["request_id"]) == "s9-impact-reconcile" {
			impacts++
			continue
		}
		transitions++
	}
	if wantImpactReception {
		if impacts != 1 {
			t.Fatalf("read-only replay wrote %d original Impact receptions, want exactly the one this event observes (added %s)", impacts, s9PointerFactJSON(added))
		}
		if transitions > 1 {
			t.Fatalf("read-only replay wrote %d graph transitions, want at most the original Impact successor", transitions)
		}
	} else if len(added) != 0 {
		t.Fatalf("read-only replay wrote %d Runtime events, want none", len(added))
	}

	// (2) Every already received S9 fact stays exactly as it was.
	beforeState := repairDispatchStateJSON(t, before[0])
	afterState := repairDispatchStateJSON(t, after[0])
	beforePointer := repairPointerCLI(beforeState)
	afterPointer := repairPointerCLI(afterState)
	for key, want := range beforePointer {
		if s9ImpactOwnedPointerFacts[key] {
			continue
		}
		got, ok := afterPointer[key]
		if !ok || s9PointerFactJSON(got) != s9PointerFactJSON(want) {
			t.Fatalf("read-only replay changed the accepted S9 fact %q: %s -> %s (pointer now %s)", key, s9PointerFactJSON(want), s9PointerFactJSON(got), s9PointerFactJSON(afterPointer))
		}
	}

	// (3) The lifecycle may advance only to the original Impact successor.
	beforePhase := lifecycleState(beforeState)
	afterPhase := lifecycleState(afterState)
	if afterPhase != beforePhase && afterPhase != "targeted_reverification" {
		t.Fatalf("read-only replay advanced the lifecycle from %q to %q; only the original Impact successor may move it", beforePhase, afterPhase)
	}

	// (4) The reception that happened is registered as the original Impact
	//     fact, and no second one was received.
	if len(added) == 1 {
		if stringValue(afterPointer["impact_ref"]) == "" {
			t.Fatalf("the original Impact reception is not registered in the Runtime repair pointer")
		}
		if stringValue(beforePointer["impact_ref"]) != "" && stringValue(beforePointer["impact_ref"]) == stringValue(afterPointer["impact_ref"]) {
			t.Fatalf("the replay reused the already received Impact %q instead of committing a distinct reception", stringValue(afterPointer["impact_ref"]))
		}
	}
}

// repairDispatchJournalEvents decodes the Runtime journal into its recorded
// events. It never repairs a damaged journal: a damaged line fails the test.
func repairDispatchJournalEvents(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("Runtime journal line is not readable: %v", err)
		}
		events = append(events, event)
	}
	return events
}

// repairDispatchStateJSON decodes a captured Runtime state document.
func repairDispatchStateJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	state := map[string]any{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("Runtime state document is not readable: %v", err)
	}
	return state
}

// s9PointerFactJSON renders one registered repair fact for an exact comparison.
func s9PointerFactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

// TestS9TypedSelectorRejectsChangedAttemptFence pins the execution fence on
// the issued S9 Result package.
//
// The issued attempt froze its own execution definition. A registered
// execution that no longer agrees on a structural coordinate is a different
// attempt, so the issued package must not be delivered as the current one.
// The sibling blocker path reconciles the same coordinates against the same
// frozen definition (internal/repair/issued_repair_result_blocker.go).
func TestS9TypedSelectorRejectsChangedAttemptFence(t *testing.T) {
	root, _, e, packet, _ := s9NativeIntegrationCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	// The issued attempt itself still resolves its own frozen definition: the
	// fence rejects a drifted attempt, not the issued one.
	if _, err := runtime.RepairResultExecution(snap.State, packet); err != nil {
		t.Fatalf("the issued attempt does not resolve its own frozen execution: %v", err)
	}
	row := mapFieldCLI(mapFieldCLI(snap.State, "workspace"), "executions")[e.AssignmentID].(map[string]any)
	row["execution_generation"] = e.Generation + 1
	selected, found, err := typedRepairResultPacketForAssignmentContext(context.Background(), root, snap.State, e.AssignmentID, e.AgentID)
	if err == nil || !found || selected.ID != packet.ID {
		t.Fatalf("changed attempt lost typed fence: %t %v", found, err)
	}
	// The fence names the drifted coordinate, so the refusal is specific
	// rather than a generic rejection.
	if _, err := runtime.RepairResultExecution(snap.State, selected); err == nil {
		t.Fatal("changed generation accepted as original attempt")
	} else if !strings.Contains(err.Error(), "execution_generation") {
		t.Fatalf("the attempt fence does not name the drifted coordinate: %v", err)
	}
}

// TestNativeS9OriginalReceptionDoesNotQualifyByGit pins that the original
// reception does not qualify the author's answer by the repository state of
// its authoring location. Git is an integration coordinate that Main resolves
// when the integration begins, not an answer qualification
// (L4-hook-platform-wiring.md:81 "不另遍历 Git worktree、比对 HEAD／branch／pin
// 或认证 actor 与来源根来授予答卷资格"; L4-agent-dispatch-governance.md:57 "S7～S9
// 接收也不核 Git／ignore 状态"; L3-S9-repair.md:136 "原稿是否 tracked、staged、
// ignored 或 Git `??` 均不属于答卷接收资格").
func TestNativeS9OriginalReceptionDoesNotQualifyByGit(t *testing.T) {
	root, worker, packet, input, _ := nativeS9WorkerFixture(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 97\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input.Event, input.CWD = "SubagentStop", worker
	var output, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &output, &diagnostic); !handled || code != 0 {
		t.Fatalf("Candidate reception: %t %d %s %s", handled, code, &output, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("registered original responsibility: %v", err)
	}
	recorded, ok := registry.Executions[packet.Binding.ManifestAssignmentID]
	if !ok {
		t.Fatal("the original responsibility did not register its authoring location")
	}
	// The authoring location is registered even though Git is unavailable.
	if !samePath(recorded.Path, worker) {
		t.Fatalf("the original authoring location was not registered: %q", recorded.Path)
	}
	// An unresolvable branch stays unknown. It is never fabricated.
	if recorded.Branch != "" {
		t.Fatalf("an unresolvable Git branch was recorded as a fact: %q", recorded.Branch)
	}
	// The received Result survives and the reception records no Main
	// completion of its own.
	contributions, err := repair.ReceivedRepairContributions(root, snapshot.State)
	if err != nil || len(contributions) != 1 {
		t.Fatalf("accepted contribution: %+v %v; diagnostic: %s", contributions, err, &diagnostic)
	}
	pointer := repairPointerCLI(snapshot.State)
	if stringValue(pointer["result_ref"]) != "" || stringValue(pointer["impact_ref"]) != "" {
		t.Fatalf("the original reception invented Main completion: %+v", pointer)
	}
}

// TestNativeS9AcceptedOriginalReplayIsIdempotent pins the professional
// semantic comparison on an accepted original: an equivalent current original
// is idempotent rather than a conflict, and it neither receives again nor
// rewrites the accepted facts
// (L4-workflow-convergence.md:112 "等价只跳过重复接收与写入"; L4-hook-platform-wiring.md:126
// "已收且责任已结束、没有新尝试和未完后继时不读私人原稿并快速返回").
func TestNativeS9AcceptedOriginalReplayIsIdempotent(t *testing.T) {
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := receiveNativeS9ReadyPlan(t.Context(), root, worker, plan, service, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	_, result, err := nativeS9AuthorPaths(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	flat := filepath.Join(worker, result)
	var out, diagnostic bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &out, &diagnostic); code != 0 {
		t.Fatalf("original reception: %d %s %s", code, &out, &diagnostic)
	}
	before := repairDispatchRuntimeBytes(t, root)
	// The very same original content is replayed. It is idempotent: no
	// conflict, no second reception and no rewritten accepted fact.
	var replayOut, replayDiagnostic bytes.Buffer
	if code := runStageAnswer([]string{"consume", "--root", root, "--path", flat}, &replayOut, &replayDiagnostic); code != 0 {
		t.Fatalf("equivalent original replay was refused: %d %s %s", code, &replayOut, &replayDiagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}
