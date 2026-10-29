package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func s9OriginalConsumeCommand(root, input string) string {
	return fmt.Sprintf("%s runtime stage-answer consume --root %s --path %s", s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")), s7CaptureShellQuote(root), s7CaptureShellQuote(input))
}

// This chooses navigation from exact already captured coordinates. The named
// consumer must still verify current issuance, worktree, input and receipt.
func s9UnreceivedActionContext(ctx context.Context, root string, handoff s9HandoffProjection) string {
	base := s9UnreceivedAction()
	if handoff.Native {
		base += " PreToolUse observes ready original answers; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer."
	} else {
		base += " Follow the original issued responsibility's reception and recovery contract."
	}
	base += " No repeated accepted submission is due."
	if observed := nativeExecutionInContext(ctx); observed == nativeExecutionLaunched || observed == nativeExecutionUnknown {
		return base + " " + nativeExecutionPendingAction(observed)
	}
	if ctx.Err() != nil {
		return base + " Current reception detail is unknown: " + ctx.Err().Error()
	}
	attempt := handoff.Attempt
	consumerAttempt := attempt
	if handoff.ConsumerFailure != nil {
		consumerAttempt = handoff.ConsumerFailure.Attempt
	}
	observations := handoff.DiagnosticFiles
	// The latest prerequisite attempt may not contain the saved typed coordinate.
	// Its retained consumer cause supplies navigation, never source authority.
	if handoff.ConsumerFailure != nil && handoff.ConsumerFailure.Attempt != nil {
		if attempt == nil || attempt.TypedInputRef == "" {
			attempt = handoff.ConsumerFailure.Attempt
			observations = handoff.ConsumerFailure.Files
		}
	}
	// Absence of an attempt is not evidence of a failed Stop. Ordinary progress
	// is not a reason to begin another reception while an actor may be running.
	if handoff.Diagnostic == "" && nativeExecutionInContext(ctx) != nativeExecutionCompleted {
		return base
	}
	input := ""
	nativePlan, nativeResult := "", ""
	packet := workpacket.RepairPlanReportPacket{ID: handoff.Plan.id, ViewPath: handoff.Plan.view, AnswerPath: handoff.Plan.answer, Binding: handoff.Plan.binding}
	nativePaths := func() error {
		var err error
		nativePlan, nativeResult, err = nativeS9AuthorPaths(root, packet)
		return err
	}
	nativePathFailure := func(err error) string {
		return base + " Original native author contract is unavailable: " + s9NativeAuthorCoordinatesFailure(root, packet, err, true) + "."
	}
	if handoff.Result != nil && filepath.IsAbs(handoff.Result.originalRoot) && filepath.Clean(handoff.Result.originalRoot) != filepath.Clean(root) {
		typed := filepath.Join(handoff.Result.originalRoot, handoff.Result.answer)
		// A coordinate by itself is not a saved input. The producer's actual saved
		// bytes and reached boundary establish which original input to continue.
		if attempt != nil && (attempt.Stage == "typed_result_saved" || attempt.Stage == "typed_result_consumer" || attempt.Stage == "typed_result_missing") && filepath.Clean(attempt.TypedInputRef) == filepath.Clean(typed) && (len(observations[typed]) == 64 || attempt.Stage == "typed_result_missing") {
			input = typed
		}
	}
	if input == "" && !handoff.Native && handoff.Result != nil && filepath.IsAbs(handoff.Result.originalRoot) {
		input = filepath.Join(handoff.Result.originalRoot, handoff.Result.answer)
	}
	if input == "" && handoff.Native && handoff.Plan.id != "" {
		if err := nativePaths(); err != nil {
			return nativePathFailure(err)
		}
		suffix := filepath.FromSlash(nativeResult)
		if attempt != nil && filepath.IsAbs(attempt.InputRef) && strings.HasSuffix(filepath.Clean(attempt.InputRef), string(filepath.Separator)+suffix) {
			source := strings.TrimSuffix(filepath.Clean(attempt.InputRef), string(filepath.Separator)+suffix)
			if source != "" && source != filepath.Clean(root) && (handoff.Result == nil || handoff.Result.originalRoot == "" || filepath.Clean(source) == filepath.Clean(handoff.Result.originalRoot)) {
				input = attempt.InputRef
			}
		}
		if input == "" && handoff.Result != nil && filepath.IsAbs(handoff.Result.originalRoot) && filepath.Clean(handoff.Result.originalRoot) != filepath.Clean(root) {
			input = filepath.Join(handoff.Result.originalRoot, suffix)
		}
	}
	if input == "" {
		return base + " Main has no captured exact original delivery input for technical continuation. Preserve the actual returned brief/absolute Result reference; restore that original source coordinate rather than guessing a directory, owner or replacement responsibility."
	}
	if available, reason := s9OriginalInputAvailable(ctx, input); !available {
		return base + " Original delivery input/source is unavailable at " + input + ": " + reason + ". Main owns this environment/source gap. Preserve the accepted facts and captured consumer cause; no saved original bytes or supported replacement have been established. A hash or recreated directory cannot substitute for the original source."
	}
	if handoff.Native {
		source := ""
		if handoff.Result != nil {
			source = handoff.Result.originalRoot
		}
		if source == "" && handoff.Plan.id != "" {
			if nativeResult == "" {
				if err := nativePaths(); err != nil {
					return nativePathFailure(err)
				}
			}
			suffix := filepath.FromSlash(nativeResult)
			if strings.HasSuffix(input, string(filepath.Separator)+suffix) {
				source = strings.TrimSuffix(input, string(filepath.Separator)+suffix)
			}
		}
		if source != "" {
			if _, err := os.Stat(filepath.Join(source, ".git")); err != nil {
				return base + " Original worktree Git source is unavailable at " + source + ": " + err.Error() + ". Main owns the missing original environment/source; preserve accepted facts and the captured cause. No source replacement has been established."
			}
			if !handoff.PlanAccepted && handoff.Plan.answer != "" {
				if nativePlan == "" {
					if err := nativePaths(); err != nil {
						return nativePathFailure(err)
					}
				}
				if available, reason := s9OriginalInputAvailable(ctx, filepath.Join(source, nativePlan)); !available {
					return base + " Original unreceived PlanReport input is unavailable at " + filepath.Join(source, nativePlan) + ": " + reason + ". Main owns this missing source; preserve the original responsibility and captured consumer cause."
				}
			}
		}
	}
	if consumerAttempt != nil {
		switch consumerAttempt.FailureKind {
		case "author":
			return base + " The original consumer identified a professional omission in " + consumerAttempt.InputRef + ". The original author may correct unaccepted professional input not fixed by a staged/pending transaction and return it through the original delivery path; technical replay cannot substitute changed professional content."
		case "conflict":
			return base + " The original consumer recorded a same-identity conflict. Preserve the receipt and changed input; follow the named original correction/rework."
		}
	}
	return base + " Main owns the system reception continuation. Resolve the concrete source/prerequisite named by the original consumer, then run this exact original receiver: " + s9OriginalConsumeCommand(root, input) + ". This explicit recovery reuses the original saved input and consumer; it requires no Worker wake-up, repeated Stop, replacement packet or edit to accepted work."
}

// A finite availability observation is navigation only. The receiver rechecks
// ownership, exact bytes, canonical worktree and the full Git/source closure.
func s9OriginalInputAvailable(ctx context.Context, input string) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, "current availability unknown: " + err.Error()
	}
	if !filepath.IsAbs(input) {
		return false, "exact absolute input reference is unknown"
	}
	parent, name, err := workpacket.OpenParent(filepath.Dir(input), filepath.Base(input), false)
	if err != nil {
		return false, err.Error()
	}
	defer parent.Close()
	file, err := parent.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return false, err.Error()
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err.Error()
	}
	if !info.Mode().IsRegular() {
		return false, "original input is not a regular file"
	}
	if err := ctx.Err(); err != nil {
		return false, "current availability unknown: " + err.Error()
	}
	return true, ""
}
