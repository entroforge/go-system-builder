package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/projectconventions"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type nativeAssignmentContext struct {
	CheckProfiles       []checkexec.Profile        `json:"check_profiles,omitempty"`
	OwnedCheckCommands  []string                   `json:"owned_check_commands,omitempty"`
	Conventions         *projectconventions.Bundle `json:"conventions,omitempty"`
	SourceRoot          string                     `json:"source_root"`
	AgentState          string                     `json:"agent_state"`
	Version             string                     `json:"schema_version"`
	RuntimeID           string                     `json:"runtime_id"`
	Generation          int                        `json:"baseline_generation"`
	Dispatch            agentcontext.Assignment    `json:"dispatch"`
	Selection           agentcontext.Selection     `json:"context_selection"`
	Inputs              map[string]string          `json:"frozen_inputs"`
	ReadingOrder        []string                   `json:"input_read_order"`
	WritePaths          []string                   `json:"product_write_paths"`
	Checks              []string                   `json:"required_checks"`
	Schema              agentcontext.Reference     `json:"plan_schema"`
	PlanDraft           map[string]any             `json:"plan_draft,omitempty"`
	PlanQuestions       map[string]string          `json:"plan_questions,omitempty"`
	PlanPath            string                     `json:"plan_draft_path,omitempty"`
	ResultDraft         map[string]any             `json:"result_draft,omitempty"`
	ResultQuestions     map[string]string          `json:"result_questions,omitempty"`
	ResultPath          string                     `json:"result_path,omitempty"`
	ResultDraftEditable bool                       `json:"result_draft_editable,omitempty"`
	WorkPacketRef       string                     `json:"work_packet_ref,omitempty"`
	AnswerPath          string                     `json:"answer_path,omitempty"`
	Instruction         string                     `json:"instruction"`
}

// projectAssignmentContext publishes a read-only projection after validating
// its current original authority. It never writes Runtime, activation or a
// professional answer. Existing workspace executions use their own sources.
func projectAssignmentContext(root string, snap runtime.Snapshot, a hookctx.AssignmentContext) (agentcontext.Reference, error) {
	state := snap.State
	if !workstate.SupportsAssignments(state["schema_version"]) {
		return agentcontext.Reference{}, fmt.Errorf("legacy Runtime has no new assignment context")
	}
	if err := fileview.ValidateAuthority(root, state); err != nil {
		return agentcontext.Reference{}, err
	}
	record, recordBound, err := hookctx.ResolveAssignmentRecordProjection(root, state, a)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	b, err := workspace.Decode(state)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	if b != nil {
		for _, e := range b.Executions {
			if e.AgentID == a.OwnerAgentID || e.AssignmentID == a.AssignmentID {
				return agentcontext.Reference{}, fmt.Errorf("use the original independent execution, never a Main context fallback")
			}
		}
	}
	d, err := workspaceDispatch(state, a)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	entities, _ := state["entities"].(map[string]any)
	var agent, team map[string]any
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row["id"] == d.Owner {
			agent = row
		}
	}
	teams, _ := entities["teams"].([]any)
	for _, raw := range teams {
		row, _ := raw.(map[string]any)
		if row["id"] == d.TeamID {
			team = row
		}
	}
	var currentS5Packets []workpacket.Packet
	if runtime.PacketEnabled(state) {
		currentS5Packets, err = runtime.S5Packets(root, state, d.Owner)
		if err != nil {
			return agentcontext.Reference{}, err
		}
		if team != nil && team["kind"] == "document_verifier" {
			current := false
			for _, packet := range currentS5Packets {
				if packet.Binding.AssignmentID == d.AssignmentID {
					current = true
					break
				}
			}
			if !current {
				return agentcontext.Reference{}, fmt.Errorf("original S5 responsibility is no longer current; Main owns its original rework/retirement route")
			}
		}
	}
	if agent == nil || team == nil || agent["role"] != d.Role || agent["definition_ref"] != d.AgentDefinitionRef || workstate.AgentActivity(agent) == "queued" || workstate.AgentActivity(agent) == "stopped" || workstate.AgentActivity(agent) == "blocked" {
		return agentcontext.Reference{}, fmt.Errorf("assignment context requires the original current Agent/Team/role")
	}
	prompt, _ := agent["prompt_ref"].(string)
	manifestPath, fragment, _ := strings.Cut(prompt, "#")
	if fragment != d.AssignmentID || manifestPath != d.ManifestRef || team["manifest_ref"] != manifestPath {
		return agentcontext.Reference{}, fmt.Errorf("assignment context manifest differs from original Agent/Team references")
	}
	manifestBytes, err := workpacket.ReadFile(root, manifestPath, 2<<20)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		return agentcontext.Reference{}, err
	}
	var manifest struct {
		RuntimeID   string                 `json:"runtime_id"`
		Generation  int                    `json:"baseline_generation"`
		WorkgroupID string                 `json:"workgroup_id"`
		Documents   []workpacket.Reference `json:"documents"`
		Assignments []struct {
			ID    string `json:"assignment_id"`
			Owner string `json:"agent_id"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return agentcontext.Reference{}, err
	}
	generation := workspace.Generation(state)
	if manifest.RuntimeID != state["runtime_id"] || manifest.Generation != generation || manifest.WorkgroupID != d.TeamID {
		return agentcontext.Reference{}, fmt.Errorf("assignment manifest Runtime/baseline/Team changed")
	}
	matches := 0
	for _, row := range manifest.Assignments {
		if row.ID == d.AssignmentID && row.Owner == d.Owner {
			matches++
		}
	}
	if matches != 1 {
		return agentcontext.Reference{}, fmt.Errorf("assignment is not uniquely registered to this owner")
	}
	paths, err := workspaceContextInputs(root, state, d.TaskID)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	selection, err := agentcontext.Select(root, d.Role, d.ResponsibilityIDs, d.AuthorizationSkillRefs)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	inputs := map[string]string{manifestPath: workpacket.Digest(manifestBytes)}
	if recordBound {
		for _, source := range record.ContextSources {
			if strings.HasPrefix(source.Ref, "gsb-embedded/") || strings.Contains(source.Ref, "#") {
				continue
			}
			if err := bindContextInput(inputs, source.Ref, source.SHA256); err != nil {
				return agentcontext.Reference{}, fmt.Errorf("canonical Assignment source has conflicting frozen hashes: %s", source.Ref)
			}
		}
	}
	for _, subject := range manifest.Documents {
		data, err := workpacket.ReadFile(root, subject.Path, 8<<20)
		if err != nil || workpacket.Digest(data) != subject.SHA256 {
			return agentcontext.Reference{}, fmt.Errorf("registered manifest document is unavailable or changed: %s", subject.Path)
		}
		if err := bindContextInput(inputs, subject.Path, subject.SHA256); err != nil {
			return agentcontext.Reference{}, fmt.Errorf("registered manifest document conflicts with a frozen input: %s", subject.Path)
		}
	}
	paths = append(paths, d.AgentDefinitionRef)
	for _, path := range paths {
		data, err := workpacket.ReadFile(root, path, 8<<20)
		if err != nil {
			return agentcontext.Reference{}, err
		}
		if err := bindContextInput(inputs, path, workpacket.Digest(data)); err != nil {
			return agentcontext.Reference{}, fmt.Errorf("registered assignment source conflicts with a frozen input: %s", path)
		}
	}
	if err := checkexec.ValidateProfiles(a.CheckProfiles, a.RequiredChecks, ""); err != nil {
		return agentcontext.Reference{}, err
	}
	for _, p := range a.CheckProfiles {
		if _, err := p.ActualEnvironment(); err != nil {
			return agentcontext.Reference{}, err
		}
		for _, ref := range append(append([]checkexec.FileRef{}, p.Dependencies...), p.Fixtures...) {
			data, err := checkexec.ReadProfileFile(root, ref.Path)
			if err != nil || checkexec.Hash(data) != ref.SHA256 {
				return agentcontext.Reference{}, fmt.Errorf("native profile source changed: %s", ref.Path)
			}
			if err := bindContextInput(inputs, ref.Path, ref.SHA256); err != nil {
				return agentcontext.Reference{}, fmt.Errorf("native profile source conflicts with a frozen input: %s", ref.Path)
			}
		}
	}
	for _, skill := range selection.Selected {
		if err := bindContextInput(inputs, skill.Source, skill.SHA256); err != nil {
			return agentcontext.Reference{}, fmt.Errorf("selected Skill source conflicts with a frozen input: %s", skill.Source)
		}
	}
	var issuedReview *workpacket.Packet
	planned := append([]string{}, a.WritePaths...)
	sourceRoot, err := workspace.Canonical(root)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	agentProjection, err := workstate.AgentProjection(agent, fmt.Sprint(state["schema_version"]))
	if err != nil {
		return agentcontext.Reference{}, err
	}
	agentState := string(agentProjection.Status)
	c := nativeAssignmentContext{CheckProfiles: a.CheckProfiles, OwnedCheckCommands: nativeOwnedCheckCommands(sourceRoot, d), SourceRoot: sourceRoot, AgentState: agentState, Version: "1.1.0", RuntimeID: workspace.RuntimeID(state), Generation: generation, Dispatch: d, Selection: selection, Inputs: inputs, WritePaths: append([]string{}, a.WritePaths...), Checks: append([]string{}, a.RequiredChecks...), Instruction: "Read the registered assignment and actual source files in input_read_order, then installed context_selection.selected Skill paths with their SHA256. This is shadow advice, not activation or permissions. If the original Agent is not yet working, fill professional fields yourself and use the original PLAN_REPORT SendMessage(plan_ref) or agent-begin path. If already working, retain the original accepted plan/activation; this draft is not a request to submit a second plan. Existing approval and domain gates still apply."}
	if runtime.PacketEnabled(state) {
		files, err := conventionFiles(root, state)
		if err != nil {
			return agentcontext.Reference{}, err
		}
		for _, p := range currentS5Packets {
			if p.Binding.AssignmentID != d.AssignmentID {
				continue
			}
			registered, ok, err := runtime.FindWorkPacket(state, p.ID)
			if err != nil {
				return agentcontext.Reference{}, err
			}
			if !ok || !reflect.DeepEqual(p, registered) {
				return agentcontext.Reference{}, fmt.Errorf("current S5 packet has not been issued by the original Writer")
			}
			if err := runtime.ValidateWorkPacket(root, state, p, files); err != nil {
				return agentcontext.Reference{}, err
			}
			for _, subject := range p.Inputs {
				if err := bindContextInput(inputs, subject.Path, subject.SHA256); err != nil {
					return agentcontext.Reference{}, fmt.Errorf("S5 subject conflicts with a frozen assignment input: %s", subject.Path)
				}
			}
			issuedReview = &p
			c.WorkPacketRef, c.AnswerPath = p.ViewPath, p.AnswerPath
			planned = []string{p.AnswerPath}
			c.WritePaths = []string{}
			if err := bindContextInput(inputs, p.ViewPath, workpacket.Digest(mustPacketBytes(p))); err != nil {
				return agentcontext.Reference{}, err
			}
			c.Instruction += " Read work_packet_ref and fill only answer in answer_path after original activation. The original save receipt determines your completion; Main owns peer work and system followup."
		}
	}
	c.Conventions, err = contextConventions(root, state, a, issuedReview)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	if err := mergeConventionInputs(root, c.Conventions, inputs); err != nil {
		return agentcontext.Reference{}, err
	}
	if err := verifyConventionView(root, state, c.Conventions, inputs); err != nil {
		return agentcontext.Reference{}, err
	}
	occurredAt, _ := agent["updated_at"].(string)
	if _, err := time.Parse(time.RFC3339Nano, occurredAt); err != nil {
		return agentcontext.Reference{}, fmt.Errorf("registered Agent timestamp is unavailable")
	}
	c.PlanDraft, c.PlanQuestions = agentcontext.PlanTemplate(d, c.RuntimeID, occurredAt, planned)
	if c.PlanDraft != nil {
		c.PlanPath = fmt.Sprintf(".claude/evidence/assignment-plan/%s/g%d/%s.json", workpacket.Digest([]byte(c.RuntimeID)), generation, workpacket.Digest([]byte(d.AssignmentID+"\x00"+d.Owner)))
	}
	c.ResultDraft, c.ResultQuestions = agentcontext.ResultTemplate(d, c.RuntimeID, occurredAt)
	if c.ResultDraft != nil {
		c.ResultPath = fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/a%d/completion.json", c.RuntimeID, generation, d.Owner, d.AssignmentRevision)
		c.ResultDraftEditable = true
		if _, hasClosing := c.ResultDraft["closing_assessment"]; hasClosing {
			c.Instruction += " After the current accepted PLAN_REPORT and actual TASK work, edit result_path as the canonical S6 Builder Result. Fill only the five null professional fields using result_questions and the full agent-message schema. Map every frozen DoneWhen condition through closing_assessment and accepted PLAN assertion indexes; the assessment is your professional judgment, not a runner receipt or PASS. Preserve message_id for an exact replay; choose a new unique message_id for a changed answer. A submitted Result is not an integration receipt."
		} else {
			c.Instruction += " After the current accepted PLAN_REPORT and actual TASK work, edit result_path as the canonical S6 Builder Result. Fill only the four null professional fields using result_questions and the full agent-message schema. Preserve message_id for an exact replay; choose a new unique message_id for a changed answer. A submitted Result is not a PASS or an integration receipt."
		}
	}
	schemaBytes, err := schema.ReadAsset("agent-message.schema.json")
	if err != nil {
		return agentcontext.Reference{}, err
	}
	c.Schema = agentcontext.Reference{Path: ".claude/assignment-context/schemas/agent-message-" + workpacket.Digest(schemaBytes) + ".json", SHA256: workpacket.Digest(schemaBytes)}
	c.ReadingOrder = agentcontext.InputReadingOrder(inputs, "")
	// Re-read every source before publishing; a projection cannot make a
	// moving or missing source ready. Do not publish the editable plan yet.
	if err := verifyContextInputs(root, inputs); err != nil {
		return agentcontext.Reference{}, err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return agentcontext.Reference{}, err
	}
	data = append(data, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", data); err != nil {
		return agentcontext.Reference{}, err
	}
	if err := publishExactContext(root, c.Schema.Path, schemaBytes); err != nil {
		return agentcontext.Reference{}, err
	}
	path := fmt.Sprintf(".claude/assignment-context/native/%s/g%d/%s.json", workpacket.Digest([]byte(c.RuntimeID)), generation, workpacket.Digest(data))
	if err := publishExactContext(root, path, data); err != nil {
		return agentcontext.Reference{}, err
	}
	if err := verifyContextInputs(root, inputs); err != nil {
		return agentcontext.Reference{}, err
	}
	latest, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil || latest.Revision != snap.Revision || !reflect.DeepEqual(latest.State, state) {
		return agentcontext.Reference{}, fmt.Errorf("Runtime changed during assignment projection; retry from original authority: %w", runtime.ErrStaleRevision)
	}
	if c.PlanDraft != nil {
		draft, err := json.MarshalIndent(c.PlanDraft, "", "  ")
		if err != nil {
			return agentcontext.Reference{}, err
		}
		if _, err := workpacket.PublishBounded(root, c.PlanPath, append(draft, '\n'), 2<<20); err != nil {
			return agentcontext.Reference{}, err
		}
	}
	if c.ResultDraft != nil {
		draft, err := json.MarshalIndent(c.ResultDraft, "", "  ")
		if err != nil {
			return agentcontext.Reference{}, err
		}
		// Keep the owner-edited answer if this deterministic projection is
		// prepared again; the context itself remains the immutable source.
		published, err := workpacket.PublishBounded(root, c.ResultPath, append(draft, '\n'), 2<<20)
		if err != nil {
			return agentcontext.Reference{}, err
		}
		if err := agentcontext.ValidateResultDraft(c.ResultDraft, published); err != nil {
			return agentcontext.Reference{}, fmt.Errorf("editable native Result does not match its frozen Record tuple: %w", err)
		}
	}
	return agentcontext.Reference{Path: path, SHA256: workpacket.Digest(data)}, nil
}

func mustPacketBytes(p workpacket.Packet) []byte {
	data, _ := json.MarshalIndent(p, "", "  ")
	return append(data, '\n')
}

func nativeOwnedCheckCommands(root string, dispatch agentcontext.Assignment) []string {
	if dispatch.AssignmentRecord == nil || dispatch.AssignmentRecordSHA256 == "" || dispatch.ClosingContract == nil || len(dispatch.ClosingContract.WorkerChecks) == 0 {
		return nil
	}
	commands := make([]string, 0, len(dispatch.ClosingContract.WorkerChecks))
	for _, check := range dispatch.ClosingContract.WorkerChecks {
		if check.Index < 0 || check.Command == "" || check.Profile.Command != check.Command || check.Profile.Location != "worker" || check.Profile.CheckIndex != check.Index {
			return nil
		}
		commands = append(commands, policy.NativeOwnedCheckInvocation(root, dispatch.Owner, check.Index))
	}
	return commands
}

func verifyContextInputs(root string, inputs map[string]string) error {
	for path, hash := range inputs {
		data, err := workpacket.ReadFile(root, path, 8<<20)
		if err != nil || workpacket.Digest(data) != hash {
			return fmt.Errorf("assignment context source changed or is unavailable: %s", path)
		}
	}
	return nil
}

func bindContextInput(inputs map[string]string, path, sha256 string) error {
	if previous, exists := inputs[path]; exists && previous != sha256 {
		return fmt.Errorf("input %s already has a different frozen SHA", path)
	}
	inputs[path] = sha256
	return nil
}

func publishExactContext(root, path string, data []byte) error {
	existing, err := workpacket.PublishBounded(root, path, data, 2<<20)
	if err != nil {
		return err
	}
	if !bytes.Equal(existing, data) {
		return fmt.Errorf("existing assignment context asset differs; preserve it for Main recovery: %s", path)
	}
	return nil
}

func assignmentContextLifecycle(root string, input policy.Input, assignments []hookctx.AssignmentContext) string {
	if input.Event != "SubagentStart" && input.Event != "SessionStart" {
		return ""
	}
	owner := input.EffectiveAgentID()
	if owner == "" || submission.CompletedOwner(root, owner) {
		return ""
	}
	// No guessing from role names: a view requires the actual registered actor.
	var found *hookctx.AssignmentContext
	for i := range assignments {
		if assignments[i].OwnerAgentID == owner {
			if found != nil {
				return ""
			}
			found = &assignments[i]
		}
	}
	if found == nil {
		return ""
	}
	snap, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil || !workstate.SupportsAssignments(snap.State["schema_version"]) {
		return ""
	}
	b, err := workspace.Decode(snap.State)
	if err != nil {
		return ""
	}
	if b != nil {
		for _, e := range b.Executions {
			if e.AgentID == owner || e.AssignmentID == found.AssignmentID {
				return ""
			}
		}
	}
	ref, err := currentAssignmentContext(root, snap, *found)
	if err != nil {
		if strings.HasPrefix(err.Error(), "original S5 responsibility is no longer current;") {
			return err.Error() + ". No new plan or source view was issued."
		}
		return "Assignment context unavailable; Main must repair the original source: " + err.Error()
	}
	projection, err := workstate.AgentProjection(contextAgent(snap.State, owner), fmt.Sprint(snap.State["schema_version"]))
	if err != nil {
		return "Assignment progress unavailable; Main must restore the original registered context."
	}
	return "Assignment sources: first Read " + ref.Path + "; sha256=" + ref.SHA256 + ". Live Agent status=" + string(projection.Status) + "; follow the original plan/activation. Captured agent_state is historical; shadow grants no permissions."
}

// Unregistered examples remain projection candidates. Only an exact current
// registered manifest can attach a view that describes a real dispatch.
func attachAssignmentContexts(root, manifestPath string, data []byte, requests []team.ReadbackRequest) error {
	snap, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return nil
	}
	if !workstate.SupportsAssignments(snap.State["schema_version"]) {
		return nil
	}
	var manifest struct {
		RuntimeID   string                 `json:"runtime_id"`
		WorkgroupID string                 `json:"workgroup_id"`
		Documents   []workpacket.Reference `json:"documents"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.RuntimeID != snap.State["runtime_id"] {
		return nil
	}
	entities, _ := snap.State["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	registered := false
	for _, raw := range teams {
		row, _ := raw.(map[string]any)
		if row["id"] == manifest.WorkgroupID {
			if row["manifest_ref"] != filepath.ToSlash(filepath.Clean(manifestPath)) {
				return fmt.Errorf("launch manifest differs from registered Team source")
			}
			registered = true
		}
	}
	if !registered {
		return nil
	}
	actual, err := workpacket.ReadFile(root, manifestPath, 2<<20)
	if err != nil || !bytes.Equal(actual, data) {
		return fmt.Errorf("registered launch manifest changed")
	}
	for i := range requests {
		for _, document := range requests[i].Documents {
			matched := false
			for _, subject := range manifest.Documents {
				if subject.ID == document.ID && subject.Path == document.Path && subject.Version == document.Version && subject.SHA256 == document.SHA256 {
					matched = true
				}
			}
			if !matched {
				return fmt.Errorf("registered readback document differs from manifest source: %s", document.Path)
			}
		}
		loaded, err := hookctx.LoadFull(root, requests[i].AgentID)
		if err != nil {
			return err
		}
		var found *hookctx.AssignmentContext
		for j := range loaded.Assignments {
			row := &loaded.Assignments[j]
			if row.OwnerAgentID == requests[i].AgentID && row.ManifestRef == manifestPath {
				if found != nil {
					return fmt.Errorf("registered launch owner is ambiguous")
				}
				found = row
			}
		}
		if found == nil || found.TaskID != requests[i].TaskID {
			return fmt.Errorf("registered launch owner/TASK does not match current Assignment")
		}
		ref, err := currentAssignmentContext(root, snap, *found)
		if err != nil {
			return err
		}
		requests[i].AssignmentContextRef = &ref
	}
	return nil
}
