package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// registerReviewPlanNaturally registers the current ReviewPlan through the
// current natural-reception chain: Main saves the original plan questionnaire
// at its current path, and an ordinary PreToolUse observation lets the
// original consumer receive it once.
//
// The manual `runtime review-plan` submission was removed by the current
// workflow contract (blueprint/l4-mechanisms/L4-workflow-convergence.md:144
// and :312; blueprint/l3-stages/L3-S7-verification-round.md:195), so a test
// that needs a registered Plan now drives the surviving original entry.
func registerReviewPlanNaturally(t *testing.T, root string, state map[string]any, planID string, planPath string) {
	t.Helper()
	path := review.PlanQuestionnairePath(root, state)
	if path == "" {
		t.Fatalf("current ReviewPlan questionnaire path is unavailable for %s", planID)
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	// The round number lives in the current Runtime identity, not in the
	// author's original answer.
	delete(answer, "review_round")
	body, err := json.MarshalIndent(answer, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	out := s7MainNaturalPre(t, root)
	if !strings.Contains(out, "review-plan: registered "+planID) {
		t.Fatalf("original consumer did not register the current Plan: %s", out)
	}
}

func packetBodyRecoveryFixture(t *testing.T) (string, []workpacket.ReviewResultPacket) {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.3.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-body-recovery", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
	plan := minimalReviewPlan(t, root, "review-plan-body-recovery")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	registerReviewPlanNaturally(t, root, state, "review-plan-body-recovery", plan)
	for _, id := range []string{"assignment-dv-1", "assignment-qa-1"} {
		markAssignmentDispatched(t, root, id, "reviewer-"+id)
	}
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		agent["dispatch_mode"] = "plan_checkpoint"
		if err := workstate.RecordAgentActivity(current.State, agent, "reading", "workgroup_registered"); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range current.State["review"].(map[string]any)["assignments"].(map[string]any) {
		if err := workstate.RecordReviewAssignmentActivity(current.State, raw.(map[string]any), "dispatched", "workgroup_registered"); err != nil {
			t.Fatal(err)
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	var packets []workpacket.ReviewResultPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")), &packets); err != nil || len(packets) != 2 {
		t.Fatalf("original packet issuance: count=%d err=%v", len(packets), err)
	}
	return root, packets
}

func TestPacketBodyCheckpointRecoveryRestoresMissingLiveBodiesThroughCLI(t *testing.T) {
	root, packets := packetBodyRecoveryFixture(t)
	path, pair := latestStageCheckpoint(t, root)
	if len(pair.PacketBodies) != len(packets) {
		t.Fatalf("checkpoint lacks original questions: %d", len(pair.PacketBodies))
	}
	for ref := range pair.PacketBodies {
		if err := os.Remove(filepath.Join(root, ref)); err != nil {
			t.Fatal(err)
		}
	}
	// A checkpoint carries its own original body bytes; a missing live view of
	// those bytes cannot prevent planning its approved original-pair restore.
	if _, err := runtime.ReadRuntimeCheckpoint(root, path, semantic.RuntimeCandidateValidator{}); err != nil {
		t.Fatalf("checkpoint depends on missing live body: %v", err)
	}
	damageStagePair(t, root)
	plan := checkpointPlan(t, root, path, pair.State)
	stageCLI(t, "runtime", "recover", "apply", "--root", root, "--plan", plan, "--approved-by", "operator")
	restored := stageSnapshot(t, root)
	if diff := originalProfessionalFactsDiff(pair.State, restored.State); diff != "" {
		t.Fatalf("restoring bodies changed original Runtime facts: %s", diff)
	}
	ctx := runtime.WithPacketBodyRoot(context.Background(), root)
	for _, original := range packets {
		got, found, err := runtime.FindReviewResultPacketContext(ctx, restored.State, original.ID)
		if err != nil || !found || !reflect.DeepEqual(got, original) {
			t.Fatalf("original questionnaire not restored: %s found=%v err=%v", original.ID, found, err)
		}
	}
}

// originalProfessionalFactsDiff reports the first original professional fact
// that the restore changed.
//
// The current contract continues the restored Runtime with its own real
// revision and journal sequence (blueprint/l4-mechanisms/L4-workflow-convergence.md:416
// "checkpoint 使用原 Runtime 身份、真实 revision 和 journal sequence 接续已登记的同次事实")
// and re-issues the stored body at a location it actually owns when the
// previous one is occupied
// (blueprint/l4-mechanisms/L4-workflow-convergence.md:412
// "登记前崩溃或路径被未知内容占用时保留现有文件、给当前实际责任准备独立可用位置").
// Those machine coordinates are therefore not part of the original
// professional facts this test protects: the authored questionnaires, the
// claims, the assignments and every other recorded fact must survive the
// restore unchanged. The retained body bytes are checked separately through
// the packet context resolver.
func originalProfessionalFactsDiff(original, restored map[string]any) string {
	strip := func(state map[string]any) map[string]any {
		raw, err := json.Marshal(state)
		if err != nil {
			return state
		}
		copied := map[string]any{}
		if err := json.Unmarshal(raw, &copied); err != nil {
			return state
		}
		// The recovery is itself a recorded Runtime event.
		delete(copied, "revision")
		delete(copied, "journal")
		if entities, ok := copied["entities"].(map[string]any); ok {
			rows, _ := entities["work_packets"].([]any)
			for _, raw := range rows {
				row, _ := raw.(map[string]any)
				if ref, ok := row["body_ref"].(map[string]any); ok {
					// The path names the location the restore owns; the
					// bytes behind it are compared through the resolver.
					delete(ref, "path")
				}
			}
		}
		return copied
	}
	left, right := strip(original), strip(restored)
	if reflect.DeepEqual(left, right) {
		return ""
	}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return "original=" + string(leftJSON) + " restored=" + string(rightJSON)
}

func TestPacketBodyMaintenanceCLICompactsLegacyAndKeepsPrivateAnswers(t *testing.T) {
	root, packets := packetBodyRecoveryFixture(t)
	before := stageSnapshot(t, root)
	var rows []any
	for _, p := range packets {
		data, _ := json.Marshal(p)
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	before.State["entities"].(map[string]any)["work_packets"] = rows
	stageJSON(t, root, ".claude/loop-state.json", before.State)
	private := []byte("private unfinished professional answer\n")
	if err := os.WriteFile(filepath.Join(root, packets[0].AnswerPath), private, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, packets[0].ViewPath)); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "compact-packets", "--root", root)
	after := stageSnapshot(t, root)
	for _, raw := range after.State["entities"].(map[string]any)["work_packets"].([]any) {
		if _, external, err := packetbody.ReferenceFromIndex(raw.(map[string]any)); err != nil || !external {
			t.Fatalf("legacy packet not compacted: %v %v", external, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(root, packets[0].AnswerPath))
	if err != nil || !reflect.DeepEqual(got, private) {
		t.Fatalf("storage maintenance rewrote private answer: %v", err)
	}
	stageCLI(t, "runtime", "compact-packets", "--root", root)
	if replay := stageSnapshot(t, root); replay.Revision != after.Revision {
		t.Fatal("storage-maintenance replay created another business event")
	}
}
