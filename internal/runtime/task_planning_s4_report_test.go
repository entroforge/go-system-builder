package runtime

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestS4ReportReaderUsesEvidencePathRulesAndFullBytes(t *testing.T) {
	root := t.TempDir()
	// Larger than the former 1 MiB and 4 MiB consumer limits.
	data := bytes.Repeat([]byte("x"), 5<<20)
	if err := os.WriteFile(filepath.Join(root, "report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("report.json", filepath.Join(root, "inside.json")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside.json")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"report.json", "inside.json", "./report.json"} {
		got, err := ReadTaskPlanningReport(root, path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("full report %q: bytes=%d err=%v", path, len(got), err)
		}
	}
	for _, path := range []string{"", "../outside.json", outside, "outside.json", "missing.json"} {
		_, readErr := ReadTaskPlanningReport(root, path)
		_, oldErr := safeEvidencePath(root, path)
		if readErr == nil || oldErr == nil || readErr.Error() != oldErr.Error() {
			t.Fatalf("path rule %q: read=%v writer=%v", path, readErr, oldErr)
		}
	}
}
