package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Registered dispatch facts and professional observations are fixture inputs.
// Issuance, natural child events, acceptance and continuation use public entry
// points. No acceptance receipt or lifecycle cursor is supplied by the test.
func minimalAnswerReviewResponsibilities(t *testing.T) (string, []workpacket.ReviewResultPacket) {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	plan := minimalReviewPlan(t, root, "review-plan-minimal-answer-flow")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	if _, err := review.RegisterPlan(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), review.PlanRequest{ExpectedRevision: -1, PlanPath: plan}); err != nil {
		t.Fatalf("register original ReviewPlan: %v", err)
	}
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-minimal-delivery")
	markAssignmentDispatched(t, root, "assignment-qa-1", "reviewer-minimal-quality")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		raw.(map[string]any)["dispatch_mode"] = "plan_checkpoint"
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	var packets []workpacket.ReviewResultPacket
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 2 {
		t.Fatalf("issue two original responsibilities: %s (%v)", issued, err)
	}
	return root, packets
}

func minimalAnswerFinding(t *testing.T, root string, packet workpacket.ReviewResultPacket, id string) {
	t.Helper()
	claim := packet.Binding.ClaimIDs[0]
	packet.Draft.Answer, _ = json.Marshal(map[string]any{
		"claim_results":  []any{map[string]any{"claim_id": claim, "conclusion": "fail", "method": "inspect response selection", "observed": "rejected input selects success", "evidence_refs": []string{}}},
		"blocked_claims": []any{}, "checks": []any{}, "deviations": []any{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "finding",
		"findings": []any{map[string]any{
			"finding_id": id, "claim_id": claim, "severity": "P2", "expected": "rejected input returns an error", "authority_refs": []string{"REQ-WORKTREE"},
			"observed": "rejected input selects success", "observation_mode": "code_inspection", "reproducibility": "once_with_deterministic_trace", "evidence_refs": []string{},
			"encounter": map[string]any{"journey_summary": "inspect the rejection response boundary", "inspection_entry": "internal/example/service.go:1", "symbol_trail": "reject -> success", "wall_action": "inspect response selection", "last_good_checkpoint": "input rejected", "first_bad_checkpoint": "success selected", "terminal_state": "success response returned"},
		}},
	})
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
}

func minimalAnswerReviewReceipt(t *testing.T, root string, packet workpacket.ReviewResultPacket) submission.Receipt {
	t.Helper()
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.ReviewResultReceipt(packet.Binding.OwnerAgentID, packet.ID)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func minimalAnswerIntakePaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(filepath.Join(root, ".claude/submissions/s8"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "intake.json" {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

func TestMinimalAnswerFlowOrdinaryFindingEntersS8AndReceivesAlreadyDispatchedPeer(t *testing.T) {
	root, packets := minimalAnswerReviewResponsibilities(t)
	first, late := packets[0], packets[1]
	minimalAnswerFinding(t, root, first, "finding-minimal-first")
	firstPayload := s7ListenerPayload(t, "opaque-minimal-first", "Review original "+filepath.Join(root, first.ViewPath))
	out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", firstPayload)
	if receipt := minimalAnswerReviewReceipt(t, root, first); !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("ordinary finding lacks original receipt: %+v; %s %s", receipt, out, diagnostic)
	}
	entered := stageSnapshot(t, root)
	lifecycle := entered.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "bug_resolution" || lifecycle["phase"] != "investigation" {
		t.Fatalf("first P2 finding did not enter S8 in the same event: %v; %s %s", lifecycle, out, diagnostic)
	}
	if entered.State["review"].(map[string]any)["observation_batch"] != nil {
		t.Fatal("minimal finding handoff manufactured an ObservationBatch")
	}
	if receipt := minimalAnswerReviewReceipt(t, root, late); receipt.Accepted {
		t.Fatalf("unanswered peer was implicitly accepted: %+v", receipt)
	}
	// Main accepts the original intake before the late answer arrives, so the
	// unchanged investigation assertion below protects a real Case authority.
	paths, err := minimalAnswerIntakePaths(root)
	if err != nil || len(paths) != 1 {
		t.Fatalf("S8 Main intake missing: %v (%v)", paths, err)
	}
	stageJSON(t, root, strings.TrimPrefix(paths[0], root+string(filepath.Separator)), map[string]any{"grouping_rationale": "Investigate the accepted response boundary as one provisional causal group."})
	s7ListenerEvent(t, root, "PostToolUse", map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": paths[0]}, "tool_response": map[string]any{"filePath": paths[0]}})
	entered = stageSnapshot(t, root)
	if entered.State["review"].(map[string]any)["investigation"] == nil {
		t.Fatal("Main intake did not commit its original Case")
	}
	investigationBefore, _ := json.Marshal(entered.State["review"].(map[string]any)["investigation"])
	minimalAnswerFinding(t, root, late, "finding-minimal-late")
	latePayload := s7ListenerPayload(t, "opaque-minimal-late", "Review original "+filepath.Join(root, late.ViewPath))
	out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", latePayload)
	if receipt := minimalAnswerReviewReceipt(t, root, late); !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("already-dispatched S7 peer not received first in S8: %+v; %s %s", receipt, out, diagnostic)
	}
	if !strings.Contains(out+diagnostic, "hand new Findings to Main") {
		t.Fatalf("late Finding lost its Main handoff: %s %s", out, diagnostic)
	}
	after := stageSnapshot(t, root)
	if !reflect.DeepEqual(after.State["lifecycle"], entered.State["lifecycle"]) {
		t.Fatalf("late answer retransitioned lifecycle: before=%v after=%v", entered.State["lifecycle"], after.State["lifecycle"])
	}
	investigationAfter, _ := json.Marshal(after.State["review"].(map[string]any)["investigation"])
	if string(investigationBefore) != string(investigationAfter) {
		t.Fatalf("late answer replaced S8 authority: before=%s after=%s", investigationBefore, investigationAfter)
	}
	if got := review.RoundFindings(after.State); len(got) != 2 {
		t.Fatalf("late original finding was lost: %v", got)
	}
	for _, packet := range packets {
		if err := os.Remove(filepath.Join(root, packet.AnswerPath)); err != nil {
			t.Fatal(err)
		}
	}
	for _, payload := range []map[string]any{firstPayload, latePayload} {
		s7ListenerEvent(t, root, "PreToolUse", payload)
	}
	if replay := stageSnapshot(t, root); replay.Revision != after.Revision {
		t.Fatalf("accepted missing-private-answer replay changed revision: %d -> %d", after.Revision, replay.Revision)
	}
}

func TestMinimalAnswerFlowEnvironmentBlockedKeepsOriginalIncompleteReceipt(t *testing.T) {
	root, packets := minimalAnswerReviewResponsibilities(t)
	packet := packets[0]
	packet.Draft.Answer = json.RawMessage(`{"claim_results":[],"blocked_claims":[],"checks":[],"findings":[],"deviations":["The test environment is unavailable; Main must restore it before this review can complete."],"site_lost":[],"verification_artifact_digest":null,"verdict":"blocked"}`)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", s7ListenerPayload(t, "opaque-minimal-blocked", "Review original "+filepath.Join(root, packet.ViewPath)))
	receipt := minimalAnswerReviewReceipt(t, root, packet)
	if !receipt.Accepted || receipt.WorkComplete {
		t.Fatalf("environment blocker lost original incomplete receipt: %+v; %s %s", receipt, out, diagnostic)
	}
	if !strings.Contains(out+diagnostic, "Main") || !strings.Contains(out+diagnostic, "work_complete=false") {
		t.Fatalf("blocked original was not visibly handed to Main as incomplete: %s %s", out, diagnostic)
	}
	after := stageSnapshot(t, root)
	if got := after.State["lifecycle"].(map[string]any)["state"]; got != "verification" {
		t.Fatalf("environment blocker completed stage: %v", got)
	}
	if len(review.RoundFindings(after.State)) != 0 || review.RequiredResponsibilitiesComplete(after.State) {
		t.Fatal("environment blocker manufactured product finding or completed round")
	}
	if err := os.Remove(filepath.Join(root, packet.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	if got := minimalAnswerReviewReceipt(t, root, packet); !got.Accepted || got.WorkComplete {
		t.Fatalf("private answer deletion revoked blocked receipt: %+v", got)
	}
}

func minimalAnswerInvestigator(t *testing.T) (string, workpacket.HypothesisResultPacket) {
	t.Helper()
	root, reviewers := minimalAnswerReviewResponsibilities(t)
	// FreshRoot provides only the shared control JSON files. The real S8
	// dispatcher additionally reads these packaged definition/protocol files.
	for _, rel := range []string{"agents/investigator.md", "docs/control/agent-protocol.md"} {
		data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	minimalAnswerFinding(t, root, reviewers[0], "finding-minimal-investigation")
	s7ListenerEvent(t, root, "PreToolUse", s7ListenerPayload(t, "opaque-minimal-source", "Review original "+filepath.Join(root, reviewers[0].ViewPath)))
	paths, err := minimalAnswerIntakePaths(root)
	if err != nil || len(paths) != 1 {
		t.Fatalf("real S7 handoff did not prepare exact Main intake: %v (%v)", paths, err)
	}
	intake := paths[0]
	stageJSON(t, root, strings.TrimPrefix(intake, root+string(filepath.Separator)), map[string]any{"grouping_rationale": "Investigate the accepted rejection response boundary without asserting its cause."})
	var preparationFeedback string
	save := func(path string) {
		t.Helper()
		out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{"filePath": path}})
		preparationFeedback += out + diagnostic
	}
	save(intake)
	questions := filepath.Join(filepath.Dir(intake), "hypotheses.json")
	stageJSON(t, root, strings.TrimPrefix(questions, root+string(filepath.Separator)), []any{map[string]any{
		"statement": "The rejection boundary selects the wrong response branch.", "invariant": "A rejected input must return an error.",
		"discriminator":      "Inspect response selection; stop as inconclusive if the branch cannot be followed.",
		"expected_outcomes":  map[string]string{"support": "rejected input enters success branch", "refute": "rejected input enters error branch"},
		"source_finding_ids": []string{"finding-minimal-investigation"}, "evidence_refs": []string{"test://minimal-response-selection"},
	}})
	save(questions)
	var packets []workpacket.HypothesisResultPacket
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 1 {
		t.Fatalf("Main original investigation issuer: %s (%v); preparation=%s", issued, err, preparationFeedback)
	}
	return root, packets[0]
}

func TestMinimalAnswerFlowS8NaturalEventsShareOriginalConsumer(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			root, packet := minimalAnswerInvestigator(t)
			packet.Draft.Answer = json.RawMessage(`{"method":"inspect response selection","evidence_refs":["test://minimal-response-selection"],"source_boundary_refs":["finding:finding-minimal-investigation"],"observed":"the rejection boundary cannot yet distinguish the competing branches","counterfactual_or_discriminator":"compare the selected branch with a direct error response","result":"inconclusive","explains_finding_ids":[],"does_not_explain":[],"new_hypotheses":[]}`)
			stageJSON(t, root, packet.AnswerPath, packet.Draft)
			payload := s7ListenerPayload(t, "opaque-minimal-investigator", "Investigate original "+filepath.Join(root, packet.ViewPath))
			payload["agent_transcript_path"] = filepath.Join(strings.TrimSuffix(payload["transcript_path"].(string), ".jsonl"), "subagents", "agent-"+payload["agent_id"].(string)+".jsonl")
			// The actual task transcript selects the original responsibility;
			// no return-reference action is part of receiving this answer.
			payload["last_assistant_message"] = "The assigned discriminator is finished; the conclusion remains uncertain."
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if event == "PostToolUse" {
				before := stageSnapshot(t, root)
				payload["tool_response"] = map[string]any{"is_error": true}
				s7ListenerEvent(t, root, event, payload)
				if receipt, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID); err != nil || receipt.Accepted {
					t.Fatalf("failed Post received answer: %+v (%v)", receipt, err)
				}
				if after := stageSnapshot(t, root); after.Revision != before.Revision {
					t.Fatal("failed Post changed original runtime")
				}
				payload["tool_response"] = map[string]any{"is_error": false}
			}
			out, diagnostic := s7ListenerEvent(t, root, event, payload)
			if receipt, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID); err != nil || !receipt.Accepted || !receipt.WorkComplete {
				t.Fatalf("%s missed original HypothesisResult consumer: %+v (%v); %s %s", event, receipt, err, out, diagnostic)
			}
			accepted := stageSnapshot(t, root)
			if err := os.Remove(filepath.Join(root, packet.AnswerPath)); err != nil {
				t.Fatal(err)
			}
			s7ListenerEvent(t, root, event, payload)
			if replay := stageSnapshot(t, root); replay.Revision != accepted.Revision {
				t.Fatalf("accepted S8 replay rereceived private answer: %d -> %d", accepted.Revision, replay.Revision)
			}
		})
	}
}

func TestMinimalAnswerFlowS8NaturalContinuationPreservesPriorConclusion(t *testing.T) {
	root, packet := minimalAnswerInvestigator(t)
	payload := s7ListenerPayload(t, "opaque-continuing-investigator", "Investigate original "+filepath.Join(root, packet.ViewPath))
	payload["agent_transcript_path"] = filepath.Join(strings.TrimSuffix(payload["transcript_path"].(string), ".jsonl"), "subagents", "agent-"+payload["agent_id"].(string)+".jsonl")
	payload["last_assistant_message"] = "The missing trace prevents the remaining discriminator; Main can recover the trace."
	packet.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"The trace is unavailable; this observation does not yet distinguish the hypotheses."}`)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	s7ListenerEvent(t, root, "PreToolUse", payload)
	accepted := stageSnapshot(t, root)
	// Repeated Stop is delivery replay even if the private original changes.
	packet.Draft.Answer = json.RawMessage(`{"result":"refuted","observed":"The newly available trace contradicts the original hypothesis."}`)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	s7ListenerEvent(t, root, "SubagentStop", payload)
	if got := stageSnapshot(t, root); got.Revision != accepted.Revision {
		t.Fatal("repeated Stop treated private edits as a new attempt")
	}
	// Continuing ordinary activity in this exact original task observes the new
	// professional answer without resume/submit or re-saving the old answer.
	s7ListenerEvent(t, root, "PostToolUse", payload)
	after := stageSnapshot(t, root)
	pointer := after.State["review"].(map[string]any)["investigation"].(map[string]any)
	data, err := os.ReadFile(filepath.Join(root, pointer["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	rows := document["hypothesis_results"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["result"] != "inconclusive" || rows[1].(map[string]any)["result"] != "refuted" {
		t.Fatalf("natural continuation lost original uncertain conclusion: %v", rows)
	}
	if len(document["hypotheses"].([]any)) != 1 {
		t.Fatal("continuation created another responsibility")
	}
	s7ListenerEvent(t, root, "PreToolUse", payload)
	if got := stageSnapshot(t, root); got.Revision != after.Revision {
		t.Fatal("same continued answer was received twice")
	}
}

func TestMinimalAnswerFlowS8StartAndIncompleteStopKeepOrdinaryWorkAvailable(t *testing.T) {
	root, packet := minimalAnswerInvestigator(t)
	payload := s7ListenerPayload(t, "opaque-unfinished-investigator", "Investigate original "+filepath.Join(root, packet.ViewPath))
	payload["agent_transcript_path"] = filepath.Join(strings.TrimSuffix(payload["transcript_path"].(string), ".jsonl"), "subagents", "agent-"+payload["agent_id"].(string)+".jsonl")
	out, diagnostic := s7ListenerEvent(t, root, "SubagentStart", payload)
	if !strings.Contains(out+diagnostic, "S8 current professional responsibility") || !strings.Contains(out+diagnostic, filepath.Join(root, packet.AnswerPath)) || strings.Contains(out+diagnostic, "Return that exact reference") || strings.Contains(out+diagnostic, "Ordinary tools do not") {
		t.Fatalf("S8 start added a ceremony or omitted the original question: %s %s", out, diagnostic)
	}
	// An unready draft can honestly return on its first Stop; it neither creates
	// an accepted professional answer nor prevents Main from restoring the work.
	payload["last_assistant_message"] = "The required trace is unavailable; the assigned discriminator remains unfinished."
	out, diagnostic = s7ListenerEvent(t, root, "SubagentStop", payload)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID); err != nil || receipt.Accepted || receipt.WorkComplete {
		t.Fatalf("unfinished draft manufactured completion: %+v %v", receipt, err)
	}
	if !strings.Contains(out+diagnostic, "Main owns recovery") {
		t.Fatalf("unfinished return omitted Main recovery: %s %s", out, diagnostic)
	}
}
