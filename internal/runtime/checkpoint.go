package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

const (
	checkpointIndexPath    = ".claude/runtime-checkpoints/index.json"
	checkpointStateLimit   = 16 << 20
	checkpointJournalLimit = 64 << 20
)

// RuntimeCheckpoint is recovery evidence; normal Runtime readers never use it.
type RuntimeCheckpoint struct {
	SchemaVersion string `json:"schema_version"`
	RuntimeID     string `json:"runtime_id"`
	Generation    int    `json:"baseline_generation"`
	Revision      int    `json:"revision"`
	StatePath     string `json:"state_path"`
	JournalPath   string `json:"journal_path"`
	StateSHA256   string `json:"state_sha256"`
	JournalSHA256 string `json:"journal_sha256"`
}

type checkpointIndex struct {
	SchemaVersion string   `json:"schema_version"`
	Manifests     []string `json:"manifests"`
}

type CheckpointPair struct {
	PacketBodies             map[string][]byte
	Manifest                 RuntimeCheckpoint
	State                    map[string]any
	StateBytes, JournalBytes []byte
}

func hasIssuedIndices(state map[string]any) bool {
	if !workstate.SupportsAssignments(state["schema_version"]) {
		return false
	}
	entities, _ := state["entities"].(map[string]any)
	for _, key := range []string{"work_packets", "submissions", "assignments", "assignment_revision_receipts"} {
		rows, _ := entities[key].([]any)
		if len(rows) > 0 {
			return true
		}
	}
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if _, bound := agent["context_binding"]; bound {
			return true
		}
	}
	return false
}

// publishCheckpointLocked runs before removing the original durable marker.
// Failure leaves that marker for the original Writer retry protocol.
func (s *Store) publishCheckpointLocked() error {
	if s.offlineRecovery || s.root == "" {
		return nil
	}
	stateBytes, err := os.ReadFile(s.statePath)
	if err != nil {
		return fmt.Errorf("read runtime: %w", err)
	}
	state, err := s.decodeState(stateBytes)
	if err != nil {
		return err
	}
	if !hasIssuedIndices(state) {
		return nil
	}
	if err := s.validateCandidate(state); err != nil {
		return err
	}
	journal, err := inspectJournal(s.journalPath)
	if err != nil {
		return err
	}
	if err := validateStateJournalPair(state, journal); err != nil {
		return err
	}
	var journalBytes []byte
	for _, event := range journal.Events {
		data, err := jsonLineBytes(event)
		if err != nil {
			return err
		}
		if len(journalBytes)+len(data) > checkpointJournalLimit {
			return errors.New("complete checkpoint journal exceeds 64 MiB; pending retained")
		}
		journalBytes = append(journalBytes, data...)
	}
	if len(stateBytes) > checkpointStateLimit {
		return errors.New("checkpoint state exceeds 16 MiB; pending retained")
	}
	revision, err := integerField(state, "revision")
	if err != nil {
		return err
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return err
	}
	stateHash, journalHash := sha256Hex(stateBytes), sha256Hex(journalBytes)
	id := sha256Hex([]byte(stateHash + ":" + journalHash))
	dir := ".claude/runtime-checkpoints/" + id
	if err := s.publishCheckpointPacketBodies(state, dir); err != nil {
		return err
	}
	manifest := RuntimeCheckpoint{SchemaVersion: "1.0.0", RuntimeID: stringValue(state["runtime_id"]), Generation: generation, Revision: revision, StatePath: dir + "/state.json", JournalPath: dir + "/events.jsonl", StateSHA256: stateHash, JournalSHA256: journalHash}
	manifestPath := dir + "/manifest.json"
	manifestBytes, err := jsonDocumentBytes(manifest)
	if err != nil {
		return err
	}
	for _, asset := range []struct {
		path  string
		data  []byte
		limit int64
	}{{manifest.StatePath, stateBytes, checkpointStateLimit}, {manifest.JournalPath, journalBytes, checkpointJournalLimit}, {manifestPath, manifestBytes, 4096}} {
		existing, err := workpacket.PublishBounded(s.root, asset.path, asset.data, asset.limit)
		if err != nil {
			return fmt.Errorf("publish checkpoint: %w", err)
		}
		if !bytes.Equal(existing, asset.data) {
			return errors.New("immutable checkpoint asset conflict")
		}
	}
	index, err := readCheckpointIndex(s.root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	next := checkpointIndex{SchemaVersion: "1.0.0", Manifests: []string{manifestPath}}
	for _, old := range index.Manifests {
		if old != manifestPath && len(next.Manifests) < 2 {
			next.Manifests = append(next.Manifests, old)
		}
	}
	data, err := jsonDocumentBytes(next)
	if err != nil {
		return err
	}
	parent, name, err := workpacket.OpenParent(s.root, checkpointIndexPath, true)
	if err != nil {
		return err
	}
	defer parent.Close()
	if info, err := parent.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return errors.New("checkpoint index is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp := ".index-pending.json"
	// Runtime lock owns this temporary projection; interrupted publication is
	// safely rewritten before index rename. Symlinks are never followed.
	if info, err := parent.Lstat(tmp); err == nil && !info.Mode().IsRegular() {
		return errors.New("checkpoint temporary index is not regular")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := parent.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := parent.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if err := parent.Rename(tmp, name); err != nil {
		return err
	}
	if err := workpacket.SyncDirectory(parent); err != nil {
		return err
	}
	// Also remove directories left by interrupted publication; at most the two
	// indexed complete pairs survive a successful publication.
	d, err := parent.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := d.ReadDir(-1)
	err = errors.Join(readErr, d.Close())
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, path := range next.Manifests {
		keep[filepath.Base(filepath.Dir(path))] = true
	}
	for _, entry := range entries {
		if isCheckpointID(entry.Name()) && !keep[entry.Name()] {
			if !entry.IsDir() {
				return errors.New("checkpoint directory is not regular")
			}
			if err := parent.RemoveAll(entry.Name()); err != nil {
				return err
			}
		}
	}
	return workpacket.SyncDirectory(parent)
}

func isCheckpointID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func readCheckpointIndex(root string) (checkpointIndex, error) {
	data, err := workpacket.ReadFile(root, checkpointIndexPath, 4096)
	if err != nil {
		return checkpointIndex{}, err
	}
	var index checkpointIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return index, err
	}
	if index.SchemaVersion != "1.0.0" || len(index.Manifests) < 1 || len(index.Manifests) > 2 {
		return index, errors.New("invalid checkpoint index")
	}
	seen := map[string]bool{}
	for _, path := range index.Manifests {
		id := filepath.Base(filepath.Dir(path))
		if !isCheckpointID(id) || path != ".claude/runtime-checkpoints/"+id+"/manifest.json" || seen[path] {
			return index, errors.New("invalid checkpoint manifest path")
		}
		seen[path] = true
	}
	return index, nil
}

// LatestRuntimeCheckpoint only locates a source; it grants no restore authority.
func LatestRuntimeCheckpoint(root string) (string, error) {
	index, err := readCheckpointIndex(root)
	if err != nil {
		return "", err
	}
	return index.Manifests[0], nil
}

// ReadRuntimeCheckpoint requires the latest complete source and validates its
// exact pair. Older snapshots cannot roll back accepted work or transitions.
func ReadRuntimeCheckpoint(root, path string, validator CandidateValidator) (CheckpointPair, error) {
	latest, err := LatestRuntimeCheckpoint(root)
	if err != nil {
		return CheckpointPair{}, err
	}
	if path != latest {
		return CheckpointPair{}, errors.New("only the latest Runtime checkpoint may be restored")
	}
	data, err := workpacket.ReadFile(root, path, 4096)
	if err != nil {
		return CheckpointPair{}, err
	}
	var manifest RuntimeCheckpoint
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return CheckpointPair{}, err
	}
	dir := strings.TrimSuffix(path, "/manifest.json")
	if manifest.SchemaVersion != "1.0.0" || manifest.StatePath != dir+"/state.json" || manifest.JournalPath != dir+"/events.jsonl" {
		return CheckpointPair{}, errors.New("invalid checkpoint manifest")
	}
	stateBytes, err := workpacket.ReadFile(root, manifest.StatePath, checkpointStateLimit)
	if err != nil {
		return CheckpointPair{}, err
	}
	journalBytes, err := workpacket.ReadFile(root, manifest.JournalPath, checkpointJournalLimit)
	if err != nil {
		return CheckpointPair{}, err
	}
	if sha256Hex(stateBytes) != manifest.StateSHA256 || sha256Hex(journalBytes) != manifest.JournalSHA256 || filepath.Base(dir) != sha256Hex([]byte(manifest.StateSHA256+":"+manifest.JournalSHA256)) {
		return CheckpointPair{}, errors.New("checkpoint fingerprint mismatch")
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return CheckpointPair{}, err
	}
	writer := NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, validator)
	if err := writer.validateCandidate(state); err != nil {
		return CheckpointPair{}, err
	}
	bodies, err := readCheckpointPacketBodies(root, dir, state)
	if err != nil {
		return CheckpointPair{}, err
	}
	bodyContext := withCheckpointPacketBodies(WithPacketBodyRoot(context.Background(), root), bodies)
	if err := ValidateCheckpointIndicesContext(bodyContext, root, state, journalBytes); err != nil {
		return CheckpointPair{}, err
	}
	if err := validateRecoveryPair(state, journalBytes); err != nil {
		return CheckpointPair{}, err
	}
	revision, err := integerField(state, "revision")
	if err != nil {
		return CheckpointPair{}, err
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return CheckpointPair{}, err
	}
	if !hasIssuedIndices(state) || state["runtime_id"] != manifest.RuntimeID || revision != manifest.Revision || generation != manifest.Generation {
		return CheckpointPair{}, errors.New("checkpoint identity mismatch")
	}
	pair := CheckpointPair{PacketBodies: bodies, Manifest: manifest, State: state, StateBytes: stateBytes, JournalBytes: journalBytes}
	if err := ValidateCheckpointRestore(root, pair); err != nil {
		return CheckpointPair{}, err
	}
	return pair, nil
}

// ValidateCheckpointRestore rejects known later or closed facts. Corrupt bytes
// are preserved by the original RR-A quarantine, never used to infer PASS.
func ValidateCheckpointRestore(root string, pair CheckpointPair) error {
	archiveManifests, err := filepath.Glob(filepath.Join(root, ".claude/runtime-archive/*/rollover.json"))
	if err != nil {
		return err
	}
	for _, path := range archiveManifests {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var archive map[string]any
		if json.Unmarshal(data, &archive) != nil {
			return errors.New("unreadable Runtime closure archive")
		}
		if archive["runtime_id"] == pair.Manifest.RuntimeID {
			return errors.New("checkpoint Runtime was closed by an original boundary")
		}
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	currentData, err := os.ReadFile(statePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var current map[string]any
	if err == nil && json.Unmarshal(currentData, &current) == nil {
		if current["runtime_id"] != pair.State["runtime_id"] || !reflect.DeepEqual(current["bound_req"], pair.State["bound_req"]) || !reflect.DeepEqual(current["baseline"], pair.State["baseline"]) {
			return errors.New("checkpoint disagrees with current Runtime identity/baseline/REQ")
		}
		revision, err := integerField(current, "revision")
		if err != nil || revision > pair.Manifest.Revision {
			return errors.New("checkpoint would roll back a newer Runtime")
		}
		if revision == pair.Manifest.Revision && !reflect.DeepEqual(current, pair.State) {
			return errors.New("same-revision Runtime facts differ from checkpoint")
		}
	}
	journal, err := inspectJournalData(pair.JournalBytes)
	if err != nil {
		return err
	}
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	paths, err := journalSegmentPaths(journalPath)
	if err != nil {
		return err
	}
	paths = append(paths, journalPath)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var event map[string]any
			if json.Unmarshal(line, &event) != nil {
				continue
			}
			// Even valid events in an otherwise damaged journal must not be lost.
			id := stringValue(event["event_id"])
			index, ok := journal.EventIndex[id]
			if id != "" && (!ok || !reflect.DeepEqual(event, journal.Events[index])) {
				return errors.New("checkpoint would discard or change a known journal event")
			}
		}
	}
	return nil
}
