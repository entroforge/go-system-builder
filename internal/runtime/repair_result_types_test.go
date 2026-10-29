package runtime

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestRepairResultRawFactsRoundTripCanonicalizesWithoutDrift(t *testing.T) {
	submitted := json.RawMessage(`{"verdict":"completed","unit_results":[{"unit_id":"u1","status":"pass","evidence_refs":["ev1"],"implementation_summary":"done","restored_invariant_refs":[]}],"before_fix_results":[],"detection_gap_artifacts":[],"evidence_refs":["ev1"],"scope_deviations":[],"residual_risks":[],"migration_ref":null,"rollback_ref":null}`)
	provenance := RepairResultCandidateProvenance{SubmittedAnswer: submitted}
	wire, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var gotProvenance RepairResultCandidateProvenance
	if err := json.Unmarshal(wire, &gotProvenance); err != nil {
		t.Fatalf("unmarshal candidate provenance: %v", err)
	}
	wantSubmitted, err := canonicalRuntimeJSON(submitted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotProvenance.SubmittedAnswer, wantSubmitted) {
		t.Fatalf("submitted answer was not canonicalized on readback:\n got %s\nwant %s", gotProvenance.SubmittedAnswer, wantSubmitted)
	}
	if workpacket.Digest(gotProvenance.SubmittedAnswer) != workpacket.Digest(wantSubmitted) {
		t.Fatal("submitted answer digest drifted across MarshalIndent/readback")
	}

	verifiedFacts := json.RawMessage(`{"check_receipts":[{"index":0,"status":"passed","profile_digest":"profile-a"}],"tested_head":"abc123","source_head":"def456","completion":{"complete":false,"reason":"awaiting main"}}`)
	proof := RepairResultMainProof{VerifiedFacts: verifiedFacts}
	wire, err = json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var gotProof RepairResultMainProof
	if err := json.Unmarshal(wire, &gotProof); err != nil {
		t.Fatalf("unmarshal Main proof: %v", err)
	}
	wantFacts, err := canonicalRuntimeJSON(verifiedFacts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotProof.VerifiedFacts, wantFacts) {
		t.Fatalf("verified facts were not canonicalized on readback:\n got %s\nwant %s", gotProof.VerifiedFacts, wantFacts)
	}
	if workpacket.Digest(gotProof.VerifiedFacts) != workpacket.Digest(wantFacts) {
		t.Fatal("verified-facts digest drifted across MarshalIndent/readback")
	}

	for name, data := range map[string][]byte{
		"submitted answer": []byte(`{"submitted_answer":{"x":1,"x":2}}`),
		"verified facts":   []byte(`{"verified_facts":{"x":1,"x":2}}`),
	} {
		t.Run(name+" duplicate keys rejected", func(t *testing.T) {
			if name == "submitted answer" {
				var v RepairResultCandidateProvenance
				if err := json.Unmarshal(data, &v); err == nil {
					t.Fatal("duplicate nested submitted-answer keys accepted")
				}
				return
			}
			var v RepairResultMainProof
			if err := json.Unmarshal(data, &v); err == nil {
				t.Fatal("duplicate nested verified-facts keys accepted")
			}
		})
	}
}

func TestProjectRepairResultExecutionFreezesDefinitionOnly(t *testing.T) {
	base := map[string]any{
		"assignment_record":        map[string]any{"path": "record.json", "sha256": "record-sha"},
		"assignment_record_sha256": "record-sha",
		"worker_checks":            []any{map[string]any{"index": 0, "command": "go test ./internal/api"}},
		"check_profiles":           []any{map[string]any{"id": "go-test", "location": "worker"}},
		"purpose":                  "repair",
		"work_packet_id":           "packet-a",
		"stage_entry":              "repair-result",
		"check_location":           "worker",
		"platform_session_id":      "session-a",
		"assignment_id":            "assignment-a",
		"runtime_id":               "runtime-a",
		"baseline_generation":      2,
		"execution_generation":     3,
		"agent_id":                 "owner-a",
		"worktree_path":            "/work/assignment-a",
		"branch":                   "worker-a",
		"target_branch":            "main",
		"base_commit":              "base-a",
		"write_paths":              []any{"internal/api"},
		"required_checks":          []any{map[string]any{"index": 0, "command": "go test ./internal/api"}},
		"inputs":                   map[string]any{"assignment_record": "record.json", "source_head": "source-a"},
		"bootstrap_sha256":         "bootstrap-a",
		"status":                   "prepared",
		"tool_receipts":            []any{map[string]any{"status": "passed", "sha256": "mutable"}},
		"delivery_ref":             "delivery-old",
		"rework_ref":               "rework-old",
	}

	projected, err := ProjectRepairResultExecution(base)
	if err != nil {
		t.Fatal(err)
	}
	baseDigest := workpacket.Digest(projected)

	mutable := cloneRuntimeMap(t, base)
	mutable["status"] = "completed"
	mutable["tool_receipts"] = []any{map[string]any{"status": "failed", "sha256": "different"}}
	mutable["delivery_ref"] = "delivery-new"
	mutable["rework_ref"] = "rework-new"
	projected, err = ProjectRepairResultExecution(mutable)
	if err != nil {
		t.Fatal(err)
	}
	if got := workpacket.Digest(projected); got != baseDigest {
		t.Fatalf("mutable progress changed frozen execution digest: got %s want %s", got, baseDigest)
	}

	for _, key := range []string{"worktree_path", "agent_id", "base_commit", "check_profiles", "inputs"} {
		t.Run(key, func(t *testing.T) {
			changed := cloneRuntimeMap(t, base)
			changed[key] = map[string]any{"changed": true}
			gotJSON, err := ProjectRepairResultExecution(changed)
			if err != nil {
				t.Fatal(err)
			}
			if got := workpacket.Digest(gotJSON); got == baseDigest {
				t.Fatalf("frozen execution field %q did not change digest", key)
			}
		})
	}

	var projectedMap map[string]json.RawMessage
	if err := json.Unmarshal(projected, &projectedMap); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "tool_receipts", "delivery_ref", "rework_ref"} {
		if _, ok := projectedMap[key]; ok {
			t.Errorf("mutable execution field %q leaked into projection", key)
		}
	}
}

func TestProjectRepairResultMainFactsIgnoresLifecycleAndBindsVerifiedFacts(t *testing.T) {
	base := repairResultCheckpointFixture()
	first, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	firstProjection, err := ProjectRepairResultMainFacts(first)
	if err != nil {
		t.Fatalf("project verified checkpoint: %v", err)
	}
	firstDigest := workpacket.Digest(firstProjection)

	lifecycle := cloneRuntimeMap(t, base)
	lifecycle["status"] = "acknowledged"
	lifecycle["revision"] = float64(91)
	lifecycle["updated_at"] = "2026-10-14T12:59:59Z"
	lifecycle["cleanup"] = map[string]any{"status": "complete", "removed": []any{"worktree"}}
	lifecycle["diagnostic"] = "replayed after cleanup"
	second, err := json.Marshal(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	secondProjection, err := ProjectRepairResultMainFacts(second)
	if err != nil {
		t.Fatalf("project lifecycle-updated checkpoint: %v", err)
	}
	if got := workpacket.Digest(secondProjection); got != firstDigest {
		t.Fatalf("ACK/cleanup lifecycle mutation changed Main facts digest: got %s want %s", got, firstDigest)
	}

	for _, mutate := range []struct {
		name string
		fn   func(map[string]any)
	}{
		{"source head", func(cp map[string]any) { cp["source_head"] = "source-b" }},
		{"tested head", func(cp map[string]any) { cp["tested_head"] = "tested-b" }},
		{"check receipt", func(cp map[string]any) {
			cp["check_receipts"] = []any{map[string]any{"receipt_ref": "new.json", "sha256": "new-sha"}}
		}},
		{"profile facts", func(cp map[string]any) {
			cp["canonical_main_facts"] = map[string]any{"checks": []any{map[string]any{"profile_digest": "profile-b", "status": "passed"}}}
		}},
		{"completion facts", func(cp map[string]any) { cp["completion_report_sha256"] = "completion-b" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			changed := cloneRuntimeMap(t, base)
			mutate.fn(changed)
			data, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := ProjectRepairResultMainFacts(data)
			if err != nil {
				t.Fatal(err)
			}
			if got := workpacket.Digest(projection); got == firstDigest {
				t.Fatal("verified Main fact mutation did not change digest")
			}
		})
	}

	for name, input := range map[string][]byte{
		"missing required tested head": []byte(`{"assignment_id":"a","baseline_generation":1,"source_head":"s","merge_commit":"m","verified_at":"now"}`),
		"duplicate keys":               []byte(`{"assignment_id":"a","assignment_id":"b","baseline_generation":1,"source_head":"s","merge_commit":"m","tested_head":"t","verified_at":"now"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ProjectRepairResultMainFacts(input); err == nil {
				t.Fatal("invalid Main facts accepted")
			}
		})
	}
}

func repairResultCheckpointFixture() map[string]any {
	return map[string]any{
		"assignment_id":            "assignment-a",
		"task_id":                  "TASK-a",
		"baseline_generation":      2,
		"assignment_record":        "record.json",
		"assignment_record_sha256": "record-sha",
		"source_branch":            "worker-a",
		"source_head":              "source-a",
		"target_branch":            "main",
		"target_head":              "target-a",
		"merge_base":               "base-a",
		"merge_commit":             "merge-a",
		"tested_head":              "tested-a",
		"worktree_path":            "/work/assignment-a",
		"changed_paths":            []any{"internal/api/payload.go"},
		"changed_paths_sha256":     "changed-paths-sha",
		"canonical_main_facts":     map[string]any{"checks": []any{map[string]any{"command": "go test ./internal/api", "status": "passed", "profile_digest": "profile-a", "receipt_sha256": "receipt-a"}}},
		"completion_report_path":   "completion/report.json",
		"completion_report_sha256": "completion-a",
		"verified_at":              "2026-10-14T12:30:00Z",
		"idempotency_key":          "integrate:assignment-a",
		"check_receipts":           []any{map[string]any{"receipt_ref": "check-a.json", "sha256": "check-a"}},
		"tool_check_receipts":      []any{map[string]any{"receipt_ref": "tool-a.json", "sha256": "tool-a"}},
		"status":                   "verified",
		"revision":                 float64(3),
		"updated_at":               "2026-10-14T12:30:00Z",
		"cleanup":                  map[string]any{"status": "pending"},
		"diagnostic":               "initial verification",
	}
}

func cloneRuntimeMap(t *testing.T, in map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRepairResultMainFactsProjectionIsStableAcrossCanonicalRoundTrip(t *testing.T) {
	data, err := json.Marshal(repairResultCheckpointFixture())
	if err != nil {
		t.Fatal(err)
	}
	projected, err := ProjectRepairResultMainFacts(data)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(projected, &decoded); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bytes.TrimSpace(projected), bytes.TrimSpace(encoded)) {
		t.Fatalf("canonical projection changed after decode/marshal:\n%s\n%s", projected, encoded)
	}
}
