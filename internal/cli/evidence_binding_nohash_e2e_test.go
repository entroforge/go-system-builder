package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestEvidenceBindingRepairUsesOriginalIDsWithoutDigest(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "document_verification", "", 7)
	state["review"] = map[string]any{"round": 0, "clean_round": nil}
	state["bound_req"].(map[string]any)["sha256"] = "legacy-wrong"
	envelope := fixtures.EvidenceEnvelope(state, "actual-result", "document_review", "reviewer", "DV-SPEC-CONSISTENCY", "pass", nil)
	entry := fixtures.WriteEvidenceEnvelope(t, root, state, "registration", "document_review", "reviewer", "DV-SPEC-CONSISTENCY", envelope, []any{})
	entry["sha256"] = "historical-does-not-match"
	state["evidence"] = []any{entry}
	fixtures.WriteState(t, root, state)
	var out, stderr bytes.Buffer
	run := func(args ...string) int {
		out.Reset()
		stderr.Reset()
		return cli.Run(append([]string{"runtime", "repair-evidence-binding", "--root", root, "--id", "registration"}, args...), nil, &out, &stderr)
	}
	before, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if code := run(); code != 0 {
		t.Fatalf("preview exit %d: %s", code, stderr.String())
	}
	if bytes.Contains(out.Bytes(), []byte("sha256")) || bytes.Contains(out.Bytes(), []byte("file_hashes")) {
		t.Fatalf("preview registered a digest: %s", out.String())
	}
	after, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("preview changed Runtime: %v", err)
	}
	var oldPlan map[string]any
	if err := json.Unmarshal(out.Bytes(), &oldPlan); err != nil {
		t.Fatal(err)
	}
	oldPlan["reason"] = "registration ID differs from fingerprinted envelope evidence_id"
	oldPlan["state_sha256"] = "arbitrary-old-value"
	oldPlan["file_hashes"] = map[string]any{"review": "stale"}
	data, err := json.Marshal(oldPlan)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, "old-plan.json")
	if err := os.WriteFile(planPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run("--apply-plan", planPath); code != 0 {
		t.Fatalf("apply exit %d: %s", code, stderr.String())
	}
	updated := fixtures.ReadState(t, root)
	row := updated["evidence"].([]any)[0].(map[string]any)
	if row["status"] != "invalid" || row["id"] != "registration" {
		t.Fatalf("wrong original registration changed: %v", row)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry["path"].(string)))); err != nil {
		t.Fatalf("original envelope removed: %v", err)
	}
}
