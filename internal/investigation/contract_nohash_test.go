package investigation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestContractApprovalUsesPresentedActualObject(t *testing.T) {
	root := t.TempDir()
	draft := []byte(`{"repair_contract_id":"repair-contract-a","case_id":"investigation-case-a","revision":2,"status":"draft","architecture_intent":"actual intent"}`)
	var original map[string]any
	if err := json.Unmarshal(draft, &original); err != nil {
		t.Fatal(err)
	}
	artifact := map[string]any{"decision": "approve_contract", "decision_id": "decision-original", "runtime_id": "runtime-original", "case_id": "investigation-case-a", "contract_id": "repair-contract-a", "contract_draft": original, "approval_hash": "outdated-history"}
	encoded, _ := json.Marshal(artifact)
	if err := os.WriteFile(filepath.Join(root, "decision.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	item := map[string]any{"id": "decision-original", "path": "decision.json", "sha256": "old-ignored"}
	state := map[string]any{"runtime_id": "runtime-original"}
	if err := validateContractApprovalArtifact(root, item, state, "investigation-case-a", "repair-contract-a", draft); err != nil {
		t.Fatalf("actual decision and original presented contract: %v", err)
	}
	changed := []byte(`{"repair_contract_id":"repair-contract-a","case_id":"investigation-case-a","revision":2,"status":"draft","architecture_intent":"different intent"}`)
	if err := validateContractApprovalArtifact(root, item, state, "investigation-case-a", "repair-contract-a", changed); err == nil {
		t.Fatal("old decision must not authorize materially different actual content")
	}
	delete(artifact, "contract_draft")
	encoded, _ = json.Marshal(artifact)
	if err := os.WriteFile(filepath.Join(root, "decision.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateContractApprovalArtifact(root, item, state, "investigation-case-a", "repair-contract-a", draft); err == nil {
		t.Fatal("missing original presented object must remain unknown")
	}
}

func TestHistoricalAuthorityMachineDigestsAreIgnoredDuringDecode(t *testing.T) {
	var grant RepairDelegation
	if err := decodeStrict([]byte(`{"decision":"delegate_bounded_repair","runtime_id":"runtime-original","req_sha256":{"legacy":true}}`), &grant); err != nil {
		t.Fatalf("legacy grant digest blocked the original authority: %v", err)
	}
	if grant.Decision != "delegate_bounded_repair" || grant.RuntimeID != "runtime-original" {
		t.Fatalf("authority identity changed: %+v", grant)
	}
	var review RepairTechnicalReview
	if err := decodeStrict([]byte(`{"decision":"approve_bounded_repair","case_id":"case-original"}`), &review); err != nil {
		t.Fatalf("legacy review digest blocked the original decision: %v", err)
	}
	if review.Decision != "approve_bounded_repair" || review.CaseID != "case-original" {
		t.Fatalf("review identity changed: %+v", review)
	}
	if err := decodeStrict([]byte(`{"decision":"approve_bounded_repair","unexpected_authority":true}`), &review); err != nil || review.Decision != "approve_bounded_repair" {
		t.Fatal("unrelated fields must not replace the actual authority decision")
	}
}
