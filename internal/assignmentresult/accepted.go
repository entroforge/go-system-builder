package assignmentresult

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

// EvidenceReadLimit bounds original Result envelopes and durable snapshots.
const EvidenceReadLimit int64 = 20 << 20

type SubjectReference struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// ResolvedEvidence identifies the actual bytes snapshotted by the original
// consumer. The author's logical Ref confers no source or check authority.
type ResolvedEvidence struct {
	Ref    string `json:"ref"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// AcceptedResult is an original completion evidence envelope. Submitted is
// acceptance of the professional answer, never a PASS or integration verdict.
type AcceptedResult struct {
	SchemaVersion          string                           `json:"schema_version"`
	EvidenceID             string                           `json:"evidence_id"`
	Kind                   string                           `json:"kind"`
	RuntimeID              string                           `json:"runtime_id"`
	BaselineGeneration     int                              `json:"baseline_generation"`
	ProducerAgentID        string                           `json:"producer_agent_id"`
	ProducerResponsibility string                           `json:"producer_responsibility"`
	SubjectRefs            []SubjectReference               `json:"subject_refs"`
	Conclusion             string                           `json:"conclusion"`
	TaskID                 string                           `json:"task_id"`
	AssignmentRecord       assignmentrecord.RecordReference `json:"assignment_record"`
	AssignmentRecordSHA256 string                           `json:"assignment_record_sha256"`
	ResultRevision         int                              `json:"result_revision"`
	SourceHead             string                           `json:"source_head,omitempty"`
	SubmissionKey          string                           `json:"submission_key"`
	AnswerDigest           string                           `json:"answer_digest"`
	Answer                 Answer                           `json:"answer"`
	WorkerCheckFacts       []WorkerCheckFact                `json:"worker_check_facts,omitempty"`
	ToolClosureDigest      string                           `json:"tool_closure_digest,omitempty"`
	PlanRef                string                           `json:"plan_ref"`
	PlanSHA256             string                           `json:"plan_sha256"`
	ResolvedEvidenceRefs   []ResolvedEvidence               `json:"resolved_evidence_refs"`
	CreatedAt              string                           `json:"created_at"`
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validRelative(ref string) bool {
	return ref != "" && ref != "." && ref != ".." && !strings.HasPrefix(ref, "/") && !strings.HasPrefix(ref, "../") && path.Clean(ref) == ref && !strings.ContainsAny(ref, "\\\x00\r\n")
}

// LoadAccepted proves original evidence registration and immutable sources.
// read must be the caller's original authority file view. This function
// does not mark assertions true or authorize a Runtime mutation.
func LoadAccepted(state map[string]any, record assignmentrecord.Record, resultRef string, read func(string) ([]byte, error)) (AcceptedResult, error) {
	return loadAccepted(state, record, resultRef, read, false, false)
}

// LoadContribution reads the immutable delivery history for a product baseline.
// Scope-invalidated evidence remains historical delivery provenance, never a
// current quality verdict. Callers must still verify the original Main receipt
// and review the current full product surface. No evidence status is changed.
func LoadContribution(state map[string]any, record assignmentrecord.Record, resultRef string, read func(string) ([]byte, error)) (AcceptedResult, error) {
	return loadAccepted(state, record, resultRef, read, true, false)
}

// LoadRecordedAccepted reads the original registered professional answer with
// current Assignment and immutable envelope binding. It does not revalidate the
// PLAN, Worker checks or evidence closure, and proves no current completion or
// integration eligibility. Use LoadAccepted for those original validations.
func LoadRecordedAccepted(state map[string]any, record assignmentrecord.Record, resultRef string, read func(string) ([]byte, error)) (AcceptedResult, error) {
	return loadAccepted(state, record, resultRef, read, false, true)
}

func loadAccepted(state map[string]any, record assignmentrecord.Record, resultRef string, read func(string) ([]byte, error), contribution, recordedOnly bool) (AcceptedResult, error) {
	var result AcceptedResult
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return result, err
	}
	registered, found, err := assignmentrecord.ForOwner(state, record.OwnerAgentID)
	if err != nil || !found {
		return result, fmt.Errorf("Result has no current original Assignment registration: %v", err)
	}
	registeredSHA, err := registered.Digest()
	expectedSHA, digestErr := record.Digest()
	if err != nil || digestErr != nil || registered.Reference() != record.Reference() || registeredSHA != expectedSHA {
		return result, fmt.Errorf("Result reader does not use the current original Assignment")
	}
	if read == nil || !validRelative(resultRef) {
		return result, fmt.Errorf("original Result reference and reader are required")
	}
	boundedRead := boundedEvidenceReader(read)
	var index map[string]any
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["path"] == resultRef {
			if index != nil {
				return result, fmt.Errorf("Result has duplicate original evidence registration")
			}
			index = row
		}
	}
	if index == nil || index["kind"] != "completion_report" || !acceptedIndexStatus(index, contribution) || number(index["baseline_generation"]) != record.BaselineGeneration || index["responsibility_id"] != "BUILD-WORK-PACKAGE" {
		return result, fmt.Errorf("Result has no valid original completion evidence registration")
	}
	if err := requireUniqueEvidenceID(rows, index); err != nil {
		return result, fmt.Errorf("Result evidence identity is ambiguous: %w", err)
	}
	data, err := boundedRead(resultRef)
	if err != nil {
		return result, fmt.Errorf("registered Result source %s is unavailable: %w", resultRef, err)
	}
	if digestBytes(data) != index["sha256"] {
		return result, fmt.Errorf("registered Result source %s changed", resultRef)
	}
	return validateAcceptedEnvelopeMode(record, index, data, boundedRead, recordedOnly)
}

// LoadSupersededAccepted validates an immutable Result from previousRecord,
// where successorRecord is its exact immediately following revision. The
// successor need not be the highest or current Assignment revision, and the
// supplied state need not identify that historical Runtime/generation as
// current. Callers that require a current successor must separately enforce
// that. The caller must first validate the original Runtime revision receipt
// and journal, then pass that receipt's exact event ID. This function
// verifies historical evidence bindings only; the event ID and old Result do
// not grant current-owner authority or PASS.
func LoadSupersededAccepted(
	state map[string]any,
	previousRecord assignmentrecord.Record,
	successorRecord assignmentrecord.Record,
	supersedingEventID string,
	resultRef string,
	read func(string) ([]byte, error),
) (AcceptedResult, error) {
	var zero AcceptedResult
	if strings.TrimSpace(supersedingEventID) == "" {
		return zero, fmt.Errorf("exact Assignment revision event ID is required")
	}
	if read == nil || !validRelative(resultRef) {
		return zero, fmt.Errorf("historical Result reference and reader are required")
	}
	if err := validateSupersededRecordPair(state, previousRecord, successorRecord); err != nil {
		return zero, err
	}
	boundedRead := boundedEvidenceReader(read)
	var index map[string]any
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["path"] == resultRef {
			if index != nil {
				return zero, fmt.Errorf("historical Result has duplicate original evidence registration")
			}
			index = row
		}
	}
	if index == nil || index["kind"] != "completion_report" || index["status"] != "superseded" || number(index["baseline_generation"]) != previousRecord.BaselineGeneration || index["responsibility_id"] != "BUILD-WORK-PACKAGE" || index["invalidated_by"] != supersedingEventID || index["invalidation_rule"] != "assignment_revision_issued" {
		return zero, fmt.Errorf("historical Result is not superseded by the exact Assignment revision event")
	}
	if !containsOwner(index["produced_by"], previousRecord.OwnerAgentID) {
		return zero, fmt.Errorf("historical Result evidence does not bind its original owner")
	}
	if err := requireUniqueEvidenceID(rows, index); err != nil {
		return zero, fmt.Errorf("historical Result evidence identity is ambiguous: %w", err)
	}
	data, err := boundedRead(resultRef)
	if err != nil || digestBytes(data) != index["sha256"] {
		return zero, fmt.Errorf("registered historical Result source is missing or changed")
	}
	return validateAcceptedEnvelope(previousRecord, index, data, boundedRead)
}

func requireUniqueEvidenceID(rows []any, target map[string]any) error {
	id, ok := target["id"].(string)
	if !ok || id == "" {
		return fmt.Errorf("registered evidence ID is missing")
	}
	count := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == id {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("registered evidence ID %q occurs %d times", id, count)
	}
	return nil
}

func boundedEvidenceReader(read func(string) ([]byte, error)) func(string) ([]byte, error) {
	return func(ref string) ([]byte, error) {
		if !validRelative(ref) {
			return nil, fmt.Errorf("evidence reference must be authority relative")
		}
		data, err := read(ref)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > EvidenceReadLimit {
			return nil, fmt.Errorf("evidence source exceeds %d byte limit", EvidenceReadLimit)
		}
		return data, nil
	}
}

func validateSupersededRecordPair(state map[string]any, previous, successor assignmentrecord.Record) error {
	if err := assignmentrecord.ValidateRecord(previous); err != nil {
		return fmt.Errorf("invalid previous Assignment Record: %w", err)
	}
	if err := assignmentrecord.ValidateRecord(successor); err != nil {
		return fmt.Errorf("invalid successor Assignment Record: %w", err)
	}
	if previous.RuntimeID != successor.RuntimeID || previous.BaselineGeneration != successor.BaselineGeneration || previous.AssignmentID != successor.AssignmentID || successor.Revision != previous.Revision+1 {
		return fmt.Errorf("historical Result Records are not consecutive revisions of one Assignment")
	}
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return fmt.Errorf("read original Assignment Record history: %w", err)
	}
	storedPrevious, found, err := assignmentrecord.FindRecordRevision(records, previous.Reference())
	if err != nil || !found {
		return fmt.Errorf("previous Assignment Record is absent from original Runtime history: %v", err)
	}
	storedSuccessor, found, err := assignmentrecord.FindRecordRevision(records, successor.Reference())
	if err != nil || !found {
		return fmt.Errorf("successor Assignment Record is absent from original Runtime history: %v", err)
	}
	previousSHA, err := previous.Digest()
	if err != nil {
		return err
	}
	storedPreviousSHA, err := storedPrevious.Digest()
	if err != nil || storedPreviousSHA != previousSHA {
		return fmt.Errorf("previous Assignment Record differs from its original Runtime bytes")
	}
	successorSHA, err := successor.Digest()
	if err != nil {
		return err
	}
	storedSuccessorSHA, err := storedSuccessor.Digest()
	if err != nil || storedSuccessorSHA != successorSHA {
		return fmt.Errorf("successor Assignment Record differs from its original Runtime bytes")
	}
	_, found, err = assignmentrecord.FindRecord(records, previous.RuntimeID, previous.BaselineGeneration, previous.AssignmentID)
	if err != nil || !found {
		return fmt.Errorf("Assignment Record history is not unique and continuous: %v", err)
	}
	issuance := storedSuccessor.RevisionIssuance
	if issuance == nil || issuance.PreviousRecord != previous.Reference() || issuance.PreviousRecordSHA256 != previousSHA {
		return fmt.Errorf("successor Assignment Record does not issue from the exact previous Record")
	}
	return nil
}

// validateAcceptedEnvelope checks the immutable Result, PLAN, answer and
// durable evidence closure shared by current and explicitly superseded
// readers. Registry status/current-owner authority remains in the public
// wrappers above.
func validateAcceptedEnvelope(record assignmentrecord.Record, index map[string]any, data []byte, read func(string) ([]byte, error)) (AcceptedResult, error) {
	return validateAcceptedEnvelopeMode(record, index, data, read, false)
}

func validateAcceptedEnvelopeMode(record assignmentrecord.Record, index map[string]any, data []byte, read func(string) ([]byte, error), recordedOnly bool) (AcceptedResult, error) {
	var result AcceptedResult
	if index["kind"] != "completion_report" || number(index["baseline_generation"]) != record.BaselineGeneration || index["responsibility_id"] != "BUILD-WORK-PACKAGE" {
		return result, fmt.Errorf("Result has invalid original evidence registration")
	}
	if digestBytes(data) != index["sha256"] {
		return result, fmt.Errorf("registered Result source is missing or changed")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode canonical Result: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("Result must contain exactly one JSON envelope")
	}
	sha, err := record.Digest()
	if err != nil {
		return result, err
	}
	if result.SchemaVersion != "1.0.0" || result.Kind != "completion_report" || result.Conclusion != "submitted" || result.EvidenceID != index["id"] || result.RuntimeID != record.RuntimeID || result.BaselineGeneration != record.BaselineGeneration || result.ProducerAgentID != record.OwnerAgentID || result.ProducerResponsibility != "BUILD-WORK-PACKAGE" || result.TaskID != record.Task.ID || result.AssignmentRecord != record.Reference() || result.AssignmentRecordSHA256 != sha || result.ResultRevision < 1 || result.SubmissionKey == "" || result.CreatedAt == "" {
		return result, fmt.Errorf("Result does not bind its original Assignment revision")
	}
	if record.Contract == assignmentrecord.NativeDeliveryContract {
		_, err := hex.DecodeString(result.SourceHead)
		if err != nil || (len(result.SourceHead) != 40 && len(result.SourceHead) != 64) || strings.ToLower(result.SourceHead) != result.SourceHead {
			return result, fmt.Errorf("native Result has no actual accepted Git source commit")
		}
	}
	if !containsOwner(index["produced_by"], record.OwnerAgentID) {
		return result, fmt.Errorf("Result evidence does not bind its original owner")
	}
	if _, err := time.Parse(time.RFC3339Nano, result.CreatedAt); err != nil {
		return result, fmt.Errorf("Result has an invalid original acceptance time: %w", err)
	}
	if len(result.SubjectRefs) != 1 || result.SubjectRefs[0] != (SubjectReference{ID: record.Task.ID, Kind: "task", Path: record.Task.Ref, Version: record.Task.Version, SHA256: record.Task.SHA256}) {
		return result, fmt.Errorf("Result subjects differ from the registered TASK")
	}
	if recordedOnly {
		answerSHA, err := AnswerDigest(result.Answer)
		if err != nil || answerSHA != result.AnswerDigest {
			return result, fmt.Errorf("Result professional answer digest changed")
		}
		return result, nil
	}
	if !validRelative(result.PlanRef) {
		return result, fmt.Errorf("Result plan source must be authority relative")
	}
	planBytes, err := read(result.PlanRef)
	if err != nil || digestBytes(planBytes) != result.PlanSHA256 {
		return result, fmt.Errorf("accepted Result PLAN source changed")
	}
	var plan struct {
		MessageType        string          `json:"message_type"`
		AgentID            string          `json:"agent_id"`
		RuntimeID          string          `json:"runtime_id"`
		AssignmentID       string          `json:"assignment_id"`
		AssignmentRevision int             `json:"assignment_revision"`
		RecordSHA256       string          `json:"assignment_record_sha256"`
		Assertions         []PlanAssertion `json:"assertion_checks"`
	}
	if err := json.Unmarshal(planBytes, &plan); err != nil || plan.MessageType != "plan_report" || plan.AgentID != record.OwnerAgentID || plan.RuntimeID != record.RuntimeID || plan.AssignmentID != record.AssignmentID || plan.AssignmentRevision != record.Revision || plan.RecordSHA256 != sha {
		return result, fmt.Errorf("Result PLAN does not bind its original Assignment revision")
	}
	if err := ValidateAnswer(result.Answer, plan.Assertions); err != nil {
		return result, err
	}
	if err := ValidateClosingAssessment(record, plan.Assertions, result.Answer); err != nil {
		return result, err
	}
	if err := ValidateWorkerClosure(record, result, read); err != nil {
		return result, fmt.Errorf("validate accepted Worker check closure: %w", err)
	}
	answerSHA, err := AnswerDigest(result.Answer)
	if err != nil || answerSHA != result.AnswerDigest {
		return result, fmt.Errorf("Result professional answer digest changed")
	}
	refs := map[string]bool{}
	for _, assertion := range result.Answer.AssertionEvidence {
		for _, ref := range assertion.EvidenceRefs {
			refs[ref] = true
		}
	}
	seen := map[string]bool{}
	sourcePrefix := fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/", record.RuntimeID, record.BaselineGeneration, record.OwnerAgentID)
	for _, source := range result.ResolvedEvidenceRefs {
		if !refs[source.Ref] || seen[source.Ref] || !validRelative(source.Path) || !strings.HasPrefix(source.Path, sourcePrefix) {
			return result, fmt.Errorf("Result has invalid or duplicate resolved evidence")
		}
		seen[source.Ref] = true
		body, err := read(source.Path)
		if err != nil || digestBytes(body) != source.SHA256 {
			return result, fmt.Errorf("Result evidence snapshot %s changed", source.Ref)
		}
	}
	if len(seen) != len(refs) {
		return result, fmt.Errorf("Result lacks resolved assertion evidence")
	}
	return result, nil
}

// AcceptedHistory returns only this Record revision's original registered
// Results. Revision numbering is explicit, unique and continuous.
func AcceptedHistory(state map[string]any, record assignmentrecord.Record, read func(string) ([]byte, error)) ([]AcceptedResult, error) {
	return acceptedHistory(state, record, read, false)
}

// ContributionHistory retains exact registered delivery revisions after scope
// invalidation without granting them current PASS or integration authority.
func ContributionHistory(state map[string]any, record assignmentrecord.Record, read func(string) ([]byte, error)) ([]AcceptedResult, error) {
	return acceptedHistory(state, record, read, true)
}

func acceptedHistory(state map[string]any, record assignmentrecord.Record, read func(string) ([]byte, error), contribution bool) ([]AcceptedResult, error) {
	var results []AcceptedResult
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["kind"] != "completion_report" || number(row["baseline_generation"]) != record.BaselineGeneration || !containsOwner(row["produced_by"], record.OwnerAgentID) {
			continue
		}
		ref, _ := row["path"].(string)
		data, err := read(ref)
		if err != nil {
			return nil, err
		}
		var binding struct {
			AssignmentRecord assignmentrecord.RecordReference `json:"assignment_record"`
		}
		if err := json.Unmarshal(data, &binding); err != nil {
			return nil, err
		}
		if binding.AssignmentRecord != record.Reference() {
			continue
		}
		result, err := loadAccepted(state, record, ref, read, contribution, false)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ResultRevision < results[j].ResultRevision })
	keys := map[string]bool{}
	identities := map[string]bool{}
	for i, result := range results {
		closureDigest, err := EffectiveToolClosureDigest(record, result)
		if err != nil {
			return nil, fmt.Errorf("Result history has invalid tool closure identity: %w", err)
		}
		identity := result.AnswerDigest + "\x00" + closureDigest
		if result.ResultRevision != i+1 || keys[result.SubmissionKey] || identities[identity] {
			return nil, fmt.Errorf("Result history has duplicate key, content identity or non-contiguous revision")
		}
		keys[result.SubmissionKey] = true
		identities[identity] = true
	}
	return results, nil
}

func number(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
func containsOwner(raw any, owner string) bool {
	switch rows := raw.(type) {
	case []any:
		for _, row := range rows {
			if row == owner {
				return true
			}
		}
	case []string:
		for _, row := range rows {
			if row == owner {
				return true
			}
		}
	}
	return false
}

// Only original source-scope invalidation can preserve contribution provenance.
// Assignment supersession continues through LoadSupersededAccepted's exact
// revision-event proof; arbitrary invalid/missing/retired registrations fail.
func acceptedIndexStatus(index map[string]any, contribution bool) bool {
	if index["status"] == "valid" {
		return true
	}
	if !contribution || index["status"] != "invalid" {
		return false
	}
	by, _ := index["invalidated_by"].(string)
	reason, _ := index["invalidation_reason"].(string)
	if strings.TrimSpace(by) == "" || strings.TrimSpace(reason) == "" {
		return false
	}
	switch index["invalidation_rule"] {
	case "unscoped_evidence", "scope_overlap", "change_impact_invalid":
		return true
	}
	return false
}
