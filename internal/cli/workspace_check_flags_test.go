package cli

import (
	"bytes"
	"testing"
)

func TestWorkspaceCheckRequiresExactlyOneNonnegativeIndex(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--check", "0", "--owned-check", "0"},
		{"--check", "0", "--owned-check", "-1"},
		{"--owned-check", "-1"},
		{"--check", "-2"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runWorkspaceCheck(args, &stdout, &stderr); code != 2 {
			t.Fatalf("invalid index selection %v returned %d: %s", args, code, stderr.String())
		}
	}
}
