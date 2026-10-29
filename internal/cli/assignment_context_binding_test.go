package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func prepareNativeBinding(t *testing.T, f stageAnswerFixture, owner string, extra ...string) map[string]any {
	t.Helper()
	args := []string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", owner}
	args = append(args, extra...)
	output := stageCLI(t, args...)
	var binding map[string]any
	if err := json.Unmarshal([]byte(output), &binding); err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestNativeContextBindingFreezesAcrossWorkingStateAndRejectsSourceDrift(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	b := prepareNativeBinding(t, f, p.Binding.Owner)
	if _, present := b["owned_check_commands"]; present {
		t.Fatalf("legacy/non-closing context unexpectedly published S6 owner-check commands: %+v", b["owned_check_commands"])
	}
	ref := b["context_ref"].(map[string]any)
	revision := stageSnapshot(t, f.root).Revision
	again := prepareNativeBinding(t, f, p.Binding.Owner)
	data, _ := json.Marshal(b)
	retried, _ := json.Marshal(again)
	if !bytes.Equal(data, retried) || stageSnapshot(t, f.root).Revision != revision {
		t.Fatal("binding retry consumed another generation/commit")
	}
	actual, initialCapture := explicitContextRead(t, f, p.Binding.Owner)
	if actual.Path != ref["path"] || actual.SHA256 != ref["sha256"] {
		t.Fatal("lifecycle did not consume frozen binding")
	}
	f.begin(t, p, nil)
	actual, _ = explicitContextRead(t, f, p.Binding.Owner)
	if actual.Path != ref["path"] || actual.SHA256 != ref["sha256"] {
		t.Fatal("working state silently rebuilt captured profile")
	}

	_, capture := explicitContextRead(t, f, p.Binding.Owner)
	snap := stageSnapshot(t, f.root)
	var live workstate.EntityProjection
	for _, raw := range snap.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == p.Binding.Owner {
			var err error
			live, err = workstate.AgentProjection(row, snap.State["schema_version"].(string))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if live.Status != workstate.Active || live.Activity != "working" || capture["agent_state"] != initialCapture["agent_state"] {
		t.Fatalf("historical capture was mistaken for live authority: live=%+v capture=%v", live, capture["agent_state"])
	}

	source := filepath.Join(f.root, ".claude/skills/document-verification/SKILL.md")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(source, append(original, []byte("\nchanged installed source\n")...), 0600)

	var out, errout bytes.Buffer
	before := stageSnapshot(t, f.root).Revision
	code := cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner}, nil, &out, &errout)
	if code == 0 || stageSnapshot(t, f.root).Revision != before || !strings.Contains(errout.String(), "document-verification") {
		t.Fatalf("source drift did not preserve binding with accurate source feedback: %d %s", code, &errout)
	}
	os.WriteFile(source, original, 0600)
	restored := prepareNativeBinding(t, f, p.Binding.Owner)
	restoredBytes, _ := json.Marshal(restored)
	if !bytes.Equal(data, restoredBytes) {
		t.Fatal("original matching source restoration changed profile")
	}
}

func TestNativeContextBindingNamedRevisionUsesExactReceivedPredecessor(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	b := prepareNativeBinding(t, f, p.Binding.Owner)
	var out, errout bytes.Buffer
	args := []string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner, "--replace-generation", "1"}
	before := stageSnapshot(t, f.root).Revision
	if code := cli.Run(args, nil, &out, &errout); code == 0 || stageSnapshot(t, f.root).Revision != before {
		t.Fatal("generic profile refresh was accepted")
	}
	f.begin(t, p, nil)
	receipt := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass"))
	var next workpacket.Packet
	issued := stageCLI(t, "runtime", "stage-answer", "revise", "--root", f.root, "--previous", receipt.SubmissionKey, "--reason", "Re-evaluate the original frozen dependency finding")
	json.Unmarshal([]byte(issued), &next)

	out.Reset()
	errout.Reset()
	staleRevision := stageSnapshot(t, f.root).Revision
	if code := cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner}, nil, &out, &errout); code == 0 || !strings.Contains(errout.String(), "replace-generation") || stageSnapshot(t, f.root).Revision != staleRevision {
		t.Fatalf("explicit preparation silently replaced fixed context: %d %s %s", code, &out, &errout)
	}

	replacement := prepareNativeBinding(t, f, p.Binding.Owner, "--replace-generation", "1")
	if replacement["generation"] != float64(2) || replacement["profile_sha256"] != b["profile_sha256"] || replacement["profile_version"] != b["profile_version"] {
		t.Fatal("named revision changed frozen profile")
	}
	committed := stageSnapshot(t, f.root).Revision
	replayed := prepareNativeBinding(t, f, p.Binding.Owner, "--replace-generation", "1")
	want, _ := json.Marshal(replacement)
	got, _ := json.Marshal(replayed)
	if !bytes.Equal(want, got) || stageSnapshot(t, f.root).Revision != committed {
		t.Fatal("context revision response-loss retry committed another binding")
	}
	_, view := explicitContextRead(t, f, p.Binding.Owner)
	if view["answer_path"] != next.AnswerPath {
		t.Fatal("new binding did not follow the original issued revision")
	}

	stageJSON(t, f.root, next.AnswerPath, completedDraft(next, "pass"))
	if code, out, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "opaque-revised-source-reviewer", filepath.Join(f.root, next.AnswerPath)); code != 0 {
		t.Fatalf("revised original Stop: %d %s %s", code, out, diagnostic)
	}
	if _, received, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, next.ID); err != nil || !received {
		t.Fatalf("fixed context revision replaced original answer admission: %v", err)
	}

}

func TestNativeContextBindingConcurrentPrepareCommitsOnceAndCannotFallbackToMain(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	before := stageSnapshot(t, f.root).Revision
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out, errout bytes.Buffer
			results <- cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner}, nil, &out, &errout)
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result != 0 {
			t.Fatalf("same prepare race failed: %d", result)
		}
	}
	if stageSnapshot(t, f.root).Revision != before+1 {
		t.Fatal("concurrent native prepare committed twice")
	}
	f.prepare(t, p)
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner}, nil, &out, &errout); code == 0 || !strings.Contains(errout.String(), "independent execution") {
		t.Fatalf("fixed binding bypassed independent execution: %d %s", code, errout.String())
	}
}

func TestNativeContextBindingCheckpointPreservesFixedFactsAndRejectsMissingSources(t *testing.T) {
	f := newStageAnswerFixture(t)
	b := prepareNativeBinding(t, f, f.packets[0].Binding.Owner)
	snap := stageSnapshot(t, f.root)
	journal, err := os.ReadFile(filepath.Join(f.root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateCheckpointIndices(f.root, snap.State, journal); err != nil {
		t.Fatalf("valid original fixed context checkpoint: %v", err)
	}
	ref := b["context_ref"].(map[string]any)
	path := filepath.Join(f.root, ref["path"].(string))
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("changed fixed metadata"), 0600)
	if err := runtime.ValidateCheckpointIndices(f.root, snap.State, journal); err == nil {
		t.Fatal("checkpoint restored another/lost fixed profile source")
	}
	os.WriteFile(path, source, 0600)
	var events []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(journal), []byte("\n")) {
		var event map[string]any
		json.Unmarshal(line, &event)
		if event["transition_id"] == "CONTEXT-PREPARE" {
			event["transition_id"] = "UNRELATED-METADATA"
			event["idempotency_key"] = "unrelated-metadata-key"
		}
		events = append(events, event)
	}
	var filtered bytes.Buffer
	for _, event := range events {
		data, _ := json.Marshal(event)
		filtered.Write(data)
		filtered.WriteByte('\n')
	}
	if err := runtime.ValidateCheckpointIndices(f.root, snap.State, filtered.Bytes()); err == nil || !strings.Contains(err.Error(), "original preparation journal") {
		t.Fatalf("fixed profile source without original prepare journal was accepted or failed for another reason: %v", err)
	}
}

func TestContextRoleRenderReturnsActualCandidateWithoutChangingAssets(t *testing.T) {
	f := newStageAnswerFixture(t)
	for _, name := range agentcontext.CoreSkills("backend-builder") {
		data, err := os.ReadFile(filepath.Join("../..", "skills", name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.root, ".claude/skills", name, "SKILL.md")
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, data, 0600)
	}
	rolePath := filepath.Join(f.root, ".claude/agents/backend-builder.md")
	original, err := os.ReadFile("../../agents/backend-builder.md")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(rolePath, original, 0600)
	before := stageSnapshot(t, f.root).Revision
	output := stageCLI(t, "runtime", "assignment-context", "render-role", "--root", f.root)
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	generated := []byte(result["generated_role"].(string))
	if result["enabled"] != false || workpacket.Digest(generated) != result["generated_sha256"] || len(result["core_skills"].([]any)) != 4 {
		t.Fatal("invalid candidate or false enabled claim")
	}
	after, _ := os.ReadFile(rolePath)
	if !bytes.Equal(after, original) || stageSnapshot(t, f.root).Revision != before {
		t.Fatal("render candidate changed installed role or Runtime")
	}
	source := result["source_ref"].(map[string]any)
	if source["path"] != ".claude/agents/backend-builder.md" || source["sha256"] != workpacket.Digest(original) {
		t.Fatal("role candidate guessed its source")
	}
}

func TestNativeContextBindingWithoutPacketIndicesPublishesOriginalCheckpoint(t *testing.T) {
	f := newStageAnswerFixtureMode(t, false)
	prepareNativeBinding(t, f, "reviewer-spec")
	snap := stageSnapshot(t, f.root)
	entities := snap.State["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	submissions, _ := entities["submissions"].([]any)
	if len(packets) != 0 || len(submissions) != 0 {
		t.Fatal("knowledge binding invented packet/reception facts")
	}
	path, err := runtime.LatestRuntimeCheckpoint(f.root)
	if err != nil || path == "" {
		t.Fatalf("native-only binding has no original pair checkpoint: %v", err)
	}
	pair, err := runtime.ReadRuntimeCheckpoint(f.root, path, semantic.RuntimeCandidateValidator{})
	if err != nil {
		t.Fatalf("native-only original pair is unverifiable: %v", err)
	}
	if err := runtime.ValidateCheckpointRestore(f.root, pair); err != nil {
		t.Fatalf("native-only original checkpoint restore rejected: %v", err)
	}
}
