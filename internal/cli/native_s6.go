package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func pinNativeS6Source(root, id string, revision int, base string) error {
	body, _ := json.Marshal(map[string]any{"base_commit": base})
	ref := fmt.Sprintf(".claude/assignments/native/%s/r%d/source.json", id, revision)
	published, err := workpacket.PublishBounded(root, ref, body, 4096)
	if err == nil && !bytes.Equal(published, body) {
		return fmt.Errorf("original native preparation source commit differs; preserve it for Main recovery")
	}
	return err
}

// Resume the original prepared input after a registration failure. Never
// overwrite its draft or recapture a different source commit.
func resumeNativeS6Preparation(root, req, task, role, owner, output string, out, diagnostic io.Writer) (bool, int) {
	ref := output
	if ref == "" {
		ref = filepath.ToSlash(filepath.Join(".claude/workgroups", req, task, "manifest.draft.json"))
	}
	data, err := workpacket.ReadFile(root, ref, 1<<20)
	if os.IsNotExist(err) {
		return false, 0
	}
	if err != nil {
		fmt.Fprintln(diagnostic, err)
		return true, 1
	}
	var draft s6ManifestDraft
	if json.Unmarshal(data, &draft) != nil || draft.AssignmentRecordContract != assignmentrecord.NativeDeliveryContract || len(draft.Assignments) != 1 || draft.Assignments[0].AgentID != owner || draft.Assignments[0].RoleFamily != role {
		fmt.Fprintln(diagnostic, "existing preparation has a different frozen contract; preserve it for Main recovery")
		return true, 1
	}
	source, err := workpacket.ReadFile(root, fmt.Sprintf(".claude/assignments/native/%s/r1/source.json", draft.Assignments[0].AssignmentID), 4096)
	var pin struct {
		Base string `json:"base_commit"`
	}
	if err != nil || json.Unmarshal(source, &pin) != nil || pin.Base == "" {
		fmt.Fprintln(diagnostic, "original preparation source commit is unavailable; Main must recover it, never recapture HEAD")
		return true, 1
	}
	for _, doc := range draft.Documents {
		if doc.ID == task {
			return true, registerNativeS6Delivery(root, ref, task, doc.Path, pin.Base, out, diagnostic)
		}
	}
	fmt.Fprintln(diagnostic, "original preparation has no exact TASK source")
	return true, 1
}

func registerNativeS6Delivery(root, manifest, taskID, taskPath, base string, stdout, stderr io.Writer) int {
	data, err := workpacket.ReadFile(root, manifest, 1<<20)
	var draft s6ManifestDraft
	if err != nil || json.Unmarshal(data, &draft) != nil || len(draft.Assignments) != 1 {
		fmt.Fprintln(stderr, "native preparation has no exact manifest Assignment")
		return 1
	}
	if err := pinNativeS6Source(root, draft.Assignments[0].AssignmentID, 1, base); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	statePath, journal := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	snap, err := assignment.Register(root, statePath, journal, assignment.Request{ExpectedRevision: -1, ManifestPath: filepath.Join(root, manifest), TaskID: taskID, TaskPath: filepath.Join(root, taskPath)})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	records, err := assignmentrecord.RecordsFromState(snap.State)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, record := range records {
		if record.Task.ID == taskID && record.Contract == assignmentrecord.NativeDeliveryContract {
			if err := prepareNativeS6Brief(root, snap.State, record, base, stdout); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		}
	}
	fmt.Fprintln(stderr, "native TASK registration has no current Record")
	return 1
}

func prepareNativeS6Brief(root string, state map[string]any, r assignmentrecord.Record, base string, out io.Writer) error {
	brief, plan, result := assignmentrecord.NativePaths(r)
	var b strings.Builder
	methods := nativeS6MethodGuidance(root, r)
	b.WriteString(methods)
	fmt.Fprintf(&b, "\nDone when:\n")
	for _, condition := range r.DoneWhen {
		fmt.Fprintf(&b, "- %s\n", condition)
	}
	fmt.Fprintf(&b, "\nRun the actual TASK self-checks (Main repeats its checks at integration):\n")
	for _, check := range r.RequiredChecks {
		fmt.Fprintf(&b, "- %s\n", check)
	}
	dependencies, err := json.Marshal(append([]string{}, r.DependsOn...))
	if err != nil {
		return err
	}
	fmt.Fprintf(&b, "\nRegistered workgroup-local Assignment dependencies (read-only): %s. The receiver supplies this exact set to the canonical Plan; do not copy dependencies into either private JSON file. TASK prerequisites have already been satisfied by dispatch. Explain actual input or environment assumptions in objective/oracles and unresolved risks_blockers instead.\n", dependencies)
	previousCurrent := nativeS6BriefInputs(root, r, false) + b.String() + nativeS6ProfessionalInstructions(base, filepath.Join(root, brief), plan, result)
	previousAligned := previousCurrent + nativeS6LegacySourceAlignment(r, base, plan, result)
	previousSourceAligned := nativeS6SourceAlignment(r, base, plan, result) + previousCurrent
	// Historical bytes remain recognized; new briefs separate owner-run checks
	// from Main's integration checks and explain the private Plan write scope.
	oldChecks := "\nRun the actual TASK self-checks (Main repeats its checks at integration):\n"
	for _, check := range r.RequiredChecks {
		oldChecks += "- " + check + "\n"
	}
	workerChecks := oldChecks
	if r.ClosingContract != nil {
		workerChecks = "\nRun these Worker self-checks from the TASK Closing Contract (Main owns its separate integration checks):\n"
		for _, check := range r.ClosingContract.WorkerChecks {
			workerChecks += "- " + check.Command + "\n"
		}
		if len(r.ClosingContract.WorkerChecks) == 0 {
			workerChecks += "- No Worker runner checks were assigned. Main-only checks are not Worker execution duties; still assess every Done when condition honestly.\n"
		}
	}
	current := strings.Replace(previousSourceAligned, oldChecks, workerChecks, 1) + nativeS6CurrentAuthoringGuidance
	previousInputs := nativeS6BriefInputs(root, r, true) + b.String()
	compact := previousInputs + fmt.Sprintf(nativeS6CompactProfessionalFormat, base, filepath.Join(root, brief), plan, result)
	legacy := previousInputs + nativeS6LegacyProfessionalInstructions(base, filepath.Join(root, brief), plan, result)
	if published, err := workpacket.PublishBounded(root, brief, []byte(current), 1<<20); err != nil {
		return err
	} else if !nativeS6BriefMatches(published, methods, current, previousSourceAligned, previousAligned, previousCurrent, compact, legacy) {
		return fmt.Errorf("original native task brief differs; preserve it for Main recovery")
	}
	bound, _ := state["bound_req"].(map[string]any)
	binding, _ := bound["workspace"].(map[string]any)
	coordinates := map[string]any{"assignment_id": r.AssignmentID, "assignment_revision": r.Revision, "base_commit": base, "target_branch": binding["dev_branch"], "worktree_path": "", "branch": ""}
	data, _ := json.MarshalIndent(coordinates, "", "  ")
	coordinatePath := filepath.Join(root, ".claude/assignments", r.AssignmentID+".json")
	previousBytes, readErr := os.ReadFile(coordinatePath)
	var previous struct {
		Revision int `json:"assignment_revision"`
	}
	_ = json.Unmarshal(previousBytes, &previous)
	if os.IsNotExist(readErr) || (readErr == nil && previous.Revision < r.Revision) {
		if err := os.WriteFile(coordinatePath, data, 0600); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Native S6 TASK registered: %s\nMain: use Claude Code's native Agent (%s) with worktree isolation. Carry this exact task brief in its prompt: %s\nBuilder saves professional plan/result in its own worktree. SubagentStop receives them; Main integrates separately. No Team, platform Agent ID or launcher is required.\n", r.Task.ID, r.RoleFamily, filepath.Join(root, brief))
	return nil
}

// Product and specification reads share the implementation's fixed worktree
// source. The control brief and installed methods retain their exact Main paths.
// The old input rendering is only for preserving already-issued brief bytes.
func nativeS6BriefInputs(root string, r assignmentrecord.Record, legacy bool) string {
	var b strings.Builder
	if legacy {
		fmt.Fprintf(&b, "# %s — %s\n\nContract: %s\n\nRead the actual TASK at %s and its linked inputs. Implement only its reviewed scope: %s.\n\n", r.Task.ID, r.RoleFamily, r.Contract, filepath.Join(root, r.Task.Ref), strings.Join(r.EffectiveWritePaths, ", "))
		for _, ref := range r.ReadPaths {
			fmt.Fprintf(&b, "- Read %s\n", filepath.Join(root, ref))
		}
		return b.String()
	}
	fmt.Fprintf(&b, "# %s — %s\n\nContract: %s\n\nRead the actual TASK at worktree-relative path %s and its linked inputs. Implement only its reviewed scope: %s.\n\nSource reads below are relative to your actual native worktree at the fixed source commit stated below; read TASK, specifications and product sources there. Main's control root %s supplies this brief and the exact installed method paths, not a replacement product/specification read root. Preserve the original source when Main advances; missing worktree inputs belong to Main recovery.\n\n", r.Task.ID, r.RoleFamily, r.Contract, filepath.ToSlash(r.Task.Ref), strings.Join(r.EffectiveWritePaths, ", "), root)
	for _, ref := range r.ReadPaths {
		fmt.Fprintf(&b, "- Read worktree-relative: %s\n", filepath.ToSlash(ref))
	}
	return b.String()
}

// Deliver the original Record's method sources. Specialty conditions stay in
// the actual role definition, where the author can apply them to real inputs;
// no prose classifier or second Skill authorization registry is introduced.
func nativeS6MethodGuidance(root string, r assignmentrecord.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nProfessional methods: use the core preloaded by your role. Its Skill Contract at %s lists exact installed specialty paths and the TASK/input conditions for reading them. Apply those conditions to your actual work; do not preload unrelated methods or seek another activation.\n", filepath.Join(root, r.AgentDefinition.Ref))
	registered := make(map[string]bool, len(r.SkillRefs))
	for _, name := range r.SkillRefs {
		registered[name] = true
	}
	for _, source := range r.ContextSources {
		if source.Kind == "authorized_skill" && registered[source.ID] {
			fmt.Fprintf(&b, "- Registered method %s: %s\n", source.ID, filepath.Join(root, source.Ref))
		}
	}
	return b.String()
}

func nativeS6Records(state map[string]any) []assignmentrecord.Record {
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "building" {
		return nil
	}
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return nil
	}
	var out []assignmentrecord.Record
	for _, r := range records {
		if current, found, err := assignmentrecord.ForOwner(state, r.OwnerAgentID); err == nil && found && current.Reference() == r.Reference() && r.Contract == assignmentrecord.NativeDeliveryContract {
			out = append(out, r)
		}
	}
	return out
}

// A known legacy owner keeps its frozen contract. No native actor is rewritten.
func nativeS6Worker(state map[string]any, in policy.Input) bool {
	if in.AgentID == "" {
		return false
	}
	if r, found, _ := assignmentrecord.ForOwner(state, in.AgentID); found {
		return r.Contract == assignmentrecord.NativeDeliveryContract
	}
	return false
}

// The initial task prompt supplies the exact brief. Roles/queue order never bind it.
func nativeS6ExactReference(text, ref string) bool {
	for offset := 0; offset < len(text); {
		index := strings.Index(text[offset:], ref)
		if index < 0 {
			return false
		}
		index += offset
		end := index + len(ref)
		before := index == 0 || strings.ContainsRune(" \t\r\n`\"'(<[{=", rune(text[index-1]))
		after := end == len(text) || strings.ContainsRune(" \t\r\n`\"')>]},;:", rune(text[end]))
		if before && after {
			return true
		}
		offset = end
	}
	return false
}

func nativeS6TaskText(in policy.Input) string {
	text, _ := nativeTaskTextDiagnostic(in, nil, false)
	return text
}

// Native worktrees have no framework bootstrap pointer or copied Runtime.
// Locate only an unambiguous existing Main authority in the same Git repository.
func nativeS6ControlRoot(root string, in policy.Input) string {
	if in.AgentID == "" {
		return root
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json")); err == nil {
		return root
	}
	listing, err := workspace.Git(contextForHook(in), root, "worktree", "list", "--porcelain")
	if err != nil {
		return root
	}
	chosen := ""
	for _, line := range strings.Split(listing, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		candidate := strings.TrimPrefix(line, "worktree ")
		if _, err := os.Stat(filepath.Join(candidate, ".claude/loop-state.json")); err != nil {
			continue
		}
		snap, err := runtime.NewStore(filepath.Join(candidate, ".claude/loop-state.json"), filepath.Join(candidate, ".claude/loop-events.jsonl")).WithContext(contextForHook(in)).Snapshot()
		if err != nil || fileview.ValidateAuthority(candidate, snap.State) != nil || len(nativeS6Records(snap.State)) == 0 && !nativeS9Available(candidate, snap.State) && nativeReviewStage(snap.State) == "" {
			continue
		}
		if chosen != "" && chosen != candidate {
			return root
		}
		chosen = candidate
	}
	if chosen != "" {
		return chosen
	}
	return root
}

func nativeS6Hook(root string, in policy.Input, out, diagnostic io.Writer) (bool, int) {
	if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json")); err != nil {
		return false, 0
	}
	snap, err := nativeRoutingSnapshot(root, in)
	if err != nil {
		return false, 0
	}
	if in.Event == "PostToolUse" && (in.ToolName == "Agent" || in.ToolName == "Task" || in.ToolName == "SubagentHandback") && len(nativeS6Records(snap.State)) > 0 {
		return true, 0
	}
	if in.AgentID == "" || in.AgentID == "Main" {
		return false, 0
	}
	if owned, found, _ := assignmentrecord.ForOwner(snap.State, in.AgentID); found && owned.Contract != assignmentrecord.NativeDeliveryContract {
		return false, 0
	}
	records := nativeS6Records(snap.State)
	if in.Event != "SubagentStart" && in.Event != "SubagentStop" {
		// Ordinary worker tools need no transcript read or lifecycle instructions.
		return nativeS6Worker(snap.State, in) || len(records) > 0, 0
	}
	if len(records) == 0 && !nativeS6Worker(snap.State, in) {
		return false, 0 // No exact S6 reference can select a responsibility.
	}
	refs := make([]string, 0, len(records))
	for _, r := range records {
		brief, _, _ := assignmentrecord.NativePaths(r)
		refs = append(refs, filepath.Join(root, brief))
	}
	taskText := nativeReviewTaskText(in, refs)
	exact := false
	for _, ref := range refs {
		if nativeS6ExactReference(taskText, ref) {
			exact = true
			break
		}
	}
	if owned, found, _ := assignmentrecord.ForOwner(snap.State, in.AgentID); found && owned.Contract == assignmentrecord.NativeDeliveryContract && exact {
		var conflicts []assignmentrecord.Record
		var observed []string
		for _, r := range records {
			brief, _, _ := assignmentrecord.NativePaths(r)
			if r.Reference() != owned.Reference() && nativeS6ExactReference(taskText, filepath.Join(root, brief)) {
				conflicts = append(conflicts, r)
				observed = append(observed, filepath.Join(root, brief))
			}
		}
		if len(conflicts) > 0 {
			brief, _, _ := assignmentrecord.NativePaths(owned)
			message := fmt.Sprintf("Native S6 responsibility mismatch: actor %s is bound to Assignment %s revision %d, original brief %s; observed conflicting exact brief(s): %s. Main resolves this original actor/brief mismatch and preserves the worktree before resuming any responsibility. No Result was inspected or received; do not assign work by role.", in.AgentID, owned.AssignmentID, owned.Revision, filepath.Join(root, brief), strings.Join(observed, "; "))
			if in.Event == "SubagentStop" {
				for _, r := range append(conflicts, owned) {
					submission.RecordPlanningDiagnostic(root, snap.State, nativeS6DiagnosticKind(r), message)
				}
			} else {
				_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "SubagentStart", "additionalContext": message}})
			}
			fmt.Fprintln(diagnostic, message)
			return true, 0
		}
	}
	if !nativeS6Worker(snap.State, in) && !exact {
		// Start often has no task prompt/transcript yet. Absence of a precise
		// responsibility is not permission to adopt a queued task or guess by role.
		// Preserve the native ordinary-tool path without adding an ACL.
		if len(records) > 0 {
			return true, 0
		}
		return false, 0
	}
	if in.Event == "SubagentStart" || in.Event == "SubagentStop" {
		if engine, err := policy.Load(filepath.Join(root, "docs/control/hook-policy.json")); err == nil {
			observed := policy.Decision{Decision: "audit", RuleID: "native_s6_lifecycle_observed", Reason: "native S6 lifecycle observed", MatchedRuleIDs: []string{"native_s6_lifecycle_observed"}, Retry: "not_applicable"}
			auditStarted := time.Now()
			defer func() {
				observed.ElapsedMS = time.Since(auditStarted).Milliseconds()
				if err := appendHookAudit(contextForHook(in), root, engine.Envelope(in, observed, time.Now())); err != nil {
					fmt.Fprintln(diagnostic, "Native lifecycle audit unavailable: "+err.Error())
				}
			}()
		}
	}
	if in.Event == "SubagentStart" {
		var start *assignmentrecord.Record
		for _, r := range records {
			brief, _, _ := assignmentrecord.NativePaths(r)
			if exact && nativeS6ExactReference(taskText, filepath.Join(root, brief)) || !exact && r.OwnerAgentID == in.AgentID {
				if start != nil {
					return true, 0
				}
				copy := r
				start = &copy
			}
		}
		if start == nil {
			return true, 0
		}
		switch workstate.AgentActivity(contextAgent(snap.State, start.OwnerAgentID)) {
		case "done", "stopped", "cancelled":
			return true, 0
		}
		brief, _, _ := assignmentrecord.NativePaths(*start)
		context := fmt.Sprintf("Native S6 %s (Assignment %s revision %d): follow the original exact brief %s and its original source instructions. Save the professional Plan before implementation, continue actual work and self-checks without activation, commit and save Result in your own worktree. Return this brief and the absolute Result reference. For a real blocker, preserve existing work and use the brief's blocked handoff; Main owns recovery and integration.", start.Task.ID, start.AssignmentID, start.Revision, filepath.Join(root, brief))
		body := map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "SubagentStart", "additionalContext": context}}
		_ = json.NewEncoder(out).Encode(body)
		return true, 0
	}
	if in.Event != "SubagentStop" {
		return true, 0
	} // Ordinary Worker tools do not manage execution.

	var chosen *assignmentrecord.Record
	for _, r := range records {
		brief, _, _ := assignmentrecord.NativePaths(r)
		if nativeS6ExactReference(taskText, filepath.Join(root, brief)) {
			if chosen != nil {
				fmt.Fprintln(diagnostic, "Native S6 return has multiple exact task briefs; Main must resolve the handoff.")
				return true, 0
			}
			r := r
			chosen = &r
		}
	}
	if chosen == nil {
		fmt.Fprintln(diagnostic, "Native S6 return has no exact task brief; preserve the native result for Main, do not guess by role.")
		return true, 0
	}
	// A closed engineering responsibility is not reopened by a late native
	// notification, including one delivered after its worktree was cleaned up.
	switch workstate.AgentActivity(contextAgent(snap.State, chosen.OwnerAgentID)) {
	case "done", "stopped", "cancelled":
		fmt.Fprintln(diagnostic, "Native S6 responsibility already closed; preserve its original receipt/history for Main.")
		return true, 0
	}
	if err := receiveNativeS6(root, in, *chosen, taskText); err != nil {
		var handoff *nativeS6Handoff
		if errors.As(err, &handoff) {
			submission.RecordPlanningDiagnostic(root, snap.State, nativeS6DiagnosticKind(*chosen), nativeS6DiagnosticMessage(root, in, *chosen, taskText, err)+". Stop allowed this blocked handoff back to Main; no completion is established")
			fmt.Fprintln(diagnostic, "Native S6 blocker returned for Main; no completion receipt created: "+err.Error())
			return true, 0
		}

		code := nativeStopFailure("Native S6", in.StopHookActive, err, diagnostic)
		message := nativeS6DiagnosticMessage(root, in, *chosen, taskText, err)
		if code == 2 {
			message += ". Stop returned code 2 to the Worker for professional correction; actual platform execution/completion must be checked before Main recovery"
		} else {
			message += ". Stop allowed the incomplete return to Main for original-responsibility recovery; no completion is established"
		}
		submission.RecordPlanningDiagnostic(root, snap.State, nativeS6DiagnosticKind(*chosen), message)
		return true, code
	}
	submission.RecordPlanningDiagnostic(root, snap.State, nativeS6DiagnosticKind(*chosen), "")
	return true, 0
}

type nativeS6Handoff struct{ reason string }

func (e *nativeS6Handoff) Error() string { return e.reason }

func receiveNativeS6(root string, in policy.Input, r assignmentrecord.Record, text string) error {
	_, planRel, resultRel := assignmentrecord.NativePaths(r)
	source, err := workspace.Canonical(in.CWD)
	if err != nil {
		return err
	}
	// An explicit absolute output reference can identify the actual native worktree.
	listing, err := workspace.Git(contextForHook(in), root, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}
	matchedSource := ""
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			candidate := strings.TrimPrefix(line, "worktree ")
			if nativeS6ExactReference(text, filepath.Join(candidate, resultRel)) {
				if matchedSource != "" && matchedSource != candidate {
					return fmt.Errorf("multiple actual worktree Result references; Main must resolve the handoff")
				}
				matchedSource = candidate
				source = candidate
			}
		}
	}
	if source == root {
		return fmt.Errorf("writing TASK requires its actual native worktree Result, not Main")
	}
	if err := workspace.ValidateCheckout(root, source); err != nil {
		return err
	}
	coordinatePath := filepath.Join(root, ".claude/assignments", r.AssignmentID+".json")
	coordinateBytes, err := os.ReadFile(coordinatePath)
	if err != nil {
		return err
	}
	var coordinate map[string]any
	if err := json.Unmarshal(coordinateBytes, &coordinate); err != nil {
		return err
	}
	if previous, _ := coordinate["worktree_path"].(string); previous != "" && previous != source {
		return fmt.Errorf("delivery belongs to a different recorded worktree; Main must resolve it")
	}
	if base, _ := coordinate["base_commit"].(string); base == "" {
		return fmt.Errorf("missing original source commit")
	} else if _, err := workspace.Git(contextForHook(in), source, "merge-base", "--is-ancestor", base, "HEAD"); err != nil {
		return fmt.Errorf("native worktree does not contain the declared source commit")
	}
	branch, err := workspace.Git(contextForHook(in), source, "branch", "--show-current")
	if err != nil || branch == "" {
		return fmt.Errorf("native worktree requires a real source branch")
	}
	resultData, err := workpacket.ReadFile(source, resultRel, 1<<20)
	if err != nil {
		return nativeArtifactReadError("read professional Result "+filepath.Join(source, resultRel)+". For ordinary delivery use the full Plan/Result in the original brief. For a real blocker: "+nativeS6BlockerInstructions(filepath.Join(source, resultRel)), err)
	}
	if reason, blocked := nativeS6Blocker(resultData); blocked {
		return &nativeS6Handoff{reason: reason}
	}
	planData, err := workpacket.ReadFile(source, planRel, 1<<20)
	if err != nil {
		return nativeArtifactReadError("read professional Plan "+filepath.Join(source, planRel)+". For a real blocker: "+nativeS6BlockerInstructions(filepath.Join(source, resultRel)), err)
	}
	projected, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, r)
	if err != nil {
		return err
	}
	plan, _ := agentcontext.PlanTemplate(projected, r.RuntimeID, time.Now().UTC().Format(time.RFC3339Nano), nil)
	result, _ := agentcontext.ResultTemplate(projected, r.RuntimeID, time.Now().UTC().Format(time.RFC3339Nano))
	if err := mergeNativeProfessional(plan, planData, []string{"objective", "planned_paths", "assertion_checks", "dependencies", "risks_blockers"}); err != nil {
		return fmt.Errorf("professional Plan %s: %w", filepath.Join(source, planRel), err)
	}
	if err := mergeNativeProfessional(result, resultData, []string{"summary", "assertion_evidence", "risks_blockers", "scope_deviations", "closing_assessment"}); err != nil {
		return fmt.Errorf("professional Result %s (Plan is %s): %w", filepath.Join(source, resultRel), filepath.Join(source, planRel), err)
	}
	plan["assignment_record_contract"] = r.Contract
	result["assignment_record_contract"] = r.Contract
	// Validate the original S6 branches directly so professional mistakes
	// report their exact nested fields instead of an unrelated oneOf branch.
	for _, candidate := range []struct {
		label, definition, path string
		message                 map[string]any
	}{{"Plan", "s6PlanReport", filepath.Join(source, planRel), plan}, {"Result", "s6CompletionReport", filepath.Join(source, resultRel), result}} {
		if candidate.label == "Result" {
			if err := projectNativeS6Result(plan, result, len(r.DoneWhen)); err != nil {
				return &nativeAuthorError{fmt.Errorf("professional Result fields at %s: %w", candidate.path, err)}
			}
		}
		data, err := json.Marshal(candidate.message)
		if err != nil {
			return err
		}
		if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json#/$defs/"+candidate.definition, data); err != nil {
			var validation *jsonschema.ValidationError
			if errors.As(err, &validation) {
				return &nativeAuthorError{fmt.Errorf("professional %s fields at %s: %w", candidate.label, candidate.path, err)}
			}
			return err
		}
	}
	// The original immutable plan artifact supplies its first receiver timestamp.
	// Repeated/concurrent notifications do not invent a new plan or activation.
	planRef := fmt.Sprintf(".claude/assignments/native/%s/r%d/plan-%s.json", r.AssignmentID, r.Revision, workpacket.Digest(planData))
	if err := publishNativeS6Plan(root, planRef, plan); err != nil {
		return err
	}
	result["occurred_at"] = plan["occurred_at"]
	head, err := workspace.Git(contextForHook(in), source, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	result["message_id"] = "msg-native-result-" + workpacket.Digest(append(append(planData, resultData...), []byte(head)...))
	resultBytes, _ := json.Marshal(result)
	resultRef := fmt.Sprintf(".claude/assignments/native/%s/r%d/result-%s.json", r.AssignmentID, r.Revision, workpacket.Digest(resultBytes))
	if published, err := workpacket.PublishBounded(root, resultRef, resultBytes, 1<<20); err != nil {
		return err
	} else if !bytes.Equal(published, resultBytes) {
		return fmt.Errorf("immutable native Result source differs")
	}
	coordinate["worktree_path"] = source
	coordinate["branch"] = branch
	updated, _ := json.MarshalIndent(coordinate, "", "  ")
	if err := os.WriteFile(coordinatePath, updated, 0600); err != nil {
		return err
	}
	_, _, err = assignment.CompleteTaskWithReceipt(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), assignment.CompletionRequest{Context: contextForHook(in), ExpectedRevision: -1, AgentID: r.OwnerAgentID, MessagePath: filepath.Join(root, resultRef), NativePlanPath: planRef, NativeAgentID: in.AgentID, NativeSessionID: in.SessionID})
	return err
}

// A blocker is an honest handoff from the original worktree, not an accepted
// Result. It needs neither fabricated Plan assertions nor completed evidence.
func nativeS6Blocker(data []byte) (string, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != 3 {
		return "", false
	}
	for _, key := range []string{"summary", "risks_blockers", "closing_assessment"} {
		if _, ok := fields[key]; !ok {
			return "", false
		}
	}
	var closing map[string]json.RawMessage
	if json.Unmarshal(fields["closing_assessment"], &closing) != nil || len(closing) != 1 {
		return "", false
	}
	var blocked struct {
		Summary string   `json:"summary"`
		Risks   []string `json:"risks_blockers"`
		Closing struct {
			Disposition string `json:"disposition"`
		} `json:"closing_assessment"`
	}
	if json.Unmarshal(data, &blocked) != nil || strings.TrimSpace(blocked.Summary) == "" || len(blocked.Risks) == 0 || blocked.Closing.Disposition != "blocked" {
		return "", false
	}
	for _, risk := range blocked.Risks {
		if strings.TrimSpace(risk) == "" {
			return "", false
		}
	}
	return blocked.Summary + " / " + strings.Join(blocked.Risks, "; "), true
}

func publishNativeS6Plan(root, ref string, plan map[string]any) error {
	for attempt := 0; attempt < 2; attempt++ {
		existing, err := workpacket.ReadFile(root, ref, 1<<20)
		if err == nil {
			var received map[string]any
			if err := json.Unmarshal(existing, &received); err != nil {
				return err
			}
			plan["occurred_at"] = received["occurred_at"]
			expected, err := json.Marshal(plan)
			if err != nil {
				return err
			}
			if !bytes.Equal(expected, existing) {
				return fmt.Errorf("immutable native professional plan differs from its original reception")
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		data, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		if published, err := workpacket.PublishBounded(root, ref, data, 1<<20); err == nil {
			if bytes.Equal(published, data) {
				return nil
			}
		} else if attempt == 1 {
			return err
		}
	}
	return fmt.Errorf("native professional plan reception unavailable")
}

func mergeNativeProfessional(target map[string]any, data []byte, allowed []string) error {
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		return &nativeAuthorError{fmt.Errorf("professional JSON: %w", err)}
	}
	if target == nil {
		return fmt.Errorf("registered TASK has no professional template")
	}
	for key, value := range answer {
		ok := false
		for _, want := range allowed {
			if want == key {
				ok = true
			}
		}
		if !ok {
			return &nativeAuthorError{fmt.Errorf("unsupported field %q; this file accepts only %s", key, strings.Join(allowed, ", "))}
		}
		target[key] = value
	}
	return nil
}

func nativeS6MainGuidance(root string, state map[string]any) string {
	var lines []string
	for _, r := range nativeS6Records(state) {
		lines = append(lines, nativeS6RecordGuidance(root, state, r))
	}
	if len(lines) > 0 {
		lines = append(lines, "For any remaining unprepared TASKs, use the original `loop-harness s6 status --advice` to identify readiness, then `s6 prepare` for an eligible TASK. Do not wait for unrelated tasks solely because they are in an earlier wave.")
	}
	return strings.Join(lines, "\n")
}

// This is a Runtime fact/guidance projection, not another completion verifier.
// Main's original task-receipt/integrator still validates checks and provenance.
func nativeS6RecordGuidance(root string, state map[string]any, r assignmentrecord.Record) string {
	brief, _, _ := assignmentrecord.NativePaths(r)
	agent := contextAgent(state, r.OwnerAgentID)
	if workstate.AgentActivity(agent) == "done" {
		return fmt.Sprintf("S6 %s: Main acknowledged the original delivery; continue the reviewed batch.", r.Task.ID)
	}
	if activity := workstate.AgentActivity(agent); activity == "stopped" || activity == "cancelled" {
		return fmt.Sprintf("S6 %s: original responsibility is closed; preserve its history without re-dispatching.", r.Task.ID)
	}
	if ref := stringValue(agent["completion_reported_ref"]); ref != "" {
		return nativeS6ReceivedGuidance(root, state, r, ref)
	}
	if submission.PlanningDiagnostic(root, state, nativeS6DiagnosticKind(r)) != "" {
		return nativeS6ReturnDiagnostic(root, state, r)
	}
	return fmt.Sprintf("S6 %s: original brief is prepared at %s for Claude Code native Agent (%s) with worktree isolation. Preparation does not prove the Agent has started. Main dispatches this exact brief if no actual native task exists, otherwise follows that task's execution/resume and actual blocker. Builder owns professional plan, implementation/self-checks and Result; SubagentStop receives delivery. No workspace launcher or activation is required.", r.Task.ID, filepath.Join(root, brief), r.RoleFamily)
}

func nativeS6Assignment(state map[string]any, owner string) bool {
	r, found, err := assignmentrecord.ForOwner(state, owner)
	return err == nil && found && r.Contract == assignmentrecord.NativeDeliveryContract
}
