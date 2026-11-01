package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9OriginalCandidateReadsHistoricalMachineValues(t *testing.T) {
	root, worker, plan, in, _ := nativeS9WorkerFixture(t)
	in.Event, in.CWD = "PreToolUse", worker
	var diagnostic bytes.Buffer
	snap := repairDispatchSnapshot(t, root)
	if err := receiveNativeS9BuilderEvent(root, snap.State, in, plan, in.LastAssistantMessage, &diagnostic, nil); err != nil {
		t.Fatalf("original Plan and Result reception: %v %s", err, &diagnostic)
	}
	snap = repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairResultPacketsContext(context.Background(), root, snap.State, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Result packet: %d %v", len(packets), err)
	}
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("original workspace: %v", err)
	}
	e := registry.Executions[plan.Binding.ManifestAssignmentID]
	if e.DeliveryRef == "" {
		t.Fatal("original Result candidate was not received")
	}
	path := filepath.Join(root, filepath.FromSlash(e.DeliveryRef))
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		old  any
		omit bool
	}{
		{name: "missing", omit: true}, {name: "null"}, {name: "nonstandard", old: "old"},
		{name: "number", old: 42}, {name: "array", old: []any{1, "old"}}, {name: "object", old: map[string]any{"old": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(original, &doc); err != nil {
				t.Fatal(err)
			}
			proposal := doc["proposal"].(map[string]any)
			if !tc.omit {
				for _, key := range []string{"contract_sha256", "source_head", "base_commit"} {
					doc[key] = tc.old
				}
				for _, key := range []string{"contract", "session", "plan", "plan_report"} {
					proposal[key].(map[string]any)["sha256"] = tc.old
				}
			}
			legacy, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, legacy, 0600); err != nil {
				t.Fatal(err)
			}
			candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef})
			if err != nil || candidate.AssignmentID != e.AssignmentID || candidate.Provenance == nil || candidate.Provenance.OwnerAgentID != e.AgentID {
				t.Fatalf("Main's original candidate: %+v %v", candidate, err)
			}
			if _, ok, err := runtime.VerifiedRepairResultCandidateReceipt(root, snap.State, packets[0].ID, e.AgentID); err != nil || !ok {
				t.Fatalf("original candidate receipt: ok=%v err=%v", ok, err)
			}
			if complete, err := repair.RepairResultWorkCompleteWithContext(context.Background(), root, snap.State, packets[0].ID, e.AgentID); err != nil || !complete {
				t.Fatalf("original completion: complete=%v err=%v", complete, err)
			}
			if _, _, err := runtime.VerifiedRepairResultCandidateReceipt(root, snap.State, packets[0].ID, "different-owner"); err == nil {
				t.Fatal("different owner claimed the original receipt")
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, legacy) {
				t.Fatalf("original candidate was modified: %v", err)
			}
		})
	}
	// The final object-valued historical metadata remains on the accepted
	// Candidate while Main uses the original actual Git/check integration path.
	beforeIntegration, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	planPath, resultPath := nativeS9TestAuthorPaths(t, root, plan)
	originals := make(map[string][]byte)
	for _, relative := range []string{planPath, resultPath} {
		originals[relative], err = os.ReadFile(filepath.Join(worker, relative))
		if err != nil {
			t.Fatal(err)
		}
	}
	var out, integrateDiagnostic bytes.Buffer
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "git grep -q 'const Restored = true' -- internal/api/one.go"}, &out, &integrateDiagnostic); code == 0 || !strings.Contains(integrateDiagnostic.String(), "cleanup pending") {
		t.Fatalf("expected integrated Main with independent cleanup pending: code=%d stdout=%s stderr=%s", code, &out, &integrateDiagnostic)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, e))
	if err != nil || !found || cp.State != integration.StateCleanupPending || cp.VerifiedAt == "" {
		t.Fatalf("Main verification and ACK checkpoint: %+v %v", cp, err)
	}
	for relative, original := range originals {
		if after, err := os.ReadFile(filepath.Join(worker, relative)); err != nil || !bytes.Equal(after, original) {
			t.Fatalf("cleanup changed original answer %s: %v", relative, err)
		}
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, beforeIntegration) {
		t.Fatalf("Main integration rewrote the original candidate: %v", err)
	}
	after := repairDispatchSnapshot(t, root)
	finals := repairArtifactRefs(repairPointerCLI(after.State), "result_refs", "result_ref")
	if len(finals) != 1 {
		t.Fatalf("Main did not accept the original Final: %v", finals)
	}
	contributions, err := repair.ReceivedRepairContributions(root, after.State)
	if err != nil || len(contributions) != 1 {
		t.Fatalf("Impact could not read accepted contribution: %+v %v", contributions, err)
	}
	next, err := repairDispatchPlan(root, after.State)
	if err != nil {
		t.Fatal(err)
	}
	division := repairDispatchPaths(root, after.State)[0]
	feedback := repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: next.Assignments[1].AssignmentID, RoleFamily: "backend-builder"}})
	if !strings.Contains(feedback, "exact S9 brief") || len(nativeS9PlanPackets(root, repairDispatchSnapshot(t, root).State)) != 2 {
		t.Fatalf("cleanup pending blocked the next S9 responsibility: %s", feedback)
	}
}
