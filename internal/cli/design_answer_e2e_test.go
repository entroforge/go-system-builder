package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Only the authorized planning baseline and authored documents are seeded.
// Issuance, reception, transition and registration use original production code.
func newDesignFixture(t *testing.T, impact string, beforeBind ...func(string)) (string, workpacket.DesignPacket) {
	t.Helper()
	root := fixtures.FreshRoot(t)
	for _, configure := range beforeBind {
		configure(root)
	}
	state := fixtures.BaseState(t, root, "planning", "design", 0)
	state["review"].(map[string]any)["round"] = 0
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["bound_req"].(map[string]any)["metadata"].(map[string]any)["ui_impact"] = impact
	entities := state["entities"].(map[string]any)
	entities["work_packets"], entities["submissions"] = []any{}, []any{}
	bound := state["bound_req"].(map[string]any)
	state["documents"] = []any{map[string]any{"id": bound["id"], "kind": "req", "path": bound["path"], "version": bound["version"], "sha256": bound["sha256"], "status": "locked", "generation": 1}}
	arch := filepath.Join(root, "docs/architecture/ARCHITECTURE-039.md")
	if err := os.MkdirAll(filepath.Dir(arch), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arch, []byte("# ARCHITECTURE-039\n> Status: locked\n> Version: v1.0.0\n\nAuthored design fixture.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtures.WriteState(t, root, state)
	// Synthetic entry preparation uses the original issuer once; normal
	// Pre/Stop below only read the current view and professional answer.
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
	p, err := service.IssueDesign(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return root, p
}

func designDraft(p workpacket.DesignPacket) workpacket.DesignDraft {
	a := p.Draft
	conclusion, path := "pass", "docs/architecture/ARCHITECTURE-039.md"
	a.Answer = workpacket.DesignAnswer{Conclusion: &conclusion, ArchitecturePath: &path}
	return a
}

func assertDesignCheckpoint(t *testing.T, root string) {
	t.Helper()
	s := stageSnapshot(t, root)
	journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateCheckpointIndices(root, s.State, journal); err != nil {
		t.Fatal(err)
	}
}

func TestDesignAnswerMainSaveOriginalTransitionAndReplay(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	assertDesignCheckpoint(t, root)
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}, "cwd": root})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("Hook %d %s %s", code, out.String(), errout.String())
	}
	if strings.Contains(out.String(), "Questionnaire: "+filepath.Join(root, p.AnswerPath)) || !strings.Contains(out.String(), "Submitted questionnaire (not the current pending answer):") {
		t.Fatalf("completed S2 answer presented as current questionnaire: %s", out.String())
	}
	if !strings.Contains(out.String(), "S2 SUBMISSION RECEIPT") {
		t.Fatalf("missing S2 receipt: %s", out.String())
	}
	s := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(s.State, p.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		t.Fatalf("original reception: %+v %v %s", record, err, out.String())
	}
	life := s.State["lifecycle"].(map[string]any)
	if life["state"] != "planning" || life["phase"] != "contracts" {
		t.Fatalf("original route: %+v", life)
	}
	prepared := false
	for _, raw := range s.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] == "s3-contract-v1" {
			prepared = true
			for _, key := range []string{"view_path", "answer_path"} {
				path := row[key].(string)
				if _, err := os.Stat(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out.String(), path) {
					t.Fatalf("next S3 path absent from receipt: %s", out.String())
				}
			}
		}
	}
	if !prepared {
		if strings.Contains(out.String(), "Complete the named materials") || !strings.Contains(out.String(), "system_followup") || !strings.Contains(out.String(), "original next-stage questionnaire preparation failed") {
			t.Fatalf("system preparation failure shifted to author or lost waiting owner: %s", out.String())
		}
		if !strings.Contains(out.String(), "docs/design") || !strings.Contains(out.String(), "transition are recorded") {
			t.Fatalf("missing next-stage material did not preserve accepted transition: %s", out.String())
		}
		planningBoundaryEvent(t, root, "PreToolUse", "Bash", "", false)
		writeContractSource(t, root, "docs/design/README.md", "# Design sources\n")
		fixtures.CommitCurrentFixture(t, root)
		changed, changeErr := planningBoundaryEvent(t, root, "PostToolUse", "Bash", "", false)
		s = stageSnapshot(t, root)
		for _, raw := range s.State["entities"].(map[string]any)["work_packets"].([]any) {
			row := raw.(map[string]any)
			if row["template_id"] != "s3-contract-v1" {
				continue
			}
			prepared = true
			for _, key := range []string{"view_path", "answer_path"} {
				path := row[key].(string)
				if _, err := os.Stat(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(changed+changeErr, path) {
					t.Fatalf("actual Git preparation did not deliver current path: %s %s", changed, changeErr)
				}
			}
		}
		if !prepared {
			t.Fatalf("actual material/Git change did not finish S3 preparation: %s %s", changed, changeErr)
		}
	}

	registered := false
	for _, raw := range s.State["documents"].([]any) {
		d := raw.(map[string]any)
		if d["path"] == "docs/architecture/ARCHITECTURE-039.md" {
			registered = true
		}
	}
	if !registered {
		t.Fatal("original action did not register design")
	}
	assertDesignCheckpoint(t, root)
	// Replaying this same ordinary save after transition is read-only.
	out.Reset()
	errout.Reset()
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("save replay: %d %s %s", code, out.String(), errout.String())
	}
	if stageSnapshot(t, root).State["revision"] != s.State["revision"] {
		t.Fatal("same answer replay changed Runtime")
	}
	assertDesignCheckpoint(t, root)
}

func TestDesignAnswerDraftAndCurrentFactsDoNotRequireSourceProof(t *testing.T) {
	for _, mode := range []string{"incomplete", "requirement_drift", "unknown_ui"} {
		t.Run(mode, func(t *testing.T) {
			impact := "none"
			if mode == "unknown_ui" {
				impact = "unknown"
			}
			root, p := newDesignFixture(t, impact)
			if mode != "incomplete" {
				draft := designDraft(p)
				draft.Title = "UI metadata may change without changing the professional answer"
				stageJSON(t, root, p.AnswerPath, draft)
			}
			if mode == "requirement_drift" {
				if err := os.WriteFile(filepath.Join(root, p.Binding.Requirement.Path), []byte("Current requirement material changed; no source certificate is required.\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
			before := stageSnapshot(t, root)
			r, err := service.SaveDesign(context.Background(), p.AnswerPath)
			if err != nil {
				t.Fatalf("professional recording: %+v %v", r, err)
			}
			after := stageSnapshot(t, root)
			if mode == "incomplete" {
				if r.Accepted || !strings.Contains(r.NextAction, "conclusion") || before.State["revision"] != after.State["revision"] || len(after.State["evidence"].([]any)) != 0 {
					t.Fatalf("empty conclusion must remain a visible draft: %+v", r)
				}
			} else {
				if !r.Accepted {
					t.Fatalf("actual professional fields were not recorded: %+v", r)
				}
				if mode == "unknown_ui" {
					life := after.State["lifecycle"].(map[string]any)
					if life["phase"] != "design" || r.WorkComplete {
						t.Fatal("unresolved actual UI impact advanced the design")
					}
				}
			}
		})
	}
}

func TestDesignAnswerPendingRecoveryPreservesReception(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	r, err := s.SaveDesign(ctx, p.AnswerPath)
	if err != nil || !r.Accepted || r.WorkComplete || len(r.WaitingOn) == 0 {
		t.Fatalf("pending receipt: %+v %v", r, err)
	}
	before := stageSnapshot(t, root)
	prior, found, err := runtime.FindSubmission(before.State, p.ID)
	if err != nil || !found || prior.ControlStatus != "pending" {
		t.Fatalf("pending record: %+v %v", prior, err)
	}
	assertDesignCheckpoint(t, root)
	if r, err := s.SaveDesign(context.Background(), p.AnswerPath); err != nil || !r.Accepted {
		t.Fatalf("same saved answer did not continue the original outlet: %+v %v", r, err)
	}
	after := stageSnapshot(t, root)
	next, _, err := runtime.FindSubmission(after.State, p.ID)
	if err != nil || next.ControlStatus != "advanced" || next.Key != prior.Key || next.ContentHash != prior.ContentHash || next.EvidenceID != prior.EvidenceID || len(after.State["evidence"].([]any)) != len(before.State["evidence"].([]any)) {
		t.Fatalf("recovery changed domain reception: %+v %v", next, err)
	}
	assertDesignCheckpoint(t, root)
	revision := after.State["revision"]
	if _, err := s.SaveDesign(context.Background(), p.AnswerPath); err != nil {
		t.Fatal(err)
	}
	if stageSnapshot(t, root).State["revision"] != revision {
		t.Fatal("recovery replay changed Runtime")
	}
	changed := designDraft(p)
	other := "docs/architecture/ARCHITECTURE-other.md"
	changed.Answer.ArchitecturePath = &other
	stageJSON(t, root, p.AnswerPath, changed)
	fresh, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := fresh.SaveDesign(context.Background(), p.AnswerPath); err == nil || r.Accepted {
		t.Fatalf("different professional answer replay accepted: %+v %v", r, err)
	}
	if stageSnapshot(t, root).State["revision"] != revision {
		t.Fatal("conflicting replay changed Runtime")
	}
}

func TestDesignAnswerOriginalPairRecovery(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
	before := stageSnapshot(t, root)
	path, pair := latestStageCheckpoint(t, root)
	if !reflect.DeepEqual(pair.State, before.State) {
		t.Fatal("checkpoint dropped S2 facts")
	}
	damageStagePair(t, root)
	plan := checkpointPlan(t, root, path, pair.State)
	stageCLI(t, "runtime", "recover", "apply", "--root", root, "--plan", plan, "--approved-by", "operator")
	if !reflect.DeepEqual(stageSnapshot(t, root).State, before.State) {
		t.Fatal("original pair restoration changed S2 reception or cursor")
	}
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
	if stageSnapshot(t, root).State["revision"] != before.State["revision"] {
		t.Fatal("restored replay repeated original transition")
	}
	assertDesignCheckpoint(t, root)
}

func TestContractAuthorityUsesOriginalS2RegisteredDesign(t *testing.T) {
	root, design := newDesignFixture(t, "none")
	stageJSON(t, root, design.AnswerPath, designDraft(design))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", design.AnswerPath)
	snap := stageSnapshot(t, root)
	binding, refs, err := runtime.CurrentContractBinding(snap.State)
	if err != nil || binding.Stage != "S3" || len(refs) != 1 {
		t.Fatalf("original S2 registration: %+v %+v %v", binding, refs, err)
	}
	p, err := workpacket.BuildContracts(binding, workpacket.ContractInputs{Designs: refs})
	if err != nil {
		t.Fatal(err)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	files := service.Files
	if err := runtime.ValidateContractPacket(root, snap.State, p, files); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateContractPacket(t.TempDir(), snap.State, p, files); err == nil {
		t.Fatal("other root granted S3 authority")
	}
	if err := os.WriteFile(filepath.Join(root, refs[0].Path), []byte("changed design"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateContractPacket(root, snap.State, p, files); err == nil {
		t.Fatal("changed design reused S3 frozen input")
	}
	snap.State["lifecycle"].(map[string]any)["phase"] = "tasks"
	if _, _, err := runtime.CurrentContractBinding(snap.State); err == nil {
		t.Fatal("S3 Main fabricated outside contracts")
	}
}

// Start at the actual entry, without a seeded S2 packet or hand-built source
// metadata. S1's original save prepares S2 and S2's original save prepares S3.
func TestDesignNormalEntryDerivesMachineFactsFromTwoProfessionalAnswers(t *testing.T) {
	root := initEntryQuestionnaireRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	bindingPath := filepath.Join(root, ".claude/submissions/requirement-binding.md")
	entryQuestionnaireHook(t, root, "SessionStart", bindingPath)
	binding, err := os.ReadFile(bindingPath)
	if err != nil {
		t.Fatal(err)
	}
	filled := string(binding)
	for question, answer := range map[string]string{
		"Who actually authorized this binding?":     "actual-test-human",
		"Which development branch was selected?":    "feature/req",
		"What is the explicit release destination?": "origin/main",
	} {
		filled = strings.Replace(filled, "- "+question+" ", "- "+question+" "+answer, 1)
	}
	if err := os.WriteFile(bindingPath, []byte(filled), 0o600); err != nil {
		t.Fatal(err)
	}
	feedback := entryQuestionnaireHook(t, root, "PostToolUse", bindingPath)
	snap := stageSnapshot(t, root)
	var packet workpacket.DesignPacket
	// Inspect original issuance only to verify the paths the Hook delivered;
	// no explicit issue command, Runtime edit, digest or envelope is submitted.
	for _, raw := range snap.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] == "s2-design-v1" {
			encoded, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &packet); err != nil {
				t.Fatal(err)
			}
		}
	}
	if packet.ID == "" || !strings.Contains(feedback, filepath.Join(root, packet.ViewPath)) || !strings.Contains(feedback, filepath.Join(root, packet.AnswerPath)) {
		t.Fatalf("normal binding did not deliver the current S2 paths: %s", feedback)
	}
	answerPath := filepath.Join(root, packet.AnswerPath)
	data, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	answer := draft["answer"].(map[string]any)
	if len(answer) != 2 || answer["conclusion"] != nil || answer["architecture_path"] != nil {
		t.Fatalf("normal S2 draft is not exactly two unfinished professional answers: %+v", answer)
	}
	const architecturePath = "docs/architecture/ARCHITECTURE-098.md"
	const architectureBody = "# ARCHITECTURE-098\n\n> 状态：locked\n> 版本：v1.2.3\n> 关联需求：`docs/requirements/REQ-098.md`\n\nAuthored architecture boundaries and decisions.\n"
	for path, content := range map[string]string{
		architecturePath:        architectureBody,
		"docs/design/README.md": "# Design\n\nNo module behavior or UI changes in this synthetic entry.\n",
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fixtures.CommitCurrentFixture(t, root)
	answer["conclusion"], answer["architecture_path"] = "pass", architecturePath
	stageJSON(t, root, packet.AnswerPath, draft)
	feedback = entryQuestionnaireHook(t, root, "PostToolUse", answerPath)
	after := stageSnapshot(t, root)
	record, accepted, err := runtime.FindSubmission(after.State, packet.ID)
	if err != nil || !accepted || record.ControlStatus != "advanced" || after.State["lifecycle"].(map[string]any)["phase"] != "contracts" {
		t.Fatalf("normal two-field save did not complete the original S2 outlet: %+v %v %s", record, err, feedback)
	}
	var material map[string]any
	for _, raw := range after.State["evidence"].([]any) {
		ev := raw.(map[string]any)
		if ev["id"] == record.EvidenceID {
			material = readJSONMap(t, filepath.Join(root, ev["path"].(string)))
		}
	}
	if material == nil || material["runtime_id"] != packet.Binding.RuntimeID || material["created_at"] == "" || material["producer_agent_id"] != "Main" {
		t.Fatalf("original consumer did not derive the evidence metadata: %+v", material)
	}
	subjects := material["subject_refs"].([]any)
	if len(subjects) != 2 {
		t.Fatalf("original consumer did not bind exact requirement and architecture subjects: %+v", subjects)
	}
	arch := subjects[1].(map[string]any)
	if arch["path"] != architecturePath || arch["version"] != "v1.2.3" || arch["sha256"] != sha256Hex([]byte(architectureBody)) {
		t.Fatalf("original consumer did not bind actual architecture bytes/version: %+v", subjects)
	}
	prepared := false
	for _, raw := range after.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] == "s3-contract-v1" {
			prepared = true
			for _, key := range []string{"view_path", "answer_path"} {
				path := filepath.Join(root, row[key].(string))
				if _, err := os.Stat(path); err != nil || !strings.Contains(feedback, path) {
					t.Fatalf("normal S2 outlet did not deliver actual S3 %s: %v %s", key, err, feedback)
				}
			}
		}
	}
	if !prepared {
		t.Fatalf("normal S2 outlet did not prepare S3: %s", feedback)
	}
}
