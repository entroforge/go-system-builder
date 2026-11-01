package req039_test

import (
	"bytes"
	"strings"
	"testing"
)

func runRecheckCLI(t *testing.T, root string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args = append(args, "--root", root)
	if code := runCLI(t, args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("CLI %v failed: code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
	}
	return stdout.String() + stderr.String()
}
