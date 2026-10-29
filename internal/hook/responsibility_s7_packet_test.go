package hook_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestPacketReviewResponsibilityReadsCommittedDelivery(t *testing.T) {
	for _, condition := range []string{"accepted", "artifact missing", "unconsumed", "invalidated"} {
		t.Run(condition, func(t *testing.T) {
			root, state, agent := canonicalReviewerReceiptFixture(t)
			state["schema_version"] = "1.2.0"
			state["configuration"] = map[string]any{"optimization": map[string]any{"packet": true}}
			reviewState := state["review"].(map[string]any)
			plan := reviewState["plan"].(map[string]any)
			row := reviewState["assignments"].(map[string]any)[agent.AssignmentID].(map[string]any)
			evidence := state["evidence"].([]any)[0].(map[string]any)
			packet, err := workpacket.BuildReviewResult(workpacket.ReviewResultBinding{
				RuntimeID: "loop-s7-test", BaselineGeneration: 7, ReviewRound: 3,
				ReviewPlanID: plan["plan_id"].(string), ReviewPlanRef: plan["path"].(string),
				ReviewPlanSHA256: plan["sha256"].(string), ReviewPlanRevision: 2,
				AssignmentID: agent.AssignmentID, AssignmentSHA256: strings.Repeat("b", 64),
				OwnerAgentID: agent.ID, ClaimIDs: []string{"claim-qa-1"}, SubjectDigest: strings.Repeat("c", 64),
			}, workpacket.ReviewResultInputs{
				Claims:         json.RawMessage(`[{"claim_id":"claim-qa-1","lens":"qa","target":"internal/example.go","assertion":"errors propagate","oracle":"no dropped errors","method":"code review","applicability":"required","source_refs":["REQ-007"]}]`),
				FrozenSubjects: json.RawMessage(`[{"path":"internal/example.go","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`),
			})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(packet)
			if err != nil {
				t.Fatal(err)
			}
			var packetRow map[string]any
			if err := json.Unmarshal(encoded, &packetRow); err != nil {
				t.Fatal(err)
			}
			state["entities"].(map[string]any)["work_packets"] = []any{packetRow}
			row["result_receipt"] = map[string]any{
				"packet_id": packet.ID, "assignment_id": agent.AssignmentID, "producer_agent_id": agent.ID,
				"result_ref": row["result_ref"], "result_sha256": evidence["sha256"],
			}
			switch condition {
			case "artifact missing":
				for _, ref := range []string{row["result_ref"].(string), plan["path"].(string)} {
					if err := os.Remove(filepath.Join(root, filepath.FromSlash(ref))); err != nil {
						t.Fatal(err)
					}
				}
			case "unconsumed":
				row["status"], row["result_ref"], row["result_receipt"] = "dispatched", nil, nil
			case "invalidated":
				evidence["invalidated_by"] = "later-domain-change"
			}
			assignment := &hookctx.AssignmentContext{AssignmentID: agent.AssignmentID, OwnerAgentID: agent.ID, TaskID: "TASK-007"}
			facts := hook.CurrentResponsibility(root, loopruntime.Snapshot{State: state}, agent, assignment, false)
			wantDone := condition == "accepted" || condition == "artifact missing"
			if facts.DeliveryRecorded != wantDone || facts.ExecutionMayEnd != wantDone || (facts.Status == "done") != wantDone {
				t.Fatalf("%s: delivery projection=%+v", condition, facts)
			}
		})
	}
}
