package hookctx

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestSameCheckProfilesComparesActualChecks(t *testing.T) {
	old := []checkexec.Profile{{ID: "check-1", Command: "go test ./...", Dependencies: []checkexec.FileRef{{Path: "src/file.go"}}, Fixtures: []checkexec.FileRef{{Path: "testdata/input"}}}}
	current := []checkexec.Profile{{ID: "check-1", Command: "go test ./...", Dependencies: []checkexec.FileRef{{Path: "src/file.go"}}, Fixtures: []checkexec.FileRef{{Path: "testdata/input"}}}}
	if !sameCheckProfiles(old, current) {
		t.Fatal("identical source paths must preserve the original profile")
	}
	current[0].Command = "go test ./other"
	if sameCheckProfiles(old, current) {
		t.Fatal("different actual check commands must remain distinct")
	}
}
