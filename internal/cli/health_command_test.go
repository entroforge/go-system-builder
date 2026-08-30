package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/metrics"
)

func TestHealthCommandIsSeparateFromDoctorStructuralChecks(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"health", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("healthy runtime command failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "runtime health: healthy") {
		t.Fatalf("health output: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "schemas") {
		t.Fatal("health must not present itself as a structural doctor")
	}

	if err := metrics.RecordCASConflict(root); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"health", "--root", root, "--fail-on-degraded"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("fail-on-degraded code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "runtime health: degraded") || !strings.Contains(stdout.String(), "runtime reconcile") {
		t.Fatalf("degraded health output must contain recovery guidance: %s", stdout.String())
	}
}
