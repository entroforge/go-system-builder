package cli

// plan_handoff.go owns the narrow trust boundary between an official
// PostToolUse(SendMessage) payload and the authority Runtime. A Worker may
// report from its registered worktree, but the authority must never read an
// arbitrary cwd, copy a worker control plane, or let a root-relative stale
// file win over the file the Worker actually sent.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const planHandoffDir = ".claude/evidence/plan-handoffs"

type planAssignmentRecord struct {
	ID           string
	WorktreePath string
}

// Empty cwd is the legacy authority-root transport. An explicit cwd that
// resolves to the authority root has the same semantics; only a different
// checkout is required to prove a registered worker handoff.
func planReportUsesAuthorityRoot(root, cwd string) bool {
	if strings.TrimSpace(cwd) == "" {
		return true
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	rootReal, rootErr := filepath.EvalSymlinks(rootAbs)
	cwdReal, cwdErr := filepath.EvalSymlinks(cwdAbs)
	if rootErr == nil && cwdErr == nil {
		if samePath(rootReal, cwdReal) {
			return true
		}
		// Main may invoke the hook from a subdirectory of the authority
		// checkout. Identify that checkout through Git rather than treating
		// every path beneath root as trusted (an in-root worktree must still
		// prove its registered Assignment).
		if pathWithin(rootReal, cwdReal) {
			out, gitErr := exec.Command("git", "-C", cwdAbs, "rev-parse", "--show-toplevel").Output()
			if gitErr == nil {
				top, topErr := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
				if topErr == nil && samePath(rootReal, top) {
					return true
				}
			}
		}
		return false
	}
	return samePath(rootAbs, cwdAbs)
}

// importWorkerPlanReport reads a PLAN_REPORT only from the registered
// assignment worktree that owns the sender. It returns the normalized,
// authority-root-relative content-addressed path. The caller must pass that
// returned path to both validation/registration and AutoAdvanceToWorking.
func importWorkerPlanReport(root string, snapshot runtime.Snapshot, request policy.Input, agentID, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("plan_ref is required")
	}
	if strings.TrimSpace(request.CWD) == "" {
		return "", fmt.Errorf("worker cwd is required for plan_report handoff")
	}
	assignment, err := registeredPlanAssignment(root, snapshot, request, agentID)
	if err != nil {
		return "", err
	}
	workerRoot, cwd, err := validateRegisteredWorkerCWD(root, assignment.WorktreePath, request.CWD)
	if err != nil {
		return "", err
	}
	source, err := resolveWorkerPlanPath(workerRoot, cwd, ref)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read worker plan_ref %q: %w", ref, err)
	}
	if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data); err != nil {
		return "", fmt.Errorf("worker plan_ref schema: %w", err)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		return "", fmt.Errorf("decode worker plan_ref %q: %w", ref, err)
	}
	if message["message_type"] != "plan_report" {
		return "", fmt.Errorf("worker plan_ref must be a plan_report")
	}
	if messageAgent, _ := message["agent_id"].(string); messageAgent != agentID {
		return "", fmt.Errorf("worker plan_ref agent_id %q does not match Agent %s", messageAgent, agentID)
	}
	if runtimeID, _ := snapshot.State["runtime_id"].(string); runtimeID != "" {
		if messageRuntime, _ := message["runtime_id"].(string); messageRuntime != runtimeID {
			return "", fmt.Errorf("worker plan_ref runtime_id does not match the current runtime")
		}
	}
	if planAssignment, _ := message["assignment_id"].(string); planAssignment != assignment.ID {
		return "", fmt.Errorf("worker plan_ref Assignment %q is not the registered Assignment %s", planAssignment, assignment.ID)
	}
	return importPlanReportBytes(root, data)
}

// registeredPlanAssignment resolves the assignment from the Runtime's
// registered Agent/Team/manifest projection. It intentionally does not infer
// ownership from cwd, branch names, or a sole worker.
func registeredPlanAssignment(root string, snapshot runtime.Snapshot, request policy.Input, agentID string) (planAssignmentRecord, error) {
	entities, _ := snapshot.State["entities"].(map[string]any)
	var agent map[string]any
	for _, raw := range anySlice(entities["agents"]) {
		candidate, _ := raw.(map[string]any)
		if stringValue(candidate["id"]) == agentID {
			agent = candidate
			break
		}
	}
	if agent == nil {
		return planAssignmentRecord{}, fmt.Errorf("Agent %s has no registered assignment", agentID)
	}
	teamID := stringValue(agent["team_id"])
	if teamID == "" {
		return planAssignmentRecord{}, fmt.Errorf("Agent %s has no registered workgroup", agentID)
	}
	var teamRow map[string]any
	for _, raw := range anySlice(entities["teams"]) {
		candidate, _ := raw.(map[string]any)
		if stringValue(candidate["id"]) == teamID {
			teamRow = candidate
			break
		}
	}
	if teamRow == nil {
		return planAssignmentRecord{}, fmt.Errorf("workgroup %s is not registered for Agent %s", teamID, agentID)
	}
	manifestRef := stringValue(teamRow["manifest_ref"])
	if manifestRef == "" {
		return planAssignmentRecord{}, fmt.Errorf("workgroup %s has no manifest_ref", teamID)
	}
	manifestPath, err := safeAuthorityPath(root, manifestRef)
	if err != nil {
		return planAssignmentRecord{}, fmt.Errorf("registered workgroup manifest rejected: %w", err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return planAssignmentRecord{}, fmt.Errorf("read registered workgroup manifest: %w", err)
	}
	if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		return planAssignmentRecord{}, fmt.Errorf("registered workgroup manifest schema: %w", err)
	}
	if err := team.ValidateBytes(manifestBytes); err != nil {
		return planAssignmentRecord{}, fmt.Errorf("registered workgroup manifest semantics: %w", err)
	}
	var manifest struct {
		Assignments []map[string]any `json:"assignments"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return planAssignmentRecord{}, fmt.Errorf("decode registered workgroup manifest: %w", err)
	}
	requestedID := stringValue(request.ToolInput["assignment_id"])
	var match planAssignmentRecord
	for _, row := range manifest.Assignments {
		if stringValue(row["agent_id"]) != agentID {
			continue
		}
		id := stringValue(row["assignment_id"])
		if id == "" || (requestedID != "" && id != requestedID) {
			continue
		}
		if match.ID != "" {
			return planAssignmentRecord{}, fmt.Errorf("Agent %s has multiple registered assignments; assignment_id is required", agentID)
		}
		match = planAssignmentRecord{ID: id, WorktreePath: stringValue(row["worktree_path"])}
	}
	if match.ID == "" {
		return planAssignmentRecord{}, fmt.Errorf("no registered Assignment for Agent %s", agentID)
	}
	if match.WorktreePath == "" {
		match.WorktreePath = registeredAssignmentSidecarPath(root, match.ID)
	}
	if match.WorktreePath == "" {
		return planAssignmentRecord{}, fmt.Errorf("registered Assignment %s has no worktree_path", match.ID)
	}
	return match, nil
}

func registeredAssignmentSidecarPath(root, assignmentID string) string {
	if assignmentID == "" || assignmentID == "." || assignmentID == ".." || filepath.Base(assignmentID) != assignmentID || strings.ContainsRune(assignmentID, rune(filepath.Separator)) || strings.ContainsRune(assignmentID, 0) {
		return ""
	}
	for _, path := range []string{
		filepath.Join(root, ".claude", "assignments", assignmentID+".json"),
		filepath.Join(root, ".claude", "assignments", assignmentID, "manifest.json"),
	} {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		safe, err := safeAuthorityPath(root, filepath.ToSlash(rel))
		if err != nil {
			continue
		}
		data, err := os.ReadFile(safe)
		if err != nil {
			continue
		}
		var row struct {
			WorktreePath string `json:"worktree_path"`
		}
		if json.Unmarshal(data, &row) == nil && row.WorktreePath != "" {
			return row.WorktreePath
		}
	}
	return ""
}

func validateRegisteredWorkerCWD(root, registeredWorker, requestedCWD string) (string, string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve authority root: %w", err)
	}
	workerPath := registeredWorker
	if !filepath.IsAbs(workerPath) {
		workerPath = filepath.Join(rootAbs, filepath.FromSlash(workerPath))
	}
	workerAbs, err := filepath.Abs(workerPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve registered worker: %w", err)
	}
	if err := workspace.ValidateCheckout(rootAbs, workerAbs); err != nil {
		return "", "", fmt.Errorf("registered worker rejected: %w", err)
	}
	if info, err := os.Lstat(workerAbs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("registered worker symlink is not allowed: %s", registeredWorker)
	}
	workerReal, err := filepath.EvalSymlinks(workerAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolve registered worker: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolve authority root: %w", err)
	}
	if samePath(workerReal, rootReal) {
		return "", "", fmt.Errorf("registered worker must be distinct from the authority root")
	}
	cwdAbs, err := filepath.Abs(requestedCWD)
	if err != nil {
		return "", "", fmt.Errorf("resolve worker cwd: %w", err)
	}
	cwdReal, err := filepath.EvalSymlinks(cwdAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolve worker cwd: %w", err)
	}
	if !pathWithin(workerReal, cwdReal) {
		return "", "", fmt.Errorf("worker cwd %s is outside registered Assignment worker %s", requestedCWD, registeredWorker)
	}
	if err := rejectSymlinkComponents(workerReal, cwdAbs); err != nil {
		return "", "", fmt.Errorf("worker cwd rejected: %w", err)
	}
	return workerReal, cwdReal, nil
}

func resolveWorkerPlanPath(workerRoot, cwd, ref string) (string, error) {
	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, filepath.FromSlash(path))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve worker plan_ref: %w", err)
	}
	if err := rejectSymlinkComponents(workerRoot, abs); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("read worker plan_ref %q: %w", ref, err)
	}
	if !pathWithin(workerRoot, resolved) {
		return "", fmt.Errorf("worker plan_ref %q escapes the registered worker", ref)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("read worker plan_ref %q: %w", ref, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("worker plan_ref %q is not a regular file", ref)
	}
	return resolved, nil
}

func rejectSymlinkComponents(base, target string) error {
	base, err := filepath.Abs(base)
	if err != nil {
		return fmt.Errorf("resolve worker boundary: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve worker plan_ref: %w", err)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("worker plan_ref %q escapes the registered worker", target)
	}
	current := base
	for _, part := range splitPath(rel) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("inspect worker plan_ref %q: %w", target, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("worker plan_ref %q uses a symlink, which is not allowed", target)
		}
	}
	return nil
}

func importPlanReportBytes(root string, data []byte) (string, error) {
	digest := sha256.Sum256(data)
	name := hex.EncodeToString(digest[:]) + ".json"
	rel := filepath.ToSlash(filepath.Join(planHandoffDir, name))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve plan handoff root: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve plan handoff root: %w", err)
	}
	dir := filepath.Join(rootReal, filepath.FromSlash(planHandoffDir))
	if err := rejectSymlinkComponents(rootReal, dir); err != nil {
		return "", fmt.Errorf("plan handoff store rejected: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create plan handoff store: %w", err)
	}
	if err := rejectSymlinkComponents(rootReal, dir); err != nil {
		return "", fmt.Errorf("plan handoff store rejected: %w", err)
	}
	dest := filepath.Join(rootReal, filepath.FromSlash(rel))
	if info, err := os.Lstat(dest); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("content-addressed plan handoff is a symlink: %s", rel)
		}
		existing, readErr := os.ReadFile(dest)
		if readErr != nil {
			return "", fmt.Errorf("read existing content-addressed plan handoff: %w", readErr)
		}
		if !bytes.Equal(existing, data) {
			return "", fmt.Errorf("content-addressed plan handoff collision: %s", rel)
		}
		if err := os.Chmod(dest, 0o444); err != nil {
			return "", fmt.Errorf("freeze content-addressed plan handoff: %w", err)
		}
		return rel, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect content-addressed plan handoff: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".plan-handoff-*")
	if err != nil {
		return "", fmt.Errorf("stage content-addressed plan handoff: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write content-addressed plan handoff: %w", err)
	}
	if err := tmp.Chmod(0o444); err != nil {
		tmp.Close()
		return "", fmt.Errorf("freeze content-addressed plan handoff: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close content-addressed plan handoff: %w", err)
	}
	if err := os.Link(tmpName, dest); err != nil {
		if !os.IsExist(err) {
			return "", fmt.Errorf("publish content-addressed plan handoff: %w", err)
		}
		info, statErr := os.Lstat(dest)
		if statErr != nil {
			return "", fmt.Errorf("inspect raced content-addressed plan handoff: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("content-addressed plan handoff is a symlink: %s", rel)
		}
		existing, readErr := os.ReadFile(dest)
		if readErr != nil || !bytes.Equal(existing, data) {
			return "", fmt.Errorf("content-addressed plan handoff collision: %s", rel)
		}
	}
	return rel, nil
}

func normalizedPlanReportInput(input map[string]any, ref string) map[string]any {
	next := make(map[string]any, len(input)+2)
	for key, value := range input {
		next[key] = value
	}
	next["plan_ref"] = ref
	if _, ok := next["plan_path"]; ok {
		next["plan_path"] = ref
	}
	return next
}

func anySlice(value any) []any {
	values, _ := value.([]any)
	return values
}

func splitPath(path string) []string {
	if path == "." || path == "" {
		return nil
	}
	return strings.FieldsFunc(path, func(r rune) bool { return r == filepath.Separator })
}

func samePath(left, right string) bool {
	left, _ = filepath.Abs(left)
	right, _ = filepath.Abs(right)
	return filepath.Clean(left) == filepath.Clean(right)
}

func safeAuthorityPath(root, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("empty authority path")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	path := ref
	if !filepath.IsAbs(path) {
		// Resolve relative authority refs from the canonical checkout. This
		// keeps a symlink alias for the repository root from making an
		// otherwise in-bound path look like it escaped the root.
		path = filepath.Join(rootReal, filepath.FromSlash(path))
	} else {
		path, err = filepath.Abs(path)
		if err != nil {
			return "", err
		}
		// An absolute ref may have been supplied through the same root alias
		// as --root. Rebase that lexical prefix onto rootReal before checking
		// components, while preserving absolute paths outside the alias for
		// the containment check below.
		if rel, relErr := filepath.Rel(rootAbs, path); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			path = filepath.Join(rootReal, rel)
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !pathWithin(rootReal, resolved) {
		return "", fmt.Errorf("path %q escapes authority root", ref)
	}
	if err := rejectSymlinkComponents(rootReal, path); err != nil {
		return "", err
	}
	return path, nil
}
