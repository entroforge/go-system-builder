package assignment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

const (
	canonicalResultMessageLimit = 1 << 20
	canonicalResultSourceLimit  = 16 << 20
	canonicalResultAssetLimit   = 20 << 20
)

// ResultAcceptanceReceipt points to one original accepted Builder Result.
// Replaying a historical Result never moves the Agent/TASK current pointer
// backwards; Current reports whether those original pointers still name it.
type ResultAcceptanceReceipt struct {
	ResultRef      string `json:"result_ref"`
	ResultSHA256   string `json:"result_sha256"`
	ResultRevision int    `json:"result_revision"`
	AnswerDigest   string `json:"answer_digest"`
	Replayed       bool   `json:"replayed"`
	Current        bool   `json:"current"`
}

type canonicalCompletionMessage struct {
	SchemaVersion            string                               `json:"schema_version"`
	MessageType              string                               `json:"message_type"`
	MessageID                string                               `json:"message_id"`
	CorrelationID            string                               `json:"correlation_id"`
	RuntimeID                string                               `json:"runtime_id"`
	ExpectedRuntimeRevision  *int                                 `json:"expected_runtime_revision,omitempty"`
	BaselineGeneration       int                                  `json:"baseline_generation"`
	AgentID                  string                               `json:"agent_id"`
	AgentDefinitionRef       string                               `json:"agent_definition_ref"`
	TaskID                   string                               `json:"task_id"`
	BugID                    any                                  `json:"bug_id"`
	TeamID                   string                               `json:"team_id"`
	OccurredAt               string                               `json:"occurred_at"`
	AssignmentID             string                               `json:"assignment_id"`
	AssignmentRevision       int                                  `json:"assignment_revision"`
	AssignmentRecordContract string                               `json:"assignment_record_contract"`
	AssignmentRecordSHA256   string                               `json:"assignment_record_sha256"`
	Summary                  string                               `json:"summary"`
	AssertionEvidence        []assignmentresult.AssertionEvidence `json:"assertion_evidence"`
	RisksBlockers            []string                             `json:"risks_blockers"`
	ScopeDeviations          []string                             `json:"scope_deviations"`
	ClosingAssessment        *assignmentresult.ClosingAssessment  `json:"closing_assessment,omitempty"`
}

func (m canonicalCompletionMessage) answer() assignmentresult.Answer {
	return assignmentresult.Answer{
		Summary:           m.Summary,
		AssertionEvidence: m.AssertionEvidence,
		RisksBlockers:     m.RisksBlockers,
		ScopeDeviations:   m.ScopeDeviations,
		ClosingAssessment: m.ClosingAssessment,
	}
}

type canonicalResultPlan struct {
	MessageType        string                           `json:"message_type"`
	MessageID          string                           `json:"message_id"`
	RuntimeID          string                           `json:"runtime_id"`
	AgentID            string                           `json:"agent_id"`
	AgentDefinitionRef string                           `json:"agent_definition_ref"`
	TaskID             string                           `json:"task_id"`
	TeamID             string                           `json:"team_id"`
	AssignmentID       string                           `json:"assignment_id"`
	AssignmentRevision int                              `json:"assignment_revision"`
	RecordContract     string                           `json:"assignment_record_contract"`
	RecordSHA256       string                           `json:"assignment_record_sha256"`
	Assertions         []assignmentresult.PlanAssertion `json:"assertion_checks"`
}

type resultExecutionView struct {
	MainRoot    string
	SourceRoot  string
	Execution   *workspace.Execution
	InputPaths  map[string]bool
	OwnerPrefix string
}

type canonicalResultSource struct {
	Resolved assignmentresult.ResolvedEvidence
	Bytes    []byte
}

type stagedCanonicalResultArtifact struct {
	Path string
	SHA  string
}

type canonicalResultReplay struct {
	Snapshot loopruntime.Snapshot
	Receipt  ResultAcceptanceReceipt
}

func (e *canonicalResultReplay) Error() string { return "canonical Result was accepted concurrently" }

// hasCanonicalResultMarker selects the strict v1.2 wire branch before the
// legacy completion decoder can interpret its professional fields as a
// legacy completed verdict.
func hasCanonicalResultMarker(data []byte) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return false
	}
	_, ok := raw["assignment_record_contract"]
	return ok
}

// rejectLegacyCompletionForS6Owner prevents an explicitly opted-in manifest
// from falling through to the legacy worker-authored "completed" envelope.
// Legacy messages without an S6 owner continue through their original path.
func rejectLegacyCompletionForS6Owner(root string, state map[string]any, agentID string) error {
	_, canonical, err := assignmentrecord.ForOwner(state, agentID)
	if err != nil {
		return fmt.Errorf("resolve completion Assignment: %w", err)
	}
	if canonical {
		return fmt.Errorf("S6 v1.2 Assignment requires a Record-bound canonical Result message")
	}
	opted, err := manifestOptsIntoS6ForOwner(root, state, agentID)
	if err != nil {
		return fmt.Errorf("resolve completion Assignment manifest: %w", err)
	}
	if opted {
		return fmt.Errorf("S6 v1.2 manifest has no current canonical Assignment Record; legacy completion is rejected")
	}
	return nil
}

func manifestOptsIntoS6ForOwner(root string, state map[string]any, agentID string) (bool, error) {
	teamID := ""
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent != nil && agent["id"] == agentID {
			teamID, _ = agent["team_id"].(string)
			break
		}
	}
	if teamID == "" {
		return false, nil
	}
	teams, _ := entities["teams"].([]any)
	manifestRef := ""
	for _, raw := range teams {
		team, _ := raw.(map[string]any)
		if team == nil || team["id"] != teamID {
			continue
		}
		if manifestRef != "" {
			return false, fmt.Errorf("Team %s has duplicate Runtime rows", teamID)
		}
		manifestRef, _ = team["manifest_ref"].(string)
	}
	if manifestRef == "" {
		return false, nil
	}
	data, err := assignmentinputs.Read(root, manifestRef)
	if err != nil {
		return false, err
	}
	return assignmentrecord.ManifestAssignmentRecordEnabled(data)
}

func completeCanonicalTaskWithReceipt(
	root, statePath, journalPath string,
	request CompletionRequest,
	data []byte,
) (loopruntime.Snapshot, ResultAcceptanceReceipt, error) {
	defer metrics.Span(request.Context, "reception/s6")()
	var empty loopruntime.Snapshot
	var noReceipt ResultAcceptanceReceipt
	if request.AgentID == "" {
		return empty, noReceipt, fmt.Errorf("agent_id is required")
	}
	if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data); err != nil {
		return empty, noReceipt, fmt.Errorf("completion message schema: %w", err)
	}
	var message canonicalCompletionMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return empty, noReceipt, fmt.Errorf("decode canonical completion Result: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return empty, noReceipt, fmt.Errorf("canonical completion Result must contain exactly one JSON envelope")
	}
	if message.MessageType != assignmentresult.CompletionMessage {
		return empty, noReceipt, fmt.Errorf("message_type %q is not a completion_report", message.MessageType)
	}
	if message.AgentID != request.AgentID {
		return empty, noReceipt, fmt.Errorf("completion message does not match Agent")
	}
	if len(message.MessageID) > 1024 {
		return empty, noReceipt, fmt.Errorf("canonical Result message_id exceeds the key budget")
	}

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	ctx := store.Context()
	snapshot, err := store.Snapshot()
	if err != nil {
		return empty, noReceipt, fmt.Errorf("read runtime: %w", err)
	}
	if _, err := commitRevision(request.ExpectedRevision, snapshot.State); err != nil {
		return empty, noReceipt, err
	}
	binding, err := resolveCanonicalResultBinding(ctx, root, snapshot, request.AgentID, message, request.NativePlanPath)
	if err != nil {
		return empty, noReceipt, err
	}
	answer := message.answer()
	if err := assignmentresult.ValidateAnswer(answer, binding.Plan.Assertions); err != nil {
		return empty, noReceipt, fmt.Errorf("canonical Result answer: %w", err)
	}
	if err := assignmentresult.ValidateClosingAssessment(binding.Record, binding.Plan.Assertions, answer); err != nil {
		return empty, noReceipt, fmt.Errorf("canonical Result closing assessment: %w", err)
	}
	answerDigest, err := assignmentresult.AnswerDigest(answer)
	if err != nil {
		return empty, noReceipt, err
	}
	readMain := func(ref string) ([]byte, error) {
		return workpacket.ReadFile(binding.View.MainRoot, ref, canonicalResultAssetLimit)
	}
	history, err := assignmentresult.AcceptedHistory(snapshot.State, binding.Record, readMain)
	if err != nil {
		return empty, noReceipt, fmt.Errorf("validate original Result history: %w", err)
	}
	acceptedKey, registeredKey, err := resultForSubmissionKey(snapshot.State, binding.Record, history, message.MessageID, readMain)
	if err != nil {
		return empty, noReceipt, err
	}
	if registeredKey {
		if acceptedKey.AnswerDigest != answerDigest {
			return empty, noReceipt, fmt.Errorf("completion message_id %s was already accepted with a different AnswerDigest", message.MessageID)
		}
		receipt, err := canonicalResultReceipt(snapshot.State, acceptedKey, readMain, true)
		return snapshot, receipt, err
	}
	for _, accepted := range history {
		if binding.Record.Contract != assignmentrecord.NativeDeliveryContract && accepted.AnswerDigest == answerDigest && (binding.Record.ClosingContract == nil || len(binding.Record.ClosingContract.WorkerChecks) == 0) {
			return bindResultSubmissionAlias(root, store, snapshot, request, message, accepted, readMain)
		}
	}
	var closureDigest string
	if binding.Record.ClosingContract != nil && len(binding.Record.ClosingContract.WorkerChecks) > 0 {
		var unlock func()
		ctx, unlock, err = integration.LockWorkspace(ctx, root)
		if err != nil {
			return empty, noReceipt, err
		}
		defer unlock()
		// Workspace lock precedes Runtime Writer; refresh the CAS snapshot.
		snapshot, err = store.Snapshot()
		if err != nil {
			return empty, noReceipt, err
		}
		if _, err := commitRevision(request.ExpectedRevision, snapshot.State); err != nil {
			return empty, noReceipt, err
		}
		binding, err = resolveCanonicalResultBinding(ctx, root, snapshot, request.AgentID, message, request.NativePlanPath)
		if err != nil {
			return empty, noReceipt, err
		}
		history, err = assignmentresult.AcceptedHistory(snapshot.State, binding.Record, readMain)
		if err != nil {
			return empty, noReceipt, err
		}
		prior, registered, keyErr := resultForSubmissionKey(snapshot.State, binding.Record, history, message.MessageID, readMain)
		if keyErr != nil {
			return empty, noReceipt, keyErr
		}
		if registered {
			if prior.AnswerDigest != answerDigest {
				return empty, noReceipt, fmt.Errorf("completion message_id %s was already accepted with a different AnswerDigest", message.MessageID)
			}
			receipt, err := canonicalResultReceipt(snapshot.State, prior, readMain, true)
			return snapshot, receipt, err
		}
		facts, artifacts, err := CollectWorkerCheckFacts(ctx, root, snapshot.State, binding.Record)
		if err != nil {
			return empty, noReceipt, err
		}
		if err := prepareWorkerCheckArtifactRefs(binding.Record, "closure-preflight", facts, artifacts); err != nil {
			return empty, noReceipt, err
		}
		closureDigest, err = assignmentresult.WorkerClosureDigest(binding.Record, facts)
		if err != nil {
			return empty, noReceipt, err
		}
		for _, prior := range history {
			priorClosure, err := assignmentresult.EffectiveToolClosureDigest(binding.Record, prior)
			if err != nil {
				return empty, noReceipt, err
			}
			if prior.AnswerDigest == answerDigest && priorClosure == closureDigest {
				return bindResultSubmissionAlias(root, store, snapshot, request, message, prior, readMain)
			}
		}
	}
	binding.View, binding.Execution, err = resolveResultExecutionView(ctx, root, snapshot.State, binding.Record)
	if err != nil {
		return empty, noReceipt, err
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	resultRevision := len(history) + 1
	resultID := canonicalResultEvidenceID(binding.Record, resultRevision, message.MessageID)
	var resultRel, resultSHA string
	var staged []stagedCanonicalResultArtifact
	result := assignmentresult.AcceptedResult{
		SchemaVersion:          "1.0.0",
		EvidenceID:             resultID,
		Kind:                   "completion_report",
		RuntimeID:              binding.Record.RuntimeID,
		BaselineGeneration:     binding.Record.BaselineGeneration,
		ProducerAgentID:        binding.Record.OwnerAgentID,
		ProducerResponsibility: "BUILD-WORK-PACKAGE",
		SubjectRefs: []assignmentresult.SubjectReference{{
			ID: binding.Record.Task.ID, Kind: "task", Path: binding.Record.Task.Ref,
			Version: binding.Record.Task.Version, SHA256: binding.Record.Task.SHA256,
		}},
		Conclusion:             "submitted",
		TaskID:                 binding.Record.Task.ID,
		AssignmentRecord:       binding.Record.Reference(),
		AssignmentRecordSHA256: binding.RecordSHA,
		ResultRevision:         resultRevision,
		SubmissionKey:          message.MessageID,
		AnswerDigest:           answerDigest,
		Answer:                 answer,
		PlanRef:                binding.PlanRef,
		PlanSHA256:             binding.PlanSHA,
		ResolvedEvidenceRefs:   []assignmentresult.ResolvedEvidence{},
		CreatedAt:              occurredAt.UTC().Format(time.RFC3339Nano),
	}
	if binding.Record.Contract == assignmentrecord.NativeDeliveryContract {
		result.SourceHead, err = workspace.Git(ctx, binding.View.SourceRoot, "rev-parse", "HEAD")
		if err != nil {
			return empty, noReceipt, fmt.Errorf("read actual native source commit: %w", err)
		}
	}
	initialSources, err := readCanonicalResultSources(binding.View, binding.Record, binding.Execution, answer)
	if err != nil {
		return empty, noReceipt, err
	}
	for _, source := range initialSources {
		resolved := source.Resolved
		resolved.Path = canonicalResultEvidenceArtifactPath(binding.Record, resultID, resolved.Ref, resolved.SHA256)
		result.ResolvedEvidenceRefs = append(result.ResolvedEvidenceRefs, resolved)
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return empty, noReceipt, fmt.Errorf("load catalog: %w", err)
	}
	agentTransitions := agentEntityTransitions(catalog)
	taskTransitions := taskEntityTransitions(catalog)
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	eventHash := sha256Of([]byte(message.MessageID))[:16]
	mutation := loopruntime.Mutation{
		EventID:            fmt.Sprintf("evt-s6-result-%s-g%d-a%d-r%d-%s", binding.Record.OwnerAgentID, binding.Record.BaselineGeneration, binding.Record.Revision, resultRevision, eventHash),
		TransitionID:       "BUILDER-RESULT",
		Event:              "completion_reported",
		Actor:              "orchestrator",
		IdempotencyKey:     fmt.Sprintf("runtime:s6-result:%s:g%d:r%d:%s", binding.Record.AssignmentID, binding.Record.BaselineGeneration, binding.Record.Revision, message.MessageID),
		RuntimeID:          binding.Record.RuntimeID,
		BaselineGeneration: binding.Record.BaselineGeneration,
		From:               cursor,
		To:                 cursor,
		EvidenceIDs:        []string{resultID},
		Message:            fmt.Sprintf("Registered submitted Builder Result revision %d for %s", resultRevision, binding.Record.Task.ID),
		OccurredAt:         occurredAt,
		Apply: func(state map[string]any) error {
			currentRevision, err := commitRevision(-1, state)
			if err != nil {
				return err
			}
			currentSnapshot := loopruntime.Snapshot{Revision: currentRevision, State: state}
			current, err := resolveCanonicalResultBinding(ctx, root, currentSnapshot, request.AgentID, message, request.NativePlanPath)
			if err != nil {
				return err
			}
			currentHistory, err := assignmentresult.AcceptedHistory(state, current.Record, readMain)
			if err != nil {
				return fmt.Errorf("validate original Result history during acceptance: %w", err)
			}
			prior, registeredKey, err := resultForSubmissionKey(state, current.Record, currentHistory, message.MessageID, readMain)
			if err != nil {
				return err
			}
			if registeredKey {
				if prior.AnswerDigest != answerDigest {
					return fmt.Errorf("completion message_id %s was already accepted with a different AnswerDigest", message.MessageID)
				}
				receipt, err := canonicalResultReceipt(state, prior, readMain, true)
				if err != nil {
					return err
				}
				return &canonicalResultReplay{Snapshot: currentSnapshot, Receipt: receipt}
			}
			if current.Record.Reference() != binding.Record.Reference() || current.RecordSHA != binding.RecordSHA || len(currentHistory)+1 != resultRevision {
				return fmt.Errorf("canonical Assignment or Result history changed during acceptance")
			}
			if err := assignmentresult.ValidateAnswer(answer, current.Plan.Assertions); err != nil {
				return fmt.Errorf("canonical Result answer: %w", err)
			}
			if err := assignmentresult.ValidateClosingAssessment(current.Record, current.Plan.Assertions, answer); err != nil {
				return fmt.Errorf("canonical Result closing assessment: %w", err)
			}
			current.View, current.Execution, err = resolveResultExecutionView(ctx, root, state, current.Record)
			if err != nil {
				return err
			}
			if current.Record.Contract == assignmentrecord.NativeDeliveryContract {
				head, err := workspace.Git(ctx, current.View.SourceRoot, "rev-parse", "HEAD")
				if err != nil || head != result.SourceHead {
					return fmt.Errorf("native source commit changed during Result acceptance; preserve the actual delivery")
				}
			}
			resultSources, err := readCanonicalResultSources(current.View, current.Record, current.Execution, answer)
			if err != nil {
				return err
			}
			result.ResolvedEvidenceRefs = make([]assignmentresult.ResolvedEvidence, 0, len(resultSources))
			for i := range resultSources {
				resultSources[i].Resolved.Path = canonicalResultEvidenceArtifactPath(current.Record, resultID, resultSources[i].Resolved.Ref, resultSources[i].Resolved.SHA256)
				result.ResolvedEvidenceRefs = append(result.ResolvedEvidenceRefs, resultSources[i].Resolved)
			}
			for _, source := range resultSources {
				staged = append(staged, stagedCanonicalResultArtifact{Path: source.Resolved.Path, SHA: source.Resolved.SHA256})
				published, err := workpacket.PublishBounded(root, source.Resolved.Path, source.Bytes, canonicalResultSourceLimit)
				if err != nil {
					return fmt.Errorf("publish immutable Result evidence %s: %w", source.Resolved.Ref, err)
				}
				if !bytes.Equal(published, source.Bytes) {
					return fmt.Errorf("immutable Result evidence path conflicts for %s", source.Resolved.Ref)
				}
			}
			if closureDigest != "" {
				facts, artifacts, err := CollectWorkerCheckFacts(ctx, root, state, current.Record)
				if err != nil {
					return err
				}
				if err := prepareWorkerCheckArtifactRefs(current.Record, resultID, facts, artifacts); err != nil {
					return err
				}
				confirmedSources, err := readCanonicalResultSources(current.View, current.Record, current.Execution, answer)
				if err != nil {
					return err
				}
				if len(confirmedSources) != len(resultSources) {
					return fmt.Errorf("Result evidence changed during Worker closure capture")
				}
				for i := range confirmedSources {
					if confirmedSources[i].Resolved.Ref != resultSources[i].Resolved.Ref || !bytes.Equal(confirmedSources[i].Bytes, resultSources[i].Bytes) {
						return fmt.Errorf("Result evidence changed during Worker closure capture; retry the same submission key")
					}
				}
				actualDigest, err := assignmentresult.WorkerClosureDigest(current.Record, facts)
				if err != nil {
					return err
				}
				if actualDigest != closureDigest {
					return fmt.Errorf("Worker closure changed during Result acceptance; retry with the same submission key")
				}
				assets, err := publishWorkerCheckArtifacts(root, current.Record, resultID, facts, artifacts)
				staged = append(staged, assets...)
				if err != nil {
					return err
				}
				result.WorkerCheckFacts, result.ToolClosureDigest = facts, actualDigest
				if err := assignmentresult.ValidateWorkerClosure(current.Record, result, readMain); err != nil {
					return fmt.Errorf("validate captured Worker closure: %w", err)
				}
			}
			encoded, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("encode canonical Result: %w", err)
			}
			envelope := append(encoded, '\n')
			actualSHA := sha256Of(envelope)
			actualRel := canonicalResultArtifactPath(current.Record, resultID, actualSHA)
			staged = append(staged, stagedCanonicalResultArtifact{Path: actualRel, SHA: actualSHA})
			published, err := workpacket.PublishBounded(root, actualRel, envelope, canonicalResultAssetLimit)
			if err != nil {
				return fmt.Errorf("publish canonical Result: %w", err)
			}
			if !bytes.Equal(published, envelope) {
				return fmt.Errorf("canonical Result path conflicts with previously staged bytes")
			}
			resultRel, resultSHA = actualRel, actualSHA
			if current.Record.Contract == assignmentrecord.NativeDeliveryContract {
				agent, found := findCompletionAgent(state, request.AgentID)
				if !found {
					return fmt.Errorf("native delivery owner missing")
				}
				agent["plan_reported_ref"] = current.PlanRef
				if err := workstate.RecordAgentActivity(state, agent, "reported", "completion_reported"); err != nil {
					return err
				}
				agent["completion_reported_ref"] = actualRel
			} else if err := applyCompletionAgent(state, request, completionMessage{AgentID: request.AgentID, TaskID: current.Record.Task.ID}, agentTransitions, actualRel, occurredAt); err != nil {
				return err
			}
			if err := clearCanonicalCompletionAcknowledgement(state, request.AgentID); err != nil {
				return err
			}
			if current.Record.Contract == assignmentrecord.NativeDeliveryContract {
				entities, _ := state["entities"].(map[string]any)
				tasks, _ := entities["tasks"].([]any)
				for _, raw := range tasks {
					task, _ := raw.(map[string]any)
					if task["id"] == current.Record.Task.ID {
						if err := workstate.RecordTaskActivity(state, task, "review", "builder_reported"); err != nil {
							return err
						}
						task["completion_report_ref"] = actualRel
					}
				}
			} else if err := applyCompletionTask(state, completionMessage{AgentID: request.AgentID, TaskID: current.Record.Task.ID}, taskTransitions, actualRel, occurredAt); err != nil {
				return err
			}
			if err := appendCanonicalResultEvidence(state, result, actualRel, actualSHA, occurredAt); err != nil {
				return err
			}
			return dispatch.ReconcilePressure(root, state)
		},
	}

	if binding.Record.Contract == assignmentrecord.NativeDeliveryContract {
		mutation.Actor = request.NativeAgentID
		if mutation.Actor == "" {
			mutation.Actor = "native_receiver"
		}
		mutation.RequestID = request.NativeSessionID + ":" + request.NativeAgentID
	}
	committed, err := store.Update(snapshot.Revision, mutation)
	if err != nil {
		var replay *canonicalResultReplay
		if errors.As(err, &replay) {
			return replay.Snapshot, replay.Receipt, nil
		}
		for _, asset := range staged {
			_, cleanupErr := store.RemoveUnreferencedArtifact(loopruntime.ArtifactCleanupRequest{
				ExpectedRevision: snapshot.Revision,
				ArtifactPath:     asset.Path,
				ArtifactSHA256:   asset.SHA,
				ReferencedPaths:  stateArtifactPaths(snapshot.State),
			})
			if cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("cleanup unreferenced Result artifact %s: %w", asset.Path, cleanupErr))
			}
		}
		return empty, noReceipt, err
	}
	receipt := ResultAcceptanceReceipt{ResultRef: resultRel, ResultSHA256: resultSHA, ResultRevision: resultRevision, AnswerDigest: answerDigest, Current: true}
	// The immutable bytes can include source references that changed between
	// preflight and the locked Apply. Resolve the exact indexed receipt from the
	// committed snapshot before returning it.
	accepted, err := assignmentresult.LoadAccepted(committed.State, binding.Record, resultRel, readMain)
	if err != nil {
		return committed, noReceipt, fmt.Errorf("validate newly accepted Result receipt: %w", err)
	}
	receipt.ResultRef, receipt.ResultSHA256, receipt.ResultRevision, receipt.AnswerDigest = resultRel, resultSHA, accepted.ResultRevision, accepted.AnswerDigest
	return committed, receipt, nil
}

func clearCanonicalCompletionAcknowledgement(state map[string]any, agentID string) error {
	agent, found := findCompletionAgent(state, agentID)
	if !found {
		return fmt.Errorf("canonical Result Agent %s is not registered", agentID)
	}
	delete(agent, "completion_acknowledged_ref")
	delete(agent, "completion_acknowledged_reports")
	// A newer Builder Result invalidates an earlier Main acknowledgement even
	// when this Runtime has no configured dispatch policy. Keep the control
	// lifecycle in building while making the new Result the Agent's report.
	lifecycle, _ := state["lifecycle"].(map[string]any)
	version := fmt.Sprint(state["schema_version"])
	projection, err := workstate.AgentProjection(agent, version)
	if err != nil {
		return fmt.Errorf("resolve canonical Result Agent activity: %w", err)
	}
	if lifecycle["state"] == "building" && projection.Activity == "done" {
		if workstate.Unified(version) {
			return fmt.Errorf("completed Assignment requires an explicit revision before accepting another Result")
		}
		if err := workstate.RecordAgentActivity(state, agent, "reported", "completion_reported"); err != nil {
			return fmt.Errorf("record repeated canonical Result Agent activity: %w", err)
		}
	}
	return nil
}

type canonicalResultBinding struct {
	Record    assignmentrecord.Record
	RecordSHA string
	PlanRef   string
	PlanSHA   string
	Plan      canonicalResultPlan
	View      resultExecutionView
	Execution *workspace.Execution
}

func resolveCanonicalResultBinding(ctx context.Context, root string, snapshot loopruntime.Snapshot, agentID string, message canonicalCompletionMessage, nativePlanPath string) (canonicalResultBinding, error) {
	var binding canonicalResultBinding
	record, found, err := assignmentrecord.ForOwner(snapshot.State, agentID)
	if err != nil {
		return binding, fmt.Errorf("resolve canonical Assignment Record: %w", err)
	}
	if !found {
		opted, optErr := manifestOptsIntoS6ForOwner(root, snapshot.State, agentID)
		if optErr != nil {
			return binding, fmt.Errorf("resolve S6 opt-in without Record: %w", optErr)
		}
		if opted {
			return binding, fmt.Errorf("S6 v1.2 manifest has no current canonical Assignment Record")
		}
		return binding, fmt.Errorf("canonical completion Result has no current Assignment Record")
	}
	if record.Contract != assignmentrecord.AssignmentRecordContract && record.Contract != assignmentrecord.NativeDeliveryContract {
		return binding, fmt.Errorf("canonical completion Result requires %s", assignmentrecord.AssignmentRecordContract)
	}
	if err := assignmentrecord.VerifySources(record, func(ref string) ([]byte, error) {
		return assignmentinputs.Read(root, ref)
	}); err != nil {
		return binding, fmt.Errorf("canonical Assignment source drift: %w", err)
	}
	recordSHA, err := record.Digest()
	if err != nil {
		return binding, err
	}
	generation := workspace.Generation(snapshot.State)
	runtimeID := workspace.RuntimeID(snapshot.State)
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "building" {
		return binding, fmt.Errorf("canonical Builder Result acceptance requires the original building lifecycle")
	}
	if message.RuntimeID != record.RuntimeID || runtimeID != record.RuntimeID || message.BaselineGeneration != record.BaselineGeneration || generation != record.BaselineGeneration ||
		message.AssignmentID != record.AssignmentID || message.AssignmentRevision != record.Revision ||
		message.AssignmentRecordContract != record.Contract || message.AssignmentRecordSHA256 != recordSHA ||
		message.AgentID != record.OwnerAgentID || message.AgentDefinitionRef != record.AgentDefinition.Ref ||
		message.TaskID != record.Task.ID || message.TeamID != record.TeamID || message.MessageID == "" {
		return binding, fmt.Errorf("canonical completion message differs from the current Assignment Record binding")
	}
	if message.BugID != nil {
		return binding, fmt.Errorf("canonical S6 completion Result cannot carry bug_id")
	}
	agent, found := findCompletionAgent(snapshot.State, agentID)
	if !found {
		return binding, fmt.Errorf("canonical Result owner Agent is not registered")
	}
	if agent["definition_ref"] != record.AgentDefinition.Ref || agent["team_id"] != record.TeamID || !stringArrayContains(asAnySlice(agent["task_ids"]), record.Task.ID) {
		return binding, fmt.Errorf("canonical Result owner Agent/TASK registration differs from the Assignment Record")
	}
	projection, err := workstate.AgentProjection(agent, fmt.Sprint(snapshot.State["schema_version"]))
	if err != nil {
		return binding, fmt.Errorf("resolve canonical Result Agent activity: %w", err)
	}
	status := projection.Activity
	if record.Contract == assignmentrecord.NativeDeliveryContract && status != "queued" && status != "reading" && status != "reported" && status != "done" {
		return binding, fmt.Errorf("native delivery responsibility is not current: %s", status)
	}
	if record.Contract != assignmentrecord.NativeDeliveryContract && status != "working" && status != "reported" && status != "done" {
		return binding, fmt.Errorf("canonical Result requires a working or previously reported Agent; got %s", status)
	}
	planRef, err := plancheckpoint.ActivatedRef(root, snapshot, agentID)
	if record.Contract == assignmentrecord.NativeDeliveryContract {
		if planRef != "" && nativePlanPath != "" && planRef != nativePlanPath {
			return binding, fmt.Errorf("native professional plan changed after reception; Main must issue the original Assignment revision")
		}
		if planRef == "" {
			planRef = nativePlanPath
		}
		if planRef == "" {
			return binding, fmt.Errorf("native Result requires its actual professional plan")
		}
		err = plancheckpoint.Validate(root, snapshot, agentID, planRef)
	} else if planRef == "" || agent["readback_ref"] != planRef || agent["plan_reported_ref"] != planRef {
		return binding, fmt.Errorf("canonical Result requires the current accepted and activated PLAN_REPORT")
	}
	if err != nil {
		return binding, fmt.Errorf("validate professional PLAN: %w", err)
	}
	planBytes, err := workpacket.ReadFile(root, planRef, canonicalResultMessageLimit)
	if err != nil {
		return binding, fmt.Errorf("read accepted PLAN_REPORT: %w", err)
	}
	var plan canonicalResultPlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return binding, fmt.Errorf("decode accepted PLAN_REPORT: %w", err)
	}
	planSHA := sha256Of(planBytes)
	if plan.MessageType != "plan_report" || plan.MessageID == "" || plan.RuntimeID != record.RuntimeID || plan.AgentID != record.OwnerAgentID || plan.AgentDefinitionRef != record.AgentDefinition.Ref || plan.TaskID != record.Task.ID || plan.TeamID != record.TeamID || plan.AssignmentID != record.AssignmentID || plan.AssignmentRevision != record.Revision || plan.RecordContract != record.Contract || plan.RecordSHA256 != recordSHA {
		return binding, fmt.Errorf("accepted PLAN_REPORT does not bind the current Assignment Record")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return binding, err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return binding, fmt.Errorf("resolve canonical Result Main root: %w", err)
	}
	view := resultExecutionView{MainRoot: rootReal, SourceRoot: rootReal, InputPaths: map[string]bool{}, OwnerPrefix: resultOwnerArtifactPrefix(record)}
	return canonicalResultBinding{Record: record, RecordSHA: recordSHA, PlanRef: planRef, PlanSHA: planSHA, Plan: plan, View: view}, nil
}

func resolveResultExecutionView(ctx context.Context, root string, state map[string]any, record assignmentrecord.Record) (resultExecutionView, *workspace.Execution, error) {
	var empty resultExecutionView
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return empty, nil, err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return empty, nil, fmt.Errorf("resolve Main source root: %w", err)
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return empty, nil, fmt.Errorf("decode current workspace execution registry: %w", err)
	}
	view := resultExecutionView{MainRoot: rootReal, SourceRoot: rootReal, InputPaths: map[string]bool{}, OwnerPrefix: resultOwnerArtifactPrefix(record)}
	if registry != nil {
		mainRoot, err := filepath.EvalSymlinks(registry.MainRoot)
		if err != nil || mainRoot != rootReal {
			return empty, nil, fmt.Errorf("current Workspace authority root differs from CompleteTask Main root")
		}
		if err := registry.Validate(ctx, rootReal); err != nil {
			return empty, nil, fmt.Errorf("validate current Workspace authority: %w", err)
		}
		if exec, found := registry.Execution(record.AssignmentID, record.RuntimeID, record.BaselineGeneration); found {
			if exec.AgentID != record.OwnerAgentID || exec.AssignmentID != record.AssignmentID || exec.Status != "ready" || exec.AssignmentRecord == nil || *exec.AssignmentRecord != record.Reference() || exec.AssignmentRecordSHA256 != canonicalRecordDigest(record) {
				return empty, nil, fmt.Errorf("current Workspace execution differs from its canonical Assignment Record")
			}
			if err := workspace.ValidateAssignmentRecord(state, exec); err != nil {
				return empty, nil, fmt.Errorf("validate Workspace Assignment binding: %w", err)
			}
			if err := registry.ValidateExecution(ctx, exec, false); err != nil {
				return empty, nil, fmt.Errorf("validate current Workspace checkout: %w", err)
			}
			if err := registry.ValidateInputs(exec); err != nil {
				return empty, nil, fmt.Errorf("validate current Workspace frozen inputs: %w", err)
			}
			view.SourceRoot = exec.Path
			view.Execution = &exec
			for path := range exec.Inputs {
				view.InputPaths[path] = true
			}
			return view, &exec, nil
		}
		if _, exists := registry.Executions[record.AssignmentID]; exists {
			return empty, nil, fmt.Errorf("Workspace execution for this Assignment is stale or bound to another Runtime")
		}
		for _, old := range registry.History {
			if old.AssignmentID == record.AssignmentID {
				return empty, nil, fmt.Errorf("Workspace has only a historical execution for this Assignment")
			}
		}
	}
	if err := requireBoundMainRoot(state, rootReal); err != nil {
		return empty, nil, err
	}
	coordinate, err := resolveLegacyWorktreeCoordinate(ctx, rootReal, state, record)
	if err != nil {
		return empty, nil, err
	}
	if coordinate != "" {
		view.SourceRoot = coordinate
	}
	return view, nil, nil
}

func requireBoundMainRoot(state map[string]any, root string) error {
	bound, _ := state["bound_req"].(map[string]any)
	workspaceBinding, _ := bound["workspace"].(map[string]any)
	projectRoot, _ := workspaceBinding["project_root"].(string)
	if projectRoot == "" {
		return fmt.Errorf("canonical Result requires the current REQ-bound project root")
	}
	projectAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return err
	}
	projectReal, err := filepath.EvalSymlinks(projectAbs)
	if err != nil || projectReal != root {
		return fmt.Errorf("canonical Result Main root differs from the REQ-bound project root")
	}
	return nil
}

type legacyWorktreeCoordinates struct {
	AssignmentID string `json:"assignment_id"`
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
	TargetBranch string `json:"target_branch"`
	BaseCommit   string `json:"base_commit"`
}

func resolveLegacyWorktreeCoordinate(ctx context.Context, root string, state map[string]any, record assignmentrecord.Record) (string, error) {
	bound, _ := state["bound_req"].(map[string]any)
	workspaceBinding, _ := bound["workspace"].(map[string]any)
	devBranch, _ := workspaceBinding["dev_branch"].(string)
	devBranch = strings.TrimPrefix(devBranch, "refs/heads/")
	manifestBytes, err := assignmentinputs.Read(root, record.Manifest.Ref)
	if err != nil {
		return "", fmt.Errorf("read frozen assignment manifest for worktree coordinates: %w", err)
	}
	var manifest struct {
		Assignments []struct {
			AssignmentID string `json:"assignment_id"`
			AgentID      string `json:"agent_id"`
			WorktreePath string `json:"worktree_path"`
			Branch       string `json:"branch"`
			TargetBranch string `json:"target_branch"`
			BaseCommit   string `json:"base_commit"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return "", fmt.Errorf("decode frozen assignment manifest coordinates: %w", err)
	}
	var manifestCoord legacyWorktreeCoordinates
	found := false
	for _, row := range manifest.Assignments {
		if row.AssignmentID != record.AssignmentID {
			continue
		}
		if found || row.AgentID != record.OwnerAgentID {
			return "", fmt.Errorf("frozen manifest has duplicate or misowned worktree coordinates")
		}
		found = true
		manifestCoord = legacyWorktreeCoordinates{AssignmentID: row.AssignmentID, WorktreePath: row.WorktreePath, Branch: row.Branch, TargetBranch: row.TargetBranch, BaseCommit: row.BaseCommit}
	}
	if !found {
		return "", fmt.Errorf("frozen manifest omits the canonical Assignment")
	}

	sidecarPath := ".claude/assignments/" + record.AssignmentID + ".json"
	var sidecar legacyWorktreeCoordinates
	sidecarBytes, sidecarErr := workpacket.ReadFile(root, sidecarPath, 64<<10)
	sidecarPresent := sidecarErr == nil
	if sidecarErr != nil && !errors.Is(sidecarErr, os.ErrNotExist) {
		return "", fmt.Errorf("read registered worktree coordinates: %w", sidecarErr)
	}
	if sidecarPresent {
		if err := json.Unmarshal(sidecarBytes, &sidecar); err != nil {
			return "", fmt.Errorf("decode registered worktree coordinates: %w", err)
		}
		if sidecar.AssignmentID != record.AssignmentID || sidecar.WorktreePath == "" || sidecar.Branch == "" || sidecar.TargetBranch == "" {
			return "", fmt.Errorf("registered worktree coordinates do not identify this Assignment")
		}
	}
	manifestHasPath, sidecarHasPath := manifestCoord.WorktreePath != "", sidecar.WorktreePath != ""
	if !manifestHasPath && !sidecarHasPath {
		return "", nil
	}
	if manifestHasPath && sidecarHasPath {
		left, err := coordinateAbsolute(root, manifestCoord.WorktreePath)
		if err != nil {
			return "", err
		}
		right, err := coordinateAbsolute(root, sidecar.WorktreePath)
		if err != nil {
			return "", err
		}
		if left != right {
			return "", fmt.Errorf("manifest and registered worktree coordinates disagree")
		}
		for field, values := range map[string][2]string{
			"branch":        {manifestCoord.Branch, sidecar.Branch},
			"target branch": {manifestCoord.TargetBranch, sidecar.TargetBranch},
			"base commit":   {manifestCoord.BaseCommit, sidecar.BaseCommit},
		} {
			if values[0] != "" && values[1] != "" && values[0] != values[1] {
				return "", fmt.Errorf("manifest and registered worktree %s coordinates disagree", field)
			}
		}
	}
	coordinates := sidecar
	if !sidecarHasPath {
		coordinates = manifestCoord
	}
	checkout, err := coordinateAbsolute(root, coordinates.WorktreePath)
	if err != nil {
		return "", err
	}
	if filepath.Clean(checkout) == filepath.Clean(root) {
		return "", fmt.Errorf("Assignment worktree cannot be the Main checkout")
	}
	if err := workspace.ValidateCheckout(root, checkout); err != nil {
		return "", fmt.Errorf("validate exact registered Assignment worktree: %w", err)
	}
	if coordinates.TargetBranch != "" && devBranch != "" && strings.TrimPrefix(coordinates.TargetBranch, "refs/heads/") != devBranch {
		return "", fmt.Errorf("registered worktree target differs from the REQ development branch")
	}
	if sidecarHasPath && (strings.TrimPrefix(sidecar.TargetBranch, "refs/heads/") != devBranch || devBranch == "") {
		return "", fmt.Errorf("registered worktree target is not the current REQ development branch")
	}
	branch, err := workspace.Git(ctx, checkout, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	if coordinates.Branch != "" && branch != coordinates.Branch {
		return "", fmt.Errorf("registered worktree branch differs from its stored coordinates")
	}
	if coordinates.BaseCommit != "" {
		if _, err := workspace.Git(ctx, checkout, "merge-base", "--is-ancestor", coordinates.BaseCommit, "HEAD"); err != nil {
			return "", fmt.Errorf("registered worktree history differs from its stored baseline")
		}
	}
	return checkout, nil
}

func coordinateAbsolute(root, stored string) (string, error) {
	if strings.TrimSpace(stored) == "" || strings.ContainsAny(stored, "\x00\r\n") {
		return "", fmt.Errorf("worktree coordinate path is empty or invalid")
	}
	pathValue := stored
	if !filepath.IsAbs(pathValue) {
		pathValue = filepath.Join(root, filepath.FromSlash(pathValue))
	}
	abs, err := filepath.Abs(pathValue)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func readCanonicalResultSources(view resultExecutionView, record assignmentrecord.Record, execution *workspace.Execution, answer assignmentresult.Answer) ([]canonicalResultSource, error) {
	refs := map[string]bool{}
	for _, assertion := range answer.AssertionEvidence {
		for _, ref := range assertion.EvidenceRefs {
			refs[ref] = true
		}
	}
	ordered := make([]string, 0, len(refs))
	for ref := range refs {
		ordered = append(ordered, ref)
	}
	sort.Strings(ordered)
	resolved := make([]canonicalResultSource, 0, len(ordered))
	for _, ref := range ordered {
		if err := validateResultEvidenceRef(record, execution, ref); err != nil {
			return nil, resultEvidenceAnswerError(fmt.Sprintf("Result evidence ref %q", ref), err)
		}
		readRoot := view.SourceRoot
		if isOwnerResultArtifact(view.OwnerPrefix, ref) || frozenResultContextPath(record, execution, ref) {
			readRoot = view.MainRoot
		}
		body, err := workpacket.ReadFile(readRoot, ref, canonicalResultSourceLimit)
		if err != nil {
			if readRoot == view.SourceRoot && os.IsNotExist(err) {
				return nil, resultEvidenceAnswerError("read actual Result evidence source "+ref, err)
			}
			return nil, fmt.Errorf("read actual Result evidence source %s: %w", ref, err)
		}
		resolved = append(resolved, canonicalResultSource{Resolved: assignmentresult.ResolvedEvidence{Ref: ref, SHA256: sha256Of(body)}, Bytes: body})
	}
	return resolved, nil
}

func resultEvidenceAnswerError(label string, cause error) error {
	return &workpacket.AnswerError{Label: label, Cause: cause, Fields: []workpacket.FieldError{{
		Pointer: "/assertion_evidence", Expected: "exact existing evidence files in the TASK write scope or listed Read paths", Observed: cause.Error(),
		Repair: "evidence_refs must be exact existing worktree-relative files in the TASK write scope or listed Read paths; omit line numbers, descriptions and commands, and put command results in summary",
	}}}
}

func validateResultEvidenceRef(record assignmentrecord.Record, execution *workspace.Execution, ref string) error {
	if ref == "" || strings.TrimSpace(ref) != ref || strings.ContainsAny(ref, "\\\x00\r\n") || path.Clean(ref) != ref || strings.HasPrefix(ref, "/") {
		return fmt.Errorf("must be a canonical authority-relative file path")
	}
	if strings.HasPrefix(ref, "../") || ref == ".." {
		return fmt.Errorf("escapes the execution source")
	}
	if canonicalControlDataPath(ref) {
		if !isOwnerResultArtifact(resultOwnerArtifactPrefix(record), ref) {
			if frozenResultContextPath(record, execution, ref) {
				return nil
			}
			return fmt.Errorf("references control data outside frozen inputs and this owner's evidence directory")
		}
		return nil
	}
	if assignmentrecord.AllowsPath(record, ref) {
		return nil
	}
	for _, pattern := range record.ReadPaths {
		if workspace.PathMatchesScope(ref, pattern) {
			return nil
		}
	}
	for _, source := range record.ContextSources {
		if !strings.HasPrefix(source.Kind, "embedded_") && source.Ref == ref {
			return nil
		}
	}
	if execution != nil && execution.Inputs[ref] != "" {
		return nil
	}
	return fmt.Errorf("is outside the Assignment's read, write, frozen-input and owner-artifact authority")
}

func canonicalControlDataPath(ref string) bool {
	if workspace.ControlFile(ref) {
		return true
	}
	return ref == ".claude/loop-workspace.json" || strings.HasPrefix(ref, ".claude/assignments/")
}

func frozenResultContextPath(record assignmentrecord.Record, execution *workspace.Execution, ref string) bool {
	for _, source := range record.ContextSources {
		if source.Ref == ref && !strings.HasPrefix(source.Kind, "embedded_") {
			return true
		}
	}
	if execution != nil && execution.Inputs[ref] != "" {
		return true
	}
	return false
}

func resultOwnerArtifactPrefix(record assignmentrecord.Record) string {
	return fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/", record.RuntimeID, record.BaselineGeneration, record.OwnerAgentID)
}

func isOwnerResultArtifact(prefix, ref string) bool {
	return strings.HasPrefix(ref, prefix) && path.Clean(ref) == ref
}

func canonicalResultEvidenceArtifactPath(record assignmentrecord.Record, resultID, ref, sha string) string {
	refHash := sha256Of([]byte(ref))[:16]
	return path.Join(resultOwnerArtifactPrefix(record), resultID+"-sources", refHash+"-"+sha+".bin")
}

func canonicalResultArtifactPath(record assignmentrecord.Record, resultID, sha string) string {
	return path.Join(resultOwnerArtifactPrefix(record), resultID+"-"+sha[:16]+".json")
}

func canonicalResultEvidenceID(record assignmentrecord.Record, revision int, submissionKey string) string {
	keyHash := sha256Of([]byte(submissionKey))[:12]
	return fmt.Sprintf("ev-completion-%s-g%d-a%d-s6-r%d-%s", record.Task.ID, record.BaselineGeneration, record.Revision, revision, keyHash)
}

func appendCanonicalResultEvidence(state map[string]any, result assignmentresult.AcceptedResult, path, digest string, occurredAt time.Time) error {
	items, ok := state["evidence"].([]any)
	if !ok {
		return fmt.Errorf("runtime evidence must be an array")
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item != nil && (item["id"] == result.EvidenceID || item["path"] == path) {
			return fmt.Errorf("canonical Result evidence %s is already registered", result.EvidenceID)
		}
	}
	items = append(items, map[string]any{
		"id": result.EvidenceID, "kind": "completion_report", "path": path, "sha256": digest,
		"status": "valid", "baseline_generation": result.BaselineGeneration, "review_round": nil,
		"produced_by": []any{result.ProducerAgentID}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": result.ProducerResponsibility, "scope_refs": []any{},
	})
	state["evidence"] = items
	state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	return nil
}

func canonicalResultReceipt(state map[string]any, result assignmentresult.AcceptedResult, read func(string) ([]byte, error), replayed bool) (ResultAcceptanceReceipt, error) {
	var receipt ResultAcceptanceReceipt
	rows, _ := state["evidence"].([]any)
	var found map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["id"] != result.EvidenceID || row["kind"] != "completion_report" {
			continue
		}
		if found != nil {
			return receipt, fmt.Errorf("canonical Result evidence id is ambiguous")
		}
		found = row
	}
	if found == nil {
		return receipt, fmt.Errorf("canonical Result has no original evidence index entry")
	}
	ref, _ := found["path"].(string)
	data, err := read(ref)
	if err != nil || sha256Of(data) != found["sha256"] {
		return receipt, fmt.Errorf("canonical Result receipt bytes differ from the original evidence index")
	}
	var taskRef, agentRef string
	if agent, ok := findCompletionAgent(state, result.ProducerAgentID); ok {
		agentRef, _ = agent["completion_reported_ref"].(string)
	}
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task != nil && task["id"] == result.TaskID {
			taskRef, _ = task["completion_report_ref"].(string)
			break
		}
	}
	return ResultAcceptanceReceipt{
		ResultRef: ref, ResultSHA256: fmt.Sprint(found["sha256"]), ResultRevision: result.ResultRevision,
		AnswerDigest: result.AnswerDigest, Replayed: replayed, Current: agentRef == ref && taskRef == ref,
	}, nil
}

func findCompletionAgent(state map[string]any, agentID string) (map[string]any, bool) {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent != nil && agent["id"] == agentID {
			return agent, true
		}
	}
	return nil, false
}

func asAnySlice(value any) []any {
	values, _ := value.([]any)
	return values
}

func canonicalRecordDigest(record assignmentrecord.Record) string {
	digest, _ := record.Digest()
	return digest
}
