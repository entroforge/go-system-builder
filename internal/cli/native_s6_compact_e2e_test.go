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
// expanded Answer while the Worker keeps the compact professional file, so the
// native delivery comparison must canonicalize both sides the same way. A
// compact Result that is accepted must still integrate.
func TestNativeS6CompactResultPassesTheOriginalIntegrationGate(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
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
	if err != nil || !found || cp.State != integration.StateComplete {
		t.Fatalf("compact Result did not complete the original Gate: %+v %v / %s %s", cp, err, &integrateOut, &integrateErr)
	}
	receipt := stageCLI(t, "runtime", "task-receipt", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID)
	if !strings.Contains(receipt, `"integrated":true`) {
		t.Fatalf("compact Result did not reach an integrated receipt: %s", receipt)
	}
}
