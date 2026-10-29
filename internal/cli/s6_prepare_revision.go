package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

var s6RevisionIntentKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

type s6PrepareRevisionInput struct {
	NativeDelivery bool
	BaseCommit     string
	Root           string
	State          map[string]any
	StateRevision  int
	Files          fileview.Reader
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
	if len(input.IntentKey) > 1024 || !s6RevisionIntentKeyPattern.MatchString(input.IntentKey) {
		fmt.Fprintln(stderr, "s6 prepare --revision-intent-key must be a stable non-empty token of at most 1024 characters")
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
	if previous.Task.ID != input.TaskID || previous.RoleFamily != input.Role || previous.OwnerAgentID == input.AgentID {
		fmt.Fprintln(stderr, "S6 revision preparation must retain the registered TASK and Builder role and use a new Agent ID")
		return 1
	}
	previousSHA, err := previous.Digest()
	if err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation Record digest: %v\n", err)
		return 1
	}
	if err := assignmentrecord.VerifySources(previous, func(ref string) ([]byte, error) {
		return s6RevisionSource(input.Root, input.Files, ref)
	}); err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation registered source drift: %v\n", err)
		return 1
	}
	manifestBytes, err := s6RevisionSource(input.Root, input.Files, previous.Manifest.Ref)
	if err != nil || workpacket.Digest(manifestBytes) != previous.Manifest.SHA256 {
		fmt.Fprintf(stderr, "S6 revision preparation original manifest source is unavailable or changed: %v\n", err)
		return 1
	}
	if err := s6RequireCanonicalRework(input, previous, previousSHA); err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation: %v\n", err)
		return 1
	}
	if err := s6RequireFreshRevisionIDs(input.State, previous, input.AgentID, input.PlatformTeamID); err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation: %v\n", err)
		return 1
	}

	nextRevision := previous.Revision + 1
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
	if err != nil {
		fmt.Fprintf(stderr, "S6 revision preparation write draft: %v\n", err)
		return 1
	}
	if err := input.FilesVerify(); err != nil {
		_ = os.Remove(filepath.Join(input.Root, filepath.FromSlash(written)))
		fmt.Fprintf(stderr, "S6 revision preparation source view changed before draft output: %v\n", err)
		return 1
	}

	if input.NativeDelivery {
		if previous.Contract != assignmentrecord.NativeDeliveryContract {
			fmt.Fprintln(stderr, "frozen legacy Assignment cannot be silently migrated; finish or explicitly retire its old execution")
			return 1
		}
		if err := pinNativeS6Source(input.Root, previous.AssignmentID, nextRevision, input.BaseCommit); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		// The exact prior Record and digest are checked again under the original
		// Writer lock. An unrelated Runtime commit does not change this intent.
		snap, err := assignment.Register(input.Root, filepath.Join(input.Root, ".claude/loop-state.json"), filepath.Join(input.Root, ".claude/loop-events.jsonl"), assignment.Request{ExpectedRevision: -1, ManifestPath: filepath.Join(input.Root, written), TaskID: previous.Task.ID, TaskPath: filepath.Join(input.Root, previous.Task.Ref), Revision: &assignment.RevisionRequest{AssignmentID: previous.AssignmentID, IntentKey: input.IntentKey, ExpectedRecord: previous.Reference(), ExpectedRecordSHA256: previousSHA}})
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
	refBytes, err := json.Marshal(previous.Reference())
	if err != nil {
		_ = os.Remove(filepath.Join(input.Root, filepath.FromSlash(written)))
		fmt.Fprintf(stderr, "S6 revision preparation Record reference: %v\n", err)
		return 1
	}
	register := fmt.Sprintf("loop-harness runtime register-workgroup --root %q --manifest %q --task-id %q --task %q --revise-assignment %q --revision-intent-key %q --expected-record-ref %q --expected-record-sha256 %q",
		input.Root, written, previous.Task.ID, previous.Task.Ref, previous.AssignmentID, input.IntentKey, string(refBytes), previousSHA)
	fmt.Fprintf(stdout, "S6 revision manifest draft written (not registered): %s\n", written)
	fmt.Fprintf(stdout, "This preserves the current Assignment contract and uses fresh Workgroup %s, platform Team %s, and Agent %s.\n", workgroupID, input.PlatformTeamID, input.AgentID)
	fmt.Fprintf(stdout, "Next: review this draft, then use the original revision Writer: %s\n", register)
	fmt.Fprintf(stdout, "After the original Writer accepts it, prepare the new execution with: loop-harness runtime workspace prepare --root %q --assignment %q --agent %q\n", input.Root, previous.AssignmentID, input.AgentID)
	return 0
}

func (input s6PrepareRevisionInput) FilesVerify() error {
	if verifier, ok := input.Files.(interface{ Verify() error }); ok {
		return verifier.Verify()
	}
	return nil
}

func s6RequireCanonicalRework(input s6PrepareRevisionInput, record assignmentrecord.Record, digest string) error {
	loaded, err := hookctx.FromSnapshot(input.Root, "", loopruntime.Snapshot{Revision: input.StateRevision, State: input.State})
	if err != nil {
		return fmt.Errorf("load current canonical Assignment projection: %w", err)
	}
	if loaded.PolicyContext.Revision != input.StateRevision || loaded.PolicyContext.RuntimeID != record.RuntimeID || loaded.BaselineGeneration != record.BaselineGeneration {
		return fmt.Errorf("Runtime changed while resolving the current rework entry; refresh S6 advice")
	}
	projected := false
	for _, assignment := range loaded.Assignments {
		if assignment.AssignmentID != record.AssignmentID {
			continue
		}
		projected = assignment.AssignmentRecord != nil && *assignment.AssignmentRecord == record.Reference() && assignment.AssignmentRecordSHA256 == digest
		break
	}
	if !projected {
		return fmt.Errorf("current canonical Assignment is not present in the original Main projection")
	}
	pending, err := integration.Pending(input.Root, loaded)
	if err != nil {
		return fmt.Errorf("read original canonical rework advice: %w", err)
	}
	for _, row := range pending {
		if row.AssignmentID != record.AssignmentID || len(row.ReworkCommand) == 0 {
			continue
		}
		for i := 0; i+1 < len(row.ReworkCommand); i++ {
			if row.ReworkCommand[i] == "--revise-assignment" && row.ReworkCommand[i+1] == record.AssignmentID {
				return nil
			}
		}
	}
	return fmt.Errorf("the original pending projection has no current canonical ReworkCommand for Assignment %s", record.AssignmentID)
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

func s6RevisionSource(root string, files fileview.Reader, ref string) ([]byte, error) {
	if strings.HasPrefix(ref, "gsb-embedded/") {
		return assignmentinputs.Read(root, ref)
	}
	viewBytes, err := files.ReadFile(filepath.Join(root, filepath.FromSlash(ref)))
	if err != nil {
		return nil, err
	}
	diskBytes, err := workpacket.ReadFile(root, ref, 16<<20)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(viewBytes, diskBytes) {
		return nil, fmt.Errorf("formal file view and checkout differ for %s", ref)
	}
	return viewBytes, nil
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
