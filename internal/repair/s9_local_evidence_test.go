package repair

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9LocalEvidenceCapturesWorkerBytesWithoutChangingAnswer(t *testing.T) {
	mainRoot, workerRoot := t.TempDir(), t.TempDir()
	for root, contents := range map[string]string{mainRoot: "Main bytes", workerRoot: "Worker observations"} {
		if err := os.MkdirAll(filepath.Join(root, "logs"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "logs/check.txt"), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	digest := workpacket.Digest([]byte("Worker observations"))
	refs := []string{"logs/check.txt", "path:logs/check.txt#sha256=" + digest, filepath.Join(workerRoot, "logs/check.txt"), "test://actual-run", "urn:observation:actual/run", "legacy-evidence-id"}
	raw, _ := json.Marshal(map[string]any{"evidence_refs": refs})
	original := append([]byte{}, raw...)
	captures, err := captureS9LocalEvidence(mainRoot, workerRoot, "packet-test", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(captures) != 3 || !bytes.Equal(raw, original) {
		t.Fatalf("capture altered author payload or captured URI/ID: %s %+v", raw, captures)
	}
	replay, err := captureS9LocalEvidence(mainRoot, workerRoot, "packet-test", raw)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(captures)
	b, _ := json.Marshal(replay)
	if !bytes.Equal(a, b) {
		t.Fatalf("capture replay changed immutable binding: %s / %s", a, b)
	}
	if err := os.RemoveAll(workerRoot); err != nil {
		t.Fatal(err)
	}
	for _, capture := range captures {
		data, err := workpacket.ReadFile(mainRoot, capture.Path, repairResultAnswerLimit)
		if err != nil || string(data) != "Worker observations" || workpacket.Digest(data) != capture.SHA256 {
			t.Fatalf("capture did not survive Worker cleanup: %s %v", data, err)
		}
	}
}

func TestS9LocalEvidenceMissingSourceDoesNotUseMain(t *testing.T) {
	mainRoot, workerRoot := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(mainRoot, "check.log"), []byte("unrelated Main log"), 0600); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"evidence_refs":["path:check.log"]}`)
	if _, err := captureS9LocalEvidence(mainRoot, workerRoot, "packet-test", raw); err == nil || !strings.Contains(err.Error(), workerRoot) || !strings.Contains(err.Error(), "source gap") {
		t.Fatalf("missing Worker source did not identify recoverable gap: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workerRoot, "check.log"), []byte("restored observation"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureS9LocalEvidence(mainRoot, workerRoot, "packet-test", raw); err != nil {
		t.Fatalf("restored source cannot retry: %v", err)
	}
	for _, ref := range []string{"path:../outside.log", filepath.Join(mainRoot, "check.log"), "path:check.log#sha256=" + strings.Repeat("0", 64)} {
		data, _ := json.Marshal(map[string]any{"evidence_refs": []string{ref}})
		if _, err := captureS9LocalEvidence(mainRoot, workerRoot, "packet-test", data); err == nil {
			t.Fatalf("accepted outside or mismatched source %q", ref)
		}
	}
	legacy := json.RawMessage(`{"evidence_refs":["legacy-id","test://not-a-file","urn:actual/run"]}`)
	if refs, err := captureS9LocalEvidence(mainRoot, workerRoot, "packet-test", legacy); err != nil || len(refs) != 0 {
		t.Fatalf("legacy identifiers became mandatory files: %+v %v", refs, err)
	}
}

func TestS9LocalEvidenceDoesNotCopyCredentialFiles(t *testing.T) {
	mainRoot, workerRoot := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(workerRoot, ".env"), []byte("TOKEN=fixture-only"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureS9LocalEvidence(mainRoot, workerRoot, "packet-test", json.RawMessage(`{"evidence_refs":["path:.env"]}`)); err == nil || !strings.Contains(err.Error(), "excludes") || strings.Contains(err.Error(), "TOKEN=") {
		t.Fatalf("credential source not protected without exposing bytes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mainRoot, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("credential capture wrote an archive: %v", err)
	}
}
