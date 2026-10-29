package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestRegisteredOwnedReceiptRelocationRetainsOriginalFacts(t *testing.T) {
	for _, historical := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "history"}[historical], func(t *testing.T) {
			root, task, taskPath := newS6RecordContextFixture(t)
			const manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
			var manifest map[string]any
			body, err := os.ReadFile(filepath.Join(root, manifestRef))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(body, &manifest); err != nil {
				t.Fatal(err)
			}
			row := manifest["assignments"].([]any)[0].(map[string]any)
			row["integration_check_mode"] = "post_merge"
			row["check_profiles"].([]any)[0].(map[string]any)["location"] = "main"
			profile := checkexec.Profile{ID: "CP-RELOCATE-OWNED", Version: "v1", CheckIndex: 0, Command: "true", Location: "worker", TimeoutSeconds: 1, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
			row["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{map[string]any{"index": 0, "command": profile.Command, "profile": profile}}}
			stageJSON(t, root, manifestRef, manifest)
			stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", task, "--task", taskPath)
			stageCLI(t, "runtime", "workspace", "bind", "--root", root)
			source := stageSnapshot(t, root)
			registry, err := workspace.Decode(source.State)
			if err != nil {
				t.Fatal(err)
			}
			record, found, err := assignmentrecord.ForOwner(source.State, "agent-s6-native-context")
			if err != nil || !found {
				t.Fatalf("original Record: found=%t err=%v", found, err)
			}
			e, err := registry.Plan(t.Context(), source.State, record.AssignmentID, record.OwnerAgentID, record.EffectiveWritePaths, record.RequiredChecks, []string{record.Task.Ref})
			if err != nil {
				t.Fatal(err)
			}
			e, err = registry.FreezeCheckProfiles(e, record.CheckProfiles)
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.Materialize(t.Context(), e); err != nil {
				t.Fatal(err)
			}
			e.Status = "ready"
			ref := capturedRelocationOwnedReceipt(t, root, e)
			if err := registry.AcceptOwnedCheckReceipt(&e, 0, ref); err != nil {
				t.Fatal(err)
			}
			originalBytes, err := os.ReadFile(ref.Path)
			if err != nil {
				t.Fatal(err)
			}
			if historical {
				e.Status = "retired"
				delete(registry.Executions, e.AssignmentID)
				registry.History = append(registry.History, e)
			} else {
				registry.Executions[e.AssignmentID] = e
			}
			source.State["workspace"] = workspace.Encode(registry)
			if err := registry.ValidateAuthority(source.State); err != nil {
				t.Fatal(err)
			}
			stageJSON(t, root, ".claude/loop-state.json", source.State)
			moved := filepath.Join(t.TempDir(), "moved")
			request := workspace.RebindRequest{RuntimeID: record.RuntimeID, OldMainRoot: root, OldCommonDir: registry.CommonDir, NewMainRoot: moved, NewCommonDir: filepath.Join(moved, ".git"), Branch: registry.Branch, ExpectedHead: registry.BoundHead, Paths: map[string]string{e.Path: filepath.Join(moved, ".worktrees", filepath.Base(e.Path))}}
			if err := os.Rename(root, moved); err != nil {
				t.Fatal(err)
			}
			if _, err := workspace.Git(t.Context(), moved, "worktree", "repair", request.Paths[e.Path]); err != nil {
				t.Fatal(err)
			}
			next, err := registry.PlanRebind(t.Context(), source.State, request)
			if err != nil {
				t.Fatal(err)
			}
			writer := runtime.NewWriter(filepath.Join(moved, ".claude/loop-state.json"), filepath.Join(moved, ".claude/loop-events.jsonl"), moved, semantic.RuntimeCandidateValidator{})
			for _, change := range []string{"locator", "checkout", "worker-index", "source-sha"} {
				bad := workspace.Encode(next)
				var execution map[string]any
				if historical {
					execution = bad["execution_history"].([]any)[0].(map[string]any)
				} else {
					execution = bad["executions"].(map[string]any)[e.AssignmentID].(map[string]any)
				}
				owned := execution["owned_check_receipts"].([]any)[0].(map[string]any)
				tool := owned["receipt"].(map[string]any)
				switch change {
				case "locator":
					tool["path"] = filepath.Join(moved, "another-receipt.json")
				case "checkout":
					tool["checkout"] = request.Paths[e.Path]
				case "worker-index":
					owned["worker_check_index"] = 1
				case "source-sha":
					tool["sha256"] = strings.Repeat("0", 64)
				}
				if _, err := writer.RelocateWorkspace(source, bad, "invalid-owned-"+change, "test"); err == nil {
					t.Fatalf("relocation changed original owned %s", change)
				}
			}
			requestRoot := t.TempDir()
			stageJSON(t, requestRoot, "relocation.json", request)
			args := []string{"runtime", "workspace", "rebind", "--root", moved, "--request", filepath.Join(requestRoot, "relocation.json"), "--reason", "synthetic receipt relocation test"}
			stageCLI(t, args...)
			accepted := stageSnapshot(t, moved)
			registered, err := workspace.Decode(accepted.State)
			if err != nil {
				t.Fatal(err)
			}
			var actual workspace.Execution
			if historical {
				actual = registered.History[0]
			} else {
				actual = registered.Executions[e.AssignmentID]
			}
			expected := ref
			rel, err := filepath.Rel(root, ref.Path)
			if err != nil {
				t.Fatal(err)
			}
			expected.Path = filepath.Join(moved, rel)
			if len(actual.OwnedCheckReceipts) != 1 || actual.OwnedCheckReceipts[0].Receipt != expected || actual.OwnedCheckReceipts[0].WorkerCheckIndex != 0 {
				t.Fatalf("captured owned facts changed: %+v", actual.OwnedCheckReceipts)
			}
			got, err := os.ReadFile(expected.Path)
			if err != nil || !bytes.Equal(got, originalBytes) {
				t.Fatalf("original receipt bytes changed: %v", err)
			}
			if _, err := checkexec.VerifyToolReceipt(filepath.Join(moved, ".claude/evidence/workspace-checks"), expected); err != nil {
				t.Fatal(err)
			}
			stageCLI(t, args...)
			replayed := stageSnapshot(t, moved)
			if replayed.Revision != accepted.Revision || accepted.Revision != source.Revision+1 {
				t.Fatalf("original relocation replay rewrote state: revision=%d/%d", replayed.Revision, accepted.Revision)
			}
		})
	}
}

func capturedRelocationOwnedReceipt(t *testing.T, root string, e workspace.Execution) checkexec.ToolReceipt {
	t.Helper()
	profile := e.WorkerChecks[0].Profile
	observation, err := checkexec.ObserveProfile(t.Context(), e.Path, profile)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".claude/evidence/workspace-checks/relocation-owned-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "output.log")
	// Explicit synthetic facts validate provenance coordinates, not a real
	// sandbox run or product check; no permission capability is fabricated.
	if err := os.WriteFile(log, []byte("Synthetic owned relocation facts.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	body, err := checkexec.SealReceipt(map[string]any{
		"version": 2, "command": profile.Command, "status": "pass", "output": log,
		"check_binding": workspace.CheckBinding(e), "profile_observation": observation,
		"check_index": 0, "worker_check_index": 0, "assignment_record": e.AssignmentRecord, "assignment_record_sha256": e.AssignmentRecordSHA256,
		"worker_check_snapshot": map[string]any{"runtime_id": e.RuntimeID, "baseline_generation": e.BaselineGeneration, "assignment_id": e.AssignmentID, "owner_agent_id": e.AgentID, "execution_generation": e.Generation, "bootstrap_sha256": e.BootstrapSHA256},
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "receipt.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	var ref checkexec.ToolReceipt
	ctx := checkexec.WithReceiptCapture(checkexec.WithBinding(checkexec.WithProfile(t.Context(), profile), workspace.CheckBinding(e)), func(value checkexec.ToolReceipt) { ref = value })
	if err := checkexec.CaptureReceipt(ctx, path, e.Path, body); err != nil {
		t.Fatal(err)
	}
	return ref
}
