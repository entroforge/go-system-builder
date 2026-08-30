package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/recovery"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestRecoveryErrorCodeClassifiesWrappedRecoveryErrors(t *testing.T) {
	reqValidation := &recovery.ValidationError{Code: recovery.ErrREQNotLocked, Field: "status"}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "req invalid uses validation error chain",
			err:  fmt.Errorf("inspect: %w", reqValidation),
			want: recoveryREQInvalidCode,
		},
		{
			name: "plan invalid uses inventory sentinel",
			err:  fmt.Errorf("validate plan: %w", recovery.ErrInvalidInventory),
			want: recoveryPlanInvalidCode,
		},
		{
			name: "source conflict uses runtime sentinel",
			err:  fmt.Errorf("apply: %w", runtime.ErrRecoveryConflict),
			want: recoverySourceConflictCode,
		},
		{
			name: "source conflict uses cli sentinel",
			err:  fmt.Errorf("replay: %w", errRecoverySourceConflict),
			want: recoverySourceConflictCode,
		},
		{
			name: "gate unknown uses recovery sentinel",
			err:  fmt.Errorf("replay: %w", errRecoveryGateUnknown),
			want: recoveryGateUnknownCode,
		},
		{
			name: "apply pending uses runtime sentinel",
			err:  fmt.Errorf("retry: %w", runtime.ErrRecoveryPending),
			want: recoveryApplyPendingCode,
		},
		{
			name: "input drift remains stable",
			err:  fmt.Errorf("apply: %w", runtime.ErrRecoveryInputDrift),
			want: recoveryInputDriftCode,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := recoveryErrorCode(tc.err); got != tc.want {
				t.Fatalf("recoveryErrorCode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRecoveryFailureFormatsStableCodeWithoutBreakingCauseText(t *testing.T) {
	err := fmt.Errorf("candidate was changed: %w", runtime.ErrRecoveryInputDrift)
	got := formatRecoveryFailure("runtime recover apply", err)
	if !strings.Contains(got, recoveryInputDriftCode) {
		t.Fatalf("formatted recovery failure = %q, missing %s", got, recoveryInputDriftCode)
	}
	if !strings.Contains(got, err.Error()) {
		t.Fatalf("formatted recovery failure = %q, missing cause %q", got, err.Error())
	}
}

func TestRecoveryAlreadyAppliedCodeRemainsSuccessProjection(t *testing.T) {
	if recoveryAlreadyApplied != "LOOP_RECOVERY_ALREADY_APPLIED" {
		t.Fatalf("already-applied code changed to %q", recoveryAlreadyApplied)
	}
	if got := recoveryErrorCode(errors.New(recoveryAlreadyApplied)); got != "" {
		t.Fatalf("already-applied success text must not be classified as error %q", got)
	}
}
