package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestReady_NotReadyPlanningContracts(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "planning", "contracts", 12)
	req039fixtures.WriteState(t, root, state)

	before := readRevision(t, root)
	report := runReadyJSON(t, root)
	after := readRevision(t, root)

	if after != before {
		t.Fatalf("ready must not mutate revision: before=%d after=%d", before, after)
	}
	if report["cursor"] != "planning.contracts" {
		t.Fatalf("cursor=%v, want planning.contracts", report["cursor"])
	}
	if report["stage"] != "S3" {
		t.Fatalf("stage=%v, want S3", report["stage"])
	}
	if report["status"] != "not_ready" {
		t.Fatalf("status=%v, want not_ready", report["status"])
	}
	if report["gate_id"] != "GATE-PLANNING-CONTRACTS-COMPLETE" {
		t.Fatalf("gate_id=%v", report["gate_id"])
	}
	if report["candidate_transition"] != "PTR-PLAN-02" {
		t.Fatalf("candidate_transition=%v", report["candidate_transition"])
	}
	missing, _ := report["missing"].([]any)
	if len(missing) == 0 {
		t.Fatalf("not_ready must list missing items, got %#v", report["missing"])
	}
	instruction, _ := report["instruction"].(string)
	if !strings.Contains(instruction, "do not call transition") {
		t.Fatalf("instruction must forbid hand-push transition, got %q", instruction)
	}
}

func TestReady_SatisfiedPlanningContracts(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "planning", "contracts", 14)
	req039fixtures.WritePlanningContractPass(t, root, state)
	req039fixtures.WriteState(t, root, state)

	before := readRevision(t, root)
	report := runReadyJSON(t, root)
	after := readRevision(t, root)

	if after != before {
		t.Fatalf("ready must not mutate revision: before=%d after=%d", before, after)
	}
	if report["status"] != "satisfied" {
		t.Fatalf("status=%v, want satisfied; full=%#v", report["status"], report)
	}
	if report["gate_id"] != "GATE-PLANNING-CONTRACTS-COMPLETE" {
		t.Fatalf("gate_id=%v", report["gate_id"])
	}
	if report["candidate_transition"] != "PTR-PLAN-02" {
		t.Fatalf("candidate_transition=%v", report["candidate_transition"])
	}
	missing, _ := report["missing"].([]any)
	if len(missing) != 0 {
		t.Fatalf("satisfied must have empty missing, got %#v", missing)
	}
}

func TestReady_ConflictVerificationDelivery(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "verification", "delivery", 32)
	req039fixtures.SeedConflictingDeliveryEvents(t, root, state)
	req039fixtures.WriteState(t, root, state)

	before := readRevision(t, root)
	report := runReadyJSON(t, root)
	after := readRevision(t, root)

	if after != before {
		t.Fatalf("ready must not mutate revision: before=%d after=%d", before, after)
	}
	if report["status"] != "unknown" {
		t.Fatalf("status=%v, want unknown; full=%#v", report["status"], report)
	}
	if report["error_code"] != "LOOP_TRIGGER_CONFLICT" {
		t.Fatalf("error_code=%v, want LOOP_TRIGGER_CONFLICT; full=%#v", report["error_code"], report)
	}
}

func runReadyJSON(t *testing.T, root string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"ready", "--root", root}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ready exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode ready json: %v\nstdout=%s", err, stdout.String())
	}
	return report
}

func readRevision(t *testing.T, root string) int {
	t.Helper()
	snap, err := runtime.NewStore(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snap.Revision
}
