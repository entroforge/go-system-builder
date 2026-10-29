package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS3BoundaryPublicDraftPathsAndFieldDiagnostics(t *testing.T) {
	root, packet := newContractAnswerFixture(t)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before, _ := os.ReadFile(statePath)
	index, contract := "docs/dev/contracts/CONTRACTS-SERVICE-009.md", "docs/dev/contracts/BE-SERVICE-009.md"
	draft := completedContractDraft(packet)
	draft.Answer.IndexPath = &index
	draft.Answer.ContractPaths = []string{contract}
	draft.Answer.Conclusion = nil
	draft.Answer.Applicability = map[string]workpacket.ContractApplicability{}
	stageJSON(t, root, packet.AnswerPath, draft)
	for _, event := range []string{"PreToolUse", "SessionStart"} {
		got := s3DeliveredContext(t, designGuidanceEvent(t, root, "draft-paths", event, "Read"))
		for _, want := range []string{filepath.Join(root, index), filepath.Join(root, contract), "safe draft paths", "repository-relative", `"index_path":"` + index + `"`, `"contract_paths":["` + contract + `"]`, "--target design --reason", "--target contracts --reason"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s missing %q: %s", event, want, got)
			}
		}
		if strings.Contains(got, "Conventional output candidates") || strings.Contains(got, "CONTRACTS-039.md") {
			t.Fatalf("draft replaced with inferred outputs: %s", got)
		}
	}
	// Completing the other answers makes these exact copyable path values eligible.
	complete := completedContractDraft(packet)
	complete.Answer.IndexPath, complete.Answer.ContractPaths = &index, []string{contract}
	encoded, _ := json.Marshal(complete)
	if _, issues := workpacket.ValidateContractAnswer(packet, encoded); len(issues) != 0 {
		t.Fatalf("copyable values rejected: %+v", issues)
	}
	// The closed answer shape is still enforced. A failed first reception must
	// locate the same concrete unknown field in the receipt and later sessions.
	var raw map[string]any
	json.Unmarshal(encoded, &raw)
	raw["answer"].(map[string]any)["unknown_contract_scope"] = "wrong place"
	stageJSON(t, root, packet.AnswerPath, raw)
	first, diagnostic := saveS3ThroughPostToolUse(t, root, packet.AnswerPath)
	first = s3DeliveredContext(t, first)
	if !strings.Contains(first, "/answer/unknown_contract_scope") || !strings.Contains(first, "unknown_contract_scope") || !strings.Contains(first, "repair:") {
		t.Fatalf("first public reception lost concrete field diagnostic: %s %s", first, diagnostic)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveContracts(context.Background(), packet.AnswerPath)
	if err == nil || receipt.Accepted {
		t.Fatalf("unknown field accepted: %+v %v", receipt, err)
	}
	if !strings.Contains(err.Error(), "/answer/unknown_contract_scope") || !strings.Contains(err.Error(), "repair:") {
		t.Fatalf("first reception lost field/reason/repair: %v", err)
	}
	for _, event := range []string{"PreToolUse", "SessionStart"} {
		got := s3DeliveredContext(t, designGuidanceEvent(t, root, "unknown-field", event, "Read"))
		for _, want := range []string{"/answer", "unknown_contract_scope", "additional", "repair:", "Last reception attempt"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s lost %q: %s", event, want, got)
			}
		}
	}
	// Invalid author paths stay diagnostic values; never turn them into a
	// supposedly legitimate rooted output by joining them to the repo path.
	invalid := filepath.Join(root, index)
	draft.Answer.IndexPath = &invalid
	stageJSON(t, root, packet.AnswerPath, draft)
	got := s3DeliveredContext(t, designGuidanceEvent(t, root, "bad-path", "SessionStart", "Read"))
	for _, want := range []string{"/answer/index_path", invalid, "repository-relative", "observed"} {
		if !strings.Contains(got, want) {
			t.Errorf("bad path lost %q: %s", want, got)
		}
	}
	if strings.Contains(got, `"index_path":"`+invalid+`"`) {
		t.Fatalf("invalid path offered as copyable value: %s", got)
	}
	if after, _ := os.ReadFile(statePath); !bytes.Equal(before, after) {
		t.Fatal("draft observation/rejection modified Runtime")
	}
}

func TestS4BoundaryPublicDraftPathValues(t *testing.T) {
	root, packet := newTaskPlanningAnswerFixture(t)
	draft := completedTaskPlanningDraft(packet)
	draft.Answer.Conclusion = nil
	task := "docs/dev/tasks/TASK-009-01.md"
	draft.Answer.TaskPaths = []string{task}
	stageJSON(t, root, packet.AnswerPath, draft)
	for _, event := range []string{"PreToolUse", "SessionStart"} {
		got := s3DeliveredContext(t, designGuidanceEvent(t, root, "s4-draft-paths", event, "Read"))
		for _, want := range []string{filepath.Join(root, task), "safe draft paths", "repository-relative", `"task_paths":["` + task + `"]`, `index_path="docs/dev/tasks/index-REQ-039.md"`} {
			if !strings.Contains(got, want) {
				t.Errorf("%s lost %q: %s", event, want, got)
			}
		}
	}
	pass := "pass"
	draft.Answer.Conclusion = &pass
	data, _ := json.Marshal(draft)
	if _, issues := workpacket.ValidateTaskPlanningAnswer(packet, data); len(issues) != 0 {
		t.Fatalf("copyable S4 path values rejected: %+v", issues)
	}
	invalid := filepath.Join(root, task)
	draft.Answer.TaskPaths = []string{invalid}
	stageJSON(t, root, packet.AnswerPath, draft)
	got := s3DeliveredContext(t, designGuidanceEvent(t, root, "s4-invalid-path", "SessionStart", "Read"))
	if !strings.Contains(got, invalid) || !strings.Contains(got, "/answer/task_paths/0") || strings.Contains(got, `"task_paths":["`+invalid+`"]`) {
		t.Fatalf("invalid path guidance: %s", got)
	}
}
