package req039_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestBUG104ApproveThenRolloverE2E(t *testing.T) {
	root := newS11SystemRoot(t)
	decideS11(t, root, "approve", "The current package is approved by the human.")
	state := req039fixtures.ReadState(t, root)
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
	deferDecisionID := decideS11(t, root, "defer", "The human needs more time to review.")
	state := req039fixtures.ReadState(t, root)
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
	// Resume restores the waiting cursor; only this subsequent explicit human
	// instruction authorizes release, using the newly prepared decision entry.
	approveDecisionID := decideS11(t, root, "approve", "After resuming review, the human explicitly approves this package.")
	if approveDecisionID == deferDecisionID {
		t.Fatal("resumed S11 must not reuse the consumed defer decision identity")
	}
	state = req039fixtures.ReadState(t, root)
	if got, _ := req039fixtures.Lifecycle(state); got != "release_authorized" {
		t.Fatalf("explicit approval after resume cursor = %q, want release_authorized", got)
	}

}

func TestBUG104RejectDefectRoutesHumanProblemToInvestigationE2E(t *testing.T) {
	root := newS11SystemRoot(t, true)
	before := req039fixtures.ReadState(t, root)
	oldBatch, _ := before["review"].(map[string]any)["observation_batch"].(map[string]any)
	oldBatchPath, _ := oldBatch["path"].(string)
	reason := "The human observed an incorrect customer total; investigate this actual report."
	decisionID := decideS11(t, root, "reject_defect", reason)
	state := req039fixtures.ReadState(t, root)
	if got, phase := req039fixtures.Lifecycle(state); got != "bug_resolution" || phase != "investigation" {
		t.Fatalf("reject_defect cursor = %s.%s, want bug_resolution.investigation", got, phase)
	}
	review := state["review"].(map[string]any)
	if review["observation_batch"] != nil || review["investigation"] != nil {
		t.Fatal("human problem must clear the old intake pointers without manufacturing a batch")
	}
	if oldBatchPath != "" {
		if _, err := os.Stat(filepath.Join(root, oldBatchPath)); err != nil {
			t.Fatalf("historical batch file must remain: %v", err)
		}
	}
	// The normal Hook prepares the current Main intake from this real human
	// problem, without requiring a Finding or constructing a sealed Batch.
	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "s11-human-problem", "agent_id": "Main", "cwd": root,
		"tool_name": "Read", "tool_input": map[string]any{"file_path": "README.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	code, feedback, hookErr := runHook(t, root, "PreToolUse", string(payload))
	if code != 0 {
		t.Fatalf("S8 human-problem preparation: code=%d stdout=%s stderr=%s", code, feedback, hookErr)
	}
	paths, err := filepath.Glob(filepath.Join(root, ".claude", "submissions", "s8", "*", "g*", "r*", "*", "intake.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !strings.Contains(feedback, paths[0]) || !strings.Contains(feedback, reason) {
		t.Fatalf("Hook must give the actual intake path and human problem: paths=%v stdout=%s", paths, feedback)
	}
	intakePath := paths[0]
	if err := os.WriteFile(intakePath, []byte(`{"grouping_rationale":"Investigate the customer-total discrepancy reported by the human; trace the first incorrect calculation without assuming its cause."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err = json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s11-human-problem", "agent_id": "Main", "cwd": root,
		"tool_name": "Edit", "tool_input": map[string]any{"file_path": intakePath},
	})
	if err != nil {
		t.Fatal(err)
	}
	code, feedback, hookErr = runHook(t, root, "PostToolUse", string(payload))
	if code != 0 {
		t.Fatalf("S8 human-problem intake save: code=%d stdout=%s stderr=%s", code, feedback, hookErr)
	}
	state = req039fixtures.ReadState(t, root)
	pointer, _ := state["review"].(map[string]any)["investigation"].(map[string]any)
	caseRef, _ := pointer["path"].(string)
	if caseRef == "" {
		t.Fatalf("original intake did not create a current Case: %s", feedback)
	}
	caseBytes, err := os.ReadFile(filepath.Join(root, caseRef))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(caseBytes, &document); err != nil {
		t.Fatal(err)
	}
	findings, _ := document["source_finding_ids"].([]any)
	if len(findings) != 0 {
		t.Fatalf("human problem manufactured Findings: %#v", findings)
	}
	for _, key := range []string{"observation_batch_id", "observation_batch_ref", "observation_batch_sha256"} {
		if _, exists := document[key]; exists {
			t.Fatalf("human-source Case must not invent %s", key)
		}
	}
	var decisionPath string
	for _, raw := range state["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if row["id"] == decisionID {
			decisionPath, _ = row["path"].(string)
		}
	}
	refs, _ := document["failure_boundary_refs"].([]any)
	var hasID, hasPath bool
	for _, ref := range refs {
		hasID = hasID || ref == decisionID
		hasPath = hasPath || ref == decisionPath
	}
	if !hasID || !hasPath {
		t.Fatalf("Case lost actual decision boundary refs: %#v", refs)
	}
	hypotheses := filepath.Join(filepath.Dir(intakePath), "hypotheses.json")
	if _, err := os.Stat(hypotheses); err != nil || !strings.Contains(feedback, hypotheses) {
		t.Fatalf("current hypotheses must be available for real investigation: err=%v stdout=%s", err, feedback)
	}

	question, err := json.Marshal([]map[string]any{{
		"statement":          "The customer total discrepancy may originate in the discount calculation rather than tax rounding.",
		"invariant":          "A loyalty discount applies only to labor, and the total includes correctly rounded tax.",
		"discriminator":      "Read the current pricing calculation and compare labor, parts, discount and tax against the human-reported order; stop without writes if the reported inputs are unavailable.",
		"expected_outcomes":  map[string]string{"support": "The first incorrect subtotal is the discount base before tax.", "refute": "The discount base is correct; the discrepancy starts in a later calculation or cannot be reproduced."},
		"source_finding_ids": []string{}, "evidence_refs": []string{decisionID},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hypotheses, question, 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err = json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s11-human-problem", "agent_id": "Main", "cwd": root,
		"tool_name": "Edit", "tool_input": map[string]any{"file_path": hypotheses},
	})
	if err != nil {
		t.Fatal(err)
	}
	code, feedback, hookErr = runHook(t, root, "PostToolUse", string(payload))
	if code != 0 {
		t.Fatalf("human-source hypothesis save: code=%d stdout=%s stderr=%s", code, feedback, hookErr)
	}
	state = req039fixtures.ReadState(t, root)
	packets, err := investigation.IssueHypothesisResultPackets(root, filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl"), int(req039fixtures.Revision(state)), "")
	if err != nil {
		t.Fatalf("issue original human-source question packet: %v; prior Hook: %s", err, feedback)
	}
	if len(packets) != 1 {
		t.Fatalf("one real question must have one original packet, got %d; Hook: %s", len(packets), feedback)
	}
	packet := packets[0]
	if packet.Binding.ObservationBatchID != "" || packet.Binding.ObservationBatchRef != "" || packet.Binding.ObservationBatchSHA256 != "" || len(packet.Binding.SourceFindingIDs) != 0 || len(packet.Inputs.Findings) != 0 {
		t.Fatalf("human question packet invented Batch or Findings: %#v", packet.Binding)
	}
	var humanMaterial bool
	for _, ref := range packet.Inputs.EvidenceRefs {
		if ref.Ref == decisionID && ref.Path == decisionPath {
			humanMaterial = true
		}
	}
	if !humanMaterial {
		t.Fatalf("question packet lost actual human decision material: %#v", packet.Inputs.EvidenceRefs)
	}
	// Read the actual current Case after issuance through the normal Main
	// listener. Packet existence alone does not prove its handoff is usable.
	payload, err = json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "s11-human-problem", "agent_id": "Main", "cwd": root,
		"tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, packet.Binding.CaseRef)},
	})
	if err != nil {
		t.Fatal(err)
	}
	code, feedback, hookErr = runHook(t, root, "PreToolUse", string(payload))
	if code != 0 {
		t.Fatalf("current human Case listener: code=%d stdout=%s stderr=%s", code, feedback, hookErr)
	}
	if strings.Contains(feedback, "Case source or route changed after issuance") || strings.Contains(feedback, "existing S8 packet "+packet.ID+" is stale") {
		t.Fatalf("unchanged human-source Case was falsely marked stale: %s", feedback)
	}
	if !strings.Contains(feedback, packet.Binding.CaseID) || !strings.Contains(feedback, filepath.Join(root, packet.Binding.CaseRef)) {
		t.Fatalf("listener omitted the actual current human Case: %s", feedback)
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
			// Inspect the complete S10 records already prepared at entry; do not
			// replace their current material paths with legacy bare envelopes.
			var acceptID, auditID string
			for _, raw := range state["evidence"].([]any) {
				entry, _ := raw.(map[string]any)
				if entry["status"] != "valid" {
					continue
				}
				if entry["kind"] == "acceptance" {
					acceptID, _ = entry["id"].(string)
				}
				if entry["kind"] == "release_audit" {
					auditID, _ = entry["id"].(string)
				}
			}
			if acceptID == "" || auditID == "" {
				t.Fatal("initial S10 review records are missing")
			}

			decisionID := decideS11(t, root, tc.disposition, "Human requests correction of the actual "+tc.name+" judgment.")
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
	decideS11(t, root, "abort", "The human abandons this release.")
	state := req039fixtures.ReadState(t, root)
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
	// Preserve the original legacy snapshot for this malformed-input case.
	root := freshRoot(t)
	state := req039fixtures.BaseState(t, root, "awaiting_human_release", "", 0)
	req039fixtures.SeedAwaitingHumanRelease(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
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

func newS11SystemRoot(t *testing.T, modernPacket ...bool) string {
	t.Helper()
	root := freshRoot(t)
	state := req039fixtures.BaseState(t, root, "release_audit", "", 0)
	// Synthetic upstream preparation only: use the existing S10 fixture and
	// original TR017 stage-entry outlet to prepare the real empty application.
	req039fixtures.SeedReleaseAuditReady(t, root, state)
	if len(modernPacket) > 0 && modernPacket[0] {
		state["schema_version"] = "1.3.0"
		state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
		// The upstream fixture represents a consumed S7 responsibility. Record
		// that same fact through the existing five-state projection at setup.
		assignments, _ := state["review"].(map[string]any)["assignments"].(map[string]any)
		for _, raw := range assignments {
			row, _ := raw.(map[string]any)
			if err := workstate.RecordReviewAssignmentActivity(state, row, "consumed", "review_result_submitted"); err != nil {
				t.Fatal(err)
			}
		}
		// Supply the static resources read by the normal dispatch before any
		// command. Schemas use embedded assets; Case/TASK/manifest/decision files
		// are produced by their original consumers below.
		for source, target := range map[string]string{
			"agents/investigator.md":         ".claude/agents/investigator.md",
			"docs/control/agent-protocol.md": "docs/control/agent-protocol.md",
		} {
			data, err := os.ReadFile(filepath.Join(repoRoot(t), source))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, target)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}

	}
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"runtime", "transition", "--root", root, "--id", "TR-017", "--actor", "orchestrator",
		"--expected-revision", "0", "--evidence", "acceptance_record=ev-acc", "--evidence", "release_audit_record=ev-audit", "--evidence", "clean_round_record=ev-clean-pass"}
	if code := runCLI(t, args, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("synthetic S10 entry preparation: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got, _ := req039fixtures.Lifecycle(req039fixtures.ReadState(t, root)); got != "awaiting_human_release" {
		t.Fatalf("stage entry did not reach S11: %s", got)
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

func decideS11(t *testing.T, root, disposition, reason string) string {
	t.Helper()
	before := req039fixtures.ReadState(t, root)

	payload, err := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "agent_id": "Main", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": "README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	code, feedback, hookErr := runHook(t, root, "PreToolUse", string(payload))
	if code != 0 {
		t.Fatalf("S11 current application feedback: code=%d stdout=%s stderr=%s", code, feedback, hookErr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(feedback), &output); err != nil {
		t.Fatal(err)
	}
	specific, _ := output["hookSpecificOutput"].(map[string]any)
	context, _ := specific["additionalContext"].(string)
	const marker = "Human decision application: "
	pos := strings.Index(context, marker)
	if pos < 0 {
		t.Fatalf("Hook did not give the current human application: %s", feedback)
	}
	rest := context[pos+len(marker):]
	end := strings.Index(rest, " (system decision_id ")
	if end < 0 {
		t.Fatalf("Hook application path is incomplete: %s", feedback)
	}
	path := rest[:end]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var application map[string]any
	if err := json.Unmarshal(data, &application); err != nil {
		t.Fatal(err)
	}
	// The simulated operator explicitly chooses only these two existing fields.
	application["disposition"] = disposition
	application["reason"] = reason
	data, err = json.MarshalIndent(application, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err = json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "agent_id": "Main", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": path}})
	if err != nil {
		t.Fatal(err)
	}
	code, feedback, hookErr = runHook(t, root, "PostToolUse", string(payload))
	if code != 0 {
		t.Fatalf("S11 %s save failed: code=%d stdout=%s stderr=%s", disposition, code, feedback, hookErr)
	}
	state := req039fixtures.ReadState(t, root)
	if req039fixtures.Revision(state) != req039fixtures.Revision(before)+1 {
		t.Fatal("decision registration, consumption and transition must share one logical commit")
	}
	wantConsumer, err := runtime.HumanReleaseTransitionID(disposition)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := state["evidence"].([]any)
	for i := len(items) - 1; i >= 0; i-- {
		entry, _ := items[i].(map[string]any)
		if entry["kind"] != "human_decision" {
			continue
		}
		if entry["consumed_by"] != wantConsumer {
			t.Fatal("normal human decision was not consumed by its transition")
		}
		data, err := os.ReadFile(filepath.Join(root, entry["path"].(string)))
		if err != nil {
			t.Fatal(err)
		}
		var answer map[string]any
		if err := json.Unmarshal(data, &answer); err != nil {
			t.Fatal(err)
		}
		if answer["disposition"] != disposition || answer["reason"] != reason {
			t.Fatalf("recorded human decision lost actual content: %#v", answer)
		}
		return entry["id"].(string)
	}
	t.Fatal("normal save did not register its human decision")
	return ""
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
