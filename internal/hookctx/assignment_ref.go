package hookctx

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"os"
	"path/filepath"
	"strings"
)

// Explicit prompt references are authority, not suggestions. If they are
// invalid, never fall back to a different manifest for the same task.
func assignmentManifest(root, taskID, ref string) (string, *workgroupManifest, string) {
	return assignmentManifestWithReader(root, taskID, ref, os.ReadFile, func() (string, *workgroupManifest) {
		return loadWorkgroupManifest(root, taskID)
	})
}

func assignmentManifestWithReader(root, taskID, ref string, read func(string) ([]byte, error), fallback func() (string, *workgroupManifest)) (string, *workgroupManifest, string) {
	if strings.TrimSpace(ref) == "" {
		path, manifest := fallback()
		return path, manifest, ""
	}
	path, fragment, hasFragment := strings.Cut(ref, "#")
	if hasFragment && (fragment == "" || strings.Contains(fragment, "#")) {
		return "", nil, ""
	}
	path = strings.ReplaceAll(path, `\`, "/")
	if path == "" || strings.Contains(path, ":") || strings.HasPrefix(path, "/") {
		return "", nil, ""
	}
	path = filepath.Clean(filepath.FromSlash(path))
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", nil, ""
	}
	absRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, ""
	}
	absRoot, err = filepath.Abs(absRoot)
	if err != nil {
		return "", nil, ""
	}
	abs, err := filepath.EvalSymlinks(filepath.Join(absRoot, path))
	if err != nil {
		return "", nil, ""
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, ""
	}
	data, err := read(abs)
	if err != nil {
		return "", nil, ""
	}
	var manifest workgroupManifest
	if json.Unmarshal(data, &manifest) != nil {
		return "", nil, ""
	}
	return filepath.ToSlash(path), &manifest, fragment
}

func selectManifestAssignment(manifest *workgroupManifest, agentID, taskID, fragment string) *workgroupAssignment {
	// Older fixtures omit documents. Where task subjects are declared, the
	// referenced manifest must actually cover this runtime agent/task pair.
	hasTask, matchedTask := false, false
	for _, doc := range manifest.Documents {
		if doc.Kind == "task" {
			hasTask = true
			if doc.ID == taskID {
				matchedTask = true
			}
		}
	}
	if hasTask && !matchedTask {
		return nil
	}
	var selected *workgroupAssignment
	for i := range manifest.Assignments {
		a := &manifest.Assignments[i]
		if a.AssignmentID == "" || (fragment != "" && a.AssignmentID != fragment) {
			continue
		}
		if a.AgentID != "" && a.AgentID != agentID {
			continue
		}
		if a.AgentID == "" && fragment == "" && len(manifest.Assignments) != 1 {
			continue
		}
		if selected != nil {
			return nil
		}
		selected = a
	}
	return selected
}

func fillAssignmentRow(row *AssignmentContext, a workgroupAssignment) {
	row.AssignmentID = a.AssignmentID
	row.RoleFamily = a.RoleFamily
	row.AgentDefinitionRef = a.AgentDefinitionRef
	row.ResponsibilityIDs = append(row.ResponsibilityIDs, a.ResponsibilityID)
	row.WritePaths = assignmentWritePaths(a.WritePaths, a.OutputPaths, a.Scope)
	row.ReadPaths = append([]string{}, a.ReadPaths...)
	row.OutputPaths = append([]string{}, a.OutputPaths...)
	row.SkillRefs = append([]string{}, a.SkillRefs...)
	row.DependsOn = append([]string{}, a.DependsOn...)
	row.RequiredChecks = append([]string(nil), a.RequiredChecks...)
	row.CheckProfiles = append([]checkexec.Profile(nil), a.CheckProfiles...)
	row.IntegrationCheckMode = a.IntegrationCheckMode
	row.DoneWhen = append([]string(nil), a.DoneWhen...)
	row.ReportStatus = a.Status
	applyAssignmentCoords(row, a.WorktreePath, a.Branch, a.TargetBranch)
}

// Registered manifest pointers take precedence over historical task-shaped paths.
func assignmentManifestForRefs(root, taskID string, refs registeredManifestRef) (string, *workgroupManifest, string) {
	return assignmentManifestForRefsWithReader(root, taskID, refs, os.ReadFile, func() (string, *workgroupManifest) {
		return loadWorkgroupManifest(root, taskID)
	})
}

func assignmentManifestForRefsWithReader(root, taskID string, refs registeredManifestRef, read func(string) ([]byte, error), fallback func() (string, *workgroupManifest)) (string, *workgroupManifest, string) {
	for _, ref := range []string{refs.PromptRef, refs.TeamRef} {
		if _, formal := registeredManifestPath(root, ref); formal {
			return assignmentManifestWithReader(root, taskID, ref, read, fallback)
		}
	}
	if refs.PromptRef != "" {
		return assignmentManifestWithReader(root, taskID, refs.PromptRef, read, fallback)
	}
	return assignmentManifestWithReader(root, taskID, "", read, fallback)
}
