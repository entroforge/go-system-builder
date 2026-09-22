package semantic

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyAgentDefinitionReferenceRemainsContained(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude/agents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := []byte("agent\r\n")
	if err := os.WriteFile(filepath.Join(dir, "qa.md"), body, 0600); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"agents/qa.md", "agents/qa.md#role", `agents\qa.md`} {
		if err := checkReachablePath(root, "agents[qa].definition_ref", ref); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range []string{"../outside.md", `..\outside.md`, `C:\agents\qa.md`, "/agents/qa.md"} {
		if err := checkReachablePath(root, "agents[qa].definition_ref", ref); err == nil {
			t.Fatalf("accepted %s", ref)
		}
	}
	if err := checkReachablePath(root, "evidence[qa]", "agents/qa.md"); err == nil {
		t.Fatal("alias broadened to arbitrary evidence")
	}
	if err := checkReachableFingerprint(root, "evidence[qa]", `.claude\agents\qa.md#role`, fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
		t.Fatal(err)
	}
	if err := checkReachableFingerprint(root, "evidence[qa]", ".claude/agents/qa.md", "wrong"); err == nil {
		t.Fatal("lost fingerprint enforcement")
	}
	outside := filepath.Join(t.TempDir(), "external.md")
	if err := os.WriteFile(outside, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "external.md")); err != nil {
		t.Fatal(err)
	}
	if err := checkReachablePath(root, "agents[qa].definition_ref", "agents/external.md"); err == nil {
		t.Fatal("alias escaped through symlink")
	}
}
