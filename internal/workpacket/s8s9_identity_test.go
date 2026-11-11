package workpacket

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// New responsibilities use their existing Case/Session; old numbered packet
// bytes and paths must remain unchanged for canonical receipt replay.
func TestS8S9PacketsUseOriginalIdentityWithoutRound(t *testing.T) {
	h := hypothesisResultTestBinding(t)
	hp, err := BuildHypothesisResult(h, hypothesisResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	p, pi := repairPlanReportTestFixture(t)
	pp, err := BuildRepairPlanReport(p, pi)
	if err != nil {
		t.Fatal(err)
	}
	r, ri := repairResultTestFixture(t)
	rp, err := BuildRepairResult(r, ri)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		schema, path, identity string
		packet                 any
	}{
		{"hypothesis-result-work-packet.schema.json", hp.ViewPath, h.CaseID, hp},
		{"repair-plan-report-work-packet.schema.json", pp.ViewPath, p.SessionID, pp},
		{"repair-result-work-packet.schema.json", rp.ViewPath, r.Sources.SessionID, rp},
	} {
		if strings.Contains(tc.path, "/r0/") || tc.path == "" {
			t.Fatalf("original identity absent from path %s", tc.path)
		}
		data, err := json.Marshal(tc.packet)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes(tc.schema, data); err != nil {
			t.Fatalf("%s: %v", tc.schema, err)
		}
		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		binding := wire["binding"].(map[string]any)
		if sources, ok := binding["sources"].(map[string]any); ok {
			binding = sources
		}
		if binding["case_id"] != tc.identity && binding["session_id"] != tc.identity {
			t.Fatalf("%s lost original responsibility identity %s", tc.schema, tc.identity)
		}
		if _, present := binding["review_round"]; present {
			t.Fatalf("%s emitted a synthetic legacy number", tc.schema)
		}
	}
	// An independent Case at the same generation must never reuse an issuance.
	h.CaseID += "-other"
	other, err := BuildHypothesisResult(h, hypothesisResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == hp.ID || other.ViewPath == hp.ViewPath {
		t.Fatal("different original Cases share an issuance")
	}
}
