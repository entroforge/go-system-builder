package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestRegisterPlanOperationRetryAndConflict(t *testing.T) {
	root := t.TempDir()
	sp, jp := writeState(t, root, baseVerificationState())
	req := PlanRequest{ExpectedRevision: 1, PlanPath: writePlanFile(t, root), OperationID: "register-plan-1"}
	first, err := RegisterPlan(root, sp, jp, req)
	if err != nil || first.Operation == nil {
		t.Fatalf("first: %+v %v", first, err)
	}
	stateBefore, journalBefore := readTestFile(t, sp), readTestFile(t, jp)
	retry, err := RegisterPlan(root, sp, jp, req)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(first.Operation)
	two, _ := json.Marshal(retry.Operation)
	if !bytes.Equal(one, two) {
		t.Fatal("retry did not return the original receipt")
	}
	if !bytes.Equal(stateBefore, readTestFile(t, sp)) || !bytes.Equal(journalBefore, readTestFile(t, jp)) {
		t.Fatal("response-loss retry changed Runtime")
	}
	var plan Plan
	if err := json.Unmarshal(readTestFile(t, req.PlanPath), &plan); err != nil {
		t.Fatal(err)
	}
	plan.Claims[0].Oracle = "different authority assertion"
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(req.PlanPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPlan(root, sp, jp, req); !errors.Is(err, loopruntime.ErrOperationConflict) {
		t.Fatalf("changed input reused operation identity: %v", err)
	}
}

func TestConcurrentIdenticalPlanOperationsPublishOnce(t *testing.T) {
	root := t.TempDir()
	sp, jp := writeState(t, root, baseVerificationState())
	req := PlanRequest{ExpectedRevision: 1, PlanPath: writePlanFile(t, root), OperationID: "register-concurrent"}
	// Let both callers pass the initial read-only lookup, then contend in
	// their normal writer path. No caller is allowed to remove the winner.
	done := make(chan struct {
		snapshot loopruntime.Snapshot
		err      error
	}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			snapshot, err := RegisterPlan(root, sp, jp, req)
			done <- struct {
				snapshot loopruntime.Snapshot
				err      error
			}{snapshot, err}
		}()
	}
	var original []byte
	for i := 0; i < 2; i++ {
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		data, _ := json.Marshal(r.snapshot.Operation)
		if i == 0 {
			original = data
		} else if !bytes.Equal(data, original) {
			t.Fatal("concurrent retries returned different receipts")
		}
	}
	if bytes.Count(readTestFile(t, jp), []byte("\n")) != 1 {
		t.Fatal("concurrent operation consumed twice")
	}
	var state map[string]any
	if err := json.Unmarshal(readTestFile(t, sp), &state); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadPlan(root, state); err != nil {
		t.Fatalf("losing retry deleted winner's artifact: %v", err)
	}
}

func TestConcurrentDifferentPlanOperationsPreserveWinner(t *testing.T) {
	root := t.TempDir()
	sp, jp := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, sp+".lock.process")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := RegisterPlan(root, sp, jp, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
			done <- err
		}()
	}
	for {
		staged, _ := filepath.Glob(filepath.Join(root, ".claude/operations/staging/*/*.data"))
		if len(staged) == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both writers did not prepare")
		case err := <-done:
			t.Fatalf("writer ended early: %v", err)
		case <-time.After(5 * time.Millisecond):
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/review/plans/review-plan-t-1.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published before acquiring commit lock")
	}
	release()
	wins, stale := 0, 0
	for i := 0; i < 2; i++ {
		err := <-done
		if err == nil {
			wins++
		} else if errors.Is(err, loopruntime.ErrStaleRevision) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || stale != 1 {
		t.Fatalf("wins=%d stale=%d", wins, stale)
	}
	var state map[string]any
	if err := json.Unmarshal(readTestFile(t, sp), &state); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadPlan(root, state); err != nil {
		t.Fatalf("losing CAS deleted winner: %v", err)
	}
}

func readTestFile(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestResultOperationRetryPreservesConsumptionAndBlockedOutcome(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "consumed", true: "site_lost"}[blocked], func(t *testing.T) {
			root := t.TempDir()
			sp, jp := writeState(t, root, baseVerificationState())
			revision, plan := dispatchedFixture(t, root, sp, jp)
			verdict := "pass"
			claims := map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}
			var findings []Finding
			if blocked {
				verdict = "finding"
				claims["claim-qa-1"] = "fail"
				finding := codeInspectionFinding("finding-op-1", "claim-qa-1")
				finding.Encounter.LastGoodCheckpoint = ""
				findings = []Finding{finding}
			}
			resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-operation-qa-1", "agent-qa-1", verdict, claims, findings)
			if blocked {
				patchResultField(t, resultPath, "site_lost", []any{map[string]any{"finding_id": "finding-op-1", "reason": "container and failure logs are gone; scene cannot be recovered"}})
			}
			req := SubmitRequest{ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath, OperationID: "qa-operation-1"}
			first, err := SubmitResult(root, sp, jp, req)
			var blockedErr *SiteLostBlockedError
			if blocked && !errors.As(err, &blockedErr) || !blocked && err != nil || first.Operation == nil {
				t.Fatalf("first result: %+v %v", first, err)
			}
			beforeState, beforeJournal := readTestFile(t, sp), readTestFile(t, jp)
			retry, err := SubmitResult(root, sp, jp, req)
			if blocked && !errors.As(err, &blockedErr) || !blocked && err != nil {
				t.Fatalf("replayed outcome changed: %v", err)
			}
			one, _ := json.Marshal(first.Operation)
			two, _ := json.Marshal(retry.Operation)
			if !bytes.Equal(one, two) || !bytes.Equal(beforeState, readTestFile(t, sp)) || !bytes.Equal(beforeJournal, readTestFile(t, jp)) {
				t.Fatal("result retry consumed again or changed original receipt")
			}
		})
	}
}
