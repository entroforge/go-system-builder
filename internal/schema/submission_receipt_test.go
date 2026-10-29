package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestSubmissionReceiptSchemaAcceptsOriginalDomainWires(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cases := []struct {
		name    string
		receipt submission.Receipt
	}{
		{
			name:    "generic accepted sha key",
			receipt: receiptWire(sha, true, "accepted", true, "S5"),
		},
		{
			name:    "generic replaced historical receipt",
			receipt: receiptWire(sha, true, "replaced", false, "S9"),
		},
		{
			name: "not accepted has no accepted key",
			receipt: submission.Receipt{
				PacketID: "packet-not-yet-accepted", Accepted: false,
				ResponsibilityStatus: "not_accepted", WorkComplete: false,
				CurrentStage: "S9", DomainRefs: []string{}, WaitingOn: []submission.Waiting{},
				NextAction: "preserve the original answer", FieldErrors: []workpacket.FieldError{},
			},
		},
		{
			name: "not accepted may precede packet selection",
			receipt: submission.Receipt{
				PacketID: "", Accepted: false, ResponsibilityStatus: "not_accepted", WorkComplete: false,
				CurrentStage: "S9", DomainRefs: []string{}, WaitingOn: []submission.Waiting{},
				NextAction: "issue the original packet", FieldErrors: []workpacket.FieldError{},
			},
		},
		{
			name:    "S7 ResultID key in a later current stage",
			receipt: receiptWire("review-result-"+sha, true, "accepted", true, "S9"),
		},
		{
			name:    "S8 accepted idempotency key",
			receipt: receiptWire("runtime:investigation-case:investigation-case-001:17", true, "accepted", true, "S9"),
		},
		{
			name:    "S9 pre-execution PlanReport",
			receipt: receiptWire("runtime:s9:issued-plan-report:packet-"+sha, true, "accepted", false, "S7"),
		},
		{
			name:    "S9 candidate accepted but owner proof pending",
			receipt: receiptWire("runtime:s9:issued-repair-candidate:packet-"+sha, true, "candidate_accepted", false, "S7"),
		},
		{
			name:    "S9 completed candidate with Main pending",
			receipt: withConsumer(receiptWire("runtime:s9:issued-repair-candidate:packet-"+sha, true, "candidate_accepted", true, "S7")),
		},
		{
			name:    "S9 final Result current completion",
			receipt: withConsumer(receiptWire("runtime:s9:issued-repair-result:packet-"+sha, true, "main_result_accepted", true, "S7")),
		},
		{
			name:    "S9 final Result needs rework",
			receipt: withConsumer(receiptWire("runtime:s9:issued-repair-result:packet-"+sha, true, "main_result_accepted_rework_required", false, "S7")),
		},
		{
			name:    "S9 final Result owner status unavailable",
			receipt: withConsumer(receiptWire("runtime:s9:issued-repair-result:packet-"+sha, true, "main_result_accepted_owner_status_unavailable", false, "S7")),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.receipt)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", data); err != nil {
				t.Fatalf("schema rejected original domain wire %s: %v\n%s", tc.name, err, data)
			}
		})
	}
}

func TestSubmissionReceiptSchemaRejectsInvalidStatusKeyAndConsumerCombinations(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	base := receiptWire("runtime:s9:issued-repair-candidate:packet-"+sha, true, "candidate_accepted", true, "S7")
	base = withConsumer(base)
	baseJSON, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var valid map[string]any
	if err := json.Unmarshal(baseJSON, &valid); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unknown status", func(v map[string]any) { v["responsibility_status"] = "accepted_but_not_really" }},
		{"accepted false with accepted status", func(v map[string]any) { v["accepted"] = false }},
		{"accepted empty packet id", func(v map[string]any) { v["packet_id"] = "" }},
		{"not accepted with completed work", func(v map[string]any) {
			v["accepted"] = false
			v["work_complete"] = true
			v["responsibility_status"] = "not_accepted"
		}},
		{"accepted missing key", func(v map[string]any) { delete(v, "submission_key") }},
		{"accepted empty domain refs", func(v map[string]any) { v["domain_refs"] = []string{} }},
		{"key outside closed union", func(v map[string]any) { v["submission_key"] = "runtime:unregistered:anything" }},
		{"S7 result key without exact digest", func(v map[string]any) { v["submission_key"] = "review-result-qa-1" }},
		{"S8 zero revision", func(v map[string]any) { v["submission_key"] = "runtime:investigation-case:investigation-case-001:0" }},
		{"S9 packet ID without digest", func(v map[string]any) { v["submission_key"] = "runtime:s9:issued-plan-report:packet-not-a-digest" }},
		{"candidate falsely keyed by generic digest", func(v map[string]any) { v["submission_key"] = sha }},
		{"candidate falsely keyed as PlanReport", func(v map[string]any) { v["submission_key"] = "runtime:s9:issued-plan-report:packet-" + sha }},
		{"candidate falsely keyed as final Result", func(v map[string]any) { v["submission_key"] = "runtime:s9:issued-repair-result:packet-" + sha }},
		{"candidate namespace cannot claim generic accepted status", func(v map[string]any) { v["responsibility_status"] = "accepted" }},
		{"Main Result falsely keyed as candidate", func(v map[string]any) {
			v["responsibility_status"] = "main_result_accepted"
			v["submission_key"] = "runtime:s9:issued-repair-candidate:packet-" + sha
		}},
		{"final Result namespace cannot claim generic accepted status", func(v map[string]any) {
			v["responsibility_status"] = "accepted"
			v["submission_key"] = "runtime:s9:issued-repair-result:packet-" + sha
		}},
		{"replaced still complete", func(v map[string]any) { v["responsibility_status"] = "replaced"; v["work_complete"] = true }},
		{"candidate complete missing consumer pair", func(v map[string]any) { delete(v, "consumer_owner"); delete(v, "consumer_pending") }},
		{"candidate complete pending without owner", func(v map[string]any) { delete(v, "consumer_owner") }},
		{"candidate complete consumer not pending", func(v map[string]any) { v["consumer_pending"] = false }},
		{"main result accepted but incomplete", func(v map[string]any) {
			v["responsibility_status"] = "main_result_accepted"
			v["submission_key"] = "runtime:s9:issued-repair-result:packet-" + sha
			v["work_complete"] = false
		}},
		{"main result rework marked complete", func(v map[string]any) {
			v["responsibility_status"] = "main_result_accepted_rework_required"
			v["submission_key"] = "runtime:s9:issued-repair-result:packet-" + sha
			v["work_complete"] = true
		}},
		{"main result consumer changed", func(v map[string]any) {
			v["responsibility_status"] = "main_result_accepted"
			v["submission_key"] = "runtime:s9:issued-repair-result:packet-" + sha
			v["consumer_owner"] = "worker"
		}},
		{"consumer owner without pending field", func(v map[string]any) {
			v["responsibility_status"] = "accepted"
			v["submission_key"] = sha
			v["work_complete"] = true
			delete(v, "consumer_pending")
		}},
		{"pending without owner", func(v map[string]any) {
			v["responsibility_status"] = "accepted"
			v["submission_key"] = sha
			v["work_complete"] = true
			delete(v, "consumer_owner")
		}},
		{"unknown field", func(v map[string]any) { v["approval_granted"] = true }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var value map[string]any
			encoded, _ := json.Marshal(valid)
			if err := json.Unmarshal(encoded, &value); err != nil {
				t.Fatal(err)
			}
			tc.mutate(value)
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", data); err == nil {
				t.Fatalf("schema accepted invalid wire: %s", data)
			}
		})
	}
}

func receiptWire(key string, accepted bool, status string, complete bool, currentStage string) submission.Receipt {
	receipt := submission.Receipt{
		PacketID: "packet-original", Accepted: accepted, ResponsibilityStatus: status,
		WorkComplete: complete, CurrentStage: currentStage,
		DomainRefs: []string{".claude/evidence/original.json"}, WaitingOn: []submission.Waiting{},
		NextAction: "continue through the named original route", FieldErrors: []workpacket.FieldError{},
	}
	if accepted {
		receipt.SubmissionKey = key
	} else {
		receipt.DomainRefs = []string{}
		receipt.Error = "answer has not been accepted"
	}
	return receipt
}

func withConsumer(receipt submission.Receipt) submission.Receipt {
	receipt.ConsumerOwner = "main"
	receipt.ConsumerPending = true
	return receipt
}

func TestSubmissionReceiptHistoricalStageDoesNotDiscriminateDomain(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, receipt := range []submission.Receipt{
		receiptWire("review-result-"+sha, true, "accepted", true, "S9"),
		receiptWire("runtime:s9:issued-plan-report:packet-"+sha, true, "accepted", false, "S7"),
	} {
		data, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", data); err != nil {
			t.Fatalf("current_stage was incorrectly treated as a domain discriminator: %v", err)
		}
	}
}

func TestSubmissionReceiptKeyPatternsAreAnchored(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	valid := []struct {
		key, status string
		complete    bool
		consumer    bool
	}{
		{sha, "accepted", false, false},
		{"review-result-" + sha, "accepted", true, false},
		{"runtime:investigation-case:investigation-case-alpha.v2:1", "accepted", true, false},
		{"runtime:s9:issued-plan-report:packet-" + sha, "accepted", false, false},
		{"runtime:s9:issued-repair-candidate:packet-" + sha, "candidate_accepted", false, false},
		{"runtime:s9:issued-repair-result:packet-" + sha, "main_result_accepted", true, true},
	}
	for _, tc := range valid {
		receipt := receiptWire(tc.key, true, tc.status, tc.complete, "S5")
		if tc.consumer {
			receipt = withConsumer(receipt)
		}
		data, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", data); err != nil {
			t.Errorf("valid original key/status %q/%q rejected: %v", tc.key, tc.status, err)
		}
	}
	invalid := []string{
		strings.ToUpper(sha),
		"x" + sha,
		"review-result-" + sha + "-suffix",
		"runtime:investigation-case:case-id:1",
		"runtime:investigation-case:investigation-case-alpha:00",
		"runtime:s9:issued-repair-candidate:packet-" + strings.ToUpper(sha),
	}
	for _, key := range invalid {
		data, err := json.Marshal(receiptWire(key, true, "accepted", false, "S5"))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", data); err == nil {
			t.Errorf("invalid original key %q accepted", key)
		}
	}
}

func TestSubmissionReceiptRejectedAttemptPreservesAcceptedHistory(t *testing.T) {
	receipt := receiptWire(strings.Repeat("a", 64), true, "accepted", true, "S4")
	receipt.AttemptStatus = "rejected"
	body, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", body); err != nil {
		t.Fatalf("rejected attempt erased accepted history in schema: %v", err)
	}
	receipt.AttemptStatus = "invented"
	body, _ = json.Marshal(receipt)
	if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", body); err == nil {
		t.Fatal("unknown attempt status accepted")
	}
}
