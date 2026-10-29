package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Execute the original native/Integrator/Final APIs up to the actual crash
// boundary. Nothing rewinds Runtime or manufactures a verified checkpoint.
// Professional answers are synthetic; Git and Main checks really execute.
func s9NativeIntegrationCandidate(t *testing.T) (string, string, workspace.Execution, workpacket.RepairResultPacket, integration.Inspection) {
	t.Helper()
	root, worker, planPacket, input, _ := nativeS9WorkerFixture(t)
	input.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
		t.Fatalf("original native Stop: %t %d %s", handled, code, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("actual delivery registry: %v", err)
	}
	e := registry.Executions[planPacket.Binding.ManifestAssignmentID]
	candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef, SHA256: e.DeliverySHA256})
	if err != nil {
		t.Fatal(err)
	}
	packet, typed, err := currentRepairResultPacketForDelivery(root, snapshot.State, e, candidate)
	if err != nil || !typed {
		t.Fatalf("original typed candidate: %v", err)
	}
	ctx := context.Background()
	logs, err := repair.WorkspaceCandidateLogProjectionForPacket(ctx, root, packet, e, candidate)
	if err != nil {
		t.Fatalf("original captured Worker evidence projection: %v", err)
	}
	private, err := repair.NativeS9PrivateProjection(ctx, root, packet, e, candidate, false)
	if err != nil {
		t.Fatalf("original Worker private evidence projection: %v", err)
	}
	area, err := workspaceNativePrivateArea(ctx, root, packet, e)
	if err != nil || area.Path == "" {
		t.Fatalf("original frozen Worker private area: %+v %v", area, err)
	}
	a := hookctx.AssignmentContext{AssignmentID: e.AssignmentID, OwnerAgentID: e.AgentID, TaskID: packet.Binding.Sources.TaskID, WorktreePath: e.Path, Branch: e.Branch, TargetBranch: e.TargetBranch}
	inspection, err := integration.Inspect(ctx, integration.InspectRequest{Root: root, Assignment: a, TargetBranch: e.TargetBranch, BaselineGeneration: e.BaselineGeneration, RuntimeID: e.RuntimeID}, integration.InspectConfig{WorkerLogEvidence: logs, WorkerPrivateEvidence: private,
		WorkerPrivateArea: area.Path, WorkerPrivateBaseCommit: area.BaseCommit, WorkerPrivateWritePaths: area.WritePaths, WorkerPrivateBaselinePaths: area.BaselinePaths, ValidateDelivery: func(_ context.Context, in integration.Inspection) error {
			if in.SourceHead != candidate.SourceHead {
				return fmt.Errorf("candidate source changed")
			}
			return nil
		}})
	if err != nil || !inspection.Ready {
		t.Fatalf("original Main Inspect: %+v %v", inspection, err)
	}
	return root, worker, e, packet, inspection
}

func s9BeforeIntegrationAck(t *testing.T, acceptFinal bool) (string, string, workspace.Execution, workpacket.RepairResultPacket, integration.Checkpoint, repair.ArtifactRef) {
	t.Helper()
	root, worker, e, packet, inspection := s9NativeIntegrationCandidate(t)
	snapshot := repairDispatchSnapshot(t, root)
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: e.RuntimeID, RequiredChecks: []string{"git grep -q 'const Restored = true' -- internal/api/one.go"}, CheckRunner: integration.CommandCheckRunner})
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("real Main check did not verify: %+v %v", verified, err)
	}
	var resultRef repair.ArtifactRef
	if acceptFinal {
		proof, err := repairResultMainProof(root, e, verified.Checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		_, result, ref, err := repair.SubmitIssuedRepairResult(context.Background(), root, resolveRootPath(root, ".claude/loop-state.json"), resolveRootPath(root, ".claude/loop-events.jsonl"), repair.SubmitIssuedRepairResultRequest{ExpectedRevision: snapshot.Revision, PacketID: packet.ID, OwnerAgentID: e.AgentID, MainProof: proof})
		if err != nil || result.Result != "pass" || result.Receipt == nil {
			t.Fatalf("original Final Writer did not accept actual Main proof: %+v %v", result, err)
		}
		resultRef = ref
	}
	return root, worker, e, packet, verified.Checkpoint, resultRef
}

func TestS9PublicRecoveryDistinguishesAcceptedFinalFromCurrentChecks(t *testing.T) {
	for _, scenario := range []string{"after-final", "before-final", "cleanup-retry", "source-advanced", "target-rewritten", "receipt-tampered", "checkpoint-tampered", "new-unperformed-check"} {
		t.Run(scenario, func(t *testing.T) {
			root, worker, e, packet, original, ref := s9BeforeIntegrationAck(t, scenario != "before-final")
			var finalBytes []byte
			if ref.Path != "" {
				var err error
				finalBytes, err = os.ReadFile(resolveRootPath(root, ref.Path))
				if err != nil {
					t.Fatal(err)
				}
			}
			writeRepairLeadFile(t, root, "internal/api/later.go", "package api\n\nconst LaterContribution = true\n")
			repairLeadGit(t, root, "add", "internal/api/later.go")
			repairLeadGit(t, root, "commit", "-qm", "Later synthetic Main contribution")
			head := repairLeadGit(t, root, "rev-parse", "HEAD")
			args := []string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID}
			switch scenario {
			case "cleanup-retry":
				writeRepairLeadFile(t, worker, "unreceived-work.txt", "preserve this unrelated Worker output\n")
			case "source-advanced":
				writeRepairLeadFile(t, worker, "internal/api/one.go", "package api\n\nconst Restored = false\n")
				repairLeadGit(t, worker, "add", "internal/api/one.go")
				repairLeadGit(t, worker, "commit", "-qm", "Unreceived different Worker source")
			case "target-rewritten":
				repairLeadGit(t, root, "reset", "--hard", original.TargetHead)
				head = repairLeadGit(t, root, "rev-parse", "HEAD")
			case "receipt-tampered":
				var receipt map[string]any
				readRepairLeadJSON(t, original.CheckReceipts[0], &receipt)
				receipt["status"] = "fail"
				writeRepairLeadJSON(t, original.CheckReceipts[0], receipt)
			case "checkpoint-tampered":
				var checkpoint map[string]any
				readRepairLeadJSON(t, workspaceExecutionCheckpointPath(root, e), &checkpoint)
				checkpoint["verified_at"] = "2026-10-21T00:00:00Z"
				writeRepairLeadJSON(t, workspaceExecutionCheckpointPath(root, e), checkpoint)
			case "new-unperformed-check":
				args = append(args, "--check-command", "test -f internal/api/later.go")
			}
			before := repairDispatchRuntimeBytes(t, root)
			var output, diagnostic bytes.Buffer
			code := runWorkspaceDelivery(args, &output, &diagnostic)
			if scenario == "cleanup-retry" {
				if code == 0 {
					t.Fatal("recovery deleted unrelated Worker output")
				}
				repairDispatchAssertUnchanged(t, root, before)
				if err := os.Remove(resolveRootPath(worker, "unreceived-work.txt")); err != nil {
					t.Fatal(err)
				}
				output.Reset()
				diagnostic.Reset()
				code = runWorkspaceDelivery(args, &output, &diagnostic)
			}
			positive := scenario == "after-final" || scenario == "before-final" || scenario == "cleanup-retry"
			if !positive {
				if code == 0 {
					t.Fatalf("invalid recovery cleaned an accepted contribution: %s", &output)
				}
				want := map[string]string{"source-advanced": "source", "target-rewritten": "merge", "receipt-tampered": "check", "checkpoint-tampered": "verified facts changed", "new-unperformed-check": "test -f internal/api/later.go"}[scenario]
				if !strings.Contains(diagnostic.String(), want) {
					t.Fatalf("recovery did not identify the actual %s boundary: %s", scenario, &diagnostic)
				}
				if _, err := os.Stat(worker); err != nil {
					t.Fatalf("failed recovery removed Worker evidence: %v %s", err, &diagnostic)
				}
				repairDispatchAssertUnchanged(t, root, before)
				if after, err := os.ReadFile(resolveRootPath(root, ref.Path)); err != nil || !bytes.Equal(after, finalBytes) {
					t.Fatalf("failed recovery rewrote accepted Final: %v", err)
				}
				if scenario != "receipt-tampered" && scenario != "checkpoint-tampered" {
					if _, found, err := runtime.VerifiedRepairResultReceipt(root, repairDispatchSnapshot(t, root).State, packet.ID, e.AgentID); err != nil || !found {
						t.Fatalf("current Main/cleanup problem invalidated the accepted historical receipt: %t %v", found, err)
					}
				}
				return
			}
			if code != 0 {
				t.Fatalf("original public integration recovery: %d %s %s", code, &output, &diagnostic)
			}
			cp, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, e))
			if err != nil || !found || cp.State != integration.StateComplete || cp.MergeCommit != original.MergeCommit {
				t.Fatalf("original attempt did not complete without another merge: %+v %v", cp, err)
			}
			if scenario != "before-final" {
				if cp.TestedHead != original.TestedHead || !reflect.DeepEqual(cp.CheckReceipts, original.CheckReceipts) || cp.VerifiedAt != original.VerifiedAt {
					t.Fatalf("accepted Final was dragged back through checks and lost its original proof: %+v", cp)
				}
				if after, err := os.ReadFile(resolveRootPath(root, ref.Path)); err != nil || !bytes.Equal(after, finalBytes) {
					t.Fatalf("accepted Final changed: %v", err)
				}
			} else if cp.TestedHead != head || len(cp.CheckReceipts) != len(original.CheckReceipts)+1 {
				t.Fatalf("unaccepted Final borrowed old-head checks: %+v", cp)
			}
			current := repairDispatchSnapshot(t, root)
			if _, found, err := runtime.VerifiedRepairResultReceipt(root, current.State, packet.ID, e.AgentID); err != nil || !found {
				t.Fatalf("completed recovery invalidated the original Final receipt: %t %v", found, err)
			}
			refs := repairArtifactRefs(repairPointerCLI(current.State), "result_refs", "result_ref", "result_sha256")
			if len(refs) != 1 || scenario == "after-final" && refs[0].Path != ref.Path {
				t.Fatalf("same responsibility was submitted again: %+v", refs)
			}
			if got := repairLeadGit(t, root, "rev-parse", "HEAD"); got != head {
				t.Fatalf("recovery changed Main HEAD: %s -> %s", head, got)
			}
			if _, err := os.Stat(worker); !os.IsNotExist(err) {
				t.Fatalf("safe cleanup incomplete: %v", err)
			}
			before = repairDispatchRuntimeBytes(t, root)
			output.Reset()
			diagnostic.Reset()
			if code := runWorkspaceDelivery(args, &output, &diagnostic); code != 0 || strings.Contains(diagnostic.String(), "new authorized verification attempt") {
				t.Fatalf("completed contribution required another attempt: %d %s", code, &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}
