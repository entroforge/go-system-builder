package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
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

// A checkpoint is reusable only after its actual path is in the original
// Writer's index and corresponds to the same Runtime revision and sequence.
// Attempt components distinguish preserved unknown publication remnants.
func checkpointBelongsToEvent(manifest, baseDir string) bool {
	if !validCheckpointManifestPath(manifest) {
		return false
	}
	dir := strings.TrimSuffix(manifest, "/manifest.json")
	if dir == baseDir {
		return true
	}
	const root = ".claude/runtime-checkpoints/"
	if !strings.HasPrefix(dir, root+"alternate-") {
		return false
	}
	suffix := strings.TrimPrefix(dir, root)
	index := strings.IndexByte(suffix, '/')
	if index < 0 || suffix[index+1:] != strings.TrimPrefix(baseDir, root) {
		return false
	}
	return validCheckpointAlternate(suffix[:index])
}

func validCheckpointAlternate(name string) bool {
	if !strings.HasPrefix(name, "alternate-") || len(name) == len("alternate-") {
		return false
	}
	for _, c := range strings.TrimPrefix(name, "alternate-") {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// claimCheckpointDirectory atomically reserves a new directory. A directory
// left by an interrupted, unindexed attempt is unknown and is never removed,
// compared or reused as evidence of completion.
func claimCheckpointDirectory(ctx context.Context, root, baseDir string) (string, error) {
	const prefix = ".claude/runtime-checkpoints/"
	if !strings.HasPrefix(baseDir, prefix+"cp/") || !validCheckpointManifestPath(baseDir+"/manifest.json") {
		return "", errors.New("invalid checkpoint publication directory")
	}
	storage, _, err := workpacket.OpenParent(root, checkpointIndexPath, true)
	if err != nil {
		return "", fmt.Errorf("open checkpoint storage: %w", err)
	}
	storage.Close()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		dir := baseDir
		if attempt > 0 {
			dir = fmt.Sprintf("%salternate-%d/%s", prefix, attempt, strings.TrimPrefix(baseDir, prefix))
		}
		parent, name, err := workpacket.OpenParent(root, dir, true)
		if errors.Is(err, workpacket.ErrPacketPathOccupied) {
			continue
		}
		if err != nil {
			return "", err
		}
		err = parent.Mkdir(name, 0700)
		if err == nil {
			err = workpacket.SyncDirectory(parent)
		}
		closeErr := parent.Close()
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil || closeErr != nil {
			return "", errors.Join(err, closeErr)
		}
		return dir, nil
	}
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
	// Runtime identity, revision and journal sequence are persisted before this
	// point, so a retry selects the same checkpoint without hashing content.
	encoded := base64.RawURLEncoding.EncodeToString([]byte(stringValue(state["runtime_id"])))
	parts := []string{"cp"}
	for len(encoded) > 120 {
		parts = append(parts, encoded[:120])
		encoded = encoded[120:]
	}
	parts = append(parts, encoded, fmt.Sprintf("r%d-s%d", revision, journal.TailSequence))
	baseDir := ".claude/runtime-checkpoints/" + strings.Join(parts, "/")
	// A previously indexed checkpoint for this exact Runtime event is the
	// original Writer's completed publication. Reuse that fact; an unknown
	// same-named directory is never read or claimed as proof of this event.
	index, err := readCheckpointIndex(s.root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, old := range index.Manifests {
		if !checkpointBelongsToEvent(old, baseDir) {
			continue
		}
		data, err := workpacket.ReadFile(s.root, old, 4096)
		var prior map[string]any
		if err == nil && json.Unmarshal(data, &prior) == nil && prior["schema_version"] == "1.0.0" &&
			stringValue(prior["runtime_id"]) == stringValue(state["runtime_id"]) && checkpointNumber(prior["baseline_generation"], generation) && checkpointNumber(prior["revision"], revision) &&
			stringValue(prior["state_path"]) == strings.TrimSuffix(old, "/manifest.json")+"/state.json" && stringValue(prior["journal_path"]) == strings.TrimSuffix(old, "/manifest.json")+"/events.jsonl" {
			return nil
		}
	}
	var manifestPath string
	for {
		dir, err := claimCheckpointDirectory(s.Context(), s.root, baseDir)
		if err != nil {
			return err
		}
		if err := s.publishCheckpointPacketBodies(state, dir); err != nil {
			if errors.Is(err, errCheckpointPublicationOccupied) {
				continue
			}
			return err
		}
		manifest := RuntimeCheckpoint{SchemaVersion: "1.0.0", RuntimeID: stringValue(state["runtime_id"]), Generation: generation, Revision: revision, StatePath: dir + "/state.json", JournalPath: dir + "/events.jsonl"}
		manifestPath = dir + "/manifest.json"
		manifestBytes, err := jsonDocumentBytes(manifest)
		if err != nil {
			return err
		}
		occupied := false
		for _, asset := range []struct {
			path  string
			data  []byte
			limit int64
		}{{manifest.StatePath, stateBytes, checkpointStateLimit}, {manifest.JournalPath, journalBytes, checkpointJournalLimit}, {manifestPath, manifestBytes, 4096}} {
			created, err := workpacket.PublishNewBounded(s.root, asset.path, asset.data, asset.limit)
			if err != nil {
				return fmt.Errorf("publish checkpoint: %w", err)
			}
			if !created {
				occupied = true
				break
			}
		}
		if !occupied {
			break
		}
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
	// A leftover index temporary may belong to an interrupted or unrelated
	// writer. Allocate our own and never remove an unknown neighbor.
	var tmp string
	var f *os.File
	for attempt := 1; ; attempt++ {
		if err := s.Context().Err(); err != nil {
			return err
		}
		tmp = fmt.Sprintf(".index-pending-%d.json", attempt)
		f, err = parent.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	defer parent.Remove(tmp)
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
	// Retire only checkpoints previously named by the index. Never remove
	// unknown neighboring data; directories containing packet bodies or other
	// files are retained for explicit maintenance rather than recursively erased.
	kept := map[string]bool{}
	for _, manifest := range next.Manifests {
		kept[manifest] = true
	}
	for _, old := range index.Manifests {
		if kept[old] || !validCheckpointManifestPath(old) {
			continue
		}
		pruneSimpleIndexedCheckpoint(s.root, old)
	}

	return workpacket.SyncDirectory(parent)
}

func checkpointNumber(value any, expected int) bool {
	n, err := integerField(map[string]any{"value": value}, "value")
	return err == nil && n == expected
}

// pruneSimpleIndexedCheckpoint is opportunistic: failures do not turn a
// completed business transaction into a failed acceptance. Only a fully
// recognized simple checkpoint is removed; any extra content is preserved.
func pruneSimpleIndexedCheckpoint(root, manifest string) {
	dir := strings.TrimSuffix(manifest, "/manifest.json")
	base, _, err := workpacket.OpenParent(root, manifest, false)
	if err != nil {
		return
	}
	defer base.Close()
	d, err := base.Open(".")
	if err != nil {
		return
	}
	entries, readErr := d.ReadDir(-1)
	_ = d.Close()
	if readErr != nil || len(entries) != 3 {
		return
	}
	allowed := map[string]bool{"state.json": true, "events.jsonl": true, "manifest.json": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] || !entry.Type().IsRegular() {
			return
		}
	}
	for _, entry := range entries {
		if err := base.Remove(entry.Name()); err != nil {
			return
		}
	}
	// Remove the now-empty directory only; nested/unknown data cannot be erased.
	parent, name, err := workpacket.OpenParent(root, dir, false)
	if err != nil {
		return
	}
	defer parent.Close()
	_ = parent.Remove(name)
}

func isCheckpointID(id string) bool {
	if strings.HasPrefix(id, "cp-") {
		for _, c := range id {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
		return len(id) > len("cp-")
	}
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

func validCheckpointManifestPath(manifest string) bool {
	const prefix = ".claude/runtime-checkpoints/"
	if !strings.HasPrefix(manifest, prefix) || path.Clean(manifest) != manifest || !strings.HasSuffix(manifest, "/manifest.json") {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(manifest, prefix), "/manifest.json"), "/")
	if len(parts) > 1 && validCheckpointAlternate(parts[0]) {
		parts = parts[1:]
	}
	if len(parts) == 1 {
		return isCheckpointID(parts[0])
	}
	if len(parts) < 3 || parts[0] != "cp" {
		return false
	}
	for _, part := range parts[1:] {
		if part == "" || len(part) > 120 {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
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
		if !validCheckpointManifestPath(path) || seen[path] {
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

// ReadRuntimeCheckpoint checks the latest available source against retained
// Runtime, journal and responsibility facts.
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
	if err := json.Unmarshal(data, &manifest); err != nil {
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
		currentREQ, _ := current["bound_req"].(map[string]any)
		checkpointREQ, _ := pair.State["bound_req"].(map[string]any)
		currentBaseline, _ := current["baseline"].(map[string]any)
		checkpointBaseline, _ := pair.State["baseline"].(map[string]any)
		if stringValue(current["runtime_id"]) != stringValue(pair.State["runtime_id"]) || stringValue(currentREQ["id"]) != stringValue(checkpointREQ["id"]) || stringValue(currentREQ["path"]) != stringValue(checkpointREQ["path"]) || stringValue(currentREQ["version"]) != stringValue(checkpointREQ["version"]) || fmt.Sprint(currentBaseline["generation"]) != fmt.Sprint(checkpointBaseline["generation"]) {
			return errors.New("checkpoint disagrees with current Runtime identity/baseline/REQ")
		}
		revision, err := integerField(current, "revision")
		if err == nil && revision > pair.Manifest.Revision {
			return errors.New("checkpoint would roll back a newer Runtime")
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
			_, ok := journal.EventIndex[id]
			if id != "" && !ok {
				return errors.New("checkpoint would discard a known journal event")
			}
		}
	}
	return nil
}
