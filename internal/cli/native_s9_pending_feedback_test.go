package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestNativeS9PendingPublicPreReturnsExecutableOriginalWriterRecovery(t *testing.T) {
	root, worker, p, packet, previousState, previousJournal, targetState, targetJournal := nativeS9OriginalCandidateBoundary(t)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	markerPath := statePath + ".commit-pending.json"
	marker := nativeS9OriginalPendingMarker(t, previousState, targetState, targetJournal)
	if err := os.WriteFile(statePath, previousState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, previousJournal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, marker, 0600); err != nil {
		t.Fatal(err)
	}
	// The frozen candidate/pending pair is sufficient. Private professional
	// inputs are deliberately unavailable or invalid throughout Pre and recover.
	if err := os.Remove(filepath.Join(worker, packet.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	plan, relative := nativeS9TestAuthorPaths(t, root, p)
	if err := os.Remove(filepath.Join(worker, plan)); err != nil {
		t.Fatal(err)
	}
	flatPath := filepath.Join(worker, relative)
	later := []byte("unrelated later private bytes; never adapt these during pending recovery")
	if err := os.WriteFile(flatPath, later, 0600); err != nil {
		t.Fatal(err)
	}
	request := policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root, ToolName: "Bash"}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"hook", "--root", root, "--event", request.Event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("public pending Pre: %d %s %s", code, &out, &diagnostic)
	}
	var response struct {
		Output struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.Output.Event != request.Event {
		t.Fatalf("missing model-side Hook context: %v %s", err, &out)
	}
	command := regexp.MustCompile(`'([^']+)' runtime workspace integrate --root '([^']+)' --assignment '([^']+)' --agent '([^']+)'`).FindStringSubmatch(response.Output.Context)
	if len(command) != 5 || command[1] != filepath.Join(root, ".claude/bin/loop-harness") || command[2] != root || command[3] != p.Binding.ManifestAssignmentID || command[4] != p.Binding.OwnerAgentID {
		t.Fatalf("same-event recovery did not name the exact original integration successor: %s", response.Output.Context)
	}
	if !strings.Contains(diagnostic.String(), "recovered the original pending Writer") || !strings.Contains(response.Output.Context, "PlanReport receipt is accepted") || !strings.Contains(response.Output.Context, "Worker candidate is recorded") {
		t.Fatalf("same-event continuation lost original recovery/reception facts: %s %s", &diagnostic, response.Output.Context)
	}
	for _, check := range []struct {
		path     string
		expected []byte
	}{{statePath, targetState}, {journalPath, targetJournal}, {flatPath, later}} {
		actual, err := os.ReadFile(check.path)
		if err != nil || !bytes.Equal(actual, check.expected) {
			t.Fatalf("same-event recovery did not preserve the accepted pair/private input %s: %v", check.path, err)
		}
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("same-event Writer recovery did not retire pending: %v", err)
	}
	for _, path := range []string{filepath.Join(worker, packet.AnswerPath), filepath.Join(worker, plan)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("ordinary Pre recreated professional input %s: %v", path, err)
		}
	}
	// Repeating the public observation keeps the accepted pair and the same
	// integration successor; missing or changed private input cannot be re-read
	// as a new answer or replace the already captured candidate.
	out.Reset()
	diagnostic.Reset()
	if code := Run([]string{"hook", "--root", root, "--event", request.Event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("replayed public Pre: %d %s %s", code, &out, &diagnostic)
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil || !strings.Contains(response.Output.Context, command[0]) {
		t.Fatalf("replayed Pre lost the exact integration successor: %v %s", err, &out)
	}
	// The explicit original recovery entry remains an idempotent fallback with
	// its receipt-array JSON contract after automatic recovery has completed.
	out.Reset()
	diagnostic.Reset()
	args := []string{"runtime", "stage-answer", "recover", "--root", command[2]}
	if code := Run(args, strings.NewReader(""), &out, &diagnostic); code != 0 {
		t.Fatalf("actual model-supplied Writer command: %d %s %s", code, &out, &diagnostic)
	}
	var receipts []submission.Receipt
	if err := json.Unmarshal(out.Bytes(), &receipts); err != nil {
		t.Fatalf("recovery changed original JSON array type: %v %s", err, &out)
	}
	if !strings.Contains(diagnostic.String(), "runtime workspace integrate") || !strings.Contains(diagnostic.String(), p.Binding.ManifestAssignmentID) {
		t.Fatalf("explicit recovery did not immediately deliver original successor: %s", &diagnostic)
	}
	for _, check := range []struct {
		path     string
		expected []byte
	}{{statePath, targetState}, {journalPath, targetJournal}, {flatPath, later}} {
		actual, err := os.ReadFile(check.path)
		if err != nil || !bytes.Equal(actual, check.expected) {
			t.Fatalf("original recovery changed accepted pair or private flat %s: %v", check.path, err)
		}
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("original Writer did not retire pending: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worker, packet.AnswerPath)); !os.IsNotExist(err) {
		t.Fatalf("recovery fabricated professional input: %v", err)
	}
}

func TestNativePendingRuntimeFeedbackDoesNotClaimWorkerOrUnrelatedEvents(t *testing.T) {
	root := t.TempDir()
	routingState(t, root, `{"revision":1}`)
	marker := filepath.Join(root, ".claude/loop-state.json.commit-pending.json")
	if err := os.WriteFile(marker, []byte("original pending marker"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, request := range []policy.Input{
		{Event: "PreToolUse", AgentID: "worker", CWD: root},
		{Event: "PostToolUse", AgentID: "Main", CWD: root, ToolName: "Read", Error: "tool failed"},
		{Event: "SubagentStop", AgentID: "Main", CWD: root},
		{Event: "PreToolUse", AgentID: "Main", CWD: t.TempDir()},
	} {
		var out bytes.Buffer
		if nativePendingRuntimeMainContext(root, request, &out) || out.Len() != 0 {
			t.Fatalf("unrelated event/Worker claimed Main pending outlet: %#v %s", request, &out)
		}
	}
}
