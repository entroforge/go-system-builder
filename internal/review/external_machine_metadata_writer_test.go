package review

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The S8 work is synthetic retained history. Its complete packet comes from
// the original producer; S7 must preserve it while receiving its own Result.
func TestS7OriginalWriterPreservesExternalPacketMachineMetadata(t *testing.T) {
	for _, representation := range []string{"nonstandard", "empty", "omitted"} {
		t.Run(representation, func(t *testing.T) {
			root, statePath, journalPath, snapshot, _, requests := issuedConcurrentResultsFixture(t)
			binding := workpacket.HypothesisResultBinding{
				RuntimeID: "loop-REQ-TEST", BaselineGeneration: 1,
				CaseID: "investigation-case-retained", CaseRef: ".claude/review/investigation/cases/retained.json", CaseRevision: 1,
				SourceFindingIDs: []string{}, HypothesisID: "hypothesis-retained", HypothesisSourceFindingIDs: []string{},
				AssignmentID: "assignment-retained", TeamID: "workgroup-retained", TaskID: "TASK-RETAINED",
				TaskRef: "docs/tasks/TASK-RETAINED.md", ManifestRef: ".claude/workgroups/retained.json", OwnerAgentID: "investigator-retained",
			}
			packet, err := workpacket.BuildHypothesisResult(binding, workpacket.HypothesisResultInputs{
				Hypothesis: workpacket.HypothesisResultQuestion{
					HypothesisID: binding.HypothesisID, AssignmentID: binding.AssignmentID,
					Statement: "A retained investigation concerns a stale response.", Invariant: "Fresh reads reflect the saved value.",
					Discriminator:    "Compare the same read with the cache bypassed.",
					ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{Support: "Bypassing restores the saved value.", Refute: "Bypassing retains the stale response."},
					SourceFindingIDs: []string{}, EvidenceRefs: []string{"test://retained-probe"}, Status: "open",
				},
				CaseRoute: workpacket.HypothesisResultCaseRoute{Status: "investigating"}, Findings: []workpacket.HypothesisResultFinding{},
				EvidenceRefs: []workpacket.HypothesisEvidenceRef{{Ref: "test://retained-probe", Origin: "hypothesis", Attestation: "execution_anchor"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(packet)
			var historical map[string]any
			if err := json.Unmarshal(data, &historical); err != nil {
				t.Fatal(err)
			}
			set := func(row map[string]any, key string) {
				switch representation {
				case "nonstandard":
					row[key] = "historical original value: " + key
				case "empty":
					row[key] = ""
				case "omitted":
					delete(row, key)
				}
			}
			set(historical, "template_sha256")
			originalBinding := historical["binding"].(map[string]any)
			for _, key := range []string{"case_sha256", "case_route_sha256", "observation_batch_sha256", "baseline_digest", "hypothesis_question_sha256", "assignment_sha256", "task_sha256", "manifest_sha256"} {
				set(originalBinding, key)
			}
			inputs := historical["inputs"].(map[string]any)
			set(inputs["evidence_refs"].([]any)[0].(map[string]any), "sha256")
			entities := snapshot.State["entities"].(map[string]any)
			entities["work_packets"] = append(entities["work_packets"].([]any), historical)
			stateBytes, _ := json.Marshal(snapshot.State)
			if err := os.WriteFile(statePath, stateBytes, 0600); err != nil {
				t.Fatal(err)
			}
			wrongOwner := requests[0]
			wrongOwner.ExpectedRevision, wrongOwner.OwnerAgentID = -1, "another-reviewer"
			if _, err := SubmitIssuedResult(root, statePath, journalPath, wrongOwner); err == nil {
				t.Fatal("historical machine compatibility relaxed the real Result owner")
			}
			var acceptedState map[string]any
			for _, request := range requests {
				request.ExpectedRevision = -1
				accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
				if err != nil {
					t.Fatalf("retained external metadata blocked the original S7 Result Writer: %v", err)
				}
				acceptedState = accepted.State
			}
			if !RequiredResponsibilitiesComplete(acceptedState) || PlanPointerFromState(acceptedState).Status != "clean" {
				t.Fatal("actual S7 results did not reach their original clean outlet")
			}
			// The typed Go decoder must also retain these exact legacy values;
			// passing the candidate schema alone is insufficient.
			var retained map[string]any
			for _, raw := range acceptedState["entities"].(map[string]any)["work_packets"].([]any) {
				row := raw.(map[string]any)
				if row["packet_id"] == packet.ID {
					retained = row
				}
			}
			if retained == nil {
				t.Fatal("S7 erased the retained original S8 packet")
			}
			original, _ := json.Marshal(historical)
			actual, _ := json.Marshal(retained)
			if !bytes.Equal(original, actual) {
				t.Fatal("S7 rewrote external historical packet metadata")
			}
			var decoded workpacket.HypothesisResultPacket
			if err := json.Unmarshal(actual, &decoded); err != nil {
				t.Fatalf("original typed packet decoder rejected retained metadata: %v", err)
			}
			beforeState, _ := os.ReadFile(statePath)
			beforeJournal, _ := os.ReadFile(journalPath)
			request := requests[1]
			request.ExpectedRevision = -1
			if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
				t.Fatalf("original Result replay: %v", err)
			}
			afterState, _ := os.ReadFile(statePath)
			afterJournal, _ := os.ReadFile(journalPath)
			if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
				t.Fatal("original Result replay rewrote Runtime or journal")
			}
		})
	}
}
