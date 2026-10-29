package checkexec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestObserveProfileLocalExecutableIgnoresCopyCoordinateButBindsBytes(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("executable script fixture uses POSIX modes")
	}
	profile := Profile{ID: "local-runner", Version: "v1.0.0", CheckIndex: 0, Command: "./tools/check.sh", Location: "worker", TimeoutSeconds: 30, EnvironmentPolicy: "declared_only", Environment: []EnvFact{}, Dependencies: []FileRef{}, Fixtures: []FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		if err := os.Mkdir(filepath.Join(root, "tools"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "tools/check.sh"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	first, err := ObserveProfile(context.Background(), roots[0], profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ObserveProfile(context.Background(), roots[1], profile)
	if err != nil {
		t.Fatal(err)
	}
	if first.RunnerDigest != second.RunnerDigest || first.SubjectDigest != second.SubjectDigest {
		t.Fatal("identical checked executables must survive a changed scratch coordinate")
	}
	if err := os.WriteFile(filepath.Join(roots[1], "tools/check.sh"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	changed, err := ObserveProfile(context.Background(), roots[1], profile)
	if err != nil {
		t.Fatal(err)
	}
	if changed.RunnerDigest == first.RunnerDigest || changed.SubjectDigest == first.SubjectDigest {
		t.Fatal("changed executable bytes must invalidate both runner and tree facts")
	}
}
