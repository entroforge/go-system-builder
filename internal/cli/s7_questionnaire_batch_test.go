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
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// Registered Plan/dispatch rows are fixture preconditions. The test executes
// the real Main context entry, original issuer, Writer and published files;
// it does not claim to test planning, native Agent creation or product work.
func TestS7MainPreparesAllDispatchedQuestionnairesInOneOriginalHandoff(t *testing.T) {
	f := newRuntimeFixture(t)
	f.state["schema_version"] = "1.3.0"
	f.state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
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
		subject.SHA256 = workpacket.Digest(body)
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
				RuntimeID: f.state["runtime_id"].(string), BaselineGeneration: plan.BaselineGeneration, ReviewRound: plan.ReviewRound,
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
				"manifest_ref": manifestRef, "manifest_sha256": workpacket.Digest(manifestBytes),
				"responsibility_ids": []string{}, "agent_ids": []string{owner}, "review_round": plan.ReviewRound,
			})
			for _, raw := range entities["agents"].([]any) {
				agent := raw.(map[string]any)
				if agent["id"] == owner {
					agent["team_id"], agent["prompt_ref"] = teamID, manifestRef+"#"+a.AssignmentID
				}
			}
		}
	}
	f.state["review"] = map[string]any{"round": 1, "clean_round": nil, "claims": map[string]any{}, "assignments": assignments, "plan": map[string]any{"plan_id": plan.ReviewPlanID, "path": planRel, "sha256": workpacket.Digest(data), "revision": 1, "review_round": 1, "status": "running", "e2e_coverage_state": "not_applicable", "submitted_at": "2026-10-23T00:00:00Z"}}
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
	if !strings.Contains(output, "Optional capture") || !strings.Contains(output, "source/environment blockers to Main") {
		t.Fatalf("new issuance did not carry the complete native task: %s", output)
	}
	for _, packet := range packets {
		if packet.Binding.AssignmentID == "assignment-queued" {
			t.Fatal("queued dependency was issued early")
		}
		for _, rel := range []string{packet.ViewPath, packet.AnswerPath} {
			path := filepath.Join(f.root, filepath.FromSlash(rel))
			if _, err := os.Stat(path); err != nil || !strings.Contains(output, path) {
				t.Fatalf("ready responsibility not delivered in same context: %s %v\n%s", path, err, output)
			}
		}
	}
	path := filepath.Join(f.root, filepath.FromSlash(packets[0].AnswerPath))
	author := []byte("preserve a real in-progress author file")
	if err := os.WriteFile(path, author, 0600); err != nil {
		t.Fatal(err)
	}
	repeated := stageReviewResultLifecycleContext(context.Background(), f.root, policy.Input{Event: "PreToolUse", CWD: f.root}, service, after, "Main")
	if strings.Contains(repeated, "Optional capture") || !strings.Contains(repeated, path) {
		t.Fatalf("ordinary reminder repeated long instructions or lost the actual answer: %s", repeated)
	}
	restored := stageReviewResultLifecycleContext(context.Background(), f.root, policy.Input{Event: "SessionStart", CWD: f.root}, service, after, "Main")
	if !strings.Contains(restored, "Optional capture") || !strings.Contains(restored, path) {
		t.Fatalf("new session lost the complete task: %s", restored)
	}
	latest, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot()
	got, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || latest.Revision != after.Revision || string(got) != string(author) {
		t.Fatalf("repeat observation reissued/overwrote original: %d/%d err=%v/%v", latest.Revision, after.Revision, err, readErr)
	}
}
