package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// The dispatcher is a fixture; real public Hook and original Result consumer handle reception.
func TestS7PublicHookReceivesOriginalWithoutLegacyActivationRoute(t *testing.T) {
	f := newRuntimeFixture(t)
	f.state["schema_version"] = "1.3.0"
	// A real S7 Runtime is bound to its authority workspace before native Hook
	// reception; this synthetic fixture uses the repository it creates below.
	f.state["bound_req"].(map[string]any)["workspace"] = map[string]any{
		"project_root": f.root, "dev_branch": "test-development", "release_upstream": "origin/release",
	}
	f.state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	// The shared minimal fixture omits the source rules that a real installed
	// loop definition supplies to the original submission service.
	definitionPath := filepath.Join(f.root, "docs/control/loop-definition.json")
	definitionBytes, err := os.ReadFile(definitionPath)
	if err != nil {
		t.Fatal(err)
	}
	var definition map[string]any
	if err := json.Unmarshal(definitionBytes, &definition); err != nil {
		t.Fatal(err)
	}
	definition["file_sources"] = []any{
		map[string]any{"path": ".", "source": "git_tree"},
		map[string]any{"path": ".claude/evidence", "source": "disk"},
		map[string]any{"path": ".claude/review", "source": "disk"},
		map[string]any{"path": ".claude/workgroups", "source": "disk"},
	}
	definitionBytes, err = json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definitionPath, definitionBytes, 0600); err != nil {
		t.Fatal(err)
	}
	f.state["lifecycle"] = map[string]any{"state": "verification", "phase": nil, "phase_revision": 4}
	data, err := schema.ReadAsset("review-plan.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan review.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	for i := range plan.FrozenSubjects {
		subject := &plan.FrozenSubjects[i]
		path := filepath.Join(f.root, filepath.FromSlash(subject.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		body := []byte("actual frozen fixture: " + subject.Path)
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	queued := plan.Claims[1]
	queued.ClaimID, queued.Target, queued.FocusKey = "claim-queued", "internal/other", "test-oracle"
	queued.DependsOn = []string{plan.Claims[1].ClaimID}
	plan.Claims = append(plan.Claims, queued)
	plan.Assignments = append(plan.Assignments, review.PlanAssignment{AssignmentID: "assignment-queued", Lens: "qa", ClaimIDs: []string{queued.ClaimID}, FocusKeys: []string{"test-oracle"}, NonOverlapBoundary: "only dependent test oracle", ExecutionWave: "static"})
	if err := review.ValidatePlan(&plan); err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(plan)
	planRel := ".claude/review/plan.json"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(f.root, planRel)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, planRel), data, 0600); err != nil {
		t.Fatal(err)
	}
	f.addAgent("reviewer-delivery", "delivery-verifier", "reading", "team-review", nil)
	f.addAgent("reviewer-quality", "qa", "reading", "team-review", nil)
	for _, raw := range f.state["entities"].(map[string]any)["agents"].([]any) {
		if err := workstate.RecordAgentActivity(f.state, raw.(map[string]any), "reading", "workgroup_registered"); err != nil {
			t.Fatal(err)
		}
	}
	assignments := map[string]any{}
	for i, a := range plan.Assignments {
		owner, status := "", "queued"
		if i < 2 {
			owner, status = []string{"reviewer-delivery", "reviewer-quality"}[i], "dispatched"
		}
		row := map[string]any{"lens": a.Lens, "agent_id": owner, "claim_ids": a.ClaimIDs, "result_ref": nil}
		event := "workgroup_registered"
		if status == "queued" {
			status, event = "planned", "review_plan_registered"
		}
		if err := workstate.RecordReviewAssignmentActivity(f.state, row, status, event); err != nil {
			t.Fatal(err)
		}
		assignments[a.AssignmentID] = row
		if owner != "" {
			// Synthetic dispatch still records its original selection source.
			// This fixture explicitly selects no additional methods; production
			// issuance must never infer an empty selection from a missing source.
			entities := f.state["entities"].(map[string]any)
			teamID := "team-" + a.AssignmentID
			manifestRef := ".claude/workgroups/" + a.AssignmentID + ".json"
			role := map[string]string{"delivery": "delivery-verifier", "qa": "qa"}[a.Lens]
			manifest := review.ManifestDraft{
				RuntimeID: f.state["runtime_id"].(string), BaselineGeneration: plan.BaselineGeneration,
				WorkgroupID: teamID, WorkgroupKind: review.LensToWorkgroupKind(a.Lens),
				Assignments: []review.ManifestAssignment{{AssignmentID: a.AssignmentID, AgentID: owner, RoleFamily: role, ClaimIDs: a.ClaimIDs, SkillRefs: []string{}}},
			}
			manifestBytes, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(f.root, manifestRef)
			if err := os.MkdirAll(filepath.Dir(manifestPath), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, manifestBytes, 0600); err != nil {
				t.Fatal(err)
			}
			teams, _ := entities["teams"].([]any)
			entities["teams"] = append(teams, map[string]any{
				"id": teamID, "platform_team_id": teamID, "kind": manifest.WorkgroupKind, "status": "planned",
				"manifest_ref":       manifestRef,
				"responsibility_ids": []string{}, "agent_ids": []string{owner},
			})
			for _, raw := range entities["agents"].([]any) {
				agent := raw.(map[string]any)
				if agent["id"] == owner {
					agent["team_id"], agent["prompt_ref"] = teamID, manifestRef+"#"+a.AssignmentID
				}
			}
		}
	}
	// Seed the same Claim and responsibility projection as the original Plan
	// Writer before adding the fixture's already-dispatched owners.
	f.state["review"] = map[string]any{}
	if err := review.ApplyRegisteredPlanProjection(f.state, plan, planRel, "", time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	f.state["review"].(map[string]any)["assignments"] = assignments
	// The shared minimal catalog has no verification phase machine.
	f.state["lifecycle"].(map[string]any)["phase"] = nil
	f.persist(t)
	// Original issuance freezes a real Git source, even when the professional
	// Plan/dispatch facts were registered synthetically by this fixture.
	repairLeadCommitFixture(t, f.root)
	service := &submission.Service{Root: f.root, StatePath: f.statePath, JournalPath: f.journalPath, Validator: semantic.RuntimeCandidateValidator{}}
	snapshot, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	stageReviewResultLifecycleContext(context.Background(), f.root, policy.Input{Event: "Stop", CWD: f.root}, service, snapshot, "Main")
	afterStop, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	if err != nil || afterStop.Revision != snapshot.Revision {
		t.Fatalf("Stop prepared unissued responsibilities: %d/%d %v", afterStop.Revision, snapshot.Revision, err)
	}
	output := stageReviewResultLifecycleContext(context.Background(), f.root, policy.Input{Event: "PreToolUse", CWD: f.root}, service, snapshot, "Main")
	after, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	packets, err := runtime.CurrentReviewResultPacketsContext(runtime.WithPacketBodyRoot(context.Background(), f.root), after.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("single original handoff issued %d/2 responsibilities: %v\n%s", len(packets), err, output)
	}
	packet := packets[0]
	for _, raw := range after.State["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == packet.Binding.OwnerAgentID {
			agent["activation_ref"] = "legacy-context-only"
		}
	}
	stateBytes, err := json.Marshal(after.State)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.statePath, stateBytes, 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(actor, text string) (int, string) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": actor, "session_id": "native-test", "cwd": f.root, "last_assistant_message": text})
		var out, diagnostic bytes.Buffer
		code := Run([]string{"hook", "--root", f.root, "--event", "SubagentStop"}, bytes.NewReader(payload), &out, &diagnostic)
		return code, out.String() + diagnostic.String()
	}
	before, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	code, notice := invoke("unrelated-actor", "unrelated task")
	if code != 0 {
		t.Fatalf("unbound work was blocked: %d %s", code, notice)
	}
	unbound, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	if err != nil || unbound.Revision != before.Revision {
		t.Fatalf("unbound task changed Runtime: %v", err)
	}
	var claims []any
	for _, id := range packet.Binding.ClaimIDs {
		claims = append(claims, map[string]any{"claim_id": id, "conclusion": "pass", "method": "inspect assigned material", "observed": "assigned material satisfies the observation"})
	}
	packet.Draft.Answer, _ = json.Marshal(map[string]any{"claim_results": claims, "verdict": "pass"})
	answer, _ := json.Marshal(packet.Draft)
	if err := os.WriteFile(filepath.Join(f.root, packet.AnswerPath), answer, 0600); err != nil {
		t.Fatal(err)
	}
	code, notice = invoke(packet.Binding.OwnerAgentID, filepath.Join(f.root, packet.AnswerPath))
	if code != 0 {
		t.Fatalf("original answer blocked: %d %s", code, notice)
	}
	received, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !review.IssuedResultWorkComplete(received.State, packet) {
		t.Fatalf("original answer was not received: %s", notice)
	}
	code, notice = invoke(packet.Binding.OwnerAgentID, filepath.Join(f.root, packet.AnswerPath))
	if code != 0 {
		t.Fatalf("original receipt replay blocked: %d %s", code, notice)
	}
	replay, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	if err != nil || replay.Revision != received.Revision {
		t.Fatalf("accepted answer replay changed Runtime: %v", err)
	}
}

// The Builder task and flat answers are the original S9 fixture. Public
// SubagentStop observes them; TeammateIdle must not turn an unfinished or
// blocked result into a completed one, nor undo an accepted result.
func TestPublicS9StopIdlePreservesOriginalResultState(t *testing.T) {
	for _, scenario := range []string{"unreceived", "blocked", "accepted"} {
		t.Run(scenario, func(t *testing.T) {
			root, worker, plan, input, _ := nativeS9WorkerFixture(t)
			_, resultPath := nativeS9TestAuthorPaths(t, root, plan)
			resultAbs := filepath.Join(worker, resultPath)
			switch scenario {
			case "unreceived":
				if err := os.Remove(resultAbs); err != nil {
					t.Fatal(err)
				}
			case "blocked":
				body, _ := json.Marshal(map[string]any{"verdict": "blocked", "residual_risks": []string{"the required environment is unavailable"}})
				if err := os.WriteFile(resultAbs, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			input.Event = "SubagentStop"
			input.CWD = worker
			input.AgentID = plan.Binding.OwnerAgentID
			nativeS9SplitChild(t, &input)
			_, notice := s9ReferencePublicHook(t, root, input)
			state := repairDispatchSnapshot(t, root)
			packets, err := runtime.RepairResultPacketsForOwnerContext(runtime.WithPacketBodyRoot(t.Context(), root), state.State, plan.Binding.OwnerAgentID)
			if err != nil {
				t.Fatal(err)
			}
			accepted, complete := false, false
			if len(packets) > 0 {
				service, err := submission.NewReadOnly(root)
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := service.RepairResultReceipt(plan.Binding.OwnerAgentID, packets[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				accepted, complete = receipt.Accepted, receipt.WorkComplete
			}
			if scenario == "unreceived" && (accepted || complete) {
				t.Fatalf("unreceived work became a result: %s", notice)
			}
			if scenario == "blocked" && (!accepted || complete) {
				t.Fatalf("real blocked result lost its unfinished state: %s", notice)
			}
			if scenario == "accepted" && !accepted {
				t.Fatalf("original completed answer was not received: %s", notice)
			}
			before := repairDispatchRuntimeBytes(t, root)
			input.Event = "TeammateIdle"
			s9ReferencePublicHook(t, root, input)
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}
