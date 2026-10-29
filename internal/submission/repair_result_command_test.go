package submission

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Execute the actual receipt command builder through a real shell and capture
// argv as NUL-separated bytes. Checking rendered quote characters alone cannot
// detect command substitution or altered newlines when Main copies the command.
func TestRepairResultReceiptIntegrateCommandPreservesLiteralArgv(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("shell argv contract requires bash")
	}
	root := filepath.Join(t.TempDir(), "project ' $(printf ROOT_EXPANDED) `printf ROOT_BACKTICK` $HOME\nnext")
	launcher := filepath.Join(root, ".claude/bin/loop-harness")
	if err := os.MkdirAll(filepath.Dir(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nprintf '%s\\000' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	assignment := "assignment ' $(printf ASSIGNMENT_EXPANDED) `printf ASSIGNMENT_BACKTICK`\nnext"
	owner := "owner '$HOME $(printf OWNER_EXPANDED) `printf OWNER_BACKTICK`\nnext"
	want := []string{"runtime", "workspace", "integrate", "--root", root, "--assignment", assignment, "--agent", owner}
	readArgv := func(command string) []string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bash, "--noprofile", "--norc", "-c", command)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("literal command execution failed: %v stderr=%s", err, &stderr)
		}
		parts := bytes.Split(out, []byte{0})
		if len(parts) == 0 || len(parts[len(parts)-1]) != 0 {
			t.Fatal("argv recorder did not terminate its NUL-separated output")
		}
		var argv []string
		for _, part := range parts[:len(parts)-1] {
			argv = append(argv, string(part))
		}
		return argv
	}
	command := repairResultIntegrateCommand(root, assignment, owner)
	if got := readArgv(command); !reflect.DeepEqual(got, want) {
		t.Fatalf("copied receipt command changed original argv:\ngot=%q\nwant=%q", got, want)
	}
	// Mutation witness for the former %q rendering: a quoted launcher alone
	// cannot protect the old double-quoted source/owner arguments from the shell.
	literalLauncher := "'" + strings.ReplaceAll(launcher, "'", `'\''`) + "'"
	unsafe := fmt.Sprintf("%s runtime workspace integrate --root %q --assignment %q --agent %q", literalLauncher, root, assignment, owner)
	if got := readArgv(unsafe); reflect.DeepEqual(got, want) {
		t.Fatal("negative witness failed to expose the former Go-quoted shell arguments")
	}
}
