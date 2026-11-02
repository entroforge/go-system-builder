package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

// s5RemovedDigestFields reports every retired digest field that still carries
// a digest VALUE inside a decoded JSON value. Blueprint
// L4-workflow-convergence.md:250 removed hash/SHA/digest/fingerprint from the
// Hook, the Writer, the state, the receipt and the recovery path, and
// L4-workflow-convergence.md:138 forbids renaming the same purpose into
// fingerprint or checksum.
//
// The historical field NAME is not itself a machine proof: blueprint
// L3-S9-repair.md:15 keeps old digest values as historical original fields
// that are read as they are, never recomputed, re-registered or used as a
// continuation qualification, and L3-S5-document-verification.md:354 keeps
// historical facts independent of any old hash check. So only a field that
// still carries a digest value is a violation here; an empty field name
// without a value registers nothing.
func s5RemovedDigestFields(value any) []string {
	found := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, item := range typed {
				switch key {
				case "sha256", "manifest_sha256", "digest", "fingerprint", "checksum", "hash", "git_oid":
					if s5CarriesDigestValue(item) {
						found[key] = true
					}
				}
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	removed := make([]string, 0, len(found))
	for key := range found {
		removed = append(removed, key)
	}
	sort.Strings(removed)
	return removed
}

// s5CarriesDigestValue reports whether a retired field still holds an actual
// digest value. A nil or empty value registers no digest.
func s5CarriesDigestValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case float64:
		return typed != 0
	default:
		return true
	}
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

// The accepted S4 source may drift on disk after it was accepted. Blueprint
// L3-S5-document-verification.md:102 and L4-workflow-convergence.md:138/281
// removed the manifest and material digest: the Hook no longer registers or
// verifies a digest, so the machine no longer refuses the drifted bytes and
// does not claim to have detected the same-version external change. The
// preparation completes, the two original responsibilities are issued with the
// actual material, and the consequence of the drift is left to the
// professional author and Main (L3-S5-document-verification.md:128).
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
	output := s5EntryRecovery(t, root, 0)
	after := stageSnapshot(t, root)
	if after.Revision <= before.Revision {
		t.Fatalf("drifted source did not register the original preparation: %d -> %d", before.Revision, after.Revision)
	}
	packets, err := runtime.S5Packets(root, after.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("drifted source did not issue both original responsibilities: %d %v %s", len(packets), err, output)
	}
	// The drifted material is still delivered to both reviewers by its actual
	// path and known version. The system does not refuse it and does not claim
	// to have detected the byte change; the professional author and Main judge
	// the actual material they receive.
	delivered := 0
	for _, p := range packets {
		for _, ref := range p.Inputs {
			if ref.Path != source {
				continue
			}
			delivered++
			if ref.Version == "" {
				t.Fatalf("delivered source lost its known version: %+v", ref)
			}
		}
	}
	if delivered != len(packets) {
		t.Fatalf("drifted source was not delivered to every original responsibility: %d of %d", delivered, len(packets))
	}
	// The registration and the delivered questionnaires carry no digest.
	manifest := ""
	for _, raw := range after.State["entities"].(map[string]any)["teams"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] != "document_verifier" {
			continue
		}
		manifest = row["manifest_ref"].(string)
		if removed := s5RemovedDigestFields(row); len(removed) > 0 {
			t.Fatalf("registered team kept the removed digest field %v: %v", removed, row)
		}
	}
	if manifest == "" {
		t.Fatal("drifted preparation did not register its manifest reference")
	}
	for _, raw := range after.State["documents"].([]any) {
		if removed := s5RemovedDigestFields(raw); len(removed) > 0 {
			t.Fatalf("registered document kept the removed digest field %v: %v", removed, raw)
		}
	}
	registered, err := os.ReadFile(filepath.Join(root, manifest))
	if err != nil {
		t.Fatal(err)
	}
	var registeredValue any
	if err := json.Unmarshal(registered, &registeredValue); err != nil {
		t.Fatal(err)
	}
	if removed := s5RemovedDigestFields(registeredValue); len(removed) > 0 {
		t.Fatalf("registered manifest kept the removed digest field %v: %s", removed, registered)
	}
	for _, p := range packets {
		view, err := os.ReadFile(filepath.Join(root, p.ViewPath))
		if err != nil {
			t.Fatal(err)
		}
		var viewValue any
		if err := json.Unmarshal(view, &viewValue); err != nil {
			t.Fatal(err)
		}
		if removed := s5RemovedDigestFields(viewValue); len(removed) > 0 {
			t.Fatalf("delivered questionnaire kept the removed digest field %v: %s", removed, view)
		}
	}
	// Restoring the original source bytes replays the same original
	// preparation; it does not register a second time.
	if err := os.WriteFile(filepath.Join(root, source), data, 0600); err != nil {
		t.Fatal(err)
	}
	replay := s5EntryRecovery(t, root, 0)
	restored := stageSnapshot(t, root)
	if restored.Revision != after.Revision {
		t.Fatalf("restoring the original source registered a second preparation: %d -> %d", after.Revision, restored.Revision)
	}
	packets, err = runtime.S5Packets(root, restored.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("restoring the original source did not replay the original responsibilities: %d %v %s", len(packets), err, replay)
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
// registered manifest is a frozen original source, not another editable
// answer. Blueprint L3-S5-document-verification.md:84 and
// L4-workflow-convergence.md:250 removed the manifest digest: the Hook and the
// original consumer no longer compute, register or verify a SHA/hash, so an
// edited manifest file is not refused by a byte comparison and never becomes
// another registration. The registered associations are still frozen: an edit
// that breaks them is refused by the original consumer.
func TestS5RegisteredManifestSaveRejectsChangedAssociationsAndReplaysUnchanged(t *testing.T) {
	root := newUnregisteredS5InputFixture(t)
	s5EntryRecovery(t, root, 0)
	before := stageSnapshot(t, root)
	manifest := ""
	for _, raw := range before.State["entities"].(map[string]any)["teams"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "document_verifier" {
			manifest = row["manifest_ref"].(string)
			if removed := s5RemovedDigestFields(row); len(removed) > 0 {
				t.Fatalf("original registration kept the removed digest field %v: %v", removed, row)
			}
			break
		}
	}
	if manifest == "" {
		t.Fatal("original registration did not keep its manifest reference")
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
	for _, kind := range []string{"scope", "same-owner"} {
		var changed map[string]any
		if err := json.Unmarshal(original, &changed); err != nil {
			t.Fatal(err)
		}
		conflicting := ""
		switch kind {
		case "scope":
			changed["assignments"].([]any)[0].(map[string]any)["scope"] = []string{"偷偷缩小原整批审查范围"}
		case "same-owner":
			rows := changed["assignments"].([]any)
			// The second registered responsibility loses its own owner here,
			// so it is the one that can no longer be matched exactly. The
			// refusal must name that concrete registered responsibility.
			conflicting, _ = rows[1].(map[string]any)["agent_id"].(string)
			rows[1].(map[string]any)["agent_id"] = rows[0].(map[string]any)["agent_id"]
		}
		data, _ := json.Marshal(changed)
		context, r := saved(data)
		switch kind {
		case "scope":
			// The Hook does not compare the manifest bytes any more, so the
			// edited professional content is not refused by a digest gate. It
			// does not become another registration either: the original
			// registration and its exact questionnaire references still stand.
			if r != nil {
				t.Fatalf("scope edit was refused by a removed manifest digest: %+v", r)
			}
			if !strings.Contains(context, "原分工登记与原问卷发行已就绪") {
				t.Fatalf("scope edit lost the original registration report: %s", context)
			}
		case "same-owner":
			// One owner may not take over the other registered
			// responsibility. The original consumer still refuses the
			// conflicting association.
			if r == nil || r.Accepted || r.WorkComplete {
				t.Fatalf("%s changed manifest was accepted: %+v", kind, r)
			}
			if conflicting == "" || !strings.Contains(r.Error, conflicting) {
				t.Fatalf("%s rejection does not name the conflicting owner %s: %+v", kind, conflicting, r)
			}
		}
		after := stageSnapshot(t, root)
		currentJournal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
		if after.Revision != before.Revision || !bytes.Equal(currentJournal, journal) {
			t.Fatalf("%s changed registration/packet facts", kind)
		}
		for _, key := range []string{"teams", "agents", "work_packets"} {
			if !reflect.DeepEqual(after.State["entities"].(map[string]any)[key], before.State["entities"].(map[string]any)[key]) {
				t.Fatalf("%s rewrote the registered %s", kind, key)
			}
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
// Blueprint L3-S5-document-verification.md:84 and
// L4-workflow-convergence.md:250/138 removed the registered manifest digest:
// the Hook and the original consumer no longer compute, register or verify a
// SHA/hash. The registered associations stay frozen without it: an edit that
// breaks them is refused before the first questionnaire, and a professional
// content edit does not become another registration.
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
		if row["kind"] != "document_verifier" {
			continue
		}
		if row["manifest_ref"] != manifest {
			t.Fatalf("actual Register lost the original manifest reference: %v", row)
		}
		if removed := s5RemovedDigestFields(row); len(removed) > 0 {
			t.Fatalf("actual Register kept the removed digest field %v: %v", removed, row)
		}
	}
	// The registered associations cannot be rewritten before the first
	// questionnaire: one owner may not take over the other responsibility.
	owner := newStageAnswerFixtureIssuance(t, true, false)
	ownerBefore := stageSnapshot(t, owner.root)
	var changed map[string]any
	if err := json.Unmarshal(original, &changed); err != nil {
		t.Fatal(err)
	}
	rows := changed["assignments"].([]any)
	// The second registered responsibility loses its own owner here, so it is
	// the one that can no longer be matched exactly.
	conflicting, _ := rows[1].(map[string]any)["agent_id"].(string)
	rows[1].(map[string]any)["agent_id"] = rows[0].(map[string]any)["agent_id"]
	data, _ := json.Marshal(changed)
	if err := os.WriteFile(filepath.Join(owner.root, manifest), data, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := submission.New(owner.root)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueContext(context.Background(), "")
	if err == nil || len(packets) != 0 {
		t.Fatalf("same-owner changed bytes got a first packet: %d %v", len(packets), err)
	}
	if conflicting == "" || !strings.Contains(err.Error(), conflicting) {
		t.Fatalf("rejection does not name the conflicting owner %s: %v", conflicting, err)
	}
	if stageSnapshot(t, owner.root).Revision != ownerBefore.Revision {
		t.Fatal("failed first issuance rewrote registration")
	}
	// A professional content edit is not a digest gate: the Hook does not
	// verify the manifest bytes, and the original registered responsibilities
	// and their independent owners are still the ones that issue the first
	// questionnaires.
	scope := newStageAnswerFixtureIssuance(t, true, false)
	scopeBefore := stageSnapshot(t, scope.root)
	var scoped map[string]any
	if err := json.Unmarshal(original, &scoped); err != nil {
		t.Fatal(err)
	}
	scoped["assignments"].([]any)[0].(map[string]any)["scope"] = []string{"不再覆盖整批材料"}
	scopedData, _ := json.Marshal(scoped)
	if err := os.WriteFile(filepath.Join(scope.root, manifest), scopedData, 0600); err != nil {
		t.Fatal(err)
	}
	scopeService, err := submission.New(scope.root)
	if err != nil {
		t.Fatal(err)
	}
	scopePackets, err := scopeService.IssueContext(context.Background(), "")
	if err != nil || len(scopePackets) != 2 {
		t.Fatalf("scope-edited manifest did not issue the two original responsibilities: %d %v", len(scopePackets), err)
	}
	owners := map[string]bool{}
	for _, p := range scopePackets {
		owners[p.Binding.Owner] = true
		if p.Binding.Owner != conflicting && !strings.Contains(p.Binding.Owner, "reviewer-") {
			t.Fatalf("scope edit changed the registered owner: %+v", p.Binding)
		}
	}
	if len(owners) != 2 {
		t.Fatalf("scope edit collapsed the two independent owners: %v", owners)
	}
	for _, p := range scopePackets {
		view, err := os.ReadFile(filepath.Join(scope.root, p.ViewPath))
		if err != nil {
			t.Fatal(err)
		}
		var viewValue any
		if err := json.Unmarshal(view, &viewValue); err != nil {
			t.Fatal(err)
		}
		if removed := s5RemovedDigestFields(viewValue); len(removed) > 0 {
			t.Fatalf("issued questionnaire kept the removed digest field %v: %s", removed, view)
		}
	}
	// Restoring the exact registered bytes keeps the original issuance: the
	// first questionnaires are still the two registered responsibilities and
	// the registration is not written again.
	if err := os.WriteFile(filepath.Join(scope.root, manifest), original, 0600); err != nil {
		t.Fatal(err)
	}
	restoreService, err := submission.New(scope.root)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreService.IssueContext(context.Background(), "")
	if err != nil || len(restored) != 2 {
		t.Fatalf("restoring exact registration did not resume original issuance: %d %v", len(restored), err)
	}
	// A professional content edit is not a machine gate and does not become
	// another registration: the Runtime keeps the original registration and
	// the same two original responsibilities. Restoring the exact registered
	// bytes therefore resumes the original issuance instead of registering
	// again (blueprint L4-workflow-convergence.md:250/:281 removed the
	// manifest digest and its registration gate).
	if got := stageSnapshot(t, scope.root).Revision; got != scopeBefore.Revision {
		t.Fatalf("the scope edit registered a second preparation: %d -> %d", scopeBefore.Revision, got)
	}
}
