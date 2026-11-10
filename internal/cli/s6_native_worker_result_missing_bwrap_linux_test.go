package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Reuse the original two-check Result/replay/source-change chain. Only runner
// invocations receive a PATH without bwrap; setup and all consumers retain the
// host PATH. This adds one fixture and reuses its two original checks.
// The original host test remains available on every supported platform.
func TestNativeWorkerCheckResultMissingBwrapPreservesEmptyLogAndBlockedResult(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	testNativeWorkerCheckResultPersistsDurableFacts(t, bin)
}
