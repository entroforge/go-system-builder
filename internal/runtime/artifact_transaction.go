package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// ImmutableArtifact is an output of a domain mutation, not an alternate way
// to register evidence. The domain writer still owns permission and validation.
type ImmutableArtifact struct {
	Path string
	Data []byte
}

type pendingArtifact struct {
	Path        string `json:"path"`
	StagingPath string `json:"staging_path"`
	SHA256      string `json:"sha256"`
}

type artifactBatch struct {
	entries []pendingArtifact
	durable bool
}

const artifactStageRoot = ".claude/operations/staging"
const artifactManifestAction = "runtime:immutable-artifacts:v2"

func artifactOutputPath(p string) bool {
	return cleanTransactionPath(p) && (strings.HasPrefix(p, ".claude/evidence/") || strings.HasPrefix(p, ".claude/review/plans/") || strings.HasPrefix(p, ".claude/review/repair/") || investigationArtifactPath(p))
}

func investigationArtifactPath(p string) bool {
	return strings.HasPrefix(p, ".claude/review/investigation/contracts/") || strings.HasPrefix(p, ".claude/review/investigation/cases/")
}

func artifactPendingVersion(entries []pendingArtifact) string {
	for _, a := range entries {
		if investigationArtifactPath(a.Path) {
			return "2.2.0"
		}
	}
	for _, a := range entries {
		if strings.HasPrefix(a.Path, ".claude/review/repair/") {
			return "2.1.0"
		}
	}
	return "2.0.0"
}

func pendingArtifactSchema(version string) string {
	switch version {
	case "2.0.0":
		return "runtime-commit-pending-v2.schema.json"
	case "2.1.0":
		return "runtime-commit-pending-v2.1.schema.json"
	case "2.2.0":
		return "runtime-commit-pending-v2.2.schema.json"
	}
	return ""
}

func cleanTransactionPath(p string) bool {
	return p != "" && p == path.Clean(p) && !strings.Contains(p, "\\") && !strings.HasPrefix(p, "/") && p != ".." && !strings.HasPrefix(p, "../")
}

func validStagingPath(p string) bool {
	if !cleanTransactionPath(p) || !strings.HasPrefix(p, artifactStageRoot+"/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(p, artifactStageRoot+"/"), "/")
	if len(parts) != 2 || len(parts[0]) != 32 || !strings.HasSuffix(parts[1], ".data") {
		return false
	}
	_, err := hex.DecodeString(parts[0])
	return err == nil
}

// Root operations prevent a concurrent symlink swap from escaping the project.
// Reject existing symlinks as well: even in-project aliases must not retarget
// the protected control plane or make two canonical artifact names equivalent.
func checkTransactionPath(root *os.Root, p string) error {
	if !cleanTransactionPath(p) {
		return fmt.Errorf("unsafe artifact path %q", p)
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact path contains a symlink: %s", p)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("artifact parent is not a directory: %s", p)
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return fmt.Errorf("artifact is not a regular file: %s", p)
		}
	}
	return nil
}

func (s *Store) prepareMutationArtifacts(m Mutation) (Mutation, func(), error) {
	if err := s.context().Err(); err != nil {
		return m, func() {}, err
	}
	noop := func() {}
	if len(m.Artifacts) == 0 {
		return m, noop, nil
	}
	if m.BoundaryReset {
		return m, noop, errors.New("boundary reset cannot publish artifacts")
	}
	if err := s.requireCandidateValidator(); err != nil {
		return m, noop, err
	}
	state, err := s.read()
	if err != nil {
		return m, noop, err
	}
	if err := s.validateWriteAuthority(state); err != nil {
		return m, noop, err
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return m, noop, err
	}
	defer root.Close()
	seen := map[string]bool{}
	for _, artifact := range m.Artifacts {
		if !artifactOutputPath(artifact.Path) || seen[artifact.Path] {
			return m, noop, fmt.Errorf("invalid/duplicate immutable artifact path %q", artifact.Path)
		}
		seen[artifact.Path] = true
		if err := checkTransactionPath(root, artifact.Path); err != nil {
			return m, noop, err
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return m, noop, err
	}
	stage := artifactStageRoot + "/" + hex.EncodeToString(random[:])
	if err := checkTransactionPath(root, stage+"/probe.data"); err != nil {
		return m, noop, err
	}
	if err := root.MkdirAll(artifactStageRoot, 0700); err != nil {
		return m, noop, err
	}
	if err := root.Mkdir(stage, 0700); err != nil {
		return m, noop, err
	}
	batch := &artifactBatch{}
	cleanup := func() {
		if !batch.durable {
			if r, err := os.OpenRoot(s.root); err == nil {
				defer r.Close()
				_ = r.RemoveAll(stage)
			}
		}
	}
	for i, artifact := range m.Artifacts {
		if err := s.context().Err(); err != nil {
			cleanup()
			return m, noop, err
		}
		p := fmt.Sprintf("%s/%d.data", stage, i)
		file, err := root.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			cleanup()
			return m, noop, err
		}
		_, writeErr := file.Write(artifact.Data)
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			cleanup()
			return m, noop, err
		}
		batch.entries = append(batch.entries, pendingArtifact{Path: artifact.Path, StagingPath: p, SHA256: sha256Hex(artifact.Data)})
	}
	// All newly created directory entries must survive alongside the marker.
	for dir := stage; dir != "."; dir = path.Dir(dir) {
		if err := syncDir(filepath.Join(s.root, filepath.FromSlash(dir))); err != nil {
			cleanup()
			return m, noop, err
		}
	}
	m.artifactBatch = batch
	m.Artifacts = nil
	return m, cleanup, nil
}

func artifactManifestDigest(entries []pendingArtifact) string {
	data, _ := json.Marshal(entries)
	return sha256Hex(data)
}

func bindArtifactManifest(event map[string]any, entries []pendingArtifact) {
	rows, _ := event["action_results"].([]any)
	// buildJournalEvent uses []map[string]any until the first JSON round trip.
	if typed, ok := event["action_results"].([]map[string]any); ok {
		for _, row := range typed {
			rows = append(rows, row)
		}
	}
	event["action_results"] = append(rows, map[string]any{"id": artifactManifestAction, "result": "committed", "detail": artifactManifestDigest(entries)})
}

func artifactReferenced(value any, p, hash string) bool {
	switch v := value.(type) {
	case map[string]any:
		if v["path"] == p && v["sha256"] == hash {
			return true
		}
		if strings.HasPrefix(p, ".claude/review/investigation/contracts/") && v["repair_contract_ref"] == p && v["repair_contract_sha256"] == hash {
			return true
		}
		if strings.HasPrefix(p, ".claude/review/repair/") {
			for _, pair := range [][2]string{{"plan_ref", "plan_sha256"}, {"plan_report_ref", "plan_report_sha256"}, {"result_ref", "result_sha256"}, {"changeset_ref", "changeset_sha256"}, {"impact_ref", "impact_sha256"}, {"handoff_ref", "handoff_sha256"}, {"review_plan_seed_ref", "review_plan_seed_sha256"}} {
				if v[pair[0]] == p && v[pair[1]] == hash {
					return true
				}
			}
		}
		for _, child := range v {
			if artifactReferenced(child, p, hash) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if artifactReferenced(child, p, hash) {
				return true
			}
		}
	}
	return false
}

func validateArtifactManifest(pending commitPending) error {
	if pending.SchemaVersion == "1.0.0" && len(pending.Artifacts) == 0 {
		data, _ := json.Marshal(pending.JournalEvent["action_results"])
		var rows []map[string]any
		if err := json.Unmarshal(data, &rows); err != nil {
			return err
		}
		for _, row := range rows {
			if row["id"] == artifactManifestAction {
				return errors.New("legacy pending commit cannot discard an artifact manifest")
			}
		}
		return nil
	}
	if pendingArtifactSchema(pending.SchemaVersion) == "" || len(pending.Artifacts) == 0 {
		return errors.New("pending artifact bundle requires supported schema 2.x and nonempty artifacts")
	}
	encoded, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes(pendingArtifactSchema(pending.SchemaVersion), encoded); err != nil {
		return err
	}
	seen, stages := map[string]bool{}, map[string]bool{}
	for _, a := range pending.Artifacts {
		decoded, err := hex.DecodeString(a.SHA256)
		if !artifactOutputPath(a.Path) || !validStagingPath(a.StagingPath) || len(decoded) != 32 || err != nil || seen[a.Path] || stages[a.StagingPath] {
			return fmt.Errorf("invalid pending artifact manifest entry %q", a.Path)
		}
		seen[a.Path], stages[a.StagingPath] = true, true
		if !artifactReferenced(pending.State, a.Path, a.SHA256) {
			return fmt.Errorf("pending artifact %s has no exact state reference", a.Path)
		}
	}
	data, _ := json.Marshal(pending.JournalEvent["action_results"])
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		return err
	}
	count := 0
	for _, row := range rows {
		if row["id"] == artifactManifestAction {
			count++
			if row["result"] != "committed" || row["detail"] != artifactManifestDigest(pending.Artifacts) {
				return errors.New("pending artifact manifest differs from its journal anchor")
			}
		}
	}
	if count != 1 {
		return errors.New("pending artifact manifest requires one journal anchor")
	}
	return nil
}

func (s *Store) preflightArtifactPublication(entries []pendingArtifact, state map[string]any) error {
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, a := range entries {
		if !artifactReferenced(state, a.Path, a.SHA256) {
			return fmt.Errorf("artifact %s has no exact candidate state reference", a.Path)
		}
		if err := checkTransactionPath(root, a.Path); err != nil {
			return err
		}
		if _, err := root.Lstat(a.Path); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return fmt.Errorf("immutable artifact %s already exists; inspect its Runtime operation before retrying; never delete historical evidence", a.Path)
		}
		if err := checkTransactionPath(root, a.StagingPath); err != nil {
			return err
		}
		data, err := root.ReadFile(a.StagingPath)
		if err != nil || sha256Hex(data) != a.SHA256 {
			return fmt.Errorf("staged artifact %s is missing or changed", a.Path)
		}
	}
	return nil
}

// Link publishes complete fsynced bytes with no-overwrite semantics. A crash
// cannot leave a partial canonical file. An existing exact file is accepted
// only while completing a validated pending bundle, never as fresh admission.
func (s *Store) publishPendingArtifacts(entries []pendingArtifact) error {
	if len(entries) == 0 {
		return nil
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	// Validate the whole bundle before publishing any missing member.
	for _, a := range entries {
		if err := s.context().Err(); err != nil {
			return err
		}
		for _, p := range []string{a.Path, a.StagingPath} {
			if err := checkTransactionPath(root, p); err != nil {
				return err
			}
		}
		data, err := root.ReadFile(a.Path)
		if err == nil {
			if sha256Hex(data) != a.SHA256 {
				return fmt.Errorf("published artifact %s conflicts with pending commit", a.Path)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		data, err = root.ReadFile(a.StagingPath)
		if err != nil || sha256Hex(data) != a.SHA256 {
			return fmt.Errorf("pending artifact %s has missing or changed staging", a.Path)
		}
	}
	for _, a := range entries {
		if err := s.context().Err(); err != nil {
			return err
		}
		if _, err := root.Lstat(a.Path); errors.Is(err, os.ErrNotExist) {
			if err := root.MkdirAll(path.Dir(a.Path), 0755); err != nil {
				return err
			}
			if err := root.Link(a.StagingPath, a.Path); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("publish immutable artifact %s: %w", a.Path, err)
			}
		}
		data, err := root.ReadFile(a.Path)
		if err != nil || sha256Hex(data) != a.SHA256 {
			return fmt.Errorf("published artifact %s failed fingerprint verification", a.Path)
		}
		for dir := path.Dir(a.Path); dir != "."; dir = path.Dir(dir) {
			if err := syncDir(filepath.Join(s.root, filepath.FromSlash(dir))); err != nil {
				return err
			}
		}
	}
	return nil
}

// Only called after both durable halves and marker removal completed. Cleanup
// failure leaves disposable private staging; it does not undo a commit.
func (s *Store) cleanupPublishedArtifacts(entries []pendingArtifact) {
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return
	}
	defer root.Close()
	for _, a := range entries {
		if validStagingPath(a.StagingPath) {
			_ = root.Remove(a.StagingPath)
			_ = root.Remove(path.Dir(a.StagingPath))
		}
	}
}
