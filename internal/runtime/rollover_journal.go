package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controlproof"
)

// Only the original rollover pending carries this short-lived cleanup proof.
// Names are local to the Store journal directory, never caller-supplied paths.
type rolloverJournalSegment struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

func (s *Store) lifecycleJournalDataLocked() ([]byte, error) {
	segments, err := journalSegmentPaths(s.journalPath)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return os.ReadFile(s.journalPath) // Preserve the original flat source bytes.
	}
	state, err := s.read()
	if err != nil {
		return nil, err
	}
	return controlproof.ReadJournalForStateAt(s.journalPath, state)
}

func (s *Store) captureRolloverJournalSegments(complete []byte) ([]rolloverJournalSegment, error) {
	paths, err := journalSegmentPaths(s.journalPath)
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	paths = append(paths, s.journalPath)
	var combined bytes.Buffer
	segments := make([]rolloverJournalSegment, 0, len(paths))
	for _, path := range paths {
		data, err := readRolloverSourceFile(path)
		if err != nil {
			return nil, err
		}
		if combined.Len()+len(data)+1 > controlproof.JournalLimit {
			return nil, errors.New("rollover complete journal exceeds the original read budget")
		}
		segments = append(segments, rolloverJournalSegment{Name: filepath.Base(path), Size: len(data), SHA256: sha256Hex(data)})
		combined.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			combined.WriteByte('\n')
		}
	}
	if !bytes.Equal(combined.Bytes(), complete) {
		return nil, errors.New("rollover journal sources changed before archive publication")
	}
	if err := s.validateRolloverSegmentBinding(segments, complete); err != nil {
		return nil, err
	}
	return segments, nil
}

// The complete archived journal has already passed the original source-pair
// validation. Prove the cleanup list is an exact partition of those bytes.
func (s *Store) validateRolloverSegmentBinding(segments []rolloverJournalSegment, complete []byte) error {
	if len(segments) < 2 || len(complete) > controlproof.JournalLimit {
		return errors.New("rollover original journal segment binding is incomplete")
	}
	active := filepath.Base(s.journalPath)
	offset, previousSequence := 0, 0
	for i, segment := range segments {
		if segment.Name == "" || filepath.Base(segment.Name) != segment.Name || segment.Size < 0 || segment.Size > len(complete)-offset {
			return errors.New("rollover original journal segment coordinate is invalid")
		}
		data := complete[offset : offset+segment.Size]
		if sha256Hex(data) != segment.SHA256 {
			return fmt.Errorf("rollover original journal segment differs from archive: %s", segment.Name)
		}
		offset += segment.Size
		if len(data) > 0 && data[len(data)-1] != '\n' {
			if offset >= len(complete) || complete[offset] != '\n' {
				return errors.New("rollover original journal separator differs from archive")
			}
			offset++
		}
		if i == len(segments)-1 {
			if segment.Name != active {
				return errors.New("rollover original active journal coordinate differs")
			}
			continue
		}
		sequence, ok := parseArchiveSeq(segment.Name)
		if !strings.HasPrefix(segment.Name, active+".archive.") || !strings.HasSuffix(segment.Name, ".jsonl") || !ok || sequence <= previousSequence {
			return errors.New("rollover original rotation segment coordinate differs")
		}
		inspection, err := inspectJournalSegmentData(data)
		if err != nil || inspection.TailSequence != sequence {
			return fmt.Errorf("rollover original rotation segment does not match its sequence: %s", segment.Name)
		}
		previousSequence = sequence
	}
	if offset != len(complete) {
		return errors.New("rollover original journal segment binding omits archived bytes")
	}
	return nil
}

func readRolloverSourceFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > controlproof.JournalLimit {
		return nil, fmt.Errorf("rollover journal source is not a bounded regular file: %s", path)
	}
	return os.ReadFile(path)
}

// All archive, approval and format checks run in the original recovery before
// this branch. Only an already durable target state permits missing old files.
func (s *Store) recoverSegmentedRolloverLocked(pending rolloverPending) error {
	complete, err := os.ReadFile(filepath.Join(pending.Record.ArchiveDir, "loop-events.jsonl"))
	if err != nil {
		return err
	}
	if err := s.validateRolloverSegmentBinding(pending.SourceJournalSegments, complete); err != nil {
		return err
	}
	stateData, err := os.ReadFile(s.statePath)
	if err != nil {
		return err
	}
	state, err := s.read()
	if err != nil {
		return err
	}
	currentHash, err := hashState(state)
	if err != nil {
		return err
	}
	targetHash, err := hashState(pending.FreshState)
	if err != nil {
		return err
	}
	source, target := sha256Hex(stateData) == pending.SourceStateSHA256, currentHash == targetHash
	if !source && !target {
		return errors.New("pending runtime rollover current state matches neither source nor target")
	}
	known := make(map[string]rolloverJournalSegment, len(pending.SourceJournalSegments))
	for _, segment := range pending.SourceJournalSegments {
		known[segment.Name] = segment
	}
	paths, err := journalSegmentPaths(s.journalPath)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if _, ok := known[filepath.Base(path)]; !ok {
			return fmt.Errorf("pending runtime rollover found an unknown journal segment; preserve it: %s", path)
		}
	}
	active := filepath.Base(s.journalPath)
	for _, segment := range pending.SourceJournalSegments {
		data, err := readRolloverSourceFile(filepath.Join(filepath.Dir(s.journalPath), segment.Name))
		if target && segment.Name != active && errors.Is(err, os.ErrNotExist) {
			continue // A completed deletion from this exact original pending.
		}
		if err != nil {
			return fmt.Errorf("pending runtime rollover source %s: %w", segment.Name, err)
		}
		if target && segment.Name == active && len(data) == 0 {
			continue
		}
		if len(data) != segment.Size || sha256Hex(data) != segment.SHA256 {
			return fmt.Errorf("pending runtime rollover source differs; preserve it: %s", segment.Name)
		}
	}
	if source {
		if err := atomicWriteJSON(s.statePath, pending.FreshState); err != nil {
			return fmt.Errorf("seed fresh runtime during segmented rollover: %w", err)
		}
	}
	if err := atomicWriteBytes(s.journalPath, nil, ".loop-journal-*.tmp"); err != nil {
		return fmt.Errorf("reset active journal during segmented rollover: %w", err)
	}
	for _, segment := range pending.SourceJournalSegments[:len(pending.SourceJournalSegments)-1] {
		path := filepath.Join(filepath.Dir(s.journalPath), segment.Name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clear original rollover journal segment %s: %w", segment.Name, err)
		}
	}
	if err := syncDir(filepath.Dir(s.journalPath)); err != nil {
		return fmt.Errorf("sync original rollover journal cleanup: %w", err)
	}
	return s.clearRolloverMarkerLocked()
}
