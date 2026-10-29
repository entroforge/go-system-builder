package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
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
	SHA256    string          `json:"sha256"`
	Decisions map[string]bool `json:"decisions"`
	dirty     bool
}
type decisionIndexEnvelope struct {
	Payload json.RawMessage `json:"payload"`
	SHA256  string          `json:"sha256"`
}

func digestString(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil)) }

// Hashing the entire current log is intentional: size/mtime/inode alone cannot
// prove an arbitrary prefix was not rewritten. Hot appends avoid decoding old
// record bodies, but still do O(log bytes) sequential I/O. Writers cooperate via
// the original OS lock. The checksums detect stale/accidentally corrupt caches;
// this local, unsigned cache is not an authentication boundary against a writer
// able to forge the index and its checksum. Such a writer can also edit the log.
func loadDecisionIndex(ctx context.Context, path string, file *os.File) (decisionIndex, hash.Hash, error) {
	identity, err := filepath.EvalSymlinks(path)
	if err != nil {
		return decisionIndex{}, nil, err
	}
	identity, err = filepath.Abs(identity)
	if err != nil {
		return decisionIndex{}, nil, err
	}
	h := sha256.New()
	var count int64
	var last byte
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return decisionIndex{}, nil, err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			_, _ = h.Write(buffer[:n])
			count += int64(n)
			last = buffer[n-1]
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return decisionIndex{}, nil, fmt.Errorf("read audit outbox: %w", readErr)
		}
	}
	// Appending after a truncated line would corrupt even an otherwise valid
	// trailing JSON object. Fail closed without repairing authoritative events.
	if count > 0 && last != '\n' {
		return decisionIndex{}, nil, errors.New("audit outbox has an incomplete trailing line")
	}
	checksum := digestString(h)
	if cached, ok := readDecisionIndex(path+".index", identity, count, checksum); ok {
		defer metrics.Span(ctx, "audit/index_hit")()
		return cached, h, nil
	}
	defer metrics.Span(ctx, "audit/index_rebuild")()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return decisionIndex{}, nil, err
	}
	decisions, err := readDecisions(ctx, file)
	if err != nil {
		return decisionIndex{}, nil, err
	}
	return decisionIndex{Version: 1, LogPath: identity, Bytes: count, SHA256: checksum, Decisions: decisions, dirty: true}, h, nil
}

func readDecisionIndex(path, identity string, size int64, checksum string) (decisionIndex, bool) {
	file, err := os.Open(path)
	if err != nil {
		return decisionIndex{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, decisionIndexLimit+1))
	if err != nil || len(data) > decisionIndexLimit {
		return decisionIndex{}, false
	}
	var envelope decisionIndexEnvelope
	if json.Unmarshal(data, &envelope) != nil {
		return decisionIndex{}, false
	}
	sum := sha256.Sum256(envelope.Payload)
	if envelope.SHA256 != hex.EncodeToString(sum[:]) {
		return decisionIndex{}, false
	}
	var index decisionIndex
	if json.Unmarshal(envelope.Payload, &index) != nil || index.Version != 1 || index.LogPath != identity || index.Bytes != size || index.SHA256 != checksum || index.Decisions == nil {
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
	sum := sha256.Sum256(payload)
	data, err := json.Marshal(decisionIndexEnvelope{Payload: payload, SHA256: hex.EncodeToString(sum[:])})
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
