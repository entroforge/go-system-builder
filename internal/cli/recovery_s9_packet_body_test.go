package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9CheckpointRecoveryPreservesOccupiedOriginalBody(t *testing.T) {
	root, _, _, _, _ := nativeS9WorkerFixture(t)
	checkpoint, err := runtime.LatestRuntimeCheckpoint(root)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := runtime.ReadRuntimeCheckpoint(root, checkpoint, semantic.RuntimeCandidateValidator{})
	if err != nil {
		t.Fatal(err)
	}
	var oldRef string
	for ref := range pair.PacketBodies {
		oldRef = ref
		break
	}
	if oldRef == "" {
		t.Fatal("S9 original packet body is missing from checkpoint")
	}
	bound := pair.State["bound_req"].(map[string]any)
	workspace := bound["workspace"].(map[string]any)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if code := Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("%v: code %d: %s", args, code, &stderr)
		}
	}
	for _, name := range []string{"loop-state.json", "loop-events.jsonl"} {
		if err := os.WriteFile(filepath.Join(root, ".claude", name), []byte("damaged original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run("runtime", "recover", "plan", "--root", root, "--req", bound["path"].(string), "--dev-branch", workspace["dev_branch"].(string), "--release-upstream", workspace["release_upstream"].(string), "--checkpoint", checkpoint)
	var plan struct {
		Path string `json:"recovery_plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(root, filepath.FromSlash(oldRef))
	if err := os.WriteFile(occupied, []byte("unknown after plan"), 0600); err != nil {
		t.Fatal(err)
	}
	run("runtime", "recover", "apply", "--root", root, "--plan", plan.Path, "--approved-by", "operator")
	if data, err := os.ReadFile(occupied); err != nil || string(data) != "unknown after plan" {
		t.Fatalf("unknown occupied file changed: %q %v", data, err)
	}
	snapshot, err := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{}).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range snapshot.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		ref, ok, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || ref.Path == oldRef {
			continue
		}
		data, err := workpacket.ReadFile(root, ref.Path, packetbody.Limit)
		if err == nil && bytes.Equal(data, pair.PacketBodies[oldRef]) {
			return
		}
	}
	t.Fatal("original professional body was not restored at the registered replacement path")
}
