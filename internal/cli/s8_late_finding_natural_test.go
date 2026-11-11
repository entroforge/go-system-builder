package cli_test

import (
	"encoding/json"
	"io/fs"
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

// The two Findings are actually received by their issued S7 responsibilities;
// Main then chooses the later one in the existing S8 question answer.
func TestS8MainQuestionIncludesNaturallyReceivedLateFinding(t *testing.T) {
	root, packets := minimalAnswerReviewResponsibilities(t)
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
	first, late := packets[0], packets[1]
	minimalAnswerFinding(t, root, first, "finding-minimal-first")
	s7ListenerEvent(t, root, "PreToolUse", s7ListenerPayload(t, "opaque-first", "Review original "+filepath.Join(root, first.ViewPath)))
	paths := lateFindingQuestionFiles(t, root, "intake.json")
	if len(paths) != 1 {
		t.Fatalf("intake: %v", paths)
	}
	stageJSON(t, root, strings.TrimPrefix(paths[0], root+string(filepath.Separator)), map[string]any{"grouping_rationale": "Investigate the first observed boundary."})
	s7ListenerEvent(t, root, "PostToolUse", map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": paths[0]}, "tool_response": map[string]any{"filePath": paths[0]}})
	before := stageSnapshot(t, root)
	ptr := before.State["review"].(map[string]any)["investigation"].(map[string]any)
	oldCase, err := os.ReadFile(filepath.Join(root, ptr["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	minimalAnswerFinding(t, root, late, "finding-minimal-late")
	s7ListenerEvent(t, root, "PostToolUse", s7ListenerPayload(t, "opaque-late", "Review original "+filepath.Join(root, late.ViewPath)))
	observed := stageSnapshot(t, root)
	if len(review.RoundFindings(observed.State)) != 2 {
		t.Fatal("late original Finding was not received")
	}
	if got := observed.State["review"].(map[string]any)["investigation"].(map[string]any)["source_finding_ids"]; !reflect.DeepEqual(got, ptr["source_finding_ids"]) {
		t.Fatalf("late Finding was automatically selected: %v", got)
	}
	qpaths := lateFindingQuestionFiles(t, root, "hypotheses.json")
	if len(qpaths) != 1 {
		t.Fatalf("original question path: %v", qpaths)
	}
	q := []any{map[string]any{"statement": "The later observation exposes another boundary", "invariant": "rejected input remains an error", "discriminator": "trace the later observed branch", "expected_outcomes": map[string]any{"support": "branch loses the rejection", "refute": "branch preserves the rejection"}, "source_finding_ids": []string{"finding-minimal-late"}, "evidence_refs": []string{"finding:finding-minimal-late"}}}
	stageJSON(t, root, strings.TrimPrefix(qpaths[0], root+string(filepath.Separator)), q)
	out, diag := s7ListenerEvent(t, root, "PostToolUse", map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": qpaths[0]}, "tool_response": map[string]any{"filePath": qpaths[0]}})
	after := stageSnapshot(t, root)
	next := after.State["review"].(map[string]any)["investigation"].(map[string]any)
	sources, _ := json.Marshal(next["source_finding_ids"])
	if !strings.Contains(string(sources), "finding-minimal-first") || !strings.Contains(string(sources), "finding-minimal-late") {
		t.Fatalf("Main selection did not reach Case: %s; %s %s", sources, out, diag)
	}
	data, err := os.ReadFile(filepath.Join(root, next["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document["hypotheses"].([]any)) != 1 || !strings.Contains(string(data), "finding-minimal-late") {
		t.Fatalf("question not registered: %s", data)
	}
	previous, err := os.ReadFile(filepath.Join(root, ptr["path"].(string)))
	if err != nil || !reflect.DeepEqual(oldCase, previous) {
		t.Fatal("original Case changed")
	}
	revision := after.Revision
	s7ListenerEvent(t, root, "PostToolUse", map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": qpaths[0]}, "tool_response": map[string]any{"filePath": qpaths[0]}})
	if got := stageSnapshot(t, root).Revision; got != revision {
		t.Fatalf("replay duplicated question: %d -> %d", revision, got)
	}
	var investigatorPackets []workpacket.HypothesisResultPacket
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	if err := json.Unmarshal([]byte(issued), &investigatorPackets); err != nil || len(investigatorPackets) != 1 {
		t.Fatalf("issue selected original Investigator question: %s (%v); %s %s", issued, err, out, diag)
	}
	packet := investigatorPackets[0]
	found := false
	for _, source := range packet.Inputs.Findings {
		if source.FindingID == "finding-minimal-late" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Investigator original packet omitted selected late Finding: %+v", packet.Inputs.Findings)
	}
	packet.Draft.Answer = json.RawMessage(`{"method":"inspect the actual later boundary","evidence_refs":["finding:finding-minimal-late"],"source_boundary_refs":["finding:finding-minimal-late"],"observed":"the selected branch loses the rejection","counterfactual_or_discriminator":"compare the selected branch with an error response","result":"supported","explains_finding_ids":["finding-minimal-late"],"does_not_explain":[],"new_hypotheses":[]}`)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	payload := s7ListenerPayload(t, "opaque-late-investigator", "Investigate original "+filepath.Join(root, packet.ViewPath))
	payload["agent_transcript_path"] = filepath.Join(strings.TrimSuffix(payload["transcript_path"].(string), ".jsonl"), "subagents", "agent-"+payload["agent_id"].(string)+".jsonl")
	payload["last_assistant_message"] = "The assigned discriminator supports the observed boundary."
	out, diag = s7ListenerEvent(t, root, "PreToolUse", payload)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID)
	if err != nil || !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("late Finding Investigator result was not naturally received: %+v %v; %s %s", receipt, err, out, diag)
	}
}

func lateFindingQuestionFiles(t *testing.T, root, name string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(filepath.Join(root, ".claude", "submissions", "s8"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == name {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}
