package qualitygate

import (
	"encoding/json"

	"github.com/entroforge/go-system-builder/internal/workstate"
)

func (r BuilderReceipt) Status() workstate.Status {
	return workstate.Project(workstate.Facts{
		Started:                            r.ownerStarted || r.DeliveryRecorded || r.ResultRef != "",
		ExplicitBlocker:                    r.explicitBlocker,
		OriginalConsumerAcceptedCompletion: r.Accepted && r.WorkComplete,
		OriginalCancellationOrReplacement:  r.originalCancellationOrReplacement,
	})
}

func (r BuilderReceipt) MarshalJSON() ([]byte, error) {
	type fields BuilderReceipt
	return json.Marshal(struct {
		fields
		Status workstate.Status `json:"status"`
	}{fields: fields(r), Status: r.Status()})
}
