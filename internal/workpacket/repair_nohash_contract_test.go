package workpacket

import (
	"encoding/json"
	"strings"
	"testing"
)

// Existing identities remain stable when only legacy hash metadata differs.
func TestS9PlanPacketIdentityUsesOriginalResponsibility(t *testing.T) {
	binding, inputs := repairPlanReportTestFixture(t)
	a, err := BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}

	b, err := BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatal("legacy digest changed original responsibility identity")
	}
	packetJSON, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(packetJSON), "_sha256\"") || strings.Contains(string(packetJSON), "_digest\"") {
		t.Fatalf("new packet copies framework digest metadata: %s", packetJSON)
	}
	data, err := json.Marshal(a.Binding)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sha256") {
		t.Fatalf("new binding wrote digest: %s", data)
	}
	old := a
	old.ID = "packet-historical"

	old.ViewPath = "old/view.json"
	data, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var retained RepairPlanReportPacket
	if err := json.Unmarshal(data, &retained); err != nil || retained.ID != old.ID || retained.ViewPath != old.ViewPath {
		t.Fatal("original issued packet changed", err)
	}
}

func TestS9ResultPacketIdentitySeparatesActualResume(t *testing.T) {
	binding, inputs := repairResultTestFixture(t)
	p, err := BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	copy := binding
	copy.Resume = &RepairResultResume{PredecessorPacketID: p.ID, BlockerRef: ".claude/blocker.json", ResolutionMessageRef: ".claude/resolution.json", ResolutionEventID: "evt-real-resolution"}
	next, err := BuildRepairResult(copy, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == next.ID {
		t.Fatal("distinct accepted resolution produced same responsibility")
	}
}

func TestS9SourcePreservesBusinessSHAFields(t *testing.T) {
	original := json.RawMessage(`{"business":{"sha256":"business-value","nested":{"feature_digest":"business-digest"}},"description":"The business API accepts sha256 as a field"}`)
	got, err := canonicalRepairPlanReportSource(original)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	business := parsed["business"].(map[string]any)
	if business["sha256"] != "business-value" || business["nested"].(map[string]any)["feature_digest"] != "business-digest" {
		t.Fatal("business fields were altered")
	}
	if parsed["description"] != "The business API accepts sha256 as a field" {
		t.Fatal("business text was altered")
	}
}
