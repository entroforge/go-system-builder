package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// policyRefFixturePolicy mirrors docs/hook-policy.json after the REQ-039
// reduction: artifact `version` v2.0.0 next to wire-format `schema_version`
// 1.2.0 (BUG-039-12).
const policyRefFixturePolicy = `{"version":"v2.0.0","schema_version":"1.2.0","policy_id":"hook-policy"}`

// writePolicyRefFixture lays down a minimal repo root containing a hook policy
// document and a runtime state whose policy_ref records the supplied
// version/sha256. It returns the repo root and the repo-relative state path.
func writePolicyRefFixture(t *testing.T, recordedVersion, recordedSHA string) (root, statePath string) {
	t.Helper()
	root = t.TempDir()
	policyPath := filepath.Join(root, "docs", "hook-policy.json")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, []byte(policyRefFixturePolicy), 0o644); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{}
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-test"
	state["revision"] = 32
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	hookControl := state["hook_control"].(map[string]any)
	hookControl["policy_ref"] = map[string]any{
		"path":    "docs/hook-policy.json",
		"version": recordedVersion,
		"sha256":  recordedSHA,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath = filepath.Join(claudeDir, "loop-state.json")
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, statePath
}

func policyRefFixtureDigest() string {
	sum := sha256.Sum256([]byte(policyRefFixturePolicy))
	return fmt.Sprintf("%x", sum)
}

func TestReportPolicyRefDriftDetectsVersionDrift(t *testing.T) {
	root, statePath := writePolicyRefFixture(t, "1.2.0", policyRefFixtureDigest())
	var stdout, stderr bytes.Buffer

	code := reportPolicyRefDrift(root, statePath, filepath.Join(root, ".claude", "loop-events.jsonl"), &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected doctor to fail on version drift, got exit %d (stdout=%q stderr=%q)", code, stdout.String(), stderr.String())
	}
	message := stderr.String()
	for _, want := range []string{"policy_ref drifted", "recorded=1.2.0", "on-disk=v2.0.0", "runtime reconcile-policy-ref"} {
		if !strings.Contains(message, want) {
			t.Fatalf("doctor finding missing %q: %s", want, message)
		}
	}
}

func TestReportPolicyRefDriftPassesWhenConsistent(t *testing.T) {
	root, statePath := writePolicyRefFixture(t, "v2.0.0", policyRefFixtureDigest())
	var stdout, stderr bytes.Buffer

	code := reportPolicyRefDrift(root, statePath, filepath.Join(root, ".claude", "loop-events.jsonl"), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected consistent policy_ref to pass, got exit %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "policy_ref consistent") {
		t.Fatalf("expected consistency line, got %q", stdout.String())
	}
}

// TestReportPolicyRefDriftSkipsUnboundRepository pins the behaviour that doctor
// still runs on a checkout with no runtime state.
func TestReportPolicyRefDriftSkipsUnboundRepository(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := reportPolicyRefDrift(root, ".claude/loop-state.json", ".claude/loop-events.jsonl", &stdout, &stderr)

	if code != 0 {
		t.Fatalf("missing runtime state must not be a doctor finding, got exit %d stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("expected silent skip, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRuntimeReconcilePolicyRefRealignsVersion(t *testing.T) {
	root, statePath := writePolicyRefFixture(t, "1.2.0", "stale-digest")
	var stdout, stderr bytes.Buffer

	code := runRuntimeReconcilePolicyRef([]string{
		"--root", root,
		"--state", statePath,
		"--journal", filepath.Join(root, ".claude", "loop-events.jsonl"),
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("reconcile-policy-ref failed: exit %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "version 1.2.0 -> v2.0.0") {
		t.Fatalf("expected version transition in output, got %q", stdout.String())
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]any
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	policyRef := after["hook_control"].(map[string]any)["policy_ref"].(map[string]any)
	if policyRef["version"] != "v2.0.0" {
		t.Fatalf("policy_ref version after reconcile: got %v want v2.0.0", policyRef["version"])
	}
	if policyRef["sha256"] != policyRefFixtureDigest() {
		t.Fatalf("policy_ref sha256 after reconcile: got %v want %s", policyRef["sha256"], policyRefFixtureDigest())
	}
	// Fingerprint refresh is non-semantic housekeeping and must not bump the
	// runtime revision or look like a transition.
	if after["revision"].(float64) != 32 {
		t.Fatalf("reconcile must not bump revision: got %v", after["revision"])
	}
}

func TestRuntimeReconcilePolicyRefCheckOnlyDoesNotWrite(t *testing.T) {
	root, statePath := writePolicyRefFixture(t, "1.2.0", "stale-digest")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := runRuntimeReconcilePolicyRef([]string{
		"--root", root,
		"--state", statePath,
		"--journal", filepath.Join(root, ".claude", "loop-events.jsonl"),
		"--check",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("--check must report drift with a non-zero exit, got %d", code)
	}
	if !strings.Contains(stderr.String(), "policy_ref drifted") {
		t.Fatalf("expected drift report, got %q", stderr.String())
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("--check must not modify runtime state")
	}
}

func TestRuntimeReconcilePolicyRefNoopWhenConsistent(t *testing.T) {
	root, statePath := writePolicyRefFixture(t, "v2.0.0", policyRefFixtureDigest())
	var stdout, stderr bytes.Buffer

	code := runRuntimeReconcilePolicyRef([]string{
		"--root", root,
		"--state", statePath,
		"--journal", filepath.Join(root, ".claude", "loop-events.jsonl"),
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("consistent policy_ref should succeed, got exit %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already consistent") {
		t.Fatalf("expected no-op message, got %q", stdout.String())
	}
}

// TestPersistGateForPreToolUseAcceptsUnknownGate is the milestone half of
// BUG-039-12. A Controller cycle that hits LOOP_TRIGGER_CONFLICT yields a real
// gate observation (status=unknown) with no gate_id and no fingerprint. The
// previous gating dropped exactly those gates, leaving milestone.quality_gate
// absent on live runtimes. Status is the presence test — matching what
// guidanceMapWithGate already uses.
func TestPersistGateForPreToolUseAcceptsUnknownGate(t *testing.T) {
	cases := []struct {
		name        string
		gate        controller.QualityGateResult
		wantAttempt bool
	}{
		{
			name: "unknown gate with no id or fingerprint still persists",
			gate: controller.QualityGateResult{
				Status:           controller.StatusUnknown,
				ErrorCode:        "LOOP_TRIGGER_CONFLICT",
				ObservedRevision: 32,
				NextCursor:       "building",
			},
			wantAttempt: true,
		},
		{
			name: "resolved gate persists",
			gate: controller.QualityGateResult{
				Status:      controller.StatusNotReady,
				GateID:      "GATE-BUILD-COMPLETE",
				Fingerprint: "sha256:abc",
			},
			wantAttempt: true,
		},
		{
			name:        "wholly zero gate is skipped (cycle never ran)",
			gate:        controller.QualityGateResult{},
			wantAttempt: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A non-existent root makes the persistence attempt fail
			// harmlessly; what we assert is whether the helper reached the
			// point of synthesizing Guidance, which it only does when the
			// gate passed the presence test.
			root := t.TempDir()
			request := &policy.Input{Event: "PreToolUse"}
			decision := &policy.Decision{}
			persistGateForPreToolUse(root, request, decision, controller.ControlResult{QualityGate: tc.gate})
			if got := decision.Guidance != nil; got != tc.wantAttempt {
				t.Fatalf("persistence attempted=%v want %v for gate %+v", got, tc.wantAttempt, tc.gate)
			}
		})
	}
}

// TestPersistGateForPreToolUseIgnoresNonPreToolUseEvents pins that only
// PreToolUse runs the control cycle and therefore only PreToolUse persists a
// gate; other events keep the legacy zero-gate refresh path.
func TestPersistGateForPreToolUseIgnoresNonPreToolUseEvents(t *testing.T) {
	root := t.TempDir()
	decision := &policy.Decision{}
	persistGateForPreToolUse(root, &policy.Input{Event: "SessionStart"}, decision,
		controller.ControlResult{QualityGate: controller.QualityGateResult{Status: controller.StatusUnknown}})
	if decision.Guidance != nil {
		t.Fatal("SessionStart must not persist a controller gate")
	}
}
