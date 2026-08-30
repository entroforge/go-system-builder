package metrics

import (
	"strings"
	"testing"
)

func TestS7RecordersRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := RecordS7RoundShape(root, 1, 3, 7, 2); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7ResultSubmit(root, "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7ResultSubmit(root, "rejected"); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7ResultSubmit(root, "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7ClaimLeadTime(root, 1, "claim-qa-1", 1500); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7ClaimLeadTime(root, 1, "claim-qa-1", 500); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7Findings(root, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7FirstFindingToSeal(root, 1, 60000); err != nil {
		t.Fatal(err)
	}
	// A second seal sample in the same round must not overwrite the first.
	if err := RecordS7FirstFindingToSeal(root, 1, 999999); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7CleanRound(root, 2); err != nil {
		t.Fatal(err)
	}

	snap, err := NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.S7Assignments["1"] != 3 || snap.S7Claims["1"] != 7 || snap.S7PlanRevision["1"] != 2 {
		t.Fatalf("round shape wrong: %+v %+v %+v", snap.S7Assignments, snap.S7Claims, snap.S7PlanRevision)
	}
	if snap.S7ResultSubmits["accepted"] != 2 || snap.S7ResultSubmits["rejected"] != 1 {
		t.Fatalf("submit outcomes wrong: %+v", snap.S7ResultSubmits)
	}
	lead := snap.S7ClaimLeadTime["r1:claim-qa-1"]
	if lead.Count != 2 || lead.SumMS != 2000 {
		t.Fatalf("claim lead time wrong: %+v", lead)
	}
	if snap.S7Findings["1"] != 2 {
		t.Fatalf("findings wrong: %+v", snap.S7Findings)
	}
	seal := snap.S7FirstFindingToSeal["1"]
	if seal.Count != 1 || seal.SumMS != 60000 {
		t.Fatalf("first-finding -> seal must keep the first sample: %+v", seal)
	}
	if snap.S7CleanRounds["2"] != 1 {
		t.Fatalf("clean rounds wrong: %+v", snap.S7CleanRounds)
	}

	// Backward compatibility: a pre-S7 metrics file without the new series
	// still reads with empty maps.
	if snap.GateEvaluations == nil || snap.IntegrationDuration == nil {
		t.Fatal("legacy series must stay initialized")
	}
}

func TestS7RoundShapeGaugeKeepsHighestRevision(t *testing.T) {
	root := t.TempDir()
	if err := RecordS7RoundShape(root, 1, 2, 4, 2); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7RoundShape(root, 1, 2, 4, 1); err != nil {
		t.Fatal(err)
	}
	snap, err := NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.S7PlanRevision["1"] != 2 {
		t.Fatalf("plan revision gauge moved backwards: %+v", snap.S7PlanRevision)
	}
}

func TestFormatS7FiltersByRound(t *testing.T) {
	root := t.TempDir()
	if err := RecordS7RoundShape(root, 1, 2, 4, 1); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7RoundShape(root, 2, 1, 3, 1); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7ClaimLeadTime(root, 1, "claim-qa-1", 100); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7ClaimLeadTime(root, 2, "claim-dv-9", 200); err != nil {
		t.Fatal(err)
	}
	if err := RecordS7ResultSubmit(root, "accepted"); err != nil {
		t.Fatal(err)
	}

	out, err := FormatS7(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"metrics (S7 §14.2 machine-collectible):",
		`loop_s7_assignments{round="1"} 2`,
		`loop_s7_claims{round="1"} 4`,
		`loop_s7_result_submits_total{outcome="accepted"} 1`,
		`loop_s7_claim_lead_time_ms{claim="r1:claim-qa-1"} count=1 sum_ms=100`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatS7(1) lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `round="2"`) || strings.Contains(out, "r2:claim-dv-9") {
		t.Fatalf("FormatS7(1) leaked round 2 series:\n%s", out)
	}

	all, err := FormatS7(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all, `round="2"`) || !strings.Contains(all, "r2:claim-dv-9") {
		t.Fatalf("FormatS7(0) must show every round:\n%s", all)
	}

	empty, err := FormatS7(t.TempDir(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, "(no samples)") {
		t.Fatalf("empty store must render no-samples lines:\n%s", empty)
	}
}
