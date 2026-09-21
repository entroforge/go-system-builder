package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEvidenceReferenceClosureIsOrderedRecoverableAndPreservesSubjects(t *testing.T) {
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("child.json", `{"conclusion":"pass","merged":true}`)
	write("manifest.json", `{"evidence":[{"path":"child.json","sha256":"old"}],"subject_refs":[{"path":"child.json","sha256":"frozen"}]}`)
	write("outer.json", `{"kind":"acceptance","audit_manifest_path":"manifest.json","audit_manifest_sha256":"old","conclusion":"pass"}`)
	row := func(path, kind string) any {
		return map[string]any{"path": path, "kind": kind, "status": "valid", "baseline_generation": 1, "review_round": 1}
	}
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "review": map[string]any{"round": 1}, "evidence": []any{row("outer.json", "acceptance"), row("child.json", "finding")}}
	read := func(path string) ([]byte, error) { return os.ReadFile(filepath.Join(root, path)) }
	writes, resolved, err := prepareEvidenceClosure(root, state, map[string]bool{"acceptance": true, "finding": true}, read, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || writes[0].Path != "manifest.json" || writes[1].Path != "outer.json" {
		t.Fatalf("not inner-first: %#v", writes)
	}
	// Crash after the inner file: replay must finish, then become a no-op.
	if err := applyFingerprintArtifacts(root, writes[:1]); err != nil {
		t.Fatal(err)
	}
	if err := applyFingerprintArtifacts(root, writes); err != nil {
		t.Fatal(err)
	}
	if err := applyFingerprintArtifacts(root, writes); err != nil {
		t.Fatal(err)
	}
	var manifest, outer map[string]any
	_ = json.Unmarshal(resolved["manifest.json"], &manifest)
	_ = json.Unmarshal(resolved["outer.json"], &outer)
	if outer["audit_manifest_sha256"] != sha256Hex(resolved["manifest.json"]) {
		t.Fatal("outer hash not rebound")
	}
	if manifest["evidence"].([]any)[0].(map[string]any)["sha256"] != sha256Hex(resolved["child.json"]) {
		t.Fatal("inner evidence hash not rebound")
	}
	if manifest["subject_refs"].([]any)[0].(map[string]any)["sha256"] != "frozen" {
		t.Fatal("frozen subject changed")
	}
	if outer["conclusion"] != "pass" {
		t.Fatal("conclusion changed")
	}
	again, _, err := prepareEvidenceClosure(root, state, map[string]bool{"acceptance": true, "finding": true}, read, func(string) bool { return true })
	if err != nil || len(again) != 0 {
		t.Fatalf("non-idempotent: %v %v", again, err)
	}
}
