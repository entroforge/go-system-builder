package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS7AcceptedOutletKeepsHistoricalReturnOutOfCurrentQueue(t *testing.T) {
	state := s7RecoveryFixtureState("running", "running")
	state["baseline"] = map[string]any{"generation": 1}
	p := workpacket.ReviewResultPacket{Binding: workpacket.ReviewResultBinding{
		RuntimeID: "loop-REQ-TEST", BaselineGeneration: 1, ReviewRound: 1, ReviewPlanID: "plan-r2", ReviewPlanRevision: 1,
	}}
	text := nativeS7AcceptedOutletContext(t.TempDir(), runtime.Snapshot{State: state}, p)
	if !strings.Contains(text, "earlier responsibility") || strings.Contains(text, "assignment-e2e-1") || strings.Contains(text, "manifest-draft") {
		t.Fatalf("historical return reactivated current/old queue: %s", text)
	}
	p.Binding.ReviewRound = 2
	mapFieldCLI(mapFieldCLI(state, "review"), "plan")["status"] = "clean"
	state["pause"] = map[string]any{"reason": "human checkpoint"}
	text = nativeS7AcceptedOutletContext(t.TempDir(), runtime.Snapshot{State: state}, p)
	if !strings.Contains(text, "TR-010/TR-011") || strings.Contains(text, "review-continue") {
		t.Fatalf("pause resumed a clean exit: %s", text)
	}
}

func TestS7AcceptedRemainingProjectionPreservesClosedRouting(t *testing.T) {
	for _, tc := range []struct{ status, want string }{
		{"observation_sealed", "TR-008"}, {"paused", "TR-010/TR-011"}, {"stale", "baseline drift"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			state := s7RecoveryFixtureState(tc.status, tc.status)
			text := nativeS7RemainingOutletContext(t.TempDir(), state)
			if !strings.Contains(text, tc.want) || strings.Contains(text, "manifest-draft") || strings.Contains(text, "registered Reviewer's queued dispatch") {
				t.Fatalf("closed routing dispatched old queue or lost next action: %s", text)
			}
		})
	}
}

func TestS7AcceptedRemainingProjectionUsesOriginalPrerequisites(t *testing.T) {
	root := t.TempDir()
	state := s7RecoveryFixtureState("running", "running")
	state["baseline"] = map[string]any{"generation": 1}
	facts := mapFieldCLI(state, "review")
	rows := mapFieldCLI(facts, "assignments")
	rows["assignment-lock"] = map[string]any{"status": "planned", "queued_agent_id": "reviewer-lock", "resource_locks": []any{"original-db"}}
	rows["assignment-qa-2"].(map[string]any)["resource_locks"] = []any{"original-db"}
	plan := review.Plan{ReviewPlanID: "plan-r2", Assignments: []review.PlanAssignment{
		{AssignmentID: "assignment-qa-2", ClaimIDs: []string{"CLAIM-QA-2"}, ExecutionWave: "static"},
		{AssignmentID: "assignment-e2e-1", ClaimIDs: []string{"CLAIM-E2E-1"}, ExecutionWave: "behavior"},
		{AssignmentID: "assignment-lock", ExecutionWave: "static"},
	}, Claims: []review.Claim{{ClaimID: "CLAIM-QA-2", Applicability: "required"}, {ClaimID: "CLAIM-E2E-1", Applicability: "required"}}}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ptr := mapFieldCLI(facts, "plan")
	ptr["path"], ptr["sha256"] = "plan.json", workpacket.Digest(data)
	text := nativeS7RemainingOutletContext(root, state)
	for _, want := range []string{"required static Claims are not all dispositioned", "manifest-draft", "reviewer-lock", "waits for resource locks original-db"} {
		if !strings.Contains(text, want) {
			t.Fatalf("original prerequisite missing %q: %s", want, text)
		}
	}
}

func TestS7AcceptedRemainingProjectionManifestQueueAndBound(t *testing.T) {
	state := s7RecoveryFixtureState("running", "running")
	state["baseline"] = map[string]any{"generation": 1}
	rows := mapFieldCLI(mapFieldCLI(state, "review"), "assignments")
	rows["assignment-e2e-1"].(map[string]any)["queued_agent_id"] = "queued-reviewer"
	rows["assignment-e2e-1"].(map[string]any)["queue_reason"] = "original resource contention"
	rows["assignment-missing"] = map[string]any{"status": "planned"}
	text := nativeS7RemainingOutletContext(t.TempDir(), state)
	for _, want := range []string{"assignment-missing", "manifest-draft", "--assignment", "queued-reviewer", "original resource contention", "restores the original ReviewPlan source", "s7 status", "personal receipt remains done"} {
		if !strings.Contains(text, want) {
			t.Fatalf("remaining projection missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "assignment-dv-1") || strings.Contains(text, "assignment-qa-1") {
		t.Fatalf("completed responsibilities reactivated: %s", text)
	}
	for i := 0; i < 50; i++ {
		rows[fmt.Sprintf("assignment-%02d", i)] = map[string]any{"status": "planned", "queued_agent_id": "reviewer", "queue_reason": strings.Repeat("q", 17000)}
	}
	text = nativeS7RemainingOutletContext(t.TempDir(), state)
	if len(text) > s7RemainingOutletLimit || !strings.Contains(text, "omitted by output bound") || !strings.Contains(text, "s7 status") {
		t.Fatalf("unbounded or missing overflow entry: %d bytes", len(text))
	}
}

func TestS7RemainingProjectionUsesCoordinatesWhenAnotherBodyIsInvalid(t *testing.T) {
	root := t.TempDir()
	state := s7RecoveryFixtureState("running", "running")
	state["baseline"] = map[string]any{"generation": 1}
	state["configuration"] = map[string]any{"optimization": map[string]any{"packet": true}}
	ptr := mapFieldCLI(mapFieldCLI(state, "review"), "plan")
	var packets []any
	for _, suffix := range []string{"2", "3"} {
		packets = append(packets, map[string]any{
			"packet_id": "packet-qa-" + suffix, "template_id": "s7-review-result-v1",
			"binding":   workpacket.ReviewResultBinding{RuntimeID: "loop-REQ-TEST", BaselineGeneration: 1, ReviewPlanID: "plan-r2", ReviewPlanRevision: 1, ReviewPlanRef: ptr["path"].(string), ReviewPlanSHA256: ptr["sha256"].(string), ReviewRound: 2, AssignmentID: "assignment-qa-" + suffix, OwnerAgentID: "agent-qa-" + suffix, ClaimIDs: []string{"CLAIM-QA-" + suffix}},
			"view_path": "original/qa-" + suffix + "/view.md", "answer_path": "original/qa-" + suffix + "/answer.json",
			// These invalid bodies must remain invalid to the body reader;
			// Main's queue handoff needs only their registered coordinates.
			"inputs": "unreadable original body",
		})
	}
	mapFieldCLI(state, "entities")["work_packets"] = packets
	if _, err := runtime.CurrentReviewResultPackets(state, ""); err == nil {
		t.Fatal("fixture unexpectedly has a valid body")
	}
	text := nativeS7RemainingOutletContext(root, state)
	for _, want := range []string{"original/qa-2/view.md", "original/qa-3/answer.json", "registration=dispatched"} {
		if !strings.Contains(text, want) {
			t.Fatalf("unrelated body erased known handoff %q: %s", want, text)
		}
	}
	if strings.Contains(text, "restores original current issued packet metadata") {
		t.Fatalf("body was read for coordinate projection: %s", text)
	}
	mapFieldCLI(state, "entities")["work_packets"] = append(packets, packets[0])
	if text := nativeS7RemainingOutletContext(root, state); !strings.Contains(text, "duplicate Runtime work packet ID") {
		t.Fatalf("index projection hid shared identity corruption: %s", text)
	}
}
