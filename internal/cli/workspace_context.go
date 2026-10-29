package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func workspaceDispatch(state map[string]any, a hookctx.AssignmentContext) (workspace.Dispatch, error) {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent["id"] != a.OwnerAgentID {
			continue
		}
		team, _ := agent["team_id"].(string)
		mode, _ := agent["dispatch_mode"].(string)
		if team == "" {
			return workspace.Dispatch{}, fmt.Errorf("registered context owner has no Team")
		}
		dispatch := workspace.Dispatch{
			AssignmentID:           a.AssignmentID,
			Owner:                  a.OwnerAgentID,
			TaskID:                 a.TaskID,
			TeamID:                 team,
			Role:                   a.RoleFamily,
			AgentDefinitionRef:     a.AgentDefinitionRef,
			ManifestRef:            a.ManifestRef,
			DispatchMode:           mode,
			ResponsibilityIDs:      append([]string{}, a.ResponsibilityIDs...),
			AuthorizationSkillRefs: append([]string{}, a.SkillRefs...),
			ReadPaths:              append([]string{}, a.ReadPaths...),
			DeclaredOutputs:        append([]string{}, a.OutputPaths...),
			DependsOn:              append([]string{}, a.DependsOn...),
			DoneWhen:               append([]string{}, a.DoneWhen...),
		}
		record, found, err := assignmentrecord.ForOwner(state, a.OwnerAgentID)
		if err != nil {
			return workspace.Dispatch{}, fmt.Errorf("resolve current canonical Assignment: %w", err)
		}
		projected := a.AssignmentRecord != nil || a.AssignmentRecordSHA256 != "" || a.AssignmentRevision != 0
		if !found {
			if projected {
				return workspace.Dispatch{}, fmt.Errorf("AssignmentContext has no current canonical Runtime Record")
			}
			return dispatch, nil
		}
		if record.Task.ID != a.TaskID {
			return workspace.Dispatch{}, fmt.Errorf("current canonical Assignment is bound to TASK %s, not %s", record.Task.ID, a.TaskID)
		}
		canonical, err := agentcontext.ApplyRecord(dispatch, record)
		if err != nil {
			return workspace.Dispatch{}, err
		}
		sha, err := record.Digest()
		if err != nil {
			return workspace.Dispatch{}, err
		}
		if !projected || a.AssignmentRecord == nil || *a.AssignmentRecord != record.Reference() || a.AssignmentRecordSHA256 != sha || a.AssignmentRevision != record.Revision ||
			dispatch.AssignmentID != canonical.AssignmentID || dispatch.Owner != canonical.Owner || dispatch.TaskID != canonical.TaskID || dispatch.TeamID != canonical.TeamID ||
			dispatch.Role != canonical.Role || dispatch.AgentDefinitionRef != canonical.AgentDefinitionRef || dispatch.ManifestRef != canonical.ManifestRef || dispatch.DispatchMode != canonical.DispatchMode ||
			!slices.Equal(dispatch.ResponsibilityIDs, canonical.ResponsibilityIDs) || !slices.Equal(dispatch.AuthorizationSkillRefs, canonical.AuthorizationSkillRefs) ||
			!slices.Equal(dispatch.ReadPaths, canonical.ReadPaths) || !slices.Equal(dispatch.DeclaredOutputs, canonical.DeclaredOutputs) || !slices.Equal(dispatch.DependsOn, canonical.DependsOn) ||
			!slices.Equal(dispatch.DoneWhen, canonical.DoneWhen) || !slices.Equal(a.WritePaths, canonical.EffectiveWritePaths) || !slices.Equal(a.RequiredChecks, canonical.RequiredChecks) ||
			a.AssignmentRevision != canonical.AssignmentRevision || a.AssignmentRecord == nil || *a.AssignmentRecord != *canonical.AssignmentRecord || a.AssignmentRecordSHA256 != canonical.AssignmentRecordSHA256 {
			return workspace.Dispatch{}, fmt.Errorf("AssignmentContext differs from the current canonical Assignment Record")
		}
		return canonical, nil
	}
	return workspace.Dispatch{}, fmt.Errorf("context owner is not registered")
}

// Formal input selection starts at the assigned TASK and its registered
// dependency closure; shared REQ/design/contracts remain the original chain.
func workspaceContextInputs(root string, state map[string]any, taskID string) ([]string, error) {
	ref, err := fileview.DevelopmentRef(state)
	if err != nil {
		return nil, err
	}
	files, err := fileview.New(root, "refs/heads/"+ref, []fileview.Rule{
		{Path: ".", Source: "git_tree"},
		{Path: ".claude/evidence", Source: "disk"},
	})
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{taskID: true}
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	changed := true
	for changed {
		changed = false
		for _, raw := range tasks {
			t, _ := raw.(map[string]any)
			id, _ := t["id"].(string)
			if !wanted[id] {
				continue
			}
			deps, _ := t["depends_on"].([]any)
			for _, raw := range deps {
				dep, _ := raw.(string)
				if dep != "" && !wanted[dep] {
					wanted[dep] = true
					changed = true
				}
			}
		}
	}
	result := []string{"docs/control/agent-protocol.md"}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	isS5 := lifecycle["state"] == "document_verification" && runtimepkg.PacketEnabled(state)
	formal, err := runtimepkg.CurrentFormalInputDocuments(root, state, files)
	if err != nil {
		if isS5 {
			return nil, fmt.Errorf("context accepted S5 input closure is unverifiable: %w", err)
		}
		return nil, fmt.Errorf("context formal input selection is unverifiable: %w", err)
	}
	for _, document := range formal {
		ref := document.Reference
		if !isS5 {
			id, kind := ref.ID, document.Kind
			if kind == "task" && !wanted[id] {
				continue
			}
			if kind != "req" && kind != "design" && kind != "contract" && kind != "task" {
				continue
			}
		}
		data, err := workpacket.ReadFile(root, ref.Path, 8<<20)
		if err != nil || workpacket.Digest(data) != ref.SHA256 {
			return nil, fmt.Errorf("context formal input is unverifiable: %s", ref.Path)
		}
		committed, err := files.ReadFile(ref.Path)
		if err != nil || workpacket.Digest(committed) != ref.SHA256 {
			return nil, fmt.Errorf("context formal input differs from committed development authority: %s", ref.Path)
		}
		result = append(result, ref.Path)
	}
	if _, err := workpacket.ReadFile(root, result[0], 1<<20); err != nil {
		return nil, fmt.Errorf("installed dispatch protocol is unavailable: %w", err)
	}
	rules := filepath.Join(root, "docs/rules")
	if err := filepath.WalkDir(rules, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("dispatch rule is not a regular file")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, err := workpacket.ReadFile(root, rel, 1<<20); err != nil {
			return err
		}
		result = append(result, rel)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("installed dispatch rule sources unavailable: %w", err)
	}
	if err := files.Verify(); err != nil {
		return nil, err
	}
	return result, nil
}
