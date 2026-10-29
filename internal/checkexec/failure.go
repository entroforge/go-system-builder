// Package checkexec describes actual check capabilities and execution failures.
// It grants no approval and never interprets command output as a product oracle.
package checkexec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

type Failure struct {
	Code           string `json:"code"`
	Category       string `json:"category"`
	Owner          string `json:"owner"`
	Check          string `json:"check"`
	Observed       string `json:"observed"`
	Repair         string `json:"repair"`
	RetryCondition string `json:"retry_condition"`
	ReceiptRef     string `json:"receipt_ref,omitempty"`
	LogRef         string `json:"log_ref,omitempty"`
	cause          error
}

func (f *Failure) Error() string { return fmt.Sprintf("%s: %s; %s", f.Code, f.Observed, f.Repair) }
func (f *Failure) Unwrap() error { return f.cause }
func New(category, owner, command string, err error) *Failure {
	code := map[string]string{"capability_missing": "CHECK_CAPABILITY_MISSING", "process_exit": "CHECK_PROCESS_EXIT", "timeout": "CHECK_TIMEOUT", "cancelled": "CHECK_CANCELLED", "diagnostic_io": "CHECK_DIAGNOSTIC_IO", "input_drift": "CHECK_INPUT_DRIFT", "unclassified_failure": "CHECK_UNCLASSIFIED"}[category]
	if code == "" {
		category = "unclassified_failure"
		code = "CHECK_UNCLASSIFIED"
	}
	repair := map[string]string{"capability_missing": "restore the declared runner/tool through the original environment owner; do not switch a ready execution", "process_exit": "inspect the original output and check contract; exit status alone does not prove a product assertion failed", "timeout": "preserve the original candidate and diagnostics; inspect resources or the approved time limit", "cancelled": "preserve the original candidate and diagnostics; retry through the original check entry", "diagnostic_io": "restore diagnostic storage and retry the original check without discarding its candidate", "input_drift": "preserve the check and candidate; revalidate the original tested input before retry", "unclassified_failure": "inspect the original error and diagnostics before classifying or retrying"}[category]
	observed := "unknown check failure"
	if err != nil {
		observed = err.Error()
	}
	return &Failure{Code: code, Category: category, Owner: owner, Check: command, Observed: observed, Repair: repair, RetryCondition: "original inputs, responsibility and execution authority are still valid", cause: err}
}
func Classify(ctx context.Context, owner, command string, err error) *Failure {
	if err == nil {
		return nil
	}
	var existing *Failure
	if errors.As(err, &existing) {
		return existing
	}
	category := "unclassified_failure"
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		category = "timeout"
	case errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled):
		category = "cancelled"
	default:
		var exit *exec.ExitError
		var path *exec.Error
		if errors.As(err, &exit) {
			category = "process_exit"
		} else if errors.As(err, &path) {
			category = "capability_missing"
		}
	}
	return New(category, owner, command, err)
}
