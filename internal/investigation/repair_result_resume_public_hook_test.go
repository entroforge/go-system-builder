package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Mechanical platform inputs exercise the public boundary, not a real-model S9 pass.
func TestS9ResumePublicHookRestoresOnlySuccessorAnswer(t *testing.T) {
	fixture, owner, predecessor, execution, workerRoot, _, _ := prepareWorkingS9RepairResultPacket(t)
	blocked := encodeS9NonpassRepairResultAnswer(t, predecessor, "blocked")
	if err := os.WriteFile(filepath.Join(workerRoot, filepath.FromSlash(predecessor.AnswerPath)), blocked, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, _ := runS9RepairResultPostToolUseWithRaw(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, predecessor.AnswerPath)
	if !accepted.Accepted || accepted.WorkComplete {
		t.Fatalf("original blocker not accepted: %+v", accepted)
	}
	path, err := repair.RepairResultResolutionAuthoringPath(predecessor)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(fixture.root, filepath.FromSlash(path))
	var draft map[string]any
	if err := json.Unmarshal(mustRead(t, abs), &draft); err != nil {
		t.Fatal(err)
	}
	draft["s9_resolution"].(map[string]any)["rationale"] = "Dependency restored; continue the original repair."
	draft["body"] = "Main reviewed the recovered dependency and chose to retry the unfinished work."
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, data, 0600); err != nil {
		t.Fatal(err)
	}
	resolved, _ := runS9RepairResultHookPayloadWithRaw(t, fixture.root, s9RepairResultHookPayload("", fixture.root, "main-resume-public-session", abs))
	if !resolved.Accepted || resolved.WorkComplete {
		t.Fatalf("Main resolution not accepted: %+v", resolved)
	}
	before := currentS9Snapshot(t, fixture)
	if pending, err := runtime.RepairResultResumePending(fixture.root, before.State, owner.agentID, predecessor.Binding.Sources.RepairAssignmentID); err != nil || !pending {
		t.Fatalf("resolved predecessor did not wait for Main: pending=%t err=%v", pending, err)
	}
	listenerState, listenerJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	listenerInput, err := json.Marshal(map[string]any{
		"hook_event_name": "SessionStart", "cwd": fixture.root, "session_id": "main-resume-listener-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	var listenerOut, listenerErr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", fixture.root}, bytes.NewReader(listenerInput), &listenerOut, &listenerErr); code != 0 {
		t.Fatalf("original Main SessionStart listener failed: code=%d stdout=%s stderr=%s", code, listenerOut.String(), listenerErr.String())
	}
	var listenerEnvelope struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(listenerOut.Bytes(), &listenerEnvelope); err != nil {
		t.Fatalf("decode Main Resume listener context: %v; stdout=%s", err, listenerOut.String())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, listenerState, listenerJournal, "read-only Main SessionStart before successor preparation")
	for _, required := range []string{"runtime workspace prepare", fixture.root, owner.manifestAssignmentID, owner.agentID} {
		if !strings.Contains(listenerEnvelope.Output.Context, required) {
			t.Fatalf("read-only Main SessionStart omitted preparation fact %q: %s", required, listenerEnvelope.Output.Context)
		}
	}
	prepareInput, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "cwd": fixture.root, "session_id": "main-resume-listener-session",
		"tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(fixture.root, filepath.FromSlash(predecessor.Binding.Sources.PlanRef))},
	})
	if err != nil {
		t.Fatal(err)
	}
	listenerOut.Reset()
	listenerErr.Reset()
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", fixture.root}, bytes.NewReader(prepareInput), &listenerOut, &listenerErr); code != 0 {
		t.Fatalf("read-only Main PreToolUse failed: code=%d stdout=%s stderr=%s", code, listenerOut.String(), listenerErr.String())
	}
	if err := json.Unmarshal(listenerOut.Bytes(), &listenerEnvelope); err != nil {
		t.Fatalf("decode Main Resume preparation context: %v; stdout=%s", err, listenerOut.String())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, listenerState, listenerJournal, "read-only Main PreToolUse before successor preparation")
	preparedBytes := runS9CommandOutput(t, fixture.root, "runtime", "workspace", "prepare", "--root", fixture.root,
		"--assignment", owner.manifestAssignmentID, "--agent", owner.agentID, "--check-location", "main")
	current := currentS9Snapshot(t, fixture)
	packets, err := runtime.CurrentRepairResultPackets(fixture.root, current.State, owner.agentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("no unique successor: packets=%v err=%v", packets, err)
	}
	successor := packets[0]
	if successor.ID == predecessor.ID || successor.AnswerPath == predecessor.AnswerPath || successor.Binding.Resume == nil || successor.Binding.Resume.PredecessorPacketID != predecessor.ID {
		t.Fatalf("successor reused the retired identity: %+v", successor)
	}
	sourceRoot, successorExecution, err := submission.RepairResultRegisteredSource(fixture.root, current.State, successor)
	if err != nil || filepath.Clean(sourceRoot) != filepath.Clean(workerRoot) || successorExecution.AssignmentID != execution.AssignmentID || successorExecution.Path != execution.Path || successorExecution.Generation != execution.Generation {
		t.Fatalf("Resume successor lost its original registered Worker source: root=%s execution=%+v err=%v", sourceRoot, successorExecution, err)
	}
	wantView := filepath.Join(sourceRoot, filepath.FromSlash(successor.ViewPath))
	wantAnswer := filepath.Join(sourceRoot, filepath.FromSlash(successor.AnswerPath))
	if !bytes.Contains(preparedBytes, []byte(successor.ViewPath)) || !bytes.Contains(preparedBytes, []byte(successor.AnswerPath)) {
		t.Fatalf("normal Main prepare omitted the actual successor paths: want view=%s answer=%s output=%s", wantView, wantAnswer, preparedBytes)
	}
	if _, err := os.Stat(wantView); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wantAnswer); err != nil {
		t.Fatal(err)
	}
	if pending, err := runtime.RepairResultResumePending(fixture.root, current.State, owner.agentID, successor.Binding.Sources.RepairAssignmentID); err != nil || pending {
		t.Fatalf("verified successor still waits for issuance: pending=%t err=%v", pending, err)
	}
	worker, err := hookctx.FromSnapshot(fixture.root, owner.agentID, current)
	if err != nil || worker == nil || worker.PolicyContext.Agent == nil {
		t.Fatalf("Worker projection missing: %v", err)
	}
	if worker.PolicyContext.Agent.RepairResultResumePending || len(worker.PolicyContext.RepairResultAnswerPaths) != 1 || worker.PolicyContext.RepairResultAnswerPaths[0] != successor.AnswerPath {
		t.Fatalf("only new answer should be writable: %+v", worker.PolicyContext)
	}
	if mayEnd, err := repair.RepairResultBlockerExecutionMayEnd(fixture.root, current.State, predecessor.ID, owner.agentID); err != nil || mayEnd {
		t.Fatalf("historical blocker revived Stop: end=%t err=%v", mayEnd, err)
	}
	if decision, handled := hook.StopIdleDecision(fixture.root, policy.Input{Event: "SubagentStop", AgentID: owner.agentID, CWD: workerRoot, SessionID: execution.PlatformSessionID}); !handled || decision.Decision != "deny" {
		t.Fatalf("successor ended before new delivery: handled=%t decision=%+v", handled, decision)
	}
	stateBytes, journalBytes := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	listenerOut.Reset()
	listenerErr.Reset()
	if code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", fixture.root}, bytes.NewReader(listenerInput), &listenerOut, &listenerErr); code != 0 {
		t.Fatalf("exact Main SessionStart successor replay failed: code=%d stderr=%s stdout=%s", code, listenerErr.String(), listenerOut.String())
	}
	if err := json.Unmarshal(listenerOut.Bytes(), &listenerEnvelope); err != nil || !strings.Contains(listenerEnvelope.Output.Context, wantAnswer) {
		t.Fatalf("exact Main listener replay lost the current successor path: err=%v stdout=%s", err, listenerOut.String())
	}
	assertS9RuntimeBytesUnchanged(t, fixture, stateBytes, journalBytes, "successor listener exact replay")
	old, _ := runS9RepairResultPostToolUseWithRaw(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, predecessor.AnswerPath)
	if old.WorkComplete {
		t.Fatalf("old answer completed successor: %+v", old)
	}
	assertS9RuntimeBytesUnchanged(t, fixture, stateBytes, journalBytes, "retired answer save")
	product := []byte("package api\n\nfunc Payload() string { return \"ready\" }\n")
	productPath := filepath.Join(workerRoot, "internal/api/payload.go")
	if err := os.MkdirAll(filepath.Dir(productPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(productPath, product, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runS9FixtureCommand(workerRoot, "sh", "-c", "test -f internal/api/payload.go"); err != nil {
		t.Fatalf("actual successor self-check: %v", err)
	}
	for _, args := range [][]string{{"add", "--", "internal/api/payload.go"}, {"commit", "-m", "Repair payload after recovered dependency"}} {
		if _, err := workspace.Git(context.Background(), workerRoot, args...); err != nil {
			t.Fatalf("ordinary successor Git %v: %v", args, err)
		}
	}
	writeS9RepairResultAnswer(t, workerRoot, successor, product)
	delivered, _ := runS9RepairResultPostToolUseWithRaw(t, fixture.root, owner.agentID, workerRoot, execution.PlatformSessionID, successor.AnswerPath)
	if !delivered.Accepted || !delivered.WorkComplete || delivered.PacketID != successor.ID || !delivered.ConsumerPending {
		t.Fatalf("actual successor candidate did not reach Main pending: %+v", delivered)
	}
	if done, err := repair.RepairResultWorkComplete(fixture.root, currentS9Snapshot(t, fixture).State, predecessor.ID, owner.agentID); err != nil || done {
		t.Fatalf("old predecessor completed the successor responsibility: done=%t err=%v", done, err)
	}

}
