package review

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Normal-original packet issuance requires a real HEAD and an unignored,
// untracked answer path. Commit the fixture's existing product baseline
// before it can issue any packets.
func reviewGitBaseline(t *testing.T, root string) {
	t.Helper()
	reviewFixtureGit(t, root, "init", "-q")
	reviewFixtureGit(t, root, "config", "user.name", "S7 Fixture")
	reviewFixtureGit(t, root, "config", "user.email", "s7-fixture@example.invalid")
	reviewFixtureGit(t, root, "config", "commit.gpgsign", "false")
	reviewFixtureGit(t, root, "config", "core.excludesFile", os.DevNull)
	reviewFixtureGit(t, root, "config", "core.hooksPath", os.DevNull)
	reviewFixtureGit(t, root, "add", "-A")
	reviewFixtureGit(t, root, "commit", "--allow-empty", "-qm", "S7 product baseline")
}

func reviewFixtureGit(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, output)
	}
	return output
}

// This fixture exercises the original Writer using synthetic dispatch rows.
// It deliberately has no valid PLAN/activation; it cannot accept a typed save.
func TestIssuedResultPacketStableOriginalWriterAndUntouchedAuthoring(t *testing.T) {
	root, statePath, journalPath, snapshot, _ := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, snapshot.Revision, "agent-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 {
		t.Fatalf("packets=%d", len(packets))
	}
	p := packets[0]
	writer := loopruntime.NewStore(statePath, journalPath)
	issued, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if issued.Revision != snapshot.Revision+1 {
		t.Fatalf("issuance revisions %d -> %d", snapshot.Revision, issued.Revision)
	}
	if err := loopruntime.ValidateCheckpointIndices(root, issued.State, mustReadTestFile(t, journalPath)); err != nil {
		t.Fatalf("original checkpoint: %v", err)
	}
	data := mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(p.AnswerPath)))
	var draft workpacket.ReviewResultDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(draft.Answer, &answer); err != nil {
		t.Fatal(err)
	}
	if answer["verdict"] != nil {
		t.Fatal("draft manufactured verdict")
	}
	for _, raw := range answer["claim_results"].([]any) {
		if raw.(map[string]any)["conclusion"] != nil {
			t.Fatal("draft manufactured Claim PASS")
		}
	}
	authorBytes := append([]byte("\n"), data...)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p.AnswerPath)), authorBytes, 0600); err != nil {
		t.Fatal(err)
	}
	viewPath := filepath.Join(root, filepath.FromSlash(p.ViewPath))
	originalView := mustReadTestFile(t, viewPath)
	originalJournal := mustReadTestFile(t, journalPath)
	if err := os.Remove(viewPath); err != nil {
		t.Fatal(err)
	}
	again, err := IssueResultPackets(root, statePath, journalPath, issued.Revision, "agent-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	after, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != issued.Revision || !reflect.DeepEqual(again, packets) {
		t.Fatal("exact issuance replay changed registry")
	}
	if !reflect.DeepEqual(mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(p.AnswerPath))), authorBytes) {
		t.Fatal("issuance overwrote author's bytes")
	}
	if !reflect.DeepEqual(mustReadTestFile(t, viewPath), originalView) || !reflect.DeepEqual(mustReadTestFile(t, journalPath), originalJournal) {
		t.Fatal("view restoration changed the original packet or duplicated its issuance event")
	}
}

func TestIssuedResultPacketRefusesWrongActorUnansweredAndDirectBypass(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, snapshot.Revision, "agent-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	p := packets[0]
	issued, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	before := mustReadTestFile(t, statePath)
	for _, owner := range []string{"agent-dv-1", "agent-qa-1"} {
		_, err := SubmitIssuedResult(root, statePath, journalPath, IssuedSubmitRequest{ExpectedRevision: issued.Revision, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: owner})
		if err == nil {
			t.Fatalf("owner %s accepted an unanswered packet", owner)
		}
	}
	resultPath := writeResultFile(t, root, plan, p.Binding.AssignmentID, "review-result-direct-bypass", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{ExpectedRevision: issued.Revision, AssignmentID: p.Binding.AssignmentID, ResultPath: resultPath})
	if err == nil || !strings.Contains(err.Error(), "issued answer") {
		t.Fatalf("direct bypass: %v", err)
	}
	if !reflect.DeepEqual(before, mustReadTestFile(t, statePath)) {
		t.Fatal("rejected draft/actor/bypass mutated Runtime")
	}
	if receipt, accepted, err := IssuedResultReceipt(root, issued.State, p.ID, p.Binding.OwnerAgentID); err != nil || accepted || receipt.ResultRef != "" {
		t.Fatalf("unanswered draft became receipt: %+v,%t,%v", receipt, accepted, err)
	}
}

func TestIssuedResultPacketUsesOriginalOwnerWithoutActivation(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, snapshot.Revision, "agent-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	p := packets[0]
	resultPath := writeResultFile(t, root, plan, p.Binding.AssignmentID, p.ResultID, p.Binding.OwnerAgentID, "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	var original map[string]json.RawMessage
	if err := json.Unmarshal(mustReadTestFile(t, resultPath), &original); err != nil {
		t.Fatal(err)
	}
	answer := s7ProfessionalTestAnswer(t, original)
	p.Draft.Answer, _ = json.Marshal(answer)
	draft, _ := json.Marshal(p.Draft)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p.AnswerPath)), draft, 0600); err != nil {
		t.Fatal(err)
	}
	issued, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, IssuedSubmitRequest{ExpectedRevision: issued.Revision, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: p.Binding.OwnerAgentID})
	if err != nil {
		t.Fatalf("original issued responsibility was blocked by extra activation: %v", err)
	}
	row := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)[p.Binding.AssignmentID].(map[string]any)
	if row["agent_id"] != p.Binding.OwnerAgentID || row["result_ref"] == nil {
		t.Fatalf("original owner or Result was lost: %#v", row)
	}

}

func TestIssuedResultPacketAcceptsPlanCheckpointQuestionnaireWithoutGenericPlan(t *testing.T) {
	root, statePath, journalPath, _, plan := packetModeFixture(t)
	stateBytes := mustReadTestFile(t, statePath)
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" {
			agent["state"] = "reading"
			agent["dispatch_mode"] = "plan_checkpoint"
			agent["plan_reported_ref"] = nil
			agent["activation_ref"] = nil
			agent["readback_ref"] = nil
		}
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue current S7 questionnaire without a generic plan: packets=%d err=%v", len(packets), err)
	}
	p := packets[0]
	// The lighter receive path retains the original live row/Reviewer checks.
	issuedForChecks, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	b := p.Binding
	resultForChecks := &Result{ResultID: p.ResultID, AssignmentID: b.AssignmentID, AssignmentRevision: b.ReviewPlanRevision, ReviewPlanID: b.ReviewPlanID, BaselineGeneration: b.BaselineGeneration, ProducerAgentID: b.OwnerAgentID}
	requestForChecks := SubmitRequest{issuedPacket: &p, packetOwner: b.OwnerAgentID, issuedSourceRoot: root}
	if err := validateIssuedResultRequest(root, issuedForChecks.State, requestForChecks, resultForChecks, false, plan); err != nil {
		t.Fatalf("original issued row must pass receive checks: %v", err)
	}
	for _, mutation := range []string{"lens", "claims", "role", "duplicate-owner"} {
		t.Run("receive-rejects-"+mutation, func(t *testing.T) {
			encoded, _ := json.Marshal(issuedForChecks.State)
			var changed map[string]any
			if err := json.Unmarshal(encoded, &changed); err != nil {
				t.Fatal(err)
			}
			row := resultPacketAssignmentRow(changed, p.Binding.AssignmentID)
			entities := changed["entities"].(map[string]any)
			switch mutation {
			case "lens":
				row["lens"] = "delivery"
			case "claims":
				row["claim_ids"] = []any{"claim-not-issued"}
			default:
				for _, raw := range entities["agents"].([]any) {
					agent := raw.(map[string]any)
					if agent["id"] == p.Binding.OwnerAgentID {
						if mutation == "role" {
							agent["role"] = "backend-builder"
						} else {
							entities["agents"] = append(entities["agents"].([]any), agent)
						}
						break
					}
				}
			}
			err := validateIssuedResultRequest(root, changed, requestForChecks, resultForChecks, false, plan)
			want := map[string]string{"lens": "Claim/lens", "claims": "Claim/lens", "role": "role differs", "duplicate-owner": "unique registered Reviewer"}[mutation]
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("receive changed %s: want %q, got %v", mutation, want, err)
			}
		})
	}
	resultPath := writeResultFile(t, root, plan, p.Binding.AssignmentID, p.ResultID, p.Binding.OwnerAgentID, "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	var original map[string]json.RawMessage
	if err := json.Unmarshal(mustReadTestFile(t, resultPath), &original); err != nil {
		t.Fatal(err)
	}
	answer := s7ProfessionalTestAnswer(t, original)
	var claims []map[string]json.RawMessage
	if err := json.Unmarshal(answer["claim_results"], &claims); err != nil {
		t.Fatal(err)
	}
	refs := make([][]string, len(claims))
	for i := range claims {
		if err := json.Unmarshal(claims[i]["evidence_refs"], &refs[i]); err != nil {
			t.Fatal(err)
		}
		for j, ref := range refs[i] {
			refs[i][j] = strings.Split(ref, "#sha256=")[0]
		}
		claims[i]["evidence_refs"], _ = json.Marshal(refs[i])
	}
	plainRef := refs[0][0]
	refs[0][0] = plainRef + "#sha256=" + strings.Repeat("9", 64)
	claims[0]["evidence_refs"], _ = json.Marshal(refs[0])
	answer["claim_results"], _ = json.Marshal(claims)

	p.Draft.Answer, _ = json.Marshal(answer)
	draft, _ := json.Marshal(p.Draft)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p.AnswerPath)), draft, 0o600); err != nil {
		t.Fatal(err)
	}
	issued, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	request := IssuedSubmitRequest{ExpectedRevision: issued.Revision, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: p.Binding.OwnerAgentID}
	// An evidence-file digest is an author's professional reference, not owner
	// authorization. Reception preserves it without rereading the evidence.
	for _, mismatch := range []string{"owner", "path", "packet"} {
		wrong := request
		switch mismatch {
		case "owner":
			wrong.OwnerAgentID = "agent-dv-1"
		case "path":
			wrong.AnswerPath += ".other"
		case "packet":
			wrong.PacketID += "-other"
		}
		beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
		if _, err := SubmitIssuedResult(root, statePath, journalPath, wrong); err == nil {
			t.Fatalf("wrong %s accepted the original responsibility", mismatch)
		}
		if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) {
			t.Fatalf("wrong %s changed Runtime or journal", mismatch)
		}
	}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("original S7 consumer required a separate generic PLAN: %v", err)
	}
	row := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)[p.Binding.AssignmentID].(map[string]any)
	if row["status"] != "consumed" || row["result_ref"] == nil {
		t.Fatalf("original S7 consumer did not consume the professional questionnaire: %#v", row)
	}
	if saved := mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(row["result_ref"].(string)))); !strings.Contains(string(saved), refs[0][0]) {
		t.Fatal("reception rewrote the author's explicit evidence digest")
	}
	// Completed replay preserves the received answer without rereading evidence.
	rel, err := parsePathEvidenceRef(plainRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("changed after acceptance"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
		t.Fatalf("completed answer replay: %v", err)
	}
	if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(draft, mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(p.AnswerPath)))) {
		t.Fatal("exact replay changed Runtime, journal or author bytes")
	}

}

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Original Result fields omit optional values; the closed author questionnaire
// names those questions explicitly, including an absent verification digest.
func s7ProfessionalTestAnswer(t *testing.T, original map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	answer := map[string]json.RawMessage{}
	for _, key := range []string{"claim_results", "blocked_claims", "checks", "findings", "deviations", "site_lost", "verdict", "verification_artifact_digest"} {
		value, ok := original[key]
		if !ok {
			value = json.RawMessage("[]")
			if key == "verification_artifact_digest" {
				value = json.RawMessage("null")
			}
		}
		answer[key] = value
	}
	var claims []map[string]any
	if err := json.Unmarshal(answer["claim_results"], &claims); err != nil {
		t.Fatal(err)
	}
	for _, claim := range claims {
		if _, exists := claim["method"]; !exists {
			claim["method"] = ""
		}
		if claim["evidence_refs"] == nil {
			claim["evidence_refs"] = []string{}
		}
	}
	answer["claim_results"], _ = json.Marshal(claims)
	return answer
}

func TestIssuedResultPacketConcurrentIssuanceUsesOneOriginalEvent(t *testing.T) {
	root, statePath, journalPath, snapshot, _ := packetModeFixture(t)
	beforeEvents := intField(snapshot.State["journal"].(map[string]any)["last_sequence"])
	var wg sync.WaitGroup
	wg.Add(2)
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			defer wg.Done()
			_, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	latest, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	packets, err := loopruntime.ReviewResultPacketsContext(loopruntime.WithPacketBodyRoot(context.Background(), root), latest.State)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || latest.Revision != snapshot.Revision+1 || intField(latest.State["journal"].(map[string]any)["last_sequence"]) != beforeEvents+1 {
		t.Fatal("concurrent issuance added duplicate registry/event")
	}
}

func TestIssuedE2ENilArtifactDigestAcceptsAndReplaysWithoutMutableWorkspace(t *testing.T) {
	root := t.TempDir()
	state := coldStartState()
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-e2e-1" {
			agent["role"] = "e2e-tester"
			agent["definition_ref"] = "agents/e2e-tester.md"
			agent["dispatch_mode"] = "plan_checkpoint"
		}
	}
	statePath, journalPath := writeState(t, root, state)
	const workspaceRel = "e2e-workspace/nil-digest"
	_, plan := coldStartFixture(t, root, statePath, journalPath, workspaceRel)
	if err := os.MkdirAll(filepath.Join(root, workspaceRel), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, workspaceRel, "fixture.txt"), []byte("actual artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	state = snapshotState(t, statePath, journalPath)
	seedResultMethodSources(t, root, state, plan)
	writeMethodState(t, statePath, state)
	reviewGitBaseline(t, root)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-e2e-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue E2E packet: count=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	resultPath := writeE2EResultFile(t, root, plan, packet.ResultID, "", "path:"+workspaceRel+"/fixture.txt")
	var original map[string]json.RawMessage
	if err := json.Unmarshal(mustReadTestFile(t, resultPath), &original); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resultPath); err != nil {
		t.Fatal(err)
	}
	packet.Draft.Answer, _ = json.Marshal(s7ProfessionalTestAnswer(t, original))
	authorBytes, _ := json.Marshal(packet.Draft)
	answerPath := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if err := os.WriteFile(answerPath, authorBytes, 0600); err != nil {
		t.Fatal(err)
	}
	request := IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("nil author digest first acceptance: %v", err)
	}
	receipt, found, err := IssuedResultReceipt(root, accepted.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("original receipt is unavailable: %+v found=%t err=%v", receipt, found, err)
	}
	// Later changes to an unrelated workspace do not prevent replay of the received answer.
	if err := os.WriteFile(filepath.Join(root, workspaceRel, "changed.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
		t.Fatalf("exact nil author replay reread mutable artifact bytes: %v", err)
	}
	if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(authorBytes, mustReadTestFile(t, answerPath)) {
		t.Fatal("exact replay rewrote Runtime, journal, or author bytes")
	}
}
