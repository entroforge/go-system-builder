package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// resultStaging is a filesystem intent, never a professional receipt. All its
// methods run inside the original Runtime Writer mutation lock, after pending
// recovery and current-source checks. The intent precedes every publication so
// a killed process leaves enough ownership proof for the original consumer.
type resultStaging struct {
	Version    int               `json:"version"`
	Owner      string            `json:"owner"`
	Source     string            `json:"source"`
	OccurredAt time.Time         `json:"occurred_at"`
	Artifacts  map[string]string `json:"artifacts"`
	root, path string
	state      map[string]any
}

func openResultStaging(root, resultRel string, state map[string]any, submitted, effective []byte, packet *workpacket.ReviewResultPacket, sourceRoot string, occurredAt time.Time) (*resultStaging, error) {
	if filepath.ToSlash(filepath.Clean(resultRel)) != resultRel || !strings.HasPrefix(resultRel, ".claude/evidence/") {
		return nil, errors.New("review staging intent path is outside canonical evidence scope")
	}
	ptr := PlanPointerFromState(state)
	var planIdentity any
	if ptr != nil {
		planIdentity = []any{ptr.PlanID, ptr.Path, ptr.SHA256, ptr.Revision}
	}
	sourceIdentity, err := json.Marshal([]any{state["runtime_id"], baselineGeneration(state), currentReviewRound(state), planIdentity, packet, sourceRoot})
	if err != nil {
		return nil, fmt.Errorf("encode review staging source: %w", err)
	}
	source := sha256Of(sourceIdentity)
	identity, err := json.Marshal([]any{source, json.RawMessage(submitted), effective})
	if err != nil {
		return nil, fmt.Errorf("encode review staging ownership: %w", err)
	}
	path, err := repositoryContainedPath(root, resultRel+".staging.json")
	if err != nil {
		return nil, err
	}
	staging := &resultStaging{Version: 1, Owner: sha256Of(identity), Source: source, OccurredAt: occurredAt, Artifacts: map[string]string{}, root: root, path: path, state: state}
	if info, statErr := os.Lstat(path); statErr == nil && !info.Mode().IsRegular() {
		return nil, errors.New("review staging intent must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		var previous resultStaging
		if err := json.Unmarshal(data, &previous); err != nil {
			return nil, fmt.Errorf("read review staging intent: %w", err)
		}
		if previous.Version != 1 || previous.Source != staging.Source || previous.OccurredAt.IsZero() || previous.Artifacts == nil {
			return nil, errors.New("review staging intent differs from this exact answer and Runtime source")
		}
		if previous.Owner != staging.Owner {
			// Successful cleanup can leave an empty intent behind. A corrected
			// answer may keep its original ID only when no prior publication or
			// Runtime reference survives. This runs under the original Writer lock.
			for rel := range previous.Artifacts {
				if filepath.ToSlash(filepath.Clean(rel)) != rel || !strings.HasPrefix(rel, ".claude/evidence/") || containsStringValue(state, rel) {
					return nil, errors.New("review staging intent differs from this exact answer and Runtime source")
				}
				abs, pathErr := repositoryContainedPath(root, rel)
				if pathErr != nil {
					return nil, pathErr
				}
				if _, statErr := os.Lstat(abs); !errors.Is(statErr, os.ErrNotExist) {
					return nil, errors.New("review staging intent differs from this exact answer and Runtime source")
				}
			}
			if err := staging.save(); err != nil {
				return nil, err
			}
			return staging, nil
		}
		staging.OccurredAt, staging.Artifacts = previous.OccurredAt, previous.Artifacts
		return staging, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := staging.save(); err != nil {
		return nil, err
	}
	return staging, nil
}

func (s *resultStaging) save() error {
	data, err := marshalArtifact(s)
	if err != nil {
		return err
	}
	return publishStagingFile(s.path, data, true)
}

func (s *resultStaging) write(relative string, data []byte) error {
	if filepath.ToSlash(filepath.Clean(relative)) != relative || !strings.HasPrefix(relative, ".claude/evidence/") {
		return errors.New("review staging path is outside evidence")
	}
	path, err := repositoryContainedPath(s.root, relative)
	if err != nil {
		return err
	}
	if containsStringValue(s.state, relative) {
		return fmt.Errorf("review staging artifact %s is already referenced by Runtime", relative)
	}
	digest := sha256Of(data)
	ownedDigest, owned := s.Artifacts[relative]
	if owned && ownedDigest != digest {
		return fmt.Errorf("review staging candidate %s differs from original intent", relative)
	}
	if info, statErr := os.Lstat(path); statErr == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("review staging artifact %s must be a regular file", relative)
	}
	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		if !owned {
			return fmt.Errorf("review artifact %s exists without this submission's ownership intent", relative)
		}
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("review staging artifact %s bytes drifted from original intent", relative)
		}
		return nil
	}
	if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if !owned {
		s.Artifacts[relative] = digest
		if err := s.save(); err != nil {
			return err
		}
	}
	return publishStagingFile(path, data, false)
}

// Publish fully synced bytes atomically. A process death cannot expose a
// partially written artifact or intent. Existing artifacts are never replaced.
func publishStagingFile(path string, data []byte, replace bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".review-stage-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0o644); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if replace {
		err = os.Rename(temporary, path)
	} else {
		err = os.Link(temporary, path)
	}
	if err != nil {
		return err
	}
	return loopruntime.SyncDirectory(filepath.Dir(path))
}
