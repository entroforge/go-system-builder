package submission

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestRepairResultBlockerReceiptSchemaKeepsOriginalKeyAndFiveStateBoundary(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	r := Receipt{
		PacketID: "packet-" + strings.Repeat("a", 64), SubmissionKey: "runtime:agent:builder-a:work_blocked:9",
		Accepted: true, ResponsibilityStatus: "blocker_recorded", WorkComplete: false,
		ConsumerOwner: "main", ConsumerPending: true, CurrentStage: "S9",
		DomainRefs: []string{"original-artifact.json", "original-message.json"},
		WaitingOn:  []Waiting{{Reason: "original blocker", Owner: "Main", Ref: "original-artifact.json"}},
		NextAction: "Main handles original recovery", currentRepairResultBlocker: true,
		FieldErrors: []workpacket.FieldError{},
	}
	for _, current := range []bool{true, false} {
		r.currentRepairResultBlocker = current
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := validator.ValidateBytes("submission-receipt.schema.json", data); err != nil {
			t.Fatalf("current=%t original blocker receipt failed schema: %s err=%v", current, data, err)
		}
	}
	r.currentRepairResultBlocker = true
	base, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"completion":          func(w map[string]any) { w["work_complete"] = true },
		"done":                func(w map[string]any) { w["status"] = "done" },
		"new state alias":     func(w map[string]any) { w["status"] = "blocker_recorded" },
		"wrong consumer":      func(w map[string]any) { w["consumer_owner"] = "worker" },
		"no pending consumer": func(w map[string]any) { w["consumer_pending"] = false },
		"wrong original key": func(w map[string]any) {
			w["submission_key"] = "runtime:s9:issued-repair-candidate:packet-" + strings.Repeat("a", 64)
		},
		"key without subtype": func(w map[string]any) { w["responsibility_status"] = "accepted" },
		"missing message":     func(w map[string]any) { w["domain_refs"] = []any{"original-artifact.json"} },
		"unknown field":       func(w map[string]any) { w["execution_may_end"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			var wire map[string]any
			if err := json.Unmarshal(base, &wire); err != nil {
				t.Fatal(err)
			}
			mutate(wire)
			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if validator.ValidateBytes("submission-receipt.schema.json", data) == nil {
				t.Fatalf("invalid blocker wire passed closed schema: %s", data)
			}
		})
	}
}

func TestRepairResultResolutionReceiptSchemaDoesNotCompleteRetiredWorker(t *testing.T) {
	r := Receipt{
		PacketID: "packet-" + strings.Repeat("a", 64), SubmissionKey: "runtime:agent:builder-a:blocker_resolved:10",
		Accepted: true, ResponsibilityStatus: "blocker_resolution_recorded", WorkComplete: false,
		ConsumerOwner: "main", ConsumerPending: true, CurrentStage: "S9",
		DomainRefs: []string{"original-resolution.json", "original-blocker.json"},
		WaitingOn:  []Waiting{{Reason: "Resume successor required", Owner: "Main", Ref: "original-resolution.json"}},
		NextAction: "Wait for Main's new verified Resume", FieldErrors: []workpacket.FieldError{},
	}
	base, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	validator := schema.NewEmbeddedValidator()
	if err := validator.ValidateBytes("submission-receipt.schema.json", base); err != nil {
		t.Fatalf("original resolution receipt failed schema: %v\n%s", err, base)
	}
	for name, mutate := range map[string]func(map[string]any){
		"worker complete": func(w map[string]any) { w["work_complete"] = true },
		"worker done":     func(w map[string]any) { w["status"] = "done" },
		"old blocker":     func(w map[string]any) { w["status"] = "blocked" },
		"wrong subtype":   func(w map[string]any) { w["responsibility_status"] = "accepted" },
		"wrong key":       func(w map[string]any) { w["submission_key"] = "runtime:agent:builder-a:work_blocked:10" },
		"not accepted":    func(w map[string]any) { w["accepted"] = false },
		"no pending Main": func(w map[string]any) { w["consumer_pending"] = false },
	} {
		t.Run(name, func(t *testing.T) {
			var wire map[string]any
			if err := json.Unmarshal(base, &wire); err != nil {
				t.Fatal(err)
			}
			mutate(wire)
			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if validator.ValidateBytes("submission-receipt.schema.json", data) == nil {
				t.Fatalf("resolution receipt granted completion or changed original key: %s", data)
			}
		})
	}
}
