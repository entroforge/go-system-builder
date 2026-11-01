package packetbody

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestOriginalPacketBodyPreservesBusinessFieldsAndResponsibility(t *testing.T) {
	root := t.TempDir()
	ctx := WithRoot(context.Background(), root)
	body := map[string]any{
		"packet_id": "packet-1", "template_id": "s9-repair-result-v1", "template_sha256": "old-machine-field", "annotation": "unrelated",
		"binding": map[string]any{"sources": map[string]any{"runtime_id": "loop-1", "baseline_generation": 1, "owner_agent_id": "worker", "manifest_assignment_id": "assignment-1", "repair_assignment_id": "repair-1", "contract_sha256": "old-machine-field", "annotation": "unrelated"}},
		"inputs":  map[string]any{"business": map[string]any{"sha256": "business-value"}, "execution": map[string]any{"assignment_id": "assignment-1", "execution_generation": 1, "worktree_path": "/tmp/worker"}},
		"draft":   map[string]any{"answer": "original"}, "answer_path": ".claude/submissions/example/answer.json", "view_path": ".claude/work-packets/example.json",
	}
	index, err := Publish(ctx, root, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := index["template_sha256"]; present {
		t.Fatal("old template machine field was registered in the new index")
	}
	if _, present := index["annotation"]; present {
		t.Fatal("unrelated body annotation became a Runtime index field")
	}
	if _, present := index["binding"].(map[string]any)["sources"].(map[string]any)["contract_sha256"]; present {
		t.Fatal("old source machine field was registered in the new index")
	}
	index["note"] = "later annotation"
	index["body_ref"].(map[string]any)["note"] = "unrelated reference detail"
	before, _ := json.Marshal(index)
	got, err := Read(ctx, index)
	if err != nil || got["packet_id"] != "packet-1" || got["inputs"].(map[string]any)["business"].(map[string]any)["sha256"] != "business-value" {
		t.Fatalf("original business content unavailable: %+v %v", got, err)
	}
	after, _ := json.Marshal(index)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read changed original index")
	}
	index["binding"].(map[string]any)["sources"].(map[string]any)["owner_agent_id"] = "other"
	if _, err := Read(ctx, index); err == nil {
		t.Fatal("different actual owner accepted")
	}
	sources := index["binding"].(map[string]any)["sources"].(map[string]any)
	sources["owner_agent_id"] = "worker"
	sources["manifest_assignment_id"] = "assignment-other"
	if _, err := Read(ctx, index); err == nil {
		t.Fatal("different actual Assignment accepted")
	}
	sources["manifest_assignment_id"] = "assignment-1"
	index["answer_path"] = ".claude/submissions/other/answer.json"
	if _, err := Read(ctx, index); err == nil {
		t.Fatal("different original answer path accepted")
	}
}
