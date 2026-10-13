package review

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// Hold the real process lock until RegisterPlan has finished preparing its
// immutable artifact. This places the change strictly between preflight and
// commit, without production test hooks or timing assumptions about hashing.
func TestRegisterPlanRechecksPreparedInputsUnderCAS(t *testing.T) {
	for _, variant := range []string{"runtime_revision", "subject_bytes"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			state := baseVerificationState()
			statePath, journalPath := writeState(t, root, state)
			if err := os.WriteFile(journalPath, nil, 0644); err != nil {
				t.Fatal(err)
			}
			planPath := writePlanFile(t, root)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			release, err := filelock.Acquire(ctx, statePath+".lock.process")
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			done := make(chan error, 1)
			go func() {
				_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath})
				done <- err
			}()
			staged := filepath.Join(root, ".claude/operations/staging/*/*.data")
			for {
				if matches, err := filepath.Glob(staged); err == nil && len(matches) > 0 {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("registration ended before its commit boundary: %v", err)
				case <-ctx.Done():
					t.Fatal("registration did not reach its commit boundary")
				case <-time.After(5 * time.Millisecond):
				}
			}
			if variant == "runtime_revision" {
				// Stand in for the winning writer while owning its process lock.
				state["revision"] = 2
				writeState(t, root, state)
			} else if err := os.WriteFile(filepath.Join(root, "internal/example/service.go"), []byte("changed after preflight"), 0644); err != nil {
				t.Fatal(err)
			}
			beforeState, _ := os.ReadFile(statePath)
			beforeJournal, _ := os.ReadFile(journalPath)
			release()
			err = <-done
			if variant == "runtime_revision" && !errors.Is(err, loopruntime.ErrStaleRevision) {
				t.Fatalf("omitted CLI revision bypassed CAS: %v", err)
			}
			if variant == "subject_bytes" && (err == nil || !strings.Contains(err.Error(), "frozen")) {
				t.Fatalf("changed source crossed commit boundary: %v", err)
			}
			afterState, _ := os.ReadFile(statePath)
			afterJournal, _ := os.ReadFile(journalPath)
			if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
				t.Fatal("rejected stale proposal changed Runtime")
			}
		})
	}
}
