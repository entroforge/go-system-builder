package assignment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"io"
	"path"
	"path/filepath"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/dispatch"
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
	canonicalResultAssetLimit   = 20 << 20
)

// ResultAcceptanceReceipt points to one original accepted Builder Result.
// Replaying a historical Result never moves the Agent/TASK current pointer
// backwards; Current reports whether those original pointers still name it.
type ResultAcceptanceReceipt struct {
	ResultRef      string `json:"result_ref"`
	ResultRevision int    `json:"result_revision"`
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
	Assertions         []assignmentresult.PlanAssertion `json:"assertion_checks"`
}

type resultExecutionView struct {
	MainRoot    string
	SourceRoot  string
	Execution   *workspace.Execution
	InputPaths  map[string]bool
	OwnerPrefix string
}

type stagedCanonicalResultArtifact struct {
	Path  string
	Bytes []byte
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
	for {
		snapshot, receipt, err := attemptCanonicalTaskWithReceipt(root, statePath, journalPath, request, data)
		if err != loopruntime.ErrStaleRevision {
			return snapshot, receipt, err
		}
		// Another Writer committed while this automatic request was preparing.
		// Re-read the original responsibility and accepted results; the normal
		// consumer resolves same-answer replay or validates the new state.
		// G18 §3.2: a stale Runtime CAS is no longer a refusal here. The
		// canonical consumer never asserts the Runtime write count, so a
		// legitimate professional completion is chased instead of being
		// rejected because the Runtime moved for an unrelated reason. The
		// protection is unchanged: the received row (the accepted Result for
		// this message_id), the journal-level dedup (the domain idempotency
		// key) and the Assignment Record's own revision decide the outcome.
		if request.Context != nil {
			if err := request.Context.Err(); err != nil {
				return loopruntime.Snapshot{}, ResultAcceptanceReceipt{}, err
			}
		}
	}
}

func attemptCanonicalTaskWithReceipt(
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
	// G18 §3.2: the canonical S6 consumer does not compare the Runtime write
	// count. The Runtime revision is an audit coordinate only
	// (L4-revision-usage.md:141). This acceptance is protected by the three
	// domain facts instead: the received row (the accepted Result for this
	// message_id, read again inside Apply), the journal-level dedup (the
	// domain idempotency key of this submission) and the Assignment Record's
	// own revision (binding.Record versus the current Record inside Apply).
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
		if !reflect.DeepEqual(acceptedKey.Answer, answer) {
			return empty, noReceipt, fmt.Errorf("completion message_id %s was already accepted with a different answer", message.MessageID)
		}
		receipt, err := canonicalResultReceipt(snapshot.State, acceptedKey, readMain, true)
		return snapshot, receipt, err
	}
	// An independent message carrying the same current professional answer
	// and actual received checks is an alias of the existing Result, not a new
	// Result. The old content-derived key is never recomputed.
	currentFacts, _, _ := CollectWorkerCheckFacts(ctx, root, snapshot.State, binding.Record)
	for i := len(history) - 1; i >= 0; i-- {
		if reflect.DeepEqual(history[i].Answer, answer) && sameActualWorkerCheckFacts(history[i].WorkerCheckFacts, currentFacts) {
			return registerCanonicalResultAlias(store, root, snapshot, binding.Record, history[i], message.MessageID, request, readMain)
		}
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	resultRevision := len(history) + 1
	resultID := canonicalResultEvidenceID(binding.Record, resultRevision, message.MessageID)
	var resultRel string
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
			Version: binding.Record.Task.Version,
		}},
		Conclusion:           "submitted",
		TaskID:               binding.Record.Task.ID,
		AssignmentRecord:     binding.Record.Reference(),
		ResultRevision:       resultRevision,
		SubmissionKey:        message.MessageID,
		Answer:               answer,
		PlanRef:              binding.PlanRef,
		ResolvedEvidenceRefs: []assignmentresult.ResolvedEvidence{},
		CreatedAt:            occurredAt.UTC().Format(time.RFC3339Nano),
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return empty, noReceipt, fmt.Errorf("load catalog: %w", err)
	}
	agentTransitions := agentEntityTransitions(catalog)
	taskTransitions := taskEntityTransitions(catalog)
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	mutation := loopruntime.Mutation{
		EventID:            fmt.Sprintf("evt-s6-result-%s-g%d-a%d-r%d", binding.Record.OwnerAgentID, binding.Record.BaselineGeneration, binding.Record.Revision, resultRevision),
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
				if !reflect.DeepEqual(prior.Answer, answer) {
					return fmt.Errorf("completion message_id %s was already accepted with a different answer", message.MessageID)
				}
				receipt, err := canonicalResultReceipt(state, prior, readMain, true)
				if err != nil {
					return err
				}
				return &canonicalResultReplay{Snapshot: currentSnapshot, Receipt: receipt}
			}
			if current.Record.Reference() != binding.Record.Reference() || len(currentHistory)+1 != resultRevision {
				return fmt.Errorf("canonical Assignment or Result history changed during acceptance")
			}
			if err := assignmentresult.ValidateAnswer(answer, current.Plan.Assertions); err != nil {
				return fmt.Errorf("canonical Result answer: %w", err)
			}
			if err := assignmentresult.ValidateClosingAssessment(current.Record, current.Plan.Assertions, answer); err != nil {
				return fmt.Errorf("canonical Result closing assessment: %w", err)
			}
			// Preserve only actual original check facts already held by this
			// responsibility. An unavailable receipt stays unavailable; it does
			// not turn professional answer reception into a new check gate.
			facts, _, _ := CollectWorkerCheckFacts(ctx, root, state, current.Record)
			result.WorkerCheckFacts = facts
			encoded, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("encode canonical Result: %w", err)
			}
			envelope := append(encoded, '\n')
			// Unregistered files, including identical contents, are not issued
			// Results. Only reserve an unoccupied path, then register that exact
			// path with the current responsibility in this Writer transaction.
			parent, _, err := workpacket.OpenParent(root, ".claude/evidence/s6-result-probe.json", true)
			if err != nil {
				return fmt.Errorf("open Result storage: %w", err)
			}
			parent.Close()
			actualRel := canonicalResultArtifactPath(current.Record, resultID)
			for attempt := 0; ; attempt++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				if attempt > 0 {
					actualRel = path.Join(".claude/evidence", fmt.Sprintf("s6-result-candidate-%d", attempt), resultID+".json")
				}
				created, err := workpacket.PublishNewBounded(root, actualRel, envelope, canonicalResultAssetLimit)
				if err != nil {
					return fmt.Errorf("publish canonical Result: %w", err)
				}
				if created {
					staged = append(staged, stagedCanonicalResultArtifact{Path: actualRel, Bytes: append([]byte(nil), envelope...)})
					break
				}
			}
			resultRel = actualRel
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
			if err := appendCanonicalResultEvidence(state, result, actualRel, occurredAt); err != nil {
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
				ArtifactBytes:    asset.Bytes,
				ReferencedPaths:  stateArtifactPaths(snapshot.State),
			})
			if cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("cleanup unreferenced Result artifact %s: %w", asset.Path, cleanupErr))
			}
		}
		return empty, noReceipt, err
	}
	receipt := ResultAcceptanceReceipt{ResultRef: resultRel, ResultRevision: resultRevision, Current: true}
	// The immutable bytes can include source references that changed between
	// preflight and the locked Apply. Resolve the exact indexed receipt from the
	// committed snapshot before returning it.
	accepted, err := assignmentresult.LoadAccepted(committed.State, binding.Record, resultRel, readMain)
	if err != nil {
		return committed, noReceipt, fmt.Errorf("validate newly accepted Result receipt: %w", err)
	}
	receipt.ResultRef, receipt.ResultRevision = resultRel, accepted.ResultRevision
	return committed, receipt, nil
}

// sameActualWorkerCheckFacts compares original receipt relationships and
// conclusions, never historical fingerprints or arbitrary author content.
func sameActualWorkerCheckFacts(left, right []assignmentresult.WorkerCheckFact) bool {
	if len(left) == 0 && len(right) != 0 {
		// Old accepted answers may predate the optional check projection.
		// Unknown or missing current checks do not force a second answer.
		for _, fact := range right {
			if fact.OriginalReceipt != nil || (fact.Status != "missing" && fact.Status != "unavailable") {
				return false
			}
		}
		return true
	}
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		a, b := left[i], right[i]
		if a.Index != b.Index || a.Command != b.Command || a.Status != b.Status || a.Record != b.Record || a.RuntimeID != b.RuntimeID || a.BaselineGeneration != b.BaselineGeneration || a.OwnerAgentID != b.OwnerAgentID || a.ExecutionGeneration != b.ExecutionGeneration {
			return false
		}
		if (a.OriginalReceipt == nil) != (b.OriginalReceipt == nil) {
			return false
		}
		if a.OriginalReceipt != nil && (a.OriginalReceipt.Path != b.OriginalReceipt.Path || a.OriginalReceipt.Binding != b.OriginalReceipt.Binding) {
			return false
		}
	}
	return true
}

func registerCanonicalResultAlias(store *loopruntime.Store, root string, snapshot loopruntime.Snapshot, record assignmentrecord.Record, target assignmentresult.AcceptedResult, key string, request CompletionRequest, read func(string) ([]byte, error)) (loopruntime.Snapshot, ResultAcceptanceReceipt, error) {
	// Historical Runtime schema versions without the alias index can still
	// reuse the original result directly; no author migration is required.
	version, _ := snapshot.State["schema_version"].(string)
	if version != "1.2.0" && version != "1.3.0" {
		receipt, err := canonicalResultReceipt(snapshot.State, target, read, true)
		return snapshot, receipt, err
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	mutation := loopruntime.Mutation{
		EventID:      fmt.Sprintf("evt-s6-result-alias-%s-a%d-r%d-%s", record.AssignmentID, record.Revision, target.ResultRevision, key),
		TransitionID: "BUILDER-RESULT", Event: "completion_reported", Actor: "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:s6-result-alias:%s:a%d:%s", record.AssignmentID, record.Revision, key),
		RuntimeID:      record.RuntimeID, BaselineGeneration: record.BaselineGeneration,
		From: cursor, To: cursor, OccurredAt: occurredAt,
		Message: "Associated a repeated Builder answer with its original accepted Result",
		Apply: func(state map[string]any) error {
			current, found, err := assignmentrecord.ForOwner(state, record.OwnerAgentID)
			if err != nil || !found || current.Reference() != record.Reference() {
				return fmt.Errorf("original Assignment changed during Result replay")
			}
			history, err := assignmentresult.AcceptedHistory(state, current, read)
			if err != nil {
				return err
			}
			for _, item := range history {
				if item.ResultRevision != target.ResultRevision {
					continue
				}
				facts, _, _ := CollectWorkerCheckFacts(store.Context(), root, state, current)
				if !reflect.DeepEqual(item.Answer, target.Answer) || !sameActualWorkerCheckFacts(item.WorkerCheckFacts, facts) {
					return fmt.Errorf("original Result or check facts changed during replay")
				}
				agent, ok := findCompletionAgent(state, record.OwnerAgentID)
				if !ok {
					return fmt.Errorf("original Result owner is missing")
				}
				rows, _ := agent["result_submission_bindings"].([]any)
				for _, row := range rows {
					if r, ok := row.(map[string]any); ok && r["submission_key"] == key {
						return nil
					}
				}
				if len(rows) >= resultSubmissionBindingLimit {
					return fmt.Errorf("Result alias index reached its existing limit")
				}
				agent["result_submission_bindings"] = append(rows, map[string]any{"record": record.Reference(), "submission_key": key, "result_ref": itemRef(state, item), "result_revision": item.ResultRevision})
				return nil
			}
			return fmt.Errorf("original Result is unavailable during replay")
		},
	}
	updated, err := store.Update(snapshot.Revision, mutation)
	if err != nil {
		return snapshot, ResultAcceptanceReceipt{}, err
	}
	receipt, err := canonicalResultReceipt(updated.State, target, read, true)
	return updated, receipt, err
}

func itemRef(state map[string]any, item assignmentresult.AcceptedResult) string {
	rows, _ := state["evidence"].([]any)
	for _, row := range rows {
		if r, ok := row.(map[string]any); ok && r["id"] == item.EvidenceID {
			ref, _ := r["path"].(string)
			return ref
		}
	}
	return ""
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
	PlanRef   string
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
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return binding, fmt.Errorf("invalid Assignment responsibility: %w", err)
	}
	generation := workspace.Generation(snapshot.State)
	runtimeID := workspace.RuntimeID(snapshot.State)
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "building" {
		return binding, fmt.Errorf("canonical Builder Result acceptance requires the original building lifecycle")
	}
	if message.RuntimeID != record.RuntimeID || runtimeID != record.RuntimeID || message.BaselineGeneration != record.BaselineGeneration || generation != record.BaselineGeneration ||
		message.AssignmentID != record.AssignmentID || message.AssignmentRevision != record.Revision ||
		message.AssignmentRecordContract != record.Contract ||
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
	// The original responsibility owns two structural facts about its Plan:
	// the Plan under reception must be the registered Plan of this owner, and
	// it must bind the current Assignment identity while staying inside that
	// Assignment's registered write domain. The registered reference is
	// resolved by coordinate first, so a Plan owned by another responsibility
	// can never pass as this one. These are structural facts about identity
	// and write domain; no revision, hash, digest, fingerprint, checksum,
	// round or CAS comparison decides any of them.
	planRef, _ := agent["plan_reported_ref"].(string)
	if planRef == "" {
		planRef = nativePlanPath
	}
	if planRef == "" {
		return binding, fmt.Errorf("Result requires the actual professional plan")
	}
	registeredPlanRef, err := plancheckpoint.ActivatedRef(root, snapshot, agentID)
	if err != nil {
		return binding, fmt.Errorf("resolve the registered professional Plan: %w", err)
	}
	if registeredPlanRef != "" && registeredPlanRef != planRef {
		return binding, fmt.Errorf("the professional Plan is not the registered Plan of the original responsibility")
	}
	if err := plancheckpoint.Validate(root, snapshot, agentID, planRef); err != nil {
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
	if plan.MessageType != "plan_report" || plan.MessageID == "" || plan.RuntimeID != record.RuntimeID || plan.AgentID != record.OwnerAgentID || plan.AgentDefinitionRef != record.AgentDefinition.Ref || plan.TaskID != record.Task.ID || plan.TeamID != record.TeamID || plan.AssignmentID != record.AssignmentID || plan.AssignmentRevision != record.Revision || plan.RecordContract != record.Contract {
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
	return canonicalResultBinding{Record: record, PlanRef: planRef, Plan: plan, View: view}, nil
}

func resultOwnerArtifactPrefix(record assignmentrecord.Record) string {
	return fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/", record.RuntimeID, record.BaselineGeneration, record.OwnerAgentID)
}

func canonicalResultArtifactPath(record assignmentrecord.Record, resultID string) string {
	return path.Join(resultOwnerArtifactPrefix(record), resultID+".json")
}

func canonicalResultEvidenceID(record assignmentrecord.Record, revision int, submissionKey string) string {
	return fmt.Sprintf("ev-completion-%s-g%d-a%d-s6-r%d", record.Task.ID, record.BaselineGeneration, record.Revision, revision)
}

func appendCanonicalResultEvidence(state map[string]any, result assignmentresult.AcceptedResult, path string, occurredAt time.Time) error {
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
		"id": result.EvidenceID, "kind": "completion_report", "path": path,
		"status": "valid", "baseline_generation": result.BaselineGeneration,
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
		ResultRef: ref, ResultRevision: result.ResultRevision, Replayed: replayed, Current: agentRef == ref && taskRef == ref,
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
