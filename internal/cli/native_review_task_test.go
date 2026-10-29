package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestNativeReviewResumeUsesLatestExactTaskAndIgnoresToolResults(t *testing.T) {
	oldRef, newRef := "/main/questions/old.md", "/main/questions/new.md"
	path := filepath.Join(t.TempDir(), "native.jsonl")
	var transcript strings.Builder
	appendRow := func(content any) {
		row, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
		transcript.Write(row)
		transcript.WriteByte('\n')
	}
	appendRow("First review " + oldRef)
	appendRow([]map[string]string{{"type": "text", "text": "Now review " + newRef}})
	appendRow([]map[string]string{{"type": "tool_result", "text": "Quoted old output " + oldRef}})
	appendRow("Continue your work")
	if err := os.WriteFile(path, []byte(transcript.String()), 0600); err != nil {
		t.Fatal(err)
	}
	text := nativeReviewTaskText(policy.Input{AgentTranscriptPath: path, LastAssistantMessage: "Finished " + newRef}, []string{oldRef, newRef})
	if strings.Contains(text, oldRef) || !nativeS6ExactReference(text, newRef) {
		t.Fatalf("resumed responsibility was confused with initial task/tool output: %s", text)
	}
	// A final handoff naming two real responsibilities is still ambiguous.
	text = nativeReviewTaskText(policy.Input{AgentTranscriptPath: path, LastAssistantMessage: oldRef + "\n" + newRef}, []string{oldRef, newRef})
	if !nativeS6ExactReference(text, oldRef) || !nativeS6ExactReference(text, newRef) {
		t.Fatal(text)
	}
}

func TestNativeS9DoesNotCaptureS8WithRetainedRepairPointer(t *testing.T) {
	state := map[string]any{"lifecycle": map[string]any{"state": "bug_resolution", "phase": "investigation"}, "review": map[string]any{"repair": map[string]any{"status": "targeted_reverification"}}}
	root := t.TempDir()
	if nativeS9Available(root, state) || len(nativeS9PlanPackets(root, state)) != 0 {
		t.Fatal("retained S9 state captured current S8 responsibility")
	}
	state["lifecycle"].(map[string]any)["phase"] = "fixing"
	if !nativeS9Available(root, state) {
		t.Fatal("actual S9 targeted work lost its lifecycle handler")
	}
}

func TestNativeReviewResumeReadsBoundedTranscriptTail(t *testing.T) {
	oldRef, newRef := "/main/old/brief.md", "/main/current/brief.md"
	var transcript strings.Builder
	appendRow := func(content any) {
		row, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
		transcript.Write(row)
		transcript.WriteByte('\n')
	}
	appendRow("Earlier " + oldRef)
	appendRow([]map[string]string{{"type": "tool_result", "text": strings.Repeat("x", 4<<20) + oldRef}})
	appendRow("Resumed actual task " + newRef)
	path := filepath.Join(t.TempDir(), "large-native.jsonl")
	if err := os.WriteFile(path, []byte(transcript.String()), 0600); err != nil {
		t.Fatal(err)
	}
	text := nativeReviewTaskText(policy.Input{AgentTranscriptPath: path, LastAssistantMessage: "Finished."}, []string{oldRef, newRef})
	if !nativeS6ExactReference(text, newRef) || nativeS6ExactReference(text, oldRef) {
		t.Fatalf("bounded tail lost current responsibility: %s", text)
	}
}

func TestNativeReviewLatestTaskKeepsHistoricalAliasAndExactReferenceBoundary(t *testing.T) {
	ref := "/worker/historical/questionnaire.md"
	path := filepath.Join(t.TempDir(), "alias.jsonl")
	var rows strings.Builder
	appendRow := func(content any) {
		data, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
		rows.Write(data)
		rows.WriteByte('\n')
	}
	appendRow("Original task " + ref)
	appendRow([]map[string]string{{"type": "text", "text": "Resumed actual task `" + ref + "`"}})
	appendRow("A different path " + ref + ".backup")
	appendRow([]map[string]string{{"type": "tool_result", "text": ref}})
	if err := os.WriteFile(path, []byte(rows.String()), 0600); err != nil {
		t.Fatal(err)
	}
	text := nativeReviewTaskText(policy.Input{AgentTranscriptPath: path}, []string{ref})
	if !strings.Contains(text, "Resumed actual task") || strings.Contains(text, "backup") || strings.Contains(text, "Original task") {
		t.Fatalf("historical alias/exact boundary changed: %s", text)
	}
}

func BenchmarkNativeReviewLatestTaskInLargeTranscript(b *testing.B) {
	ref := "/main/current/questionnaire.md"
	old, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": strings.Repeat("old task material ", 128)}})
	latest, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "Actual resumed task " + ref}})
	data := append([]byte(strings.Repeat(string(old)+"\n", 1800)), append(latest, '\n')...)
	path := filepath.Join(b.TempDir(), "large.jsonl")
	if err := os.WriteFile(path, data, 0600); err != nil {
		b.Fatal(err)
	}
	in := policy.Input{AgentTranscriptPath: path}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if text := nativeReviewTaskText(in, []string{ref}); !strings.Contains(text, "Actual resumed task") {
			b.Fatal(text)
		}
	}
}
