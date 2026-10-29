package targetedauthoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const sourceReadLimit int64 = 20 << 20
const sourceCacheLimit = 32 << 20

// A readScope belongs to one source-proof invocation. It reuses only already
// verified original control bytes, never Runtime decisions, mutable product
// checks, or data across reception boundaries. It is deliberately not global.
type readScope struct {
	ctx         context.Context
	rows        map[string]map[string]map[string]any
	duplicates  map[string]map[string]bool
	sources     map[string][]byte
	tasks       map[string]map[string]json.RawMessage
	cachedBytes int
}

func sourceContext(contexts []context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background() // Compatibility for explicit non-Hook callers.
}

func newReadScope(state map[string]any, ctx context.Context) (*readScope, error) {
	s := &readScope{ctx: ctx, rows: map[string]map[string]map[string]any{}, duplicates: map[string]map[string]bool{}, sources: map[string][]byte{}, tasks: map[string]map[string]json.RawMessage{}}
	entities, _ := state["entities"].(map[string]any)
	for _, kind := range []string{"agents", "teams", "tasks"} {
		s.rows[kind] = map[string]map[string]any{}
		s.duplicates[kind] = map[string]bool{}
		rows, _ := entities[kind].([]any)
		for _, raw := range rows {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			row, _ := raw.(map[string]any)
			id := stringField(row["id"])
			if _, exists := s.rows[kind][id]; exists {
				s.duplicates[kind][id] = true
			}
			s.rows[kind][id] = row
		}
	}
	return s, ctx.Err()
}

func (s *readScope) lookup(kind, id string) (map[string]any, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if s.duplicates[kind][id] {
		return nil, fmt.Errorf("Targeted duplicate registered %s %s", kind, id)
	}
	return s.rows[kind][id], nil
}

func (s *readScope) task(root string, row map[string]any) (map[string]json.RawMessage, error) {
	ref, digest := stringField(row["path"]), stringField(row["sha256"])
	key := root + "\x00" + ref + "\x00" + digest
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if fields, ok := s.tasks[key]; ok {
		return fields, nil
	}
	data, err := s.read(root, ref, digest)
	if err != nil {
		return nil, fmt.Errorf("Targeted original TASK %s: %w", stringField(row["id"]), err)
	}
	fields := map[string]json.RawMessage{}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		if err := workpacket.ValidateUniqueJSON(data); err != nil {
			return nil, err
		}
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, fmt.Errorf("Targeted original TASK JSON: %w", err)
		}
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if _, cached := s.sources[key]; cached {
		s.tasks[key] = fields
	}
	return fields, nil
}

func (s *readScope) read(root, ref, digest string) ([]byte, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	key := root + "\x00" + ref + "\x00" + digest
	if data, ok := s.sources[key]; ok {
		return data, nil
	}
	parent, name, err := workpacket.OpenParent(root, ref, false)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("original control source is not a regular file")
	}
	f, err := parent.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) {
		return nil, fmt.Errorf("original control source changed while opening")
	}
	var out bytes.Buffer
	buffer := make([]byte, 32<<10)
	limited := io.LimitReader(f, sourceReadLimit+1)
	for {
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		n, err := limited.Read(buffer)
		if n > 0 {
			out.Write(buffer[:n])
		}
		if int64(out.Len()) > sourceReadLimit {
			return nil, fmt.Errorf("original control source exceeds %d bytes", sourceReadLimit)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	data := out.Bytes()
	if digest == "" || workpacket.Digest(data) != digest {
		return nil, fmt.Errorf("original registered source digest differs: %s", ref)
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.sources) < 64 && len(data) <= sourceCacheLimit-s.cachedBytes {
		s.sources[key] = data
		s.cachedBytes += len(data)
	}
	return data, nil
}
