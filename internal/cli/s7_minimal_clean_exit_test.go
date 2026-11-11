package cli_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestS7MinimalConclusionsEnterS10WithoutMachineProofs(t *testing.T) {
	root, packets := minimalAnswerReviewResponsibilities(t)
	for i, packet := range packets {
		// Checks remain professional observations. A failed check is not an
		// automatic veto of the author's explicit overall conclusion.
		packet.Draft.Answer, _ = json.Marshal(map[string]any{
			"verdict": "pass", "findings": []any{}, "checks": []any{map[string]any{"name": "optional check", "result": "fail"}},
		})
		stageJSON(t, root, packet.AnswerPath, packet.Draft)
		payload := s7ListenerPayload(t, "opaque-clean", "Review original "+filepath.Join(root, packet.ViewPath))
		out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", payload)
		if receipt := minimalAnswerReviewReceipt(t, root, packet); !receipt.Accepted || !receipt.WorkComplete {
			t.Fatalf("original conclusion not accepted: %+v; %s %s", receipt, out, diagnostic)
		}
		state := stageSnapshot(t, root).State
		want := "verification"
		if i == len(packets)-1 {
			want = "acceptance"
		}
		if stage := state["lifecycle"].(map[string]any)["state"]; stage != want {
			t.Fatalf("after responsibility %d stage = %v, want %s; %s %s", i, stage, want, out, diagnostic)
		}
		for _, raw := range state["evidence"].([]any) {
			kind := raw.(map[string]any)["kind"]
			if kind == "clean_round" || kind == "observation_batch" {
				t.Fatalf("minimal S7 exit manufactured %s evidence", kind)
			}
		}
	}
}
