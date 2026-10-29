package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func packetBodyRecoveryFixture(t *testing.T) (string, []workpacket.ReviewResultPacket) {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.3.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	plan := minimalReviewPlan(t, root, "review-plan-body-recovery")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	stageCLI(t, "runtime", "review-plan", "--root", root, "--file", plan)
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
	if !reflect.DeepEqual(restored.State, pair.State) {
		t.Fatal("restoring bodies changed original Runtime facts")
	}
	ctx := runtime.WithPacketBodyRoot(context.Background(), root)
	for _, original := range packets {
		got, found, err := runtime.FindReviewResultPacketContext(ctx, restored.State, original.ID)
		if err != nil || !found || !reflect.DeepEqual(got, original) {
			t.Fatalf("original questionnaire not restored: %s found=%v err=%v", original.ID, found, err)
		}
	}
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
