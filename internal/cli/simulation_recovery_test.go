package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNativeInterruptedReceptionPreservesAnswerAndNamesOriginalRetry(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		err := fmt.Errorf("original writer: %w", cause)
		var diagnostic bytes.Buffer
		if code := nativeS9HandoffFailure("original-worker", "/original/result.json", err, &diagnostic); code != 0 {
			t.Fatal("interrupted return was blocked")
		}
		for _, want := range []string{"not a professional content rejection", "If already accepted", "original consumer owns", "does not establish completion", "/original/result.json"} {
			if !strings.Contains(diagnostic.String(), want) {
				t.Fatalf("missing %q: %s", want, &diagnostic)
			}
		}
		if strings.Contains(diagnostic.String(), "Consumer rejection:") {
			t.Fatal("timeout labeled content rejection")
		}
		for _, forbidden := range []string{"resume the same native Agent", "save the unchanged file again", "change the artifact id", "resume the original professional responsibility", "restore missing or stale sources"} {
			if strings.Contains(diagnostic.String(), forbidden) {
				t.Fatalf("timeout assigned transaction recovery to author: %s", &diagnostic)
			}
		}
	}
	if nativeReceptionInterrupted(errors.New("author omitted required assertion")) {
		t.Fatal("author error treated as interruption")
	}
}

// TestReviewPlanRecoveryHelpAndJSONFilePath pins the current ReviewPlan
// reception boundary. The manual `runtime review-plan` registration /
// revision / revive route is gone: Main saves the Hook-provided ReviewPlan
// questionnaire and the original RegisterPlan consumer receives it, so there
// is no plan registration or submission command, no source-ref / affected-
// surface parameter and no frozen-diff recovery route
// (L3-S7 §7.1, L4-workflow-convergence §"可选素材与恢复观察").
func TestReviewPlanRecoveryHelpAndJSONFilePath(t *testing.T) {
	for _, verb := range []string{"revive", "revise", "register"} {
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"runtime", "review-plan", verb, "--root", ".", "--file", "plan.json"}, nil, &out, &diagnostic); code == 0 {
			t.Fatalf("removed review-plan verb %q is still accepted: %s", verb, &out)
		} else if !strings.Contains(diagnostic.String(), "unknown runtime command") {
			t.Fatalf("removed review-plan verb %q was not reported as an unknown runtime command: %d %s", verb, code, &diagnostic)
		}
	}
	// The generic runtime help must not document the removed plan verbs any
	// more: no manual plan registration, no source-ref / affected-surface
	// parameters and no blanket file-restore recovery.
	var planHelp, planDiagnostic bytes.Buffer
	if code := Run([]string{"runtime", "review-plan", "revive", "--help"}, nil, &planHelp, &planDiagnostic); code != 0 {
		t.Fatalf("runtime help failed: %d %s", code, &planDiagnostic)
	}
	for _, unwanted := range []string{"JSON file", "--source-ref", "--affected-surface", "does not restore files", "never use blanket reset/clean/restore"} {
		if strings.Contains(planHelp.String(), unwanted) {
			t.Fatalf("runtime help still documents the removed plan option %q: %s", unwanted, &planHelp)
		}
	}
	// The runtime command surface must not advertise the removed plan
	// registration / recovery verbs any more.
	var runtimeHelp, runtimeDiagnostic bytes.Buffer
	if code := Run([]string{"runtime"}, nil, &runtimeHelp, &runtimeDiagnostic); code == 0 {
		t.Fatalf("runtime without a verb must fail: %s", &runtimeHelp)
	}
	if strings.Contains(runtimeDiagnostic.String(), "review-plan") || strings.Contains(runtimeDiagnostic.String(), "review-result") {
		t.Fatalf("runtime usage still advertises the removed plan verbs: %s", &runtimeDiagnostic)
	}
	if !strings.Contains(runtimeDiagnostic.String(), "review-continue") || !strings.Contains(runtimeDiagnostic.String(), "audit-prepare") {
		t.Fatalf("runtime usage lost the named S7 recovery entries: %s", &runtimeDiagnostic)
	}
	// The current contract: Main completes the Hook-provided ReviewPlan
	// questionnaire and saves it for the original RegisterPlan consumer; the
	// named recovery entries stay review-continue / audit-prepare.
	var catalog, catalogDiagnostic bytes.Buffer
	if code := Run([]string{"actions"}, nil, &catalog, &catalogDiagnostic); code != 0 {
		t.Fatalf("actions catalog failed: %d %s", code, &catalogDiagnostic)
	}
	for _, want := range []string{
		"Main completes the Hook-provided ReviewPlan questionnaire and saves it for the original RegisterPlan consumer.",
		"runtime review-continue",
		"runtime audit-prepare",
	} {
		if !strings.Contains(catalog.String(), want) {
			t.Fatalf("action catalog missing %q: %s", want, &catalog)
		}
	}
	if strings.Contains(catalog.String(), "runtime review-plan") {
		t.Fatalf("action catalog still advertises the removed plan registration command: %s", &catalog)
	}
}
