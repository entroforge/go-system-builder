package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// This bounded display record is an observation, never a receipt or authority
// for the original input. Unknown references remain explicitly empty.
type nativeS9Attempt struct {
	Kind              string `json:"kind"`
	ResponsibilityRef string `json:"responsibility_ref"`
	Owner             string `json:"owner"`
	Stage             string `json:"stage"`
	InputRef          string `json:"input_ref"`
	TypedInputRef     string `json:"typed_input_ref"`
	FailureKind       string `json:"failure_kind"`
	ObservedAt        string `json:"observed_at"`
}

type nativeS9AttemptCapture struct {
	root, key string
	value     nativeS9Diagnostic
	consumer  bool
}

func newNativeS9AttemptCapture(ctx context.Context, root string, snap runtime.Snapshot, key, owner, kind string) *nativeS9AttemptCapture {
	canonical, err := nativeS9PlanDiagnosticKey(snap.State, key)
	if err == nil {
		key = canonical
	}
	a := &nativeS9AttemptCapture{root: root, key: key}
	a.value = nativeS9Diagnostic{Scope: nativeS9DiagnosticScopeContext(ctx, root, snap.State, key), Runtime: stringValue(snap.State["runtime_id"]), Files: map[string]string{}, Attempt: &nativeS9Attempt{Kind: kind, ResponsibilityRef: key, Owner: owner, Stage: "selected", FailureKind: "unknown", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	return a
}

// SetResponsibilityRef captures the original issued brief (Builder) or exact
// Targeted request. The storage key remains the stable packet/assignment key.
func (a *nativeS9AttemptCapture) SetResponsibilityRef(ref string) {
	a.value.Attempt.ResponsibilityRef = ref
}

// Mark the actual adapter/domain consumer boundary at the call site. Successful
// consumption clears this marker before later preparation can fail.
func (a *nativeS9AttemptCapture) EnterConsumer(stage string) { a.consumer = true; a.SetStage(stage) }
func (a *nativeS9AttemptCapture) ConsumerSucceeded() {
	a.consumer = false
	// Later prerequisite failures cannot inherit a successful consumer marker.
	a.SetStage("consumer_completed")
}

func nativeS9ConsumerAttempt(a *nativeS9Attempt) bool {
	if a == nil {
		return false
	}
	switch a.Stage {
	case "flat_result_consumer", "plan_report_consumer", "typed_result_consumer", "targeted_consumer", "typed_result_saved":
		return true
	}
	return false
}

func (a *nativeS9AttemptCapture) SetStage(stage string)    { a.value.Attempt.Stage = stage }
func (a *nativeS9AttemptCapture) SetInput(ref string)      { a.value.Attempt.InputRef = ref }
func (a *nativeS9AttemptCapture) SetTypedInput(ref string) { a.value.Attempt.TypedInputRef = ref }
func (a *nativeS9AttemptCapture) ObserveInput(path string, data []byte) {
	if a.value.Attempt.InputRef == "" {
		a.SetInput(path)
	}
	if len(a.value.Files) < 16 {
		a.value.Files[path] = workpacket.Digest(data)
	}
}

// ObserveInputError records the result of the original failed read, without
// trying again to fill in diagnostic observations.
func (a *nativeS9AttemptCapture) ObserveInputError(path string, err error) {
	if a.value.Attempt.InputRef == "" {
		a.SetInput(path)
	}
	if len(a.value.Files) >= 16 {
		return
	}
	if os.IsNotExist(err) {
		a.value.Files[path] = "missing"
	} else if err != nil {
		a.value.Files[path] = "unreadable: " + err.Error()
	}
}

// Publish does no business reads. All scope, references and observations were
// captured at the original selection/read points, before a possible timeout.
func (a *nativeS9AttemptCapture) Publish(ctx context.Context, failure error) error {
	// A canceled subprocess may surface an ExitError (signal killed) rather
	// than wrapping the deadline. The original request's typed termination is
	// an explicit transport fact; retain it alongside the original diagnostic.
	requestFailure := ctx.Err()
	if failure != nil && requestFailure != nil {
		failure = errors.Join(failure, requestFailure)
	}
	ctx = nativeS9PublicationContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.value.Scope == "" || a.key == "" {
		return fmt.Errorf("S9 diagnostic responsibility scope unavailable")
	}

	// One fixed short lock protects only bounded display records, never Runtime.
	lockRef := ".claude/diagnostics/native-s9/.publish.lock"
	if err := workpacket.CheckPath(a.root, lockRef); err != nil {
		return err
	}
	release, err := filelock.Acquire(ctx, filepath.Join(a.root, filepath.FromSlash(lockRef)))
	if err != nil {
		return err
	}
	defer release()
	if failure == nil {
		parent, name, err := workpacket.OpenParent(a.root, nativeS9DiagnosticPath(a.key), false)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		defer parent.Close()
		err = parent.Remove(name)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	a.value.Attempt.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	var completion *repairExitFailure
	var author *nativeAuthorError
	var fields *workpacket.AnswerError
	switch {
	case errors.Is(failure, context.DeadlineExceeded):
		a.value.Attempt.FailureKind = "deadline"
	case errors.Is(failure, context.Canceled):
		a.value.Attempt.FailureKind = "canceled"
	case errors.As(failure, &completion):
		a.value.Attempt.FailureKind = "successor"
	case errors.Is(failure, runtime.ErrSubmissionConflict):
		a.value.Attempt.FailureKind = "conflict"
	case errors.As(failure, &author) || errors.As(failure, &fields):
		a.value.Attempt.FailureKind = "author"
	default:
		a.value.Attempt.FailureKind = "unknown"
	}
	observedRefs := make([]string, 0, len(a.value.Files))
	for ref := range a.value.Files {
		observedRefs = append(observedRefs, ref)
	}
	sort.Strings(observedRefs)
	a.value.Message = fmt.Sprintf("A preserved S9 reception attempt for %s reached %s. Exact input: %s; typed input: %s; observed references: %v. Consumer diagnostic: %s.", a.value.Attempt.Owner, a.value.Attempt.Stage, a.value.Attempt.InputRef, a.value.Attempt.TypedInputRef, observedRefs, failure.Error())
	if errors.As(failure, &completion) {
		detail := "completion outlet unfinished"
		if cause := errors.Join(completion.cause, requestFailure); cause != nil {
			detail = cause.Error()
		}
		a.value.Message = fmt.Sprintf("Original Targeted observations for %s are accepted. Completion diagnostic: %s.", a.value.Attempt.Owner, detail)
	}

	// A new rejection replaces the previous consumer cause. A prerequisite
	// failure retains one prior cause, without accumulating an error history.
	a.value.ConsumerFailure = nil
	if !a.consumer {
		if raw, readErr := workpacket.ReadFile(a.root, nativeS9DiagnosticPath(a.key), 64<<10); readErr == nil {
			var previous nativeS9Diagnostic
			if json.Unmarshal(raw, &previous) == nil && previous.Scope == a.value.Scope && previous.Runtime == a.value.Runtime {
				if nativeS9ConsumerAttempt(previous.Attempt) {
					a.value.ConsumerFailure = &nativeS9ConsumerFailure{Message: previous.Message, Files: previous.Files, Attempt: previous.Attempt}
				} else {
					a.value.ConsumerFailure = previous.ConsumerFailure
				}
			}
		}
	}
	data, err := json.Marshal(a.value)
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return fmt.Errorf("S9 diagnostic exceeds 64 KiB")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, name, err := workpacket.OpenParent(a.root, nativeS9DiagnosticPath(a.key), true)
	if err != nil {
		return err
	}
	defer parent.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := fmt.Sprintf(".diagnostic-%x.tmp", random)
	f, err := parent.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer parent.Remove(temporary)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return parent.Rename(temporary, name)
}

// Stable responsibility/source coordinates come only from the original index.
// Receiving Plan changes phase and global revision, neither changes this scope.
func nativeS9DiagnosticScopeContext(ctx context.Context, root string, state map[string]any, key string) string {
	if ctx.Err() != nil {
		return ""
	}
	pointer := repairPointerCLI(state)
	source := map[string]any{}
	for _, field := range []string{"case_id", "case_ref", "case_sha256", "contract_id", "contract_ref", "contract_sha256", "session_id", "session_ref", "session_sha256", "plan_id", "plan_ref", "plan_sha256"} {
		source[field] = pointer[field]
	}
	var responsibility any
	for _, row := range objectSliceAny(mapFieldCLI(state, "entities")["work_packets"]) {
		if row["template_id"] != "s9-plan-report-v1" || row["packet_id"] != key {
			continue
		}
		if responsibility != nil {
			return ""
		}
		var binding workpacket.RepairPlanReportBinding
		data, err := json.Marshal(row["binding"])
		if err != nil || json.Unmarshal(data, &binding) != nil {
			return ""
		}
		if binding.RuntimeID != state["runtime_id"] || binding.BaselineGeneration != workspace.Generation(state) || binding.SessionID != pointer["session_id"] || binding.PlanRef != pointer["plan_ref"] || stringMapCLI(pointer["assignment_owners"])[binding.RepairAssignmentID] != binding.OwnerAgentID {
			return ""
		}
		responsibility = []any{key, binding}
	}
	if responsibility == nil {
		for assignment, owner := range stringMapCLI(pointer["assignment_owners"]) {
			if path, err := repairTargetedQuestionnairePath(root, state, assignment); err == nil && path == key {
				if responsibility != nil {
					return ""
				}
				responsibility = []string{assignment, owner}
			}
		}
		if responsibility == nil {
			return ""
		}
		source["impact_ref"], source["impact_sha256"] = pointer["impact_ref"], pointer["impact_sha256"]
	}
	data, err := json.Marshal([]any{state["runtime_id"], workspace.Generation(state), nestedStateValue(state, "review", "round"), source, responsibility})
	if err != nil {
		return ""
	}
	return workpacket.Digest(data)
}
