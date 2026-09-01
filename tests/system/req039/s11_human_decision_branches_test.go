package req039_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestBUG104ApproveThenRolloverE2E(t *testing.T) {
	root := newS11SystemRoot(t)
	state := req039fixtures.ReadState(t, root)
	decisionID := addS11Evidence(t, root, state, "ev-approve", "human_decision", "approved")
	req039fixtures.WriteState(t, root, state)

	decideS11(t, root, "approve", decisionID, "")
	state = req039fixtures.ReadState(t, root)
	if got, _ := req039fixtures.Lifecycle(state); got != "release_authorized" {
		t.Fatalf("approve cursor = %q, want release_authorized", got)
	}

	revision := int(req039fixtures.Revision(state))
	approvalID := addS11EvidenceWithScopeProducer(t, root, state, "ev-rollover", "human_decision", "approved", "release-owner", []any{
		fmt.Sprintf("runtime_rollover:%s@%d", req039fixtures.RuntimeIDFromState(state), revision),
	})
	req039fixtures.WriteState(t, root, state)

	archive := filepath.Join(root, "archive")
	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "rollover", "--root", root, "--archive-dir", archive,
		"--approved-by", "release-owner", "--approval-evidence", approvalID,
	}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("rollover after approve failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	state = req039fixtures.ReadState(t, root)
	if got, _ := req039fixtures.Lifecycle(state); got != "inactive" {
		t.Fatalf("post-rollover cursor = %q, want inactive", got)
	}
	entries, err := os.ReadDir(archive)
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive entries = %v, err=%v; want one archived runtime", len(entries), err)
	}
}

func TestBUG104DeferResumeS11E2E(t *testing.T) {
	root := newS11SystemRoot(t)
	state := req039fixtures.ReadState(t, root)
	decisionID := addS11Evidence(t, root, state, "ev-defer", "human_decision", "deferred")
	req039fixtures.WriteState(t, root, state)

	decideS11(t, root, "defer", decisionID, "")
	state = req039fixtures.ReadState(t, root)
	if got, _ := req039fixtures.Lifecycle(state); got != "paused" {
		t.Fatalf("defer cursor = %q, want paused", got)
	}
	pause := state["pause"].(map[string]any)
	if pause["from_state"] != "awaiting_human_release" || pause["from_phase"] != nil {
		t.Fatalf("defer checkpoint = %#v, want S11 cursor", pause)
	}

	// The defer decision is scoped to its own revision and cannot authorize
	// the resume — a fresh runtime_resume-scoped decision is required.
	lifecycle, _ := state["lifecycle"].(map[string]any)
	resumeDecision := req039fixtures.EvidenceEnvelope(state, "ev-resume", "human_decision", "release-owner", "release owner", "approved", map[string]any{
		"decision_id": "ev-resume", "disposition": "resume",
		"target_cursor": map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]},
	})
	req039fixtures.AppendEvidence(state, req039fixtures.WriteEvidenceEnvelope(t, root, state, "ev-resume", "human_decision", "release-owner", "release owner", resumeDecision, []any{
		fmt.Sprintf("runtime_resume:%s@%d", req039fixtures.RuntimeIDFromState(state), int(req039fixtures.Revision(state))),
	}))
	req039fixtures.WriteState(t, root, state)

	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "transition", "--root", root, "--state", statePath, "--journal", journalPath,
		"--id", "TR-019", "--expected-revision", fmt.Sprint(int(req039fixtures.Revision(state))),
		"--actor", "user", "--evidence", "human_decision_record=ev-resume",
		"--evidence", "pause_record=generated:pause_checkpoint",
	}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("TR-019 resume failed: code=%d stderr=%s", code, stderr.String())
	}
	state = req039fixtures.ReadState(t, root)
	if got, _ := req039fixtures.Lifecycle(state); got != "awaiting_human_release" || state["pause"] != nil {
		t.Fatalf("resume result = %#v pause=%#v, want S11 with cleared checkpoint", state["lifecycle"], state["pause"])
	}
}

func TestBUG104RejectDefectRequiresFindingAndRoutesToInvestigationE2E(t *testing.T) {
	root := newS11SystemRoot(t)
	state := req039fixtures.ReadState(t, root)
	decisionID := addS11Evidence(t, root, state, "ev-reject-defect", "human_decision", "reject_defect")
	req039fixtures.WriteState(t, root, state)

	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "human-decision", "--root", root, "--disposition", "reject_defect",
		"--expected-revision", fmt.Sprint(int(req039fixtures.Revision(state))), "--actor", "user",
		"--decision-evidence", decisionID,
	}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--finding-evidence") {
		t.Fatalf("reject_defect without finding code=%d stderr=%s", code, stderr.String())
	}
	before := req039fixtures.ReadState(t, root)
	findingID := addS11Evidence(t, root, before, "ev-finding", "bug", "defect")
	req039fixtures.WriteState(t, root, before)
	decideS11(t, root, "reject_defect", decisionID, findingID)
	state = req039fixtures.ReadState(t, root)
	if got, phase := req039fixtures.Lifecycle(state); got != "bug_resolution" || phase != "investigation" {
		t.Fatalf("reject_defect cursor = %s.%s, want bug_resolution.investigation", got, phase)
	}
}

func TestBUG104RejectAcceptanceAndAuditInvalidateOnlyScopedEvidenceE2E(t *testing.T) {
	tests := []struct {
		name        string
		disposition string
		wantState   string
		acceptValid bool
		auditValid  bool
	}{
		{name: "acceptance", disposition: "reject_acceptance", wantState: "acceptance", acceptValid: false, auditValid: false},
		{name: "release audit", disposition: "reject_release_audit", wantState: "release_audit", acceptValid: true, auditValid: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := newS11SystemRoot(t)
			state := req039fixtures.ReadState(t, root)
			decisionID := addS11Evidence(t, root, state, "ev-"+tc.name+"-decision", "human_decision", "rejected")
			acceptID := addS11Evidence(t, root, state, "ev-"+tc.name+"-accept", "acceptance", "pass")
			auditID := addS11Evidence(t, root, state, "ev-"+tc.name+"-audit", "release_audit", "approved")
			req039fixtures.WriteState(t, root, state)

			decideS11(t, root, tc.disposition, decisionID, "")
			state = req039fixtures.ReadState(t, root)
			if got, _ := req039fixtures.Lifecycle(state); got != tc.wantState {
				t.Fatalf("cursor = %q, want %q", got, tc.wantState)
			}
			if got := s11EvidenceStatus(state, acceptID); (got == "valid") != tc.acceptValid {
				t.Fatalf("acceptance evidence status=%q, want valid=%t", got, tc.acceptValid)
			}
			if got := s11EvidenceStatus(state, auditID); (got == "valid") != tc.auditValid {
				t.Fatalf("release-audit evidence status=%q, want valid=%t", got, tc.auditValid)
			}
			if got := s11EvidenceStatus(state, decisionID); got != "valid" {
				t.Fatalf("decision evidence status=%q, want valid", got)
			}
		})
	}
}

func TestBUG104AbortThenRolloverE2E(t *testing.T) {
	root := newS11SystemRoot(t)
	state := req039fixtures.ReadState(t, root)
	decisionID := addS11Evidence(t, root, state, "ev-abort", "human_decision", "abort")
	req039fixtures.WriteState(t, root, state)
	decideS11(t, root, "abort", decisionID, "")
	state = req039fixtures.ReadState(t, root)
	if got, _ := req039fixtures.Lifecycle(state); got != "aborted" {
		t.Fatalf("abort cursor = %q, want aborted", got)
	}
	approvalID := addS11EvidenceWithScopeProducer(t, root, state, "ev-abort-rollover", "human_decision", "approved", "release-owner", []any{
		fmt.Sprintf("runtime_rollover:%s@%d", req039fixtures.RuntimeIDFromState(state), int(req039fixtures.Revision(state))),
	})
	req039fixtures.WriteState(t, root, state)
	archive := filepath.Join(root, "archive")
	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{"runtime", "rollover", "--root", root, "--archive-dir", archive, "--approved-by", "release-owner", "--approval-evidence", approvalID}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("aborted rollover failed: code=%d stderr=%s", code, stderr.String())
	}
	if got, _ := req039fixtures.Lifecycle(req039fixtures.ReadState(t, root)); got != "inactive" {
		t.Fatalf("post-abort rollover cursor = %q, want inactive", got)
	}
}

func TestBUG104S11InvalidDispositionDoesNotMutateLegacySnapshotE2E(t *testing.T) {
	root := newS11SystemRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "human-decision", "--root", root, "--disposition", "release_authorized",
		"--expected-revision", "0", "--actor", "user", "--decision-evidence", "missing",
	}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "choose approve") {
		t.Fatalf("invalid legacy disposition code=%d stderr=%s", code, stderr.String())
	}
	afterState, _ := os.ReadFile(statePath)
	afterJournal, _ := os.ReadFile(journalPath)
	if string(afterState) != string(beforeState) || string(afterJournal) != string(beforeJournal) {
		t.Fatal("invalid legacy disposition mutated state or journal")
	}
}

func newS11SystemRoot(t *testing.T) string {
	t.Helper()
	root := freshRoot(t)
	state := req039fixtures.BaseState(t, root, "awaiting_human_release", "", 0)
	req039fixtures.SeedAwaitingHumanRelease(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func addS11Evidence(t *testing.T, root string, state map[string]any, id, kind, conclusion string) string {
	return addS11EvidenceWithScope(t, root, state, id, kind, conclusion, nil)
}

func addS11EvidenceWithScope(t *testing.T, root string, state map[string]any, id, kind, conclusion string, scope []any) string {
	return addS11EvidenceWithScopeProducer(t, root, state, id, kind, conclusion, "bug-104-agent", scope)
}

func addS11EvidenceWithScopeProducer(t *testing.T, root string, state map[string]any, id, kind, conclusion, producer string, scope []any) string {
	t.Helper()
	if scope == nil && kind == "human_decision" {
		scope = []any{fmt.Sprintf("runtime_release:%s@%d", req039fixtures.RuntimeIDFromState(state), int(req039fixtures.Revision(state)))}
	}
	extra := map[string]any(nil)
	if kind == "human_decision" {
		disposition := ""
		switch {
		case conclusion == "approved":
			disposition = "approve"
		case conclusion == "deferred":
			disposition = "defer"
		case conclusion == "reject_defect":
			disposition = "reject_defect"
		case conclusion == "abort":
			disposition = "abort"
		case strings.Contains(id, "acceptance"):
			disposition = "reject_acceptance"
		case strings.Contains(id, "audit"):
			disposition = "reject_release_audit"
		}
		lifecycle, _ := state["lifecycle"].(map[string]any)
		extra = map[string]any{
			"decision_id": id, "disposition": disposition,
			"target_cursor": map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]},
		}
	}
	envelope := req039fixtures.EvidenceEnvelope(state, id, kind, producer, "BUG-104", conclusion, extra)
	entry := req039fixtures.WriteEvidenceEnvelope(t, root, state, id, kind, producer, "BUG-104", envelope, scope)
	req039fixtures.AppendEvidence(state, entry)
	return id
}

func decideS11(t *testing.T, root, disposition, decisionID, findingID string) {
	t.Helper()
	state := req039fixtures.ReadState(t, root)
	args := []string{
		"runtime", "human-decision", "--root", root, "--disposition", disposition,
		"--expected-revision", fmt.Sprint(int(req039fixtures.Revision(state))),
		"--actor", "user", "--decision-evidence", decisionID,
	}
	if findingID != "" {
		args = append(args, "--finding-evidence", findingID)
	}
	var stdout, stderr bytes.Buffer
	if code := runCLI(t, args, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("S11 %s failed: code=%d stdout=%s stderr=%s", disposition, code, stdout.String(), stderr.String())
	}
}

func s11EvidenceStatus(state map[string]any, id string) string {
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item != nil && item["id"] == id {
			status, _ := item["status"].(string)
			return status
		}
	}
	return ""
}
