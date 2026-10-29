package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/team"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// S4 is actually accepted through the original consumer. The test does not
// assemble an S5 manifest, SHA, version, owner, source list or validation time.
func newUnregisteredS5InputFixture(t *testing.T) string {
	t.Helper()
	root, p := newTaskPlanningAnswerFixture(t, func(root string) {
		for _, rel := range []string{"docs/control/agent-protocol.md", "docs/rules"} {
			copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, rel))
		}
		for _, rel := range []string{"agents/document-verifier.md", "skills/document-verification", "skills/specification-planning", "skills/api-contracts", "skills/testing-strategy", "skills/agent-dispatch"} {
			copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, ".claude", rel))
		}
	})
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.SaveTaskPlanning(context.Background(), p.AnswerPath); err != nil || !r.Accepted {
		t.Fatalf("S4 reception: %+v %v", r, err)
	}
	settleMainPlanningFollowups(t, root)
	return root
}

func s5EntryHook(t *testing.T, root string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "session_id": "s5-prepared-input", "cwd": root})
	var out, diag bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", root}, bytes.NewReader(body), &out, &diag); code != 0 {
		t.Fatalf("entry hook %d: %s %s", code, &out, &diag)
	}
	return out.String() + diag.String()
}

func s5EntryRecovery(t *testing.T, root string, wantCode int) string {
	t.Helper()
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", root}, nil, &out, &diagnostic); code != wantCode {
		t.Fatalf("original S5 preparation recovery: got %d want %d: %s %s", code, wantCode, &out, &diagnostic)
	}
	return out.String() + diagnostic.String() + s5EntryHook(t, root)
}

func TestS5NamedRecoveryPreparesOriginalResponsibilitiesWhileSessionOnlyObserves(t *testing.T) {
	root := newUnregisteredS5InputFixture(t)
	before := stageSnapshot(t, root)
	observed := s5EntryHook(t, root)
	if stageSnapshot(t, root).Revision != before.Revision || !strings.Contains(observed, "runtime stage-answer issue") {
		t.Fatalf("session observation prepared work or omitted named recovery: %s", observed)
	}
	// Recover the interrupted original S4 -> S5 preparation without authoring
	// a metadata questionnaire, synthetic SHA or another confirmation save.
	output := s5EntryRecovery(t, root, 0)
	after := stageSnapshot(t, root)
	packets, err := runtime.S5Packets(root, after.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("entry failed to issue both original responsibilities: packets=%d err=%v output=%s", len(packets), err, output)
	}
	for _, p := range packets {
		if _, found, err := runtime.FindWorkPacket(after.State, p.ID); err != nil || !found {
			t.Fatalf("original packet not registered: %v", err)
		}
		if !strings.Contains(output, filepath.Join(root, p.ViewPath)) || !strings.Contains(output, filepath.Join(root, p.AnswerPath)) {
			t.Fatalf("entry omitted exact references: %s", output)
		}
	}
	if packets[0].Binding.Owner == packets[1].Binding.Owner {
		t.Fatal("default responsibilities share an owner")
	}
	if after.Revision <= before.Revision {
		t.Fatal("no original registration occurred")
	}
	manifest := ""
	for _, raw := range after.State["entities"].(map[string]any)["teams"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "document_verifier" {
			manifest = row["manifest_ref"].(string)
			break
		}
	}
	if manifest == "" {
		t.Fatal("original team did not retain its manifest reference")
	}
	data, err := os.ReadFile(filepath.Join(root, manifest))
	if err != nil {
		t.Fatal(err)
	}
	if err := team.ValidateBytes(data); err != nil {
		t.Fatal(err)
	}
	original := after.Revision
	s5EntryHook(t, root)
	replay := stageSnapshot(t, root)
	if replay.Revision != original {
		t.Fatalf("entry replay wrote another registration: %d -> %d", original, replay.Revision)
	}
	again, _ := os.ReadFile(filepath.Join(root, manifest))
	if string(again) != string(data) {
		t.Fatal("replay changed frozen manifest bytes or validation time")
	}
	// Explicit same-owner edits remain invalid; automatic preparation cannot
	// manufacture independence or refresh already frozen manifest content.
	var value map[string]any
	json.Unmarshal(data, &value)
	rows := value["assignments"].([]any)
	rows[1].(map[string]any)["agent_id"] = rows[0].(map[string]any)["agent_id"]
	forged, _ := json.Marshal(value)
	if err := team.ValidateBytes(forged); err == nil {
		t.Fatalf("same owner accepted: %v", err)
	}
	// Follow the newly prepared original packet through the actual native
	// Stop route. No Main-authored answer metadata or generic PLAN is added.
	p := packets[0]
	stageJSON(t, root, p.AnswerPath, completedDraft(p, "pass"))
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "actual-native-s5-prepared-reviewer", "cwd": root, "last_assistant_message": filepath.Join(root, p.AnswerPath)})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(payload), &out, &diagnostic); code != 0 {
		t.Fatalf("native Stop rejected prepared original packet: %d %s %s", code, &out, &diagnostic)
	}
	record, found, err := runtime.FindSubmission(stageSnapshot(t, root).State, p.ID)
	if err != nil || !found || record.Owner != p.Binding.Owner {
		t.Fatalf("prepared packet did not reach original review reception: %+v %v %s", record, err, &out)
	}
}

func TestS5PreparationRecoveryRejectsAcceptedSourceDriftWithoutRefreshingSHA(t *testing.T) {
	root := newUnregisteredS5InputFixture(t)
	before := stageSnapshot(t, root)
	source := "docs/dev/tasks/TASK-039-01.md"
	data, err := os.ReadFile(filepath.Join(root, source))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, source), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	output := s5EntryRecovery(t, root, 1)
	after := stageSnapshot(t, root)
	for _, key := range []string{"documents", "evidence", "lifecycle"} {
		if !reflect.DeepEqual(after.State[key], before.State[key]) {
			t.Fatalf("source drift changed %s", key)
		}
	}
	for _, key := range []string{"teams", "agents", "work_packets"} {
		if !reflect.DeepEqual(after.State["entities"].(map[string]any)[key], before.State["entities"].(map[string]any)[key]) {
			t.Fatalf("source drift registered %s", key)
		}
	}
	if !strings.Contains(output, source) {
		t.Fatalf("source error lacks exact recovery target: %s", output)
	}
	packets, err := runtime.S5Packets(root, after.State, "")
	if len(packets) != 0 {
		t.Fatalf("source drift issued reviewers: %v %d", err, len(packets))
	}
	if err := os.WriteFile(filepath.Join(root, source), data, 0600); err != nil {
		t.Fatal(err)
	}
	output = s5EntryRecovery(t, root, 0)
	packets, err = runtime.S5Packets(root, stageSnapshot(t, root).State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("restoring original source did not recover preparation: %v %s", err, output)
	}
}

func TestS5EntryAcceptsProfessionalOnlyManifestWithoutMachineFields(t *testing.T) {
	root := newUnregisteredS5InputFixture(t)
	before := stageSnapshot(t, root)
	life := before.State["lifecycle"].(map[string]any)
	path := fmt.Sprintf(".claude/workgroups/REQ-039/review-e%.0f/manifest.json", life["phase_revision"])
	stageJSON(t, root, path, map[string]any{"assignments": []any{
		map[string]any{"scope": []string{"整批规格链，特别核对无权限拒绝语义"}},
		map[string]any{"scope": []string{"整批任务，特别核对错误路径的真实验证责任"}},
	}})
	output := s5EntryRecovery(t, root, 0)
	packets, err := runtime.S5Packets(root, stageSnapshot(t, root).State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("professional choices without machine fields rejected: %v %s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	var completed map[string]any
	if err := json.Unmarshal(data, &completed); err != nil {
		t.Fatal(err)
	}
	if completed["runtime_id"] == nil || completed["documents"] == nil || completed["validation"] == nil {
		t.Fatalf("system fields not supplied: %s", data)
	}
	if !strings.Contains(string(data), "无权限拒绝语义") || !strings.Contains(string(data), "错误路径的真实验证责任") {
		t.Fatal("preparation overwrote Main professional choices")
	}
}

// Exercise the normal Main save Hook, not just the lower-level issuer. A
// registered manifest is a frozen original source, not another editable answer.
func TestS5RegisteredManifestSaveRejectsChangedAssociationsAndReplaysUnchanged(t *testing.T) {
	root := newUnregisteredS5InputFixture(t)
	s5EntryRecovery(t, root, 0)
	before := stageSnapshot(t, root)
	manifest := ""
	for _, raw := range before.State["entities"].(map[string]any)["teams"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "document_verifier" {
			manifest = row["manifest_ref"].(string)
			if row["manifest_sha256"] == nil {
				t.Fatal("original registration did not pin its manifest")
			}
			break
		}
	}
	original, err := os.ReadFile(filepath.Join(root, manifest))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	saved := func(data []byte) (string, *submission.Receipt) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, manifest), data, 0600); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, manifest)}, "cwd": root})
		var output, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(payload), &output, &diagnostic); code != 0 {
			t.Fatalf("save Hook: %d %s %s", code, &output, &diagnostic)
		}
		var envelope struct {
			Specific struct {
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
			t.Fatalf("invalid Hook output: %v %s", err, &output)
		}
		const prefix = "S5 SUBMISSION RECEIPT: "
		context := envelope.Specific.Context
		if strings.HasPrefix(context, prefix) {
			data := []byte(strings.TrimPrefix(context, prefix))
			if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", data); err != nil {
				t.Fatalf("invalid rejection receipt: %v %s", err, data)
			}
			var receipt submission.Receipt
			if err := json.Unmarshal(data, &receipt); err != nil {
				t.Fatal(err)
			}
			return context, &receipt
		}
		return context, nil
	}
	for _, kind := range []string{"scope", "source", "same-owner"} {
		var changed map[string]any
		if err := json.Unmarshal(original, &changed); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "scope":
			changed["assignments"].([]any)[0].(map[string]any)["scope"] = []string{"偷偷缩小原整批审查范围"}
		case "source":
			changed["documents"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("0", 64)
		case "same-owner":
			rows := changed["assignments"].([]any)
			rows[1].(map[string]any)["agent_id"] = rows[0].(map[string]any)["agent_id"]
		}
		data, _ := json.Marshal(changed)
		_, r := saved(data)
		if r == nil || r.Accepted || r.WorkComplete || !strings.Contains(r.Error, "original registration") {
			t.Fatalf("%s changed manifest was accepted: %+v", kind, r)
		}
		after := stageSnapshot(t, root)
		currentJournal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
		if after.Revision != before.Revision || !bytes.Equal(currentJournal, journal) {
			t.Fatalf("%s changed registration/packet facts", kind)
		}
	}
	context, r := saved(original)
	if r != nil || !strings.Contains(context, "原分工登记与原问卷发行已就绪") || strings.Contains(context, "SUBMISSION RECEIPT") {
		t.Fatalf("registration projected a fictitious packet reception: %s", context)
	}
	packets, err := runtime.S5Packets(root, before.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("original packet facts missing: %d %v", len(packets), err)
	}
	for _, p := range packets {
		if !strings.Contains(context, filepath.Join(root, p.ViewPath)) || !strings.Contains(context, filepath.Join(root, p.AnswerPath)) {
			t.Fatalf("missing exact original references: %s", context)
		}
	}
	after := stageSnapshot(t, root)
	currentJournal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	currentBytes, _ := os.ReadFile(filepath.Join(root, manifest))
	if after.Revision != before.Revision || !bytes.Equal(currentJournal, journal) || !bytes.Equal(currentBytes, original) {
		t.Fatal("unchanged replay re-registered or re-signed original facts")
	}
}

// This fixture deliberately stops at the original register-workgroup command;
// it exercises the interrupted register -> first issue seam, not author UX.
// The normal no-machine-input UX is independently covered by TestS5Entry*.
func TestS5RegisteredManifestCannotChangeBeforeFirstQuestionnaire(t *testing.T) {
	f := newStageAnswerFixtureIssuance(t, true, false)
	before := stageSnapshot(t, f.root)
	manifest := ".claude/workgroups/REQ-039/review/manifest.json"
	original, err := os.ReadFile(filepath.Join(f.root, manifest))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range before.State["entities"].(map[string]any)["teams"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "document_verifier" && row["manifest_sha256"] == nil {
			t.Fatal("actual Register omitted the original manifest pin")
		}
	}
	for _, kind := range []string{"scope", "source"} {
		var changed map[string]any
		json.Unmarshal(original, &changed)
		if kind == "scope" {
			changed["assignments"].([]any)[0].(map[string]any)["scope"] = []string{"不再覆盖整批材料"}
		} else {
			changed["documents"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("0", 64)
		}
		data, _ := json.Marshal(changed)
		if err := os.WriteFile(filepath.Join(f.root, manifest), data, 0600); err != nil {
			t.Fatal(err)
		}
		service, err := submission.New(f.root)
		if err != nil {
			t.Fatal(err)
		}
		packets, err := service.IssueContext(context.Background(), "")
		if err == nil || len(packets) != 0 || !strings.Contains(err.Error(), "original registration") {
			t.Fatalf("%s changed bytes got a first packet: %d %v", kind, len(packets), err)
		}
		if stageSnapshot(t, f.root).Revision != before.Revision {
			t.Fatal("failed first issuance rewrote registration")
		}
	}
	if err := os.WriteFile(filepath.Join(f.root, manifest), original, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueContext(context.Background(), "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("restoring exact registration did not resume original issuance: %d %v", len(packets), err)
	}
}
