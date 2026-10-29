package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
)

const nativeTaskWindow int64 = 4 << 20
const nativeTaskReadBudget int64 = 2 * nativeTaskWindow
const nativeTaskReadBlock int64 = 64 << 10

type nativeTaskReadDiagnostic struct {
	Reason                       string
	Bytes                        int64
	Calls                        int
	TailTruncated, HeadTruncated bool
	Err                          error
}

type nativeTranscriptChunk struct {
	offset int64
	data   []byte
}

// One descriptor, cursor and cumulative budget serve both selection passes.
// Cached ranges belong to this read only, never to a later Hook invocation.
type nativeTaskTranscript struct {
	ctx        context.Context
	reader     io.ReaderAt
	size       int64
	chunks     []nativeTranscriptChunk
	diagnostic nativeTaskReadDiagnostic
}

func (r *nativeTaskTranscript) read(offset, length int64) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]byte, length)
	for position := offset; position < offset+length; {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		end := offset + length
		var cached []byte
		for _, chunk := range r.chunks {
			if chunk.offset <= position && position < chunk.offset+int64(len(chunk.data)) {
				cached = chunk.data[position-chunk.offset:]
				break
			}
			if chunk.offset > position && chunk.offset < end {
				end = chunk.offset
			}
		}
		if cached != nil {
			count := copy(out[position-offset:], cached)
			position += int64(count)
			continue
		}
		if end-position > nativeTaskReadBudget-r.diagnostic.Bytes {
			return nil, errors.New("native task transcript cumulative read budget exhausted")
		}
		data := make([]byte, end-position)
		count, err := r.reader.ReadAt(data, position)
		r.diagnostic.Calls++
		r.diagnostic.Bytes += int64(count)
		if count != 0 {
			r.chunks = append(r.chunks, nativeTranscriptChunk{position, data[:count]})
			copy(out[position-offset:], data[:count])
		}
		if err != nil && !(err == io.EOF && count == len(data)) {
			return nil, err
		}
		if count != len(data) {
			return nil, io.ErrUnexpectedEOF
		}
		position = end
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Parse only actual user text. tool_result blocks cannot nominate a task.
// A plain empty user string retains the original S6 selection semantics.
func nativeTranscriptUserTask(line []byte, final string) (string, bool, bool) {
	var row struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &row) != nil || row.Type != "user" || row.Message.Role != "user" {
		return "", false, false
	}
	var plain string
	if json.Unmarshal(row.Message.Content, &plain) == nil {
		return plain, true, true
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(row.Message.Content, &parts) != nil {
		return "", false, false
	}
	var task strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			task.WriteByte('\n')
			task.WriteString(part.Text)
		}
	}
	text := task.String()
	return text, strings.TrimSpace(final+text) != strings.TrimSpace(final), false
}

// Reverse fragments are kept as slices until a complete JSON row is found.
// Even a multi-megabyte line is joined once rather than copied per block.
func nativeTaskJoin(prefix []byte, fragments [][]byte, reverse bool) []byte {
	if len(fragments) == 0 {
		return prefix
	}
	length := len(prefix)
	for _, fragment := range fragments {
		length += len(fragment)
	}
	out := make([]byte, 0, length)
	out = append(out, prefix...)
	if reverse {
		for i := len(fragments) - 1; i >= 0; i-- {
			out = append(out, fragments[i]...)
		}
	} else {
		for _, fragment := range fragments {
			out = append(out, fragment...)
		}
	}
	return out
}

func (r *nativeTaskTranscript) latest(refs []string, final string) (string, bool, error) {
	lower := max(int64(0), r.size-nativeTaskWindow)
	r.diagnostic.TailTruncated = lower != 0
	var fragments [][]byte
	match := func(line []byte) (string, bool) {
		task, ok, _ := nativeTranscriptUserTask(line, final)
		if ok {
			for _, ref := range refs {
				if nativeS6ExactReference(task, ref) {
					return task, true
				}
			}
		}
		return "", false
	}
	for end := r.size; end > lower; {
		start := max(lower, end-nativeTaskReadBlock)
		data, err := r.read(start, end-start)
		if err != nil {
			return "", false, err
		}
		for edge := len(data); ; {
			split := bytes.LastIndexByte(data[:edge], '\n')
			if split < 0 {
				fragments = append(fragments, data[:edge])
				break
			}
			line := nativeTaskJoin(data[split+1:edge], fragments, true)
			fragments = fragments[:0]
			if task, ok := match(line); ok {
				return task, true, nil
			}
			edge = split
		}
		end = start
	}
	// A truncated leading row is never interpreted as a complete JSON record.
	if lower == 0 {
		if task, ok := match(nativeTaskJoin(nil, fragments, true)); ok {
			return task, true, nil
		}
	}
	return "", false, nil
}

func (r *nativeTaskTranscript) earliest(final string) (string, bool, error) {
	upper := min(r.size, nativeTaskWindow)
	r.diagnostic.HeadTruncated = upper < r.size
	var fragments [][]byte
	suffix := ""
	consume := func(line []byte) bool {
		task, ok, plain := nativeTranscriptUserTask(line, final+suffix)
		if plain {
			suffix += "\n" + task
		} else {
			suffix += task
		}
		return ok
	}
	for start := int64(0); start < upper; {
		end := min(upper, start+nativeTaskReadBlock)
		data, err := r.read(start, end-start)
		if err != nil {
			return "", false, err
		}
		for len(data) > 0 {
			split := bytes.IndexByte(data, '\n')
			if split < 0 {
				fragments = append(fragments, data)
				break
			}
			// Head fragments are in forward order; append this row's final slice.
			line := data[:split]
			if len(fragments) != 0 {
				line = nativeTaskJoin(nil, append(fragments, line), false)
			}
			fragments = nil
			if consume(line) {
				return suffix, true, nil
			}
			data = data[split+1:]
		}
		start = end
	}
	// Match the legacy head reader's treatment of a final non-newline row.
	if consume(nativeTaskJoin(nil, fragments, false)) {
		return suffix, true, nil
	}
	return suffix, false, nil
}

func nativeTaskTextDiagnostic(in policy.Input, refs []string, latest bool) (string, nativeTaskReadDiagnostic) {
	file, err := os.Open(in.AgentTranscriptPath)
	if err != nil {
		return in.LastAssistantMessage, nativeTaskReadDiagnostic{Reason: "unreadable", Err: err}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return in.LastAssistantMessage, nativeTaskReadDiagnostic{Reason: "unreadable", Err: err}
	}
	reader := &nativeTaskTranscript{ctx: contextForHook(in), reader: file, size: info.Size()}
	return reader.taskText(in.LastAssistantMessage, refs, latest)
}

func (r *nativeTaskTranscript) taskText(final string, refs []string, latest bool) (string, nativeTaskReadDiagnostic) {
	if latest {
		task, ok, err := r.latest(refs, final)
		if ok && r.ctx.Err() != nil {
			err, ok = r.ctx.Err(), false
		}
		if ok {
			r.diagnostic.Reason = "latest_exact_task"
			return final + "\n" + task, r.diagnostic
		}
		r.diagnostic.Err = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			r.diagnostic.Reason = "canceled"
			return final, r.diagnostic
		}
	}
	// The compatibility pass still selects the earliest real user task in the
	// original head window, using cached tail ranges wherever they overlap.
	task, ok, err := r.earliest(final)
	if ok && r.ctx.Err() != nil {
		err, ok = r.ctx.Err(), false
	}
	if err != nil {
		r.diagnostic.Err = err
	}
	switch {
	case errors.Is(r.diagnostic.Err, context.Canceled), errors.Is(r.diagnostic.Err, context.DeadlineExceeded):
		r.diagnostic.Reason = "canceled"
	case r.diagnostic.Err != nil:
		r.diagnostic.Reason = "unreadable"
	case ok:
		r.diagnostic.Reason = "earliest_compatibility_task"
	case r.diagnostic.TailTruncated || r.diagnostic.HeadTruncated:
		r.diagnostic.Reason = "read_boundary"
	default:
		r.diagnostic.Reason = "no_task_match"
	}
	if r.diagnostic.Reason == "canceled" {
		return final, r.diagnostic
	}
	return final + task, r.diagnostic
}
