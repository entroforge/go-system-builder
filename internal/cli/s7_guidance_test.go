package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS7DispatchGuidanceUsesActualPrerequisitesAndSource(t *testing.T) {
	for _, scenario := range []string{"ready", "static-wait", "dependency-wait", "resource-wait", "source-drift", "sealed", "paused", "s8"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".claude/workgroups"), 0700); err != nil {
				t.Fatal(err)
			}
			plan := review.Plan{ReviewPlanID: "current", ReviewRound: 1, BaselineGeneration: 1,
				Claims:      []review.Claim{{ClaimID: "upstream", Applicability: "required"}, {ClaimID: "next", Applicability: "required"}},
				Assignments: []review.PlanAssignment{{AssignmentID: "upstream", ClaimIDs: []string{"upstream"}, ExecutionWave: "static"}, {AssignmentID: "next", ClaimIDs: []string{"next"}, ExecutionWave: "static"}},
			}
			if scenario == "static-wait" {
				plan.Assignments[1].ExecutionWave = "behavior"
			}
			if scenario == "dependency-wait" {
				plan.Claims[1].DependsOn = []string{"upstream"}
			}
			data, _ := json.Marshal(plan)
			path := filepath.Join(root, "plan.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			pointer := map[string]any{"plan_id": "current", "path": "plan.json", "sha256": workpacket.Digest(data), "review_round": 1, "status": "running"}
			candidate := map[string]any{"status": "planned"}
			holder := map[string]any{"status": "dispatched", "agent_id": "qa", "result_ref": nil}
			state := map[string]any{"schema_version": "1.2.0", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "verification", "phase": "running"}, "review": map[string]any{
				"round": 1, "plan": pointer, "assignments": map[string]any{"upstream": holder, "next": candidate},
				"claims": map[string]any{"upstream": map[string]any{"disposition": "running"}, "next": map[string]any{"disposition": "planned"}},
			}}
			switch scenario {
			case "resource-wait":
				candidate["resource_locks"], holder["resource_locks"] = []any{"db"}, []any{"db"}
			case "source-drift":
				if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "sealed":
				pointer["status"] = "observation_sealed"
			case "paused":
				state["pause"] = map[string]any{"reason": "real external wait"}
			case "s8":
				state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "investigation"}
			}
			out := s7PendingReviewGuidance(root, state)
			if strings.Contains(out, "manifest-draft") != (scenario == "ready") {
				t.Fatalf("incorrect preparation direction: %s", out)
			}
			if scenario == "source-drift" && !strings.Contains(out, "drifted") {
				t.Fatalf("lost actual source failure: %s", out)
			}
			if strings.HasSuffix(scenario, "-wait") && !strings.Contains(out, "waits") {
				t.Fatalf("lost wait reason: %s", out)
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/workgroups/manifest-g1-r1-next.json")); !os.IsNotExist(err) {
				t.Fatal("guidance wrote a manifest")
			}
		})
	}
}

func TestS7ClosedRoundGuidanceDoesNotRedispatchOldQueue(t *testing.T) {
	for _, status := range []string{"observation_sealed", "paused", "clean"} {
		root := t.TempDir()
		state := map[string]any{"review": map[string]any{"plan": map[string]any{"plan_id": "original", "path": "plan.json", "status": status}, "assignments": map[string]any{"unobserved": map[string]any{"status": "planned"}}}}
		out := stageReviewResultLifecycleContext(context.Background(), root, policy.Input{Event: "PreToolUse", CWD: root}, nil, runtime.Snapshot{State: state}, "Main")
		if strings.Contains(out, "manifest-draft") || strings.Contains(out, "Main carries") {
			t.Fatalf("closed round redispatch: %s", out)
		}
		if status == "observation_sealed" && (!strings.Contains(out, "TR-008") || !strings.Contains(out, "S8 intake")) {
			t.Fatalf("missing original handoff: %s", out)
		}
	}
}

func TestS7NativeTaskCarriesActualSourceCaptureAndReturn(t *testing.T) {
	root, source := "/main review", "/reviewer's original"
	p := workpacket.ReviewResultPacket{Binding: workpacket.ReviewResultBinding{RuntimeID: "loop-req", BaselineGeneration: 3, AssignmentID: "qa-current", OwnerAgentID: "reviewer-current"}}
	out := s7ReviewerTaskGuidance(root, source, source+"/view.json", source+"/answer.json", p, true)
	for _, want := range []string{source + "/view.json", source + "/answer.json", "frozen subjects/Claims", "PreToolUse", "Stop/SubagentStop", "reception does not wait for a final message or returned reference", "source/environment blockers to Main", "Optional capture", "--cwd " + s7CaptureShellQuote(source), "--assignment 'qa-current'", "capture step", review.CaptureFile(root, "loop-req", 3, "qa-current"), "path: evidence"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	short := s7ReviewerTaskGuidance(root, source, source+"/view.json", source+"/answer.json", p, false)
	if strings.Contains(short, "capture exec") || len(short) >= len(out) || !strings.Contains(short, source+"/view.json") || !strings.Contains(short, source+"/answer.json") || !strings.Contains(short, "SubagentStop") {
		t.Fatalf("ordinary reminder lost the exact handoff or repeated long capture instructions: %s", short)
	}
}

// Both ordinary observation and session recovery must select the static wave
// even when a behavior Assignment sorts before it in the visible queue.
func TestS7RecoveryAndOrdinaryGuidanceShareWaveEligibility(t *testing.T) {
	root := t.TempDir()
	plan := review.Plan{ReviewPlanID: "wave-plan", ReviewRound: 1, BaselineGeneration: 1,
		Claims:      []review.Claim{{ClaimID: "static", Applicability: "required"}, {ClaimID: "behavior", Applicability: "required"}},
		Assignments: []review.PlanAssignment{{AssignmentID: "z-static", ClaimIDs: []string{"static"}, ExecutionWave: "static"}, {AssignmentID: "a-behavior", ClaimIDs: []string{"behavior"}, ExecutionWave: "behavior"}},
	}
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(filepath.Join(root, "plan.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "loop-wave", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "verification", "phase": "running"}, "review": map[string]any{
		"round": 1, "plan": map[string]any{"plan_id": "wave-plan", "path": "plan.json", "sha256": workpacket.Digest(data), "review_round": 1, "status": "running"},
		"assignments": map[string]any{"z-static": map[string]any{"status": "planned"}, "a-behavior": map[string]any{"status": "planned"}},
		"claims":      map[string]any{"static": map[string]any{"disposition": "planned"}, "behavior": map[string]any{"disposition": "planned"}},
	}}
	ordinary := s7PendingReviewGuidance(root, state)
	for _, event := range []string{"SessionStart", "PreCompact"} {
		guidance := buildGuidance(root, state, event, policy.Input{})
		if strings.TrimSpace(ordinary) != guidance.Action || !strings.Contains(guidance.Action, "assignment=a-behavior waits:") || !strings.Contains(guidance.Action, "--assignment 'z-static'") || strings.Contains(guidance.Action, "--assignment 'a-behavior'") {
			t.Fatalf("%s disagrees with actual registration eligibility: %s", event, guidance.Action)
		}
	}
}
