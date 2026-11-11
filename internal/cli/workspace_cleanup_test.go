package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestLastFinalPreparesImpactBeforeIndependentCleanup(t *testing.T) {
	for _, variant := range []string{"impact-before-cleanup", "impact-prepare-failure", "failed-main-check", "view-change-during-check", "view-symlink-during-check"} {
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
			// Blueprint L4-workflow-convergence 3.3.5 and L4-worktree-governance
			// 6.1: cleanup_pending is a resumable resource state, not an
			// integration failure and not a reason to force the worktree away.
			// The first Final stays verified and acknowledged; only the
			// untracked professional material keeps its cleanup pending.
			firstCode := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", first.AssignmentID, "--agent", first.AgentID, "--check-command", "test -f internal/api/one.go"}, &out, &diag)
			if firstCode == 0 {
				t.Fatalf("first Final reported success while its cleanup was still pending: %d %s", firstCode, &diag)
			}
			if !strings.Contains(diag.String(), "integration verified") || !strings.Contains(diag.String(), "cleanup pending") {
				t.Fatalf("first Final did not report a verified integration with pending cleanup: %d %s", firstCode, &diag)
			}
			firstCheckpoint, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, first))
			if err != nil || !found || firstCheckpoint.State != integration.StateCleanupPending || firstCheckpoint.LastErrorCode != "LOOP_INTEGRATION_CLEANUP_PENDING" {
				t.Fatalf("first Final did not record acknowledged cleanup_pending: %+v %v", firstCheckpoint, err)
			}
			// The verified integration and its recorded checks are the facts
			// that must survive a pending cleanup. The completion report
			// reference is Main-side: in this scenario Main has not filed its
			// own completion report yet, so its absence says nothing about the
			// integration that was already verified and acknowledged.
			if firstCheckpoint.VerifiedAt == "" || len(firstCheckpoint.CheckReceipts) == 0 {
				t.Fatalf("first Final cleanup_pending lost its verified integration: %+v", firstCheckpoint)
			}
			// The checkpoint state machine only reaches cleanup_pending after the
			// merge, the checks and the completion ACK have all been recorded,
			// so the recorded state is the structural proof that the
			// acknowledged Result survived the pending cleanup.
			if firstCheckpoint.State != integration.StateCleanupPending || firstCheckpoint.LastErrorCode != "LOOP_INTEGRATION_CLEANUP_PENDING" {
				t.Fatalf("first Final cleanup_pending lost its acknowledged Result: %+v", firstCheckpoint)
			}
			if _, err := os.Stat(worker); err != nil {
				t.Fatalf("first Final cleanup removed the Worker worktree instead of preserving it: %v", err)
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
			// The original consumer addresses the author's private submission
			// through the native packet path, which splits an over-long packet
			// identity into readable segments instead of one oversized file
			// name (internal/repair/native_s9_private.go:13). The fixture must
			// use the same production entry point, otherwise it builds a path
			// the system never reads.
			secondPacketDir := filepath.Join(secondWorker, ".loop-submissions/native-s9/"+repair.NativeS9PacketPath(secondPacket.ID))
			if err := os.MkdirAll(secondPacketDir, 0700); err != nil {
				t.Fatal(err)
			}
			writeRepairLeadJSON(t, filepath.Join(secondPacketDir, "plan.json"), workpacket.RepairPlanReportProfessionalAnswer{Plan: "Synthetic fixture: inspect existing dependent source; no product edit required", ProposedPaths: []string{}, RedChecks: []workpacket.RepairPlanReportCheck{{Name: "existing source", Command: "test -f " + assignment.Scope[0], Result: "pass", EvidenceRefs: assignment.Scope}}})
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
			impactTarget, err := repairLeadQuestionnairePath(root, current.State, "change-impact.json")
			if err != nil {
				t.Fatal(err)
			}
			originalPlanPath, originalResultPath := nativeS9TestAuthorPaths(t, root, secondPacket)
			originals := map[string][]byte{}
			for _, relative := range []string{originalPlanPath, originalResultPath} {
				raw, err := os.ReadFile(filepath.Join(secondWorker, relative))
				if err != nil {
					t.Fatal(err)
				}
				originals[relative] = raw
			}
			// The ChangeImpact questionnaire may already be prepared when Main
			// takes the candidate: blueprint L3-S9-repair.md:271 "Main 接手
			// Candidate 时即可准备原 ChangeImpact 和已有贡献材料,不要求先造
			// Final,也不把提前准备题目当成整批完成". Preparing it early is
			// allowed, so the prepared draft is captured as-is and must not
			// change unless Main actually answers it.
			preparedImpact, impactPrepared := []byte(nil), false
			if raw, err := os.ReadFile(impactTarget); err == nil {
				preparedImpact, impactPrepared = raw, true
			}
			if variant == "impact-prepare-failure" {
				// The actual Main check creates a filesystem failure at the original
				// issuer's exact target, after verification and before first prepare.
				// The obstacle has to be a directory there, so any already
				// prepared draft is cleared first; preparing the questionnaire
				// early is allowed and is not itself the failure under test.
				if impactPrepared {
					if err := os.Remove(impactTarget); err != nil {
						t.Fatal(err)
					}
				}
				check += " && mkdir -p " + s7CaptureShellQuote(impactTarget)
			}
			diag.Reset()
			out.Reset()
			secondArgs := []string{"integrate", "--root", root, "--assignment", second.AssignmentID, "--agent", second.AgentID, "--check-command", check}
			code := runWorkspaceDelivery(secondArgs, &out, &diag)
			after := repairDispatchSnapshot(t, root)
			finals := repairArtifactRefs(repairPointerCLI(after.State), "result_refs", "result_ref")
			impactPath, err := repairLeadQuestionnairePath(root, after.State, "change-impact.json")
			if err != nil {
				t.Fatal(err)
			}
			if variant == "failed-main-check" {
				if code == 0 || len(finals) != 1 || stringValue(repairPointerCLI(after.State)["status"]) == "impact_reconciliation" {
					t.Fatalf("failed Main checks manufactured Final/Impact: %d %d %s", code, len(finals), &diag)
				}
				// A failed Main check must not turn into a saved Impact answer.
				// The questionnaire itself may already be prepared (blueprint
				// L3-S9-repair.md:271), but it must still hold the prepared draft.
				if impactPrepared {
					if raw, err := os.ReadFile(impactPath); err != nil || !bytes.Equal(raw, preparedImpact) {
						t.Fatalf("failed Main checks saved an Impact answer: %v %s", err, &diag)
					}
				} else if !questionnaireMissing(impactPath) {
					t.Fatalf("failed Main checks manufactured the Impact questionnaire: %s", &diag)
				}
				t.Logf("EXPECTED Main failure leaves first Final only, no last Final/Impact and source preserved: %s", &diag)
				return
			}
			var recoveredImpact []byte
			if variant == "impact-prepare-failure" {
				cp, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, second))
				if code == 0 || len(finals) != 2 || err != nil || !found || cp.State != integration.StateAcknowledged || stringValue(repairPointerCLI(after.State)["status"]) != "impact_reconciliation" {
					t.Fatalf("first Impact failure lost Final/ACK: %d %+v %v %s", code, cp, err, &diag)
				}
				if !strings.Contains(diag.String(), repairImpactPreparationRecovery(root)) || !strings.Contains(diag.String(), "not a regular file") || strings.Contains(diag.String(), "cleanup_pending") {
					t.Fatalf("wrong original Impact failure/recovery boundary: %s", &diag)
				}
				for relative, original := range originals {
					if raw, err := os.ReadFile(filepath.Join(secondWorker, relative)); err != nil || !bytes.Equal(raw, original) {
						t.Fatalf("cleanup touched original before Impact preparation: %s %v", relative, err)
					}
				}
				if raw, err := os.ReadFile(checkCount); err != nil || string(raw) != "check" {
					t.Fatalf("Main check missing/repeated: %q %v", raw, err)
				}
				beforePrepare := repairDispatchRuntimeBytes(t, root)
				if err := os.Remove(impactTarget); err != nil {
					t.Fatal(err)
				}
				diag.Reset()
				out.Reset()
				if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &out, &diag); code != 0 {
					t.Fatalf("first Impact preparation recovery: %s", &diag)
				}
				repairDispatchAssertUnchanged(t, root, beforePrepare)
				var impact repair.ChangeImpactRequest
				readRepairLeadJSON(t, impactTarget, &impact)
				required, err := repairImpactReverificationIDs(after.State)
				// The system-issued Impact identity is the responsibility key of the
				// Session, not the legacy dispatch slug form
				// (internal/cli/repair_lead_questionnaire.go:1158 keeps
				// "impact-"+dispatchSlug as the legacy identity only).
				if err != nil || impact.ImpactID != "impact-"+repairResponsibilityKey(stringValue(repairPointerCLI(after.State)["session_id"])) || impact.RuntimeID != stringValue(after.State["runtime_id"]) || len(impact.RequiredReverificationIDs) != len(required) || len(required) == 0 {
					t.Fatalf("recovered Impact lost original responsibility: %+v %v", impact, err)
				}
				for i, id := range required {
					if impact.RequiredReverificationIDs[i] != id {
						t.Fatal("Impact changed required independent reverification")
					}
				}
				// Preserve real professional draft content through both prepare and
				// integration reentry; neither is another answer producer.
				impact.ChangeTypes = []string{"test"}
				if err := persistRepairLeadQuestionnaire(impactTarget, impact); err != nil {
					t.Fatal(err)
				}
				recoveredImpact, err = os.ReadFile(impactTarget)
				if err != nil {
					t.Fatal(err)
				}
				if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &out, &diag); code != 0 {
					t.Fatalf("existing professional Impact prepare: %s", &diag)
				}
				repairDispatchAssertUnchanged(t, root, beforePrepare)
				recoveredCP, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, second))
				if err != nil || !found || recoveredCP.State != integration.StateAcknowledged || recoveredCP.VerifiedAt != cp.VerifiedAt {
					t.Fatalf("prepare changed ACK: %+v %v", recoveredCP, err)
				}
				diag.Reset()
				out.Reset()
				code = runWorkspaceDelivery(secondArgs, &out, &diag)
				repairDispatchAssertUnchanged(t, root, beforePrepare)
				if raw, err := os.ReadFile(checkCount); err != nil || string(raw) != "check" {
					t.Fatalf("Impact recovery repeated Main checks: %q %v", raw, err)
				}
				if raw, err := os.ReadFile(impactTarget); err != nil || !bytes.Equal(raw, recoveredImpact) {
					t.Fatal("Impact recovery overwrote professional draft")
				}
				nowFinals := repairArtifactRefs(repairPointerCLI(repairDispatchSnapshot(t, root).State), "result_refs", "result_ref")
				if len(nowFinals) != len(finals) || nowFinals[0] != finals[0] || nowFinals[1] != finals[1] {
					t.Fatal("Impact recovery replaced Final")
				}
			}
			cp, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, second))
			if strings.HasPrefix(variant, "view-") {
				// The issued view was mutated while the required check ran, so
				// the required check failed. The merge is recorded, the
				// verification is not, and the Builder checkout is preserved for
				// Main to inspect; the integration is NOT a verified one with a
				// pending cleanup (blueprint L4-workflow-convergence.md:302
				// "未知、未跟踪、忽略或私人材料保留"; the failed required check is
				// reported as a failure, not as a pending cleanup).
				if code == 0 {
					t.Fatalf("failed required check was reported as success: %d %v %s", code, finals, &diag)
				}
				if len(finals) != 1 {
					t.Fatalf("failed required check still produced a Final: %v %s", finals, &diag)
				}
				if err != nil || !found || cp.State != integration.StatePreserved {
					t.Fatalf("failed required check did not keep the preserved checkpoint: %+v %v", cp, err)
				}
				if !strings.Contains(diag.String(), "integration required check failed") || !strings.Contains(diag.String(), "worktree preserved") {
					t.Fatalf("failed required check was not reported with the preserved checkout: %s", &diag)
				}
			} else {
				if code == 0 || len(finals) != 2 || stringValue(repairPointerCLI(after.State)["status"]) != "impact_reconciliation" {
					t.Fatalf("expected actual last Final with independent cleanup_pending: %d %v %s", code, finals, &diag)
				}
				if err != nil || !found || cp.State != integration.StateCleanupPending {
					t.Fatalf("missing original cleanup checkpoint: %+v %v", cp, err)
				}
			}
			if _, err := os.Stat(filepath.Join(secondWorker, "unreceived-ignored.txt")); err != nil {
				t.Fatal("unknown private content lost")
			}
			if strings.HasPrefix(variant, "view-") {
				// The issued view was mutated while the required check ran, so the
				// required check itself failed. The system reports that failure
				// and keeps the Builder checkout; it does not have to name the
				// mutated file itself, because the check contract and its log
				// carry that detail (blueprint
				// L4-workflow-convergence.md:302 keeps unknown material; a
				// failed required check is a failure, not a pending cleanup).
				if !strings.Contains(diag.String(), "integration required check failed") || !strings.Contains(diag.String(), "worktree preserved") {
					t.Fatalf("issued view mutation was not reported as a failed required check: %s", &diag)
				}
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
				// The accepted Final artifacts before the cleanup retry. The
				// cleanup is a resource state; it must not rewrite the
				// professional material that was already accepted.
				acceptedFinals := map[string][]byte{}
				for _, ref := range finals {
					raw, readErr := os.ReadFile(resolveRootPath(root, ref.Path))
					if readErr != nil {
						t.Fatalf("accepted Final artifact unavailable before the cleanup retry: %s %v", ref.Path, readErr)
					}
					acceptedFinals[ref.Path] = raw
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
				retryCode := runWorkspaceDelivery(secondArgs, &out, &diag)
				if retryCode != 0 && !strings.Contains(diag.String(), "complete and save ChangeImpact") {
					t.Fatalf("original independent cleanup retry after owner resolution: %d %s", retryCode, &diag)
				}
				recovered, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, second))
				// The original public cleanup retry is independent: once the
				// unknown material has an owner and is resolved, the cleanup
				// completes on its own, without repeating the Main checks,
				// re-accepting a Final, rewriting the Impact questionnaire or
				// touching the preserved private bytes. The verified facts and
				// the acknowledgement stay exactly as they were.
				//
				// The cleanup is a resource state, not an integration failure and
				// not an author action to buy a stage pass
				// (L4-workflow-convergence.md:302/435). The retained material
				// keeps the cleanup pending, and the pending reason is bound to
				// that retained material only: a missing original Plan is a later
				// Main-material issue and must not hold this cleanup
				// (L4-workflow-convergence.md:302 — cleanup_pending 只与保留材料绑定;
				// L3-S9-repair.md:271 — Main 可准备原 ChangeImpact 而不使其成为完成门).
				// The accepted Final stays accepted either way.
				if err != nil || !found {
					t.Fatalf("cleanup recovery lost the original checkpoint: %+v %v", recovered, err)
				}
				if recovered.State != integration.StateComplete && recovered.State != integration.StateCleanupPending {
					t.Fatalf("cleanup recovery replaced verified facts: %+v %v", recovered, err)
				}
				if recovered.VerifiedAt != cp.VerifiedAt {
					t.Fatalf("cleanup recovery replaced verified facts: %+v %v", recovered, err)
				}
				if recovered.State == integration.StateCleanupPending {
					reason := recovered.FailureReason + " " + strings.Join(recovered.Blockers, " ")
					// The pending reason must name the retained material. It must
					// not be held by a missing original Plan, and it must not be
					// held by the outstanding ChangeImpact answer.
					for _, missing := range []string{"no such file", "not found", "missing", "unavailable", "does not exist"} {
						if strings.Contains(strings.ToLower(reason), missing) {
							t.Fatalf("cleanup was held by an unavailable source (%q) instead of the retained material: %+v", missing, recovered)
						}
					}
					if strings.Contains(reason, "ChangeImpact") {
						t.Fatalf("cleanup was held by the outstanding ChangeImpact answer instead of the retained material: %+v", recovered)
					}
					if !strings.Contains(reason, "worktree") {
						t.Fatalf("cleanup_pending did not name the retained material: %+v", recovered)
					}
					// The retained worktree must be reported and must still be
					// there: the private answer material was not deleted to fake
					// a completion.
					if recovered.WorktreePath == "" {
						t.Fatalf("cleanup_pending did not report the retained worktree: %+v", recovered)
					}
					if _, statErr := os.Stat(recovered.WorktreePath); statErr != nil {
						t.Fatalf("cleanup_pending did not retain the author's worktree: %v", statErr)
					}
				}
				// The worktree itself stays, and the private answer material in it
				// is byte-identical: nothing was deleted to fake a completion.
				if _, err := os.Stat(secondWorker); err != nil {
					t.Fatalf("the retained worktree was removed to fake a completion: %v", err)
				}
				for relative, original := range originals {
					if raw, readErr := os.ReadFile(filepath.Join(secondWorker, relative)); readErr != nil || !bytes.Equal(raw, original) {
						t.Fatalf("cleanup changed the retained private answer %s: %v", relative, readErr)
					}
				}
				// The already accepted Final keeps its bytes.
				for path, original := range acceptedFinals {
					raw, readErr := os.ReadFile(resolveRootPath(root, path))
					if readErr != nil || !bytes.Equal(raw, original) {
						t.Fatalf("cleanup retry changed the accepted Final %s: %v", path, readErr)
					}
				}
				if body, err := os.ReadFile(checkCount); err != nil || string(body) != "check" {
					t.Fatalf("cleanup retry reran original Main checks: %q %v", body, err)
				}
				if body, err := os.ReadFile(impactPath); err != nil || !bytes.Equal(body, originalImpact) {
					t.Fatal("cleanup retry rewrote Impact questionnaire")
				}
				nowFinals := repairArtifactRefs(repairPointerCLI(repairDispatchSnapshot(t, root).State), "result_refs", "result_ref")
				if len(nowFinals) != len(finals) || nowFinals[0] != finals[0] || nowFinals[1] != finals[1] {
					t.Fatal("cleanup retry reaccepted/replaced Final")
				}
				if body, err := os.ReadFile(resolved); err != nil || !bytes.Equal(body, unknownBytes) {
					t.Fatal("owner-preserved private bytes changed")
				}
				t.Log("Original public cleanup retry completed independently after synthetic owner resolution; no repeated Main checks, Final, Impact or author submission; VerifiedAt unchanged")
			}
			// A failed required check never prepared the ChangeImpact, so there
			// is no original Impact to recover; the recovery entry point must
			// report that honestly instead of preparing one.
			if !strings.HasPrefix(variant, "view-") {
				beforeRuntime := repairDispatchRuntimeBytes(t, root)
				diag.Reset()
				out.Reset()
				if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &out, &diag); code != 0 {
					t.Fatalf("original public Impact preparation recovery: %s", &diag)
				}
				repairDispatchAssertUnchanged(t, root, beforeRuntime)
				t.Log("Original public impact prepare recovery/idempotence succeeds without resubmitting Final or author work; Runtime unchanged")
			}
		})
	}
}
