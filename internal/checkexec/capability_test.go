package checkexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMainCapabilityLiteralToolsMissingAndOpaque(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0700)
	for _, tool := range []string{"sh", "project-test"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\nexit 93\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	report := MainCapabilities(context.Background(), root, []string{"project-test --unit", "test -f x", "unknown-test --unit", "project-test && another", "locked:review"})
	if report.Available || report.FullyVerified || len(report.Checks) != 4 || report.Checks[0].Status != "entrance_available" || report.Checks[2].Failure.Category != "capability_missing" || report.Checks[3].Status != "unverified" {
		t.Fatal("incorrect partial readiness", report)
	}
	var failure *Failure
	if !errors.As(report.Error(), &failure) || failure.Owner != "environment owner" {
		t.Fatal("lost typed repair owner")
	}
	t.Setenv("PATH", root)
	if r := MainCapabilities(context.Background(), root, []string{"true"}); r.Available || r.Error() == nil {
		t.Fatal("missing interpreter reported ready")
	}
	if r := MainCapabilities(context.Background(), root, []string{"locked:review"}); !r.Available || len(r.Checks) != 0 {
		t.Fatal("non-executable check required a shell")
	}
}
func TestLiteralEntranceQuotesAndNoExecution(t *testing.T) {
	cases := []struct {
		command, tool string
		ok            bool
	}{
		{"'./目录/my test' 'literal $value'", "./目录/my test", true},
		{"exec sleep 2", "sleep", true}, {"GOFLAGS=x go test", "", false}, {"go test && echo x", "", false}, {"go test \"$HOME\"", "", false}, {"go test $(touch marker)", "", false}, {"go test > result", "", false}, {"\"unfinished", "", false},
	}
	for _, c := range cases {
		tool, ok := LiteralEntrance(c.command)
		if tool != c.tool || ok != c.ok {
			t.Fatalf("%q: %q %v", c.command, tool, ok)
		}
	}
	root := t.TempDir()
	tool := filepath.Join(root, "目录/my test")
	os.MkdirAll(filepath.Dir(tool), 0700)
	os.WriteFile(tool, []byte("#!/bin/sh\ntouch marker\n"), 0700)
	r := MainCapabilities(context.Background(), root, []string{"'./目录/my test'"})
	if !r.Available || r.Checks[0].Status != "entrance_available" {
		t.Fatal("quoted path unavailable", r)
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
		t.Fatal("preflight executed a project tool")
	}
}
func TestFailurePreservesCancellationAndUnclassifiedError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failure := Classify(ctx, "main", "check", context.Canceled)
	if failure.Category != "cancelled" || !errors.Is(failure, context.Canceled) {
		t.Fatal("lost cancellation cause")
	}
	unknown := errors.New("opaque failure")
	failure = Classify(context.Background(), "main", "check", unknown)
	if failure.Category != "unclassified_failure" || !errors.Is(failure, unknown) {
		t.Fatal("invented product assertion failure")
	}
}
