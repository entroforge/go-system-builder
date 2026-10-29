package investigation_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// Include directories and files so an observation cannot silently write a
// diagnostic, cache, or other artifact while leaving Runtime unchanged.
func s8ObservationTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			tree[rel] = info.Mode().String()
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			tree[rel] = "symlink:" + target
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[rel] = fmt.Sprintf("%s:%x", info.Mode(), sha256.Sum256(data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestNativeS8StopReceivesOriginalCaseAnswerWithoutGenericPlanOrActorAlias(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	before := currentS8Snapshot(t, fixture)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, before.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original issue: %d %v", len(packets), err)
	}
	p := packets[0]
	path := writeS8ProfessionalAnswer(t, fixture.root, p, "Native Investigator observed the source-boundary discrepancy without modifying product files.")
	query := func(flag, ref, agent string) (submission.Receipt, int) {
		t.Helper()
		beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"runtime", "stage-answer", "receipt", "--root", fixture.root, "--agent", agent, flag, ref}, nil, &out, &diagnostic)
		var receipt submission.Receipt
		if code == 0 {
			if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
				t.Fatalf("public S8 receipt wire: %v %s %s", err, &out, &diagnostic)
			}
		}
		if !bytes.Equal(beforeState, mustRead(t, fixture.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, fixture.journalPath)) {
			t.Fatal("S8 receipt query mutated Runtime/journal")
		}
		return receipt, code
	}
	if receipt, code := query("--packet", p.ID, owner.ownerID); code != 0 || receipt.Accepted || receipt.WorkComplete {
		t.Fatalf("an authored but unreceived answer became completion: %+v code=%d", receipt, code)
	}
	var boardOut, boardErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "investigation", "status", "--root", fixture.root, "--case-id", p.Binding.CaseID}, nil, &boardOut, &boardErr); code != 0 {
		t.Fatalf("native S8 status: %d %s", code, &boardErr)
	}
	var board struct {
		Board struct {
			NextAction string `json:"next_action"`
		} `json:"board"`
	}
	if err := json.Unmarshal(boardOut.Bytes(), &board); err != nil || !strings.Contains(board.Board.NextAction, "SubagentStop") || strings.Contains(board.Board.NextAction, "PostToolUse") {
		t.Fatalf("status must guide the actual native receiver: %v %s", err, &boardOut)
	}
	active := false
	call := func(event, actor, ref string) (int, string, string) {
		body, _ := json.Marshal(map[string]any{"hook_event_name": event, "stop_hook_active": active, "agent_id": actor, "agent_type": "investigator", "session_id": "actual-native-parent", "cwd": fixture.root, "last_assistant_message": ref, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(fixture.root, path)}})
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", fixture.root, "--event", event}, bytes.NewReader(body), &out, &diagnostic)
		return code, out.String(), diagnostic.String()
	}
	before = currentS8Snapshot(t, fixture)
	for _, event := range []string{"SubagentStart", "PreToolUse", "PostToolUse", "TeammateIdle"} {
		if code, out, diagnostic := call(event, "opaque-native-investigator", ""); code != 0 || event != "SubagentStart" && out != "" {
			t.Fatalf("native %s: %d %s %s", event, code, out, diagnostic)
		}
		if currentS8Snapshot(t, fixture).Revision != before.Revision {
			t.Fatalf("native %s changed Runtime", event)
		}
	}
	if code, _, diagnostic := call("PostToolUse", "", ""); code != 0 || currentS8Snapshot(t, fixture).Revision != before.Revision {
		t.Fatalf("Main received S8 answer before native Stop: %d %s", code, diagnostic)
	}
	answerPath := filepath.Join(fixture.root, path)
	observeMain := func(response map[string]any) string {
		t.Helper()
		event := map[string]any{"hook_event_name": "PostToolUse", "cwd": fixture.root, "tool_name": "Agent", "tool_input": map[string]any{"prompt": filepath.Join(fixture.root, p.ViewPath)}}
		if response != nil {
			event["tool_response"] = response
		}
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		before := s8ObservationTree(t, fixture.root)
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("Main execution observation: %d %s", code, &diagnostic)
		}
		if !reflect.DeepEqual(before, s8ObservationTree(t, fixture.root)) {
			t.Fatal("Main execution observation changed project files")
		}
		var message struct {
			Output struct {
				Event string `json:"hookEventName"`
				Text  string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(out.Bytes(), &message); err != nil || message.Output.Event != "PostToolUse" {
			t.Fatalf("Main execution observation did not return Hook context: %v %s", err, &out)
		}
		return message.Output.Text
	}
	valid, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatal(err)
	}
	var unfinished map[string]any
	if err := json.Unmarshal(valid, &unfinished); err != nil {
		t.Fatal(err)
	}
	answer := unfinished["answer"].(map[string]any)
	answer["result"] = "inconclusive"
	answer["observed"] = "The discriminator could not obtain its required source; Main must restore the missing evidence."
	answer["evidence_refs"] = []any{}
	answer["source_boundary_refs"] = []any{}
	unfinishedBytes, _ := json.Marshal(unfinished)
	beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	for _, scenario := range []struct {
		name string
		data []byte
	}{
		{"malformed-answer", []byte("{}")},
		{"honest-missing-evidence", unfinishedBytes},
		{"missing-answer", nil},
	} {
		if scenario.data == nil {
			if err := os.Remove(answerPath); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(answerPath, scenario.data, 0600); err != nil {
			t.Fatal(err)
		}
		for _, repeated := range []bool{false, true} {
			active = repeated
			if code, _, diagnostic := call("SubagentStop", "opaque-native-investigator", filepath.Join(fixture.root, p.ViewPath)); code != 0 || !strings.Contains(diagnostic, "Main owns recovery") || !strings.Contains(diagnostic, owner.ownerID) || !strings.Contains(diagnostic, answerPath) {
				t.Fatalf("%s repeated=%t did not return the original unfinished responsibility: %d %s", scenario.name, repeated, code, diagnostic)
			}
			if !bytes.Equal(beforeState, mustRead(t, fixture.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, fixture.journalPath)) {
				t.Fatalf("%s incomplete return changed Runtime or journal", scenario.name)
			}
			if receipt, code := query("--packet", p.ID, owner.ownerID); code != 0 || receipt.Accepted || receipt.WorkComplete {
				t.Fatalf("%s incomplete return fabricated acceptance: %+v code=%d", scenario.name, receipt, code)
			}
			prior := submission.PlanningDiagnostic(fixture.root, before.State, "S8 native Stop "+p.ID)
			reason := strings.SplitN(prior, "answer "+answerPath+": ", 2)
			if !strings.Contains(prior, "owner "+owner.ownerID) || len(reason) != 2 || strings.TrimSpace(reason[1]) == "" {
				t.Fatalf("%s original Stop did not retain its owner, answer and reason: %s", scenario.name, prior)
			}
			// Each event independently projects the same original Stop and
			// receipt; a missing tool_response never supplies completed status.
			for _, observation := range []struct {
				name     string
				response map[string]any
				want     []string
				forbid   []string
			}{
				{"unknown", nil, []string{"Native execution observation for ", "Platform execution is unknown here", "Do not infer a running or returned Worker, a failed Stop, or a need to re-dispatch"}, []string{"Native return for ", "续跑原 Investigator", "不能把已结束但未接收的责任当作仍在后台运行"}},
				{"statusless prose", map[string]any{"content": []any{map[string]any{"type": "text", "text": "Finished."}}}, []string{"Native execution observation for ", "Platform execution is unknown here", "Do not infer a running or returned Worker, a failed Stop, or a need to re-dispatch"}, []string{"Native return for ", "续跑原 Investigator", "不能把已结束但未接收的责任当作仍在后台运行"}},
				{"completed", map[string]any{"status": "completed", "content": []any{map[string]any{"type": "text", "text": "Finished."}}}, []string{"Native return for ", "具体字段/来源诊断", "续跑原 Investigator", "不能把已结束但未接收的责任当作仍在后台运行"}, []string{"Platform execution is unknown here"}},
			} {
				text := observeMain(observation.response)
				for _, want := range append(observation.want, prior, p.ID, owner.ownerID, answerPath, `"accepted":false`, `"work_complete":false`, "HypothesisResult has not been accepted") {
					if !strings.Contains(text, want) {
						t.Fatalf("%s repeated=%t %s lost %q: %s", scenario.name, repeated, observation.name, want, text)
					}
				}
				for _, bad := range append(observation.forbid, `"accepted":true`, `"work_complete":true`) {
					if strings.Contains(text, bad) {
						t.Fatalf("%s repeated=%t %s inferred %q: %s", scenario.name, repeated, observation.name, bad, text)
					}
				}
				if !bytes.Equal(beforeState, mustRead(t, fixture.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, fixture.journalPath)) || submission.PlanningDiagnostic(fixture.root, before.State, "S8 native Stop "+p.ID) != prior {
					t.Fatalf("%s %s changed Runtime, journal or original Stop diagnostic", scenario.name, observation.name)
				}
				if receipt, code := query("--packet", p.ID, owner.ownerID); code != 0 || receipt.Accepted || receipt.WorkComplete {
					t.Fatalf("%s %s fabricated reception: %+v code=%d", scenario.name, observation.name, receipt, code)
				}
			}
		}
		if scenario.data != nil && !bytes.Equal(scenario.data, mustRead(t, answerPath)) {
			t.Fatalf("%s return rewrote the author's incomplete answer", scenario.name)
		}
	}
	if err := os.WriteFile(answerPath, valid, 0600); err != nil {
		t.Fatal(err)
	}
	active = false
	// Source preparation can fail before current packets are selectable.
	// The original registered reference still identifies the unfinished
	// responsibility, but can never authorize reception from a damaged Case.
	currentCasePath := filepath.Join(fixture.root, p.Binding.CaseRef)
	currentCaseBytes := mustRead(t, currentCasePath)
	if err := os.WriteFile(currentCasePath, append(append([]byte(nil), currentCaseBytes...), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := call("SubagentStop", "opaque-native-investigator", filepath.Join(fixture.root, p.ViewPath)); code != 0 || !strings.Contains(diagnostic, "Main owns recovery") || !strings.Contains(diagnostic, owner.ownerID) || !strings.Contains(diagnostic, answerPath) {
		t.Fatalf("Case preparation failure lost the original handoff: %d %s", code, diagnostic)
	}
	if !bytes.Equal(beforeState, mustRead(t, fixture.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("Case preparation failure fabricated reception")
	}
	if err := os.WriteFile(currentCasePath, currentCaseBytes, 0600); err != nil {
		t.Fatal(err)
	}

	if code, out, diagnostic := call("SubagentStop", "opaque-native-investigator", filepath.Join(fixture.root, p.ViewPath)); code != 0 || out != "" || !strings.Contains(diagnostic, "original consumer") {
		t.Fatalf("native Stop: %d %s %s", code, out, diagnostic)
	}
	if text := observeMain(nil); !strings.Contains(text, "Platform execution is unknown here") || !strings.Contains(text, `"accepted":true`) || !strings.Contains(text, p.ID) || strings.Contains(text, "Native return for ") {
		t.Fatalf("unknown execution lost independently accepted Case reception: %s", text)
	}
	if text := observeMain(map[string]any{"status": "completed"}); !strings.Contains(text, "Native return for ") || !strings.Contains(text, `"accepted":true`) || !strings.Contains(text, p.ID) {
		t.Fatalf("completed execution missed original Case reception: %s", text)
	}
	accepted := currentS8Snapshot(t, fixture)
	if _, found, err := investigation.IssuedHypothesisResultReceipt(fixture.root, accepted.State, p.ID, owner.ownerID); err != nil || !found {
		t.Fatalf("original Case receipt missing: %t %v", found, err)
	}
	if code, _, diagnostic := call("SubagentStop", "fresh-native-actor-after-resume", filepath.Join(fixture.root, p.AnswerPath)); code != 0 {
		t.Fatalf("native replay: %d %s", code, diagnostic)
	}
	if currentS8Snapshot(t, fixture).Revision != accepted.Revision {
		t.Fatal("same answer after native resume changed Runtime")
	}
	canonical, found, err := investigation.IssuedHypothesisResultReceipt(fixture.root, accepted.State, p.ID, owner.ownerID)
	if err != nil || !found {
		t.Fatalf("original accepted receipt: %v", err)
	}
	casePath := filepath.Join(fixture.root, canonical.SuccessorCaseRef)
	caseBytes := mustRead(t, casePath)
	acceptedState, acceptedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	writeS8ProfessionalAnswer(t, fixture.root, p, "Changed submitted content is a conflicting answer, not a receipt query.")
	if _, err := submitS8Original(fixture, accepted.Revision, p, path, owner.ownerID); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("actual changed submission bypassed the original digest conflict: %v", err)
	}
	if err := os.WriteFile(answerPath, []byte("broken private answer"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"corrupt-answer", "removed-author-files"} {
		if mode == "removed-author-files" {
			for _, file := range []string{answerPath, filepath.Join(fixture.root, p.ViewPath)} {
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			}
		}
		for flag, ref := range map[string]string{"--packet": p.ID, "--path": answerPath} {
			receipt, code := query(flag, ref, owner.ownerID)
			if code != 0 || !receipt.Accepted || !receipt.WorkComplete || receipt.PacketID != p.ID || len(receipt.DomainRefs) != 1 || receipt.DomainRefs[0] != canonical.SuccessorCaseRef {
				t.Fatalf("%s lost original accepted result with %s: %+v code=%d", mode, flag, receipt, code)
			}
		}
		if text := observeMain(nil); !strings.Contains(text, "Platform execution is unknown here") || !strings.Contains(text, `"accepted":true`) || !strings.Contains(text, canonical.SuccessorCaseRef) {
			t.Fatalf("%s Main asked for private answer instead of original receipt: %s", mode, text)
		}
	}
	if receipt, code := query("--packet", p.ID, "different-investigator"); code == 0 && (receipt.Accepted || receipt.WorkComplete) {
		t.Fatalf("receipt lent completion to a different owner: %+v", receipt)
	}
	if !bytes.Equal(caseBytes, mustRead(t, casePath)) || !bytes.Equal(acceptedState, mustRead(t, fixture.statePath)) || !bytes.Equal(acceptedJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("receipt recovery or changed-answer conflict rewrote original Case/Runtime/journal")
	}
	if err := os.WriteFile(casePath, append(append([]byte(nil), caseBytes...), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if receipt, code := query("--packet", p.ID, owner.ownerID); code == 0 && receipt.Accepted {
		t.Fatalf("author-file independence bypassed damaged canonical Case proof: %+v", receipt)
	}
}
