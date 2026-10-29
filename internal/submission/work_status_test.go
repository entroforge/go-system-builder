package submission

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workstate"
)

func TestReceiptJSONDerivesFiveStateFromOriginalConsumerFacts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		receipt Receipt
		want    workstate.Status
	}{
		{"unissued", Receipt{}, workstate.Pending},
		{"issued", Receipt{PacketID: "p"}, workstate.Active},
		{"reception alone", Receipt{PacketID: "p", Accepted: true}, workstate.Active},
		{"consumer complete", Receipt{PacketID: "p", Accepted: true, WorkComplete: true}, workstate.Done},
		{"completed prior stage waiting on next preparation", Receipt{PacketID: "p", Accepted: true, WorkComplete: true, WaitingOn: []Waiting{{Reason: "system_followup", Owner: "Main", Ref: "PTR-PLAN-01"}}}, workstate.Done},
		{"unaccepted completion", Receipt{PacketID: "p", WorkComplete: true}, workstate.Active},
		{"rework", Receipt{PacketID: "p", Accepted: true, ResponsibilityStatus: "main_result_accepted_rework_required"}, workstate.Blocked},
		{"owner proof unavailable", Receipt{PacketID: "p", Accepted: true, ResponsibilityStatus: "main_result_accepted_owner_status_unavailable"}, workstate.Blocked},
		{"replaced", Receipt{PacketID: "p", Accepted: true, ResponsibilityStatus: "replaced"}, workstate.Cancelled},
		{"blocker text alone", Receipt{PacketID: "p", Accepted: true, ResponsibilityStatus: "blocker_recorded"}, workstate.Active},
		{"current original blocker", Receipt{PacketID: "p", Accepted: true, ResponsibilityStatus: "blocker_recorded", currentRepairResultBlocker: true}, workstate.Blocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.receipt)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if wire["status"] != string(tc.want) || tc.receipt.Status() != tc.want {
				t.Fatalf("status=%v want=%s JSON=%s", wire["status"], tc.want, data)
			}
		})
	}
}

func TestDecodedBlockerReceiptCannotManufactureCurrentProof(t *testing.T) {
	var r Receipt
	if err := json.Unmarshal([]byte(`{"packet_id":"p","accepted":true,"responsibility_status":"blocker_recorded","work_complete":false,"status":"blocked","currentRepairResultBlocker":true}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Status() != workstate.Active || r.currentRepairResultBlocker {
		t.Fatal("untrusted receipt text manufactured a current original blocker proof")
	}
}
