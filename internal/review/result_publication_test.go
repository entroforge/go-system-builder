package review

import (
	"bytes"
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

type interruptAfterReviewPublication struct {
	context.Context
	artifact string
}

var reviewPublicationInterrupted = errors.New("simulated process interruption after artifact publication")

func (c interruptAfterReviewPublication) Err() error {
	if _, err := os.Stat(c.artifact); err == nil {
		panic(reviewPublicationInterrupted)
	}
	return c.Context.Err()
}

// Panic models a pre-pending interruption. The original Writer lock is still
// released, while unregistered publications remain unknown.
func interruptReviewPublication(t *testing.T, root, statePath, journalPath, input, artifact string, revision int) {
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
			if value := recover(); value != reviewPublicationInterrupted {
				t.Fatalf("unexpected interruption: %v", value)
			}
		}()
		_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{Context: interruptAfterReviewPublication{context.Background(), artifact}, ExpectedRevision: revision, AssignmentID: assignment, ResultPath: input})
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

func TestReviewPublicationResumesAfterInterruptedConsumer(t *testing.T) {
	for _, mode := range []string{"same-answer", "different-answer", "drift", "unowned"} {
		t.Run(mode, func(t *testing.T) {
			root, sp, jp, snapshot, plan := packetModeFixture(t)
			const id = "review-result-staging-recovery"
			input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
			interruptReviewPublication(t, root, sp, jp, input, artifact, snapshot.Revision)
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
				if err := os.WriteFile(artifact+".staging.json", []byte("unknown historical marker"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			unregistered := mustReadTestFile(t, artifact)
			accepted, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
			if err != nil {
				t.Fatalf("receive original responsibility past unknown publication: %v", err)
			}
			if !bytes.Equal(mustReadTestFile(t, artifact), unregistered) {
				t.Fatal("unregistered artifact changed")
			}
			row := resultPacketAssignmentRow(accepted.State, "assignment-qa-1")
			ref := row["result_receipt"].(map[string]any)["result_ref"].(string)
			if filepath.Join(root, ref) == artifact {
				t.Fatal("unregistered artifact was adopted")
			}
			if _, err := os.Stat(filepath.Join(root, ref)); err != nil {
				t.Fatal(err)
			}
			if mode == "unowned" && string(mustReadTestFile(t, artifact+".staging.json")) != "unknown historical marker" {
				t.Fatal("unknown marker changed")
			}
			if accepted.Revision != snapshot.Revision+1 {
				t.Fatal("resume did not commit exactly once")
			}
			if replay, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input}); err != nil || replay.Revision != accepted.Revision {
				t.Fatalf("accepted replay: %v", err)
			}

		})
	}
}

type deadlineAfterReviewPublication struct {
	context.Context
	artifact, input string
	triggered       bool
	canceled        bool
}

func (c *deadlineAfterReviewPublication) Err() error {
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
func TestReviewPublicationDeadlineTailUsesFrozenAnswer(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "explicit-cancellation"}[canceled], func(t *testing.T) {
			root, sp, jp, snapshot, plan := packetModeFixture(t)
			const id = "review-result-frozen-tail"
			input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
			parent, timings := metrics.WithCallTimings(context.Background())
			ctx := &deadlineAfterReviewPublication{Context: parent, artifact: artifact, input: input, canceled: canceled}
			accepted, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
			spans := timings.Snapshot()
			wantRecoveries := int64(1)
			if canceled {
				wantRecoveries = 0
			}
			if spans["reception/s7/recovery_tail"].Count != wantRecoveries {
				t.Fatalf("recovery attempts = %d, want %d", spans["reception/s7/recovery_tail"].Count, wantRecoveries)
			}
			if spans["reception/s7/cleanup"].Count != 0 {
				t.Fatalf("cleanup budget was repeated: %+v", spans)
			}
			if canceled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("explicit cancellation = %v", err)
				}
				if _, statErr := os.Stat(artifact); statErr != nil {
					t.Fatalf("unknown canceled publication was not preserved: %v", statErr)
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

func TestReviewPublicationFinalResultResumesCleanRoundInSameConsumption(t *testing.T) {
	root, sp, jp, snapshot, plan := packetModeFixture(t)
	qa := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-staging-first", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	accepted, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: qa})
	if err != nil {
		t.Fatal(err)
	}
	const id = "review-result-staging-final"
	dv := writeResultFile(t, root, plan, "assignment-dv-1", id, "agent-dv-1", "pass", map[string]string{"claim-dv-1": "pass"}, nil)
	artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-dv-1", id+".json")
	interruptReviewPublication(t, root, sp, jp, dv, artifact, accepted.Revision)
	resumed, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: accepted.Revision, AssignmentID: "assignment-dv-1", ResultPath: dv})
	if err != nil {
		t.Fatal(err)
	}
	if ptr := PlanPointerFromState(resumed.State); ptr == nil || ptr.Status != "clean" {
		t.Fatal("final result did not produce CleanRound in original transaction")
	}
}

func TestReviewPublicationConcurrentDifferentAnswersNeverAdoptsOtherAnswer(t *testing.T) {
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

func TestReviewPublicationExhaustedDeadlineRecordsInterruption(t *testing.T) {
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
	if spans := timings.Snapshot(); spans["reception/s7/recovery_tail"].Count != 0 {
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

func TestReviewPublicationRejectsPathsOutsideEvidenceScope(t *testing.T) {
	for _, rel := range []string{"README.md", ".claude/evidence/../../README.md"} {
		if _, err := publishResultArtifact(context.Background(), t.TempDir(), nil, rel, []byte(`{}`)); err == nil {
			t.Fatalf("accepted path %q", rel)
		}
	}
}

func TestReviewPublicationNeverAdoptsUnknownOccupants(t *testing.T) {
	for _, mode := range []string{"same", "different", "malformed", "directory", "symlink", "parent-file", "parent-symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			rel := ".claude/evidence/original/result.json"
			abs := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("professional original")
			switch mode {
			case "directory":
				if err := os.Mkdir(abs, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("missing-user-file", abs); err != nil {
					t.Skip(err)
				}
			case "parent-file", "parent-symlink":
				if err := os.Remove(filepath.Dir(abs)); err != nil {
					t.Fatal(err)
				}
				if mode == "parent-file" {
					if err := os.WriteFile(filepath.Dir(abs), original, 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink("missing-user-directory", filepath.Dir(abs)); err != nil {
					t.Skip(err)
				}
			default:
				if mode == "different" {
					original = []byte("another observation")
				}
				if mode == "malformed" {
					original = []byte("{broken")
				}
				if err := os.WriteFile(abs, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			adopted, err := publishResultArtifact(context.Background(), root, nil, rel, []byte("professional original"))
			if err != nil || adopted == rel {
				t.Fatalf("occupied path was not bypassed: %s %v", adopted, err)
			}
			if string(mustReadTestFile(t, filepath.Join(root, adopted))) != "professional original" {
				t.Fatal("new expression changed")
			}
			if mode == "same" || mode == "different" || mode == "malformed" {
				if !bytes.Equal(mustReadTestFile(t, abs), original) {
					t.Fatal("unknown data changed")
				}
			}
			if mode == "parent-file" && !bytes.Equal(mustReadTestFile(t, filepath.Dir(abs)), original) {
				t.Fatal("unknown parent changed")
			}
			if mode == "symlink" {
				if target, err := os.Readlink(abs); err != nil || target != "missing-user-file" {
					t.Fatal("unknown symlink changed")
				}
			}
			if _, err := os.Lstat(filepath.Join(root, adopted) + ".staging.json"); !os.IsNotExist(err) {
				t.Fatal("new ownership marker created")
			}
		})
	}
}

func TestReviewPublicationCorrectedProfessionalAnswerKeepsOriginalID(t *testing.T) {
	root, sp, jp, snapshot, plan := packetModeFixture(t)
	const id = "review-result-corrected-after-interruption"
	input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
	ctx := &deadlineAfterReviewPublication{Context: context.Background(), artifact: artifact, canceled: true}
	if _, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input}); !errors.Is(err, context.Canceled) {
		t.Fatalf("first interrupted attempt: %v", err)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("unknown canceled artifact was not retained: %v", err)
	}
	var answer map[string]any
	if err := json.Unmarshal(original, &answer); err != nil {
		t.Fatal(err)
	}
	claims := answer["claim_results"].([]any)
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

func TestReviewPublicationBypassesRegisteredMissingSources(t *testing.T) {
	const preferred = ".claude/evidence/original/result.json"
	for _, mode := range []string{"evidence-other-owner", "finding-other-owner", "assignment-receipt", "issued-receipt", "professional-mention"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			state := map[string]any{}
			switch mode {
			case "evidence-other-owner":
				state["evidence"] = []any{map[string]any{"path": preferred, "produced_by": []any{"another-author"}}}
			case "finding-other-owner":
				state["entities"] = map[string]any{"findings": []any{map[string]any{"path": preferred, "original_finder": "another-author"}}}
			case "assignment-receipt":
				state["review"] = map[string]any{"assignments": map[string]any{"another-assignment": map[string]any{"result_receipt": map[string]any{"result_ref": preferred}}}}
			case "issued-receipt":
				state["entities"] = map[string]any{"work_packets": []any{map[string]any{"packet_id": "another-packet", "review_result_reception": map[string]any{"receipt": map[string]any{"result_ref": preferred}}}}}
			default:
				state["professional_note"] = preferred
			}
			actual, err := publishResultArtifact(context.Background(), root, state, preferred, []byte("this responsibility's result"))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "professional-mention" {
				if actual != preferred {
					t.Fatal("unrelated prose narrowed publication")
				}
			} else {
				if actual == preferred {
					t.Fatal("registered missing source was replaced")
				}
				if _, err := os.Lstat(filepath.Join(root, preferred)); !os.IsNotExist(err) {
					t.Fatalf("registered missing path was touched: %v", err)
				}
			}
			if string(mustReadTestFile(t, filepath.Join(root, actual))) != "this responsibility's result" {
				t.Fatal("new original expression changed")
			}
		})
	}
}
