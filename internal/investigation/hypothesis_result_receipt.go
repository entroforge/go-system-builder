package investigation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/agentauth"
	"github.com/entroforge/go-system-builder/internal/evidence"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
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
	defer metrics.Span(request.Context, "reception/s8")()
	if strings.TrimSpace(root) == "" {
		return loopruntime.Snapshot{}, errors.New("S8 repository root is required")
	}
	if request.PacketID == "" || request.OwnerAgentID == "" {
		return loopruntime.Snapshot{}, errors.New("S8 packet and trusted owner are required")
	}
	writer := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	snapshot, err := writer.Snapshot()
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	if !loopruntime.PacketEnabled(snapshot.State) {
		return snapshot, errors.New("S8 save requires explicit Runtime v1.2/v1.3 packet mode")
	}
	packet, found, err := loopruntime.FindHypothesisResultPacket(snapshot.State, request.PacketID)
	if err != nil {
		return snapshot, err
	}
	if !found || packet.Binding.OwnerAgentID != request.OwnerAgentID {
		return snapshot, errors.New("S8 save has no exact packet issuance for the trusted owner")
	}
	if err := loopruntime.VerifyHypothesisResultPacketIssue(root, snapshot.State, packet); err != nil {
		return snapshot, err
	}
	// A prior acceptance changes mutable Case/Agent status. Check the original
	// immutable receipt first so exact replay can use the registered source
	// identity without requiring a live Worker checkout or re-running work.
	existingReceipt, accepted, receiptErr := loopruntime.VerifiedHypothesisResultReceipt(root, snapshot.State, packet.ID, request.OwnerAgentID)
	if receiptErr != nil {
		return snapshot, receiptErr
	}
	sourceRoot, _, err := hypothesisResultSourceRoot(root, snapshot.State, packet, !accepted, request.Context)
	if err != nil {
		return snapshot, err
	}
	if request.SourceRoot != "" {
		requestedRoot, err := workspace.Canonical(request.SourceRoot)
		if err != nil {
			return snapshot, fmt.Errorf("resolve requested S8 source root: %w", err)
		}
		if requestedRoot != sourceRoot {
			return snapshot, errors.New("S8 answer source differs from the original registered execution root")
		}
	}
	if request.AuthorizeSource != nil {
		if err := request.AuthorizeSource(root, snapshot.State); err != nil {
			return snapshot, err
		}
	}
	answerPath, err := exactHypothesisAnswerPath(sourceRoot, request.AnswerPath, packet.AnswerPath)
	if err != nil {
		return snapshot, err
	}
	authoringBytes, err := workpacket.ReadFile(sourceRoot, answerPath, hypothesisResultPacketReadLimit)
	if err != nil {
		return snapshot, fmt.Errorf("read exact S8 answer: %w", err)
	}
	answerJSON, issues := workpacket.ValidateHypothesisResultAnswer(packet, authoringBytes)
	if len(issues) != 0 {
		return snapshot, &workpacket.AnswerError{Label: "S8 answer", Fields: issues}
	}
	answerSHA := workpacket.Digest(answerJSON)
	var answer workpacket.HypothesisResultAnswer
	if err := json.Unmarshal(answerJSON, &answer); err != nil {
		return snapshot, err
	}
	// Bind new local observations while retaining the original author JSON and
	// digest in the receipt. Frozen Finding references are never rebound.
	if err := bindS8LocalEvidence(root, sourceRoot, packet, &answer, accepted); err != nil {
		return snapshot, err
	}
	if accepted {
		if err := verifyS8ReplayedLocalEvidence(root, existingReceipt, packet, answer); err != nil {
			return snapshot, err
		}
		if existingReceipt.AnswerSHA256 != answerSHA || existingReceipt.ExecutionRoot != sourceRoot {
			return snapshot, errors.New("S8 packet replay conflicts with the original accepted answer/source")
		}
		return snapshot, nil
	}
	if request.ExpectedRevision >= 0 && snapshot.Revision != request.ExpectedRevision {
		return snapshot, loopruntime.ErrStaleRevision
	}

	if err := hypothesisResultLifecycle(snapshot.State); err != nil {
		return snapshot, err
	}
	if err := validateS8AnswerEvidence(root, snapshot.State, packet, answer); err != nil {
		return snapshot, err
	}
	if err := validateS8ApprovalMode(root, snapshot.State, request.OwnerAgentID); err != nil {
		return snapshot, err
	}
	current, err := readCurrentInvestigationCase(root, snapshot.State)
	if err != nil {
		return snapshot, err
	}
	dispatches, err := resolveHypothesisDispatches(root, snapshot.State, current)
	if err != nil {
		return snapshot, err
	}
	dispatch, ok := findHypothesisDispatch(dispatches, packet.Binding.AssignmentID, request.OwnerAgentID)
	if !ok {
		return snapshot, errors.New("S8 packet no longer has the exact current registered Investigator dispatch")
	}
	if err := verifyHypothesisResultPacketCurrent(root, snapshot.State, current, dispatch, packet); err != nil {
		return snapshot, fmt.Errorf("S8 packet %s for Case %s: %w; Main must inspect the original receipt and current Case/dispatch before resuming reception; preserve the professional answer at %s", packet.ID, packet.Binding.CaseID, err, packet.AnswerPath)
	}
	if err := ensureHypothesisResultNotAlreadyOnCase(current.document, packet); err != nil {
		return snapshot, err
	}

	commitRevision := snapshot.Revision
	answerSHAHex := answerSHA
	packetSHA, err := workpacket.HypothesisResultPacketDigest(packet)
	if err != nil {
		return snapshot, err
	}
	journal, _ := snapshot.State["journal"].(map[string]any)
	acceptedEvent := fmt.Sprintf("evt-investigation-case-%s-r%d", packet.Binding.CaseID, commitRevision+1)
	successorRevision := current.revision + 1
	successorRef := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "cases", packet.Binding.CaseID+fmt.Sprintf("-r%d.json", successorRevision)))
	receipt := loopruntime.HypothesisResultReceipt{
		Contract: hypothesisResultReceiptContract, PacketID: packet.ID, PacketSHA256: packetSHA,
		AnswerSHA256: answerSHAHex, SubmittedAnswer: append(json.RawMessage(nil), answerJSON...),
		OwnerAgentID: packet.Binding.OwnerAgentID, RuntimeID: packet.Binding.RuntimeID,
		BaselineGeneration: packet.Binding.BaselineGeneration, ReviewRound: packet.Binding.ReviewRound,
		AssignmentID: packet.Binding.AssignmentID, AssignmentSHA256: packet.Binding.AssignmentSHA256,
		TeamID: packet.Binding.TeamID, TaskID: packet.Binding.TaskID, TaskRef: packet.Binding.TaskRef,
		TaskSHA256: packet.Binding.TaskSHA256, ManifestRef: packet.Binding.ManifestRef, ManifestSHA256: packet.Binding.ManifestSHA256,
		ExecutionRoot: sourceRoot, IssuanceEventID: packet.IssuanceEventID,
		AcceptedEventID:              acceptedEvent,
		AcceptedIdempotencyKey:       fmt.Sprintf("runtime:investigation-case:%s:%d", packet.Binding.CaseID, commitRevision),
		AcceptedAfterRuntimeRevision: commitRevision + 1,
		AcceptedSequence:             intFieldS8(journal["last_sequence"]) + 1,
		SuccessorCaseRef:             successorRef, SuccessorCaseRevision: successorRevision,
	}
	newQuestions, err := materializeS8NewHypotheses(packet.ID, answer.NewHypotheses)
	if err != nil {
		return snapshot, err
	}
	result := *answer.Result
	validateCurrent := func(lockedRoot string, state, document map[string]any, caseBytes []byte) error {
		if !bytesEqualDigest(caseBytes, current.sha) {
			return errors.New("S8 current Case bytes differ from the pinned preflight revision")
		}
		if err := validateS8RuntimeCaseRoute(state, document); err != nil {
			return err
		}
		if err := hypothesisResultLifecycle(state); err != nil {
			return err
		}
		if err := validateS8ApprovalMode(lockedRoot, state, request.OwnerAgentID); err != nil {
			return err
		}
		if err := loopruntime.VerifyHypothesisResultPacketIssue(lockedRoot, state, packet); err != nil {
			return err
		}
		lockedSourceRoot, _, err := hypothesisResultSourceRoot(lockedRoot, state, packet, true, request.Context)
		if err != nil {
			return err
		}
		if lockedSourceRoot != sourceRoot {
			return errors.New("S8 registered execution root changed before original Case commit")
		}
		lockedCurrent := s8CurrentCase{document: document, bytes: caseBytes, path: current.path, sha: current.sha, revision: current.revision, findings: current.findings}
		lockedDispatches, err := resolveHypothesisDispatches(lockedRoot, state, lockedCurrent)
		if err != nil {
			return err
		}
		lockedDispatch, ok := findHypothesisDispatch(lockedDispatches, packet.Binding.AssignmentID, request.OwnerAgentID)
		if !ok {
			return errors.New("S8 registered Investigator dispatch changed before original Case CAS")
		}
		if err := verifyHypothesisResultPacketCurrent(lockedRoot, state, lockedCurrent, lockedDispatch, packet); err != nil {
			return err
		}
		return validateS8AnswerEvidence(lockedRoot, state, packet, answer)
	}
	requestToOriginal := HypothesisResultRequest{
		Context:          request.Context,
		ExpectedRevision: snapshot.Revision, ExpectedCaseRevision: current.revision, ExpectedCaseSHA256: current.sha,
		CaseID: packet.Binding.CaseID, HypothesisID: packet.Binding.HypothesisID, AssignmentID: packet.Binding.AssignmentID,
		Method: answer.Method, EvidenceRefs: append([]string(nil), answer.EvidenceRefs...), SourceBoundaryRefs: append([]string(nil), answer.SourceBoundaryRefs...),
		Observed: answer.Observed, Counterfactual: answer.CounterfactualOrDiscriminator, Result: result,
		ExplainsFindingIDs: append([]string(nil), answer.ExplainsFindingIDs...), DoesNotExplain: append([]string(nil), answer.DoesNotExplain...),
		NewHypotheses: newQuestions, OccurredAt: request.OccurredAt, typedPacket: &packet, typedReceipt: &receipt, typedValidation: validateCurrent,
	}
	updated, submitErr := SubmitHypothesisResult(root, statePath, journalPath, requestToOriginal)
	if submitErr == nil {
		return updated, nil
	}
	// If the original Runtime Writer committed but its acknowledgement was
	// lost, recover only from its exact durable Case receipt and journal proof.
	latest, readErr := writer.Snapshot()
	if readErr == nil {
		committed, ok, verifyErr := loopruntime.VerifiedHypothesisResultReceipt(root, latest.State, packet.ID, request.OwnerAgentID)
		if verifyErr == nil && ok && committed.AnswerSHA256 == answerSHAHex && committed.ExecutionRoot == sourceRoot {
			return latest, nil
		}
	}
	if errors.Is(submitErr, loopruntime.ErrStaleRevision) {
		return loopruntime.Snapshot{}, fmt.Errorf("S8 packet %s for Case %s: %w; Main must inspect the original receipt and current Case/dispatch before resuming the original reception; preserve the professional answer at %s", packet.ID, packet.Binding.CaseID, submitErr, packet.AnswerPath)
	}
	return loopruntime.Snapshot{}, submitErr
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
		r.RuntimeID != b.RuntimeID || r.BaselineGeneration != b.BaselineGeneration || r.ReviewRound != b.ReviewRound ||
		r.AssignmentID != b.AssignmentID || r.AssignmentSHA256 != b.AssignmentSHA256 || r.TeamID != b.TeamID ||
		r.TaskID != b.TaskID || r.TaskRef != b.TaskRef || r.TaskSHA256 != b.TaskSHA256 || r.ManifestRef != b.ManifestRef || r.ManifestSHA256 != b.ManifestSHA256 ||
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

func validateS8AnswerEvidence(root string, state map[string]any, packet workpacket.HypothesisResultPacket, answer workpacket.HypothesisResultAnswer) error {
	refs, err := remainingS8AnswerEvidenceRefs(root, &packet, answer.EvidenceRefs)
	if err != nil {
		return err
	}
	for _, question := range answer.NewHypotheses {
		questionRefs := question.EvidenceRefs
		if packet.Binding.ObservationBatchRef != "" {
			current, err := readCurrentInvestigationCase(root, state)
			if err != nil {
				return err
			}
			questionRefs, err = remainingFindingQuestionRefs(root, state, current.document, question.SourceFindingIDs, questionRefs)
			if err != nil {
				return err
			}
		}
		refs = append(refs, questionRefs...)
	}
	if packet.Binding.ObservationBatchRef == "" {
		data, err := workpacket.ReadFile(root, packet.Binding.CaseRef, hypothesisResultPacketReadLimit)
		if err != nil || workpacket.Digest(data) != packet.Binding.CaseSHA256 {
			return fmt.Errorf("S8 original human Case bytes/SHA differ: %v", err)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			return err
		}
		source, err := humanCaseSource(root, state, document)
		if err != nil {
			return err
		}
		remaining := withoutHumanSource(refs, source)
		if len(remaining) > 0 {
			if err := ValidateEvidenceRefs(remaining, EvidenceAttestationOptions{AllowPaths: true, State: state, Root: root, RequireSHA: true, RequireReviewRound: packet.Binding.ReviewRound}); err != nil {
				return fmt.Errorf("S8 answer evidence references: %w", err)
			}
		}
		boundaryError := func(ref string) error {
			return &workpacket.AnswerError{Label: "S8 human source boundary", Fields: []workpacket.FieldError{{Pointer: "/answer/source_boundary_refs", Expected: "the frozen original human report", Observed: ref, Repair: "Cite the supplied original human decision ID or exact path; do not invent a Finding or Batch."}}}
		}
		if len(answer.SourceBoundaryRefs) == 0 {
			return boundaryError("empty")
		}
		for _, ref := range answer.SourceBoundaryRefs {
			if !humanSourceRef(ref, source) {
				return boundaryError(ref)
			}
		}
	} else {
		if len(refs) > 0 {
			if err := ValidateEvidenceRefs(refs, EvidenceAttestationOptions{AllowPaths: true, State: state, Root: root, RequireSHA: true, RequireReviewRound: packet.Binding.ReviewRound}); err != nil {
				return fmt.Errorf("S8 answer evidence references: %w", err)
			}
		}
		if err := validateS8SourceBoundaryRefs(root, packet, answer.SourceBoundaryRefs); err != nil {
			return err
		}
	}
	return nil
}

// Only exact sources imported from the original verified Finding may bypass
// index lookup. Follow-up questions separately verify their own selected
// Findings; arbitrary new answer paths retain the original evidence contract. The packet consumer verifies issuance/continuity
// before calling this helper and again inside the original Writer.
func remainingS8AnswerEvidenceRefs(root string, packet *workpacket.HypothesisResultPacket, refs []string) ([]string, error) {
	frozen := map[string]bool{}
	if packet != nil {
		for _, ref := range packet.Inputs.EvidenceRefs {
			if ref.Attestation == "finding_source" && ref.Origin == "finding" {
				frozen[ref.Ref] = true
			}
		}
	}
	remaining := make([]string, 0, len(refs))
	for _, ref := range refs {
		if !frozen[ref] {
			remaining = append(remaining, ref)
			continue
		}
		if err := evidence.ValidateFindingReference(root, ref); err != nil {
			return nil, fmt.Errorf("S8 answer original Finding source %q: %w", ref, err)
		}
	}
	return remaining, nil
}

func validateS8SourceBoundaryRefs(root string, packet workpacket.HypothesisResultPacket, refs []string) error {
	if len(refs) == 0 {
		return errors.New("S8 source_boundary_refs must cite frozen source evidence")
	}
	findingByID := make(map[string]map[string]any, len(packet.Inputs.Findings))
	for _, finding := range packet.Inputs.Findings {
		var document map[string]any
		if err := json.Unmarshal(finding.Document, &document); err != nil {
			return err
		}
		findingByID[finding.FindingID] = document
	}
	allowed := make(map[string]bool, len(findingByID))
	for id := range findingByID {
		allowed["finding:"+id] = true
	}
	batchBytes, err := workpacket.ReadFile(root, packet.Binding.ObservationBatchRef, hypothesisResultPacketReadLimit)
	if err != nil || workpacket.Digest(batchBytes) != packet.Binding.ObservationBatchSHA256 {
		return fmt.Errorf("S8 original ObservationBatch bytes/SHA differ while checking boundaries: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("observation-batch.schema.json", batchBytes); err != nil {
		return err
	}
	var batch struct {
		ClaimCoverageSummary struct {
			BlockedClaims []struct {
				EvidenceRefs []string `json:"evidence_refs"`
			} `json:"blocked_claims"`
		} `json:"claim_coverage_summary"`
	}
	if err := json.Unmarshal(batchBytes, &batch); err != nil {
		return err
	}
	for _, blocked := range batch.ClaimCoverageSummary.BlockedClaims {
		for _, ref := range blocked.EvidenceRefs {
			allowed[ref] = true
		}
	}
	for _, ref := range refs {
		if allowed[ref] {
			continue
		}
		if !strings.HasPrefix(ref, "finding:") {
			return fmt.Errorf("S8 source boundary %q is neither a frozen Finding encounter nor a sealed blocked-Claim reference", ref)
		}
		parts := strings.SplitN(strings.TrimPrefix(ref, "finding:"), "#", 2)
		finding := findingByID[parts[0]]
		if finding == nil {
			return fmt.Errorf("S8 source boundary %q names no frozen Case Finding", ref)
		}
		encounter, ok := finding["encounter"].(map[string]any)
		if !ok || encounter == nil {
			return fmt.Errorf("S8 source boundary %q has no original Finding encounter", ref)
		}
		if len(parts) == 1 {
			continue // finding:<id> identifies the full frozen encounter.
		}
		if !strings.HasPrefix(parts[1], "/encounter/") {
			return fmt.Errorf("S8 source boundary %q is not an encounter JSON Pointer", ref)
		}
		value, found := resolveS8JSONPointer(finding, parts[1])
		if !found || value == nil {
			return fmt.Errorf("S8 source boundary %q does not resolve in the frozen Finding", ref)
		}
	}
	return nil
}

func resolveS8JSONPointer(document any, pointer string) (any, bool) {
	if pointer == "" {
		return document, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	current := document
	for _, encoded := range strings.Split(pointer[1:], "/") {
		var token strings.Builder
		for i := 0; i < len(encoded); i++ {
			if encoded[i] != '~' {
				token.WriteByte(encoded[i])
				continue
			}
			if i+1 >= len(encoded) {
				return nil, false
			}
			i++
			switch encoded[i] {
			case '0':
				token.WriteByte('~')
			case '1':
				token.WriteByte('/')
			default:
				return nil, false
			}
		}
		key := token.String()
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[key]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(value) || strconv.Itoa(index) != key {
				return nil, false
			}
			current = value[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func materializeS8NewHypotheses(packetID string, authored []workpacket.HypothesisNewQuestion) ([]map[string]any, error) {
	rows := make([]map[string]any, 0, len(authored))
	for i, question := range authored {
		sum := sha256.Sum256([]byte(packetID + "\x00" + strconv.Itoa(i)))
		suffix := hex.EncodeToString(sum[:])
		rows = append(rows, map[string]any{
			"hypothesis_id":      "hypothesis-s8-" + suffix,
			"assignment_id":      "assignment-s8-" + suffix,
			"statement":          strings.TrimSpace(question.Statement),
			"invariant":          strings.TrimSpace(question.Invariant),
			"discriminator":      strings.TrimSpace(question.Discriminator),
			"expected_outcomes":  map[string]any{"support": strings.TrimSpace(question.ExpectedOutcomes.Support), "refute": strings.TrimSpace(question.ExpectedOutcomes.Refute)},
			"source_finding_ids": stringSliceAny(question.SourceFindingIDs),
			"evidence_refs":      stringSliceAny(question.EvidenceRefs),
			"status":             "open",
		})
	}
	return rows, nil
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

func bytesEqualDigest(data []byte, expected string) bool {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == expected
}
