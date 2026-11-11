package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
)

func TestNativeS6CompactResultReceivesThroughOriginalConsumerAndReplays(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	path := filepath.Join(source, result)
	full, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	compact := func() map[string]any {
		var answer map[string]any
		if err := json.Unmarshal(full, &answer); err != nil {
			t.Fatal(err)
		}
		row := answer["assertion_evidence"].([]any)[0].(map[string]any)
		delete(row, "assertion")
		delete(row, "oracle")
		row["plan_assertion_index"] = float64(0)
		condition := answer["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any)
		delete(condition, "plan_assertion_indexes")
		return answer
	}
	text := filepath.Join(root, brief) + "\n" + path
	before := stageSnapshot(t, root).Revision
	for _, tc := range []struct {
		name, pointer string
		mutate        func(map[string]any)
	}{
		{"fractional_index", "/assertion_evidence/0/plan_assertion_index", func(a map[string]any) {
			a["assertion_evidence"].([]any)[0].(map[string]any)["plan_assertion_index"] = 0.5
		}},
		{"other_assertion", "/assertion_evidence/0/assertion", func(a map[string]any) {
			a["assertion_evidence"].([]any)[0].(map[string]any)["assertion"] = "A different responsibility's assertion"
		}},
		{"missing_evidence", "assertion_evidence/0", func(a map[string]any) { delete(a["assertion_evidence"].([]any)[0].(map[string]any), "evidence_refs") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer := compact()
			tc.mutate(answer)
			stageJSON(t, source, result, answer)
			if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 2 || !strings.Contains(output, "professional Result fields") || !strings.Contains(output, tc.pointer) || !strings.Contains(output, path) {
				t.Fatalf("compact reference error lacks original field feedback: %d %s", code, output)
			}
			if stageSnapshot(t, root).Revision != before {
				t.Fatal("malformed compact Result changed Runtime")
			}
		})
	}
	stageJSON(t, source, result, compact())
	authored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 0 {
		t.Fatalf("compact Result was not received: %d %s", code, output)
	}
	accepted := stageSnapshot(t, root)
	if accepted.Revision <= before {
		t.Fatal("compact Result produced no original reception")
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(authored) {
		t.Fatal("receiver rewrote the author's private Result")
	}
	receipt := stageCLI(t, "runtime", "task-receipt", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID)
	if !strings.Contains(receipt, `"accepted":true`) || !strings.Contains(receipt, `"integrated":false`) {
		t.Fatalf("original receipt confused reception and integration: %s", receipt)
	}
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, true); code != 0 {
		t.Fatalf("compact Result replay failed: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != accepted.Revision {
		t.Fatal("identical compact Result replay created another reception")
	}
}

// TestNativeS6CompactResultPassesTheOriginalIntegrationGate covers the step
// the compact authoring path previously never reached. The receiver stores the
// expanded Answer while the Worker keeps the compact professional file. A
// compact Result that is accepted must still integrate and reach S7, even if
// its private worktree must be preserved separately.
func TestNativeS6CompactResultPassesTheOriginalIntegrationGate(t *testing.T) {
	root, source, r := nativeDeliveryFixtureWithMainBaseline(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, plan, result := assignmentrecord.NativePaths(r)
	path := filepath.Join(source, result)
	full, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(full, &answer); err != nil {
		t.Fatal(err)
	}
	row := answer["assertion_evidence"].([]any)[0].(map[string]any)
	delete(row, "assertion")
	delete(row, "oracle")
	row["plan_assertion_index"] = float64(0)
	condition := answer["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any)
	delete(condition, "plan_assertion_indexes")
	stageJSON(t, source, result, answer)

	text := filepath.Join(root, brief) + "\n" + path
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 0 {
		t.Fatalf("compact Result was not received: %d %s", code, output)
	}
	var integrateOut, integrateErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID}, nil, &integrateOut, &integrateErr); code != 0 {
		t.Fatalf("Main integration of a compact Result: %d %s %s", code, &integrateOut, &integrateErr)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, r.Reference()))
	if err != nil || !found || cp.State != integration.StateCleanupPending || cp.CanonicalMainFacts == nil || cp.VerifiedAt == "" || len(cp.CheckReceipts) == 0 {
		t.Fatalf("compact Result did not record verified integration and pending cleanup: %+v %v / %s %s", cp, err, &integrateOut, &integrateErr)
	}
	for _, name := range []string{plan, result} {
		if _, err := os.Stat(filepath.Join(source, name)); err != nil {
			t.Fatalf("private original %s was not preserved: %v", name, err)
		}
	}
	for _, fact := range []string{source, ".claude/", "cleanup authorization", "cleanup_pending", "completion_ack recorded", "TR-006", "ReviewPlan questionnaire:"} {
		if !strings.Contains(integrateOut.String(), fact) {
			t.Fatalf("compact handoff omitted %q: %s %s", fact, &integrateOut, &integrateErr)
		}
	}
	// The current generation owns the original TR-003 common Main baseline, so
	// the integration exit reads the current batch and reports the original
	// hand-off instead of a missing baseline or an uninferrable start.
	for _, gap := range []string{"no original TR-003 common Main baseline", "historical start cannot be inferred"} {
		if strings.Contains(integrateOut.String(), gap) {
			t.Fatalf("compact integration reported a missing TR-003 baseline: %s %s", &integrateOut, &integrateErr)
		}
	}
	if lifecycle := stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"]; lifecycle != "verification" {
		t.Fatalf("compact delivery did not reach S7: %v", lifecycle)
	}
	if product, err := os.ReadFile(filepath.Join(root, "src/pkg/proof.txt")); err != nil || string(product) != "Actual implementation evidence.\n" {
		t.Fatalf("Main did not receive actual product: %q %v", product, err)
	}
	receipt := stageCLI(t, "runtime", "task-receipt", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID)
	if !strings.Contains(receipt, `"accepted":true`) || !strings.Contains(receipt, `"work_complete":true`) || !strings.Contains(receipt, `"integrated":true`) {
		t.Fatalf("compact Result did not reach an integrated receipt: %s", receipt)
	}
}
