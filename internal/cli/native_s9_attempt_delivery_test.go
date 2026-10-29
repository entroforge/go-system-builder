package cli

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

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func attemptDeliverySnapshot() runtime.Snapshot {
	binding := workpacket.RepairPlanReportBinding{RuntimeID: "runtime-original", SessionID: "session-original", PlanRef: "unavailable-plan.json", RepairAssignmentID: "assignment-original", OwnerAgentID: "builder-original"}
	return runtime.Snapshot{State: map[string]any{
		"runtime_id": "runtime-original",
		"review":     map[string]any{"repair": map[string]any{"session_id": "session-original", "plan_ref": "unavailable-plan.json", "assignment_owners": map[string]any{"assignment-original": "builder-original"}}},
		"entities":   map[string]any{"work_packets": []any{map[string]any{"template_id": "s9-plan-report-v1", "packet_id": "plan-original", "binding": binding, "body_ref": "absent-body.json"}}},
	}}
}

func TestNativeS9FailurePublicationUsesCapturedFactsAfterWorkDeadline(t *testing.T) {
	root := t.TempDir()
	snap := attemptDeliverySnapshot()
	// A work deadline already passed while the original outer tail remains.
	work, cancel := hookRequestContexts(t.Context(), time.Now().Add(time.Second))
	defer cancel()
	a := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
	a.SetInput(filepath.Join(root, "absent-input.json"))
	a.SetStage("plan-accepted")
	if a.value.Scope == "" {
		t.Fatal("indexed responsibility required a packet body or source read")
	}
	if !errors.Is(work.Err(), context.DeadlineExceeded) {
		t.Fatal("work did not expire")
	}
	if err := a.Publish(work, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	// A new request has only original index state; phase changes cannot erase
	// a failure captured before Plan reception changed the phase.
	snap.State["lifecycle"] = map[string]any{"state": "repairing", "phase": "result"}
	snap.Revision++
	value, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
	if !ok || value.Attempt == nil || value.Attempt.Stage != "plan-accepted" || value.Attempt.FailureKind != "deadline" || !strings.Contains(value.Message, "deadline exceeded") {
		t.Fatalf("cross-request delivery: %#v %v", value, ok)
	}
	if len(value.Files) != 0 {
		t.Fatal("publication invented missing-file observations")
	}
	repairPointerCLI(snap.State)["plan_ref"] = "different-plan.json"
	if _, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original"); ok {
		t.Fatal("new source retained old failure")
	}
}

func TestNativeS9FailurePublicationDoesNotRereadCapturedInput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "answer.json")
	a := newNativeS9AttemptCapture(t.Context(), root, attemptDeliverySnapshot(), "plan-original", "builder-original", "Builder")
	a.ObserveInput(path, []byte("already-read"))
	// No file exists. Publishing must retain the original digest, not observe
	// a missing path by doing a new business read after failure.
	if err := a.Publish(t.Context(), errors.New("original typed failure")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, nativeS9DiagnosticPath("plan-original")))
	if err != nil {
		t.Fatal(err)
	}
	var value nativeS9Diagnostic
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if value.Files[path] != workpacket.Digest([]byte("already-read")) {
		t.Fatal("captured bytes replaced with a later observation")
	}
}

func TestNativeS9PublicationKeepsCancellationAndAbsoluteDeadline(t *testing.T) {
	root := t.TempDir()
	snap := attemptDeliverySnapshot()
	parent, cancelParent := context.WithCancel(t.Context())
	outer := time.Now().Add(5 * time.Second)
	work, cancel := hookRequestContexts(parent, outer)
	defer cancel()
	publication := nativeS9PublicationContext(work)
	deadline, _ := publication.Deadline()
	if !deadline.Equal(outer) {
		t.Fatal("publication renewed absolute deadline")
	}
	a := newNativeS9AttemptCapture(work, root, snap, "plan-original", "builder-original", "Builder")
	cancelParent()
	if err := a.Publish(work, context.DeadlineExceeded); !errors.Is(err, context.Canceled) {
		t.Fatalf("explicit cancellation escaped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatal("canceled publication touched storage")
	}
	expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := a.Publish(expired, context.DeadlineExceeded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("direct expired call gained budget: %v", err)
	}
	t.Setenv(hookDeadlineEnvironment, outer.Add(-4*time.Second).Format(time.RFC3339Nano))
	if got := hookOuterDeadline(outer); !got.Equal(outer.Add(-4 * time.Second)) {
		t.Fatal("worker reset parent deadline")
	}
}

func TestNativeS9FourFieldDiagnosticRemainsReadable(t *testing.T) {
	root := t.TempDir()
	snap := attemptDeliverySnapshot()
	value := nativeS9Diagnostic{Scope: nativeS9DiagnosticScope(root, snap.State, "plan-original"), Runtime: "runtime-original", Message: "old specific failure", Files: map[string]string{}}
	data, _ := json.Marshal(value)
	path := filepath.Join(root, nativeS9DiagnosticPath("plan-original"))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
	if !ok || got.Attempt != nil || got.Message != value.Message {
		t.Fatalf("legacy diagnostic unreadable: %#v %v", got, ok)
	}
}

func TestNativeS9FailureKindUsesTypedErrorsBeforeProse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		kind    string
	}{
		{"native-author", &nativeAuthorError{errors.New("original missing answer")}, "author"},
		{"answer-validator", &workpacket.AnswerError{Label: "original field error"}, "author"},
		{"author-deadline", &nativeAuthorError{context.DeadlineExceeded}, "deadline"},
		{"answer-canceled", &workpacket.AnswerError{Label: "original validation", Cause: context.Canceled}, "canceled"},
		{"same-key-conflict", runtime.ErrSubmissionConflict, "conflict"},
		{"successor", &repairExitFailure{cause: errors.New("original exit failure"), message: "original exit failure"}, "successor"},
		{"successor-deadline", &repairExitFailure{cause: context.DeadlineExceeded, message: "exit timeout"}, "deadline"},
		{"untyped-prose", errors.New("deadline exceeded; author omission; submission identity conflict"), "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			snap := attemptDeliverySnapshot()
			a := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
			a.SetResponsibilityRef("original/native/brief.md")
			if err := a.Publish(t.Context(), tc.failure); err != nil {
				t.Fatal(err)
			}
			value, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
			if !ok || value.Attempt == nil || value.Attempt.FailureKind != tc.kind || value.Attempt.ResponsibilityRef != "original/native/brief.md" {
				t.Fatalf("typed classification: %#v %v", value, ok)
			}
		})
	}
}

func TestNativeS9DerivedWorkCancellationCannotEscapeToPublication(t *testing.T) {
	root := t.TempDir()
	work, cancel := hookRequestContexts(t.Context(), time.Now().Add(5*time.Second))
	defer cancel()
	publication := nativeS9PublicationContext(work)
	a := newNativeS9AttemptCapture(work, root, attemptDeliverySnapshot(), "plan-original", "builder-original", "Builder")
	derived, cancelDerived := context.WithCancel(work)
	cancelDerived()
	if publication.Err() != nil {
		t.Fatal("test did not preserve the live sibling publication branch")
	}
	if err := a.Publish(derived, context.DeadlineExceeded); !errors.Is(err, context.Canceled) {
		t.Fatalf("derived explicit cancellation gained sibling publication budget: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatal("derived canceled publication touched storage")
	}
}

func TestNativeS9PublicationPreservesTypedWorkDeadlineBehindGitExitError(t *testing.T) {
	for _, failure := range []error{&exec.ExitError{}, fmt.Errorf("git rev-parse: %w", errors.New("signal: killed"))} {
		root := t.TempDir()
		snap := attemptDeliverySnapshot()
		work, cancel := hookRequestContexts(t.Context(), time.Now().Add(time.Second))
		a := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
		if err := a.Publish(work, failure); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		value, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
		if !ok || value.Attempt == nil || value.Attempt.FailureKind != "deadline" || !strings.Contains(value.Message, "context deadline exceeded") {
			t.Fatalf("original typed work deadline lost behind subprocess error: %#v %v", value, ok)
		}
	}
}

func TestNativeS9NewAttemptMessageContainsFactsWithoutRecoveryActions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		want    string
	}{
		{"deadline", context.DeadlineExceeded, "context deadline exceeded"},
		{"successor", &repairExitFailure{cause: errors.New("actual original exit source unavailable"), message: "resume original consumer using runtime repair handoff complete"}, "actual original exit source unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			snap := attemptDeliverySnapshot()
			a := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
			if err := a.Publish(t.Context(), tc.failure); err != nil {
				t.Fatal(err)
			}
			value, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
			if !ok || !strings.Contains(value.Message, tc.want) {
				t.Fatalf("actual failure detail lost: %#v %v", value, ok)
			}
			for _, wrong := range []string{"resume the original consumer", "resume original consumer", "run this exact", "runtime repair handoff complete", "preserve the original", "preserve any accepted", "do not resubmit", "original consumer owns"} {
				if strings.Contains(strings.ToLower(value.Message), wrong) {
					t.Errorf("new diagnostic stored recovery imperative %q: %s", wrong, value.Message)
				}
			}
			if tc.name == "successor" && !strings.Contains(value.Message, "are accepted") {
				t.Fatal("accepted Targeted fact lost")
			}
		})
	}
}

func TestNativeS9SavedTypedInputMissingRetainsOriginalWindowWithoutWriting(t *testing.T) {
	root := t.TempDir()
	snap := attemptDeliverySnapshot()
	typed := filepath.Join(root, "original-typed-answer.json")
	expected := workpacket.Digest([]byte("original already-read fixed typed input"))
	a := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
	a.SetInput(filepath.Join(root, "original-flat-result.json"))
	a.SetTypedInput(typed)
	a.SetStage("typed_result_saved")
	a.ObserveInput(typed, []byte("original already-read fixed typed input"))
	if err := a.Publish(t.Context(), context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, nativeS9DiagnosticPath("plan-original"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
	if !ok || got.Attempt == nil || got.Attempt.Stage != "typed_result_saved" || got.Attempt.TypedInputRef != typed || got.Files[typed] != expected {
		t.Fatalf("saved typed window vanished or changed: %#v %v", got, ok)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only missing input derivation rewrote diagnostic")
	}
	var persisted nativeS9Diagnostic
	if err := json.Unmarshal(after, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Attempt.Stage != "typed_result_saved" {
		t.Fatal("derived missing stage became persistent state")
	}
	if err := os.WriteFile(typed, []byte("changed current typed bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original"); !ok {
		t.Fatal("changed draft erased historical failure")
	}
}

func TestNativeS9MissingObservationExceptionIsOnlyForExactSavedTypedInput(t *testing.T) {
	for _, tc := range []struct{ name, stage, typedRef, observed string }{
		{"prepared-is-not-saved", "typed_result_prepared", "exact", workpacket.Digest([]byte("read"))},
		{"different-typed-coordinate", "typed_result_saved", "different", workpacket.Digest([]byte("read"))},
		{"not-a-byte-digest", "typed_result_saved", "exact", "unreadable: actual original error"},
		{"another-original-input-changed", "typed_result_saved", "exact", workpacket.Digest([]byte("read"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			snap := attemptDeliverySnapshot()
			path := filepath.Join(root, "missing-original.json")
			a := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
			a.SetStage(tc.stage)
			a.SetTypedInput(path)
			if tc.typedRef == "different" {
				a.SetTypedInput(filepath.Join(root, "different-input.json"))
			}
			a.value.Files[path] = tc.observed
			if tc.name == "another-original-input-changed" {
				a.value.Files[filepath.Join(root, "different-original-input.json")] = workpacket.Digest([]byte("original other input"))
			}
			if err := a.Publish(t.Context(), context.DeadlineExceeded); err != nil {
				t.Fatal(err)
			}
			if _, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original"); !ok {
				t.Fatal("private observation loss erased historical failure")
			}
		})
	}
}
