package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

const decisionIndexLimit = 16 << 20

type decisionIndex struct {
	Version   int             `json:"version"`
	LogPath   string          `json:"log_path"`
	Bytes     int64           `json:"bytes"`
	Decisions map[string]bool `json:"decisions"`
	dirty     bool
}
type decisionIndexEnvelope struct {
	Payload json.RawMessage `json:"payload"`
}

// The log is authoritative. The cache is only an optimization for cooperating
// writers under the shared lock; size cannot prove external bytes unchanged.
func loadDecisionIndex(ctx context.Context, path string, file *os.File) (decisionIndex, error) {
	identity, err := filepath.EvalSymlinks(path)
	if err != nil {
		return decisionIndex{}, err
	}
	identity, err = filepath.Abs(identity)
	if err != nil {
		return decisionIndex{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return decisionIndex{}, err
	}
	count := stat.Size()
	if count > 0 {
		var last [1]byte
		if _, err := file.ReadAt(last[:], count-1); err != nil {
			return decisionIndex{}, err
		}
		if last[0] != '\n' {
			return decisionIndex{}, errors.New("audit outbox has an incomplete trailing line")
		}
	}
	if cached, ok := readDecisionIndex(path+".index", identity, count); ok {
		defer metrics.Span(ctx, "audit/index_hit")()
		return cached, nil
	}
	defer metrics.Span(ctx, "audit/index_rebuild")()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return decisionIndex{}, err
	}
	decisions, err := readDecisions(ctx, file)
	if err != nil {
		return decisionIndex{}, err
	}
	return decisionIndex{Version: 2, LogPath: identity, Bytes: count, Decisions: decisions, dirty: true}, nil
}

func readDecisionIndex(path, identity string, size int64) (decisionIndex, bool) {
	file, err := os.Open(path)
	if err != nil {
		return decisionIndex{}, false
	}
	defer file.Close()
	// An index above the bounded cache budget can never be used, and reading it
	// would only pay for bytes the reader then throws away. Decide from the
	// size, so a damaged cache costs one stat instead of a full read that
	// scales with the host's I/O load.
	if info, statErr := file.Stat(); statErr != nil || info.Size() > decisionIndexLimit {
		return decisionIndex{}, false
	}
	data, err := io.ReadAll(io.LimitReader(file, decisionIndexLimit+1))
	if err != nil || len(data) > decisionIndexLimit {
		return decisionIndex{}, false
	}
	var envelope decisionIndexEnvelope
	if json.Unmarshal(data, &envelope) != nil {
		return decisionIndex{}, false
	}
	var index decisionIndex
	if json.Unmarshal(envelope.Payload, &index) != nil || index.Version != 2 || index.LogPath != identity || index.Bytes != size || index.Decisions == nil {
		return decisionIndex{}, false
	}
	for id, present := range index.Decisions {
		if id == "" || !present {
			return decisionIndex{}, false
		}
	}
	return index, true
}

func readDecisions(ctx context.Context, file *os.File) (map[string]bool, error) {
	decisions := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var identity struct {
			DecisionID string `json:"decision_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &identity); err != nil {
			return nil, fmt.Errorf("decode audit outbox: %w", err)
		}
		if identity.DecisionID != "" {
			decisions[identity.DecisionID] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read audit outbox: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return decisions, nil
}

func writeDecisionIndex(ctx context.Context, path string, index decisionIndex) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := json.Marshal(index)
	if err != nil {
		return err
	}
	// At this size, disable caching rather than introducing a new audit capacity
	// limit. Subsequent calls rebuild from the real log. This small marker also
	// replaces any stale cache; it never asserts that the decision set is empty.
	if len(payload) > decisionIndexLimit/2 {
		payload = []byte(`{"cache_disabled":"decision index exceeds bounded cache budget"}`)
	}
	data, err := json.Marshal(decisionIndexEnvelope{Payload: payload})
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "hook-audit-index-*.scratch")
	if err != nil {
		return fmt.Errorf("create audit decision index: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	defer temp.Close()
	if _, err := temp.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit decision index: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync audit decision index: %w", err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path+".index"); err != nil {
		return fmt.Errorf("publish audit decision index: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync audit index directory: %w", err)
	}
	return nil
}
