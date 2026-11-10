package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func receiveSavedQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	// Script/MCP tools can return the actual saved path. This is the same
	// existing path selector, not a shell parser or a native identity mapping.
	if _, saved := savedQuestionnairePath(request); !saved {
		if path, ok := request.ToolResponse["file_path"].(string); ok {
			located := request
			located.ToolInput = map[string]any{"file_path": path}
			if actual, ok := savedQuestionnairePath(located); ok && (isS9RepairPlanReportPath(actual) || isS9RepairResultPath(actual) || isS9RepairResultResolutionPath(actual) || (strings.Contains(filepath.ToSlash(actual), "/.claude/submissions/s9/") || strings.Contains(filepath.ToSlash(actual), "/.loop-submissions/s9/"))) {
				request = located
			}
		}
	}
	// The original consumer signs for the registered owner of the exact saved
	// answer it selects. It may only complete that responsibility when the
	// platform event identity is that owner.
	if !savedQuestionnaireEventOwnsAnswer(root, request) {
		return false, 0
	}
	for _, receive := range []func(string, policy.Input, io.Writer, io.Writer) (bool, int){
		runSavedEntryQuestionnaire, runSavedS5ManifestQuestionnaire, runSavedRepairDispatch, runSavedRepairVerifierDispatch,
		runSavedInvestigationLeadQuestionnaire, runSavedRepairLeadQuestionnaire,
		runSavedReviewPlanQuestionnaire, runSavedAuditQuestionnaire, runSavedHumanReleaseQuestionnaire,
		runSavedBuilderQuestionnaire, runSavedStageAnswerHook,
	} {
		var output bytes.Buffer
		if handled, code := receive(root, request, &output, stderr); handled {
			_, _ = io.Copy(stdout, &output)
			return true, code
		}
	}
	return false, 0
}

// savedQuestionnaireEventOwnsAnswer reports whether the platform event
// identity can be attributed to the registered original owner of the exact
// saved answer that the event carries.
//
// The original consumer acts for the registered owner of that answer path.
// The platform event identity and the recorded original responsibility are the
// only facts that distinguish a known event: a payload that carries another
// agent's identity is that agent's own event and never completes the original
// owner's responsibility, and an event without an identity is never completed
// from the answer path alone. No revision, hash, digest, checksum, fingerprint
// or round is compared, and no owner is inferred from a path, a filename, a
// recency order or a unique candidate.
func savedQuestionnaireEventOwnsAnswer(root string, request policy.Input) bool {
	path, saved := savedQuestionnairePath(request)
	if !saved || !savedQuestionnaireWorkerAnswerPath(path) {
		return true
	}
	owner, registered := savedQuestionnaireOwner(root, path)
	if !registered {
		// No current registered issuance owns this exact path. The original
		// consumer reports that exact fact and never signs for another
		// responsibility.
		return true
	}
	if owner == "" {
		// The path is registered, but its recorded owner is ambiguous. The
		// event cannot be attributed to one original responsibility.
		return false
	}
	if owner == "Main" {
		return true
	}
	actor := request.EffectiveAgentID()
	if actor == "" {
		// An event without a platform identity cannot be attributed to the
		// original owner. A recorded session association has already supplied
		// that identity before this consumer is reached.
		return false
	}
	return actor == owner
}

// savedQuestionnaireWorkerAnswerPath reports whether the exact saved answer
// path belongs to a Worker-owned original questionnaire family. Main's own
// answers keep their existing Main-viewer guard.
func savedQuestionnaireWorkerAnswerPath(path string) bool {
	return isS8HypothesisResultPath(path) || isS9RepairPlanReportPath(path) || isS9RepairResultPath(path)
}

// savedQuestionnaireOwner resolves the registered original owner of an exact
// saved answer path.
//
// Only the current registered issuances decide this answer: the Runtime's own
// work-packet index carries the exact answer path and its registered original
// owner. An empty owner with a registered answer means that the recorded
// issuances disagree, so no single original responsibility can be attributed.
// The owner is never inferred from a path convention, a filename, a recency
// order or a unique candidate.
func savedQuestionnaireOwner(root string, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return "", false
	}
	roots := savedQuestionnaireAnswerRoots(root, snapshot.State)
	owner := ""
	registered := false
	for _, row := range objectSliceAny(mapFieldCLI(snapshot.State, "entities")["work_packets"]) {
		ref := stringValue(row["answer_path"])
		if ref == "" {
			continue
		}
		matched := false
		for _, candidateRoot := range roots {
			if samePath(filepath.Join(candidateRoot, filepath.FromSlash(ref)), path) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		registered = true
		candidate := savedQuestionnaireBindingOwner(row)
		if candidate == "" {
			continue
		}
		if owner != "" && owner != candidate {
			// Two recorded original responsibilities claim the same exact
			// answer path. Neither one can be attributed to this event.
			return "", true
		}
		owner = candidate
	}
	return owner, registered
}

// savedQuestionnaireAnswerRoots lists the recorded roots an original answer
// file may live in: the Main root and the registered Worker checkout of every
// recorded execution. They are recorded locations, never candidates inferred
// from the answer path.
func savedQuestionnaireAnswerRoots(root string, state map[string]any) []string {
	roots := []string{root}
	registry, err := workspace.Decode(state)
	if err != nil || registry == nil {
		return roots
	}
	for _, execution := range registry.AllExecutions() {
		if execution.Path != "" {
			roots = appendUnique(roots, execution.Path)
		}
	}
	return roots
}

// savedQuestionnaireBindingOwner reports the registered original owner of a
// work-packet index row.
func savedQuestionnaireBindingOwner(row map[string]any) string {
	binding := mapFieldCLI(row, "binding")
	if owner := stringValue(binding["owner_agent_id"]); owner != "" {
		return owner
	}
	if owner := stringValue(binding["owner"]); owner != "" {
		return owner
	}
	if owner := stringValue(mapFieldCLI(binding, "sources")["owner_agent_id"]); owner != "" {
		return owner
	}
	return ""
}

type observedQuestionnaire struct {
	professionalWorker bool
	path               string
	draft              any
	s6Result           bool
	s7Plan             bool
	s8Packet           *workpacket.HypothesisResultPacket
	s8AcceptedAnswer   json.RawMessage
	s8Case             map[string]any
	s9Kind             string
	s9Packet           *s9ListenerQuestionnaire
}

// A successful script or MCP tool can save a questionnaire without exposing a
// file_path. Read the graph's existing exact files, never parse command text or
// invent a tool policy. Unchanged issued drafts do not invoke a domain consumer.
func runObservedQuestionnaireSaves(root string, request policy.Input, stdout, stderr io.Writer, progress ...bool) (bool, int) {
	return runObservedQuestionnaireSavesForOwner(root, request, stdout, stderr, "", progress...)
}

// owner is an already resolved frozen S6 responsibility, never a candidate
// inferred from the stage or from a questionnaire path.
func runObservedQuestionnaireSavesForOwner(root string, request policy.Input, stdout, stderr io.Writer, owner string, progress ...bool) (bool, int) {
	if owner == "" && s2PendingMaterialSave(request, root) {
		if receipt, path, attempted, err := continueOriginalS2Pending(root, request); attempted {
			if err != nil {
				fmt.Fprintf(stderr, "S2 original followup pending: %v\n", err)
				if receipt.Error == "" {
					receipt.Error = err.Error()
				}
			}
			return true, renderStageAnswerHook(stdout, path, receipt, "S2")
		}
	}
	// A successful Post can finish an already recorded S4 responsibility even
	// when it did not save a file. Keep its real tool identity; this is the
	// original pending followup, never a Read promoted into a new submission.
	if request.Event == "PostToolUse" && request.Error == "" {
		failed, _ := request.ToolResponse["is_error"].(bool)
		if owner := request.EffectiveAgentID(); !failed && (owner == "" || owner == "Main") {
			snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
			if err == nil && owner == "Main" && mapFieldCLI(snapshot.State, "lifecycle")["state"] == "document_verification" && s5HasAcceptedPlanning(snapshot.State) {
				packets, packetErr := observedS5Packets(snapshot.State)
				if packetErr == nil && len(packets) < 2 {
					ctx, cancel := context.WithTimeout(contextForHook(request), 12*time.Second)
					prepareErr := prepareS5ReviewResponsibilities(ctx, root, snapshot.State)
					cancel()
					_, message, observeErr := s5IssueHandoffContext(contextForHook(request), root, prepareErr)
					if prepareErr != nil {
						fmt.Fprintf(stderr, "S5 original preparation pending: %v\n", prepareErr)
					}
					if observeErr != nil {
						fmt.Fprintf(stderr, "S5 original preparation observation: %v\n", observeErr)
					}
					_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": message}})
					return true, 0
				}
			}
			if err == nil && mapFieldCLI(snapshot.State, "lifecycle")["phase"] == "tasks" {
				row, rowErr := registeredPlanningRow(snapshot.State)
				if rowErr == nil && row != nil {
					record, found, findErr := runtime.FindSubmission(snapshot.State, stringValue(row["packet_id"]))
					if findErr == nil && found && (record.ControlStatus == "pending" || record.ControlStatus == "no_transition") {
						path := filepath.Join(root, stringValue(row["answer_path"]))
						service, serviceErr := submission.New(root)
						if serviceErr == nil {
							ctx, cancel := context.WithTimeout(contextForHook(request), 12*time.Second)
							defer cancel()
							// Continue the durable original reception, without reading or
							// receiving the author's answer again on an unrelated Post.
							recovered, saveErr := service.Recover(ctx, "Main", 2)
							receipt, receiptErr := service.TaskPlanningReceipt("", record.PacketID)
							for _, candidate := range recovered {
								if candidate.PacketID == record.PacketID {
									receipt = candidate
									receiptErr = nil
									break
								}
							}
							if saveErr == nil {
								saveErr = receiptErr
							}
							if saveErr != nil {
								fmt.Fprintf(stderr, "S4 original followup pending: %v\n", saveErr)
								if receipt.Error == "" {
									receipt.Error = saveErr.Error()
								}
							}
							continueS5PlanningHandoff(contextForHook(request), root, &receipt)
							return true, renderStageAnswerHook(stdout, path, receipt, "S4")
						}
					}
				}
			}
		}
	}
	mainS9Impact := false
	if reader, ok := contextForHook(request).Value(hookReaderKey{}).(*hookReader); ok && reader.Kind == "Main" && reader.snapshotErr == nil {
		mainS9Impact = stringValue(repairPointerCLI(reader.snapshot.State)["session_id"]) != ""
	}
	if len(progress) == 2 && progress[0] && !progress[1] && !planningChangeFrom(request).Active && !mainS9Impact {
		return false, 0
	}
	switch request.ToolName {
	case "Read", "Glob", "Grep":
		return false, 0
	}
	if request.Event != "PostToolUse" || request.Error != "" {
		return false, 0
	}
	if failed, _ := request.ToolResponse["is_error"].(bool); failed {
		return false, 0
	}
	if _, explicit := savedQuestionnairePath(request); explicit && (len(progress) != 2 || !progress[0]) && !planningChangeFrom(request).Active {
		return false, 0
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return false, 0
	}
	life := mapFieldCLI(snapshot.State, "lifecycle")
	strictPlanning := life["state"] == "planning" && (life["phase"] == "design" || life["phase"] == "contracts")
	change := planningChangeFrom(request)
	if strictPlanning && (!change.Known || len(change.SavedAnswers) == 0) {
		return false, 0
	}
	var contexts []string
	seen := map[string]bool{}
	var leadPaths []string
	if owner == "" && currentAuditKind(snapshot.State) == "" {
		leadPaths = investigationLeadQuestionnairePaths(root, snapshot.State)
		leadPaths = append(leadPaths, repairDelegationApplicationPaths(root, snapshot.State)...)
	}
	var questionnaires []observedQuestionnaire
	if owner != "" {
		questionnaires = observableFrozenS6Questionnaires(root, snapshot, owner)
	} else {
		questionnaires = observableQuestionnaires(root, snapshot)
	}
	if owner == "" && mapFieldCLI(snapshot.State, "lifecycle")["state"] == "bug_resolution" && mapFieldCLI(snapshot.State, "lifecycle")["phase"] != "investigation" {
		questionnaires = s9ObservableQuestionnaires(root, snapshot.State, request)
	} else if owner == "" && mainS9Impact {
		// A late Main Impact update remains observable alongside the current
		// phase's questions; changing cursor alone does not close that work.
		for _, q := range s9ObservableQuestionnaires(root, snapshot.State, request) {
			if q.s9Kind == "impact" {
				questionnaires = append(questionnaires, q)
			}
		}
	}
questionnaires:
	for _, q := range questionnaires {
		if strictPlanning && !change.SavedAnswers[filepath.Clean(q.path)] {
			continue
		}
		// Main tools observe Main forms only. A ready Worker answer is not a
		// save by Main and must wait for its actual native SubagentStop.
		if mainQuestionnaireViewer(request) && q.professionalWorker {
			continue
		}

		if q.path == "" || seen[q.path] {
			continue
		}
		seen[q.path] = true
		if q.s9Packet != nil && q.s9Packet.result == nil {
			// This is the original issued Plan questionnaire, not native Result
			// delivery or Main's later integration. The native route runs first
			// and returns before this observer when it owns the event.
			if message := observeOriginalS9Plan(root, request, snapshot.State, q, stderr); message != "" {
				contexts = append(contexts, message)
			}
			continue
		}
		info, err := os.Stat(q.path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 20<<20 {
			continue
		}
		data, err := os.ReadFile(q.path)
		if err != nil {
			continue
		}
		if q.s9Kind != "" {
			unchanged, observationErr := s9ObservedQuestionnaireUnchangedContext(contextForHook(request), root, snapshot.State, q, data)
			if observationErr != nil {
				contexts = append(contexts, "S9 observation is incomplete for "+q.path+": its original registered execution projection is unavailable: "+observationErr.Error()+". Preserve the original responsibility and accepted work; no new submission or guessed command was inferred.")
				continue
			}
			if unchanged {
				continue
			}
		}
		if q.s9Packet != nil && q.draft == nil {
			draft, draftErr := s9ListenerQuestionnaireDraft(contextForHook(request), root, snapshot.State, *q.s9Packet)
			if draftErr != nil {
				contexts = append(contexts, "S9 observation is incomplete for "+q.path+": its recorded responsibility and paths remain known; comparison with the original draft is unknown within this request: "+draftErr.Error()+". Preserve the professional answer. No answer rejection or missing questionnaire was inferred.")
				continue
			}
			q.draft = draft
			unchanged, observationErr := s9ObservedQuestionnaireUnchangedContext(contextForHook(request), root, snapshot.State, q, data)
			if observationErr != nil {
				contexts = append(contexts, "S9 observation is incomplete for "+q.path+": "+observationErr.Error()+". Preserve the original responsibility and accepted work.")
				continue
			}
			if unchanged {
				continue
			}
		}
		for _, leadPath := range leadPaths {
			if !samePath(q.path, leadPath) {
				continue
			}
			if !investigationLeadQuestionnaireHasProfessionalContent(q.path, data) ||
				repairDelegationApplicationAlreadyRecorded(root, snapshot.State, q.path, data) ||
				s8ObservedHypothesesAlreadyRegistered(q.path, data, snapshot.State, q.s8Case) ||
				s8ObservedRouteAlreadyRegistered(root, q.path, data, snapshot.State, q.s8Case) ||
				s8ObservedContractAlreadyProjected(root, q.path, data, snapshot.State, q.s8Case) {
				continue questionnaires
			}
		}
		if q.s8Packet != nil && len(q.s8AcceptedAnswer) > 0 {
			answer, issues := workpacket.ValidateHypothesisResultAnswer(*q.s8Packet, data)
			var current, accepted workpacket.HypothesisResultAnswer
			if len(issues) == 0 && json.Unmarshal(answer, &current) == nil && json.Unmarshal(q.s8AcceptedAnswer, &accepted) == nil && sameS8ObservedAnswer(current, accepted) {
				continue // The original accepted content is unchanged; new content reaches the original consumer.
			}
		}
		var answer any
		if json.Unmarshal(data, &answer) == nil {
			if q.s6Result && !s6ObservedResultHasProfessionalContent(data) {
				continue
			}
			if q.s9Kind == "" && q.draft != nil && reflect.DeepEqual(answer, q.draft) {
				continue
			}
			if q.draft == nil && q.s9Kind == "" {
				fields, _ := answer.(map[string]any)
				if _, targeted := fields["performing_assignment_id"]; targeted && stringValue(fields["result"]) == "" {
					continue
				}
				changeTypes, _ := fields["change_types"].([]any)
				decisions, _ := fields["decisions"].([]any)
				if _, impact := fields["changed_artifacts"]; impact && len(changeTypes) == 0 && len(decisions) == 0 {
					continue
				}
			}
		}
		// Normalize an observed file save only for the existing adapters. The
		// actual host event and audit log keep their original tool identity.
		observed := request
		observed.ToolName = "Write"
		observed.ToolInput = map[string]any{"file_path": q.path}
		var out bytes.Buffer
		if q.s7Plan {
			// The script/MCP observation already read the original answer.
			// Reuse those bytes and the same consumer as the ordinary Pre and
			// explicit Post routes; do not invent a second save or read.
			result := receiveReviewPlanQuestionnaire(root, request, snapshot, q.path, data)
			if result.Context != "" {
				contexts = append(contexts, result.Context)
			}
			continue
		} else if q.s9Kind == "impact" {
			// This event already read the original Main answer; pass those same
			// bytes to its consumer instead of reopening it via a synthetic save.
			if handled, _ := saveRepairImpactQuestionnaireBytes(root, snapshot, q.path, data, &out, contextForHook(request)); !handled {
				continue
			}
		} else if handled, _ := receiveSavedQuestionnaire(root, observed, &out, stderr); !handled {
			continue
		}
		var envelope struct {
			Output struct {
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if json.Unmarshal(out.Bytes(), &envelope) == nil && envelope.Output.Context != "" {
			contexts = append(contexts, envelope.Output.Context)
		}
	}
	if len(contexts) == 0 {
		return false, 0
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": strings.Join(contexts, "\n")}})
	return true, 0
}

// Continue only the already received current Main S2 responsibility. In
// particular this never rereads the author's private answer to resume it.
func continueOriginalS2Pending(root string, request policy.Input) (submission.Receipt, string, bool, error) {
	if !mainQuestionnaireViewer(request) {
		return submission.Receipt{}, "", false, nil
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return submission.Receipt{}, "", false, nil
	}
	life := mapFieldCLI(snapshot.State, "lifecycle")
	if life["state"] != "planning" || life["phase"] != "design" {
		return submission.Receipt{}, "", false, nil
	}
	packet, found, err := runtime.CurrentIssuedDesignPacket(snapshot.State)
	if err != nil || !found || packet.AnswerPath == "" {
		return submission.Receipt{}, "", false, nil
	}
	record, found, err := runtime.FindSubmission(snapshot.State, packet.ID)
	if err != nil || !found || record.Owner != "Main" || record.ControlStatus != "pending" {
		return submission.Receipt{}, "", false, nil
	}
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
	ctx, cancel := context.WithTimeout(contextForHook(request), 12*time.Second)
	defer cancel()
	receipt, err := service.ContinueDesign(ctx, packet.ID)
	return receipt, filepath.Join(root, packet.AnswerPath), true, err
}

// s8ObservedAcceptedAnswer reuses the actual professional content already in
// the original Case/late-result fact. A historical digest alone proves nothing.
func s8ObservedAcceptedAnswer(document map[string]any, packet workpacket.HypothesisResultPacket, receipt runtime.HypothesisResultReceipt) json.RawMessage {
	if len(receipt.Answer) > 0 {
		return receipt.Answer
	}
	if len(receipt.SubmittedAnswer) > 0 {
		return receipt.SubmittedAnswer
	}
	rows := objectSliceAny(document["hypothesis_results"])
	for i := len(rows) - 1; i >= 0; i-- {
		raw, _ := rows[i]["receipt"].(map[string]any)
		if raw["packet_id"] != packet.ID {
			continue
		}
		data, err := json.Marshal(rows[i])
		var answer workpacket.HypothesisResultAnswer
		if err == nil && json.Unmarshal(data, &answer) == nil && answer.Result != nil {
			data, _ = json.Marshal(answer)
			return data
		}
	}
	return nil
}

func sameS8ObservedAnswer(a, b workpacket.HypothesisResultAnswer) bool {
	// Old Case rows can omit empty arrays; the authoring parser normalizes them.
	a.EvidenceRefs, b.EvidenceRefs = append([]string{}, a.EvidenceRefs...), append([]string{}, b.EvidenceRefs...)
	a.SourceBoundaryRefs, b.SourceBoundaryRefs = append([]string{}, a.SourceBoundaryRefs...), append([]string{}, b.SourceBoundaryRefs...)
	a.ExplainsFindingIDs, b.ExplainsFindingIDs = append([]string{}, a.ExplainsFindingIDs...), append([]string{}, b.ExplainsFindingIDs...)
	a.DoesNotExplain, b.DoesNotExplain = append([]string{}, a.DoesNotExplain...), append([]string{}, b.DoesNotExplain...)
	a.NewHypotheses, b.NewHypotheses = append([]workpacket.HypothesisNewQuestion{}, a.NewHypotheses...), append([]workpacket.HypothesisNewQuestion{}, b.NewHypotheses...)
	return reflect.DeepEqual(a, b)
}

// Distinguish an untouched machine draft from an expressed Claim without
// rebuilding sources. A complete Claim or professional oracle from any lens
// is observable even while other Claims remain placeholders.
func s7ObservedPlanHasProfessionalContent(data []byte) bool {
	var plan review.Plan
	if json.Unmarshal(data, &plan) != nil {
		return true
	}
	return review.HasProfessionalPlanContent(&plan)
}

func observableQuestionnaires(root string, snapshot runtime.Snapshot) []observedQuestionnaire {
	state := snapshot.State
	lifecycle, _ := state["lifecycle"].(map[string]any)
	stage, _, _ := projectNext(stringValue(lifecycle["state"]), stringValue(lifecycle["phase"]), root)
	if lifecycle["state"] == "inactive" {
		if path, err := bindingQuestionnairePath(root); err == nil {
			out := []observedQuestionnaire{{path: path}}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				for _, req := range scanRequirements(root) {
					if req.Status == "locked" {
						out = append(out, observedQuestionnaire{path: filepath.Join(root, filepath.FromSlash(req.Path))})
					}
				}
			}
			return out
		}
		return nil
	}
	if stage == "S0" || stage == "S1" {
		return nil
	}
	if lifecycle["state"] == "awaiting_human_release" {
		if path, _, err := humanReleaseQuestionnairePath(root, state); err == nil {
			return []observedQuestionnaire{{path: path}}
		}
		return nil
	}
	var out []observedQuestionnaire
	var s8Case map[string]any
	if stage == "S8" {
		s8Case, _ = readS8ListenerCase(root, state)
		paths := append(investigationLeadQuestionnairePaths(root, state), repairDelegationApplicationPaths(root, state)...)
		for _, path := range paths {
			out = append(out, observedQuestionnaire{path: path, s8Case: s8Case})
		}
	}
	if stage == "S7" {
		if !currentReviewPlanRegistered(state) {
			out = append(out, observedQuestionnaire{path: reviewPlanQuestionnairePath(root, state), s7Plan: true})
		}
	}
	if stage == "S9" {
		return nil // S9 passive probes need the actual current responsibility.
	}
	if kind := currentAuditKind(state); kind != "" {
		if path, err := auditQuestionnairePath(root, state, kind); err == nil {
			return []observedQuestionnaire{{path: filepath.Join(root, filepath.FromSlash(path))}}
		}
		return nil
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	registry, _ := workspace.Decode(state)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		template := stringValue(row["template_id"])
		if !strings.HasPrefix(template, strings.ToLower(stage)+"-") {
			continue
		}
		path, source := stringValue(row["answer_path"]), root
		binding, _ := row["binding"].(map[string]any)
		if template == "s8-hypothesis-result-v1" {
			pointer := investigationPointer(state)
			baseline, _ := state["baseline"].(map[string]any)
			if pointer == nil || binding["runtime_id"] != state["runtime_id"] || tolerantInt(binding["baseline_generation"]) != tolerantInt(baseline["generation"]) {
				continue
			}
		}
		if stage == "S2" || stage == "S3" || stage == "S4" {
			current, issued, verified, err := currentIssuedPlanningAnswerPath(state, stringValue(lifecycle["phase"]))
			if err != nil || !issued || !verified || path != current {
				continue
			}
		}
		if stage == "S7" {
			facts, _ := state["review"].(map[string]any)
			issued := reviewmeta.Identity{PlanID: stringValue(binding["review_plan_id"])}
			if reviewmeta.CompareIdentity(issued, reviewmeta.FromReview(facts)) != reviewmeta.Same {
				continue
			}
		}
		owner, assignment := stringValue(binding["owner_agent_id"]), stringValue(binding["assignment_id"])
		var acceptedPacket *workpacket.HypothesisResultPacket
		var acceptedAnswer json.RawMessage
		switch stringValue(row["template_id"]) {
		case "s7-review-result-v1", "s8-hypothesis-result-v1":
			if template == "s7-review-result-v1" {
				facts, _ := state["review"].(map[string]any)
				assignments, _ := facts["assignments"].(map[string]any)
				assignmentRow, _ := assignments[assignment].(map[string]any)
				if activity := workstate.ReviewAssignmentActivity(assignmentRow); activity == "consumed" || activity == "result_submitted" {
					continue
				}
			}
			var err error
			source, err = questionnaireSourceRoot(root, state, assignment, owner, stringValue(binding["runtime_id"]), tolerantInt(binding["baseline_generation"]))
			if err != nil {
				continue
			}
			if template == "s8-hypothesis-result-v1" && s8Case != nil {
				data, _ := json.Marshal(row)
				var packet workpacket.HypothesisResultPacket
				if json.Unmarshal(data, &packet) == nil {
					if receipt, accepted := s8ListenerReceipt(s8Case, packet); accepted && filepath.Clean(receipt.ExecutionRoot) == filepath.Clean(source) {
						acceptedPacket, acceptedAnswer = &packet, s8ObservedAcceptedAnswer(s8Case, packet, receipt)
					}
				}
			}
		case "s5-document-review-v1":
			data, _ := json.Marshal(row)
			var packet workpacket.Packet
			if json.Unmarshal(data, &packet) != nil {
				continue
			}
			if execution, exists, err := workspace.ReviewExecution(state, packet); err != nil {
				continue
			} else if exists {
				if registry == nil || execution.Status != "ready" {
					continue
				}
				paths, err := registry.WorkerSubmissionPaths(execution)
				if err != nil {
					continue
				}
				source, path = execution.Path, paths.Answer
			}
		}
		if path != "" {
			out = append(out, observedQuestionnaire{path: filepath.Join(source, filepath.FromSlash(path)), draft: row["draft"], s8Packet: acceptedPacket, s8AcceptedAnswer: acceptedAnswer, professionalWorker: template == "s5-document-review-v1" || template == "s7-review-result-v1" || template == "s8-hypothesis-result-v1"})
		}
	}
	if stage == "S6" {
		out = append(out, observableFrozenS6Questionnaires(root, snapshot, "")...)
	}
	return out
}

func observableFrozenS6Questionnaires(root string, snapshot runtime.Snapshot, owner string) []observedQuestionnaire {
	if mapFieldCLI(snapshot.State, "lifecycle")["state"] != "building" {
		return nil
	}
	state := snapshot.State
	var out []observedQuestionnaire
	questions, _ := projectS6BuilderQuestionnaires(root, owner, snapshot)
	for _, q := range questions {
		activity := workstate.AgentActivity(contextAgent(state, q.AgentID))
		if activity == "reported" || activity == "stopped" {
			continue
		}
		data, err := os.ReadFile(q.ContextPath)
		if err != nil {
			continue
		}
		var context map[string]any
		if json.Unmarshal(data, &context) != nil {
			continue
		}
		if activity != "working" {
			out = append(out, observedQuestionnaire{path: q.PlanPath, draft: context["plan_draft"], professionalWorker: true})
		}
		out = append(out, observedQuestionnaire{path: q.ResultPath, draft: context["result_draft"], s6Result: true, professionalWorker: true})
	}
	return out
}

// PLAN acceptance can change only machine-owned fields in a canonical S6
// Result. A generic successful-tool observer must not submit that projection
// as if the Builder had authored an answer. Explicit path saves still use the
// same original Result consumer and its normal validation.
func s6ObservedResultHasProfessionalContent(data []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return true // Keep malformed changed files on the original validation path.
	}
	if s6NonNull(fields["summary"]) || s6NonNull(fields["risks_blockers"]) || s6NonNull(fields["scope_deviations"]) {
		return true
	}
	// Frozen pre-Record completion reports express work through these original
	// fields. Empty default arrays alone do not make an untouched draft a report.
	for _, key := range []string{"changed_paths", "checks", "evidence_refs"} {
		if raw := bytes.TrimSpace(fields[key]); len(raw) > 0 && string(raw) != "null" && string(raw) != "[]" {
			return true
		}
	}
	if raw := fields["assertion_evidence"]; len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		var rows []struct {
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return true
		}
		for _, row := range rows {
			if len(row.EvidenceRefs) > 0 {
				return true
			}
		}
	}
	if raw := fields["closing_assessment"]; len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		var assessment struct {
			Disposition json.RawMessage `json:"disposition"`
			Conditions  []struct {
				Judgment  json.RawMessage `json:"judgment"`
				Rationale json.RawMessage `json:"rationale"`
			} `json:"conditions"`
		}
		if err := json.Unmarshal(raw, &assessment); err != nil {
			return true
		}
		if s6NonNull(assessment.Disposition) {
			return true
		}
		for _, condition := range assessment.Conditions {
			if s6NonNull(condition.Judgment) || s6NonNull(condition.Rationale) {
				return true
			}
		}
	}
	return false
}

func s6NonNull(raw json.RawMessage) bool {
	return len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null"
}

// Unchanged Main questions are already represented by the original Case and
// Runtime dispatch. This is an observer projection, never an acceptance check;
// explicit saves and any changed or incomplete registration keep the original
// consumer's complete validation and conflict feedback.
func s8ObservedHypothesesAlreadyRegistered(path string, data []byte, state, document map[string]any) bool {
	if filepath.Base(path) != "hypotheses.json" || document == nil || workpacket.ValidateUniqueJSON(data) != nil {
		return false
	}
	var questions []investigationLeadQuestion
	if investigationLeadDecode(data, &questions) != nil || len(questions) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, q := range questions {
		if q.HypothesisID == "" || seen[q.HypothesisID] {
			return false
		}
		seen[q.HypothesisID] = true
		var registered map[string]any
		for _, h := range objectSliceAny(document["hypotheses"]) {
			if h["hypothesis_id"] == q.HypothesisID {
				if registered != nil {
					return false
				}
				registered = h
			}
		}
		if registered == nil || !investigationLeadSameQuestion(q, investigationLeadQuestionFromCase(registered)) {
			return false
		}
		assignment := stringValue(registered["assignment_id"])
		owner := investigationLeadRegisteredOwner(state, assignment)
		if assignment == "" || owner == "" || q.AgentID != "" && q.AgentID != owner || !s8ObservedDispatchRecorded(state, assignment, owner) {
			return false
		}
	}
	return true
}

func s8ObservedDispatchRecorded(state map[string]any, assignment, owner string) bool {
	entities := mapFieldCLI(state, "entities")
	var agent map[string]any
	for _, row := range objectSliceAny(entities["agents"]) {
		if row["id"] == owner {
			if agent != nil {
				return false
			}
			agent = row
		}
	}
	if agent == nil || agent["role"] != "investigator" {
		return false
	}
	manifest, fragment, found := strings.Cut(stringValue(agent["prompt_ref"]), "#")
	if !found || manifest == "" || fragment != assignment {
		return false
	}
	teamFound := false
	for _, team := range objectSliceAny(entities["teams"]) {
		if team["id"] == agent["team_id"] && team["kind"] == "investigator" && team["manifest_ref"] == manifest {
			if teamFound {
				return false
			}
			teamFound = true
		}
	}
	if !teamFound {
		return false
	}
	for _, task := range objectSliceAny(entities["tasks"]) {
		if slices.Contains(stringSliceAny(agent["task_ids"]), stringValue(task["id"])) && slices.Contains(stringSliceAny(task["owner_agent_ids"]), owner) && stringValue(task["path"]) != "" {
			return true
		}
	}
	return false
}

// A persisted route is already represented by its original Case revision.
// Routes with a separate downstream consumer remain observable until that
// existing consumer records its acknowledgement; an unchanged save can then
// recover an interrupted handoff instead of being silently dropped.
func s8ObservedRouteAlreadyRegistered(root, path string, data []byte, state, document map[string]any) bool {
	if document == nil || !strings.HasPrefix(filepath.Base(path), "route-") || !investigationLeadAtRouteRevision(document) || workpacket.ValidateUniqueJSON(data) != nil {
		return false
	}
	expected, err := investigationLeadRoutePath(root, state, document)
	if err != nil || !samePath(path, expected) {
		return false
	}
	var answer investigationLeadRouteAnswer
	if investigationLeadDecode(data, &answer) != nil || !investigationLeadRouteMatches(answer, document) {
		return false
	}
	pointer := investigationPointer(state)
	route := stringValue(document["route"])
	if pointer == nil || pointer["case_id"] != document["case_id"] || stringValue(pointer["route"]) != route {
		return false
	}
	switch route {
	case "s9_repair", "human_req_change", "investigate_more":
		return true
	case "s2_spec_rework", "duplicate", "s7_no_change":
		return stringValue(pointer["route_consumed_at"]) != "" && pointer["route_consumer"] == "S8-ROUTE-CONSUME"
	}
	return false
}

// Equal current projection bytes already exist in the original final draft.
// This skips passive resubmission only; it records no acceptance or approval.
func s8ObservedContractAlreadyProjected(root, path string, data []byte, state, document map[string]any) bool {
	if document == nil || document["route"] != "s9_repair" || !strings.HasPrefix(filepath.Base(path), "contract-") {
		return false
	}
	pointer := investigationPointer(state)
	if pointer == nil || pointer["case_id"] != document["case_id"] || tolerantInt(pointer["revision"]) != tolerantInt(document["revision"]) {
		return false
	}
	paths := investigationLeadContractPaths(root, state)
	if len(paths) != 1 || !samePath(path, paths[0]) {
		return false
	}
	finalPath, finalBytes, err := investigationLeadContractProjectionBytes(root, data, document)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, finalPath)
	if err != nil {
		return false
	}
	actual, err := workpacket.ReadFile(root, filepath.ToSlash(relative), 20<<20)
	return err == nil && bytes.Equal(actual, finalBytes)
}

// observeOriginalS9Plan consumes the already located Plan through its original
// consumer. Accepted facts are checked before opening the author's file; an
// unreceived original is read once and passed directly without a typed copy.
func observeOriginalS9Plan(root string, request policy.Input, state map[string]any, q observedQuestionnaire, diagnostic io.Writer) string {
	packet := q.s9Packet
	if packet == nil || packet.result != nil {
		return ""
	}
	owner := request.EffectiveAgentID()
	locatedOwner := owner == packet.binding.OwnerAgentID
	if !locatedOwner {
		execution := s9ObservedExecution(state, *packet)
		locatedOwner = request.CWD != "" && samePath(request.CWD, stringValue(execution["worktree_path"])) && execution["agent_id"] == packet.binding.OwnerAgentID
	}
	if !locatedOwner {
		return ""
	} // No role, opaque actor or unique-candidate guess.
	ctx := contextForHook(request)
	service, err := submission.NewContext(ctx, root, true)
	if err != nil {
		return "S9 Plan observation is incomplete for " + q.path + ": " + err.Error()
	}
	receipt, err := service.RepairPlanReportReceipt(packet.binding.OwnerAgentID, packet.id)
	if err != nil {
		return "S9 original Plan receipt is unavailable for " + q.path + ": " + err.Error()
	}
	if receipt.Accepted {
		return ""
	}
	// The existing Plan questionnaire projection uses the control root; native
	// private worktree Plans retain their separate native_s9 reception path.
	raw, err := workpacket.ReadFile(root, packet.answer, 20<<20)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		return "S9 original Plan could not be read at " + q.path + ": " + err.Error()
	}
	receipt, err = service.SaveRepairPlanReportBytes(ctx, packet.binding.OwnerAgentID, packet.id, root, raw)
	if err != nil {
		var fields *workpacket.AnswerError
		if errors.As(err, &fields) && len(fields.Fields) > 0 {
			first := fields.Fields[0]
			if first.Pointer == "/" || first.Pointer == "/answer/plan" && first.Observed == "empty" {
				return ""
			}
		}
		fmt.Fprintf(diagnostic, "S9 PlanReport not accepted: %v\n", err)
		if receipt.Error == "" {
			receipt.Error = err.Error()
		}
	}
	var out bytes.Buffer
	renderStageAnswerHook(&out, q.path, receipt, "S9")
	var envelope struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal(out.Bytes(), &envelope) != nil {
		return ""
	}
	return envelope.Output.Context
}
