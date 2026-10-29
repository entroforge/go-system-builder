package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func planningBoundaryEvent(t *testing.T, root, event, tool, path string, failed bool) (string, string) {
	t.Helper()
	input := map[string]any{"command": "opaque host command"}
	if path != "" {
		input = map[string]any{"file_path": filepath.Join(root, path)}
	}
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "session_id": "planning-boundary", "tool_use_id": "tool-boundary", "cwd": root, "tool_name": tool, "tool_input": input, "tool_response": map[string]any{"is_error": failed}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("%s: %d %s %s", event, code, &out, &diagnostic)
	}
	if out.Len() > 0 {
		decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("invalid Hook response: %v %s", err, &out)
		}
		if err := decoder.Decode(&response); err != io.EOF {
			t.Fatalf("more than one Hook response: %v %s", err, &out)
		}
	}
	return out.String(), diagnostic.String()
}

func TestS2ReadAndUnknownEventsNeverReceiveExistingAnswer(t *testing.T) {
	for _, test := range []struct {
		name, tool string
		before     bool
	}{
		{"custom_read", "mcp__filesystem__read_file", true},
		{"script_no_change", "Bash", true},
		{"missing_before_event", "mcp__filesystem__write_file", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, packet := newDesignFixture(t, "none")
			stageJSON(t, root, packet.AnswerPath, designDraft(packet))
			before := stageSnapshot(t, root).Revision
			if test.before {
				planningBoundaryEvent(t, root, "PreToolUse", test.tool, packet.AnswerPath, false)
			}
			out, _ := planningBoundaryEvent(t, root, "PostToolUse", test.tool, packet.AnswerPath, false)
			after := stageSnapshot(t, root)
			if _, found, err := runtime.FindSubmission(after.State, packet.ID); err != nil || found || before != after.Revision {
				t.Fatalf("observation received answer or changed Runtime: %t %v %s", found, err, out)
			}
			if strings.Contains(out, "SUBMISSION RECEIPT") || strings.Contains(out, "permissionDecision") {
				t.Fatalf("read changed submission or tool permissions: %s", out)
			}
			if !test.before && (!strings.Contains(out, "stage-answer consume") || !strings.Contains(out, packet.AnswerPath)) {
				t.Fatalf("missing event recovery has no exact original receiver: %s", out)
			}
		})
	}
}

func TestS2FailedAndUnchangedToolsDoNotRestoreMaterials(t *testing.T) {
	for _, test := range []struct {
		name, tool, event string
		failed            bool
	}{
		{"failed_write", "Write", "PostToolUse", true},
		{"failure_event", "Write", "PostToolUseFailure", true},
		{"unchanged_script", "Bash", "PostToolUse", false},
		{"unchanged_custom_read", "mcp__filesystem__read_file", "PostToolUse", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, packet := newDesignFixture(t, "none")
			if err := os.Remove(filepath.Join(root, packet.AnswerPath)); err != nil {
				t.Fatal(err)
			}
			before := stageSnapshot(t, root).Revision
			planningBoundaryEvent(t, root, "PreToolUse", test.tool, "docs/architecture/ARCHITECTURE-039.md", false)
			planningBoundaryEvent(t, root, test.event, test.tool, "docs/architecture/ARCHITECTURE-039.md", test.failed)
			if _, err := os.Stat(filepath.Join(root, packet.AnswerPath)); !os.IsNotExist(err) {
				t.Fatalf("failed/unchanged event restored missing answer: %v", err)
			}
			if after := stageSnapshot(t, root).Revision; after != before {
				t.Fatalf("failed/unchanged event changed Runtime: %d -> %d", before, after)
			}
		})
	}
}

func TestS3RepairDeliversNextPathsInSamePostResponse(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	planningBoundaryEvent(t, root, "PostToolUse", "Write", packet.AnswerPath, false)
	// Material bytes already exist before git commit; the event must observe
	// the bound Git ref change rather than accidentally rely on a later read.
	writeContractSource(t, root, "docs/design/README.md", "# Design sources\n")
	planningBoundaryEvent(t, root, "PreToolUse", "Bash", "", false)
	fixtures.CommitCurrentFixture(t, root)
	out, diagnostic := planningBoundaryEvent(t, root, "PostToolUse", "Bash", "", false)
	var response struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatalf("no Hook response: %v stdout=%s stderr=%s", err, out, diagnostic)
	}
	if !strings.Contains(response.Output.Context, "/S3/") || !strings.Contains(response.Output.Context, "answer.json") {
		t.Fatalf("new paths not delivered to model: stdout=%s stderr=%s", out, diagnostic)
	}
	if strings.Contains(diagnostic, "S3 current") {
		t.Fatalf("model guidance leaked into diagnostic channel: %s", diagnostic)
	}
}

func TestS3PlanningObserverRequiresActualAnswerChange(t *testing.T) {
	root, packet := newContractAnswerFixture(t)
	save := func(conclusion string) {
		stageJSON(t, root, packet.AnswerPath, map[string]any{"answer": map[string]any{"conclusion": conclusion}})
	}
	requireReceipt := func(out string) {
		t.Helper()
		if !strings.Contains(out, "S3 SUBMISSION RECEIPT") {
			t.Fatalf("original consumer not invoked: %s", out)
		}
	}
	save("pass")
	out, _ := planningBoundaryEvent(t, root, "PostToolUse", "Edit", packet.AnswerPath, false)
	requireReceipt(out) // Explicit native save can report incomplete work.
	for _, tool := range []string{"Bash", "mcp__filesystem__read_file"} {
		planningBoundaryEvent(t, root, "PreToolUse", tool, packet.AnswerPath, false)
		out, _ := planningBoundaryEvent(t, root, "PostToolUse", tool, packet.AnswerPath, false)
		if strings.Contains(out, "SUBMISSION RECEIPT") {
			t.Fatalf("unchanged tool resubmitted answer: %s", out)
		}
	}
	// A failed save does not become a later unrelated successful tool's save.
	planningBoundaryEvent(t, root, "PreToolUse", "Bash", "", false)
	save("fail")
	planningBoundaryEvent(t, root, "PostToolUse", "Bash", "", true)
	out, _ = planningBoundaryEvent(t, root, "PostToolUse", "Bash", "", false)
	if strings.Contains(out, "SUBMISSION RECEIPT") {
		t.Fatalf("failed callback left reusable mutation proof: %s", out)
	}
	planningBoundaryEvent(t, root, "PreToolUse", "mcp__sandbox__execute", "", false)
	save("blocked")
	out, _ = planningBoundaryEvent(t, root, "PostToolUse", "mcp__sandbox__execute", "", false)
	requireReceipt(out)
	planningBoundaryEvent(t, root, "PreToolUse", "Bash", "", false)
	writeContractSource(t, root, "docs/dev/observer-note.md", "# Material edit\n")
	fixtures.CommitCurrentFixture(t, root)
	out, _ = planningBoundaryEvent(t, root, "PostToolUse", "Bash", "", false)
	if strings.Contains(out, "SUBMISSION RECEIPT") {
		t.Fatalf("commit resubmitted unchanged answer: %s", out)
	}
	planningBoundaryEvent(t, root, "PreToolUse", "mcp__sandbox__execute", "", false)
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	out, _ = planningBoundaryEvent(t, root, "PostToolUse", "mcp__sandbox__execute", "", false)
	requireReceipt(out)
	record, accepted, err := runtime.FindSubmission(stageSnapshot(t, root).State, packet.ID)
	if err != nil || !accepted || record.ControlStatus != "advanced" {
		t.Fatalf("completed answer did not advance: %+v %v", record, err)
	}
}
