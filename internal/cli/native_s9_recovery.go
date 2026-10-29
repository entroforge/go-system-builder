package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func nativeS9Capture(captures []*nativeS9AttemptCapture) *nativeS9AttemptCapture {
	if len(captures) == 0 {
		return nil
	}
	return captures[0]
}

// Only the exact private native Result coordinate or original Targeted request
// enters this adapter. Typed paths retain their existing submission selector.
func consumeNativeS9Original(ctx context.Context, service *submission.Service, path string) (any, bool, error) {
	flat := (strings.Contains(filepath.ToSlash(path), "/.claude/submissions/native-s9/") || strings.Contains(filepath.ToSlash(path), "/.loop-submissions/native-s9/")) && filepath.Base(path) == "result.json"
	targeted := (strings.Contains(filepath.ToSlash(path), "/.claude/submissions/s9/") && strings.HasPrefix(filepath.Base(path), "targeted-reverification-") || strings.Contains(filepath.ToSlash(path), "/.loop-submissions/s9/") && strings.HasPrefix(filepath.Base(path), "targeted-")) && strings.HasSuffix(path, ".json")
	if !flat && !targeted {
		return nil, false, nil
	}
	snap, err := runtime.NewWriter(service.StatePath, service.JournalPath, service.Root, service.Validator).WithContext(ctx).Snapshot()
	if err != nil {
		return nil, true, err
	}
	if !runtime.PacketEnabled(snap.State) {
		return nil, true, fmt.Errorf("original S9 input has no current packet-enabled responsibility")
	}
	var feedback bytes.Buffer
	if targeted {
		selections, err := repairTargetedSelections(service.Root, snap.State)
		if err != nil {
			return nil, true, err
		}
		for _, selection := range selections {
			ref, err := repairTargetedQuestionnairePath(service.Root, snap.State, selection.performingAssignmentID)
			if err != nil {
				return nil, true, err
			}
			if filepath.Clean(ref) != filepath.Clean(path) {
				continue
			}
			if accepted, ok := registeredTargetedForSelection(service.Root, repairPointerCLI(snap.State), selection); ok {
				return map[string]any{"input_ref": path, "accepted_ref": accepted.Path, "next_action": "Targeted is already received. " + repairHandoffProjectionContext(service.Root, snap.State)}, true, nil
			}
			status := stringValue(repairPointerCLI(snap.State)["status"])
			if status != "targeted_reverification" && status != "blocked" {
				return nil, true, fmt.Errorf("Targeted answer has no current unreceived independent responsibility or accepted receipt")
			}
			capture := newNativeS9AttemptCapture(ctx, service.Root, snap, ref, selection.verifierOwner, "Targeted")
			capture.SetResponsibilityRef(ref)
			err = receiveNativeS9Targeted(ctx, service.Root, snap, ref, &feedback, capture)
			if publishErr := capture.Publish(ctx, err); publishErr != nil {
				fmt.Fprintln(&feedback, "S9 reception diagnostic could not be delivered:", publishErr)
			}
			return map[string]any{"input_ref": path, "next_action": strings.TrimSpace(feedback.String())}, true, err
		}
		return nil, true, fmt.Errorf("Targeted answer path has no exact current independent responsibility")
	}
	life := mapFieldCLI(snap.State, "lifecycle")
	if life["state"] != "bug_resolution" || !s9ProfessionalPhase(stringValue(life["phase"])) {
		return nil, true, fmt.Errorf("native Result has no current professional S9 responsibility")
	}
	var selected *workpacket.RepairPlanReportPacket
	var source string
	packets, err := runtime.CurrentRepairPlanReportPacketsContext(ctx, service.Root, snap.State, "")
	if err != nil {
		return nil, true, err
	}
	for _, packet := range packets {
		if filepath.Base(filepath.Dir(path)) != packet.ID {
			continue
		}
		_, result, err := nativeS9AuthorPaths(service.Root, packet)
		if err != nil {
			return nil, true, fmt.Errorf("read frozen native author contract: %w", err)
		}
		suffix := string(filepath.Separator) + filepath.FromSlash(result)
		if !strings.HasSuffix(path, suffix) {
			continue
		}
		prepared, err := nativeS9RecoveryPrepared(ctx, service.Root, snap.State, packet)
		if err != nil {
			return nil, true, err
		}
		if !prepared {
			return nil, true, fmt.Errorf("original Plan packet has no verified native preparation; preserve its frozen/legacy consumer")
		}
		if selected != nil {
			return nil, true, fmt.Errorf("native Result path selects multiple current responsibilities")
		}
		packet := packet
		selected, source = &packet, strings.TrimSuffix(path, suffix)
	}
	if selected == nil {
		return nil, true, fmt.Errorf("native Result path has no exact current original Plan packet")
	}
	// CanonicalProspective permits a cleaned accepted source; the frozen
	// execution path is still checked by the shared receiver before projection.
	source, err = workspace.CanonicalProspective(source)
	if err != nil {
		return nil, true, err
	}
	capture := newNativeS9AttemptCapture(ctx, service.Root, snap, selected.ID, selected.Binding.OwnerAgentID, "Builder")
	brief, _, _ := nativeS9Paths(*selected)
	capture.SetResponsibilityRef(filepath.Join(service.Root, brief))
	capture.SetInput(path)
	err = receiveNativeS9BuilderFrom(ctx, service.Root, snap.State, *selected, source, true, &feedback, capture)
	if publishErr := capture.Publish(ctx, err); publishErr != nil {
		fmt.Fprintln(&feedback, "S9 reception diagnostic could not be delivered:", publishErr)
	}
	return map[string]any{"input_ref": path, "next_action": strings.TrimSpace(feedback.String())}, true, err
}

// Technical recovery retries the saved typed bytes. A real author event may
// adapt a changed original only after the original Candidate artifacts establish
// that no attempt was staged. A pending pair is handled by the original Writer
// before this reader.
func recoverNativeS9TypedResult(ctx context.Context, service *submission.Service, state map[string]any, plan workpacket.RepairPlanReportPacket, source string, projectAccepted, authorEvent bool, diagnostic io.Writer, capture *nativeS9AttemptCapture) (bool, error) {
	packets, err := runtime.CurrentRepairResultPacketsContext(ctx, service.Root, state, plan.Binding.OwnerAgentID)
	if err != nil {
		return false, err
	}
	for _, packet := range packets {
		if packet.Binding.Sources.RepairAssignmentID != plan.Binding.RepairAssignmentID {
			continue
		}
		registry, err := workspace.Decode(state)
		if err != nil {
			return true, err
		}
		if registry == nil {
			return true, fmt.Errorf("original typed Result has no registered native source")
		}
		execution, ok := registry.Executions[plan.Binding.ManifestAssignmentID]
		if !ok || execution.Path != source || execution.AgentID != plan.Binding.OwnerAgentID || execution.RuntimeID != plan.Binding.RuntimeID || execution.BaselineGeneration != plan.Binding.BaselineGeneration || execution.Purpose != workspace.NativeDeliveryPurpose {
			return true, fmt.Errorf("original native Result source differs from its frozen execution")
		}
		receipt, err := service.WithContext(ctx).RepairResultReceipt(plan.Binding.OwnerAgentID, packet.ID)
		if err != nil {
			return true, err
		}
		if receipt.Accepted && (receipt.ResponsibilityStatus == "main_result_accepted" || receipt.ResponsibilityStatus == "candidate_accepted" || receipt.WorkComplete) {
			if !projectAccepted {
				return false, nil
			}
			// Historical C acceptance still owns its original retained source
			// contract, including the issued Plan identity and surviving inputs.
			// Do not call ReadRepairResultInput here: it can publish a typed wrapper
			// and is reserved for resuming an unfinished original transaction.
			if repair.NativeS9SourceReference(execution).Path != "" {
				if _, err := nativeS9RetainedSource(ctx, service.Root, state, plan, source); err != nil {
					return true, err
				}
			}
			// An accepted source may have been reclaimed. Present originals still
			// have to match the original candidate bytes; never adopt a new draft.
			if execution.DeliveryRef != "" {
				raw, err := workpacket.ReadFile(service.Root, execution.DeliveryRef, 20<<20)
				if err != nil {
					return true, err
				}
				if execution.DeliverySHA256 == "" || workpacket.Digest(raw) != execution.DeliverySHA256 {
					return true, fmt.Errorf("original accepted candidate bytes changed")
				}
				var candidate repair.WorkspaceCandidate
				if err := json.Unmarshal(raw, &candidate); err != nil {
					return true, err
				}
				if _, err := repair.NativeS9PrivateProjection(ctx, service.Root, packet, execution, candidate, true); err != nil {
					return true, err
				}
			}
			fmt.Fprintln(diagnostic, receipt.NextAction)
			return true, nil
		}
		locked, unlock, err := integration.LockWorkspace(ctx, service.Root)
		if err != nil {
			return true, err
		}
		defer unlock()
		ctx = locked
		// Another original consumer may have committed while this event waited
		// for the workspace lock. Never adapt against the old snapshot.
		fresh, err := runtime.NewStore(service.StatePath, service.JournalPath).WithContext(ctx).Snapshot()
		if err != nil {
			return true, err
		}
		currentPackets, err := runtime.CurrentRepairResultPacketsContext(ctx, service.Root, fresh.State, plan.Binding.OwnerAgentID)
		if err != nil {
			return true, err
		}
		current := false
		for _, item := range currentPackets {
			if item.ID == packet.ID {
				current = true
				break
			}
		}
		if !current {
			return true, fmt.Errorf("original typed Result no longer identifies the current responsibility")
		}
		freshRegistry, err := workspace.Decode(fresh.State)
		if err != nil || freshRegistry == nil {
			return true, fmt.Errorf("original native source is unavailable: %v", err)
		}
		freshExecution, found := freshRegistry.Executions[plan.Binding.ManifestAssignmentID]
		if !found || freshExecution.Path != execution.Path || freshExecution.AgentID != execution.AgentID || freshExecution.RuntimeID != execution.RuntimeID || freshExecution.BaselineGeneration != execution.BaselineGeneration || freshExecution.Generation != execution.Generation || freshExecution.ReworkRef != execution.ReworkRef || freshExecution.Purpose != workspace.NativeDeliveryPurpose {
			return true, fmt.Errorf("original native Result execution changed before recovery")
		}
		receipt, err = service.WithContext(ctx).RepairResultReceipt(plan.Binding.OwnerAgentID, packet.ID)
		if err != nil {
			return true, err
		}
		if receipt.Accepted && (receipt.ResponsibilityStatus == "main_result_accepted" || receipt.ResponsibilityStatus == "candidate_accepted" || receipt.WorkComplete) {
			return recoverNativeS9TypedResult(ctx, service, fresh.State, plan, source, projectAccepted, authorEvent, diagnostic, capture)
		}
		state, execution = fresh.State, freshExecution
		path := filepath.Join(source, packet.AnswerPath)
		diagnosticPath := path
		if retained, err := repair.RetainedNativeS9Source(ctx, service.Root, execution, packet.Binding.Sources); err != nil {
			return true, err
		} else if retained != nil {
			diagnosticPath = filepath.Join(service.Root, repair.NativeS9TypedInputPath(retained, packet))
		}
		if err := ctx.Err(); err != nil {
			return true, err
		}
		raw, err := repair.ReadRepairResultInput(ctx, service.Root, state, packet, execution)
		if err != nil && capture != nil {
			capture.ObserveInputError(diagnosticPath, err)
		}
		if os.IsNotExist(err) {
			// An issued slot may already have an immutable staged candidate even
			// before the Writer publishes its pending marker. Without its exact
			// typed bytes, a mutable flat file cannot replace that original input.
			if nativeS9SavedTypedAttempt(ctx, service.Root, state, plan, path) {
				if capture != nil {
					capture.SetTypedInput(diagnosticPath)
					capture.SetStage("typed_result_missing")
				}
				return true, fmt.Errorf("original saved typed Result is missing at %s; preserve staged artifacts and restore its original saved input or paired Runtime pending write: %w", path, err)
			}
			// A crash can leave the original Candidate before either its
			// pending pair or diagnostic exists. A missing typed path alone is
			// not proof that the issued draft has never been adapted.
			staged, stageErr := repair.FindNativeS9StagedCandidate(ctx, service.Root, packet, execution)
			if stageErr != nil {
				return true, fmt.Errorf("original typed Result is missing and its staged Candidate could not be verified; preserve the original input: %v", stageErr)
			}
			if staged != "" {
				return true, fmt.Errorf("original typed Result is missing at %s; original Candidate is staged at %s; restore its original saved input or paired Runtime pending write", path, staged)
			}
			return false, nil
		}
		if err != nil {
			return true, err
		}
		if capture != nil {
			capture.SetTypedInput(diagnosticPath)
			capture.ObserveInput(diagnosticPath, raw)
		}
		if !nativeS9TypedInputSaved(packet, raw, service.Root) {
			return false, nil
		}
		if capture != nil {
			capture.SetStage("typed_result_saved")
		}
		if authorEvent {
			if handled, correctionErr := receiveNativeS9CorrectedOriginal(ctx, service, plan, packet, execution, source, raw, diagnostic, capture); handled {
				return true, correctionErr
			}
		}
		if capture != nil {
			capture.EnterConsumer("typed_result_consumer")
		}
		receipt, err = service.SaveRepairResultFrom(ctx, plan.Binding.OwnerAgentID, path, source, nil)
		if err != nil {
			return true, err
		}
		if !receipt.Accepted {
			return true, fmt.Errorf("original RepairResult was not accepted: %s", receipt.Error)
		}
		if capture != nil {
			capture.ConsumerSucceeded()
		}
		fmt.Fprintln(diagnostic, receipt.NextAction)
		return true, nil
	}
	return false, nil
}

// The original workspace lock remains held from Candidate verification through
// any adaptation and the original consumer invocation.
func receiveNativeS9CorrectedOriginal(ctx context.Context, service *submission.Service, plan workpacket.RepairPlanReportPacket, packet workpacket.RepairResultPacket, execution workspace.Execution, source string, typed []byte, diagnostic io.Writer, capture *nativeS9AttemptCapture) (bool, error) {
	_, result, err := nativeS9AuthorPaths(service.Root, plan)
	if err != nil {
		return true, err
	}
	flat, err := workpacket.ReadFile(source, result, 20<<20)
	if err != nil || workpacket.ValidateUniqueJSON(flat) != nil {
		return false, nil
	}
	var header workpacket.RepairResultProfessionalAnswer
	if json.Unmarshal(flat, &header) != nil || strings.TrimSpace(header.Verdict) == "" {
		return false, nil
	}
	var previous struct {
		Answer json.RawMessage `json:"answer"`
	}
	var oldAnswer, newAnswer any
	if json.Unmarshal(typed, &previous) != nil || json.Unmarshal(previous.Answer, &oldAnswer) != nil || json.Unmarshal(flat, &newAnswer) != nil || reflect.DeepEqual(oldAnswer, newAnswer) {
		return false, nil
	}
	staged, err := repair.FindNativeS9StagedCandidate(ctx, service.Root, packet, execution)
	if err != nil {
		return true, fmt.Errorf("original staged Candidate could not be verified; preserve the original typed Result: %w", err)
	}
	if staged != "" {
		return true, fmt.Errorf("original Candidate is already staged at %s; preserve its typed Result and resume the original transaction", staged)
	}
	currentTyped, err := workpacket.ReadFile(source, packet.AnswerPath, 20<<20)
	if err != nil || !bytes.Equal(currentTyped, typed) {
		return true, fmt.Errorf("original typed Result changed while checking its staged Candidate: %w", runtime.ErrSubmissionConflict)
	}
	currentFlat, err := workpacket.ReadFile(source, result, 20<<20)
	if err != nil || !bytes.Equal(currentFlat, flat) {
		return true, fmt.Errorf("professional original changed while checking its staged Candidate: %w", runtime.ErrSubmissionConflict)
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	draft := packet.Draft
	draft.Answer = append(json.RawMessage(nil), flat...)
	typedBytes, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return true, err
	}
	typedBytes = append(typedBytes, '\n')
	path := filepath.Join(source, packet.AnswerPath)
	if err := os.WriteFile(path, typedBytes, 0o644); err != nil {
		return true, err
	}
	if capture != nil {
		capture.SetInput(filepath.Join(source, result))
		capture.ObserveInput(filepath.Join(source, result), flat)
		capture.SetTypedInput(path)
		capture.ObserveInput(path, typedBytes)
		capture.EnterConsumer("typed_result_consumer")
	}
	receipt, err := service.SaveRepairResultFrom(ctx, plan.Binding.OwnerAgentID, path, source, nil)
	if err != nil {
		return true, nativeS9FlatArtifactError("Result", filepath.Join(source, result), err)
	}
	if !receipt.Accepted {
		return true, fmt.Errorf("original RepairResult was not accepted: %s", receipt.Error)
	}
	if capture != nil {
		capture.ConsumerSucceeded()
		capture.SetStage("result_received")
	}
	nativeS9WorkerMessage(ctx, fmt.Sprintf("S9 Result for %s was accepted by the original consumer from %s. Your original submission is received. Main owns the subsequent integration and checks; keep the original artifacts available.", plan.Binding.OwnerAgentID, filepath.Join(source, result)))
	fmt.Fprintln(diagnostic, "Native S9 professional Result received; Main owns original integration and checks. "+receipt.NextAction)
	return true, nil
}

// nativeS9TypedInputSaved compares the original issued draft with bytes already
// read by the caller. It performs no I/O and makes no receipt/pending claim.
// Malformed non-draft bytes remain the original typed consumer's responsibility.
func nativeS9TypedInputSaved(packet workpacket.RepairResultPacket, raw []byte, roots ...string) bool {
	draft, err := json.Marshal(packet.Draft)
	if err != nil {
		return true
	}
	var actual, empty any
	if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(draft, &empty) != nil {
		return true
	}
	if reflect.DeepEqual(actual, empty) {
		return false
	}
	// The original issuer also publishes a display draft whose read references
	// are absolute Main paths. It is still an empty draft, not a saved attempt.
	if len(roots) != 0 {
		root, err := filepath.Abs(roots[0])
		if err != nil {
			return true
		}
		display := packet.Draft
		display.Read = append([]workpacket.ReadItem(nil), packet.Draft.Read...)
		for i := range display.Read {
			if !filepath.IsAbs(display.Read[i].Path) {
				display.Read[i].Path = filepath.Join(root, filepath.FromSlash(display.Read[i].Path))
			}
		}
		encoded, err := json.Marshal(display)
		if err != nil || json.Unmarshal(encoded, &empty) != nil {
			return true
		}
		if reflect.DeepEqual(actual, empty) {
			return false
		}
	}
	return true
}

// This observes a prior successful typed write, including when the current
// file has disappeared. It supplies no answer bytes or source authorization.
// Recovery uses the stable original binding to name the missing
// saved input and refuses to substitute a mutable private answer.
func nativeS9SavedTypedAttempt(ctx context.Context, root string, state map[string]any, plan workpacket.RepairPlanReportPacket, path string) bool {
	if ctx.Err() != nil {
		return false
	}
	raw, err := workpacket.ReadFile(root, nativeS9DiagnosticPath(plan.ID), 64<<10)
	if err != nil {
		return false
	}
	var value nativeS9Diagnostic
	if json.Unmarshal(raw, &value) != nil || value.Runtime != stringValue(state["runtime_id"]) || value.Scope != nativeS9DiagnosticScopeContext(ctx, root, state, plan.ID) || value.Attempt == nil {
		return false
	}
	for _, observation := range []nativeS9ConsumerFailure{
		{Message: value.Message, Files: value.Files, Attempt: value.Attempt},
		func() nativeS9ConsumerFailure {
			if value.ConsumerFailure != nil {
				return *value.ConsumerFailure
			}
			return nativeS9ConsumerFailure{}
		}(),
	} {
		attempt := observation.Attempt
		if attempt == nil {
			continue
		}
		observed := observation.Files[path]
		saved := (attempt.Stage == "typed_result_saved" || attempt.Stage == "typed_result_consumer" || attempt.Stage == "result_received") && len(observed) == 64
		missingSaved := attempt.Stage == "typed_result_missing" && observed == "missing"
		if attempt.Kind == "Builder" && attempt.Owner == plan.Binding.OwnerAgentID && attempt.TypedInputRef == path && (saved || missingSaved) {
			return true
		}
	}
	return false
}

// A display brief may disappear after the Plan has been received. Select from
// original packet coordinates and native preparation evidence; an existing
// frozen independent execution always retains its original consumer.
func nativeS9RecoveryPrepared(ctx context.Context, root string, state map[string]any, plan workpacket.RepairPlanReportPacket) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return false, err
	}
	if registry != nil {
		if execution, ok := registry.Executions[plan.Binding.ManifestAssignmentID]; ok {
			if execution.Purpose != workspace.NativeDeliveryPurpose || execution.BootstrapSHA256 != "" || execution.PlatformSessionID != "" {
				return false, nil
			}
			b := plan.Binding
			if execution.AgentID != b.OwnerAgentID || execution.RuntimeID != b.RuntimeID || execution.BaselineGeneration != b.BaselineGeneration {
				return false, fmt.Errorf("original native preparation differs from the current Plan responsibility")
			}
			return true, nil
		}
	}
	brief, pin, _ := nativeS9Paths(plan)
	info, err := os.Lstat(filepath.Join(root, pin))
	if err == nil {
		return info.Mode().IsRegular(), nil
	}
	if !os.IsNotExist(err) {
		return false, err
	}
	raw, err := workpacket.ReadFile(root, nativeS9DiagnosticPath(plan.ID), 64<<10)
	if err != nil {
		return false, nil
	}
	var value nativeS9Diagnostic
	if json.Unmarshal(raw, &value) != nil || value.Attempt == nil || value.Runtime != stringValue(state["runtime_id"]) || value.Scope != nativeS9DiagnosticScopeContext(ctx, root, state, plan.ID) {
		return false, nil
	}
	attempt := value.Attempt
	return attempt.Kind == "Builder" && attempt.Owner == plan.Binding.OwnerAgentID && attempt.ResponsibilityRef == filepath.Join(root, brief), nil
}

// Continue only an existing Stop-bound source for the actual completed issued
// task. Neither response prose nor an unregistered capture grants authority.
func continueNativeS9MainReturn(ctx context.Context, root string, in policy.Input, snap runtime.Snapshot, subject nativeReturnSubject) (runtime.Snapshot, bool, error) {
	if subject.kind != "s9-plan-report-v1" || in.Error != "" || in.ToolUseID == "" || nativeExecutionFromResponse(in.ToolResponse) != nativeExecutionCompleted {
		return snap, false, nil
	}
	if failed, present := in.ToolResponse["is_error"]; present && failed != false {
		return snap, false, nil
	}
	prompt, _ := in.ToolInput["prompt"].(string)
	if !nativeS6ExactReference(prompt, subject.ref) {
		return snap, false, nil
	}
	actor, _ := in.ToolResponse["agentId"].(string)
	if actor == "" || actor == "Main" || !runtime.PacketEnabled(snap.State) {
		return snap, false, nil
	}
	life := mapFieldCLI(snap.State, "lifecycle")
	if life["state"] != "bug_resolution" || !s9ProfessionalPhase(stringValue(life["phase"])) {
		return snap, false, nil
	}
	packets, err := runtime.CurrentRepairPlanReportPacketsContext(ctx, root, snap.State, "")
	if err != nil {
		return snap, false, nil
	}
	var selected *workpacket.RepairPlanReportPacket
	for _, packet := range packets {
		brief, _, _ := nativeS9Paths(packet)
		if packet.ID == subject.id && subject.ref == filepath.Join(root, brief) {
			if selected != nil {
				return snap, false, nil
			}
			p := packet
			selected = &p
		}
	}
	if selected == nil {
		return snap, false, nil
	}
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		return snap, false, nil
	}
	execution, ok := registry.Executions[selected.Binding.ManifestAssignmentID]
	if !ok || execution.DeliveryRef != "" || execution.Status == "complete" {
		return snap, false, nil
	}
	if repair.NativeS9SourceReference(execution).Path != "" {
		retained, err := nativeS9RetainedSource(ctx, root, snap.State, *selected, execution.Path)
		if err != nil {
			return snap, true, err
		}
		if retained == nil || retained.Actor != actor {
			return snap, false, nil
		}
	} else {
		if err := nativeS9CompletedLiveSource(ctx, root, snap.State, *selected, execution, in.ToolResponse); err != nil {
			return snap, true, err
		}
	}
	prepared, err := nativeS9RecoveryPrepared(ctx, root, snap.State, *selected)
	if err != nil {
		return snap, true, err
	}
	if !prepared {
		return snap, false, nil
	}
	var feedback bytes.Buffer
	capture := newNativeS9AttemptCapture(ctx, root, snap, selected.ID, selected.Binding.OwnerAgentID, "Builder")
	capture.SetResponsibilityRef(subject.ref)
	err = receiveNativeS9BuilderFrom(ctx, root, snap.State, *selected, execution.Path, true, &feedback, capture)
	if publishErr := capture.Publish(ctx, err); publishErr != nil {
		if err == nil {
			err = fmt.Errorf("S9 diagnostic publication: %w", publishErr)
		} else {
			err = fmt.Errorf("%w; S9 diagnostic publication: %v", err, publishErr)
		}
	}
	// Readers must project the new receipt, never the pre-consumption snapshot.
	if cached, ok := ctx.Value(nativeRoutingKey{}).(*nativeRoutingRead); ok {
		*cached = nativeRoutingRead{root: root}
	}
	latest, readErr := nativeRoutingSnapshot(root, in)
	if readErr != nil {
		if err == nil {
			err = readErr
		}
		return snap, true, err
	}
	refreshRecoveredHookReader(root, in)
	return latest, true, err
}

// The platform's structured completed response associates its actor with the
// actual worktree. Prose, a diagnostic, or file existence grants no source.
// Historical C sources keep their original retained-actor proof above.
func nativeS9CompletedLiveSource(ctx context.Context, root string, state map[string]any, packet workpacket.RepairPlanReportPacket, e workspace.Execution, response map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plan, _, err := nativeS9AuthorPaths(root, packet)
	if err != nil {
		return fmt.Errorf("read original native author contract: %w", err)
	}
	if plan == packet.AnswerPath {
		return fmt.Errorf("original frozen native source has no D completed-return contract; preserve its original recovery")
	}
	b := packet.Binding
	if e.Purpose != workspace.NativeDeliveryPurpose || e.BootstrapSHA256 != "" || e.PlatformSessionID != "" || e.AgentID != b.OwnerAgentID || e.RuntimeID != b.RuntimeID || e.BaselineGeneration != b.BaselineGeneration || e.AssignmentID != b.ManifestAssignmentID {
		return fmt.Errorf("completed return differs from its original native execution responsibility")
	}
	_, pin, _ := nativeS9Paths(packet)
	raw, err := workpacket.ReadFile(root, pin, 4096)
	if err != nil {
		return err
	}
	if e.Inputs[pin] == "" || e.Inputs[pin] != workpacket.Digest(raw) {
		return fmt.Errorf("completed return has no unchanged original D source pin")
	}
	path, _ := response["worktreePath"].(string)
	branch, _ := response["worktreeBranch"].(string)
	if !filepath.IsAbs(path) || branch == "" {
		return fmt.Errorf("completed return lacks structured worktreePath/worktreeBranch; original live source continuation is unproven")
	}
	canonical, err := workspace.Canonical(path)
	if err != nil {
		return fmt.Errorf("completed return source is unavailable: %w", err)
	}
	if canonical != e.Path || branch != e.Branch {
		return fmt.Errorf("completed return worktree differs from its original registered source")
	}
	if err := runtime.VerifyRepairPlanReportPacketIssueContext(ctx, root, state, packet); err != nil {
		return err
	}
	if err := runtime.VerifyRepairPlanReportCurrent(root, state, packet, ctx); err != nil {
		return err
	}
	registry, err := workspace.Decode(state)
	if err != nil || registry == nil {
		return fmt.Errorf("original native source registry is unavailable: %v", err)
	}
	if err := registry.Validate(ctx, root); err != nil {
		return err
	}
	return registry.ValidateExecution(ctx, e, false)
}
