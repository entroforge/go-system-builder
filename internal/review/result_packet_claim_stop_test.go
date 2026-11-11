package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Start with a real issued questionnaire. The author supplies observations
// and paths, never the complete internal Result envelope or evidence hashes.
func issuedClaimStopFixture(t *testing.T) (string, string, string, workpacket.ReviewResultPacket, IssuedSubmitRequest) {
	t.Helper()
	root, statePath, journalPath, _, _ := packetModeFixture(t)
	var state map[string]any
	if err := json.Unmarshal(mustReadTestFile(t, statePath), &state); err != nil {
		t.Fatal(err)
	}
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" {
			agent["state"], agent["dispatch_mode"] = "reading", "plan_checkpoint"
			agent["plan_reported_ref"], agent["activation_ref"], agent["readback_ref"] = nil, nil, nil
		}
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue QA packet: %v (%d packets)", err, len(packets))
	}
	p := packets[0]
	return root, statePath, journalPath, p, IssuedSubmitRequest{ExpectedRevision: -1, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: p.Binding.OwnerAgentID}
}

// Each negative case gets its own copy of a genuinely issued Git and Runtime
// fixture, so a rejected answer cannot leave artifacts for another case.
func cloneIssuedClaimStopFixture(t *testing.T, root, statePath, journalPath string, packet workpacket.ReviewResultPacket, request IssuedSubmitRequest) (string, string, string, workpacket.ReviewResultPacket, IssuedSubmitRequest) {
	t.Helper()
	clone := t.TempDir()
	if err := os.CopyFS(clone, os.DirFS(root)); err != nil {
		t.Fatalf("clone issued fixture: %v", err)
	}
	stateRel, err := filepath.Rel(root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalRel, err := filepath.Rel(root, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	previous := fixtureEvidenceRoot
	fixtureEvidenceRoot = clone
	t.Cleanup(func() { fixtureEvidenceRoot = previous })
	return clone, filepath.Join(clone, stateRel), filepath.Join(clone, journalRel), packet, request
}

func claimStopAnswer(t *testing.T, severity string) map[string]any {
	t.Helper()
	finding := codeInspectionFinding("finding-safety-stop", "claim-qa-1")
	finding.Severity = severity
	finding.Encounter.LastGoodCheckpoint = ""
	finding.Encounter.CaptureGaps = []string{"stopped the remaining checks before exercising the data-destructive operation"}
	if severity != "P0" {
		finding.Encounter.LastGoodCheckpoint = "caller still preserves the requested state"
	}
	data, _ := json.Marshal(finding)
	var professional map[string]any
	_ = json.Unmarshal(data, &professional)
	delete(professional, "schema_version")
	delete(professional, "lens")
	delete(professional, "finding_id")
	ref := strings.SplitN(finding.EvidenceRefs[0], "#sha256=", 2)[0]
	professional["evidence_refs"] = []any{ref}
	return map[string]any{
		"claim_results":  []any{map[string]any{"claim_id": "claim-qa-1", "conclusion": "fail", "method": "trace the actual mutation path", "observed": "the failed operation persists a destructive write", "evidence_refs": []any{ref}}},
		"blocked_claims": []any{}, "checks": []any{}, "findings": []any{professional},
		"deviations": []any{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "finding",
	}
}

func saveClaimStopAnswer(t *testing.T, root string, p workpacket.ReviewResultPacket, answer map[string]any) []byte {
	t.Helper()
	p.Draft.Answer, _ = json.Marshal(answer)
	data, _ := json.Marshal(p.Draft)
	if err := os.WriteFile(filepath.Join(root, p.AnswerPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestIssuedP0PartialClaimStopPreservesUnobservedAndReplay(t *testing.T) {
	root, statePath, journalPath, packet, request := issuedClaimStopFixture(t)
	authored := saveClaimStopAnswer(t, root, packet, claimStopAnswer(t, "P0"))
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("P0 must not require unsafe remaining Claims: %v", err)
	}
	if ptr := PlanPointerFromState(accepted.State); ptr.Status != "observation_sealed" {
		t.Fatalf("P0 did not seal: %+v", ptr)
	}
	// The P0 answer preserves the actual unfinished own and other responsibilities.
	unobserved := []string{"claim-dv-1", "claim-qa-2"}
	for _, id := range unobserved {
		d := Dispositions(accepted.State)[id]
		if d.ResultID != "" || d.Disposition == "pass" || d.Disposition == "blocked" {
			t.Fatalf("unobserved Claim fabricated a result: %+v", d)
		}
	}
	if err := loopruntime.ValidateCheckpointIndices(root, accepted.State, mustReadTestFile(t, journalPath)); err != nil {
		t.Fatalf("partial P0 broke durable checkpoint: %v", err)
	}
	beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
		t.Fatalf("partial P0 replay: %v", err)
	}
	if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(authored, mustReadTestFile(t, filepath.Join(root, packet.AnswerPath))) {
		t.Fatal("replay changed authority or authored bytes")
	}
}

func TestIssuedBlockedClaimConsumesTemplateEvidence(t *testing.T) {
	for _, form := range []string{"path", "same_finding", "runtime"} {
		t.Run(form, func(t *testing.T) {
			root, statePath, journalPath, packet, request := issuedClaimStopFixture(t)
			answer := claimStopAnswer(t, "P1")
			finding := answer["findings"].([]any)[0].(map[string]any)
			finding["finding_id"] = "finding-product-blocker"
			ref := finding["evidence_refs"].([]any)[0].(string)
			if form == "same_finding" {
				ref = "finding-product-blocker"
			}
			if form == "runtime" {
				var state map[string]any
				_ = json.Unmarshal(mustReadTestFile(t, statePath), &state)
				rel := strings.TrimPrefix(ref, "path:")
				row := revisionSourceState()["evidence"].([]any)[0].(map[string]any)
				row["id"], row["path"], row["sha256"] = "evidence-blocked-start", rel, sha256Of(mustReadTestFile(t, filepath.Join(root, rel)))
				state["evidence"] = append(evidenceEntries(state), row)
				data, _ := json.Marshal(state)
				if err := os.WriteFile(statePath, data, 0600); err != nil {
					t.Fatal(err)
				}
				ref = "runtime:evidence-blocked-start"
			}
			answer["blocked_claims"] = []any{map[string]any{
				"claim_id": "claim-qa-2", "blocking_finding_ids": []any{"finding-product-blocker"},
				"failed_precondition": map[string]any{"kind": "start", "detail": "the confirmed product defect prevents the required startup"},
				"evidence_refs":       []any{ref}, "after_repair_required": true,
			}}
			authored := saveClaimStopAnswer(t, root, packet, answer)
			accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
			if err != nil {
				t.Fatalf("normal blocked %s evidence: %v", form, err)
			}
			if d := Dispositions(accepted.State)["claim-qa-2"]; d.Disposition != "blocked" || !slices.Equal(d.FindingIDs, []string{"finding-product-blocker"}) {
				t.Fatalf("blocked projection lost its Finding: %+v", d)
			}
			beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
			if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
				t.Fatalf("blocked replay: %v", err)
			}
			if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(authored, mustReadTestFile(t, filepath.Join(root, packet.AnswerPath))) {
				t.Fatal("blocked replay changed authority or author bytes")
			}
		})
	}
}
