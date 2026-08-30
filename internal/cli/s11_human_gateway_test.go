package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestRuntimeHumanDecisionRequiresFailClosedInputs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "human-decision"}, strings.NewReader(""), &stdout, &stderr)

	if code != 2 {
		t.Fatalf("missing human decision inputs exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--disposition") ||
		!strings.Contains(stderr.String(), "--expected-revision") ||
		!strings.Contains(stderr.String(), "--actor") ||
		!strings.Contains(stderr.String(), "--decision-evidence") {
		t.Fatalf("missing-input error is not actionable: %q", stderr.String())
	}
}

func TestRuntimeHumanDecisionRejectsUnsupportedDispositionBeforeRuntimeAccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "human-decision",
		"--root", t.TempDir(),
		"--disposition", "arbitrary-target",
		"--expected-revision", "7",
		"--actor", "release-owner",
		"--decision-evidence", "decision-104",
	}, strings.NewReader(""), &stdout, &stderr)

	if code != 2 {
		t.Fatalf("unsupported disposition exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "approve") || !strings.Contains(stderr.String(), "reject_defect") {
		t.Fatalf("unsupported-disposition error must list the finite choices: %q", stderr.String())
	}
}

func TestRuntimeHumanDecisionRejectDefectRequiresFindingEvidence(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "human-decision",
		"--root", t.TempDir(),
		"--disposition", "reject_defect",
		"--expected-revision", "7",
		"--actor", "release-owner",
		"--decision-evidence", "decision-104",
	}, strings.NewReader(""), &stdout, &stderr)

	if code != 2 {
		t.Fatalf("missing finding evidence exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--finding-evidence") {
		t.Fatalf("missing finding-evidence error is not actionable: %q", stderr.String())
	}
}
