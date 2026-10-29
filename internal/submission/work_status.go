package submission

import (
	"encoding/json"

	"github.com/entroforge/go-system-builder/internal/workstate"
)

// Status is the responsibility's current progress, reconstructed from the
// original domain receipt. Reception subtypes remain transport compatibility
// facts and never become additional work states.
func (r Receipt) Status() workstate.Status {
	return workstate.Project(workstate.Facts{
		Started:                            r.PacketID != "",
		OriginalConsumerAcceptedCompletion: r.Accepted && r.WorkComplete,
		OriginalCancellationOrReplacement:  r.ResponsibilityStatus == "replaced",
		ExplicitBlocker: r.Accepted && r.ResponsibilityStatus == "blocker_recorded" && r.currentRepairResultBlocker ||
			r.ResponsibilityStatus == "main_result_accepted_owner_status_unavailable" ||
			r.ResponsibilityStatus == "main_result_accepted_rework_required",
	})
}

// MarshalJSON always derives status from domain facts, including error and
// replay paths. No call site can forget to refresh a separately stored status.
func (r Receipt) MarshalJSON() ([]byte, error) {
	type fields Receipt
	return json.Marshal(struct {
		fields
		Status workstate.Status `json:"status"`
	}{fields: fields(r), Status: r.Status()})
}
