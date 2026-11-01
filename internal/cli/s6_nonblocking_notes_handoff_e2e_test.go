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

// The original native Worker answer is received once, with its professional
// risk and scope notes intact, then travels through Main's real Git merge,
// check and acknowledgement to the original TR-006/S7 handoff.
func TestNativeS6NonblockingNotesReachS7WithoutReanswer(t *testing.T) {
	root, source, record := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, record)
	brief, _, result := assignmentrecord.NativePaths(record)
	resultPath := filepath.Join(source, result)
	initial, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var authored map[string]any
	if err := json.Unmarshal(initial, &authored); err != nil {
		t.Fatal(err)
	}
	authored["risks_blockers"] = []string{"Non-blocking environment assumption for Main."}
	authored["scope_deviations"] = []string{"Informational scope clarification for Main."}
	stageJSON(t, source, result, authored)
	original, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root)
	text := filepath.Join(root, brief) + "\n" + resultPath
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 0 {
		t.Fatalf("original Worker result was not received: code=%d output=%s", code, output)
	}
	accepted := stageSnapshot(t, root)
	if accepted.Revision <= before.Revision {
		t.Fatal("original Stop did not receive the result")
	}
	var out, stderr bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", record.AssignmentID, "--agent-id", record.OwnerAgentID}, nil, &out, &stderr); code != 0 {
		t.Fatalf("Main integration failed: code=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, record.Reference()))
	if err != nil || !found || cp.CanonicalMainFacts == nil || cp.VerifiedAt == "" || len(cp.CheckReceipts) == 0 {
		t.Fatalf("actual Main merge/check facts missing: checkpoint=%+v found=%v err=%v", cp, found, err)
	}
	for _, fact := range []string{"completion_ack recorded", "TR-006", "ReviewPlan questionnaire:"} {
		if !strings.Contains(out.String(), fact) {
			t.Fatalf("handoff omitted %q: %s %s", fact, &out, &stderr)
		}
	}
	state := stageSnapshot(t, root).State
	if lifecycle := state["lifecycle"].(map[string]any)["state"]; lifecycle != "verification" {
		t.Fatalf("TR-006 did not reach S7: %v", lifecycle)
	}
	if actual, err := os.ReadFile(filepath.Join(root, "src/pkg/proof.txt")); err != nil || string(actual) != "Actual implementation evidence.\n" {
		t.Fatalf("Main did not merge the original product: %q %v", actual, err)
	}
	if after, err := os.ReadFile(resultPath); err != nil || !bytes.Equal(original, after) {
		t.Fatalf("owner's original Result was changed or removed: %v", err)
	}
	receipt := stageCLI(t, "runtime", "task-receipt", "--root", root, "--assignment-id", record.AssignmentID, "--agent-id", record.OwnerAgentID)
	for _, fact := range []string{`"accepted":true`, `"work_complete":true`, `"integrated":true`} {
		if !strings.Contains(receipt, fact) {
			t.Fatalf("receipt omitted %s: %s", fact, receipt)
		}
	}
}
