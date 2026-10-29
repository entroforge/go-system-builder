package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// This barrier belongs to the test process only. It stops at an observed,
// published artifact while the original Writer still owns its lock. Unlike a
// panic, killing this process executes none of the consumer's deferred cleanup.
type resultProcessBarrier struct {
	context.Context
	artifact, ready string
}

func (c resultProcessBarrier) Err() error {
	if _, err := os.Stat(c.artifact); err == nil {
		if err := os.WriteFile(c.ready, []byte("artifact published; Writer has not returned\n"), 0600); err != nil {
			panic(err)
		}
		select {}
	}
	return c.Context.Err()
}

type resultProcessRequest struct {
	Root, State, Journal, Input, Artifact, Ready, Outcome string
	Revision                                              int
}

func TestReviewResultProcessHelper(t *testing.T) {
	mode := os.Getenv("GSB_RESULT_PROCESS_MODE")
	if mode == "" {
		return
	}
	var in resultProcessRequest
	if err := json.Unmarshal([]byte(os.Getenv("GSB_RESULT_PROCESS_REQUEST")), &in); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var work context.Context = ctx
	if mode == "barrier" {
		work = resultProcessBarrier{Context: ctx, artifact: in.Artifact, ready: in.Ready}
	}
	got, err := SubmitResult(in.Root, in.State, in.Journal, SubmitRequest{
		Context: work, ExpectedRevision: in.Revision, AssignmentID: "assignment-qa-1", ResultPath: in.Input,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"revision": got.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in.Outcome, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func resultProcessCommand(t *testing.T, mode string, request resultProcessRequest) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestReviewResultProcessHelper$", "-test.timeout=30s", "-test.v")
	command.Env = append(os.Environ(), "GSB_RESULT_PROCESS_MODE="+mode, "GSB_RESULT_PROCESS_REQUEST="+string(data))
	return command
}

func TestReviewResultKilledAfterPublicationRecoversInFreshProcess(t *testing.T) {
	for _, mode := range []string{"original-answer", "tampered-artifact", "missing-intent"} {
		t.Run(mode, func(t *testing.T) {
			root, sp, jp, before, plan := packetModeFixture(t)
			const id = "review-result-real-process-kill"
			input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			artifact := filepath.Join(root, ".claude", "evidence", before.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
			in := resultProcessRequest{Root: root, State: sp, Journal: jp, Input: input, Artifact: artifact, Ready: filepath.Join(t.TempDir(), "ready"), Outcome: filepath.Join(t.TempDir(), "outcome.json"), Revision: before.Revision}
			stateBytes, journalBytes := mustReadTestFile(t, sp), mustReadTestFile(t, jp)
			child := resultProcessCommand(t, "barrier", in)
			var diagnostic bytes.Buffer
			child.Stdout, child.Stderr = &diagnostic, &diagnostic
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			reaped := false
			t.Cleanup(func() {
				if !reaped {
					_ = child.Process.Kill()
					_ = child.Wait()
				}
			})
			deadline := time.NewTimer(15 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
		wait:
			for {
				if _, err := os.Stat(in.Ready); err == nil {
					break
				}
				select {
				case <-deadline.C:
					t.Fatal("child did not reach artifact publication barrier")
				case <-ticker.C:
				case <-t.Context().Done():
					break wait
				}
			}
			published := mustReadTestFile(t, artifact)
			if _, err := os.Stat(artifact + ".staging.json"); err != nil {
				t.Fatalf("publication lacks original ownership intent: %v", err)
			}
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("barrier child exited normally instead of being killed")
			}
			reaped = true
			// Prove the actual process-owned lock was released by death. A
			// remaining compatibility sentinel must not be mistaken for a live
			// Writer or for an artifact-ownership rejection.
			lockContext, lockCancel := context.WithTimeout(t.Context(), time.Second)
			release, lockErr := filelock.Acquire(lockContext, sp+".lock.process")
			lockCancel()
			if lockErr != nil {
				t.Fatalf("killed process still owns OS lock: %v", lockErr)
			}
			release()
			if !bytes.Equal(stateBytes, mustReadTestFile(t, sp)) || !bytes.Equal(journalBytes, mustReadTestFile(t, jp)) {
				t.Fatal("pre-pending kill published reception")
			}
			if _, err := os.Stat(sp + ".commit-pending.json"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("wrong injection window: pending exists: %v", err)
			}
			switch mode {
			case "tampered-artifact":
				if err := os.WriteFile(artifact, append(published, ' '), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-intent":
				if err := os.Remove(artifact + ".staging.json"); err != nil {
					t.Fatal(err)
				}
			}
			ageSentinel := func() {
				t.Helper()
				// Negative ownership cases isolate that boundary from the
				// separately asserted immediate-recovery requirement. This is a
				// test-only time fixture, never an author recovery instruction.
				old := time.Now().Add(-time.Minute)
				if err := os.Chtimes(sp+".lock", old, old); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "original-answer" {
				ageSentinel()
			}
			currentArtifact := mustReadTestFile(t, artifact)
			for replay := 0; replay < 2; replay++ {
				output, err := resultProcessCommand(t, "resume", in).CombinedOutput()
				if mode != "original-answer" {
					if err == nil {
						t.Fatalf("unsafe %s accepted: %s", mode, output)
					}
					want := "bytes drifted"
					if mode == "missing-intent" {
						want = "without this submission's ownership intent"
					}
					if !strings.Contains(string(output), want) {
						t.Fatalf("negative recovery rejected for the wrong boundary; want %q: %s", want, output)
					}
					if !bytes.Equal(stateBytes, mustReadTestFile(t, sp)) || !bytes.Equal(journalBytes, mustReadTestFile(t, jp)) || !bytes.Equal(currentArtifact, mustReadTestFile(t, artifact)) {
						t.Fatal("failed recovery changed original facts or untrusted artifact")
					}
					break
				}
				if err != nil {
					t.Errorf("fresh process cannot immediately resume original transaction: %v\n%s", err, output)
					if !strings.Contains(string(output), "context deadline exceeded") && !strings.Contains(string(output), "runtime lock timeout") {
						return
					}
					ageSentinel()
					output, err = resultProcessCommand(t, "resume", in).CombinedOutput()
					if err != nil {
						t.Fatalf("control with expired compatibility sentinel also failed: %v\n%s", err, output)
					}
					t.Log("CONTROL ONLY: original answer recovered after aging the dead compatibility sentinel; immediate recovery above remains FAILED")
				}
				got, err := loopruntime.NewStore(sp, jp).Snapshot()
				if err != nil || got.Revision != before.Revision+1 {
					t.Fatalf("reception not exactly once after replay %d: revision=%d error=%v", replay, got.Revision, err)
				}
				if !bytes.Equal(published, mustReadTestFile(t, artifact)) {
					t.Fatal("recovery replaced exact original artifact")
				}
			}
			t.Log(fmt.Sprintf("actual process kill and fresh-process recovery checked: %s", mode))
		})
	}
}
