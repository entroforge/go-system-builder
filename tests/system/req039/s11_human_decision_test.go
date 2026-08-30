package req039_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// The normal Hook stops at S11; only an explicit human decision may approve it.
func TestBUG104S11ExplicitApproveE2E(t *testing.T) {
	root := freshRoot(t)
	state := req039fixtures.BaseState(t, root, "release_audit", "", 10)
	req039fixtures.SeedReleaseAuditReady(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	code, _, stderr := runHook(t, root, "PreToolUse", req039fixtures.PreToolUseBody(
		"bug-104-red", "Bash", map[string]any{"command": "go test ./..."},
	))
	if code != 0 {
		t.Fatalf("release_audit Hook failed: code=%d stderr=%s", code, stderr)
	}
	state = req039fixtures.ReadState(t, root)
	got, _ := req039fixtures.Lifecycle(state)
	if got != "awaiting_human_release" {
		t.Fatalf("normal Hook must stop at S11: got %q", got)
	}
	expectedRevision := int(req039fixtures.Revision(state))

	decision := req039fixtures.EvidenceEnvelope(state, "ev-human-approve", "human_decision", "release-owner", "release owner", "approved", nil)
	req039fixtures.AppendEvidence(state, req039fixtures.WriteEvidenceEnvelope(t, root, state, "ev-human-approve", "human_decision", "release-owner", "release owner", decision, []any{
		fmt.Sprintf("runtime_release:%s@%d", req039fixtures.RuntimeIDFromState(state), expectedRevision),
	}))
	req039fixtures.WriteState(t, root, state)
	var stdout, decisionStderr bytes.Buffer
	code = runCLI(t, []string{
		"runtime", "human-decision", "--root", root,
		"--disposition", "approve", "--expected-revision", fmt.Sprint(expectedRevision),
		"--actor", "user", "--decision-evidence", "ev-human-approve",
	}, bytes.NewReader(nil), &stdout, &decisionStderr)
	if code != 0 {
		t.Fatalf("explicit approve failed: code=%d stdout=%s stderr=%s", code, stdout.String(), decisionStderr.String())
	}
	got, _ = req039fixtures.Lifecycle(req039fixtures.ReadState(t, root))
	if got != "release_authorized" {
		t.Fatalf("explicit approve must reach release_authorized: got %q", got)
	}
}

// RED probe: terminal rollover must require a separately scoped approval
// evidence record, rather than accepting the S11 decision record blindly.
func TestBUG104ApproveRolloverE2E(t *testing.T) {
	root := freshRoot(t)
	state := req039fixtures.BaseState(t, root, "awaiting_human_release", "", 0)
	req039fixtures.SeedAwaitingHumanRelease(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	decision := req039fixtures.EvidenceEnvelope(state, "ev-human-approve", "human_decision", "release-owner", "release owner", "approved", nil)
	releaseScope := fmt.Sprintf("runtime_release:%s@%d", req039fixtures.RuntimeIDFromState(state), int(req039fixtures.Revision(state)))
	req039fixtures.AppendEvidence(state, req039fixtures.WriteEvidenceEnvelope(t, root, state, "ev-human-approve", "human_decision", "release-owner", "release owner", decision, []any{releaseScope}))
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "human-decision", "--root", root,
		"--disposition", "approve", "--expected-revision", "0",
		"--actor", "user", "--decision-evidence", "ev-human-approve",
	}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("approve failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	state = req039fixtures.ReadState(t, root)
	revision := int(req039fixtures.Revision(state))
	scope := fmt.Sprintf("runtime_rollover:%s@%d", req039fixtures.RuntimeIDFromState(state), revision)
	rollover := req039fixtures.EvidenceEnvelope(state, "ev-rollover-approval", "human_decision", "user", "release owner", "approved", nil)
	req039fixtures.AppendEvidence(state, req039fixtures.WriteEvidenceEnvelope(t, root, state, "ev-rollover-approval", "human_decision", "user", "release owner", rollover, []any{scope}))
	req039fixtures.WriteState(t, root, state)

	stdout.Reset()
	stderr.Reset()
	code = runCLI(t, []string{
		"runtime", "rollover", "--root", root,
		"--approved-by", "user", "--approval-evidence", "ev-rollover-approval",
	}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("release_authorized rollover must pass: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
