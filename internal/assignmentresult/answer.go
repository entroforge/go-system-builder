// Package assignmentresult contains the typed professional answer for the
// opt-in S6 Result contract. It deliberately does not accept or verify
// machine-authored Git, runner, or evidence facts.
package assignmentresult

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

const (
	AnswerSchemaVersion = "1.0.0"
	CompletionMessage   = "completion_report"
)

// PlanAssertion is one assertion/oracle pair copied from the already accepted
// PLAN_REPORT supplied by the caller. This package does not decide whether a
// PLAN_REPORT was accepted or current.
type PlanAssertion struct {
	Assertion       string `json:"assertion"`
	Oracle          string `json:"oracle"`
	DoneWhenIndexes []int  `json:"done_when_indexes,omitempty"`
}

// AssertionEvidence is the professional response for one accepted PLAN
// assertion. EvidenceRefs are references only: this package does not read
// bytes, resolve source identities, or establish that an assertion is true.
type AssertionEvidence struct {
	Assertion    string   `json:"assertion"`
	Oracle       string   `json:"oracle"`
	EvidenceRefs []string `json:"evidence_refs"`
}

const S6ClosingContract = assignmentrecord.ClosingContractVersion

// ClosingCondition is the Builder's professional judgment for one frozen
// Assignment DoneWhen entry. Its PLAN references bind the judgment to the
// already accepted assertion/evidence rows; they do not execute the oracle.
type ClosingCondition struct {
	DoneWhenIndex        int    `json:"done_when_index"`
	Judgment             string `json:"judgment"`
	PlanAssertionIndexes []int  `json:"plan_assertion_indexes"`
	Rationale            string `json:"rationale"`
}

// ClosingAssessment is an explicit, owner-authored assessment of the frozen
// S6 closing contract. It is not a machine PASS or independent review.
type ClosingAssessment struct {
	Disposition string             `json:"disposition"`
	Conditions  []ClosingCondition `json:"conditions"`
}

// Answer contains only the Worker-authored professional part of an S6
// Result. It has no status, PASS, changed-path, check, or integration fields.
type Answer struct {
	Summary           string              `json:"summary"`
	AssertionEvidence []AssertionEvidence `json:"assertion_evidence"`
	RisksBlockers     []string            `json:"risks_blockers"`
	ScopeDeviations   []string            `json:"scope_deviations"`
	ClosingAssessment *ClosingAssessment  `json:"closing_assessment,omitempty"`
}

// Draft is the standalone v1.2 Result scaffold. Machine fields are projected
// from one validated Assignment Record; the caller must load that Record from
// the authoritative current Runtime state. Nil professional pointers marshal
// as JSON null, requiring the Worker to supply the professional answer.
// Message/correlation/time envelope fields are owned by the surrounding
// original protocol template and are intentionally not represented here.
type Draft struct {
	SchemaVersion            string               `json:"schema_version"`
	MessageType              string               `json:"message_type"`
	RuntimeID                string               `json:"runtime_id"`
	BaselineGeneration       int                  `json:"baseline_generation"`
	AssignmentID             string               `json:"assignment_id"`
	AssignmentRevision       int                  `json:"assignment_revision"`
	AssignmentRecordContract string               `json:"assignment_record_contract"`
	AssignmentRecordSHA256   string               `json:"assignment_record_sha256"`
	AgentID                  string               `json:"agent_id"`
	AgentDefinitionRef       string               `json:"agent_definition_ref"`
	TaskID                   string               `json:"task_id"`
	TeamID                   string               `json:"team_id"`
	Summary                  *string              `json:"summary"`
	AssertionEvidence        *[]AssertionEvidence `json:"assertion_evidence"`
	RisksBlockers            *[]string            `json:"risks_blockers"`
	ScopeDeviations          *[]string            `json:"scope_deviations"`
	ClosingAssessment        *ClosingAssessment   `json:"closing_assessment"`
}

// DraftFromRecord projects the Result machine binding from a structurally
// valid canonical Record. It cannot establish that the passed Record is the
// latest Runtime revision; the Runtime-backed caller owns that lookup.
func DraftFromRecord(record assignmentrecord.Record) (Draft, error) {
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return Draft{}, fmt.Errorf("validate canonical Assignment Record: %w", err)
	}
	digest, err := record.Digest()
	if err != nil {
		return Draft{}, fmt.Errorf("digest canonical Assignment Record: %w", err)
	}
	return Draft{
		SchemaVersion:            AnswerSchemaVersion,
		MessageType:              CompletionMessage,
		RuntimeID:                record.RuntimeID,
		BaselineGeneration:       record.BaselineGeneration,
		AssignmentID:             record.AssignmentID,
		AssignmentRevision:       record.Revision,
		AssignmentRecordContract: record.Contract,
		AssignmentRecordSHA256:   digest,
		AgentID:                  record.OwnerAgentID,
		AgentDefinitionRef:       record.AgentDefinition.Ref,
		TaskID:                   record.Task.ID,
		TeamID:                   record.TeamID,
		ClosingAssessment:        nil,
	}, nil
}

// WithAnswer returns a draft populated with a complete professional answer
// after checking it against assertion/oracle pairs supplied from the accepted
// PLAN_REPORT. It does not verify the referenced evidence or change the
// machine binding.
func (draft Draft) WithAnswer(answer Answer, acceptedPlan []PlanAssertion) (Draft, error) {
	if err := ValidateAnswer(answer, acceptedPlan); err != nil {
		return Draft{}, err
	}
	summary := answer.Summary
	assertions := cloneAssertionEvidence(answer.AssertionEvidence)
	risks := cloneStrings(answer.RisksBlockers)
	deviations := cloneStrings(answer.ScopeDeviations)
	draft.Summary = &summary
	draft.AssertionEvidence = &assertions
	draft.RisksBlockers = &risks
	draft.ScopeDeviations = &deviations
	draft.ClosingAssessment = cloneClosingAssessment(answer.ClosingAssessment)
	return draft, nil
}

// ValidateClosingAssessment validates the condition-to-PLAN closure map for
// the frozen Assignment. Legacy Records without this contract continue to
// accept the original four-field Answer and digest; they cannot gain closure
// by submitting an unbound assessment.
func ValidateClosingAssessment(record assignmentrecord.Record, acceptedPlan []PlanAssertion, answer Answer) error {
	if err := validateAnswerShape(answer); err != nil {
		return err
	}
	contract := record.ClosingContract
	if contract == nil {
		if answer.ClosingAssessment != nil {
			return fmt.Errorf("closing_assessment has no frozen Assignment closing_contract")
		}
		return nil
	}
	if contract.Contract != S6ClosingContract {
		return fmt.Errorf("unsupported Assignment closing_contract %q", contract.Contract)
	}
	assessment := answer.ClosingAssessment
	if assessment == nil {
		return fmt.Errorf("closing_assessment is required by the frozen Assignment closing_contract")
	}
	if assessment.Disposition != "complete" && assessment.Disposition != "incomplete" && assessment.Disposition != "blocked" {
		return fmt.Errorf("closing_assessment disposition must be complete, incomplete, or blocked")
	}
	if len(record.DoneWhen) == 0 || len(assessment.Conditions) != len(record.DoneWhen) {
		return fmt.Errorf("closing_assessment must judge every frozen DoneWhen exactly once")
	}
	if err := ValidatePlanClosingContract(record, acceptedPlan); err != nil {
		return err
	}
	seenConditions := make(map[int]bool, len(assessment.Conditions))
	allSatisfied := true
	anyBlocked := false
	for i, condition := range assessment.Conditions {
		if condition.DoneWhenIndex < 0 || condition.DoneWhenIndex >= len(record.DoneWhen) || seenConditions[condition.DoneWhenIndex] {
			return fmt.Errorf("closing_assessment condition %d has a duplicate or invalid done_when_index", i)
		}
		seenConditions[condition.DoneWhenIndex] = true
		if condition.Judgment != "satisfied" && condition.Judgment != "incomplete" && condition.Judgment != "blocked" {
			return fmt.Errorf("closing_assessment condition %d judgment must be satisfied, incomplete, or blocked", i)
		}
		if condition.Judgment != "satisfied" {
			allSatisfied = false
		}
		if condition.Judgment == "blocked" {
			anyBlocked = true
		}
		if strings.TrimSpace(condition.Rationale) == "" {
			return fmt.Errorf("closing_assessment condition %d requires a rationale", i)
		}
		if len(condition.PlanAssertionIndexes) == 0 {
			return fmt.Errorf("closing_assessment condition %d requires PLAN assertion references", i)
		}
		seenAssertions := make(map[int]bool, len(condition.PlanAssertionIndexes))
		for _, assertionIndex := range condition.PlanAssertionIndexes {
			if assertionIndex < 0 || assertionIndex >= len(acceptedPlan) || seenAssertions[assertionIndex] {
				return fmt.Errorf("closing_assessment condition %d has an invalid or duplicate PLAN assertion index", i)
			}
			seenAssertions[assertionIndex] = true
			if !containsIndex(acceptedPlan[assertionIndex].DoneWhenIndexes, condition.DoneWhenIndex) {
				return fmt.Errorf("closing_assessment condition %d cites a PLAN assertion that does not map its DoneWhen", i)
			}
			if !hasAssertionEvidence(answer.AssertionEvidence, acceptedPlan[assertionIndex]) {
				return fmt.Errorf("closing_assessment condition %d cites a PLAN assertion without matching evidence", i)
			}
		}
	}
	if assessment.Disposition == "complete" && !allSatisfied {
		return fmt.Errorf("closing_assessment disposition complete requires every DoneWhen judgment to be satisfied")
	}
	if assessment.Disposition == "incomplete" && allSatisfied {
		return fmt.Errorf("closing_assessment disposition incomplete requires an incomplete or blocked DoneWhen")
	}
	if assessment.Disposition == "blocked" && !anyBlocked {
		return fmt.Errorf("closing_assessment disposition blocked requires a blocked DoneWhen")
	}
	return nil
}

// ValidatePlanClosingContract checks stable coverage references from the
// accepted PLAN to the Assignment's frozen DoneWhen list. It does not assess
// the truth of any assertion or oracle.
func ValidatePlanClosingContract(record assignmentrecord.Record, acceptedPlan []PlanAssertion) error {
	if record.ClosingContract == nil {
		return nil
	}
	if record.ClosingContract.Contract != S6ClosingContract {
		return fmt.Errorf("unsupported Assignment closing_contract %q", record.ClosingContract.Contract)
	}
	if len(record.DoneWhen) == 0 || len(acceptedPlan) == 0 {
		return fmt.Errorf("accepted PLAN_REPORT must cover the frozen closing contract")
	}
	covered := make([]bool, len(record.DoneWhen))
	for i, assertion := range acceptedPlan {
		if len(assertion.DoneWhenIndexes) == 0 {
			return fmt.Errorf("accepted PLAN_REPORT assertion %d does not map a frozen DoneWhen", i)
		}
		seen := make(map[int]bool, len(assertion.DoneWhenIndexes))
		for _, index := range assertion.DoneWhenIndexes {
			if index < 0 || index >= len(record.DoneWhen) || seen[index] {
				return fmt.Errorf("accepted PLAN_REPORT assertion %d has an invalid done_when_indexes entry", i)
			}
			seen[index] = true
			covered[index] = true
		}
	}
	for index, ok := range covered {
		if !ok {
			return fmt.Errorf("accepted PLAN_REPORT does not cover frozen DoneWhen %d", index)
		}
	}
	return nil
}

// ClosingAssessmentComplete reports the authored condition only. Callers must
// first validate the assessment against the current Record, PLAN and sources.
func ClosingAssessmentComplete(answer Answer) bool {
	if answer.ClosingAssessment == nil || answer.ClosingAssessment.Disposition != "complete" || len(answer.ClosingAssessment.Conditions) == 0 {
		return false
	}
	for _, condition := range answer.ClosingAssessment.Conditions {
		if condition.Judgment != "satisfied" {
			return false
		}
	}
	return true
}

// ValidateAnswer requires a complete professional answer and exact
// multiplicity coverage of the accepted PLAN assertion/oracle pairs; answer
// order is immaterial. The caller must pass assertions from the already
// accepted PLAN_REPORT; this function does not load or accept a PLAN_REPORT
// itself.
func ValidateAnswer(answer Answer, acceptedPlan []PlanAssertion) error {
	if err := validateAnswerShape(answer); err != nil {
		return err
	}
	if len(acceptedPlan) == 0 {
		return fmt.Errorf("accepted PLAN_REPORT must contain at least one assertion check")
	}
	if len(answer.AssertionEvidence) != len(acceptedPlan) {
		return fmt.Errorf("assertion_evidence has %d entries; accepted PLAN_REPORT has %d assertion checks", len(answer.AssertionEvidence), len(acceptedPlan))
	}
	type assertionPair struct {
		assertion string
		oracle    string
	}
	remaining := make(map[assertionPair]int, len(acceptedPlan))
	for i, planned := range acceptedPlan {
		if strings.TrimSpace(planned.Assertion) == "" || strings.TrimSpace(planned.Oracle) == "" {
			return fmt.Errorf("accepted PLAN_REPORT assertion check %d is incomplete", i)
		}
		remaining[assertionPair{assertion: planned.Assertion, oracle: planned.Oracle}]++
	}
	for i, provided := range answer.AssertionEvidence {
		pair := assertionPair{assertion: provided.Assertion, oracle: provided.Oracle}
		if remaining[pair] == 0 {
			return fmt.Errorf("assertion_evidence entry %d does not match an unreported accepted PLAN_REPORT assertion and oracle", i)
		}
		remaining[pair]--
	}
	return nil
}

// AnswerDigest returns the SHA-256 of the deterministic JSON encoding of the
// four professional fields only. It excludes the machine tuple and transport
// envelope. The original CompleteTask Writer must validate the accepted PLAN
// binding before accepting the answer and persist Record/Result revisions.
func AnswerDigest(answer Answer) (string, error) {
	if err := validateAnswerShape(answer); err != nil {
		return "", err
	}
	data, err := json.Marshal(answer)
	if err != nil {
		return "", fmt.Errorf("marshal professional Answer: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validateAnswerShape(answer Answer) error {
	if strings.TrimSpace(answer.Summary) == "" {
		return fmt.Errorf("summary must be non-empty")
	}
	if answer.AssertionEvidence == nil {
		return fmt.Errorf("assertion_evidence must be a JSON array")
	}
	if answer.RisksBlockers == nil {
		return fmt.Errorf("risks_blockers must be a JSON array")
	}
	if answer.ScopeDeviations == nil {
		return fmt.Errorf("scope_deviations must be a JSON array")
	}
	for i, item := range answer.AssertionEvidence {
		if strings.TrimSpace(item.Assertion) == "" || strings.TrimSpace(item.Oracle) == "" {
			return fmt.Errorf("assertion_evidence entry %d requires assertion and oracle", i)
		}
		if item.EvidenceRefs == nil || len(item.EvidenceRefs) == 0 {
			return fmt.Errorf("assertion_evidence entry %d requires one or more evidence_refs", i)
		}
		seen := make(map[string]struct{}, len(item.EvidenceRefs))
		for j, ref := range item.EvidenceRefs {
			if strings.TrimSpace(ref) == "" {
				return fmt.Errorf("assertion_evidence entry %d evidence_refs[%d] must be non-empty", i, j)
			}
			if _, ok := seen[ref]; ok {
				return fmt.Errorf("assertion_evidence entry %d contains duplicate evidence ref %q", i, ref)
			}
			seen[ref] = struct{}{}
		}
	}
	if err := validateNonEmptyEntries("risks_blockers", answer.RisksBlockers); err != nil {
		return err
	}
	if err := validateNonEmptyEntries("scope_deviations", answer.ScopeDeviations); err != nil {
		return err
	}
	if answer.ClosingAssessment != nil {
		if answer.ClosingAssessment.Conditions == nil || len(answer.ClosingAssessment.Conditions) == 0 {
			return fmt.Errorf("closing_assessment conditions must be a non-empty JSON array")
		}
		if answer.ClosingAssessment.Disposition != "complete" && answer.ClosingAssessment.Disposition != "incomplete" && answer.ClosingAssessment.Disposition != "blocked" {
			return fmt.Errorf("closing_assessment disposition must be complete, incomplete, or blocked")
		}
		for i, condition := range answer.ClosingAssessment.Conditions {
			if len(condition.PlanAssertionIndexes) == 0 || strings.TrimSpace(condition.Rationale) == "" {
				return fmt.Errorf("closing_assessment condition %d requires PLAN references and a rationale", i)
			}
			if condition.DoneWhenIndex < 0 {
				return fmt.Errorf("closing_assessment condition %d has a negative DoneWhen index", i)
			}
			if condition.Judgment != "satisfied" && condition.Judgment != "incomplete" && condition.Judgment != "blocked" {
				return fmt.Errorf("closing_assessment condition %d has an unsupported judgment", i)
			}
			seen := make(map[int]bool, len(condition.PlanAssertionIndexes))
			for _, index := range condition.PlanAssertionIndexes {
				if index < 0 || seen[index] {
					return fmt.Errorf("closing_assessment condition %d has a negative or duplicate PLAN assertion index", i)
				}
				seen[index] = true
			}
		}
	}
	return nil
}

func containsIndex(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasAssertionEvidence(evidence []AssertionEvidence, assertion PlanAssertion) bool {
	for _, item := range evidence {
		if item.Assertion == assertion.Assertion && item.Oracle == assertion.Oracle && len(item.EvidenceRefs) > 0 {
			return true
		}
	}
	return false
}

func cloneClosingAssessment(value *ClosingAssessment) *ClosingAssessment {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Conditions = append([]ClosingCondition{}, value.Conditions...)
	for i := range cloned.Conditions {
		cloned.Conditions[i].PlanAssertionIndexes = append([]int{}, value.Conditions[i].PlanAssertionIndexes...)
	}
	return &cloned
}

func validateNonEmptyEntries(name string, values []string) error {
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s[%d] must be non-empty", name, i)
		}
	}
	return nil
}

func cloneAssertionEvidence(values []AssertionEvidence) []AssertionEvidence {
	cloned := make([]AssertionEvidence, len(values))
	for i, value := range values {
		cloned[i] = value
		cloned[i].EvidenceRefs = cloneStrings(value.EvidenceRefs)
	}
	return cloned
}

func cloneStrings(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
