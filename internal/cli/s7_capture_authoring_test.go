package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS7OriginalAnswerBindsActualCommandCaptureWithoutAuthorMetadata(t *testing.T) {
	root, _, captured := finalCaptureExecFixture(t, "review-plan-professional-capture", "printf actual-captured-output")
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	// Adapt the registered legacy fixture to the existing packet contract;
	// actual capture CLI execution and all original provenance remain intact.
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == "agent-capture-dv" {
			row["state"], row["dispatch_mode"] = "reading", "plan_checkpoint"
		}
	}
	data, _ = json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := review.IssueResultPackets(root, statePath, journalPath, -1, "agent-capture-dv")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original questionnaire: %d %v", len(packets), err)
	}
	packet := packets[0]
	plain := strings.SplitN(captured, "#sha256=", 2)[0]
	packet.Draft.Answer, _ = json.Marshal(map[string]any{
		"claim_results":  []any{map[string]any{"claim_id": packet.Binding.ClaimIDs[0], "conclusion": "pass", "method": "actual command capture", "observed": "actual-captured-output", "evidence_refs": []string{plain}}},
		"blocked_claims": []any{}, "checks": []any{}, "findings": []any{}, "deviations": []string{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "pass",
	})
	author, _ := json.Marshal(packet.Draft)
	answerPath := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if err := os.WriteFile(answerPath, author, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := review.SubmitIssuedResult(root, statePath, journalPath, review.IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID})
	if err != nil {
		t.Fatalf("normal author was asked for capture SHA/bytes/truncated: %v", err)
	}
	receipt, found, err := review.IssuedResultReceipt(root, accepted.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("original accepted receipt: %t %v", found, err)
	}
	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(receipt.ResultRef)))
	if err != nil || !bytes.Contains(stored, []byte(captured)) {
		t.Fatalf("original capture metadata not fixed in accepted result: %v\n%s", err, stored)
	}
	got, err := os.ReadFile(answerPath)
	if err != nil || !bytes.Equal(got, author) {
		t.Fatal("system rewrote the author's original questionnaire")
	}
	packetsAfter, err := runtime.CurrentReviewResultPacketsContext(runtime.WithPacketBodyRoot(context.Background(), root), accepted.State, packet.Binding.OwnerAgentID)
	if err != nil || len(packetsAfter) != 1 {
		t.Fatalf("original responsibility association changed: %d %v", len(packetsAfter), err)
	}
}
