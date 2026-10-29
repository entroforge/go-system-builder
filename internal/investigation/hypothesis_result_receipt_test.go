package investigation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS8SourceBoundaryRefsResolveOnlyFrozenSources(t *testing.T) {
	root := t.TempDir()
	batchRef := ".claude/evidence/observation-batch-s8-boundary.json"
	batch := map[string]any{
		"schema_version": "1.0.0", "observation_batch_id": "observation-batch-s8-boundary", "conclusion": "sealed",
		"evidence_id": "observation-batch-s8-boundary", "kind": "observation_batch", "runtime_id": "loop-REQ-039",
		"producer_agent_id": "round-consumer", "producer_responsibility": "Orchestrator", "review_plan_id": "review-plan-s8",
		"review_round": 1, "baseline_generation": 2, "subject_digest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"finding_ids": []string{"finding-s8-boundary"}, "drain_policy": "complete_required_claims",
		"claim_coverage_summary": map[string]any{
			"total_required": 1, "pass": 0, "finding": 0, "not_applicable": 0, "blocked": 1,
			"blocked_claims": []any{map[string]any{
				"claim_id": "claim-s8-blocked", "blocking_finding_ids": []string{"finding-s8-boundary"},
				"failed_precondition": map[string]any{"kind": "precondition", "detail": "the source condition was not satisfied"},
				"evidence_refs":       []string{"evidence://sealed-blocked-claim"}, "after_repair_required": true, "result_id": "review-result-s8",
			}}, "plan_revision": 1,
		},
		"unobserved_claim_ids": []string{}, "original_finder_routes": []any{},
		"investigation_readiness": []any{map[string]any{"finding_id": "finding-s8-boundary", "status": "ready"}},
		"sealed_at":               "2026-10-14T00:00:00Z", "sealed_by": "round-consumer", "revision": 1,
	}
	batchBytes, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("observation-batch.schema.json", batchBytes); err != nil {
		t.Fatalf("fixture ObservationBatch schema: %v", err)
	}
	batchPath := filepath.Join(root, filepath.FromSlash(batchRef))
	if err := os.MkdirAll(filepath.Dir(batchPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(batchPath, batchBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	finding := map[string]any{
		"finding_id": "finding-s8-boundary",
		"encounter": map[string]any{
			"inspection_entry":     "internal/edge/handler.go",
			"first_bad_checkpoint": "the decoded value is absent",
		},
	}
	findingBytes, err := json.Marshal(finding)
	if err != nil {
		t.Fatal(err)
	}
	packet := workpacket.HypothesisResultPacket{Binding: workpacket.HypothesisResultBinding{
		ObservationBatchRef: batchRef, ObservationBatchSHA256: workpacket.Digest(batchBytes),
	}, Inputs: workpacket.HypothesisResultInputs{Findings: []workpacket.HypothesisResultFinding{{
		FindingID: "finding-s8-boundary", Document: json.RawMessage(findingBytes),
	}}}}

	for _, ref := range []string{
		"finding:finding-s8-boundary",
		"finding:finding-s8-boundary#/encounter/inspection_entry",
		"finding:finding-s8-boundary#/encounter/first_bad_checkpoint",
		"evidence://sealed-blocked-claim",
	} {
		if err := validateS8SourceBoundaryRefs(root, packet, []string{ref}); err != nil {
			t.Errorf("frozen source boundary %q rejected: %v", ref, err)
		}
	}
	for _, ref := range []string{
		"finding:unknown-finding",
		"finding:finding-s8-boundary#/encounter/not-present",
		"finding:finding-s8-boundary#/encounter/../secret",
		"source://unregistered-boundary",
		"evidence://not-in-blocked-claim",
	} {
		if err := validateS8SourceBoundaryRefs(root, packet, []string{ref}); err == nil {
			t.Errorf("unfrozen source boundary %q was accepted", ref)
		}
	}
}

func TestS8MaterializedFollowupHypothesesAreServerIdentified(t *testing.T) {
	answer := []workpacket.HypothesisNewQuestion{{
		Statement:        "A second cache boundary may preserve the stale entry.",
		Invariant:        "Every cache boundary must invalidate the stale entry.",
		Discriminator:    "Compare the entry before and after the second boundary.",
		ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{Support: "the stale entry remains", Refute: "the stale entry is invalidated"},
		SourceFindingIDs: []string{"finding-s8-boundary"}, EvidenceRefs: []string{"evidence://sealed-blocked-claim"},
	}}
	one, err := materializeS8NewHypotheses("packet-s8-test", answer)
	if err != nil {
		t.Fatal(err)
	}
	two, err := materializeS8NewHypotheses("packet-s8-test", answer)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one, two) || len(one) != 1 {
		t.Fatalf("follow-up identity is not deterministic: first=%#v second=%#v", one, two)
	}
	if one[0]["hypothesis_id"] == "" || one[0]["assignment_id"] == "" || one[0]["status"] != "open" {
		t.Fatalf("follow-up question lacks server identities/open status: %#v", one[0])
	}
	empty, err := materializeS8NewHypotheses("packet-s8-empty", nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty follow-up projection must remain an explicit empty list: %#v err=%v", empty, err)
	}
}
