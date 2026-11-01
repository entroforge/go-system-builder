package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9OriginalReceptionAndReplayWithoutDigest(t *testing.T) {
	root, worker, plan, in, _ := nativeS9WorkerFixture(t)
	in.Event, in.CWD = "PreToolUse", worker
	var diagnostic bytes.Buffer
	snap := repairDispatchSnapshot(t, root)
	if err := receiveNativeS9BuilderEvent(root, snap.State, in, plan, in.LastAssistantMessage, &diagnostic, nil); err != nil {
		t.Fatalf("original Plan/Result reception: %v %s", err, &diagnostic)
	}
	snap = repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil {
		t.Fatal(err)
	}
	execution, ok := registry.Executions[plan.Binding.ManifestAssignmentID]
	if !ok || execution.DeliveryRef == "" {
		t.Fatalf("original result was not received without digest: %+v", execution)
	}
	raw, err := os.ReadFile(filepath.Join(root, execution.DeliveryRef))
	if err != nil {
		t.Fatal(err)
	}
	var candidate map[string]any
	if err := json.Unmarshal(raw, &candidate); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"contract_sha256", "base_commit"} {
		if _, present := candidate[key]; present {
			t.Fatalf("new Candidate registered %s: %s", key, raw)
		}
	}
	provenance, _ := candidate["provenance"].(map[string]any)
	for _, key := range []string{"packet_sha256", "answer_sha256", "execution_sha256", "plan_report_sha256", "completion_report_sha256", "rework_sha256", "resume_sha256"} {
		if _, present := provenance[key]; present {
			t.Fatalf("new Candidate registered %s: %s", key, raw)
		}
	}
	before := repairDispatchRuntimeBytes(t, root)
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		in.Event = event
		if err := receiveNativeS9BuilderEvent(root, snap.State, in, plan, in.LastAssistantMessage, &diagnostic, nil); err != nil {
			t.Fatalf("accepted result replay %s: %v %s", event, err, &diagnostic)
		}
		repairDispatchAssertUnchanged(t, root, before)
	}
}
