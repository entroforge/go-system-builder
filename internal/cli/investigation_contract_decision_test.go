package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// Choices below are synthetic mechanism fixtures. No test represents actual
// human consent, professional investigation, or product repair acceptance.
func TestS8ContractHumanChoiceNormalHookWithoutMachineInput(t *testing.T) {
	root, contract := s8LeadContractTestReady(t)
	writeRepairLeadJSON(t, contract, s8LeadContractTestAnswer())
	s8LeadContractTestSave(t, root, contract)
	current := investigationLeadTestSnapshot(t, root)
	paths := investigationLeadContractDecisionPaths(root, current.State)
	if len(paths) != 1 || questionnaireMissing(paths[0]) {
		t.Fatalf("original application not prepared: %v", paths)
	}
	path := paths[0]
	var draft map[string]any
	readRepairLeadJSON(t, path, &draft)
	if len(draft) != 2 || draft["decision"] != "" || draft["reason"] != "" {
		t.Fatalf("machine DTO leaked or approval preselected: %#v", draft)
	}
	before := s8DecisionControlBytes(t, root)
	s8DecisionNormalHook(t, root, path)
	s8DecisionRequireUnchanged(t, root, before)
	writeRepairLeadJSON(t, path, map[string]any{"decision": "approve_contract", "reason": "Synthetic actual human choice for the fixed presented draft."})
	out := s8DecisionNormalHook(t, root, path)
	approved := investigationLeadTestSnapshot(t, root)
	if stringValue(investigationPointer(approved.State)["status"]) != "contract_approved" || !strings.Contains(out, "approval recorded") {
		t.Fatalf("actual choice did not follow original approval: %s", out)
	}
	approvedBytes := s8DecisionControlBytes(t, root)
	out = s8DecisionNormalHook(t, root, path)
	if !strings.Contains(out, "already recorded") {
		t.Fatalf("normal replay lost original receipt: %s", out)
	}
	s8DecisionRequireUnchanged(t, root, approvedBytes)
	writeRepairLeadJSON(t, path, map[string]any{"decision": "approve_contract", "reason": "A different decision after approval."})
	out = s8DecisionNormalHook(t, root, path)
	if !strings.Contains(out, "conflicts") {
		t.Fatalf("changed approval replay was not rejected: %s", out)
	}
	s8DecisionRequireUnchanged(t, root, approvedBytes)
}

func TestS8ContractChoiceCannotFollowChangedDraftCaseOrBytes(t *testing.T) {
	for _, fault := range []string{"professional_edit", "case_revision", "presented_bytes", "defer", "reject", "machine_fields"} {
		t.Run(fault, func(t *testing.T) {
			root, contract := s8LeadContractTestReady(t)
			writeRepairLeadJSON(t, contract, s8LeadContractTestAnswer())
			s8LeadContractTestSave(t, root, contract)
			current := investigationLeadTestSnapshot(t, root)
			path := investigationLeadContractDecisionPaths(root, current.State)[0]
			writeRepairLeadJSON(t, path, map[string]any{"decision": "approve_contract", "reason": "Synthetic choice for the earlier exact presented draft."})
			switch fault {
			case "professional_edit":
				changed := s8LeadContractTestAnswer()
				changed.ArchitectureIntent = "A different repair intention"
				writeRepairLeadJSON(t, contract, changed)
				s8LeadContractTestSave(t, root, contract)
			case "case_revision":
				_, err := investigation.UpdateCase(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), investigation.CaseRevisionRequest{ExpectedRevision: current.Revision, CaseID: stringValue(investigationPointer(current.State)["case_id"]), Operation: "case_revised", Mutate: func(doc map[string]any) error { doc["grouping_rationale"] = "new professional information"; return nil }})
				if err != nil {
					t.Fatal(err)
				}
			case "presented_bytes":
				_, document, _, _ := investigationLeadSource(root, current.State)
				final, _, _ := investigationLeadContractProjection(root, contract, document)
				if err := os.WriteFile(final, []byte("changed reviewed bytes"), 0644); err != nil {
					t.Fatal(err)
				}
			case "defer", "reject":
				choice := "defer"
				if fault == "reject" {
					choice = "reject_contract"
				}
				writeRepairLeadJSON(t, path, map[string]any{"decision": choice, "reason": "Synthetic refusal to authorize this draft."})
			case "machine_fields":
				writeRepairLeadJSON(t, path, map[string]any{"decision": "approve_contract", "approval_hash": strings.Repeat("0", 64)})
			}
			before := s8DecisionControlBytes(t, root)
			s8DecisionNormalHook(t, root, path)
			s8DecisionRequireUnchanged(t, root, before)
			if investigationPointer(investigationLeadTestSnapshot(t, root).State)["status"] == "contract_approved" {
				t.Fatal("unapproved or changed draft acquired approval")
			}
		})
	}
}

func TestS8ExplicitGrantAndDriverChoiceNormalHookWithoutMachineInput(t *testing.T) {
	for _, designated := range []string{"Main", "driver-reviewer"} {
		t.Run(designated, func(t *testing.T) {
			root, contract := s8LeadContractTestReady(t)
			writeRepairLeadJSON(t, contract, s8LeadContractTestAnswer())
			s8LeadContractTestSave(t, root, contract)
			current := investigationLeadTestSnapshot(t, root)
			grantPaths := repairDelegationApplicationPaths(root, current.State)
			if len(grantPaths) != 1 || questionnaireMissing(grantPaths[0]) {
				t.Fatalf("optional original grant not prepared: %v", grantPaths)
			}
			grantPath := grantPaths[0]
			before := s8DecisionControlBytes(t, root)
			s8DecisionNormalHook(t, root, grantPath)
			s8DecisionRequireUnchanged(t, root, before)
			writeRepairLeadJSON(t, grantPath, repairDelegationApplicationAnswer{Decision: "delegate_bounded_repair", Reviewer: designated, AllowedPaths: []string{"internal/example"}, ForbiddenPaths: []string{"internal/unrelated-private"}, MaxContracts: 2, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
			out := s8DecisionNormalHook(t, root, grantPath)
			if !strings.Contains(out, "finite grant was recorded") {
				t.Fatalf("professional human choices could not create original grant: %s", out)
			}
			current = investigationLeadTestSnapshot(t, root)
			authority, reviewer, err := investigation.ContractReviewAuthority(root, current.State)
			if err != nil || authority == "" || reviewer != designated {
				t.Fatalf("original authority not found: %s %s %v", authority, reviewer, err)
			}
			afterGrant := s8DecisionControlBytes(t, root)
			s8DecisionNormalHook(t, root, grantPath)
			s8DecisionRequireUnchanged(t, root, afterGrant)
			// A later ordinary script is not another save of the accepted human choice.
			var unrelatedOut, unrelatedDiag bytes.Buffer
			if handled, _ := runObservedQuestionnaireSaves(root, policy.Input{Event: "PostToolUse", ToolName: "Bash", CWD: root}, &unrelatedOut, &unrelatedDiag); handled {
				t.Fatalf("ordinary tool resubmitted an already recorded grant: %s %s", &unrelatedOut, &unrelatedDiag)
			}
			s8DecisionRequireUnchanged(t, root, afterGrant)
			paths := investigationLeadContractDecisionPaths(root, current.State)
			if len(paths) != 1 || !strings.Contains(paths[0], "s8-review-") || questionnaireMissing(paths[0]) {
				t.Fatalf("original Driver review not prepared: %v", paths)
			}
			reviewPath := paths[0]
			_, doc, _, _ := investigationLeadSource(root, current.State)
			finalPath, finalBytes, _ := investigationLeadContractProjection(root, contract, doc)
			guidance := investigationContractDecisionContext(root, current.State, finalBytes)
			for _, field := range []string{reviewPath, finalPath, "requirements_unchanged/business_semantics_unchanged/reversible/scope_complete", "evidence_refs and rationale"} {
				if !strings.Contains(guidance, field) {
					t.Fatalf("Driver handoff omitted %q: %s", field, guidance)
				}
			}
			if designated == "Main" {
				if !strings.Contains(guidance, "Main is the designated Driver: personally review") {
					t.Fatalf("Main reviewer ownership missing: %s", guidance)
				}
			} else {
				for _, text := range []string{"That Driver returns their own decision", "Main faithfully records that returned judgment", "A Worker save does not consume"} {
					if !strings.Contains(guidance, text) {
						t.Fatalf("non-Main handoff omitted %q: %s", text, guidance)
					}
				}
			}
			before = s8DecisionControlBytes(t, root)
			s8DecisionNormalHook(t, root, reviewPath)
			s8DecisionRequireUnchanged(t, root, before)
			supportRel := ".claude/evidence/s8-real-boundary.json"
			writeRepairLeadFile(t, root, supportRel, `{"observed":"synthetic verified boundary trace"}`)
			_, err = runtime.RecordEvidence(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), runtime.EvidenceRequest{ExpectedRevision: current.Revision, ID: "s8-boundary", Kind: "builder_report", Path: supportRel, ProducedBy: []string{designated}, ScopeRefs: []string{"boundary"}, Validator: semantic.RuntimeCandidateValidator{}})
			if err != nil {
				t.Fatal(err)
			}
			answer := investigationContractDecisionAnswer{Decision: "approve_bounded_repair", RequirementsUnchanged: true, BusinessSemanticsUnchanged: true, Reversible: true, ScopeComplete: false, EvidenceRefs: []string{"s8-boundary"}, Rationale: "Synthetic Driver traced current entry, source authority, complete scope and rollback."}
			writeRepairLeadJSON(t, reviewPath, answer)
			before = s8DecisionControlBytes(t, root)
			out = s8DecisionNormalHook(t, root, reviewPath)
			s8DecisionRequireUnchanged(t, root, before)
			if !strings.Contains(out, "four actual technical judgments") {
				t.Fatalf("missing judgment not explained: %s", out)
			}
			answer.ScopeComplete = true
			writeRepairLeadJSON(t, reviewPath, answer)
			if designated != "Main" {
				beforeWorker := s8DecisionControlBytes(t, root)
				var workerOut bytes.Buffer
				handled, code := runSavedInvestigationContractDecision(root, repairLeadSaveInput(root, reviewPath, designated), &workerOut)
				if !handled || code != 0 || !strings.Contains(workerOut.String(), "A Worker save does not consume") {
					t.Fatalf("Worker save boundary unclear: %s", &workerOut)
				}
				s8DecisionRequireUnchanged(t, root, beforeWorker)
			}
			out = s8DecisionNormalHook(t, root, reviewPath)
			approved := investigationLeadTestSnapshot(t, root)
			if stringValue(investigationPointer(approved.State)["status"]) != "contract_approved" {
				t.Fatalf("original bounded approval did not consume professional review: %s", out)
			}
			uses := mapFieldCLI(mapFieldCLI(mapFieldCLI(approved.State, "configuration"), "repair"), "delegation_uses")
			if integerValue(uses[authority]) != 1 {
				t.Fatalf("original finite budget not consumed exactly once: %v", uses)
			}
			approvedBytes := s8DecisionControlBytes(t, root)
			out = s8DecisionNormalHook(t, root, reviewPath)
			s8DecisionRequireUnchanged(t, root, approvedBytes)
			if !strings.Contains(out, "already recorded") {
				t.Fatalf("bounded replay lost receipt: %s", out)
			}
		})
	}
}

func s8DecisionNormalHook(t *testing.T, root, path string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{"file_path": path}})
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(payload), &out, &diagnostic); code != 0 {
		t.Fatalf("normal S8 save Hook failed: %d %s %s", code, &out, &diagnostic)
	}
	return out.String() + diagnostic.String()
}
func s8DecisionControlBytes(t *testing.T, root string) []byte {
	t.Helper()
	state, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return append(append(state, 0), journal...)
}
func s8DecisionRequireUnchanged(t *testing.T, root string, before []byte) {
	t.Helper()
	if !bytes.Equal(s8DecisionControlBytes(t, root), before) {
		t.Fatal("rejected, empty or repeated decision changed original Runtime/journal")
	}
}

// Ensure the system's optional preparation never alters a user's authored
// choice and does not create an extra S1 binding requirement.
func TestS8DelegationPreparationPreservesChoiceAndRequiresCurrentBinding(t *testing.T) {
	root, _ := s8LeadContractTestReady(t)
	current := investigationLeadTestSnapshot(t, root)
	context := stageRepairDelegationApplicationContext(root, policy.Input{Event: "PreToolUse"}, current)
	path := repairDelegationApplicationPaths(root, current.State)[0]
	writeRepairLeadJSON(t, path, map[string]any{"decision": "", "reviewer": "actual-reviewer", "allowed_paths": []string{"internal/example"}, "forbidden_paths": []string{}, "max_contracts": 3, "expires_at": ""})
	authored, _ := os.ReadFile(path)
	stageRepairDelegationApplicationContext(root, policy.Input{Event: "PreToolUse"}, current)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(authored, after) || !strings.Contains(context, "Optional finite") {
		t.Fatal("preparation overwrote or made finite delegation mandatory")
	}
	bound := mapFieldCLI(current.State, "bound_req")
	bound["sha256"] = strings.Repeat("f", 64)
	if samePath(repairDelegationApplicationPaths(root, current.State)[0], path) {
		t.Fatal("old finite grant application followed changed REQ hash")
	}
}
