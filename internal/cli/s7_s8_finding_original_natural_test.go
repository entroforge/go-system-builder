package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
)

// The finding is synthetic test data modeled on the original reviewer answer.
func TestS7NaturalFindingKeepsProfessionalFieldsThroughS8Question(t *testing.T) {
	root, packet := s7ListenerFixture(t)
	finding := map[string]any{
		"claim_id": packet.Binding.ClaimIDs[0], "severity": "P0",
		"expected":       "the original observation can be independently investigated",
		"authority_refs": []string{"REQ-WORKTREE"}, "observed": "the reported symptom was not reproduced on the current tree",
		"observation_mode": "code_inspection", "reproducibility": "not_reproducible",
		"evidence_refs":          []string{"original observation reference"},
		"encounter":              map[string]any{"journey_summary": "read the observation, inspect the tree, record what happened", "last_good": "last known ordinary state", "wall": "the expected symptom was not observed", "first_bad": "no reproducible first bad point"},
		"professional_extension": map[string]any{"note": "the observation is explicitly synthetic", "subject_digest": "business description", "hash": map[string]any{"sha256": "product example"}},
	}
	answer := map[string]any{
		"claim_results":  []any{map[string]any{"claim_id": packet.Binding.ClaimIDs[0], "conclusion": "fail", "method": "inspect the original observation", "observed": "the reported symptom remains an investigation question", "evidence_refs": []string{"original observation reference"}}},
		"blocked_claims": []any{}, "checks": []any{}, "findings": []any{finding}, "deviations": []any{}, "site_lost": []any{}, "verdict": "finding",
	}
	answer["subject_digest"] = map[string]any{"retired": true}
	answer["verification_artifact_digest"] = 7
	packet.Draft.Answer, _ = json.Marshal(answer)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	originalAnswer, err := os.ReadFile(filepath.Join(root, packet.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	// The actual original consumer must publish beyond unknown default
	// Result/Finding parents, then carry its registered paths into S8.
	base := filepath.Join(root, ".claude/evidence", packet.Binding.RuntimeID, "g"+strconv.Itoa(packet.Binding.BaselineGeneration))
	unknownParents := []string{filepath.Join(base, "reviews", packet.Binding.OwnerAgentID), filepath.Join(base, "findings")}
	for _, path := range unknownParents {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("unknown professional material"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	payload := s7ListenerPayload(t, packet.Binding.OwnerAgentID, "Review "+filepath.Join(root, packet.ViewPath))
	out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", payload)
	if !strings.Contains(out, "accepted=true") || !strings.Contains(diagnostic, "original consumer") {
		t.Fatalf("original natural reception: %s %s", out, diagnostic)
	}
	snapshot := stageSnapshot(t, root)
	life := snapshot.State["lifecycle"].(map[string]any)
	if life["state"] != "bug_resolution" || life["phase"] != "investigation" {
		t.Fatalf("original Finding did not enter S8: %#v", life)
	}
	rows := snapshot.State["entities"].(map[string]any)["findings"].([]any)
	if len(rows) != 1 {
		t.Fatalf("registered Findings: %#v", rows)
	}
	row := rows[0].(map[string]any)
	if row["original_finder"] != packet.Binding.OwnerAgentID || row["claim_id"] != packet.Binding.ClaimIDs[0] {
		t.Fatalf("original Finding responsibility changed: %#v", row)
	}
	findingID, findingPath := row["finding_id"].(string), row["path"].(string)
	for _, path := range unknownParents {
		if data, err := os.ReadFile(path); err != nil || string(data) != "unknown professional material" {
			t.Fatalf("unknown default publication parent changed: %s %v", path, err)
		}
	}
	assertFinding := func(data []byte) {
		t.Helper()
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got["reproducibility"] != "not_reproducible" || got["professional_extension"].(map[string]any)["note"] != "the observation is explicitly synthetic" {
			t.Fatalf("lost original professional fields: %s", data)
		}
		if !reflect.DeepEqual(got["professional_extension"], finding["professional_extension"]) {
			t.Fatalf("professional hash content changed: %s", data)
		}
		encounter := got["encounter"].(map[string]any)
		for _, key := range []string{"last_good", "wall", "first_bad"} {
			if encounter[key] != finding["encounter"].(map[string]any)[key] {
				t.Fatalf("lost original encounter %s: %s", key, data)
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(root, findingPath))
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(data)
	reception := snapshot.State["review"].(map[string]any)["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)["result_receipt"].(map[string]any)
	if _, err := os.Lstat(filepath.Join(root, reception["result_ref"].(string)) + ".staging.json"); !os.IsNotExist(err) {
		t.Fatalf("current Result created an ownership sidecar: %v", err)
	}
	resultBytes, err := os.ReadFile(filepath.Join(root, reception["result_ref"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Answer map[string]json.RawMessage `json:"effective_answer"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"subject_digest", "verification_artifact_digest"} {
		if _, exists := result.Answer[key]; exists {
			t.Fatalf("new result registered top-level machine field %s", key)
		}
	}
	s7ListenerEvent(t, root, "PostToolUse", payload)
	if replay := stageSnapshot(t, root); replay.Revision != snapshot.Revision {
		t.Fatalf("original accepted Result replay created another Runtime event: before=%d after=%d", snapshot.Revision, replay.Revision)
	}
	findOne := func(name string) string {
		t.Helper()
		var matches []string
		err := filepath.WalkDir(filepath.Join(root, ".claude/submissions/s8"), func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() && e.Name() == name {
				matches = append(matches, p)
			}
			return err
		})
		if err != nil || len(matches) != 1 {
			t.Fatalf("original S8 %s: %v %v", name, matches, err)
		}
		return matches[0]
	}
	intake := findOne("intake.json")
	stageJSON(t, root, strings.TrimPrefix(intake, root+string(filepath.Separator)), map[string]any{"grouping_rationale": "investigate the original observation without inventing a cause"})
	if out := entryQuestionnaireHook(t, root, "PostToolUse", intake); !strings.Contains(out, "intake committed Case") {
		t.Fatalf("original S8 intake: %s", out)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "agents", "investigator.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "investigator.md"), definition, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude", "agents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "agents", "investigator.md"), definition, 0600); err != nil {
		t.Fatal(err)
	}
	protocol, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "agent-protocol.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "agent-protocol.md"), protocol, 0600); err != nil {
		t.Fatal(err)
	}
	questions := findOne("hypotheses.json")
	stageJSON(t, root, strings.TrimPrefix(questions, root+string(filepath.Separator)), []any{map[string]any{"agent_id": "investigator-finding", "statement": "the source boundary may differ", "invariant": "the observation remains interpretable", "discriminator": "inspect the original source boundary", "expected_outcomes": map[string]any{"support": "boundary differs", "refute": "boundary agrees"}, "source_finding_ids": []string{findingID}, "evidence_refs": []string{"original observation reference"}}})
	if out := entryQuestionnaireHook(t, root, "PostToolUse", questions); !strings.Contains(out, "owner=investigator-finding") {
		t.Fatalf("original hypothesis and dispatch: %s", out)
	}
	packets, err := investigation.IssueHypothesisResultPackets(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), -1, "investigator-finding")
	if err != nil || len(packets) != 1 || len(packets[0].Inputs.Findings) != 1 {
		t.Fatalf("original S8 question: packets=%d err=%v", len(packets), err)
	}
	assertFinding(packets[0].Inputs.Findings[0].Document)
	if saved, err := os.ReadFile(filepath.Join(root, packet.AnswerPath)); err != nil || !reflect.DeepEqual(saved, originalAnswer) {
		t.Fatalf("original author answer changed: %v", err)
	}
}
