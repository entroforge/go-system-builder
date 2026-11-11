package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

type evidenceBindingPlan struct {
	Runtime    string `json:"runtime_id"`
	Revision   int    `json:"revision"`
	ID         string `json:"evidence_id"`
	EnvelopeID string `json:"envelope_id"`
	Reason     string `json:"reason"`
}

// This is deliberately not a general evidence revocation escape hatch.
// It identifies an actual S5 registration whose original envelope has a
// different evidence ID; historical fingerprints are not used as proof.
func inspectEvidenceBinding(root string, state map[string]any, id string) (evidenceBindingPlan, error) {
	p := evidenceBindingPlan{Runtime: stringValue(state["runtime_id"]), Revision: batchInt(state["revision"]), ID: id, Reason: "registration ID differs from original envelope evidence_id"}
	life, _ := state["lifecycle"].(map[string]any)
	if id == "" || life["state"] != "document_verification" || state["pause"] != nil {
		return p, fmt.Errorf("requires unpaused S5 and an explicit evidence ID")
	}
	req, _ := state["bound_req"].(map[string]any)
	if req["status"] != "locked" {
		return p, fmt.Errorf("locked REQ required")
	}
	// This repair decides only whether the selected original evidence row is
	// misbound. Unrelated document bodies were previously read solely for
	// fingerprint authentication and are not required to make that decision.
	evs, _ := state["evidence"].([]any)
	count := 0
	for _, raw := range evs {
		e, _ := raw.(map[string]any)
		if e["id"] != id {
			continue
		}
		count++
		if e["kind"] != "document_review" || e["status"] != "valid" || e["invalidated_by"] != nil || batchInt(e["baseline_generation"]) != batchNestedInt(state, "baseline", "generation") {
			return p, fmt.Errorf("target must be current valid document_review")
		}
		b, err := scopeFile(root, stringValue(e["path"]))
		if err != nil {
			return p, err
		}
		var envelope map[string]any
		if json.Unmarshal(b, &envelope) != nil {
			return p, fmt.Errorf("target is not a JSON envelope")
		}
		p.EnvelopeID = stringValue(envelope["evidence_id"])
		if p.EnvelopeID == "" || p.EnvelopeID == id || envelope["kind"] != "document_review" || envelope["runtime_id"] != p.Runtime || batchInt(envelope["baseline_generation"]) != batchNestedInt(state, "baseline", "generation") {
			return p, fmt.Errorf("no proven ID-only misbinding in this runtime/generation")
		}
	}
	if count != 1 {
		return p, fmt.Errorf("requires exactly one target registration")
	}
	return p, nil
}

func runEvidenceBindingRepair(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("runtime repair-evidence-binding", flag.ContinueOnError)
	f.SetOutput(stderr)
	root := f.String("root", ".", "project root")
	id := f.String("id", "", "single misbound document_review ID")
	apply := f.String("apply-plan", "", "apply exactly the read-only JSON plan")
	if err := f.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	store := runtime.NewWriter(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{})
	snapshot, err := store.Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := inspectEvidenceBinding(*root, snapshot.State, *id)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *apply == "" {
		return encodeJSON(stdout, plan)
	}
	b, err := os.ReadFile(*apply)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var approved evidenceBindingPlan
	decodeErr := json.Unmarshal(b, &approved)
	// Old preview wording described digest authentication that no longer
	// exists. Preserve the reviewed original IDs and revision; the old
	// reason text must not force a new preview by itself.
	if approved.Reason == "registration ID differs from fingerprinted envelope evidence_id" {
		approved.Reason = plan.Reason
	}
	if decodeErr != nil || !reflect.DeepEqual(plan, approved) {
		fmt.Fprintln(stderr, "stale or changed repair plan; inspect again")
		return 1
	}
	// The responsibility is the Runtime that registered the evidence; the
	// intent is the exact misbound registration and its original envelope.
	// The invalidated registration admits no second binding repair, so this
	// intent key is stable and carries no Runtime write count (G18 §1.3).
	event := fmt.Sprintf("evt-evidence-binding-repair-%s-%s", plan.ID, plan.EnvelopeID)
	next, err := store.Update(plan.Revision, runtime.Mutation{EventID: event, TransitionID: "EVIDENCE-BINDING-REPAIR", Actor: "framework-maintainer", Event: "evidence_binding_repaired", IdempotencyKey: fmt.Sprintf("runtime:evidence-binding-repair:%s:%s:%s", plan.Runtime, plan.ID, plan.EnvelopeID), RuntimeID: plan.Runtime, From: map[string]any{"state": "document_verification", "phase": nil}, To: map[string]any{"state": "document_verification", "phase": nil}, BaselineGeneration: batchNestedInt(snapshot.State, "baseline", "generation"), RequestID: "repair-evidence-binding", EvidenceIDs: []string{plan.ID}, OccurredAt: time.Now().UTC(), Message: fmt.Sprintf("Invalidated misbound registration %s (envelope %s); preserved files and all other evidence; %s", plan.ID, plan.EnvelopeID, plan.Reason), Apply: func(state map[string]any) error {
		current, err := inspectEvidenceBinding(*root, state, *id)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, plan) {
			return fmt.Errorf("repair facts changed")
		}
		evs, _ := state["evidence"].([]any)
		for _, raw := range evs {
			e, _ := raw.(map[string]any)
			if e["id"] == plan.ID {
				e["status"] = "invalid"
				e["invalidated_by"] = "EVIDENCE-BINDING-REPAIR"
				e["invalidation_rule"] = "evidence_binding_repair"
				e["invalidation_reason"] = plan.Reason
			}
		}
		delete(state, "milestone")
		return nil
	}})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return encodeJSON(stdout, map[string]any{"status": "repaired", "revision": next.Revision, "plan": plan})
}
