package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type s6PrepareRevisionInput struct {
	BaseCommit     string
	Root           string
	State          map[string]any
	StateRevision  int
	Files          fileview.Reader
	Selected       s6SelectedSources
	IntentKey      string
	AssignmentID   string
	TaskID         string
	Role           string
	AgentID        string
	PlatformTeamID string
	Output         string
	DoneWhen       []string
	REQID          string
}

func runS6PrepareRevision(input s6PrepareRevisionInput, stdout, stderr io.Writer) int {
	if input.Output != "" || len(input.DoneWhen) > 0 {
		fmt.Fprintln(stderr, "S6 revision preparation uses the existing registered manifest contract; --out and --done-when are unavailable on this route")
		return 2
	}
	if input.AssignmentID == "" || input.TaskID == "" || input.REQID == "" {
		fmt.Fprintln(stderr, "S6 revision preparation TASK or Assignment identity is incomplete")
		return 1
	}

	runtimeID, generation, _, err := assignmentrecord.CurrentRuntimeBinding(input.State)
	if err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation Runtime identity: %v\n", err)
		return 1
	}
	records, err := assignmentrecord.RecordsFromState(input.State)
	if err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation Assignment registry: %v\n", err)
		return 1
	}
	previous, found, err := assignmentrecord.FindRecord(records, runtimeID, generation, input.AssignmentID)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("no current canonical Assignment Record exists for %s", input.AssignmentID)
		}
		fmt.Fprintf(stderr, "S6 revision preparation: %v\n", err)
		return 1
	}
	if previous.Task.ID != input.TaskID || previous.RoleFamily != input.Role {
		fmt.Fprintln(stderr, "S6 revision preparation must retain the registered TASK and Builder role")
		return 1
	}
	pending, err := s6CurrentCanonicalRework(input, previous)
	if err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation: %v\n", err)
		return 1
	}
	if !pending {
		if previous.Revision > 1 && previous.RevisionIssuance != nil {
			fmt.Fprintln(stdout, "No new rework is recorded; the existing original revision remains current.")
			if err := prepareNativeS6Brief(input.Root, input.State, previous, "", stdout); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		}
		fmt.Fprintln(stderr, "S6 revision preparation: no current canonical ReworkCommand; preserve the original responsibility and resolve the actual pending work")
		return 1
	}
	input.IntentKey = fmt.Sprintf("s6-rework-r%d", previous.Revision)
	nextRevision := previous.Revision + 1
	input.AgentID = fmt.Sprintf("native-%s-r%d", input.TaskID, nextRevision)
	input.PlatformTeamID = fmt.Sprintf("engineering-%s-r%d", input.TaskID, nextRevision)
	if previous.OwnerAgentID == input.AgentID {
		fmt.Fprintln(stderr, "S6 revision preparation conflicts with the current Builder identity")
		return 1
	}
	manifestBytes, err := s6RevisionSource(input.Root, input.Files, previous.Manifest.Ref)
	if err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation cannot read original manifest %s: %v\n", previous.Manifest.Ref, err)
		return 1
	}
	if err := s6RequireFreshRevisionIDs(input.State, previous, input.AgentID, input.PlatformTeamID); err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation: %v\n", err)
		return 1
	}

	manifestRef := filepath.ToSlash(filepath.Join(".claude", "workgroups", input.REQID, input.TaskID, fmt.Sprintf("manifest-s6-r%d.json", nextRevision)))
	workgroupID := fmt.Sprintf("workgroup-build-%s-r%d", input.TaskID, nextRevision)
	manifestID := fmt.Sprintf("team-manifest-build-%s-r%d", input.TaskID, nextRevision)
	if s6RuntimeHasEntity(input.State, "teams", "id", workgroupID) {
		fmt.Fprintf(stderr, "generated S6 revision Workgroup ID %s already exists; inspect the original Runtime before preparing another revision\n", workgroupID)
		return 1
	}
	if err := input.FilesVerify(); err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation source view changed: %v\n", err)
		return 1
	}

	draftBytes, err := s6CloneRevisionManifest(manifestBytes, manifestID, workgroupID, input.PlatformTeamID, input.AgentID, nextRevision)
	if err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation manifest: %v\n", err)
		return 1
	}
	if err := team.ValidateBytes(draftBytes); err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation manifest structure: %v\n", err)
		return 1
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", draftBytes); err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation manifest schema: %v\n", err)
		return 1
	}
	written, err := s6WriteDraft(input.Root, input.REQID, input.TaskID, manifestRef, draftBytes)
	created := err == nil
	if err != nil {
		if recoveryErr := s6ResumeRevisionDraft(input, previous, manifestRef, manifestID, workgroupID); recoveryErr != nil {
			fmt.Fprintf(stderr, "S6 revision preparation preserves existing draft %s: %v\n", manifestRef, recoveryErr)
			return 1
		}
		written = manifestRef
	}
	if err := input.FilesVerify(); err != nil {
		if created {
			_ = os.Remove(filepath.Join(input.Root, filepath.FromSlash(written)))
		}
		fmt.Fprintf(stderr, "S6 revision preparation source view changed before draft output: %v\n", err)
		return 1
	}

	// The original Writer resolves concurrent attempts against this predecessor.
	snap, err := assignment.Register(input.Root, filepath.Join(input.Root, ".claude/loop-state.json"), filepath.Join(input.Root, ".claude/loop-events.jsonl"), assignment.Request{ExpectedRevision: -1, ManifestPath: filepath.Join(input.Root, written), TaskID: previous.Task.ID, TaskPath: filepath.Join(input.Root, previous.Task.Ref), SelectedTaskBytes: input.Selected.Task, SelectedBoard: input.Selected.Board, Revision: &assignment.RevisionRequest{AssignmentID: previous.AssignmentID, IntentKey: input.IntentKey, ExpectedRecord: previous.Reference()}})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("S6 revision registration", err))
		return 1
	}
	current, found, err := assignmentrecord.ForOwner(snap.State, input.AgentID)
	if err != nil || !found {
		fmt.Fprintf(stderr, "native revision registration missing: %v\n", err)
		return 1
	}
	if err := prepareNativeS6Brief(input.Root, snap.State, current, input.BaseCommit, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// Reuse only an existing draft that names this same issued responsibility.
// Its professional material remains untouched and the original Writer owns it.
func s6ResumeRevisionDraft(input s6PrepareRevisionInput, previous assignmentrecord.Record, path, manifestID, workgroupID string) error {
	data, err := workpacket.ReadFile(input.Root, path, 1<<20)
	if err != nil {
		return err
	}
	var draft s6ManifestDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		return fmt.Errorf("original draft is not readable: %w", err)
	}
	if draft.RuntimeID != previous.RuntimeID || draft.BaselineGeneration != previous.BaselineGeneration || draft.ManifestID != manifestID || draft.WorkgroupID != workgroupID || draft.PlatformTeamID != input.PlatformTeamID || len(draft.Assignments) != 1 {
		return fmt.Errorf("original draft responsibility is not the current correction")
	}
	a := draft.Assignments[0]
	if a.AssignmentID != previous.AssignmentID || a.AgentID != input.AgentID || a.RoleFamily != previous.RoleFamily {
		return fmt.Errorf("original draft Assignment or owner differs from the current correction")
	}
	for _, document := range draft.Documents {
		if document.ID == previous.Task.ID && document.Path == previous.Task.Ref {
			return nil
		}
	}
	return fmt.Errorf("original draft does not identify the current TASK")
}

func (input s6PrepareRevisionInput) FilesVerify() error {
	if verifier, ok := input.Files.(interface{ Verify() error }); ok {
		return verifier.Verify()
	}
	return nil
}

func s6CurrentCanonicalRework(input s6PrepareRevisionInput, record assignmentrecord.Record) (bool, error) {
	loaded, err := hookctx.FromSnapshot(input.Root, "", loopruntime.Snapshot{Revision: input.StateRevision, State: input.State})
	if err != nil {
		return false, fmt.Errorf("load current canonical Assignment projection: %w", err)
	}
	if loaded.PolicyContext.Revision != input.StateRevision || loaded.PolicyContext.RuntimeID != record.RuntimeID || loaded.BaselineGeneration != record.BaselineGeneration {
		return false, fmt.Errorf("Runtime changed while resolving the current rework entry; refresh S6 advice")
	}
	projected := false
	for _, assignment := range loaded.Assignments {
		if assignment.AssignmentID != record.AssignmentID {
			continue
		}
		projected = assignment.AssignmentRecord != nil && *assignment.AssignmentRecord == record.Reference()
		break
	}
	if !projected {
		return false, fmt.Errorf("current canonical Assignment is not present in the original Main projection")
	}
	pending, err := integration.Pending(input.Root, loaded)
	if err != nil {
		return false, fmt.Errorf("read original canonical rework advice: %w", err)
	}
	for _, row := range pending {
		if row.AssignmentID != record.AssignmentID || len(row.ReworkCommand) == 0 {
			continue
		}
		for i := 0; i+1 < len(row.ReworkCommand); i++ {
			if row.ReworkCommand[i] == "--revise-assignment" && row.ReworkCommand[i+1] == record.AssignmentID {
				return true, nil
			}
		}
	}
	return false, nil
}

func s6RequireFreshRevisionIDs(state map[string]any, previous assignmentrecord.Record, agentID, platformTeamID string) error {
	entities, _ := state["entities"].(map[string]any)
	for _, raw := range s6Rows(entities["agents"]) {
		if s6String(raw["id"]) == agentID {
			return fmt.Errorf("revision Agent ID %s already exists; choose a fresh actual Agent", agentID)
		}
	}
	for _, raw := range s6Rows(entities["teams"]) {
		if s6String(raw["id"]) == previous.TeamID {
			continue
		}
		if s6String(raw["platform_team_id"]) == platformTeamID {
			return fmt.Errorf("revision platform Team ID %s already exists; choose the new platform Team for this revision", platformTeamID)
		}
	}
	return nil
}

func s6CloneRevisionManifest(source []byte, manifestID, workgroupID, platformTeamID, agentID string, revision int) ([]byte, error) {
	var manifest map[string]any
	if err := json.Unmarshal(source, &manifest); err != nil {
		return nil, fmt.Errorf("decode current registered manifest: %w", err)
	}
	assignments, ok := manifest["assignments"].([]any)
	if !ok || len(assignments) != 1 {
		return nil, fmt.Errorf("current canonical S6 manifest must contain exactly one Assignment")
	}
	assignment, ok := assignments[0].(map[string]any)
	if !ok || s6String(assignment["assignment_id"]) == "" || s6String(assignment["agent_id"]) == "" {
		return nil, fmt.Errorf("current canonical S6 manifest Assignment identity is incomplete")
	}
	// New machine references use the current output shape, while preserving
	// unknown professional extensions. The original manifest is never rewritten.
	if value, exists := manifest["documents"]; exists {
		manifest["documents"] = s6RevisionReferenceOutput(value)
	}
	projectProfile := func(profile map[string]any) {
		for _, field := range []string{"dependencies", "fixtures"} {
			if value, exists := profile[field]; exists {
				profile[field] = s6RevisionReferenceOutput(value)
			}
		}
	}
	for _, profile := range s6Rows(assignment["check_profiles"]) {
		projectProfile(profile)
	}
	if closing, ok := assignment["closing_contract"].(map[string]any); ok {
		for _, check := range s6Rows(closing["worker_checks"]) {
			if profile, ok := check["profile"].(map[string]any); ok {
				projectProfile(profile)
			}
		}
	}
	manifest["manifest_id"] = manifestID
	manifest["version"] = fmt.Sprintf("v1.0.%d", revision-1)
	manifest["platform_team_id"] = platformTeamID
	manifest["workgroup_id"] = workgroupID
	manifest["status"] = "planned"
	assignment["agent_id"] = agentID
	assignment["status"] = "planned"
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// s6RevisionReferenceOutput is only the new revision Writer's output boundary.
// It adds no source decoding or qualification and never walks author prose.
func s6RevisionReferenceOutput(raw any) any {
	refs, ok := raw.([]any)
	if !ok {
		return raw
	}
	current := make([]any, len(refs))
	for i, raw := range refs {
		ref, ok := raw.(map[string]any)
		if !ok {
			current[i] = raw
			continue
		}
		fields := make(map[string]any, len(ref))
		for key, value := range ref {
			if key != "sha256" {
				fields[key] = value
			}
		}
		current[i] = fields
	}
	return current
}

func s6RevisionSource(root string, files fileview.Reader, ref string) ([]byte, error) {
	if strings.HasPrefix(ref, "gsb-embedded/") {
		return assignmentinputs.Read(root, ref)
	}
	return files.ReadFile(filepath.Join(root, filepath.FromSlash(ref)))
}

func s6RuntimeHasEntity(state map[string]any, group, field, value string) bool {
	entities, _ := state["entities"].(map[string]any)
	for _, row := range s6Rows(entities[group]) {
		if s6String(row[field]) == value {
			return true
		}
	}
	return false
}

func s6Rows(value any) []map[string]any {
	rows, _ := value.([]any)
	result := make([]map[string]any, 0, len(rows))
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil {
			result = append(result, row)
		}
	}
	return result
}
