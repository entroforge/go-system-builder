package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

type interruptAfterReviewStaging struct {
	context.Context
	artifact string
}

var reviewStagingInterrupted = errors.New("simulated process interruption after artifact publication")

func (c interruptAfterReviewStaging) Err() error {
	if _, err := os.Stat(c.artifact); err == nil {
		panic(reviewStagingInterrupted)
	}
	return c.Context.Err()
}

// Panic skips the submitter's best-effort cleanup just as process termination
// does. The Writer lock's defer still releases the in-process test lock.
func interruptStagedReview(t *testing.T, root, statePath, journalPath, input, artifact string, revision int) {
	t.Helper()
	var answer Result
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	assignment := answer.AssignmentID
	func() {
		defer func() {
			if value := recover(); value != reviewStagingInterrupted {
				t.Fatalf("unexpected interruption: %v", value)
			}
		}()
		_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{Context: interruptAfterReviewStaging{context.Background(), artifact}, ExpectedRevision: revision, AssignmentID: assignment, ResultPath: input})
		t.Fatalf("did not interrupt staging: %v", err)
	}()
	snapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != revision {
		t.Fatal("interrupted pre-pending submission committed")
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatal(err)
	}
}

func TestReviewStagingResumesAfterInterruptedConsumer(t *testing.T) {
	for _, mode := range []string{"same-answer", "different-answer", "drift", "unowned"} {
		t.Run(mode, func(t *testing.T) {
			root, sp, jp, snapshot, plan := packetModeFixture(t)
			const id = "review-result-staging-recovery"
			input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
			interruptStagedReview(t, root, sp, jp, input, artifact, snapshot.Revision)
			original, err := os.ReadFile(artifact)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "different-answer":
				data, err := os.ReadFile(input)
				if err != nil {
					t.Fatal(err)
				}
				data = []byte(strings.Replace(string(data), `observed claim-qa-1`, `different observation for claim-qa-1`, 1))
				if err := os.WriteFile(input, data, 0o644); err != nil {
					t.Fatal(err)
				}
			case "drift":
				if err := os.WriteFile(artifact, append(original, ' '), 0o644); err != nil {
					t.Fatal(err)
				}
			case "unowned":
				if err := os.Remove(artifact + ".staging.json"); err != nil {
					t.Fatal(err)
				}
			}
			accepted, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
			if mode != "same-answer" {
				if err == nil {
					t.Fatal("unsafe staging accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("resume original staging: %v", err)
			}
			final, err := os.ReadFile(artifact)
			if err != nil {
				t.Fatal(err)
			}
			if string(final) != string(original) {
				t.Fatal("recovery replaced original exact artifact")
			}
			if accepted.Revision != snapshot.Revision+1 {
				t.Fatal("resume did not commit exactly once")
			}
			if _, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input}); err != nil {
				t.Fatalf("accepted replay: %v", err)
			}
		})
	}
}

type deadlineAfterReviewStaging struct {
	context.Context
	artifact, input string
	triggered       bool
	canceled        bool
}

func (c *deadlineAfterReviewStaging) Err() error {
	if _, err := os.Stat(c.artifact); err == nil {
		c.triggered = true
		if c.input != "" {
			_ = os.WriteFile(c.input, []byte(`{"changed":"after frozen read"}`), 0o644)
		}
	}
	if c.triggered {
		if c.canceled {
			return context.Canceled
		}
		return context.DeadlineExceeded
	}
	return c.Context.Err()
}
func TestReviewStagingDeadlineTailUsesFrozenAnswer(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "explicit-cancellation"}[canceled], func(t *testing.T) {
			root, sp, jp, snapshot, plan := packetModeFixture(t)
			const id = "review-result-frozen-tail"
			input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
			parent, timings := metrics.WithCallTimings(context.Background())
			ctx := &deadlineAfterReviewStaging{Context: parent, artifact: artifact, input: input, canceled: canceled}
			accepted, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
			spans := timings.Snapshot()
			wantRecoveries := int64(1)
			if canceled {
				wantRecoveries = 0
			}
			if spans["reception/s7/recovery_tail"].Count != wantRecoveries {
				t.Fatalf("recovery attempts = %d, want %d", spans["reception/s7/recovery_tail"].Count, wantRecoveries)
			}
			if spans["reception/s7/cleanup"].Count != 1 {
				t.Fatalf("cleanup budget was repeated: %+v", spans)
			}
			if canceled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("explicit cancellation = %v", err)
				}
				if _, statErr := os.Stat(artifact); !os.IsNotExist(statErr) {
					t.Fatalf("independent cleanup did not remove unreferenced canceled staging: %v", statErr)
				}
				current, e := loopruntime.NewStore(sp, jp).Snapshot()
				if e != nil {
					t.Fatal(e)
				}
				counts, metricsErr := metrics.NewStore(root).Read()
				if metricsErr != nil {
					t.Fatal(metricsErr)
				}
				if counts.S7ResultSubmits["interrupted"] != 1 || counts.S7ResultSubmits["rejected"] != 0 {
					t.Fatalf("cancellation counted as content rejection: %+v", counts.S7ResultSubmits)
				}
				if current.Revision != snapshot.Revision {
					t.Fatal("explicit cancellation resumed submission")
				}
				return
			}
			if err != nil {
				t.Fatalf("deadline tail did not receive original frozen answer: %v", err)
			}
			if accepted.Revision != snapshot.Revision+1 {
				t.Fatal("deadline tail did not commit exactly once")
			}
		})
	}
}

func TestReviewStagingFinalResultResumesCleanRoundInSameConsumption(t *testing.T) {
	root, sp, jp, snapshot, plan := packetModeFixture(t)
	qa := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-staging-first", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	accepted, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: qa})
	if err != nil {
		t.Fatal(err)
	}
	const id = "review-result-staging-final"
	dv := writeResultFile(t, root, plan, "assignment-dv-1", id, "agent-dv-1", "pass", map[string]string{"claim-dv-1": "pass"}, nil)
	artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-dv-1", id+".json")
	interruptStagedReview(t, root, sp, jp, dv, artifact, accepted.Revision)
	cleanPath := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "review", "clean-round-r1.json")
	original, err := os.ReadFile(cleanPath)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: accepted.Revision, AssignmentID: "assignment-dv-1", ResultPath: dv})
	if err != nil {
		t.Fatal(err)
	}
	clean, err := os.ReadFile(cleanPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(clean) != string(original) {
		t.Fatal("resumed CleanRound changed exact original bytes")
	}
	if ptr := PlanPointerFromState(resumed.State); ptr == nil || ptr.Status != "clean" {
		t.Fatal("final result did not produce CleanRound in original transaction")
	}
}

func TestReviewStagingConcurrentDifferentAnswersNeverAdoptsOtherAnswer(t *testing.T) {
	root, sp, jp, snapshot, plan := packetModeFixture(t)
	original := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-concurrent-different", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	data, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	different := filepath.Join(root, "docs", "reports", "different-answer.json")
	if err := os.WriteFile(different, []byte(strings.Replace(string(data), "observed claim-qa-1", "a different exact observation", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{original, different}
	var workers sync.WaitGroup
	start := make(chan struct{})
	outcomes := make([]error, 2)
	for i := range paths {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			_, outcomes[i] = SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: paths[i]})
		}(i)
	}
	close(start)
	workers.Wait()
	successes := 0
	for _, err := range outcomes {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("different concurrent answers successes=%d errors=%v", successes, outcomes)
	}
	current, err := loopruntime.NewStore(sp, jp).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != snapshot.Revision+1 {
		t.Fatal("different concurrent answers committed twice")
	}
	for i, err := range outcomes {
		if err == nil {
			continue
		}
		if _, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: paths[i]}); err == nil {
			t.Fatal("different answer replay adopted accepted artifact")
		}
	}
}

func TestReviewStagingOwnershipUsesImmutablePlanCoordinates(t *testing.T) {
	root, _, _, snapshot, _ := packetModeFixture(t)
	const rel = ".claude/evidence/owner-test/result.json"
	first, err := openResultStaging(root, rel, snapshot.State, []byte(`{"answer":"original"}`), nil, nil, "", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan := snapshot.State["review"].(map[string]any)["plan"].(map[string]any)
	plan["status"] = "cannot_clean"
	second, err := openResultStaging(root, rel, snapshot.State, []byte(`{"answer":"original"}`), nil, nil, "", time.Now().UTC())
	if err != nil {
		t.Fatalf("peer changed only plan status: %v", err)
	}
	if first.Owner != second.Owner || !first.OccurredAt.Equal(second.OccurredAt) {
		t.Fatal("mutable status changed original staging ownership")
	}
	plan["sha256"] = strings.Repeat("0", 64)
	if _, err := openResultStaging(root, rel, snapshot.State, []byte(`{"answer":"original"}`), nil, nil, "", time.Now().UTC()); err == nil {
		t.Fatal("changed pinned plan bytes retained staging ownership")
	}
}

func TestReviewStagingExhaustedDeadlineRecordsInterruption(t *testing.T) {
	root, sp, jp, snapshot, plan := packetModeFixture(t)
	input := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-expired-tail", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	// A live lock prevents the one bounded continuation from starting a mutation.
	if err := os.WriteFile(sp+".lock", []byte("occupied by test"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent, timings := metrics.WithCallTimings(context.Background())
	ctx, cancel := context.WithDeadline(parent, time.Now().Add(-time.Second))
	defer cancel()
	_, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exhausted deadline = %v", err)
	}
	if spans := timings.Snapshot(); spans["reception/s7/recovery_tail"].Count != 1 {
		t.Fatalf("deadline continuation repeated: %+v", spans)
	}
	counts, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if counts.S7ResultSubmits["interrupted"] != 1 || counts.S7ResultSubmits["rejected"] != 0 {
		t.Fatalf("deadline counted as content rejection: %+v", counts.S7ResultSubmits)
	}
}

func TestReviewStagingIntentRejectsPathsOutsideEvidenceScope(t *testing.T) {
	for _, rel := range []string{"README.md", ".claude/evidence/../../README.md"} {
		if _, err := openResultStaging(t.TempDir(), rel, map[string]any{}, []byte(`{}`), nil, nil, "", time.Now()); err == nil {
			t.Fatalf("intent accepted path %q", rel)
		}
	}
}

func TestReviewStagingEmptyIntentAllowsCorrectedOriginalID(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{}
	const rel = ".claude/evidence/original/result.json"
	first, err := openResultStaging(root, rel, state, []byte(`{"answer":"first"}`), nil, nil, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.write(rel, []byte("first exact artifact")); err != nil {
		t.Fatal(err)
	}
	if _, err := openResultStaging(root, rel, state, []byte(`{"answer":"corrected"}`), nil, nil, "", time.Now()); err == nil {
		t.Fatal("different answer adopted surviving original publication")
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
	state["reference"] = rel
	if _, err := openResultStaging(root, rel, state, []byte(`{"answer":"corrected"}`), nil, nil, "", time.Now()); err == nil {
		t.Fatal("referenced publication intent replaced")
	}
	delete(state, "reference")
	corrected, err := openResultStaging(root, rel, state, []byte(`{"answer":"corrected"}`), nil, nil, "", time.Now())
	if err != nil {
		t.Fatalf("fully cleaned staging blocked corrected answer using original ID: %v", err)
	}
	if corrected.Owner == first.Owner {
		t.Fatal("corrected answer retained prior ownership")
	}
	if err := corrected.write(rel, []byte("corrected exact artifact")); err != nil {
		t.Fatal(err)
	}
}

func TestReviewStagingCorrectedProfessionalAnswerKeepsOriginalID(t *testing.T) {
	root, sp, jp, snapshot, plan := packetModeFixture(t)
	const id = "review-result-corrected-after-cleanup"
	input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
	ctx := &deadlineAfterReviewStaging{Context: context.Background(), artifact: artifact, canceled: true}
	if _, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input}); !errors.Is(err, context.Canceled) {
		t.Fatalf("first interrupted attempt: %v", err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("canceled artifact was not cleaned: %v", err)
	}
	var answer map[string]any
	if err := json.Unmarshal(original, &answer); err != nil {
		t.Fatal(err)
	}
	claims := answer["claim_results"].([]any)
	answer["claim_results"] = claims[:1]
	incomplete, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, incomplete, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input}); err == nil {
		t.Fatal("genuine missing professional Claim was accepted")
	}
	answer["claim_results"] = claims
	claims[0].(map[string]any)["observed"] = "corrected complete professional observation"
	corrected, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, corrected, 0o644); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
	if err != nil {
		t.Fatalf("corrected original answer and original ID blocked by cleaned intent: %v", err)
	}
	if accepted.Revision != snapshot.Revision+1 {
		t.Fatal("corrected original answer did not commit once")
	}
	if err := os.WriteFile(input, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: accepted.Revision, AssignmentID: "assignment-qa-1", ResultPath: input}); err == nil {
		t.Fatal("accepted original ID was reused for a different answer")
	}
	final, err := loopruntime.NewStore(sp, jp).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if final.Revision != accepted.Revision {
		t.Fatal("accepted receipt conflict created another commit")
	}
}
