package req039_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
)

func seedS3AuditFrozenModel(t *testing.T) (string, string, string, []byte) {
	t.Helper()
	root := freshRoot(t)
	worker := seedIntegrableAssignment(t, root)
	model := "internal/orders.schema.json"
	original := []byte(`{"type":"object","properties":{"order_id":{"type":"string"}}}`)
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, model), original, 0644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "add", model)
	runGitIn(t, root, "commit", "-m", "frozen shared model baseline")
	runGitIn(t, worker, "merge", "develop", "--no-edit")

	statePath := filepath.Join(root, ".claude/loop-state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	// The reusable integrator seed starts from a planning fixture; align its
	// display projection with building so the normal root lock is active.
	if milestone, ok := state["milestone"].(map[string]any); ok {
		milestone["stage"] = "S6"
		milestone["lifecycle_state"] = "building"
		milestone["lifecycle_phase"] = nil
	}
	digest := sha256.Sum256(original)
	docs, _ := state["documents"].([]any)
	state["documents"] = append(docs, map[string]any{
		"id": "shared-model:" + model, "kind": "design", "path": model,
		"version": "unversioned", "sha256": hex.EncodeToString(digest[:]),
		"status": "locked", "generation": 1,
	})
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	locked := false
	for _, artifact := range loaded.PolicyContext.LockedArtifacts {
		if artifact.Path == model {
			locked = true
		}
	}
	if !locked {
		t.Fatal("fixture model is not projected into locked artifacts")
	}

	return root, worker, model, original
}

// TestS3AuditFrozenModelCannotMergeFromWorker exercises the real
// task-integrate CLI and Git merge, starting with the locked design row
// produced by S3/S5 registration. Every mutation of the frozen path must be
// rejected before merge, including Git's delete and rename representations.
func TestS3AuditFrozenModelCannotMergeFromWorker(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, worker, model string)
	}{
		{
			name: "modify",
			mutate: func(t *testing.T, worker, model string) {
				t.Helper()
				changed := []byte(`{"type":"object","properties":{"order_id":{"type":"number"}}}`)
				if err := os.WriteFile(filepath.Join(worker, model), changed, 0644); err != nil {
					t.Fatal(err)
				}
				runGitIn(t, worker, "add", model)
				runGitIn(t, worker, "commit", "-m", "incompatible shared model change")
			},
		},
		{
			name: "delete",
			mutate: func(t *testing.T, worker, model string) {
				t.Helper()
				runGitIn(t, worker, "rm", model)
				runGitIn(t, worker, "commit", "-m", "delete frozen shared model")
			},
		},
		{
			name: "rename",
			mutate: func(t *testing.T, worker, model string) {
				t.Helper()
				runGitIn(t, worker, "mv", model, model+".renamed")
				runGitIn(t, worker, "commit", "-m", "rename frozen shared model")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, worker, model, original := seedS3AuditFrozenModel(t)
			rootBefore := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
			targetBefore := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
			tc.mutate(t, worker, model)
			workerAfterMutation := strings.TrimSpace(runGitIn(t, worker, "rev-parse", "HEAD"))

			code, stdout, stderr := runTaskIntegrate(t, root, "assignment-ti")
			out := strings.ToLower(stdout + stderr)
			if !strings.Contains(out, strings.ToLower(integration.ErrLockedArtifact.Error())) {
				t.Fatalf("frozen %s rejection did not surface stable locked-artifact blocker: code=%d stdout=%s stderr=%s", tc.name, code, stdout, stderr)
			}
			rootAfter := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
			targetAfter := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
			got, err := os.ReadFile(filepath.Join(root, model))
			if err != nil {
				t.Fatal(err)
			}
			if rootBefore != rootAfter || targetBefore != targetAfter || string(got) != string(original) {
				t.Fatalf("frozen shared model was merged for %s: code=%d root_advanced=%t target_advanced=%t model=%s stdout=%s stderr=%s", tc.name, code, rootBefore != rootAfter, targetBefore != targetAfter, got, stdout, stderr)
			}
			workerHead := strings.TrimSpace(runGitIn(t, worker, "rev-parse", "HEAD"))
			if workerHead != workerAfterMutation {
				t.Fatalf("rejected %s changed worker HEAD: before=%s after=%s", tc.name, workerAfterMutation, workerHead)
			}
			if _, err := os.Stat(worker); err != nil {
				t.Fatalf("rejected %s worker must remain: %v", tc.name, err)
			}
		})
	}
}

// An implementation-only worker change is outside the frozen shared-model
// surface and must still complete the ordinary merge/verify/ack/cleanup path.
func TestS3AuditUnrelatedImplementationChangeMerges(t *testing.T) {
	root, worker, model, original := seedS3AuditFrozenModel(t)
	implementation := filepath.Join(worker, "internal", "order_handler.go")
	if err := os.WriteFile(implementation, []byte("package internal\n\nconst orderHandlerVersion = 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, worker, "add", "internal/order_handler.go")
	runGitIn(t, worker, "commit", "-m", "unrelated implementation delivery")

	rootBefore := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	targetBefore := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
	workerBefore := strings.TrimSpace(runGitIn(t, worker, "rev-parse", "HEAD"))
	code, stdout, stderr := runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("unrelated implementation integration failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	rootAfter := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	targetAfter := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
	if rootAfter != targetAfter || rootAfter == rootBefore || targetAfter == targetBefore {
		t.Fatalf("successful integration did not advance authority target exactly once: root=%s→%s target=%s→%s", rootBefore, rootAfter, targetBefore, targetAfter)
	}
	parents := strings.Fields(runGitIn(t, root, "show", "-s", "--format=%P", targetAfter))
	if len(parents) != 2 || parents[1] != workerBefore {
		t.Fatalf("successful integration must be a non-squash worker merge: parents=%v worker=%s", parents, workerBefore)
	}
	modelBytes, err := os.ReadFile(filepath.Join(root, model))
	if err != nil {
		t.Fatal(err)
	}
	if string(modelBytes) != string(original) {
		t.Fatalf("unrelated implementation merge changed frozen model: %s", modelBytes)
	}
	implementationBytes, err := os.ReadFile(filepath.Join(root, "internal", "order_handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(implementationBytes) != "package internal\n\nconst orderHandlerVersion = 2\n" {
		t.Fatalf("unrelated implementation was not merged: %s", implementationBytes)
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("successful integration must clean the worker checkout: %v", err)
	}
}

// Root writes remain protected; this control isolates the missing merge input
// from a general failure to project shared model rows into policy.
func TestS3AuditFrozenModelRootWriteBlocked(t *testing.T) {
	root, _, model, _ := seedS3AuditFrozenModel(t)
	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "tool_name": "Write", "cwd": root,
		"tool_input": map[string]any{"file_path": filepath.Join(root, model), "content": "changed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{"hook", "--event", "PreToolUse", "--root", root}, bytes.NewReader(payload), &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), `"permissionDecision":"deny"`) || !strings.Contains(stdout.String(), "locked") {
		t.Fatalf("root frozen model write was not denied: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
