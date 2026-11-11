package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// A new Session and Plan use the already-approved contract and the actual
// responsibility. They do not need a Git checkout, content hashes or a scan
// of the repository to construct an execution baseline.
func TestApprovedContractSessionPlanWithoutDigests(t *testing.T) {
	root := t.TempDir()
	contract := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-one", "case_id": "investigation-case-one", "revision": 1, "status": "approved",
		"source_finding_ids": []string{"finding-one"}, "root_cause_statement": "boundary", "violated_invariant": "correct result",
		"causal_model_ref": "case://one", "architecture_intent": "restore boundary",
		"repair_units":      []map[string]any{{"id": "one", "description": "repair boundary", "scope": []string{"internal/a"}, "assertion_ids": []string{"symptom-1"}}},
		"prospective_scope": []string{"internal/a"}, "forbidden_scope": []string{}, "symptom_assertions": []string{"symptom"},
		"root_invariant_assertions": []string{"invariant"}, "detection_gap_assertions": []string{"regression"},
		"stop_escalation_conditions": []string{"scope change"}, "approved_by": "human", "approved_at": "2026-01-01T00:00:00Z",
	}
	data, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "contract.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	session, sessionRef, err := CreateRepairSession(root, SessionRequest{Contract: ContractRef{Path: "contract.json"}, SessionID: "repair-session-one", RuntimeID: "loop-REQ-001", ReqID: "REQ-001", BaselineGeneration: 1, CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(session.BaselineArtifacts) != 0 {
		t.Fatalf("session registered digest/baseline: %+v %+v", session, sessionRef)
	}
	plan, planRef, err := CreateRepairPlan(root, PlanRequest{Contract: ContractRef{Path: "contract.json"}, Session: sessionRef, PlanID: "repair-plan-one", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SessionID != session.SessionID {
		t.Fatalf("plan reference or responsibility invalid: %+v %+v", plan, planRef)
	}
	for _, rel := range []string{sessionRef.Path, planRef.Path} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"contract_sha256", "baseline_digest", "baseline_artifacts", "sha256"} {
			if _, present := object[key]; present {
				t.Fatalf("new %s registered %s", rel, key)
			}
		}
	}
}

// A blocked original responsibility and its Main resolution are distinct
// events, retaining the user's actual text without introducing hash metadata.
func TestBlockerResolutionUsesOriginalResponsibilityWithoutDigests(t *testing.T) {
	packet := workpacket.RepairResultPacket{ID: "packet-" + strings.Repeat("long-id-", 50)}
	original := map[string]any{"message_type": "blocker_report", "s9_blocker": map[string]any{"answer_sha256": strings.Repeat("a", 64)}}
	blocker := runtimepkg.RepairResultBlockerReceipt{ArtifactRef: ".claude/old/blocker.json"}
	message, err := buildRepairResultResolutionEnvelope(original, packet, blocker, "SHA-256 is mentioned in the business task", []any{}, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if message["body"] != "SHA-256 is mentioned in the business task" {
		t.Fatalf("professional text changed: %v", message["body"])
	}
	if _, exists := message["s9_blocker"]; exists {
		t.Fatal("old blocker digest metadata copied into new message")
	}
	resolution, ok := message["s9_resolution"].(map[string]any)
	if !ok || resolution["packet_id"] != packet.ID || resolution["blocker_ref"] != blocker.ArtifactRef {
		t.Fatalf("original responsibility lost: %#v", message)
	}
	for _, key := range []string{"packet_sha256", "blocker_sha256", "execution_sha256"} {
		if _, exists := resolution[key]; exists {
			t.Fatalf("resolution registered %s", key)
		}
	}
	for _, component := range strings.Split(s9PacketEvidenceDirectory(packet.ID), "/") {
		if len(component) > 120 {
			t.Fatalf("unbounded path component: %d", len(component))
		}
	}
}
