package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func nativeContextHook(t *testing.T, f stageAnswerFixture, owner string) (string, int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStart", "agent_id": owner, "cwd": f.root})
	var out, errout bytes.Buffer
	code := cli.Run([]string{"hook", "--root", f.root, "--event", "SubagentStart"}, bytes.NewReader(body), &out, &errout)
	var wire struct {
		Specific struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &wire); err != nil {
		t.Fatalf("Hook wire: %v %s %s", err, out.String(), errout.String())
	}
	return wire.Specific.Context, code, errout.String()
}

// Explicit compatibility preparation is the source consumer. Native Start
// remains bounded and does not issue a generic context or PLAN prerequisite.
func explicitContextRead(t *testing.T, f stageAnswerFixture, owner string) (agentcontext.Reference, map[string]any) {
	t.Helper()
	binding := prepareNativeBinding(t, f, owner)
	raw := binding["context_ref"].(map[string]any)
	ref := agentcontext.Reference{Path: raw["path"].(string), SHA256: raw["sha256"].(string)}
	data, err := os.ReadFile(filepath.Join(f.root, ref.Path))
	if err != nil || workpacket.Digest(data) != ref.SHA256 {
		t.Fatalf("original source: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", data); err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return ref, c
}

func TestExplicitAssignmentContextDeliversSourcesAndPreservesLegacyDraft(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	ref, c := explicitContextRead(t, f, p.Binding.Owner)
	before := stageSnapshot(t, f.root)
	if again, _ := explicitContextRead(t, f, p.Binding.Owner); again != ref {
		t.Fatal("repeat original preparation changed fixed source")
	}
	after := stageSnapshot(t, f.root)
	// First explicit preparation records its binding. Reading and replaying
	// that original consumer must preserve the same authority facts.
	for _, key := range []string{"entities", "lifecycle", "baseline", "evidence"} {
		left, _ := json.Marshal(before.State[key])
		right, _ := json.Marshal(after.State[key])
		if !bytes.Equal(left, right) {
			t.Fatalf("projection/Controller changed original dispatch or authority: %s", key)
		}
	}
	dispatch := c["dispatch"].(map[string]any)
	if dispatch["owner"] != p.Binding.Owner || dispatch["assignment_id"] != p.Binding.AssignmentID || dispatch["task_id"] != f.taskID || c["work_packet_ref"] != p.ViewPath || c["answer_path"] != p.AnswerPath {
		t.Fatal("invented dispatch or foreign answer")
	}
	if len(c["product_write_paths"].([]any)) != 0 {
		t.Fatal("S5 projected product authority")
	}
	if c["context_selection"].(map[string]any)["mode"] != "shadow" {
		t.Fatal("projection enabled profile")
	}
	inputs := c["frozen_inputs"].(map[string]any)
	for path, hash := range inputs {
		data, err := os.ReadFile(filepath.Join(f.root, path))
		if err != nil || workpacket.Digest(data) != hash {
			t.Fatalf("unavailable actual source %s %v", path, err)
		}
	}
	order := c["input_read_order"].([]any)
	if len(order) < 3 || !strings.HasPrefix(order[0].(string), "docs/dev/tasks/") {
		t.Fatal("lost original bottom-up source")
	}

	// The explicit compatibility projection retains its historical PLAN draft;
	// native S5 does not submit it or use it as a dispatch prerequisite.
	planPath := c["plan_draft_path"].(string)
	draftBytes, err := os.ReadFile(filepath.Join(f.root, planPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", draftBytes); err == nil {
		t.Fatal("blank compatibility PLAN became an authored professional report")
	}
	var plan map[string]any
	if err := json.Unmarshal(draftBytes, &plan); err != nil {
		t.Fatal(err)
	}
	plan["objective"] = "Evaluate my original registered independent document responsibility"
	plan["steps"] = []any{map[string]any{"description": "Read the assigned chain and cite concrete gaps", "target": f.taskPath}}
	plan["assertion_checks"] = []any{map[string]any{"assertion": "Required responsibility is fully accounted for", "oracle": "Cited findings compared with frozen inputs"}}
	plan["risks_blockers"] = []any{}
	stageJSON(t, f.root, planPath, plan)
	authoredPlan, err := os.ReadFile(filepath.Join(f.root, planPath))
	if err != nil {
		t.Fatal(err)
	}

	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	authored, err := os.ReadFile(filepath.Join(f.root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := explicitContextRead(t, f, p.Binding.Owner)
	if again != ref {
		t.Fatal("same sources created another fixed context")
	}
	retried, err := os.ReadFile(filepath.Join(f.root, p.AnswerPath))
	if err != nil || !bytes.Equal(authored, retried) {
		t.Fatal("source preparation overwrote the professional answer", err)
	}
	retriedPlan, err := os.ReadFile(filepath.Join(f.root, planPath))
	if err != nil || !bytes.Equal(authoredPlan, retriedPlan) {
		t.Fatal("explicit compatibility preparation overwrote authored PLAN", err)
	}

	if code, out, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "opaque-source-reviewer", filepath.Join(f.root, p.AnswerPath)); code != 0 {
		t.Fatalf("original S5 Stop: %d %s %s", code, out, diagnostic)
	}
	if _, received, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, p.ID); err != nil || !received {
		t.Fatalf("original S5 consumer did not receive professional answer: %v", err)
	}

}

func TestNativeAssignmentContextDoesNotGuessOwnerOrFallbackFromExecution(t *testing.T) {
	f := newStageAnswerFixture(t)
	context, _, _ := nativeContextHook(t, f, "unknown-owner")
	if strings.Contains(context, "assignment-context/native") || strings.Contains(context, f.packets[0].Binding.Owner) {
		t.Fatal("unknown identity got another owner's view")
	}
	p := f.packets[0]
	f.prepare(t, p)
	context, _, _ = nativeContextHook(t, f, p.Binding.Owner)
	if strings.Contains(context, "assignment-context/native") {
		t.Fatal("independent execution fell back to Main projection")
	}
}

func TestExplicitAssignmentContextRejectsMissingSourcesWithoutAuthorOverwrite(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]

	_, c := explicitContextRead(t, f, p.Binding.Owner)
	path := c["answer_path"].(string)
	stageJSON(t, f.root, path, completedDraft(p, "pass"))
	before, err := os.ReadFile(filepath.Join(f.root, path))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(f.root, ".claude/skills/document-verification/SKILL.md")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	stateBefore, journalBefore := s6CanonicalRuntimePair(t, f.root)
	var out, diagnostic bytes.Buffer
	code := cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner}, nil, &out, &diagnostic)
	if code == 0 || !strings.Contains(diagnostic.String(), "document-verification") {
		t.Fatalf("missing installed source became ready: %d %s %s", code, &out, &diagnostic)
	}
	s6AssertCanonicalRuntimePair(t, f.root, stateBefore, journalBefore)
	after, err := os.ReadFile(filepath.Join(f.root, path))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed explicit preparation overwrote professional answer", err)
	}

}

func TestRegisteredTeammateProjectionMatchesExplicitContextAndPreservesAuthority(t *testing.T) {
	f := newStageAnswerFixture(t)
	// Deliver the original legacy Skill set exactly as an installation does.
	// These are test setup assets, not production fallback from missing files.
	for _, name := range agentcontext.LegacySkills("document-verifier") {
		source, err := os.ReadFile(filepath.Join("../..", "skills", name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.root, ".claude/skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, source, 0600); err != nil {
			t.Fatal(err)
		}
	}
	example, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	json.Unmarshal(example, &messages)
	var template map[string]any
	for _, m := range messages {
		if m["message_type"] == "readback_request" {
			template = m
			break
		}
	}
	template["task_id"] = f.taskID
	docs := []any{}
	state := stageSnapshot(t, f.root).State
	for i, kind := range []string{"task", "contract", "req", "design"} {
		for _, raw := range state["documents"].([]any) {
			doc := raw.(map[string]any)
			if doc["kind"] == kind {
				docs = append(docs, map[string]any{"id": doc["id"], "kind": kind, "path": doc["path"], "version": doc["version"], "sha256": doc["sha256"], "read_order": i + 1})
				break
			}
		}
	}
	template["documents"] = docs
	path := ".claude/registered-readback-template.json"
	stageJSON(t, f.root, path, template)
	// Warm only the original Controller milestone, then compare both entrances.
	nativeRef, _ := explicitContextRead(t, f, f.packets[0].Binding.Owner)
	beforeState, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	beforeJournal, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-events.jsonl"))
	output := stageCLI(t, "team", "launch", "--root", f.root, "--manifest", ".claude/workgroups/REQ-039/review/manifest.json", "--request-template", path)
	var packages []map[string]any
	if err := json.Unmarshal([]byte(output), &packages); err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 {
		t.Fatal("lost independent owner")
	}
	validator := schema.NewEmbeddedValidator()
	for _, p := range packages {
		data, _ := json.Marshal(p)
		if err := validator.ValidateBytes("agent-message.schema.json", data); err != nil {
			t.Fatal(err)
		}
		ref := p["assignment_context_ref"].(map[string]any)
		actual, err := os.ReadFile(filepath.Join(f.root, ref["path"].(string)))
		if err != nil || workpacket.Digest(actual) != ref["sha256"] {
			t.Fatal("teammate got an unreadable context")
		}
		if p["agent_id"] == f.packets[0].Binding.Owner && (ref["path"] != nativeRef.Path || ref["sha256"] != nativeRef.SHA256) {
			t.Fatal("native and teammate produced different current sources")
		}
	}
	afterState, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	afterJournal, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-events.jsonl"))
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("read-only team projection mutated authority")
	}
	// A schema-shaped request with a guessed subject digest cannot be emitted
	// as the registered owner's launch, even when its file exists.
	docs[0].(map[string]any)["sha256"] = strings.Repeat("a", 64)
	stageJSON(t, f.root, path, template)
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"team", "launch", "--root", f.root, "--manifest", ".claude/workgroups/REQ-039/review/manifest.json", "--request-template", path}, nil, &out, &errout); code == 0 || !strings.Contains(errout.String(), "differs from manifest") {
		t.Fatalf("guessed source became registered launch: %d %s", code, errout.String())
	}
}

func TestNativeAssignmentContextDoesNotIssuePlanForRetiredS5Peer(t *testing.T) {
	f := newStageAnswerFixture(t)
	first := f.packets[0]
	f.begin(t, first, nil)
	receipt := saveStageHook(t, f.root, f.root, first.Binding.Owner, first.AnswerPath, completedDraft(first, "fix_required"))
	if !receipt.WorkComplete || stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"] != "planning" {
		t.Fatal("original FIX did not retire its S5 entry")
	}

	before := stageSnapshot(t, f.root)
	context, _, _ := nativeContextHook(t, f, f.packets[1].Binding.Owner)
	if strings.Contains(context, "first Read ") || strings.Contains(context, "Main must repair the original source") {
		t.Fatalf("retired peer received a new source obligation: %s", context)
	}
	after := stageSnapshot(t, f.root)
	for _, key := range []string{"entities", "evidence"} {
		left, _ := json.Marshal(before.State[key])
		right, _ := json.Marshal(after.State[key])
		if !bytes.Equal(left, right) {
			t.Fatalf("retired peer issued or received new %s", key)
		}
	}

	if _, err := os.Stat(filepath.Join(f.root, ".claude/assignment-context/native")); !os.IsNotExist(err) {
		t.Fatalf("retired responsibility published new context: %v", err)
	}
}
