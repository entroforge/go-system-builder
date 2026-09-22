package investigation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

type delegationFixture struct {
	f       *intakeFixture
	request investigation.ContractRequest
	grant   investigation.RepairDelegation
	review  investigation.RepairTechnicalReview
}

func newDelegationFixture(t *testing.T, mutateGrant func(*investigation.RepairDelegation), mutateReview func(*investigation.RepairTechnicalReview)) *delegationFixture {
	t.Helper()
	f := newIntakeFixture(t, []string{"finding-1"})
	setContractLifecycle(t, f)
	raw := mustRead(t, f.statePath)
	var state map[string]any
	json.Unmarshal(raw, &state)
	bound := state["bound_req"].(map[string]any)
	bound["approved_by"] = "human-owner"
	reqText := []byte("locked requirement: preserve existing semantics\n")
	bound["sha256"] = hash(reqText)
	reqPath := filepath.Join(f.root, bound["path"].(string))
	os.MkdirAll(filepath.Dir(reqPath), 0755)
	os.WriteFile(reqPath, reqText, 0644)
	for _, raw := range state["documents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == bound["id"] {
			row["sha256"] = hash(reqText)
		}
	}
	raw, _ = json.Marshal(state)
	os.WriteFile(f.statePath, raw, 0644)
	if _, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: -1, GroupingRationale: "same bounded fault"}); err != nil {
		t.Fatal(err)
	}
	prepareCaseForContractApproval(t, f)
	draftPath := writeContractDraft(t, f.root, []string{"finding-1"})
	var draft map[string]any
	json.Unmarshal(mustRead(t, draftPath), &draft)
	draft["repair_units"].([]any)[0].(map[string]any)["assertion_ids"] = []string{"all"}
	draftBytes, _ := json.Marshal(draft)
	os.WriteFile(draftPath, draftBytes, 0644)
	draftHash := hash(draftBytes)
	grant := investigation.RepairDelegation{Decision: "delegate_bounded_repair", DecisionID: "ev-grant", RuntimeID: "loop-REQ-INTAKE", REQSHA256: hash(reqText), BaselineGeneration: 3, ApprovedBy: "human-owner", Reviewer: "driver", AllowedPaths: []string{"internal/api", "frontend/client"}, ForbiddenPaths: []string{"private"}, MaxContracts: 2, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	review := investigation.RepairTechnicalReview{Decision: "approve_bounded_repair", RuntimeID: grant.RuntimeID, CaseID: draft["case_id"].(string), ContractID: draft["repair_contract_id"].(string), ApprovalHash: draftHash, ReviewedBy: "driver", DelegationEvidenceID: "ev-grant", RequirementsUnchanged: true, BusinessSemanticsUnchanged: true, Reversible: true, ScopeComplete: true, EvidenceRefs: []string{"ev-boundary"}, Rationale: "Traced the existing publication entry and transaction; no changed business rule."}
	if mutateGrant != nil {
		mutateGrant(&grant)
	}
	if mutateReview != nil {
		mutateReview(&review)
	}
	recordDelegationEvidence(t, f, "ev-boundary", "builder_report", "driver", []string{"boundary"}, map[string]any{"observed": "entry lacks projection"})
	recordDelegationEvidence(t, f, "ev-grant", "human_decision", "human-owner", []string{"s8_repair_delegation:loop-REQ-INTAKE"}, grant)
	recordDelegationEvidence(t, f, "ev-review", "repair_contract_review", "driver", []string{"s8_contract_review:" + review.CaseID}, review)
	return &delegationFixture{f, investigation.ContractRequest{ExpectedRevision: -1, CaseID: draft["case_id"].(string), ContractPath: draftPath, ApprovedBy: "driver", ApprovalHash: draftHash, ApprovalEvidenceID: "ev-review", DelegationEvidenceID: "ev-grant"}, grant, review}
}
func recordDelegationEvidence(t *testing.T, f *intakeFixture, id, kind, actor string, scope []string, value any) {
	t.Helper()
	data, _ := json.Marshal(value)
	rel := ".claude/evidence/" + id + ".json"
	os.MkdirAll(filepath.Dir(filepath.Join(f.root, rel)), 0755)
	os.WriteFile(filepath.Join(f.root, rel), data, 0644)
	if _, err := runtime.RecordEvidence(f.root, f.statePath, f.journalPath, runtime.EvidenceRequest{ExpectedRevision: -1, ID: id, Kind: kind, Path: rel, ProducedBy: []string{actor}, ScopeRefs: scope, Validator: semantic.RuntimeCandidateValidator{}}); err != nil {
		t.Fatal(err)
	}
}
func TestDelegatedApprovalBindsAuthorityAndResponseLossRetry(t *testing.T) {
	x := newDelegationFixture(t, nil, nil)
	result, err := investigation.ApproveContract(x.f.root, x.f.statePath, x.f.journalPath, x.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State["lifecycle"].(map[string]any)["phase"] != "repair_readback" {
		t.Fatal("S9 not entered")
	}
	uses := result.State["configuration"].(map[string]any)["repair"].(map[string]any)["delegation_uses"].(map[string]any)
	if uses["ev-grant"] != float64(1) && uses["ev-grant"] != 1 {
		t.Fatalf("uses=%v", uses)
	}
	cp := result.State["review"].(map[string]any)["investigation"].(map[string]any)
	var approved map[string]any
	json.Unmarshal(mustRead(t, filepath.Join(x.f.root, cp["repair_contract_ref"].(string))), &approved)
	if approved["approved_by"] != "driver" || approved["delegated_authority"].(map[string]any)["human_authorizer"] != "human-owner" {
		t.Fatal("human and reviewer conflated")
	}
	if _, err := repair.ValidateApprovedContractRef(x.f.root, repair.ContractRef{Path: cp["repair_contract_ref"].(string), SHA256: cp["repair_contract_sha256"].(string)}); err != nil {
		t.Fatalf("S9 cannot consume delegated contract: %v", err)
	}
	beforeState, beforeJournal := string(mustRead(t, x.f.statePath)), string(mustRead(t, x.f.journalPath))
	retry, err := investigation.ApproveContract(x.f.root, x.f.statePath, x.f.journalPath, x.request)
	if err != nil || retry.Revision != result.Revision || string(mustRead(t, x.f.statePath)) != beforeState || string(mustRead(t, x.f.journalPath)) != beforeJournal {
		t.Fatalf("retry mutated authority: %v", err)
	}
	x.request.ApprovedBy = "human-owner"
	if _, err := investigation.ApproveContract(x.f.root, x.f.statePath, x.f.journalPath, x.request); err == nil {
		t.Fatal("different actor reused approval")
	}
}
func TestDelegatedApprovalFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		grant  func(*investigation.RepairDelegation)
		review func(*investigation.RepairTechnicalReview)
		after  func(*testing.T, *delegationFixture)
	}{
		{name: "foreign-runtime", grant: func(g *investigation.RepairDelegation) { g.RuntimeID = "other" }},
		{name: "old-generation", grant: func(g *investigation.RepairDelegation) { g.BaselineGeneration = 2 }},
		{name: "different-req", grant: func(g *investigation.RepairDelegation) { g.REQSHA256 = strings.Repeat("a", 64) }},
		{name: "forged-human", grant: func(g *investigation.RepairDelegation) { g.ApprovedBy = "driver" }},
		{name: "no-scope", grant: func(g *investigation.RepairDelegation) { g.AllowedPaths = nil }},
		{name: "out-of-scope", grant: func(g *investigation.RepairDelegation) { g.AllowedPaths = []string{"internal/api"} }},
		{name: "forbidden-child-overlap", grant: func(g *investigation.RepairDelegation) { g.ForbiddenPaths = []string{"internal/api/private"} }},
		{name: "wildcard", grant: func(g *investigation.RepairDelegation) { g.AllowedPaths = []string{"**"} }},
		{name: "expired", grant: func(g *investigation.RepairDelegation) { g.ExpiresAt = "2020-01-01T00:00:00Z" }},
		{name: "zero-budget", grant: func(g *investigation.RepairDelegation) { g.MaxContracts = 0 }},
		{name: "changed-business", review: func(r *investigation.RepairTechnicalReview) { r.BusinessSemanticsUnchanged = false }},
		{name: "irreversible", review: func(r *investigation.RepairTechnicalReview) { r.Reversible = false }},
		{name: "incomplete-scope", review: func(r *investigation.RepairTechnicalReview) { r.ScopeComplete = false }},
		{name: "wrong-hash", review: func(r *investigation.RepairTechnicalReview) { r.ApprovalHash = strings.Repeat("a", 64) }},
		{name: "phantom-evidence", review: func(r *investigation.RepairTechnicalReview) { r.EvidenceRefs = []string{"missing"} }},
		{name: "tampered-grant", after: func(t *testing.T, x *delegationFixture) {
			os.WriteFile(filepath.Join(x.f.root, ".claude/evidence/ev-grant.json"), []byte("{}"), 0600)
		}},
		{name: "symlink-scope", after: func(t *testing.T, x *delegationFixture) {
			os.MkdirAll(filepath.Join(x.f.root, "frontend"), 0755)
			if err := os.Symlink(t.TempDir(), filepath.Join(x.f.root, "frontend/client")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "revoked", after: func(t *testing.T, x *delegationFixture) {
			mutateDelegationState(t, x, func(s map[string]any) {
				for _, raw := range s["evidence"].([]any) {
					e := raw.(map[string]any)
					if e["id"] == "ev-grant" {
						e["status"] = "invalid"
					}
				}
			})
		}},
		{name: "budget-used", after: func(t *testing.T, x *delegationFixture) {
			mutateDelegationState(t, x, func(s map[string]any) {
				s["configuration"].(map[string]any)["repair"].(map[string]any)["delegation_uses"] = map[string]any{"ev-grant": 2}
			})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newDelegationFixture(t, c.grant, c.review)
			if c.after != nil {
				c.after(t, x)
			}
			state, journal := string(mustRead(t, x.f.statePath)), string(mustRead(t, x.f.journalPath))
			if _, err := investigation.ApproveContract(x.f.root, x.f.statePath, x.f.journalPath, x.request); err == nil {
				t.Fatal("invalid delegation accepted")
			}
			if string(mustRead(t, x.f.statePath)) != state || string(mustRead(t, x.f.journalPath)) != journal {
				t.Fatal("rejection mutated Runtime")
			}
		})
	}
}
func mutateDelegationState(t *testing.T, x *delegationFixture, fn func(map[string]any)) {
	t.Helper()
	var s map[string]any
	json.Unmarshal(mustRead(t, x.f.statePath), &s)
	fn(s)
	data, _ := json.Marshal(s)
	os.WriteFile(x.f.statePath, data, 0644)
}

func TestDelegatedApprovalCannotAuthorizeGovernanceChanges(t *testing.T) {
	x := newDelegationFixture(t, func(g *investigation.RepairDelegation) { g.AllowedPaths = []string{"docs/requirements"} }, nil)
	var draft map[string]any
	json.Unmarshal(mustRead(t, x.request.ContractPath), &draft)
	draft["prospective_scope"] = []string{"docs/requirements"}
	draft["forbidden_scope"] = []string{}
	data, _ := json.Marshal(draft)
	os.WriteFile(x.request.ContractPath, data, 0644)
	x.request.ApprovalHash = hash(data)
	if _, err := investigation.ApproveContract(x.f.root, x.f.statePath, x.f.journalPath, x.request); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("governance was not rejected by scope gate: %v", err)
	}
}
func TestTechnicalReviewCannotMasqueradeAsHumanApproval(t *testing.T) {
	x := newDelegationFixture(t, nil, nil)
	x.request.DelegationEvidenceID = ""
	if _, err := investigation.ApproveContract(x.f.root, x.f.statePath, x.f.journalPath, x.request); err == nil {
		t.Fatal("technical review accepted as human decision")
	}
}

func TestDelegatedCLIApprovalOpensS9ButDoesNotBypassExecutionBarrier(t *testing.T) {
	x := newDelegationFixture(t, nil, nil)
	var out, errout bytes.Buffer
	code := cli.Run([]string{"runtime", "investigation", "contract", "approve", "--root", x.f.root, "--case-id", x.request.CaseID, "--file", x.request.ContractPath, "--approved-by", "driver", "--approval-hash", x.request.ApprovalHash, "--approval-evidence-id", "ev-review", "--delegation-evidence-id", "ev-grant"}, strings.NewReader(""), &out, &errout)
	if code != 0 {
		t.Fatalf("CLI approval failed: %s %s", out.String(), errout.String())
	}
	_, _, _, err := repair.OpenRepairSession(x.f.root, x.f.statePath, x.f.journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "driver"}, SessionID: "repair-session-delegated", CreatedBy: "driver"})
	if err != nil {
		t.Fatal(err)
	}
	_, plan, _, err := repair.CompileRepairPlan(x.f.root, x.f.statePath, x.f.journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "driver"}, PlanID: "repair-plan-delegated", CreatedBy: "driver"})
	if err != nil || len(plan.Units) != 1 {
		t.Fatalf("S9 compile: %+v %v", plan, err)
	}
	before := string(mustRead(t, x.f.statePath))
	if _, err := repair.BeginRepairExecution(x.f.root, x.f.statePath, x.f.journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "driver"}}); err == nil {
		t.Fatal("delegation bypassed missing PlanReport/red-check barrier")
	}
	if string(mustRead(t, x.f.statePath)) != before {
		t.Fatal("blocked execution mutated Runtime")
	}
}

// A project-approved policy is pinned at binding; no synthetic human_decision
// row is needed for every REQ or technical repair. Actual technical review is.
func TestBoundRepairPolicyApprovalAndTamperRejection(t *testing.T) {
	for _, scenario := range []string{"approve", "changed-policy", "missing-pin", "other-generation", "other-runtime", "other-req"} {
		t.Run(scenario, func(t *testing.T) {
			x := newDelegationFixture(t, nil, nil)
			data, _ := json.Marshal(map[string]any{"version": "1", "approved_by": x.grant.ApprovedBy, "reviewer": x.grant.Reviewer, "allowed_paths": x.grant.AllowedPaths, "forbidden_paths": x.grant.ForbiddenPaths, "max_contracts": 2})
			rel := ".claude/approved-repair-policy.json"
			os.WriteFile(filepath.Join(x.f.root, rel), data, 0600)
			sha := hash(data)
			var s map[string]any
			json.Unmarshal(mustRead(t, x.f.statePath), &s)
			s["configuration"].(map[string]any)["repair"].(map[string]any)["bound_policy"] = map[string]any{"path": rel, "sha256": sha, "approved_by": x.grant.ApprovedBy, "runtime_id": x.grant.RuntimeID, "req_sha256": x.grant.REQSHA256, "baseline_generation": x.grant.BaselineGeneration}
			pin := s["configuration"].(map[string]any)["repair"].(map[string]any)["bound_policy"].(map[string]any)
			switch scenario {
			case "missing-pin":
				delete(s["configuration"].(map[string]any)["repair"].(map[string]any), "bound_policy")
			case "other-generation":
				pin["baseline_generation"] = x.grant.BaselineGeneration + 1
			case "other-runtime":
				pin["runtime_id"] = "loop-other"
			case "other-req":
				pin["req_sha256"] = strings.Repeat("e", 64)
			}
			raw, _ := json.Marshal(s)
			os.WriteFile(x.f.statePath, raw, 0600)
			id := "binding-policy:" + sha
			x.request.DelegationEvidenceID = id
			x.review.DelegationEvidenceID = id
			// Refresh the fixture's registered technical evidence with a new immutable id.
			x.request.ApprovalEvidenceID = "ev-policy-review"
			recordDelegationEvidence(t, x.f, "ev-policy-review", "repair_contract_review", "driver", []string{"s8_contract_review:" + x.request.CaseID}, x.review)
			if scenario == "changed-policy" {
				os.WriteFile(filepath.Join(x.f.root, rel), append(data, ' '), 0600)
			}
			_, err := investigation.ApproveContract(x.f.root, x.f.statePath, x.f.journalPath, x.request)
			if scenario != "approve" {
				if err == nil {
					t.Fatal("changed policy accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
