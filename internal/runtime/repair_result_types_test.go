package runtime

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
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

	verifiedFacts := json.RawMessage(`{"check_receipts":[{"index":0,"status":"passed"}],"completion":{"complete":false,"reason":"awaiting main"}}`)
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
		"assignment_record":    map[string]any{"path": "record.json"},
		"worker_checks":        []any{map[string]any{"index": 0, "command": "go test ./internal/api"}},
		"check_profiles":       []any{map[string]any{"id": "go-test", "location": "worker"}},
		"purpose":              "repair",
		"work_packet_id":       "packet-a",
		"stage_entry":          "repair-result",
		"check_location":       "worker",
		"platform_session_id":  "session-a",
		"assignment_id":        "assignment-a",
		"runtime_id":           "runtime-a",
		"baseline_generation":  2,
		"execution_generation": 3,
		"agent_id":             "owner-a",
		"worktree_path":        "/work/assignment-a",
		"branch":               "worker-a",
		"target_branch":        "main",
		"base_commit":          "base-a",
		"write_paths":          []any{"internal/api"},
		"required_checks":      []any{map[string]any{"index": 0, "command": "go test ./internal/api"}},
		"inputs":               map[string]any{"assignment_record": "record.json"},
		"bootstrap_sha256":     "bootstrap-a",
		"status":               "prepared",
		"tool_receipts":        []any{map[string]any{"status": "passed"}},
		"delivery_ref":         "delivery-old",
		"rework_ref":           "rework-old",
	}

	projected, err := ProjectRepairResultExecution(base)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), projected...)

	mutable := cloneRuntimeMap(t, base)
	mutable["status"] = "completed"
	mutable["tool_receipts"] = []any{map[string]any{"status": "failed"}}
	mutable["delivery_ref"] = "delivery-new"
	mutable["rework_ref"] = "rework-new"
	projected, err = ProjectRepairResultExecution(mutable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(projected, original) {
		t.Fatal("mutable progress changed original execution facts")
	}

	for _, key := range []string{"worktree_path", "agent_id", "check_profiles"} {
		t.Run(key, func(t *testing.T) {
			changed := cloneRuntimeMap(t, base)
			changed[key] = map[string]any{"changed": true}
			gotJSON, err := ProjectRepairResultExecution(changed)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(gotJSON, original) {
				t.Fatalf("execution fact %q was lost", key)
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

func TestProjectRepairResultMainFactsPreservesActualWork(t *testing.T) {
	base := repairResultCheckpointFixture()
	data, _ := json.Marshal(base)
	original, err := ProjectRepairResultMainFacts(data)
	if err != nil {
		t.Fatal(err)
	}
	later := cloneRuntimeMap(t, base)
	later["status"], later["revision"], later["cleanup"] = "acknowledged", 91, map[string]any{"status": "complete"}
	data, _ = json.Marshal(later)
	replay, err := ProjectRepairResultMainFacts(data)
	if err != nil || !bytes.Equal(original, replay) {
		t.Fatalf("lifecycle changed recorded work: %v", err)
	}
	changed := cloneRuntimeMap(t, base)
	changed["changed_paths"] = []any{"internal/api/other.go"}
	data, _ = json.Marshal(changed)
	actual, err := ProjectRepairResultMainFacts(data)
	if err != nil || bytes.Equal(original, actual) {
		t.Fatalf("actual changed paths were lost: %v", err)
	}
	for _, raw := range [][]byte{[]byte(`{"baseline_generation":1}`), []byte(`{"assignment_id":"a","assignment_id":"b"}`)} {
		if _, err := ProjectRepairResultMainFacts(raw); err == nil {
			t.Fatal("invalid original responsibility accepted")
		}
	}
}

func repairResultCheckpointFixture() map[string]any {
	return map[string]any{
		"assignment_id":          "assignment-a",
		"task_id":                "TASK-a",
		"baseline_generation":    2,
		"assignment_record":      "record.json",
		"source_branch":          "worker-a",
		"target_branch":          "main",
		"worktree_path":          "/work/assignment-a",
		"changed_paths":          []any{"internal/api/payload.go"},
		"canonical_main_facts":   map[string]any{"checks": []any{map[string]any{"command": "go test ./internal/api", "status": "passed", "receipt_ref": "receipt-a"}}},
		"completion_report_path": "completion/report.json",
		"verified_at":            "2026-10-14T12:30:00Z",
		"idempotency_key":        "integrate:assignment-a",
		"check_receipts":         []any{map[string]any{"receipt_ref": "check-a.json"}},
		"tool_check_receipts":    []any{map[string]any{"receipt_ref": "tool-a.json"}},
		"status":                 "verified",
		"revision":               float64(3),
		"updated_at":             "2026-10-14T12:30:00Z",
		"cleanup":                map[string]any{"status": "pending"},
		"diagnostic":             "initial verification",
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

func TestS9MainFactsKeepsProfessionalContent(t *testing.T) {
	input := []byte(`{"assignment_id":"assignment-a","baseline_generation":1,"source_branch":"fix/a","changed_paths":["a.go"],"canonical_main_facts":{"checks":[{"command":"go test ./...","result":"pass","note":"SHA is a user discussion","custom":{"sha256":"business-value"}}]}}`)
	data, err := ProjectRepairResultMainFacts(input)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	facts := got["canonical_main_facts"].(map[string]any)
	check := facts["checks"].([]any)[0].(map[string]any)
	if got["source_branch"] != "fix/a" || check["note"] != "SHA is a user discussion" || check["custom"].(map[string]any)["sha256"] != "business-value" {
		t.Fatal("actual work or professional content changed")
	}
}
