package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
)

// Stop the actual original state machine after Git merged but before the
// merged checkpoint write. The existing storage clock is test injection only;
// no production callback, fake ready record or state rewind is introduced.
func loseMergeReceiptBeforeWrite(t *testing.T, inspection integration.Inspection, cfg integration.IntegrateConfig, slot string) integration.Checkpoint {
	t.Helper()
	const interrupted = "fixture interruption before merged checkpoint write"
	writes := 0
	store := &integration.CheckpointStore{Clock: func() time.Time {
		writes++
		if writes == 2 {
			panic(interrupted)
		}
		return time.Now().UTC()
	}, NowString: func(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }}
	cfg.CheckpointStore = store
	caught := false
	func() {
		defer func() {
			if p := recover(); p != nil {
				if p != interrupted {
					panic(p)
				}
				caught = true
			}
		}()
		_, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, cfg)
		t.Fatalf("did not interrupt the actual merge/receipt window: %v", err)
	}()
	checkpoint, found, err := store.Load(slot)
	if !caught || err != nil || !found || checkpoint.State != integration.StateReady || checkpoint.MergeCommit != "" {
		t.Fatalf("actual interrupted checkpoint: %+v found=%t err=%v", checkpoint, found, err)
	}
	merge := readGit(t, cfg.Root, "rev-parse", "HEAD")
	assertTwoParentMerge(t, cfg.Root, merge, checkpoint.TargetHead, checkpoint.SourceHead)
	return checkpoint
}

func TestReadyMergeResponseLossRecoversAfterRealMainDescendants(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		for _, mode := range []string{"checkpoint-inspection", "fresh-inspection", "dirty-worker"} {
			t.Run(map[bool]string{false: "generic", true: "canonical-record"}[canonical]+"/"+mode, func(t *testing.T) {
				var root, worker, slot string
				var inspection integration.Inspection
				var cfg integration.IntegrateConfig
				var fixture *canonicalMainFixture
				checks := 0
				if canonical {
					fixture = newCanonicalMainFixture(t, canonicalSourceFile)
					root, worker = fixture.root, fixture.worker
					inspection, cfg = fixture.inspect(t, ""), fixture.integrateConfig()
					slot = integration.DefaultCheckpointStore().PathForRecord(root, fixture.record.Reference())
				} else {
					root, worker = t.TempDir(), filepath.Join(t.TempDir(), "worker")
					gitFixture(t, root, "init", "-b", "main")
					gitFixture(t, root, "config", "user.name", "Response Loss Fixture")
					gitFixture(t, root, "config", "user.email", "response-loss@example.invalid")
					gitFixture(t, root, "config", "commit.gpgsign", "false")
					writeFixtureFile(t, root, "base.txt", []byte("base\n"))
					gitFixture(t, root, "add", ".")
					gitFixture(t, root, "commit", "-m", "fixture baseline")
					gitFixture(t, root, "worktree", "add", "-b", "worker", worker, "HEAD")
					writeFixtureFile(t, worker, "change.txt", []byte("actual synthetic contribution\n"))
					gitFixture(t, worker, "add", "change.txt")
					gitFixture(t, worker, "commit", "-m", "fixture contribution")
					var err error
					inspection, err = integration.Inspect(context.Background(), integration.InspectRequest{Root: root, Assignment: hookctx.AssignmentContext{AssignmentID: "assignment-response-loss", WorktreePath: worker, Branch: "worker", TargetBranch: "main"}, BaselineGeneration: 1}, integration.InspectConfig{SkipCompletionCheck: true})
					if err != nil || !inspection.Ready {
						t.Fatalf("original Inspect: %+v %v", inspection, err)
					}
					slot = filepath.Join(t.TempDir(), "checkpoint.json")
					cfg = integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: "loop-response-loss", CheckpointDir: slot, RequiredChecks: []string{"test -f change.txt && test -f later.txt"}, CheckRunner: func(ctx context.Context, dir, command string) error {
						checks++
						return integration.CommandCheckRunner(ctx, dir, command)
					}}
				}
				if !inspection.Ready {
					t.Fatalf("fixture is not ready: %+v", inspection)
				}
				checkpoint := loseMergeReceiptBeforeWrite(t, inspection, cfg, slot)
				merge := readGit(t, root, "rev-parse", "HEAD")
				writeFixtureFile(t, root, "later.txt", []byte("later actual Main contribution\n"))
				gitFixture(t, root, "add", "later.txt")
				gitFixture(t, root, "commit", "-m", "later Main contribution")
				head, count := readGit(t, root, "rev-parse", "HEAD"), readGit(t, root, "rev-list", "--all", "--count")
				recovery := integration.InspectionFromCheckpoint(checkpoint)
				if mode == "fresh-inspection" || mode == "dirty-worker" {
					if mode == "dirty-worker" {
						writeFixtureFile(t, worker, "unreceived.txt", []byte("preserve actual unreceived Worker work\n"))
					}
					if canonical {
						recovery = fixture.inspect(t, "")
					} else {
						var err error
						recovery, err = integration.Inspect(context.Background(), integration.InspectRequest{Root: root, Assignment: inspection.AssignmentProjection, BaselineGeneration: inspection.BaselineGeneration, RuntimeID: cfg.RuntimeID}, integration.InspectConfig{SkipCompletionCheck: true})
						if err != nil {
							t.Fatal(err)
						}
					}
					if recovery.Ready || len(recovery.Blockers) == 0 {
						t.Fatalf("fresh post-merge inspection did not expose its real readiness cause: %+v", recovery)
					}
				}
				result, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: recovery}, cfg)
				if mode == "dirty-worker" {
					if result.Checkpoint.State != integration.StateReady || result.Checkpoint.MergeCommit != "" || checks != 0 || canonical && fileSize(t, fixture.counterPath) != 0 || readGit(t, root, "rev-parse", "HEAD") != head {
						t.Fatalf("finding a merge bypassed actual dirty Worker readiness: %+v %v", result, err)
					}
					if _, err := os.Stat(filepath.Join(worker, "unreceived.txt")); err != nil {
						t.Fatalf("dirty recovery removed unreceived output: %v", err)
					}
					return
				}
				if err != nil || result.Checkpoint.State != integration.StateVerified || result.Checkpoint.MergeCommit != merge || result.Checkpoint.TestedHead != head {
					t.Fatalf("actual ready response-loss recovery after target advanced: %+v %v", result, err)
				}
				if canonical {
					if fileSize(t, fixture.counterPath) != 1 {
						t.Fatal("canonical RequiredCheck did not execute once on current Main")
					}
					facts := result.Checkpoint.CanonicalMainFacts
					if facts == nil || facts.Binding.AssignmentRecord != fixture.record.Reference() || facts.Binding.AssignmentRecordSHA256 != mustRecordDigest(t, fixture.record) || facts.SourceHead != inspection.SourceHead || facts.MergeBase != inspection.MergeBase || facts.MergeCommit != merge || facts.TestedHead != head || len(facts.ChangedPaths) != 1 || facts.ChangedPaths[0] != canonicalSourceFile || len(facts.Checks) != 1 || facts.Checks[0].TestedHead != head || facts.Checks[0].Command != fixture.command {
						t.Fatalf("canonical recovery changed its source/Record scope or failed to check current Main: %+v", facts)
					}
					assertCanonicalQualitygateState(t, fixture, true, qualitygate.StatusSatisfied)
				} else if checks != 1 {
					t.Fatalf("actual Main checks executed %d times", checks)
				}
				again, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: integration.InspectionFromCheckpoint(result.Checkpoint), Acknowledge: true, Cleanup: true}, cfg)
				if err != nil || again.Checkpoint.State != integration.StateComplete || again.Checkpoint.MergeCommit != merge || !strings.EqualFold(readGit(t, root, "rev-parse", "HEAD"), head) || readGit(t, root, "rev-list", "--all", "--count") != count {
					t.Fatalf("response-loss cleanup repeated merge/work: %+v %v", again, err)
				}
				if _, err := os.Stat(worker); !os.IsNotExist(err) {
					t.Fatalf("original cleanup did not finish: %v", err)
				}
			})
		}
	}
}
