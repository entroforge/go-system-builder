package policy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/schema"
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

func TestEnvelopeCarriesHookEvaluationElapsedMilliseconds(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	envelope := engine.Envelope(policy.Input{
		SessionID: "session-timing",
		Event:     "PreToolUse",
	}, policy.Decision{Decision: "allow", ElapsedMS: 17}, time.Unix(1, 0))
	if envelope.ElapsedMS != 17 {
		t.Fatalf("elapsed_ms = %d, want 17", envelope.ElapsedMS)
	}
}

func TestEnvelopeDecisionIDSeparatesNativeToolUses(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	base := policy.Input{SessionID: "session-native", Event: "PostToolUseFailure"}
	first := engine.Envelope(base, policy.Decision{Decision: "audit"}, time.Unix(1, 0))
	secondInput := base
	secondInput.ToolUseID = "toolu-2"
	second := engine.Envelope(secondInput, policy.Decision{Decision: "audit"}, time.Unix(1, 0))
	if first.DecisionID == second.DecisionID {
		t.Fatalf("distinct tool uses must receive distinct decision IDs: %q", first.DecisionID)
	}
}

func TestEnvelopeDecisionIDSeparatesDistinctNativeEventPayloads(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	first := engine.Envelope(policy.Input{
		SessionID: "session-config",
		Event:     "ConfigChange",
		Source:    "project_settings",
		FilePath:  "docs/hook-policy.json",
	}, policy.Decision{Decision: "audit"}, time.Unix(1, 0))
	second := engine.Envelope(policy.Input{
		SessionID: "session-config",
		Event:     "ConfigChange",
		Source:    "project_settings",
		FilePath:  "settings.json",
	}, policy.Decision{Decision: "audit"}, time.Unix(1, 0))
	if first.DecisionID == second.DecisionID {
		t.Fatalf("distinct native event payloads must receive distinct decision IDs: %q", first.DecisionID)
	}
}

func TestEnvelopeRuleIDsSatisfyHookDecisionSchema(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	envelope := engine.Envelope(policy.Input{
		SessionID: "session-schema",
		Event:     "PreToolUse",
	}, policy.Decision{
		Decision:       "block",
		RuleID:         policy.RuleLockedArtifactWrite,
		MatchedRuleIDs: []string{policy.RuleLockedArtifactWrite},
		Reason:         "locked artifact write",
		Missing:        []string{},
		Recovery:       []string{"create a new version"},
		Retry:          "never",
		HumanRequired:  true,
	}, time.Unix(1, 0))
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("hook-decision.schema.json", data); err != nil {
		t.Fatalf("real policy envelope must satisfy hook-decision.schema.json: %v\n%s", err, data)
	}
}

func TestEnvelopeCanonicalizesRetryForEveryDecisionClass(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	cases := []struct {
		name       string
		decision   policy.Decision
		wantRetry  string
		wantStatus string
	}{
		{
			name: "recoverable deny uses canonical recovery retry",
			decision: policy.Decision{
				Decision:      "deny",
				RuleID:        policy.RuleAssignmentWriteBeforePlan,
				Reason:        "assignment checkpoint is missing",
				Recovery:      []string{"record the plan checkpoint"},
				Retry:         "after_dispatch",
				HumanRequired: false,
			},
			wantRetry:  "rerun after recovery validation",
			wantStatus: "deny",
		},
		{
			name: "human block never retries",
			decision: policy.Decision{
				Decision:      "block",
				RuleID:        policy.RuleLockedArtifactWrite,
				Reason:        "locked artifact",
				Recovery:      []string{"create a new generation"},
				Retry:         "after_rework",
				HumanRequired: true,
			},
			wantRetry:  "never",
			wantStatus: "block",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envelope := engine.Envelope(policy.Input{
				SessionID: "session-retry-contract",
				Event:     "PreToolUse",
			}, tc.decision, time.Unix(1, 0))
			if envelope.Decision != tc.wantStatus || envelope.Retry != tc.wantRetry {
				t.Fatalf("envelope decision/retry = %q/%q, want %q/%q", envelope.Decision, envelope.Retry, tc.wantStatus, tc.wantRetry)
			}
			data, err := json.Marshal(envelope)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("hook-decision.schema.json", data); err != nil {
				t.Fatalf("decision envelope must satisfy hook-decision.schema.json: %v\n%s", err, data)
			}
		})
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
