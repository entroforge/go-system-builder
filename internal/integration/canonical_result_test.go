package integration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalizeGitPathOutputPreservesWhitespaceAndSorts(t *testing.T) {
	output := "z name.txt\x00line\nbreak.go\x00 leading and trailing \x00tab\tname\x00"
	paths, err := canonicalizeGitPathOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{" leading and trailing ", "line\nbreak.go", "tab\tname", "z name.txt"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	digest, err := changedPathsDigest(paths)
	if err != nil {
		t.Fatal(err)
	}
	reorderedDigest, err := changedPathsDigest([]string{want[3], want[1], want[0], want[2]})
	if err != nil {
		t.Fatal(err)
	}
	if digest != reorderedDigest {
		t.Fatalf("pathset digest depends on input ordering: %s != %s", digest, reorderedDigest)
	}
	if !samePathSet(paths, []string{want[3], want[1], want[0], want[2]}) {
		t.Fatal("samePathSet() rejected the same paths in a different order")
	}
}

func TestCanonicalizeGitPathOutputRejectsAmbiguousPaths(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
	}{
		{name: "unterminated", output: "path"},
		{name: "empty entry", output: "a\x00\x00"},
		{name: "duplicate", output: "a\x00a\x00"},
		{name: "parent path", output: "../outside\x00"},
		{name: "absolute path", output: "/outside\x00"},
		{name: "invalid utf8", output: string([]byte{0xff, 'x', 0})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := canonicalizeGitPathOutput(test.output); err == nil {
				t.Fatal("canonicalizeGitPathOutput() accepted ambiguous/non-local path bytes")
			}
		})
	}
	if paths, err := canonicalizeGitPathOutput(""); err != nil || paths == nil || len(paths) != 0 {
		t.Fatalf("empty actual diff must be a non-nil empty set: %#v, %v", paths, err)
	}
}

func TestCanonicalGitChangedPathsUsesExactGitRange(t *testing.T) {
	runner := &canonicalPathsRunner{output: "src/z.go\x00 docs/answer.json\x00src/a.go\x00"}
	restore := withRunner(runner)
	defer restore()

	paths, digest, err := canonicalGitChangedPaths(context.Background(), "main", "base-sha", "source-sha")
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"diff", "--no-renames", "--name-only", "-z", "base-sha..source-sha"}
	if runner.root != "main" || !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("Git call = root %q args %#v, want root main args %#v", runner.root, runner.args, wantArgs)
	}
	wantPaths := []string{" docs/answer.json", "src/a.go", "src/z.go"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths = %#v, want %#v", paths, wantPaths)
	}
	wantDigest, err := changedPathsDigest(wantPaths)
	if err != nil || digest != wantDigest {
		t.Fatalf("digest = %q, want %q (err %v)", digest, wantDigest, err)
	}
}

type canonicalPathsRunner struct {
	root   string
	args   []string
	output string
}

func (r *canonicalPathsRunner) Run(_ context.Context, root string, args ...string) (string, error) {
	r.root = root
	r.args = append([]string(nil), args...)
	if !strings.HasPrefix(strings.Join(args, " "), "diff --no-renames --name-only -z ") {
		return "", errors.New("unexpected git command")
	}
	return r.output, nil
}

func (r *canonicalPathsRunner) RunStdin(context.Context, string, string, ...string) (string, error) {
	return "", errors.New("unexpected git stdin command")
}
