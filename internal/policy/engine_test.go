package policy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestLoadReportsMissingPolicy(t *testing.T) {
	_, err := policy.Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "read hook policy") {
		t.Fatalf("Load error = %v, want wrapped read error", err)
	}
}

func TestLoadReportsMalformedPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-policy.json")
	if err := os.WriteFile(path, []byte(`{"mode":`), 0o600); err != nil {
		t.Fatalf("write malformed policy: %v", err)
	}
	_, err := policy.Load(path)
	if err == nil || !strings.Contains(err.Error(), "decode hook policy") {
		t.Fatalf("Load error = %v, want wrapped decode error", err)
	}
}

func TestAllowEnvelopeRemainsStableAndComplete(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	input := policy.Input{
		SessionID: "session-minimal-policy",
		Event:     "PreToolUse",
		AgentID:   "agent-039-03",
		Runtime: policy.RuntimeContext{
			RuntimeID: "loop-REQ-039",
			Revision:  22,
		},
	}
	first := engine.Envelope(input, policy.Decision{Decision: "allow"}, time.Unix(1, 0))
	second := engine.Envelope(input, policy.Decision{Decision: "allow"}, time.Unix(2, 0))

	if first.DecisionID != second.DecisionID {
		t.Fatalf("stable input produced different IDs: %q != %q", first.DecisionID, second.DecisionID)
	}
	if first.Decision != "allow" || first.Reason == "" || first.Retry != "not_applicable" {
		t.Fatalf("incomplete allow envelope: %#v", first)
	}
	if first.RuntimeID == nil || *first.RuntimeID != "loop-REQ-039" {
		t.Fatalf("runtime identity missing from envelope: %#v", first)
	}
}

func TestEngineAccessorsPreserveLoadedMetadata(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	if engine.Mode() != "enforce" {
		t.Fatalf("mode = %q, want enforce", engine.Mode())
	}
	if engine.PolicyVersion() == "" {
		t.Fatal("policy version must remain readable during config migration")
	}
	if !engine.HasRule("HOOK_LOCKED_ARTIFACT_WRITE") {
		t.Fatal("minimal policy must retain the locked_artifact_write rule")
	}
	if !engine.HasRule("HOOK_SQUASH_MERGE") {
		t.Fatal("minimal policy must retain the squash_merge rule")
	}
}

func loadRepositoryPolicy(t *testing.T) *policy.Engine {
	t.Helper()
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load repository policy: %v", err)
	}
	return engine
}
