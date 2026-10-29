package cli

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestS9HandoffCopyableCommandPreservesLiteralCoordinates(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell roundtrip requires sh")
	}
	root := "/tmp/project $(printf expanded) `printf expanded` 'quotes' and spaces"
	assignment, owner := "assignment'one", "owner $literal"
	text := s9CandidateAction(root, assignment, owner)
	command, found := strings.CutPrefix(text, "The Worker candidate is recorded. Main next runs runtime workspace integrate using this exact command: ")
	if !found {
		t.Fatal("missing copyable command", text)
	}
	command, _, found = strings.Cut(command, ". Select necessary checks")
	if !found {
		t.Fatal("missing command boundary", text)
	}
	// Parse the delivered command as shell arguments without executing runtime.
	output, err := exec.Command(sh, "-c", "set -- "+command+"; printf '%s\\0' \"$@\"").Output()
	if err != nil {
		t.Fatal(err)
	}
	parts := bytes.Split(bytes.TrimSuffix(output, []byte{0}), []byte{0})
	var actual []string
	for _, part := range parts {
		actual = append(actual, string(part))
	}
	expected := []string{filepath.Join(root, ".claude/bin/loop-harness"), "runtime", "workspace", "integrate", "--root", root, "--assignment", assignment, "--agent", owner}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("delivered coordinates were interpreted by the shell: got %q want %q", actual, expected)
	}
}
