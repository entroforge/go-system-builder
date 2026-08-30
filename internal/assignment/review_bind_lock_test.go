package assignment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// §14.1 resource-lock conflict detection + queueing at register-workgroup
// (L3-S7 §4.5 + L4 §6.2).
//
// These tests exercise the deterministic conflict detector directly: a
// deterministic mechanical gate that records the held keys, never rejects
// a dispatching assignment, and never trims coverage when the round is
// already at coverage_complete. The Register-workgroup integration path
// composes the same logic through bindReviewPlanAssignments.
// ---------------------------------------------------------------------------

func rowWithLocks(status string, locks []string) map[string]any {
	items := make([]any, len(locks))
	for i, l := range locks {
		items[i] = l
	}
	row := map[string]any{
		"lens":           "qa",
		"claim_ids":      []any{"claim-dummy"},
		"status":         status,
		"agent_id":       nil,
		"result_ref":     nil,
		"resource_locks": items,
		"queue_reason":   nil,
	}
	if status == "dispatched" {
		row["agent_id"] = "agent-dummy"
	}
	return row
}

func TestFindResourceLockConflictEmpty(t *testing.T) {
	// Empty candidate locks never conflict.
	projection := map[string]any{
		"assignment-A": rowWithLocks("dispatched", []string{"port:8080"}),
	}
	candidate := rowWithLocks("planned", nil)
	if got := findResourceLockConflict("B", candidate, projection); got != "" {
		t.Fatalf("empty candidate must not conflict, got %q", got)
	}
}

func TestFindResourceLockConflictSingleKey(t *testing.T) {
	projection := map[string]any{
		"assignment-A": rowWithLocks("dispatched", []string{"port:8080"}),
	}
	candidate := rowWithLocks("planned", []string{"port:8080"})
	got := findResourceLockConflict("B", candidate, projection)
	if got != "port:8080" {
		t.Fatalf("expected port:8080 conflict, got %q", got)
	}
}

func TestFindResourceLockConflictMultipleKeys(t *testing.T) {
	projection := map[string]any{
		"assignment-A": rowWithLocks("dispatched", []string{"port:8080", "shared:fixture"}),
	}
	candidate := rowWithLocks("planned", []string{"shared:fixture", "port:8080"})
	got := findResourceLockConflict("B", candidate, projection)
	if got != "port:8080,shared:fixture" {
		t.Fatalf("expected sorted multi-key conflict, got %q", got)
	}
}

func TestFindResourceLockConflictIgnoresConsumedHolder(t *testing.T) {
	// The holder's Result was consumed → lock released → candidate must dispatch.
	projection := map[string]any{
		"assignment-A": rowWithLocks("consumed", []string{"port:8080"}),
	}
	candidate := rowWithLocks("planned", []string{"port:8080"})
	if got := findResourceLockConflict("B", candidate, projection); got != "" {
		t.Fatalf("consumed holder must release lock, got conflict %q", got)
	}
}

func TestFindResourceLockConflictIgnoresSelf(t *testing.T) {
	projection := map[string]any{
		"assignment-A": rowWithLocks("dispatched", []string{"port:8080"}),
	}
	candidate := rowWithLocks("dispatched", []string{"port:8080"})
	// Self-check should be skipped — a row can't conflict with itself.
	if got := findResourceLockConflict("assignment-A", candidate, projection); got != "" {
		t.Fatalf("self-reference must not produce a conflict, got %q", got)
	}
}

func TestFindResourceLockConflictIgnoresResultSubmitted(t *testing.T) {
	// result_submitted still holds the lock (consumer hasn't flipped to
	// consumed yet) — the gate must remain conservative.
	projection := map[string]any{
		"assignment-A": rowWithLocks("result_submitted", []string{"port:8080"}),
	}
	rowA := projection["assignment-A"].(map[string]any)
	rowA["result_ref"] = "review-result-x"
	candidate := rowWithLocks("planned", []string{"port:8080"})
	if got := findResourceLockConflict("B", candidate, projection); got != "port:8080" {
		t.Fatalf("result_submitted must still hold the lock, got %q", got)
	}
}

func TestFindResourceLockConflictIgnoresPlannedOrCancelled(t *testing.T) {
	projection := map[string]any{
		"assignment-A": rowWithLocks("planned", []string{"port:8080"}),
	}
	candidate := rowWithLocks("planned", []string{"port:8080"})
	if got := findResourceLockConflict("B", candidate, projection); got != "" {
		t.Fatalf("planned holder must not hold the lock, got %q", got)
	}
}

func TestIsLockHeldByStates(t *testing.T) {
	cases := []struct {
		status    string
		held      bool
		resultRef any
	}{
		{"planned", false, nil},
		{"queued", false, nil},
		{"dispatched", true, nil},
		{"dispatched", false, "review-result-x"},
		{"result_submitted", true, "review-result-x"},
		{"consumed", false, "review-result-x"},
		{"cancelled", false, nil},
	}
	for _, tc := range cases {
		row := map[string]any{"status": tc.status}
		if tc.resultRef != nil {
			row["result_ref"] = tc.resultRef
		}
		if got := isLockHeldBy(row); got != tc.held {
			t.Errorf("isLockHeldBy(status=%q result=%v) = %v, want %v", tc.status, tc.resultRef, got, tc.held)
		}
	}
}

func TestReadLockSetFiltersEmptyAndWhitespace(t *testing.T) {
	raw := []any{"port:8080", "", "   ", "shared:fixture"}
	got := readLockSet(raw)
	if len(got) != 2 || !got["port:8080"] || !got["shared:fixture"] {
		t.Fatalf("readLockSet must drop empties/whitespace, got %v", got)
	}
	if got := readLockSet(nil); len(got) != 0 {
		t.Fatalf("nil must produce empty set, got %v", got)
	}
}

// §14.1 integration: a dispatch that would conflict records queue_reason
// and stays planned; its Claims stay planned; the round is never trimmed.
// This walks bindReviewPlanAssignments directly so we don't depend on the
// full team-manifest schema surface.
func TestBindReviewPlanQueuesResourceLockConflict(t *testing.T) {
	state, root := buildBindReviewState(t, []planAssignmentFixture{
		{id: "assignment-A", lens: "qa", claims: []string{"claim-A"}, rowLocks: []string{"port:8080"}, status: "dispatched"},
		{id: "assignment-B", lens: "qa", claims: []string{"claim-B"}, rowLocks: []string{"port:8080"}, status: "planned"},
	})
	value := manifest{
		WorkgroupKind: "qa",
		Assignments: []assignment{
			{AssignmentID: "assignment-B", RoleFamily: "qa", AgentID: "agent-B",
				AgentDefinitionRef: "agents/qa.md", ClaimIDs: []string{"claim-B"},
				DispatchMode: "plan_checkpoint"},
		},
	}
	if err := bindReviewPlanAssignments(root, state, value); err != nil {
		t.Fatalf("bindReviewPlanAssignments must NOT reject a lock conflict, got %v", err)
	}
	assignments := state["review"].(map[string]any)["assignments"].(map[string]any)
	a := assignments["assignment-A"].(map[string]any)
	b := assignments["assignment-B"].(map[string]any)
	if a["status"] != "dispatched" {
		t.Fatalf("holder A status = %v, want dispatched", a["status"])
	}
	if b["status"] != "planned" {
		t.Fatalf("conflicting B status = %v, want planned (queued)", b["status"])
	}
	reason, _ := b["queue_reason"].(string)
	if !strings.HasPrefix(reason, "resource_lock:") {
		t.Fatalf("queue_reason = %q, want resource_lock:<keys>", reason)
	}
	if !strings.Contains(reason, "port:8080") {
		t.Fatalf("queue_reason must name the held key, got %q", reason)
	}
	claims := state["review"].(map[string]any)["claims"].(map[string]any)
	if claims["claim-B"].(map[string]any)["disposition"] != "planned" {
		t.Fatalf("queued Claims must stay planned, got %v",
			claims["claim-B"])
	}
}

// §14.1: distinct resource locks → both Assignments dispatch in the same
// round; no queueing fires.
func TestBindReviewPlanNoConflictOnDistinctLocks(t *testing.T) {
	state, root := buildBindReviewState(t, []planAssignmentFixture{
		{id: "assignment-A", lens: "qa", claims: []string{"claim-A"}, rowLocks: []string{"port:8080"}, status: "dispatched"},
		{id: "assignment-B", lens: "qa", claims: []string{"claim-B"}, rowLocks: []string{"port:9090"}, status: "planned"},
	})
	value := manifest{
		WorkgroupKind: "qa",
		Assignments: []assignment{
			{AssignmentID: "assignment-B", RoleFamily: "qa", AgentID: "agent-B",
				AgentDefinitionRef: "agents/qa.md", ClaimIDs: []string{"claim-B"},
				DispatchMode: "plan_checkpoint"},
		},
	}
	if err := bindReviewPlanAssignments(root, state, value); err != nil {
		t.Fatalf("bindReviewPlanAssignments: %v", err)
	}
	assignments := state["review"].(map[string]any)["assignments"].(map[string]any)
	b := assignments["assignment-B"].(map[string]any)
	if b["status"] != "dispatched" {
		t.Fatalf("distinct locks must dispatch, got status=%v", b["status"])
	}
	if b["queue_reason"] != nil {
		t.Fatalf("queue_reason must stay nil on dispatch, got %v", b["queue_reason"])
	}
}

type planAssignmentFixture struct {
	id       string
	lens     string
	claims   []string
	rowLocks []string
	status   string
}

// buildBindReviewState creates a runtime state with a registered ReviewPlan
// and a pre-populated assignments/claims projection. The plan file lives
// under t.TempDir()/<planRel> so review.LoadPlan can hash-verify it; the
// function returns the state and the root path used by bindReviewPlanAssignments.
func buildBindReviewState(t *testing.T, fixtures []planAssignmentFixture) (map[string]any, string) {
	t.Helper()
	plan := map[string]any{
		"schema_version":      "1.0.0",
		"review_plan_id":      "review-plan-bind-lock",
		"review_round":        1,
		"baseline_generation": 1,
		"frozen_subjects": []any{
			map[string]any{"path": "docs/tasks/TASK-700.md", "sha256": strings.Repeat("f", 64), "kind": "task"},
		},
		"claims":                          []any{},
		"assignments":                     []any{},
		"e2e_coverage_state":              "not_applicable",
		"verification_artifact_workspace": nil,
		"dispatch_capacity_policy":        "coverage_complete",
		"created_by":                      "test",
		"created_at":                      "2026-08-23T00:00:00Z",
	}
	planClaims := []any{}
	planAssignments := []any{}
	for _, a := range fixtures {
		planAssignments = append(planAssignments, map[string]any{
			"assignment_id":        a.id,
			"lens":                 a.lens,
			"claim_ids":            a.claims,
			"non_overlap_boundary": "owns its lock scope",
			"execution_wave":       "static",
			"resource_locks":       a.rowLocks,
		})
		for _, claimID := range a.claims {
			planClaims = append(planClaims, map[string]any{
				"claim_id": claimID, "lens": a.lens, "target": "x",
				"assertion": "covered", "oracle": "evidence", "method": "review",
				"applicability": "required", "source_refs": []any{"REQ-700"},
			})
		}
	}
	plan["claims"] = planClaims
	plan["assignments"] = planAssignments

	root := t.TempDir()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	pathRel := ".claude/review/plans/review-plan-bind-lock.json"
	absPath := root + "/" + filepath.FromSlash(pathRel)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	shaSum := sha256.Sum256(append(data, '\n'))
	sha := hex.EncodeToString(shaSum[:])

	state := map[string]any{
		"runtime_id": "loop-REQ-700",
		"lifecycle":  map[string]any{"state": "verification", "phase": "running"},
		"baseline":   map[string]any{"generation": 1},
		"review": map[string]any{
			"round": 1,
			"plan": map[string]any{
				"plan_id": "review-plan-bind-lock", "path": pathRel, "sha256": sha,
				"revision":           1,
				"review_round":       1,
				"status":             "running",
				"e2e_coverage_state": "not_applicable",
				"submitted_at":       "2026-08-23T00:00:00Z",
			},
			"claims":            map[string]any{},
			"assignments":       map[string]any{},
			"observation_batch": nil,
		},
	}
	projection := state["review"].(map[string]any)["assignments"].(map[string]any)
	claimsProj := state["review"].(map[string]any)["claims"].(map[string]any)
	for _, a := range fixtures {
		items := make([]any, len(a.rowLocks))
		for i, l := range a.rowLocks {
			items[i] = l
		}
		row := map[string]any{
			"lens": a.lens, "claim_ids": stringSliceToAny(a.claims), "status": a.status,
			"agent_id": nil, "result_ref": nil, "resource_locks": items, "queue_reason": nil,
		}
		if a.status == "dispatched" {
			row["agent_id"] = "agent-" + a.id
		}
		projection[a.id] = row
		for _, claimID := range a.claims {
			claimsProj[claimID] = map[string]any{
				"lens": a.lens, "applicability": "required", "disposition": "planned",
				"assignment_id": a.id, "result_id": nil, "finding_ids": []any{},
			}
		}
	}
	return state, root
}

func stringSliceToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// Sanity: queue_reason stores the exact held keys sorted and comma-joined.
func TestQueueReasonFormat(t *testing.T) {
	state, root := buildBindReviewState(t, []planAssignmentFixture{
		{id: "assignment-A", lens: "qa", claims: []string{"claim-A"}, rowLocks: []string{"port:8080", "dataset:users"}, status: "dispatched"},
		{id: "assignment-B", lens: "qa", claims: []string{"claim-B"}, rowLocks: []string{"dataset:users", "port:8080", "shared:fixture"}, status: "planned"},
	})
	value := manifest{
		WorkgroupKind: "qa",
		Assignments: []assignment{
			{AssignmentID: "assignment-B", RoleFamily: "qa", AgentID: "agent-B",
				AgentDefinitionRef: "agents/qa.md", ClaimIDs: []string{"claim-B"},
				DispatchMode: "plan_checkpoint"},
		},
	}
	if err := bindReviewPlanAssignments(root, state, value); err != nil {
		t.Fatalf("bindReviewPlanAssignments: %v", err)
	}
	reason, _ := state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-B"].(map[string]any)["queue_reason"].(string)
	want := "resource_lock:dataset:users,port:8080"
	if reason != want {
		t.Fatalf("queue_reason = %q, want %q", reason, want)
	}
	// shared:fixture is a candidate-only lock; it must NOT appear in the
	// queue_reason (the reason names held keys, not candidate keys).
	if strings.Contains(reason, "shared:fixture") {
		t.Fatalf("queue_reason must name held keys only, got %q", reason)
	}
}

// compile-time reference to fmt to keep the import stable for future debug.
var _ = fmt.Sprintf
