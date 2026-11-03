package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/projectconventions"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
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
	inputs := map[string]string{manifestPath: manifestPath}
	if recordBound {
		for _, source := range record.ContextSources {
			if strings.HasPrefix(source.Ref, "gsb-embedded/") || strings.Contains(source.Ref, "#") {
				continue
			}
			inputs[source.Ref] = source.Ref
		}
	}
	for _, subject := range manifest.Documents {
		if _, err := workpacket.ReadFile(root, subject.Path, 8<<20); err != nil {
			return agentcontext.Reference{}, fmt.Errorf("registered manifest document is unavailable: %s", subject.Path)
		}
		inputs[subject.Path] = subject.Path
	}
	paths = append(paths, d.AgentDefinitionRef)
	for _, path := range paths {
		if _, err := workpacket.ReadFile(root, path, 8<<20); err != nil {
			return agentcontext.Reference{}, err
		}
		inputs[path] = path
	}
	if err := checkexec.ValidateProfiles(a.CheckProfiles, a.RequiredChecks, ""); err != nil {
		return agentcontext.Reference{}, err
	}
	for _, p := range a.CheckProfiles {
		if _, err := p.ActualEnvironment(); err != nil {
			return agentcontext.Reference{}, err
		}
		for _, ref := range append(append([]checkexec.FileRef{}, p.Dependencies...), p.Fixtures...) {
			if _, err := checkexec.ReadProfileFile(root, ref.Path); err != nil {
				return agentcontext.Reference{}, fmt.Errorf("native profile source unavailable: %s", ref.Path)
			}
			inputs[ref.Path] = ref.Path
		}
	}
	for _, skill := range selection.Selected {
		// The registered value is the source path itself, so the replay check
		// stays a path-level existence judgment on the declared source.
		inputs[skill.Source] = skill.Source
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
	c := nativeAssignmentContext{CheckProfiles: copyContextProfiles(a.CheckProfiles), OwnedCheckCommands: nativeOwnedCheckCommands(sourceRoot, d), SourceRoot: sourceRoot, AgentState: agentState, Version: "1.1.0", RuntimeID: workspace.RuntimeID(state), Generation: generation, Dispatch: copyContextDispatch(d), Selection: selection, Inputs: inputs, WritePaths: append([]string{}, a.WritePaths...), Checks: append([]string{}, a.RequiredChecks...), Instruction: "Read the registered assignment, the actual source files in input_read_order and the selected Skill paths. Complete your assigned professional work and save the original answer at its provided path; the original consumer handles reception and follow-up. Existing approval and domain responsibilities still apply."}
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
				inputs[subject.Path] = subject.Path
			}
			issuedReview = &p
			c.WorkPacketRef, c.AnswerPath = p.ViewPath, p.AnswerPath
			planned = []string{p.AnswerPath}
			c.WritePaths = []string{}
			inputs[p.ViewPath] = p.ViewPath
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
		c.PlanPath = fmt.Sprintf(".claude/evidence/assignment-plan/%s/g%d/%s/%s/plan.json", contextPathID(c.RuntimeID), generation, contextPathID(d.AssignmentID), contextPathID(d.Owner))
	}
	c.ResultDraft, c.ResultQuestions = agentcontext.ResultTemplate(d, c.RuntimeID, occurredAt)
	if c.ResultDraft != nil {
		c.ResultPath = fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/a%d/completion.json", c.RuntimeID, generation, d.Owner, d.AssignmentRevision)
		c.ResultDraftEditable = true
		if _, hasClosing := c.ResultDraft["closing_assessment"]; hasClosing {
			c.Instruction += " After the current accepted PLAN_REPORT and actual TASK work, edit result_path as the canonical S6 Builder Result. Fill only the five null professional fields using result_questions and the full agent-message schema. Map every frozen DoneWhen condition through closing_assessment and accepted PLAN assertion indexes; the assessment is your professional judgment, not a runner receipt or PASS. A submitted Result is not an integration receipt."
		} else {
			c.Instruction += " After the current accepted PLAN_REPORT and actual TASK work, edit result_path as the canonical S6 Builder Result. Fill only the four null professional fields using result_questions and the full agent-message schema. A submitted Result is not a PASS or an integration receipt."
		}
	}
	schemaBytes, err := schema.ReadAsset("agent-message.schema.json")
	if err != nil {
		return agentcontext.Reference{}, err
	}
	contextBase := fmt.Sprintf(".claude/assignment-context/native/%s/g%d/%s/%s/r%d", contextPathID(c.RuntimeID), generation, contextPathID(d.AssignmentID), contextPathID(d.Owner), snap.Revision)
	c.Schema.Path, err = publishNativeContextAsset(root, contextBase+"/agent-message.schema.json", ".claude/assignment-context", "schema", schemaBytes)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	if c.PlanDraft != nil {
		draft, err := json.MarshalIndent(c.PlanDraft, "", "  ")
		if err != nil {
			return agentcontext.Reference{}, err
		}
		c.PlanPath, err = publishNativeContextAsset(root, c.PlanPath, ".claude/evidence", "plan", append(draft, '\n'))
		if err != nil {
			return agentcontext.Reference{}, err
		}
	}
	if c.ResultDraft != nil {
		draft, err := json.MarshalIndent(c.ResultDraft, "", "  ")
		if err != nil {
			return agentcontext.Reference{}, err
		}
		c.ResultPath, err = publishNativeContextAsset(root, c.ResultPath, ".claude/evidence", "result", append(draft, '\n'))
		if err != nil {
			return agentcontext.Reference{}, err
		}
	}
	c.ReadingOrder = agentcontext.InputReadingOrder(inputs, "")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return agentcontext.Reference{}, err
	}
	data = append(data, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", data); err != nil {
		return agentcontext.Reference{}, err
	}
	path, err := publishNativeContextAsset(root, contextBase+"/context.json", ".claude/assignment-context", "context", data)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	latest, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil || latest.Revision != snap.Revision || !reflect.DeepEqual(latest.State, state) {
		return agentcontext.Reference{}, fmt.Errorf("Runtime changed during assignment projection; retry from original authority: %w", runtime.ErrStaleRevision)
	}
	return agentcontext.Reference{Path: path}, nil
}

// Copy nested profiles so the registered context does not share mutable slices.
// A declared collection stays declared: a non-nil empty source is copied into a
// non-nil empty slice so the published context shows an empty array, while an
// undeclared (nil) collection stays undeclared instead of being invented.
func copyContextProfiles(in []checkexec.Profile) []checkexec.Profile {
	out := make([]checkexec.Profile, len(in))
	copy(out, in)
	for i := range out {
		if out[i].Environment != nil {
			out[i].Environment = append([]checkexec.EnvFact{}, out[i].Environment...)
		}
		if out[i].Dependencies != nil {
			out[i].Dependencies = append([]checkexec.FileRef{}, out[i].Dependencies...)
		}
		if out[i].Fixtures != nil {
			out[i].Fixtures = append([]checkexec.FileRef{}, out[i].Fixtures...)
		}
	}
	return out
}
func copyContextDispatch(in agentcontext.Assignment) agentcontext.Assignment {
	if in.ClosingContract != nil {
		copyOf := *in.ClosingContract
		if in.ClosingContract.WorkerChecks != nil {
			copyOf.WorkerChecks = append([]assignmentrecord.WorkerCheck{}, in.ClosingContract.WorkerChecks...)
		}
		for i := range copyOf.WorkerChecks {
			copyOf.WorkerChecks[i].Profile = copyContextProfiles([]checkexec.Profile{copyOf.WorkerChecks[i].Profile})[0]
		}
		in.ClosingContract = &copyOf
	}
	return in
}

func mustPacketBytes(p workpacket.Packet) []byte {
	data, _ := json.MarshalIndent(p, "", "  ")
	return append(data, '\n')
}

func nativeOwnedCheckCommands(root string, dispatch agentcontext.Assignment) []string {
	if dispatch.AssignmentRecord == nil || dispatch.ClosingContract == nil || len(dispatch.ClosingContract.WorkerChecks) == 0 {
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

// contextPathID preserves the full original identity while bounding each
// filesystem component; no professional content contributes to the path.
func contextPathID(value string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(value))
	if encoded == "" {
		return "empty"
	}
	parts := []string{}
	for len(encoded) > 100 {
		parts = append(parts, encoded[:100])
		encoded = encoded[100:]
	}
	parts = append(parts, encoded)
	return strings.Join(parts, "/")
}

// An unregistered candidate never gains ownership from matching contents.
// The original Agent context Writer records the returned actual paths.
func publishNativeContextAsset(root, original, storageRoot, kind string, data []byte) (string, error) {
	for attempt := 0; ; attempt++ {
		path := original
		if attempt > 0 {
			path = filepath.ToSlash(filepath.Join(storageRoot, fmt.Sprintf("native-%s-candidate-%d", kind, attempt), kind+".json"))
		}
		created, err := workpacket.PublishNewBounded(root, path, data, 2<<20)
		if err != nil {
			return "", err
		}
		if created {
			return path, nil
		}
		if err := workpacket.CheckPath(root, filepath.ToSlash(filepath.Join(storageRoot, "native-context-probe", "file.json"))); err != nil {
			return "", fmt.Errorf("native context storage unavailable: %w", err)
		}
	}
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
	return "Assignment sources: first Read " + ref.Path + ". Live Agent status=" + string(projection.Status) + "; follow the original plan/activation. Captured agent_state is historical; shadow grants no permissions."
}
