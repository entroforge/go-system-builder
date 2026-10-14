package runtime_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

type killedArtifactBoundary struct {
	root string
	step runtime.CommitFailureStep
}

func (b killedArtifactBoundary) Inject(step runtime.CommitFailureStep) error {
	if step != b.step {
		return nil
	}
	if err := os.WriteFile(filepath.Join(b.root, "child-ready"), []byte(step), 0600); err != nil {
		return err
	}
	for {
		time.Sleep(time.Hour)
	}
}

func processArtifactMutation(bundle string) runtime.Mutation {
	paths := []string{".claude/evidence/process-result.json", ".claude/review/plans/process-plan.json"}
	if bundle == "s9" {
		paths = []string{".claude/review/repair/process/session.json", ".claude/review/repair/process/result.json"}
	}
	m := artifactMutation(paths...)
	m.Operation = &runtime.Operation{ID: "process-artifact-operation", InputSHA256: sha256HexForTest([]byte(bundle)), RuntimeID: m.RuntimeID, Actor: m.Actor, Kind: m.TransitionID}
	return m
}

func TestArtifactCommitBoundaryChild(t *testing.T) {
	root := os.Getenv("GSB_ARTIFACT_COMMIT_CHILD_ROOT")
	if root == "" {
		t.Skip("helper for real SIGKILL transaction test")
	}
	store := testWriter(filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")).WithCommitFailureInjector(killedArtifactBoundary{root: root, step: runtime.CommitFailureStep(os.Getenv("GSB_ARTIFACT_COMMIT_STEP"))})
	_, err := store.Update(1, processArtifactMutation(os.Getenv("GSB_ARTIFACT_COMMIT_BUNDLE")))
	t.Fatalf("child returned without being killed at its commit boundary: %v", err)
}

// Exercise the real Runtime 1.1 writer and both existing artifact pending
// formats. These are storage-boundary tests, not a claim of running the entire
// S7/Repair domain or the future browser producer through public CLI.
func TestArtifactSIGKILLRecoveryAfterCommitBoundary(t *testing.T) {
	steps := []runtime.CommitFailureStep{runtime.CommitBeforePendingMarker, runtime.CommitAfterPendingMarker, runtime.CommitAfterArtifactPublish, runtime.CommitAfterStateWrite, runtime.CommitAfterJournalAppend, runtime.CommitAfterMarkerClear}
	for _, bundle := range []string{"s7", "s9"} {
		for _, step := range steps {
			t.Run(bundle+"/"+string(step), func(t *testing.T) {
				root := t.TempDir()
				sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
				writeState(t, sp, 1)
				if err := os.WriteFile(jp, nil, 0600); err != nil {
					t.Fatal(err)
				}
				m := processArtifactMutation(bundle)
				binary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(binary, "-test.run=^TestArtifactCommitBoundaryChild$")
				cmd.Env = append(os.Environ(), "GSB_ARTIFACT_COMMIT_CHILD_ROOT="+root, "GSB_ARTIFACT_COMMIT_STEP="+string(step), "GSB_ARTIFACT_COMMIT_BUNDLE="+bundle)
				var output bytes.Buffer
				cmd.Stdout, cmd.Stderr = &output, &output
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan struct{})
				var childErr error
				go func() { childErr = cmd.Wait(); close(done) }()
				t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
				deadline := time.NewTimer(10 * time.Second)
				defer deadline.Stop()
				tick := time.NewTicker(10 * time.Millisecond)
				defer tick.Stop()
				for {
					if ready, err := os.ReadFile(filepath.Join(root, "child-ready")); err == nil {
						if string(ready) != string(step) {
							t.Fatalf("wrong boundary: %s", ready)
						}
						break
					}
					select {
					case <-done:
						t.Fatalf("child exited before boundary: %v %s", childErr, output.String())
					case <-deadline.C:
						t.Fatal("child did not reach requested commit boundary")
					case <-tick.C:
					}
				}
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				<-done
				var exited *exec.ExitError
				if !errors.As(childErr, &exited) {
					t.Fatalf("expected actual SIGKILL: %v", childErr)
				}
				status, ok := exited.Sys().(syscall.WaitStatus)
				if !ok || status.Signal() != syscall.SIGKILL {
					t.Fatalf("wrong signal: %v", childErr)
				}
				privateBefore, _ := filepath.Glob(filepath.Join(root, ".claude/operations/staging/*/*.data"))
				store := testWriter(sp, jp)
				if _, err := store.RecoverPendingOperations(); err != nil {
					t.Fatalf("recover after actual SIGKILL: %v", err)
				}
				inspection, err := runtime.NewStore(sp, jp).InspectOperation(root, m.Operation.ID)
				if err != nil {
					t.Fatal(err)
				}
				want := "committed"
				if step == runtime.CommitBeforePendingMarker {
					want = "not_found"
				}
				if inspection.Status != want {
					t.Fatalf("status=%+v want=%s", inspection, want)
				}
				if want == "not_found" {
					for _, a := range m.Artifacts {
						if _, err := os.Stat(filepath.Join(root, a.Path)); !errors.Is(err, os.ErrNotExist) {
							t.Fatal("artifact appeared before durable pending")
						}
					}
					for _, p := range privateBefore {
						if _, err := os.Stat(p); err != nil {
							t.Fatal("recovery guessed that abandoned private staging was garbage")
						}
					}
				} else {
					for _, a := range m.Artifacts {
						if !bytes.Equal(a.Data, mustRead(t, filepath.Join(root, a.Path))) {
							t.Fatal("committed artifact changed")
						}
					}
					m.Apply = func(map[string]any) error { t.Fatal("response-loss retry applied its mutation twice"); return nil }
				}
				retried, err := store.Update(1, m)
				if err != nil || retried.Operation == nil || retried.OperationReplayed != (want == "committed") {
					t.Fatalf("same-operation retry: %+v %v", retried, err)
				}
				state, journal := mustRead(t, sp), mustRead(t, jp)
				if _, err := store.RecoverPendingOperations(); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(state, mustRead(t, sp)) || !bytes.Equal(journal, mustRead(t, jp)) {
					t.Fatal("repeated recovery duplicated a commit")
				}
				m.Operation.InputSHA256 = strings.Repeat("e", 64)
				if _, err := store.Update(2, m); !errors.Is(err, runtime.ErrOperationConflict) {
					t.Fatalf("changed-input replay accepted: %v", err)
				}
				if _, err := os.Stat(sp + ".lock.process"); err != nil {
					t.Fatal("persistent OS-lock inode removed")
				}
				t.Logf("actual_signal=SIGKILL bundle=%s boundary=%s recovered_status=%s no_duplicate_commit=true", bundle, step, want)
			})
		}
	}
}
