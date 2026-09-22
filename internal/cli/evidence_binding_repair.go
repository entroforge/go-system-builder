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
	Runtime    string            `json:"runtime_id"`
	Revision   int               `json:"revision"`
	StateHash  string            `json:"state_sha256"`
	ID         string            `json:"evidence_id"`
	EnvelopeID string            `json:"envelope_id"`
	Files      map[string]string `json:"file_hashes"`
	Reason     string            `json:"reason"`
}

// This is deliberately not a general evidence revocation escape hatch.
// Only an objectively misbound, fingerprint-intact S5 registration qualifies.
func inspectEvidenceBinding(root string, state map[string]any, id string) (evidenceBindingPlan, error) {
	p := evidenceBindingPlan{Runtime: stringValue(state["runtime_id"]), Revision: batchInt(state["revision"]), ID: id, Files: map[string]string{}, Reason: "registration ID differs from fingerprinted envelope evidence_id"}
	b, _ := json.Marshal(state)
	p.StateHash = scopeHash(b)
	life, _ := state["lifecycle"].(map[string]any)
	if id == "" || life["state"] != "document_verification" || state["pause"] != nil || batchNestedInt(state, "review", "round") != 0 {
		return p, fmt.Errorf("requires unpaused S5 before S7 and an explicit evidence ID")
	}
	read := func(path string, hash any) ([]byte, error) {
		b, err := scopeFile(root, path)
		if err != nil {
			return nil, err
		}
		h := scopeHash(b)
		if h != hash {
			return nil, fmt.Errorf("fingerprint drift: %s", path)
		}
		p.Files[path] = h
		return b, nil
	}
	req, _ := state["bound_req"].(map[string]any)
	if req["status"] != "locked" {
		return p, fmt.Errorf("locked REQ required")
	}
	if _, err := read(stringValue(req["path"]), req["sha256"]); err != nil {
		return p, err
	}
	docs, _ := state["documents"].([]any)
	for _, raw := range docs {
		d, _ := raw.(map[string]any)
		if _, err := read(stringValue(d["path"]), d["sha256"]); err != nil {
			return p, err
		}
	}
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
		b, err := read(stringValue(e["path"]), e["sha256"])
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
	if json.Unmarshal(b, &approved) != nil || !reflect.DeepEqual(plan, approved) {
		fmt.Fprintln(stderr, "stale or changed repair plan; inspect again")
		return 1
	}
	event := fmt.Sprintf("evt-evidence-binding-repair-r%d", plan.Revision+1)
	next, err := store.Update(plan.Revision, runtime.Mutation{EventID: event, TransitionID: "EVIDENCE-BINDING-REPAIR", Actor: "framework-maintainer", Event: "evidence_binding_repaired", IdempotencyKey: event, RuntimeID: plan.Runtime, From: map[string]any{"state": "document_verification", "phase": nil}, To: map[string]any{"state": "document_verification", "phase": nil}, BaselineGeneration: batchNestedInt(snapshot.State, "baseline", "generation"), RequestID: "repair-evidence-binding", EvidenceIDs: []string{plan.ID}, OccurredAt: time.Now().UTC(), Message: fmt.Sprintf("Invalidated misbound registration %s (envelope %s); preserved files and all other evidence; %s", plan.ID, plan.EnvelopeID, plan.Reason), Apply: func(state map[string]any) error {
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
