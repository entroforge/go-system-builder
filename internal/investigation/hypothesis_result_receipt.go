package investigation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/agentauth"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const hypothesisResultReceiptContract = "s8-hypothesis-result-receipt-v1"

// HypothesisResultReceipt is the original Case receipt wire. Runtime owns the
// neutral DTO and its recovery verifier; investigation only projects into the
// existing Case revision Writer.
type HypothesisResultReceipt = loopruntime.HypothesisResultReceipt

// SubmitIssuedHypothesisResult reads one exact issued authoring file, checks
// its current registered dispatch/source tuple, and submits through the
// original InvestigationCase Writer. It does not create a second result
// store or require Hook actor/session authorization.
func SubmitIssuedHypothesisResult(root, statePath, journalPath string, request IssuedHypothesisResultRequest) (loopruntime.Snapshot, error) {
	snapshot, _, err := ReceiveIssuedHypothesisResult(root, statePath, journalPath, request, true)
	return snapshot, err
}

// ReceiveIssuedHypothesisResult returns the original receipt directly, so a
// caller does not reopen the Case to reconstruct an already-known outcome.
// continueUnresolved comes from actual activity on the original responsibility;
// a replay does not inspect the private answer, regardless of its conclusion.
func ReceiveIssuedHypothesisResult(root, statePath, journalPath string, request IssuedHypothesisResultRequest, continueUnresolved bool) (loopruntime.Snapshot, HypothesisResultReceipt, error) {
	defer metrics.Span(request.Context, "reception/s8")()
	if strings.TrimSpace(root) == "" {
		return loopruntime.Snapshot{}, HypothesisResultReceipt{}, errors.New("S8 repository root is required")
	}
	if request.PacketID == "" || request.OwnerAgentID == "" {
		return loopruntime.Snapshot{}, HypothesisResultReceipt{}, errors.New("S8 packet and trusted owner are required")
	}
	writer := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	snapshot, err := writer.Snapshot()
	if err != nil {
		return loopruntime.Snapshot{}, HypothesisResultReceipt{}, err
	}
	if !loopruntime.PacketEnabled(snapshot.State) {
		return snapshot, HypothesisResultReceipt{}, errors.New("S8 save requires explicit Runtime v1.2/v1.3 packet mode")
	}
	packet, found, err := loopruntime.FindHypothesisResultPacket(snapshot.State, request.PacketID)
	if err != nil {
		return snapshot, HypothesisResultReceipt{}, err
	}
	if !found || packet.Binding.OwnerAgentID != request.OwnerAgentID {
		return snapshot, HypothesisResultReceipt{}, errors.New("S8 save has no exact packet issuance for the trusted owner")
	}
	existingReceipt, accepted, receiptErr := loopruntime.VerifiedHypothesisResultReceipt(root, snapshot.State, packet.ID, request.OwnerAgentID)
	if receiptErr != nil {
		return snapshot, HypothesisResultReceipt{}, receiptErr
	}
	// A resolved original question has no continuing attempt. An unresolved
	// question may receive a later delivery on its original responsibility;
	// its accepted fact never depends on a surviving private authoring file.
	if accepted && (!continueUnresolved || existingReceipt.Result != "inconclusive") {
		return snapshot, existingReceipt, nil
	}
	sourceRoot, _, err := hypothesisResultSourceRoot(root, snapshot.State, packet, false, request.Context)
	if err != nil {
		return snapshot, HypothesisResultReceipt{}, err
	}
	if request.SourceRoot != "" {
		requestedRoot, err := workspace.Canonical(request.SourceRoot)
		if err != nil {
			return snapshot, HypothesisResultReceipt{}, fmt.Errorf("resolve requested S8 source root: %w", err)
		}
		if requestedRoot != sourceRoot {
			return snapshot, HypothesisResultReceipt{}, errors.New("S8 answer source differs from the original registered execution root")
		}
	}
	if request.AuthorizeSource != nil {
		if err := request.AuthorizeSource(root, snapshot.State); err != nil {
			return snapshot, HypothesisResultReceipt{}, err
		}
	}
	answerPath, err := exactHypothesisAnswerPath(sourceRoot, request.AnswerPath, packet.AnswerPath)
	if err != nil {
		return snapshot, HypothesisResultReceipt{}, err
	}
	authoringBytes := request.frozenAnswer
	if authoringBytes == nil {
		authoringBytes, err = workpacket.ReadFile(sourceRoot, answerPath, hypothesisResultPacketReadLimit)
	}
	if err != nil {
		if accepted && os.IsNotExist(err) {
			return snapshot, existingReceipt, nil
		}
		return snapshot, HypothesisResultReceipt{}, fmt.Errorf("read exact S8 answer: %w", err)
	}
	answer, answerJSON, issues := workpacket.ParseHypothesisResultAnswer(packet, authoringBytes)
	if len(issues) != 0 {
		return snapshot, HypothesisResultReceipt{}, &workpacket.AnswerError{Label: "S8 answer", Fields: issues}
	}
	if accepted && bytes.Equal(existingReceipt.Answer, answerJSON) {
		return snapshot, existingReceipt, nil
	}
	if request.ExpectedRevision >= 0 && snapshot.Revision != request.ExpectedRevision {
		return snapshot, HypothesisResultReceipt{}, loopruntime.ErrStaleRevision
	}

	review, _ := snapshot.State["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	life, _ := snapshot.State["lifecycle"].(map[string]any)
	if pointer == nil || pointer["case_id"] != packet.Binding.CaseID || life["state"] != "bug_resolution" || life["phase"] != "investigation" {
		// An issued Investigator may return after its Case has been approved
		// or its stage has advanced. Retain the original answer without
		// rewriting the approved Case or moving the current lifecycle.
		return receiveLateHypothesisResult(writer, snapshot, packet, sourceRoot, answerJSON, request)
	}
	if err := hypothesisResultLifecycle(snapshot.State); err != nil {
		return snapshot, HypothesisResultReceipt{}, err
	}
	current, err := readCurrentInvestigationCase(root, snapshot.State)
	if err != nil {
		return snapshot, HypothesisResultReceipt{}, err
	}
	if err := validateS8ResultResponsibility(snapshot.State, current.document, packet); err != nil {
		return snapshot, HypothesisResultReceipt{}, err
	}
	if !accepted {
		if err := ensureHypothesisResultNotAlreadyOnCase(current.document, packet); err != nil {
			return snapshot, HypothesisResultReceipt{}, err
		}
	}

	commitRevision := snapshot.Revision
	journal, _ := snapshot.State["journal"].(map[string]any)
	acceptedEvent := fmt.Sprintf("evt-investigation-case-%s-r%d", packet.Binding.CaseID, commitRevision+1)
	receipt := loopruntime.HypothesisResultReceipt{
		Contract: hypothesisResultReceiptContract, PacketID: packet.ID,
		OwnerAgentID: packet.Binding.OwnerAgentID, RuntimeID: packet.Binding.RuntimeID,
		BaselineGeneration: packet.Binding.BaselineGeneration,
		AssignmentID:       packet.Binding.AssignmentID,
		TeamID:             packet.Binding.TeamID, TaskID: packet.Binding.TaskID, TaskRef: packet.Binding.TaskRef,
		ManifestRef:   packet.Binding.ManifestRef,
		ExecutionRoot: sourceRoot, IssuanceEventID: packet.IssuanceEventID,
		AcceptedEventID:              acceptedEvent,
		AcceptedIdempotencyKey:       fmt.Sprintf("runtime:investigation-case:%s:%d", packet.Binding.CaseID, commitRevision),
		AcceptedAfterRuntimeRevision: commitRevision + 1,
		AcceptedSequence:             intFieldS8(journal["last_sequence"]) + 1,
	}
	newQuestions := make([]map[string]any, 0, len(answer.NewHypotheses))
	for _, suggestion := range answer.NewHypotheses {
		data, err := json.Marshal(suggestion)
		if err != nil {
			return snapshot, HypothesisResultReceipt{}, err
		}
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			return snapshot, HypothesisResultReceipt{}, err
		}
		newQuestions = append(newQuestions, row)
	}
	result := *answer.Result
	validateCurrent := func(_ string, state, document map[string]any, _ []byte) error {
		return validateS8ResultResponsibility(state, document, packet)
	}
	requestToOriginal := HypothesisResultRequest{
		Context:          request.Context,
		ExpectedRevision: snapshot.Revision, ExpectedCaseRevision: current.revision,
		CaseID: packet.Binding.CaseID, HypothesisID: packet.Binding.HypothesisID, AssignmentID: packet.Binding.AssignmentID,
		Method: answer.Method, EvidenceRefs: append([]string(nil), answer.EvidenceRefs...), SourceBoundaryRefs: append([]string(nil), answer.SourceBoundaryRefs...),
		Observed: answer.Observed, Counterfactual: answer.CounterfactualOrDiscriminator, Result: result,
		ExplainsFindingIDs: append([]string(nil), answer.ExplainsFindingIDs...), DoesNotExplain: append([]string(nil), answer.DoesNotExplain...),
		NewHypotheses: newQuestions, OccurredAt: request.OccurredAt, typedPacket: &packet, typedReceipt: &receipt, typedValidation: validateCurrent,
	}
	updated, submitErr := SubmitHypothesisResult(root, statePath, journalPath, requestToOriginal)
	if submitErr == nil {
		receipt.Result, receipt.Answer = result, answerJSON
		return updated, receipt, nil
	}
	// If the original Runtime Writer committed but its acknowledgement was
	// lost, recover only from its exact durable Case receipt and journal proof.
	latest, readErr := writer.Snapshot()
	if readErr == nil {
		committed, ok, verifyErr := loopruntime.VerifiedHypothesisResultReceipt(root, latest.State, packet.ID, request.OwnerAgentID)
		if verifyErr == nil && ok && bytes.Equal(committed.Answer, answerJSON) && committed.ExecutionRoot == sourceRoot {
			return latest, committed, nil
		}
	}
	if errors.Is(submitErr, loopruntime.ErrStaleRevision) {
		if request.ExpectedRevision < 0 && request.concurrencyRetry < 3 && (request.Context == nil || request.Context.Err() == nil) {
			request.frozenAnswer = append([]byte(nil), authoringBytes...)
			request.concurrencyRetry++
			return ReceiveIssuedHypothesisResult(root, statePath, journalPath, request, continueUnresolved)
		}
		return loopruntime.Snapshot{}, HypothesisResultReceipt{}, fmt.Errorf("S8 packet %s for Case %s: %w; Main must inspect the original receipt and current Case/dispatch before resuming the original reception; preserve the professional answer at %s", packet.ID, packet.Binding.CaseID, submitErr, packet.AnswerPath)
	}
	return loopruntime.Snapshot{}, HypothesisResultReceipt{}, submitErr
}

// receiveLateHypothesisResult retains an independently issued responsibility
// after a newer Case became current. It does not alter the current Case pointer.
func receiveLateHypothesisResult(writer *loopruntime.Store, current loopruntime.Snapshot, packet workpacket.HypothesisResultPacket, sourceRoot string, answerJSON []byte, request IssuedHypothesisResultRequest) (loopruntime.Snapshot, HypothesisResultReceipt, error) {
	life, _ := current.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	journal, _ := current.State["journal"].(map[string]any)
	eventID := fmt.Sprintf("evt-s8-late-result-r%d", current.Revision+1)
	key := fmt.Sprintf("s8-late:%s:r%d", packet.ID, current.Revision)
	late := workpacket.HypothesisLateResult{Answer: json.RawMessage(append([]byte(nil), answerJSON...)), ExecutionRoot: sourceRoot,
		AcceptedEventID: eventID, AcceptedIdempotencyKey: key, AcceptedAfterRuntimeRevision: current.Revision + 1, AcceptedSequence: intFieldS8(journal["last_sequence"]) + 1}
	next, err := writer.Update(current.Revision, loopruntime.Mutation{EventID: eventID, TransitionID: "S8-LATE-RESULT", Event: "hypothesis_result_received",
		Actor: "orchestrator", IdempotencyKey: key, RuntimeID: packet.Binding.RuntimeID, BaselineGeneration: packet.Binding.BaselineGeneration,
		From: cursor, To: cursor, RetainLastTransition: true, OccurredAt: request.OccurredAt,
		Apply: func(state map[string]any) error {
			entities, _ := state["entities"].(map[string]any)
			rows, _ := entities["work_packets"].([]any)
			for _, item := range rows {
				row, _ := item.(map[string]any)
				if row["packet_id"] != packet.ID {
					continue
				}
				binding, _ := row["binding"].(map[string]any)
				if binding["case_id"] != packet.Binding.CaseID || binding["assignment_id"] != packet.Binding.AssignmentID || binding["owner_agent_id"] != packet.Binding.OwnerAgentID {
					return errors.New("S8 original late responsibility changed")
				}
				encoded, err := json.Marshal(late)
				if err != nil {
					return err
				}
				var entry map[string]any
				if err := json.Unmarshal(encoded, &entry); err != nil {
					return err
				}
				prior, _ := row["late_results"].([]any)
				row["late_results"] = append(prior, entry)
				return nil
			}
			return errors.New("S8 original late responsibility is unavailable")
		}})
	if err != nil {
		return loopruntime.Snapshot{}, HypothesisResultReceipt{}, err
	}
	receipt, found, err := loopruntime.VerifiedHypothesisResultReceipt("", next.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !found {
		return next, HypothesisResultReceipt{}, fmt.Errorf("S8 late result was saved but its original receipt is unavailable: %v", err)
	}
	return next, receipt, nil
}

func validateS8ApprovalMode(root string, state map[string]any, owner string) error {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent["id"] != owner {
			continue
		}
		mode, _ := agent["dispatch_mode"].(string)
		if mode == "plan_checkpoint" || mode == "one_shot" {
			return nil
		}
		// Explicit approval-mode and legacy dispatches retain their original
		// approved activation/readback chain. Continuous plan_checkpoint
		// work uses the issued professional Case questionnaire directly.
		return agentauth.ValidateWorkingAuthorization(root, state, owner)
	}
	return fmt.Errorf("S8 Investigator %s is not registered", owner)
}

// IssuedHypothesisResultReceipt is a read-only reconstruction from the
// original Case pointer/history and committed Runtime journal.
func IssuedHypothesisResultReceipt(root string, state map[string]any, packetID, owner string) (HypothesisResultReceipt, bool, error) {
	return loopruntime.VerifiedHypothesisResultReceipt(root, state, packetID, owner)
}

func validateHypothesisSubmissionBinding(state map[string]any, request HypothesisResultRequest) error {
	packets, err := loopruntime.HypothesisResultPackets(state)
	if err != nil {
		return err
	}
	var registered *workpacket.HypothesisResultPacket
	for i := range packets {
		b := packets[i].Binding
		if b.CaseID == request.CaseID && b.HypothesisID == request.HypothesisID && b.AssignmentID == request.AssignmentID {
			if registered != nil {
				return errors.New("S8 target has multiple issued original packets")
			}
			copyPacket := packets[i]
			registered = &copyPacket
		}
	}
	if request.typedPacket == nil {
		if registered != nil || request.typedReceipt != nil || request.typedValidation != nil {
			return errors.New("S8 target has an issued answer; use its exact original packet consumer")
		}
		return nil
	}
	if registered == nil || !reflect.DeepEqual(*registered, *request.typedPacket) || request.typedReceipt == nil || request.typedValidation == nil {
		return errors.New("S8 packet-backed Case mutation lacks its original Runtime packet and receipt capability")
	}
	b := request.typedPacket.Binding
	r := request.typedReceipt
	if b.CaseID != request.CaseID || b.HypothesisID != request.HypothesisID || b.AssignmentID != request.AssignmentID ||
		r.Contract != hypothesisResultReceiptContract || r.PacketID != request.typedPacket.ID || r.OwnerAgentID != b.OwnerAgentID ||
		r.RuntimeID != b.RuntimeID || r.BaselineGeneration != b.BaselineGeneration ||
		r.AssignmentID != b.AssignmentID || r.TeamID != b.TeamID ||
		r.TaskID != b.TaskID || r.TaskRef != b.TaskRef || r.ManifestRef != b.ManifestRef ||
		r.IssuanceEventID != request.typedPacket.IssuanceEventID {
		return errors.New("S8 packet/receipt identity differs from the target Hypothesis")
	}
	return nil
}

func exactHypothesisAnswerPath(sourceRoot, requested, expected string) (string, error) {
	if requested == "" {
		return "", errors.New("S8 answer path is required")
	}
	path := requested
	if filepath.IsAbs(path) {
		rootAbs, err := filepath.Abs(sourceRoot)
		if err != nil {
			return "", err
		}
		pathAbs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(rootAbs, pathAbs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return "", errors.New("S8 answer path escapes its original execution root")
		}
		path = rel
	}
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path != expected {
		return "", errors.New("S8 answer path differs from the exact original packet path")
	}
	return path, nil
}

func ensureHypothesisResultNotAlreadyOnCase(document map[string]any, packet workpacket.HypothesisResultPacket) error {
	rows, err := objectArrayAllowEmpty(document["hypothesis_results"], "InvestigationCase.hypothesis_results")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row["hypothesis_id"] == packet.Binding.HypothesisID && row["assignment_id"] == packet.Binding.AssignmentID {
			return errors.New("S8 original Case already contains this result but has no verified packet receipt")
		}
	}
	return nil
}

// validateS8ResultResponsibility checks only the original responsibility. It
// does not reopen manifests, evidence, sibling results or historical Cases.
func validateS8ResultResponsibility(state, document map[string]any, packet workpacket.HypothesisResultPacket) error {
	if document["case_id"] != packet.Binding.CaseID || document["status"] != "investigating" {
		return errors.New("S8 result no longer belongs to the current investigating Case")
	}
	question, err := uniqueHypothesis(document, packet.Binding.HypothesisID)
	if err != nil {
		return err
	}
	if question["assignment_id"] != packet.Binding.AssignmentID || !oneOf(stringField(question["status"]), "open", "inconclusive") {
		return errors.New("S8 result no longer belongs to the original open Assignment")
	}
	return nil
}
