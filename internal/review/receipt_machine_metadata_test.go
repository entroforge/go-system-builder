package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestOriginalReviewReceiptIgnoresHistoricalMachineFields(t *testing.T) {
	root, statePath, journalPath, _, _ := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue: %d %v", len(packets), err)
	}
	p := packets[0]
	p.Draft.Answer = json.RawMessage(`{"claim_results":[],"blocked_claims":[],"checks":[],"findings":[],"deviations":["environment unavailable"],"site_lost":[],"verification_artifact_digest":null,"verdict":"blocked"}`)
	data, err := json.Marshal(p.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, p.AnswerPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, IssuedSubmitRequest{ExpectedRevision: -1, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: p.Binding.OwnerAgentID})
	if err != nil {
		t.Fatal(err)
	}
	reception := loopruntime.ReviewResultReception(accepted.State, p.ID)
	receipt, ok := reception["receipt"].(map[string]any)
	if !ok {
		t.Fatal("original receipt missing")
	}
	for _, old := range []any{nil, "old", float64(7), map[string]any{"old": true}} {
		for _, key := range []string{"review_plan_sha256", "assignment_sha256", "subject_digest", "verification_artifact_digest", "answer_sha256", "effective_answer_sha256", "result_sha256", "packet_sha256"} {
			receipt[key] = old
		}
		before, _ := json.Marshal(accepted.State)
		got, found, err := IssuedResultReceipt(root, accepted.State, p.ID, p.Binding.OwnerAgentID)
		if err != nil || !found || got.PacketID != p.ID || got.ProducerAgentID != p.Binding.OwnerAgentID {
			t.Fatalf("original receipt old=%v found=%v err=%v", old, found, err)
		}
		after, _ := json.Marshal(accepted.State)
		if string(before) != string(after) {
			t.Fatal("receipt read rewrote the original state")
		}
	}
	if _, _, err := IssuedResultReceipt(root, accepted.State, p.ID, "other-owner"); err == nil {
		t.Fatal("different owner acquired original receipt")
	}
}

func TestOriginalReviewerAnswerIgnoresLegacyMachineField(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"null", nil}, {"text", "legacy"}, {"number", 7}, {"object", map[string]any{"old": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, statePath, journalPath, _, _ := packetModeFixture(t)
			packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
			if err != nil || len(packets) != 1 {
				t.Fatalf("issue original: %d %v", len(packets), err)
			}
			p := packets[0]
			answer, err := json.Marshal(map[string]any{
				"claim_results": []any{}, "blocked_claims": []any{}, "checks": []any{}, "findings": []any{},
				"deviations": []string{"environment unavailable"}, "site_lost": []any{},
				"verification_artifact_digest": tc.value, "verdict": "blocked",
			})
			if err != nil {
				t.Fatal(err)
			}
			p.Draft.Answer = answer
			data, err := json.Marshal(p.Draft)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, p.AnswerPath)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			request := IssuedSubmitRequest{ExpectedRevision: -1, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: "other-owner"}
			if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err == nil {
				t.Fatal("wrong owner accepted")
			}
			request.OwnerAgentID = p.Binding.OwnerAgentID
			accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
			if err != nil {
				t.Fatalf("author answer old=%v: %v", tc.value, err)
			}
			if _, found, err := IssuedResultReceipt(root, accepted.State, p.ID, p.Binding.OwnerAgentID); err != nil || !found {
				t.Fatalf("original receipt: %v %v", found, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, after) {
				t.Fatal("original authoring answer changed")
			}
		})
	}
}
