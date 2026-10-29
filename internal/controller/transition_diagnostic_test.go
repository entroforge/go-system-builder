package controller

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTransitionApplyDiagnosticNormalizesAndBoundsUTF8Line(t *testing.T) {
	reason := "guard contracts_checked failed: missing 契约\r\ninput\n" + string([]byte{0xff}) + strings.Repeat("超长原因", 200)
	diagnostic := transitionApplyDiagnostic("PTR-PLAN-02", reason)

	if !utf8.ValidString(diagnostic) {
		t.Fatalf("diagnostic is not valid UTF-8: %q", diagnostic)
	}
	if strings.ContainsAny(diagnostic, "\r\n") {
		t.Fatalf("diagnostic must be one line, got %q", diagnostic)
	}
	if len([]rune(diagnostic)) > maxTransitionDiagnosticRunes {
		t.Fatalf("diagnostic has %d runes, max is %d", len([]rune(diagnostic)), maxTransitionDiagnosticRunes)
	}
	if !strings.HasPrefix(diagnostic, "transition:PTR-PLAN-02:guard contracts_checked failed:") {
		t.Fatalf("diagnostic lost transition/guard identity: %q", diagnostic)
	}
	if !strings.Contains(diagnostic, "missing 契约 input �") {
		t.Fatalf("diagnostic lost normalized non-ASCII reason: %q", diagnostic)
	}
	if !strings.HasSuffix(diagnostic, transitionDiagnosticTruncated) {
		t.Fatalf("diagnostic must include an explicit truncation marker, got %q", diagnostic)
	}
}

func TestTransitionApplyDiagnosticPreservesShortReason(t *testing.T) {
	got := transitionApplyDiagnostic("PTR-PLAN-02", "guard contracts_checked failed: missing contract")
	want := "transition:PTR-PLAN-02:guard contracts_checked failed: missing contract"
	if got != want {
		t.Fatalf("diagnostic=%q, want %q", got, want)
	}
}
