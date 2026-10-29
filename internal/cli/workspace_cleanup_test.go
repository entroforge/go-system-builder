package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestLastFinalPreparesImpactBeforeIndependentCleanup(t *testing.T) {
	for _, variant := range []string{"impact-before-cleanup", "failed-main-check", "view-change-during-check", "view-symlink-during-check"} {
		t.Run(variant, func(t *testing.T) {
			root, worker, p, _, _ := nativeS9WorkerFixture(t)

			originalPlan, _, err := nativeS9AuthorPaths(root, p)
			if err != nil {
				t.Fatal(err)
			}
			if originalPlan != p.AnswerPath {
				if raw, readErr := os.ReadFile(filepath.Join(worker, p.AnswerPath)); readErr == nil {
					writeRepairLeadFile(t, worker, originalPlan, string(raw))
					if err := os.Remove(filepath.Join(worker, p.AnswerPath)); err != nil {
						t.Fatal(err)
					}
				}
			}
			var diag, out bytes.Buffer
			if err := receiveNativeS9BuilderFrom(context.Background(), root, repairDispatchSnapshot(t, root).State, p, worker, false, &diag, nil); err != nil {
				t.Fatal(err)
			}
			snapshot := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snapshot.State)
			if err != nil {
				t.Fatal(err)
			}
			first := registry.Executions[p.Binding.ManifestAssignmentID]
			if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", first.AssignmentID, "--agent", first.AgentID, "--check-command", "test -f internal/api/one.go"}, &out, &diag); code != 0 {
				t.Fatalf("first Final: %s", &diag)
			}
			beforeSecond := repairDispatchSnapshot(t, root)
			plan, err := repairDispatchPlan(root, beforeSecond.State)
			if err != nil {
				t.Fatal(err)
			}
			division := repairDispatchPaths(root, beforeSecond.State)[0]
			repairDispatchSave(t, root, division, []repairDispatchChoice{{AssignmentID: plan.Assignments[1].AssignmentID, RoleFamily: "backend-builder"}})
			var secondPacket workpacket.RepairPlanReportPacket
			for _, q := range nativeS9PlanPackets(root, repairDispatchSnapshot(t, root).State) {
				if q.Binding.RepairAssignmentID == plan.Assignments[1].AssignmentID {
					secondPacket = q
				}
			}
			if secondPacket.ID == "" {
				t.Fatal("original second Plan not issued")
			}
			secondWorker := filepath.Join(t.TempDir(), "last-worker")
			repairLeadGit(t, root, "worktree", "add", "-b", "d-last-final", secondWorker, "HEAD")

			assignment := plan.Assignments[1]
			if err := os.MkdirAll(filepath.Join(secondWorker, ".loop-submissions/native-s9/"+secondPacket.ID), 0700); err != nil {
				t.Fatal(err)
			}
			writeRepairLeadJSON(t, filepath.Join(secondWorker, ".loop-submissions/native-s9/"+secondPacket.ID+"/plan.json"), workpacket.RepairPlanReportProfessionalAnswer{Plan: "Synthetic fixture: inspect existing dependent source; no product edit required", ProposedPaths: []string{}, RedChecks: []workpacket.RepairPlanReportCheck{{Name: "existing source", Command: "test -f " + assignment.Scope[0], Result: "pass", EvidenceRefs: assignment.Scope}}})
			answer := workpacket.RepairResultProfessionalAnswer{Verdict: "completed", BeforeFixResults: []workpacket.RepairResultBeforeFixResult{}, UnitResults: []workpacket.RepairResultUnitResult{}, DetectionGapArtifacts: []string{}, EvidenceRefs: assignment.Scope, ScopeDeviations: []string{}, ResidualRisks: []string{}}
			for _, id := range assignment.AssertionIDs {
				answer.BeforeFixResults = append(answer.BeforeFixResults, workpacket.RepairResultBeforeFixResult{AssertionID: id, ObservedRed: "Synthetic existing dependent source observation", EvidenceRefs: assignment.Scope})
			}
			for _, id := range assignment.UnitIDs {
				answer.UnitResults = append(answer.UnitResults, workpacket.RepairResultUnitResult{UnitID: id, Status: "pass", EvidenceRefs: assignment.Scope, ImplementationSummary: "Synthetic existing dependent source inspected", RestoredInvariantRefs: []string{}})
			}
			_, result, err := nativeS9AuthorPaths(root, secondPacket)
			if err != nil {
				t.Fatal(err)
			}
			writeRepairLeadJSON(t, filepath.Join(secondWorker, result), answer)
			diag.Reset()
			if err := receiveNativeS9BuilderFrom(context.Background(), root, repairDispatchSnapshot(t, root).State, secondPacket, secondWorker, false, &diag, nil); err != nil {
				t.Fatalf("second original Candidate: %v %s", err, &diag)
			}
			current := repairDispatchSnapshot(t, root)
			registry, err = workspace.Decode(current.State)
			if err != nil {
				t.Fatal(err)
			}
			second := registry.Executions[secondPacket.Binding.ManifestAssignmentID]
			exclude := repairLeadGit(t, secondWorker, "rev-parse", "--git-path", "info/exclude")
			body, _ := os.ReadFile(exclude)
			if err := os.WriteFile(exclude, append(body, []byte("\nunreceived-ignored.txt\n")...), 0600); err != nil {
				t.Fatal(err)
			}
			writeRepairLeadFile(t, secondWorker, "unreceived-ignored.txt", "retain unknown private data\n")
			checkCount := filepath.Join(root, ".claude/d-test-check-count")
			check := "test -f internal/api/two.go && printf check >> " + s7CaptureShellQuote(checkCount)
			if variant == "failed-main-check" {
				check = "false"
			}
			outside := filepath.Join(t.TempDir(), "outside-private")
			writeRepairLeadFile(t, filepath.Dir(outside), filepath.Base(outside), "external owner bytes")
			if strings.HasPrefix(variant, "view-") {
				packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), root), current.State)
				if err != nil {
					t.Fatal(err)
				}
				viewPath := ""
				for _, packet := range packets {
					if packet.Binding.Sources.RepairAssignmentID == secondPacket.Binding.RepairAssignmentID {
						viewPath = filepath.Join(secondWorker, packet.ViewPath)
					}
				}
				if viewPath == "" {
					t.Fatal("original issued view missing")
				}
				if variant == "view-change-during-check" {
					check += " && printf changed > " + s7CaptureShellQuote(viewPath)
				} else {
					check += " && rm " + s7CaptureShellQuote(viewPath) + " && ln -s " + s7CaptureShellQuote(outside) + " " + s7CaptureShellQuote(viewPath)
				}
			}
			diag.Reset()
			out.Reset()
			secondArgs := []string{"integrate", "--root", root, "--assignment", second.AssignmentID, "--agent", second.AgentID, "--check-command", check}
			code := runWorkspaceDelivery(secondArgs, &out, &diag)
			after := repairDispatchSnapshot(t, root)
			finals := repairArtifactRefs(repairPointerCLI(after.State), "result_refs", "result_ref", "result_sha256")
			impactPath, err := repairLeadQuestionnairePath(root, after.State, "change-impact.json")
			if err != nil {
				t.Fatal(err)
			}
			if variant == "failed-main-check" {
				if code == 0 || len(finals) != 1 || !questionnaireMissing(impactPath) || stringValue(repairPointerCLI(after.State)["status"]) == "impact_reconciliation" {
					t.Fatalf("failed Main checks manufactured Final/Impact: %d %d %s", code, len(finals), &diag)
				}
				t.Logf("EXPECTED Main failure leaves first Final only, no last Final/Impact and source preserved: %s", &diag)
				return
			}
			if code == 0 || len(finals) != 2 || stringValue(repairPointerCLI(after.State)["status"]) != "impact_reconciliation" {
				t.Fatalf("expected actual last Final with independent cleanup_pending: %d %v %s", code, finals, &diag)
			}
			cp, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, second))
			if err != nil || !found || cp.State != integration.StateCleanupPending {
				t.Fatalf("missing original cleanup checkpoint: %+v %v", cp, err)
			}
			if _, err := os.Stat(filepath.Join(secondWorker, "unreceived-ignored.txt")); err != nil {
				t.Fatal("unknown private content lost")
			}
			if strings.HasPrefix(variant, "view-") && !strings.Contains(diag.String(), "changed or nonregular ignored Worker file") {
				t.Fatalf("issued view mutation was not identified before cleanup: %s", &diag)
			}
			if raw, err := os.ReadFile(outside); err != nil || string(raw) != "external owner bytes" {
				t.Fatalf("external owner bytes changed: %q %v", raw, err)
			}
			originalPlan, originalResult, err := nativeS9AuthorPaths(root, secondPacket)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{originalPlan, originalResult} {
				if _, err := os.Stat(filepath.Join(secondWorker, path)); err != nil {
					t.Fatalf("professional original removed before ignored guard: %s %v", path, err)
				}
			}

			if questionnaireMissing(impactPath) {
				t.Fatal("original accepted Final exit did not prepare Impact before cleanup")
			}
			data, err := os.ReadFile(impactPath)
			if err != nil || !bytes.Contains(data, []byte("required_reverification_ids")) {
				t.Fatal("original Impact obligation projection missing")
			}
			t.Logf("Public integrate prepared original Impact before independent cleanup_pending: %s", &diag)
			if variant == "impact-before-cleanup" {
				originalImpact, err := os.ReadFile(impactPath)
				if err != nil {
					t.Fatal(err)
				}
				unknownPath := filepath.Join(secondWorker, "unreceived-ignored.txt")
				unknownBytes, err := os.ReadFile(unknownPath)
				if err != nil {
					t.Fatal(err)
				}
				// Synthetic owner resolution: preserve its bytes outside the source, then remove its fixture copy. Not a new author delivery step.
				resolved := filepath.Join(t.TempDir(), "owner-preserved.txt")
				if err := os.WriteFile(resolved, unknownBytes, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(unknownPath); err != nil {
					t.Fatal(err)
				}
				diag.Reset()
				out.Reset()
				if retryCode := runWorkspaceDelivery(secondArgs, &out, &diag); retryCode != 0 {
					t.Fatalf("original independent cleanup retry after owner resolution: %s", &diag)
				}
				recovered, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, second))
				if err != nil || !found || recovered.State != integration.StateComplete || recovered.VerifiedAt != cp.VerifiedAt {
					t.Fatalf("cleanup recovery replaced verified facts: %+v %v", recovered, err)
				}
				if _, err := os.Stat(secondWorker); !os.IsNotExist(err) {
					t.Fatal("resolved source not cleaned")
				}
				if body, err := os.ReadFile(checkCount); err != nil || string(body) != "check" {
					t.Fatalf("cleanup retry reran original Main checks: %q %v", body, err)
				}
				if body, err := os.ReadFile(impactPath); err != nil || !bytes.Equal(body, originalImpact) {
					t.Fatal("cleanup retry rewrote Impact questionnaire")
				}
				nowFinals := repairArtifactRefs(repairPointerCLI(repairDispatchSnapshot(t, root).State), "result_refs", "result_ref", "result_sha256")
				if len(nowFinals) != len(finals) || nowFinals[0] != finals[0] || nowFinals[1] != finals[1] {
					t.Fatal("cleanup retry reaccepted/replaced Final")
				}
				if body, err := os.ReadFile(resolved); err != nil || !bytes.Equal(body, unknownBytes) {
					t.Fatal("owner-preserved private bytes changed")
				}
				t.Log("Original public cleanup retry completed independently after synthetic owner resolution; no repeated Main checks, Final, Impact or author submission; VerifiedAt unchanged")
			}
			beforeRuntime := repairDispatchRuntimeBytes(t, root)
			diag.Reset()
			out.Reset()
			if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &out, &diag); code != 0 {
				t.Fatalf("original public Impact preparation recovery: %s", &diag)
			}
			repairDispatchAssertUnchanged(t, root, beforeRuntime)
			t.Log("Original public impact prepare recovery/idempotence succeeds without resubmitting Final or author work; Runtime unchanged")
		})
	}
}
