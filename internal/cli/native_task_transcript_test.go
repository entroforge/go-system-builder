package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

type nativeTranscriptReadCounter struct {
	t         *testing.T
	reader    *bytes.Reader
	ranges    [][2]int64
	afterRead func()
}

func (r *nativeTranscriptReadCounter) ReadAt(data []byte, offset int64) (int, error) {
	count, err := r.reader.ReadAt(data, offset)
	end := offset + int64(count)
	for _, previous := range r.ranges {
		if offset < previous[1] && end > previous[0] {
			r.t.Fatalf("duplicate transcript IO [%d,%d) overlaps %v", offset, end, previous)
		}
	}
	r.ranges = append(r.ranges, [2]int64{offset, end})
	if r.afterRead != nil {
		r.afterRead()
	}
	return count, err
}

func nativeTranscriptTestRow(role string, content any) []byte {
	row, _ := json.Marshal(map[string]any{"type": role, "message": map[string]any{"role": role, "content": content}})
	return append(row, '\n')
}

func nativeTranscriptTestReader(t *testing.T, data []byte) (*nativeTaskTranscript, *nativeTranscriptReadCounter) {
	t.Helper()
	counter := &nativeTranscriptReadCounter{t: t, reader: bytes.NewReader(data)}
	return &nativeTaskTranscript{ctx: context.Background(), reader: counter, size: int64(len(data))}, counter
}

func TestNativeTranscriptLatestTaskStopsAfterOneBlock(t *testing.T) {
	ref := "/actual/current/questionnaire.md"
	data := append(bytes.Repeat([]byte("historical material\n"), 500000), nativeTranscriptTestRow("user", "Actual resumed task "+ref)...)
	reader, _ := nativeTranscriptTestReader(t, data)
	text, diagnostic := reader.taskText("Delivered "+ref, []string{ref}, true)
	if !strings.Contains(text, "Actual resumed task") || diagnostic.Reason != "latest_exact_task" {
		t.Fatalf("selection: %q %+v", text, diagnostic)
	}
	if diagnostic.Calls != 1 || diagnostic.Bytes != nativeTaskReadBlock {
		t.Fatalf("near-tail match did not stop IO: %+v", diagnostic)
	}
}

func TestNativeTranscriptFallbackKeepsOriginalHeadTask(t *testing.T) {
	oldRef, currentRef := "/main/original/brief.md", "/main/current/brief.md"
	head := nativeTranscriptTestRow("user", "Original task "+oldRef)
	data := append(head, nativeTranscriptTestRow("assistant", strings.Repeat("x", 5<<20))...)
	reader, _ := nativeTranscriptTestReader(t, data)
	text, diagnostic := reader.taskText("Finished", []string{currentRef}, true)
	if text != "Finished\nOriginal task "+oldRef || diagnostic.Reason != "earliest_compatibility_task" {
		t.Fatalf("original fallback lost: %q %+v", text, diagnostic)
	}
	if diagnostic.Bytes > nativeTaskWindow+nativeTaskReadBlock {
		t.Fatalf("fallback did not stop at first task: %+v", diagnostic)
	}
}

func TestNativeTranscriptFallbackReusesOverlappingReadRanges(t *testing.T) {
	for _, size := range []int{3 << 20, 6 << 20, 10 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			reader, _ := nativeTranscriptTestReader(t, bytes.Repeat([]byte("x"), size))
			text, diagnostic := reader.taskText("Final delivery", []string{"/absent/task.md"}, true)
			if text != "Final delivery" || diagnostic.Reason != "read_boundary" && diagnostic.Reason != "no_task_match" {
				t.Fatalf("unexpected fallback: %q %+v", text, diagnostic)
			}
			wantBytes := min(int64(size), nativeTaskReadBudget)
			if diagnostic.Bytes != wantBytes {
				t.Fatalf("read bytes=%d want unique coverage %d: %+v", diagnostic.Bytes, wantBytes, diagnostic)
			}
			if diagnostic.Calls != int(wantBytes/nativeTaskReadBlock) {
				t.Fatalf("calls=%d want %d", diagnostic.Calls, wantBytes/nativeTaskReadBlock)
			}
		})
	}
}

func TestNativeTranscriptTaskSelectionAcrossLongAndPartialRows(t *testing.T) {
	ref := "/historical/alias/questionnaire.md"
	for _, mode := range []string{"long-complete", "tail-truncated", "unterminated-final", "wrong-role", "tools-and-boundaries"} {
		t.Run(mode, func(t *testing.T) {
			data := nativeTranscriptTestRow("user", "Initial task")
			want := "Finished\nInitial task"
			switch mode {
			case "long-complete":
				task := strings.Repeat("long task material ", int(nativeTaskReadBlock/8)) + " `" + ref + "`"
				data = append(data, nativeTranscriptTestRow("user", task)...)
				want = "Finished\n" + task
			case "tail-truncated":
				data = nativeTranscriptTestRow("user", strings.Repeat("x", int(nativeTaskWindow)+512)+" "+ref)
				want = "Finished"
			case "unterminated-final":
				row := nativeTranscriptTestRow("user", "Actual task "+ref)
				data = append(data, row[:len(row)-1]...)
				want = "Finished\nActual task " + ref
			case "wrong-role":
				row, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "assistant", "content": ref}})
				data = append(data, row...)
			case "tools-and-boundaries":
				data = append(data, nativeTranscriptTestRow("user", []map[string]string{{"type": "text", "text": "Actual task `" + ref + "`"}})...)
				data = append(data, nativeTranscriptTestRow("user", []map[string]string{{"type": "tool_result", "text": ref}})...)
				data = append(data, nativeTranscriptTestRow("user", "Different path "+ref+".backup")...)
				want = "Finished\n\nActual task `" + ref + "`"
			}
			reader, _ := nativeTranscriptTestReader(t, data)
			text, _ := reader.taskText("Finished", []string{ref}, true)
			if text != want {
				t.Fatalf("task mismatch (%d bytes got, %d wanted)", len(text), len(want))
			}
		})
	}
}

func TestNativeTranscriptS6RetainsEarliestUserSelectionAndRendering(t *testing.T) {
	for _, content := range []any{"First task", "", []map[string]string{{"type": "text", "text": "First task"}}} {
		data := nativeTranscriptTestRow("user", []map[string]string{{"type": "tool_result", "text": "Quoted /wrong/task.md"}})
		data = append(data, nativeTranscriptTestRow("user", content)...)
		data = append(data, nativeTranscriptTestRow("user", "Later task /latest/task.md")...)
		reader, _ := nativeTranscriptTestReader(t, data)
		text, diagnostic := reader.taskText("Final", nil, false)
		want := "Final\nFirst task"
		if plain, ok := content.(string); ok {
			want = "Final\n" + plain
		}
		if text != want || diagnostic.Reason != "earliest_compatibility_task" {
			t.Fatalf("earliest S6 changed: %q %+v", text, diagnostic)
		}
	}
}

func TestNativeTranscriptCancellationDoesNotStartHeadFallback(t *testing.T) {
	data := append(nativeTranscriptTestRow("user", "Old task"), bytes.Repeat([]byte("x"), int(nativeTaskReadBlock)*3)...)
	reader, counter := nativeTranscriptTestReader(t, data)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader.ctx = ctx
	counter.afterRead = cancel
	text, diagnostic := reader.taskText("Final", []string{"/absent/task.md"}, true)
	if text != "Final" || diagnostic.Reason != "canceled" || !errors.Is(diagnostic.Err, context.Canceled) || diagnostic.Calls != 1 {
		t.Fatalf("cancellation fell back: %q %+v", text, diagnostic)
	}
}

type nativeTranscriptBrokenReader struct{}

func (nativeTranscriptBrokenReader) ReadAt([]byte, int64) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestNativeTranscriptUnreadableAndNoMatchHaveDifferentDiagnostics(t *testing.T) {
	broken := &nativeTaskTranscript{ctx: context.Background(), reader: nativeTranscriptBrokenReader{}, size: 100}
	if _, diagnostic := broken.taskText("Final", nil, true); diagnostic.Reason != "unreadable" {
		t.Fatal(diagnostic)
	}
	reader, _ := nativeTranscriptTestReader(t, nativeTranscriptTestRow("assistant", "No user task"))
	if _, diagnostic := reader.taskText("Final", nil, true); diagnostic.Reason != "no_task_match" {
		t.Fatal(diagnostic)
	}
}

func BenchmarkNativeTranscriptNearTailReadCost(b *testing.B) {
	ref := "/actual/current/questionnaire.md"
	row := nativeTranscriptTestRow("assistant", strings.Repeat("historical output ", 128))
	for _, size := range []int{1 << 20, 16 << 20, 64 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "large.jsonl")
			data := bytes.Repeat(row, size/len(row))
			data = append(data, nativeTranscriptTestRow("user", "Actual task "+ref)...)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				b.Fatal(err)
			}
			in := policy.Input{AgentTranscriptPath: path}
			b.ReportAllocs()
			var readBytes int64
			var readCalls int
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				text, diagnostic := nativeTaskTextDiagnostic(in, []string{ref}, true)
				if !strings.Contains(text, "Actual task") || diagnostic.Calls != 1 {
					b.Fatalf("%s %+v", text, diagnostic)
				}
				readBytes += diagnostic.Bytes
				readCalls += diagnostic.Calls
			}
			b.StopTimer()
			b.ReportMetric(float64(readBytes)/float64(b.N), "read-bytes/op")
			b.ReportMetric(float64(readCalls)/float64(b.N), "reads/op")
		})
	}
}

func TestNativeTranscriptTailCutoffDiscardsLeadingRowEvenAtRowBoundary(t *testing.T) {
	ref := "/actual/boundary/task.md"
	row := nativeTranscriptTestRow("user", "Boundary task "+ref)
	padding := int(nativeTaskWindow) - len(row)
	row = nativeTranscriptTestRow("user", strings.Repeat("x", padding)+"Boundary task "+ref)
	if int64(len(row)) != nativeTaskWindow {
		t.Fatal("fixture row is not exactly the tail window")
	}
	// The original bounded tail discarded its leading row whenever truncated,
	// including this exact alignment. The head contains only a partial row.
	data := append([]byte("not-json\n"), row...)
	reader, _ := nativeTranscriptTestReader(t, data)
	text, diagnostic := reader.taskText("Final", []string{ref}, true)
	if text != "Final" || diagnostic.Reason != "read_boundary" {
		t.Fatalf("leading cutoff row became a task: %d bytes %+v", len(text), diagnostic)
	}
	if diagnostic.Bytes != int64(len(data)) {
		t.Fatalf("fallback reread overlapping boundary: %+v", diagnostic)
	}
}
