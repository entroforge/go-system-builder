package qualitygate

import (
	"strings"
	"testing"
)

func legacyAliasFixture(t *testing.T) (Input, []documentFact, map[string]any, map[string]any, EvidenceRequirement) {
	t.Helper()
	in, docs := priorEntryS5EvidenceInput(t, "DV-SPEC-CONSISTENCY", "pass", "")
	setS5PacketEnabled(in.Snapshot.State, false)
	real := in.Snapshot.State["evidence"].([]any)[0].(map[string]any)
	alias := map[string]any{}
	for k, v := range real {
		alias[k] = v
	}
	alias["id"] = "ev-misregistered"
	in.Snapshot.State["evidence"] = []any{alias, real}
	return in, docs, alias, real, EvidenceRequirement{Kind: "document_review_record", Responsibilities: []string{"DV-SPEC-CONSISTENCY"}, Conclusions: []string{"pass"}, CurrentReviewRound: true, MinCount: 1}
}

func TestLegacyEvidenceAliasRequiresExactQualifiedReplacement(t *testing.T) {
	for _, order := range []bool{false, true} {
		t.Run(map[bool]string{false: "alias-first", true: "real-first"}[order], func(t *testing.T) {
			in, docs, alias, real, req := legacyAliasFixture(t)
			if order {
				in.Snapshot.State["evidence"] = []any{real, alias}
			}
			ids, conflicts := qualifiedEvidence(in.Snapshot.State, in.Files, "loop-test", 1, 2, req, docs)
			if len(ids) != 1 || ids[0] != "ev-old" || len(conflicts) != 0 {
				t.Fatalf("exact alias still blocked recovery: %v %v", ids, conflicts)
			}
		})
	}
	for _, test := range []struct {
		name   string
		mutate func(Input, map[string]any, map[string]any)
	}{
		{"no-replacement", func(in Input, a, r map[string]any) { in.Snapshot.State["evidence"] = []any{a} }},
		{"other-path", func(in Input, a, r map[string]any) {
			p := ".claude/evidence/copy.json"
			in.Files.(s5EvidenceFiles)[p] = append([]byte(nil), in.Files.(s5EvidenceFiles)[r["path"].(string)]...)
			a["path"] = p
		}},
		{"wrong-producer", func(_ Input, a, r map[string]any) { a["produced_by"] = []any{"someone-else"} }},
		{"wrong-responsibility", func(_ Input, a, r map[string]any) { a["responsibility_id"] = "FOREIGN" }},
		{"unreadable", func(_ Input, a, r map[string]any) { a["path"] = "missing.json" }},
		{"unqualified-replacement", func(_ Input, a, r map[string]any) { r["status"] = "invalid" }},
		{"malformed-json", func(in Input, a, r map[string]any) {
			p := ".claude/evidence/broken.json"
			data := []byte("{broken")
			in.Files.(s5EvidenceFiles)[p] = data
			a["path"] = p
			a["sha256"] = sha256Hex(data)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, docs, a, r, req := legacyAliasFixture(t)
			test.mutate(in, a, r)
			_, conflicts := qualifiedEvidence(in.Snapshot.State, in.Files, "loop-test", 1, 2, req, docs)
			if len(conflicts) == 0 {
				t.Fatal("an unrelated PASS hid the broken current record")
			}
		})
	}
}

func TestLegacyEvidenceAliasNeverCountsTowardMinimum(t *testing.T) {
	in, docs, _, real, req := legacyAliasFixture(t)
	in.Snapshot.State["evidence"] = append(in.Snapshot.State["evidence"].([]any), real)
	req.MinCount = 2
	got := evaluateRegisteredGate(in, Evaluation{GateID: "alias-test", Status: StatusSatisfied}, GateSpec{EvidenceRequirements: []EvidenceRequirement{req}}, docs)
	if got.Status != StatusNotReady || len(got.Conflicts) != 0 || len(got.Missing) == 0 {
		t.Fatalf("alias or duplicate ID supplied independent evidence: %+v", got)
	}
}

func TestLegacyEvidenceAliasCannotEraseAnotherConflictWithSameWrongID(t *testing.T) {
	in, docs, a, _, req := legacyAliasFixture(t)
	bad := map[string]any{}
	for k, v := range a {
		bad[k] = v
	}
	bad["responsibility_id"] = "FOREIGN"
	in.Snapshot.State["evidence"] = append(in.Snapshot.State["evidence"].([]any), bad)
	_, conflicts := qualifiedEvidence(in.Snapshot.State, in.Files, "loop-test", 1, 2, req, docs)
	if len(conflicts) != 1 || !strings.Contains(conflicts[0], "ev-misregistered:schema") {
		t.Fatalf("per-ID suppression swallowed another failure: %v", conflicts)
	}
}
