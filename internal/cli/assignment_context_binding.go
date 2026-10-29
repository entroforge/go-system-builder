package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"io"
	"path/filepath"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type assignmentContextBinding struct {
	Version        int                    `json:"version"`
	Generation     int                    `json:"generation"`
	Context        agentcontext.Reference `json:"context_ref"`
	ProfileVersion string                 `json:"profile_version"`
	ProfileSHA256  string                 `json:"profile_sha256"`
	Mode           string                 `json:"mode"`
}

func contextAgent(state map[string]any, owner string) map[string]any {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row["id"] == owner {
			return row
		}
	}
	return nil
}
func decodeContextBinding(agent map[string]any) (assignmentContextBinding, bool, error) {
	raw, ok := agent["context_binding"]
	if !ok {
		return assignmentContextBinding{}, false, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return assignmentContextBinding{}, true, err
	}
	var b assignmentContextBinding
	if err := json.Unmarshal(data, &b); err != nil {
		return b, true, err
	}
	if b.Version != 1 || b.Generation < 1 || b.Context.Path == "" || len(b.Context.SHA256) != 64 {
		return b, true, fmt.Errorf("original context binding is invalid; preserve it for recovery")
	}
	return b, true, nil
}
func readFixedContext(root string, ref agentcontext.Reference) (nativeAssignmentContext, error) {
	var c nativeAssignmentContext
	data, err := workpacket.ReadFile(root, ref.Path, 2<<20)
	if err != nil || workpacket.Digest(data) != ref.SHA256 {
		return c, fmt.Errorf("fixed context bytes are missing or changed; restore original sources")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", data); err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	return c, nil
}

// verifyFixedContext consumes the original frozen profile, never today's
// selector. Agent state/plan/activation continue to come from live Runtime.
func verifyFixedContext(root string, state map[string]any, a hookctx.AssignmentContext, b assignmentContextBinding) (nativeAssignmentContext, error) {
	registry, err := workspace.Decode(state)
	if err != nil {
		return nativeAssignmentContext{}, err
	}
	if registry != nil {
		for _, execution := range registry.Executions {
			if execution.AgentID == a.OwnerAgentID || execution.AssignmentID == a.AssignmentID {
				return nativeAssignmentContext{}, fmt.Errorf("use the original independent execution; fixed native context is not a Main fallback")
			}
		}
	}
	c, err := readFixedContext(root, b.Context)
	if err != nil {
		return c, err
	}
	canonical, err := workspace.Canonical(root)
	if err != nil || c.SourceRoot != canonical {
		return c, fmt.Errorf("fixed context authority moved; use original migration/recovery")
	}
	d, err := workspaceDispatch(state, a)
	if err != nil {
		return c, err
	}
	if err := checkexec.ValidateProfiles(c.CheckProfiles, c.Checks, ""); err != nil {
		return c, err
	}
	for _, p := range c.CheckProfiles {
		if _, err := p.ActualEnvironment(); err != nil {
			return c, err
		}
	}
	if c.RuntimeID != workspace.RuntimeID(state) || c.Generation != workspace.Generation(state) || !reflect.DeepEqual(c.Dispatch, d) {
		return c, fmt.Errorf("fixed context belongs to a different current dispatch")
	}
	if len(c.OwnedCheckCommands) > 0 {
		want := nativeOwnedCheckCommands(c.SourceRoot, c.Dispatch)
		if len(want) == 0 || !reflect.DeepEqual(c.OwnedCheckCommands, want) {
			return c, fmt.Errorf("fixed owned_check_commands differ from the current frozen native WorkerChecks")
		}
	}
	if c.Selection.ProfileVersion != b.ProfileVersion || c.Selection.ProfileSHA256 != b.ProfileSHA256 || c.Selection.Mode != b.Mode {
		return c, fmt.Errorf("fixed context profile binding differs")
	}
	if err := verifyConventionView(root, state, c.Conventions, c.Inputs); err != nil {
		return c, err
	}
	if err := verifyContextInputs(root, c.Inputs); err != nil {
		return c, err
	}
	if c.ResultDraft != nil {
		if !c.ResultDraftEditable {
			return c, fmt.Errorf("fixed Result draft is not marked editable")
		}
		wantPath := fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/a%d/completion.json", c.RuntimeID, c.Generation, c.Dispatch.Owner, c.Dispatch.AssignmentRevision)
		if c.ResultPath != wantPath {
			return c, fmt.Errorf("fixed Result draft path differs from its original owner/revision")
		}
		occurredAt, _ := c.ResultDraft["occurred_at"].(string)
		canonicalDraft, questions := agentcontext.ResultTemplate(c.Dispatch, c.RuntimeID, occurredAt)
		canonicalBytes, canonicalErr := json.Marshal(canonicalDraft)
		capturedBytes, capturedErr := json.Marshal(c.ResultDraft)
		if canonicalErr != nil || capturedErr != nil || !bytes.Equal(canonicalBytes, capturedBytes) || !agentcontext.ResultQuestionsMatch(questions, c.ResultQuestions) {
			return c, fmt.Errorf("fixed Result template differs from the current canonical Assignment Record projection")
		}
		draftBytes, err := workpacket.ReadFile(root, c.ResultPath, 2<<20)
		if err != nil {
			return c, fmt.Errorf("editable fixed Result draft is unavailable: %w", err)
		}
		if err := agentcontext.ValidateResultDraft(c.ResultDraft, draftBytes); err != nil {
			return c, fmt.Errorf("editable fixed Result draft differs from its frozen Record tuple: %w", err)
		}
	} else if c.ResultPath != "" || c.ResultDraftEditable || len(c.ResultQuestions) != 0 {
		return c, fmt.Errorf("fixed context has incomplete Result draft metadata")
	}
	schemaBytes, err := workpacket.ReadFile(root, c.Schema.Path, 2<<20)
	if err != nil || workpacket.Digest(schemaBytes) != c.Schema.SHA256 {
		return c, fmt.Errorf("original fixed plan Schema is unverifiable")
	}
	if c.WorkPacketRef != "" {
		packets, err := runtime.S5Packets(root, state, a.OwnerAgentID)
		if err != nil {
			return c, err
		}
		matched := false
		for _, p := range packets {
			if p.Binding.AssignmentID == a.AssignmentID && p.ViewPath == c.WorkPacketRef && p.AnswerPath == c.AnswerPath {
				matched = true
			}
		}
		if !matched {
			return c, fmt.Errorf("fixed S5 context is retired or revised; prepare its original named revision with --replace-generation %d", b.Generation)
		}
	}
	return c, nil
}
func currentAssignmentContext(root string, snap runtime.Snapshot, a hookctx.AssignmentContext) (agentcontext.Reference, error) {
	agent := contextAgent(snap.State, a.OwnerAgentID)
	if agent == nil {
		return agentcontext.Reference{}, fmt.Errorf("context owner is not registered")
	}
	b, found, err := decodeContextBinding(agent)
	if err != nil {
		return agentcontext.Reference{}, err
	}
	if !found {
		return projectAssignmentContext(root, snap, a)
	}
	if _, err := verifyFixedContext(root, snap.State, a, b); err != nil {
		return agentcontext.Reference{}, err
	}
	return b.Context, nil
}

func runAssignmentContext(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "runtime assignment-context <prepare|render-role|check>; prepare fixes an original 1.2 native/teammate context; check runs one frozen native WorkerCheck through the isolated original runner; render-role returns a core-role candidate without enabling it")
		return 0
	}
	if args[0] == "render-role" {
		return runContextRoleRender(args[1:], stdout, stderr)
	}
	if args[0] == "check" {
		return runNativeAssignmentCheck(args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "prepare" {
		fmt.Fprintln(stderr, "runtime assignment-context requires prepare")
		return 2
	}
	fs := flag.NewFlagSet("runtime assignment-context prepare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "original Main authority root")
	owner := fs.String("agent-id", "", "actual registered owner")
	replace := fs.Int("replace-generation", 0, "exact old context generation, only for an original S5 named revision")
	if err := parseWorkspaceFlags(fs, args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if *owner == "" || *replace < 0 {
		return fail(fmt.Errorf("actual owner and nonnegative replacement generation required"))
	}
	if err := workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	writer := runtime.NewWriter(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{})
	snap, err := writer.Snapshot()
	if err != nil {
		return fail(err)
	}
	if !workstate.SupportsAssignments(snap.State["schema_version"]) {
		return fail(fmt.Errorf("context binding requires original 1.2; legacy Runtime is not upgraded"))
	}
	loaded, err := hookctx.LoadFull(*root, *owner)
	if err != nil {
		return fail(err)
	}
	var a *hookctx.AssignmentContext
	for i := range loaded.Assignments {
		if loaded.Assignments[i].OwnerAgentID == *owner {
			if a != nil {
				return fail(fmt.Errorf("registered owner has ambiguous Assignments"))
			}
			a = &loaded.Assignments[i]
		}
	}
	if a == nil {
		return fail(fmt.Errorf("owner has no current registered Assignment"))
	}
	old, found, err := decodeContextBinding(contextAgent(snap.State, *owner))
	if err != nil {
		return fail(err)
	}
	if found && *replace == 0 {
		capture, err := verifyFixedContext(*root, snap.State, *a, old)
		if err != nil {
			return fail(err)
		}
		return encodeContextBindingWithCommands(stdout, old, capture.OwnedCheckCommands)
	}
	if !found && *replace != 0 {
		return fail(fmt.Errorf("no original context generation to replace"))
	}
	if found && *replace > 0 && old.Generation == *replace+1 {
		capture, err := verifyFixedContext(*root, snap.State, *a, old)
		if err != nil {
			return fail(err)
		}
		packets, err := runtime.S5Packets(*root, snap.State, *owner)
		if err != nil {
			return fail(err)
		}
		for _, packet := range packets {
			if capture.WorkPacketRef == packet.ViewPath && packet.Binding.PreviousSubmissionKey != "" {
				return encodeContextBindingWithCommands(stdout, old, capture.OwnedCheckCommands)
			}
		}
		return fail(fmt.Errorf("context replacement retry is not the original named revision"))
	}
	if found && old.Generation != *replace {
		return fail(fmt.Errorf("context generation changed; preserve the current binding"))
	}
	ref, err := projectAssignmentContext(*root, snap, *a)
	if errors.Is(err, runtime.ErrStaleRevision) {
		latest, e := writer.Snapshot()
		if e == nil {
			winner, has, e := decodeContextBinding(contextAgent(latest.State, *owner))
			expected := 1
			if found {
				expected = old.Generation + 1
			}
			if e == nil && has && winner.Generation == expected {
				capture, e := verifyFixedContext(*root, latest.State, *a, winner)
				if e == nil && (!found || validateContextReissue(*root, latest.State, old, capture) == nil) {
					return encodeContextBindingWithCommands(stdout, winner, capture.OwnedCheckCommands)
				}
			}
		}
	}
	if err != nil {
		return fail(err)
	}
	c, err := readFixedContext(*root, ref)
	if err != nil {
		return fail(err)
	}
	binding := assignmentContextBinding{Version: 1, Generation: 1, Context: ref, ProfileVersion: c.Selection.ProfileVersion, ProfileSHA256: c.Selection.ProfileSHA256, Mode: c.Selection.Mode}
	if found {
		if err := validateContextReissue(*root, snap.State, old, c); err != nil {
			return fail(err)
		}
		binding.Generation = old.Generation + 1
	}
	next, err := writer.Update(snap.Revision, runtime.Mutation{EventID: fmt.Sprintf("context-%s-g%d-r%d", workpacket.Digest([]byte(*owner))[:16], binding.Generation, snap.Revision+1), TransitionID: "CONTEXT-PREPARE", Event: "assignment_context_prepared", Actor: "main", RuntimeID: workspace.RuntimeID(snap.State), IdempotencyKey: fmt.Sprintf("context:%s:%d:%s", *owner, binding.Generation, ref.SHA256), RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		agent := contextAgent(state, *owner)
		if agent == nil {
			return fmt.Errorf("registered context owner disappeared")
		}
		existing, has, err := decodeContextBinding(agent)
		if err != nil {
			return err
		}
		if has != found || has && !reflect.DeepEqual(existing, old) {
			return fmt.Errorf("original context binding changed")
		}
		if _, err := verifyFixedContext(*root, state, *a, binding); err != nil {
			return err
		}
		if found {
			if err := validateContextReissue(*root, state, old, c); err != nil {
				return err
			}
		}
		data, _ := json.Marshal(binding)
		var value map[string]any
		json.Unmarshal(data, &value)
		agent["context_binding"] = value
		return nil
	}})
	if errors.Is(err, runtime.ErrStaleRevision) {
		latest, e := writer.Snapshot()
		if e == nil {
			winner, has, e := decodeContextBinding(contextAgent(latest.State, *owner))
			if e == nil && has && reflect.DeepEqual(winner, binding) {
				if capture, e := verifyFixedContext(*root, latest.State, *a, winner); e == nil {
					return encodeContextBindingWithCommands(stdout, winner, capture.OwnedCheckCommands)
				}
			}
		}
	}
	if err != nil {
		return fail(err)
	}
	_ = next
	return encodeContextBindingWithCommands(stdout, binding, c.OwnedCheckCommands)
}
func encodeContextBinding(out io.Writer, b assignmentContextBinding) int {
	return encodeContextBindingWithCommands(out, b, nil)
}

func encodeContextBindingWithCommands(out io.Writer, b assignmentContextBinding, commands []string) int {
	wire := struct {
		Version            int                    `json:"version"`
		Generation         int                    `json:"generation"`
		Context            agentcontext.Reference `json:"context_ref"`
		ProfileVersion     string                 `json:"profile_version"`
		ProfileSHA256      string                 `json:"profile_sha256"`
		Mode               string                 `json:"mode"`
		OwnedCheckCommands []string               `json:"owned_check_commands,omitempty"`
	}{b.Version, b.Generation, b.Context, b.ProfileVersion, b.ProfileSHA256, b.Mode, append([]string(nil), commands...)}
	if err := json.NewEncoder(out).Encode(wire); err != nil {
		return 1
	}
	return 0
}

func validateContextReissue(root string, state map[string]any, old assignmentContextBinding, next nativeAssignmentContext) error {
	previous, err := readFixedContext(root, old.Context)
	if err != nil {
		return err
	}
	if previous.RuntimeID != next.RuntimeID || previous.Generation != next.Generation || !reflect.DeepEqual(previous.Dispatch, next.Dispatch) || !reflect.DeepEqual(previous.Selection, next.Selection) || previous.Schema != next.Schema {
		return fmt.Errorf("named context revision cannot change original dispatch/profile/Skill/Schema")
	}
	if previous.Inputs[previous.Dispatch.AgentDefinitionRef] != next.Inputs[next.Dispatch.AgentDefinitionRef] {
		return fmt.Errorf("named context revision cannot change the frozen role asset")
	}
	if previous.WorkPacketRef == "" || next.WorkPacketRef == "" || previous.WorkPacketRef == next.WorkPacketRef {
		return fmt.Errorf("context replacement requires an original new S5 packet")
	}
	data, err := workpacket.ReadFile(root, previous.WorkPacketRef, 1<<20)
	if err != nil {
		return err
	}
	var prior workpacket.Packet
	if err := json.Unmarshal(data, &prior); err != nil {
		return err
	}
	registered, ok, err := runtime.FindWorkPacket(state, prior.ID)
	if err != nil || !ok || !reflect.DeepEqual(prior, registered) {
		return fmt.Errorf("original frozen packet differs from Runtime")
	}
	receipt, ok, err := runtime.FindSubmission(state, prior.ID)
	if err != nil || !ok {
		return fmt.Errorf("context revision has no original received predecessor")
	}
	packets, err := runtime.S5Packets(root, state, next.Dispatch.Owner)
	if err != nil {
		return err
	}
	for _, p := range packets {
		if p.ViewPath == next.WorkPacketRef && p.Binding.PreviousSubmissionKey == receipt.Key && p.Binding.RuntimeID == prior.Binding.RuntimeID && p.Binding.Generation == prior.Binding.Generation && p.Binding.StageEntry == prior.Binding.StageEntry && p.Binding.Owner == prior.Binding.Owner && p.Binding.AssignmentID == prior.Binding.AssignmentID && receipt.RevisionPacketID == p.ID {
			return nil
		}
	}
	return fmt.Errorf("context replacement is outside the original S5 revision lineage")
}

func runContextRoleRender(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime assignment-context render-role", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "original installed asset root")
	role := fs.String("role", "backend-builder", "first Builder core profile")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	source, path, err := catalog.ReadAgentAsset(*root, *role)
	if err != nil {
		return fail(err)
	}
	generated, err := agentcontext.RenderCoreRole(*role, source)
	if err != nil {
		return fail(err)
	}
	selection, err := agentcontext.Select(*root, *role, nil, nil)
	if err != nil {
		return fail(err)
	}
	result := map[string]any{"profile_version": selection.ProfileVersion, "profile_sha256": selection.ProfileSHA256, "source_ref": agentcontext.Reference{Path: path, SHA256: workpacket.Digest(source)}, "core_skills": agentcontext.CoreSkills(*role), "generated_role": string(generated), "generated_sha256": workpacket.Digest(generated), "enabled": false}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return fail(err)
	}
	return 0
}
