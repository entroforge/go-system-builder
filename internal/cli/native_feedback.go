package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Only the exact native artifact/validator boundary may mark author-owned work.
// Unmarked failures remain Main's responsibility; no error-text guessing.
type nativeAuthorError struct{ error }

func (e *nativeAuthorError) Unwrap() error { return e.error }

func nativeArtifactReadError(label string, err error) error {
	wrapped := fmt.Errorf("%s: %w", label, err)
	if os.IsNotExist(err) {
		return &nativeAuthorError{wrapped}
	}
	return wrapped
}

func nativeStopFailure(stage string, active bool, err error, diagnostic io.Writer) int {
	var author *nativeAuthorError
	var fields *workpacket.AnswerError
	detail := nativeFailureDetail(err)
	errors.As(err, &fields)
	if !active && !nativeReceptionInterrupted(err) && (errors.As(err, &author) || fields != nil) {
		fmt.Fprintln(diagnostic, stage+": correct your actual professional artifact or return a real blocker. "+detail)
		return 2
	}
	fmt.Fprintln(diagnostic, stage+" reception incomplete; Main owns recovery; preserve the original artifacts and receipt, inspect this failure before resuming the original responsibility: "+detail)
	return 0
}

// S5 reviewers may be unable to form a conclusion, notably when their actual
// authorship prevents independent review. The original consumer still rejects
// incomplete work. Returning that failure to Main must not force the reviewer
// to choose a conclusion just to end the native turn. No prose classification,
// new blocker protocol or accepted receipt is introduced at this boundary.
func nativeS5HandoffFailure(owner, answer string, err error, diagnostic io.Writer) int {
	fmt.Fprintf(diagnostic, "S5 reception incomplete; Main owns recovery for %s at %s. Preserve the original artifacts and any accepted receipt; this return does not establish review completion. Inspect the actual native return and this diagnostic: resume the original responsibility for professional omissions, or restore sources/arrange an independent reviewer for a real blocker. Do not invent a conclusion to exit. %s\n", owner, answer, nativeFailureDetail(err))
	return 0
}

// S7 can end without a receivable result when the actual review cannot be
// completed. Preserve the consumer's rejection and hand the original work back
// to Main on the first return, without classifying the author's prose or forcing
// a second Stop to report the obstacle.
func nativeS7HandoffFailure(owner, answer string, err error, diagnostic io.Writer) int {
	if nativeReceptionInterrupted(err) {
		fmt.Fprintf(diagnostic, "S7 reception incomplete; the original receiver retains transaction recovery for %s at %s. %s\n", owner, answer, nativeFailureDetail(err))
		return 0
	}
	fmt.Fprintf(diagnostic, "S7 reception incomplete; Main owns recovery for %s at %s. Preserve the original answer, observations and any accepted receipt; this return does not establish review completion. Inspect the actual native return and this diagnostic: restore missing or stale sources and environmental prerequisites, or resume the original reviewer responsibility to correct professional omissions. A recorded site_lost blocker uses its original blocker-resolution action before the same reviewer resumes. Do not invent a verdict, product Finding or blocked Claim to exit. %s\n", owner, answer, nativeFailureDetail(err))
	return 0
}

// A discriminator may end without enough evidence for a receivable answer.
// Preserve the original rejection while returning the responsibility to Main;
// an incomplete native return neither supplies evidence nor completes S8.
func nativeS8HandoffFailure(owner, answer string, err error, diagnostic io.Writer) int {
	fmt.Fprintf(diagnostic, "S8 reception incomplete; Main owns recovery for %s at %s. Preserve the original answer, observations and any accepted receipt; this return does not establish investigation completion. Inspect the actual native return and this diagnostic: restore missing or stale sources and discriminator prerequisites, or resume the original Investigator to correct professional omissions. A supported/refuted/inconclusive result still requires its actual evidence and source boundary. Do not invent evidence or a conclusion to exit. %s\n", owner, answer, nativeFailureDetail(err))
	return 0
}

func nativeReceptionInterrupted(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func nativeFailureDetail(err error) string {
	if nativeReceptionInterrupted(err) {
		return "Reception interrupted by timeout/cancellation: " + err.Error() + ". This is not a professional content rejection. The original consumer owns staged-artifact and pending recovery using this exact delivery. If already accepted, only the original pending outlet remains; do not resubmit accepted work. This interrupted return does not establish completion or request another report. Preserve the original answer and partial receipts; timeout alone requires no answer edit, evidence deletion or artifact-ID change."
	}
	detail := err.Error()
	var fields *workpacket.AnswerError
	if errors.As(err, &fields) && len(fields.Fields) > 0 && fields.Fields[0].Repair != "" {
		detail += " " + fields.Fields[0].Repair
	}
	return detail
}

// S9 returns unfinished professional work on the first Stop while the original
// consumer continues to reject it. No blocker fields or forced conclusion are
// needed to end the native turn, and an accepted partial result stays accepted.
func nativeS9HandoffFailure(owner, references string, err error, diagnostic io.Writer) int {
	var completion *repairExitFailure
	if errors.As(err, &completion) {
		fmt.Fprintf(diagnostic, "S9 Targeted is accepted; Main owns the incomplete original Handoff/S7 preparation. Preserve the independent observations and resume the supplied completion recovery, without another verifier report. %s\n", completion.Error())
		return 0
	}
	if nativeReceptionInterrupted(err) {
		fmt.Fprintf(diagnostic, "S9 reception incomplete; the original receiver retains transaction recovery for %s. Exact brief/answer references: %s. %s\n", owner, references, nativeFailureDetail(err))
		return 0
	}
	fmt.Fprintf(diagnostic, "S9 reception incomplete; Main owns recovery for %s. Exact brief/answer references: %s. Preserve original artifacts and any accepted PlanReport/Targeted receipt; this return does not establish repair completion. Restore missing or stale sources/prerequisites, resume the original professional responsibility for omissions, or complete Main integration/Handoff when the professional result is already accepted. Consumer diagnostic: %s\n", owner, references, nativeFailureDetail(err))
	return 0
}
