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

// policyRefFixturePolicy mirrors docs/control/hook-policy.json after the REQ-039
// reduction: artifact `version` v2.0.0 next to wire-format `schema_version`
// 1.2.0 (BUG-039-12).
const policyRefFixturePolicy = `{"version":"v2.0.0","schema_version":"1.2.0","policy_id":"hook-policy"}`

// writePolicyRefFixture lays down a minimal repo root containing a hook policy
// document and a runtime state whose policy_ref records the supplied
// version/sha256. It returns the repo root and the repo-relative state path.
func writePolicyRefFixture(t *testing.T, recordedVersion, recordedSHA string) (root, statePath string) {
	t.Helper()
	root = t.TempDir()
	policyPath := filepath.Join(root, "docs", "control", "hook-policy.json")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, []byte(policyRefFixturePolicy), 0o644); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
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
		"path":    "docs/control/hook-policy.json",
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

// TestReportPolicyRefDriftDoesNotQualifyDoctorOnVersionDrift pins that the
// retired fingerprint maintenance no longer turns a recorded policy reference
// into a doctor qualification. L4-state-transition-core.md:359 rewrites the
// legacy `runtime fingerprint`/`reconcile-policy-ref` entry as "旧 fingerprint
// 维护入口 … 历史入口不再供 Hook 登记或重算散列；原状态修订或真实来源问题沿
// 对应实际责任处理", and L4-state-transition-core.md:163 removes hash
// registration and validation from the S0-S11 hooks and their original
// consumers. A stale recorded version therefore stays a recorded fact that
// the original responsibility handles; doctor must not fail on it.
func TestReportPolicyRefDriftDoesNotQualifyDoctorOnVersionDrift(t *testing.T) {
	root, statePath := writePolicyRefFixture(t, "1.2.0", policyRefFixtureDigest())
	var stdout, stderr bytes.Buffer

	code := reportPolicyRefDrift(root, statePath, filepath.Join(root, ".claude", "loop-events.jsonl"), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected doctor to stay green on a recorded version drift, got exit %d (stdout=%q stderr=%q)", code, stdout.String(), stderr.String())
	}
	for _, unwanted := range []string{"policy_ref drifted", "recorded=1.2.0", "on-disk=v2.0.0"} {
		if strings.Contains(stdout.String(), unwanted) || strings.Contains(stderr.String(), unwanted) {
			t.Fatalf("retired policy reference qualification reappeared (%q): stdout=%q stderr=%q", unwanted, stdout.String(), stderr.String())
		}
	}
}

// TestReportPolicyRefDriftLeavesNoPolicyFinding pins that the retired check
// leaves no trace at all: doctor neither fails nor claims that it verified the
// historical policy contents. L4-workflow-convergence.md:291 keeps doctor
// consuming the current configuration and the original references while
// "删除摘要字段及旧字段兼容接线", so there is no "policy_ref consistent"
// proof line either — the original path and version no longer prove that the
// bytes on disk were not changed (L4-workflow-convergence.md:281).
func TestReportPolicyRefDriftLeavesNoPolicyFinding(t *testing.T) {
	root, statePath := writePolicyRefFixture(t, "v2.0.0", policyRefFixtureDigest())
	var stdout, stderr bytes.Buffer

	code := reportPolicyRefDrift(root, statePath, filepath.Join(root, ".claude", "loop-events.jsonl"), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected the retired check to stay green, got exit %d stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("expected no policy reference finding, got stdout=%q stderr=%q", stdout.String(), stderr.String())
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

// TestRuntimeReconcilePolicyRefReportsAvailablePolicyWithoutRewritingState
// pins the surviving behaviour of the legacy maintenance entry: it reports the
// policy that is currently available and writes nothing. L4-state-transition-core.md:359
// retires the entry as a fingerprint maintenance path, and
// L4-state-transition-core.md:217 keeps the historical file and its original
// path while holding that "旧散列字段不迁入新责任或变成新的接收/恢复条件" — the
// recorded digest and version must not be silently refreshed into a new
// responsibility.
func TestRuntimeReconcilePolicyRefReportsAvailablePolicyWithoutRewritingState(t *testing.T) {
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
	out := stdout.String()
	for _, want := range []string{"policy available", "docs/control/hook-policy.json", "version=v2.0.0", "historical contents are not verified"} {
		if !strings.Contains(out, want) {
			t.Fatalf("available policy report missing %q: %s", want, out)
		}
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
	if policyRef["version"] != "1.2.0" {
		t.Fatalf("retired entry must not rewrite the recorded policy version: got %v want 1.2.0", policyRef["version"])
	}
	if policyRef["sha256"] != "stale-digest" {
		t.Fatalf("retired entry must not recompute the recorded digest: got %v want stale-digest", policyRef["sha256"])
	}
	// Reporting the available policy is non-semantic housekeeping and must not
	// bump the runtime revision or look like a transition.
	if after["revision"].(float64) != 32 {
		t.Fatalf("reconcile must not bump revision: got %v", after["revision"])
	}
}

// TestRuntimeReconcilePolicyRefCheckOnlyReportsTheCurrentPolicy pins that the
// inspection mode reports the currently available policy without claiming that
// the historical contents were verified and without touching runtime state.
func TestRuntimeReconcilePolicyRefCheckOnlyReportsTheCurrentPolicy(t *testing.T) {
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

	if code != 0 {
		t.Fatalf("--check must report the available policy with exit 0, got %d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "policy available") || !strings.Contains(out, "historical contents are not verified") {
		t.Fatalf("expected the available policy report, got %q", out)
	}
	if strings.Contains(out, "policy_ref drifted") || strings.Contains(stderr.String(), "policy_ref drifted") {
		t.Fatalf("retired drift report reappeared: stdout=%q stderr=%q", out, stderr.String())
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("--check must not modify runtime state")
	}
}

// TestRuntimeReconcilePolicyRefDoesNotClaimHistoricalConsistency pins that the
// retired entry no longer prints a consistency proof. L4-workflow-convergence.md:281
// records that without a hash the original path and version no longer
// independently prove that the bytes were not changed externally, so a
// "consistent" claim would overstate what was actually verified.
func TestRuntimeReconcilePolicyRefDoesNotClaimHistoricalConsistency(t *testing.T) {
	root, statePath := writePolicyRefFixture(t, "v2.0.0", policyRefFixtureDigest())
	var stdout, stderr bytes.Buffer

	code := runRuntimeReconcilePolicyRef([]string{
		"--root", root,
		"--state", statePath,
		"--journal", filepath.Join(root, ".claude", "loop-events.jsonl"),
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("policy availability report should succeed, got exit %d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "already consistent") {
		t.Fatalf("retired consistency proof reappeared: %q", out)
	}
	if !strings.Contains(out, "historical contents are not verified") {
		t.Fatalf("expected the explicit no-verification statement, got %q", out)
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
				Status: controller.StatusNotReady,
				GateID: "GATE-BUILD-COMPLETE",
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
