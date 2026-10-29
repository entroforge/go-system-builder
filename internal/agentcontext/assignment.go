package agentcontext

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

// Assignment contains the originally registered facts, not a new authority.
type Assignment struct {
	AssignmentID           string                            `json:"assignment_id"`
	Owner                  string                            `json:"owner"`
	TaskID                 string                            `json:"task_id"`
	TeamID                 string                            `json:"team_id"`
	Role                   string                            `json:"role"`
	AgentDefinitionRef     string                            `json:"agent_definition_ref"`
	ManifestRef            string                            `json:"manifest_ref"`
	DispatchMode           string                            `json:"dispatch_mode"`
	IntegrationCheckMode   string                            `json:"integration_check_mode,omitempty"`
	ResponsibilityIDs      []string                          `json:"responsibility_ids"`
	AuthorizationSkillRefs []string                          `json:"authorization_skill_refs"`
	ReadPaths              []string                          `json:"read_paths"`
	DeclaredOutputs        []string                          `json:"declared_outputs"`
	DependsOn              []string                          `json:"depends_on"`
	DoneWhen               []string                          `json:"done_when"`
	AssignmentRevision     int                               `json:"assignment_revision,omitempty"`
	EffectiveWritePaths    []string                          `json:"effective_write_paths,omitempty"`
	RequiredChecks         []string                          `json:"required_checks,omitempty"`
	ClosingContract        *assignmentrecord.ClosingContract `json:"closing_contract,omitempty"`
	AssignmentRecord       *assignmentrecord.RecordReference `json:"assignment_record,omitempty"`
	AssignmentRecordSHA256 string                            `json:"assignment_record_sha256,omitempty"`
}

type Reference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ApplyRecord projects the canonical Runtime Assignment Record into the
// existing context shape. The Record is the source for every machine-owned
// Assignment value; a non-empty input ID must refer to the same Assignment.
func ApplyRecord(current Assignment, record assignmentrecord.Record) (Assignment, error) {
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return Assignment{}, fmt.Errorf("validate canonical Assignment Record: %w", err)
	}
	if current.AssignmentID != "" && current.AssignmentID != record.AssignmentID {
		return Assignment{}, fmt.Errorf("context Assignment %s cannot project Record for %s", current.AssignmentID, record.AssignmentID)
	}
	digest, err := record.Digest()
	if err != nil {
		return Assignment{}, fmt.Errorf("digest canonical Assignment Record: %w", err)
	}
	reference := record.Reference()
	closingContract, err := cloneClosingContract(record.ClosingContract)
	if err != nil {
		return Assignment{}, fmt.Errorf("copy canonical closing contract: %w", err)
	}
	return Assignment{
		AssignmentID:           record.AssignmentID,
		Owner:                  record.OwnerAgentID,
		TaskID:                 record.Task.ID,
		TeamID:                 record.TeamID,
		Role:                   record.RoleFamily,
		AgentDefinitionRef:     record.AgentDefinition.Ref,
		ManifestRef:            record.Manifest.Ref,
		DispatchMode:           record.DispatchMode,
		IntegrationCheckMode:   record.IntegrationCheckMode,
		ResponsibilityIDs:      []string{record.ResponsibilityID},
		AuthorizationSkillRefs: cloneStrings(record.SkillRefs),
		ReadPaths:              cloneStrings(record.ReadPaths),
		DeclaredOutputs:        cloneStrings(record.ScopeBasis.ManifestOutputPaths),
		DependsOn:              cloneStrings(record.DependsOn),
		DoneWhen:               cloneStrings(record.DoneWhen),
		AssignmentRevision:     record.Revision,
		EffectiveWritePaths:    cloneStrings(record.EffectiveWritePaths),
		RequiredChecks:         cloneStrings(record.RequiredChecks),
		ClosingContract:        closingContract,
		AssignmentRecord:       &reference,
		AssignmentRecordSHA256: digest,
	}, nil
}

// PlanTemplate answers only machine fields. The professional fields make this
// deliberately invalid as a submitted PLAN_REPORT until its owner answers.
const resultOracleTiming = "Choose oracles verifiable before submitting the Result. The receiver's acceptance receipt proves Result registration afterwards. Checks owned by Main are verified by Main during integration."

func dependencyQuestion(dependsOn []string) string {
	encoded, _ := json.Marshal(append([]string{}, dependsOn...))
	return fmt.Sprintf("Read-only registered workgroup-local Assignment dependencies: %s. The original receiver carries them into the canonical Plan; the private professional answer does not need this field. TASK prerequisites are handled by dispatch; input files, environment conditions and waiting for Main are not Assignment identifiers.", encoded)
}

func PlanTemplate(d Assignment, runtimeID, occurredAt string, planned []string) (map[string]any, map[string]string) {
	if d.DispatchMode != "plan_checkpoint" {
		return nil, nil
	}
	if hasRecordProjection(d) {
		if !validRecordProjection(d, runtimeID) {
			return nil, nil
		}
		plan := map[string]any{
			"schema_version": "1.0.0", "message_type": "plan_report",
			"message_id": "msg-plan-" + d.AssignmentID, "correlation_id": "corr-" + d.AssignmentID,
			"runtime_id": runtimeID, "agent_id": d.Owner, "agent_definition_ref": d.AgentDefinitionRef,
			"task_id": d.TaskID, "bug_id": nil, "team_id": d.TeamID, "occurred_at": occurredAt,
			"assignment_id": d.AssignmentID, "assignment_revision": d.AssignmentRecord.Revision,
			"assignment_record_contract": assignmentrecord.AssignmentRecordContract,
			"assignment_record_sha256":   d.AssignmentRecordSHA256,
			"objective":                  nil, "planned_paths": []string{}, "assertion_checks": []any{},
			"dependencies": append([]string{}, d.DependsOn...), "risks_blockers": nil,
		}
		questions := map[string]string{
			"/objective":        "State the concrete work and user value after reading the actual TASK and responsibility (at least 8 characters).",
			"/planned_paths":    "List the paths this plan will change, within the canonical effective write paths and in your execution view.",
			"/assertion_checks": "Give one or more actual assertions and observable oracles; use the registered done_when and required_checks.",
			"/dependencies":     dependencyQuestion(d.DependsOn),
			"/risks_blockers":   "List actual risks and blockers; use [] only after judging that none remain.",
		}
		if d.ClosingContract != nil {
			questions["/assertion_checks"] = closingPlanQuestion(d.DoneWhen, d.RequiredChecks)
		}
		questions["/assertion_checks"] += " " + resultOracleTiming
		return plan, questions
	}
	plan := map[string]any{"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-plan-" + d.AssignmentID, "correlation_id": "corr-" + d.AssignmentID, "runtime_id": runtimeID, "agent_id": d.Owner, "agent_definition_ref": d.AgentDefinitionRef, "task_id": d.TaskID, "bug_id": nil, "team_id": d.TeamID, "occurred_at": occurredAt, "assignment_id": d.AssignmentID, "assignment_revision": 1, "objective": nil, "planned_paths": append([]string{}, planned...), "steps": []any{}, "assertion_checks": []any{}, "dependencies": append([]string{}, d.DependsOn...), "risks_blockers": nil}
	questions := map[string]string{"/objective": "State the concrete work and user value after reading the actual TASK and responsibility (at least 8 characters).", "/steps": "Give one or more concrete steps with description (at least 4 characters) and optional target path.", "/assertion_checks": "Give one or more actual assertions and observable oracles (at least 4 characters each); use the registered done_when and required_checks.", "/planned_paths": "Choose paths within the original assignment. An S5 issued answer path is authority-relative; the local Worker answer path is in its execution view.", "/dependencies": dependencyQuestion(d.DependsOn), "/risks_blockers": "List actual risks and blockers; use [] only after judging that none remain."}
	questions["/assertion_checks"] += " " + resultOracleTiming
	return plan, questions
}

// ResultTemplate projects only the machine tuple for an explicit canonical
// S6 Builder assignment. The professional answer remains blank until its
// owner reads the accepted PLAN_REPORT and supplies evidence and judgments.
func ResultTemplate(d Assignment, runtimeID, occurredAt string) (map[string]any, map[string]string) {
	if !hasRecordProjection(d) || !validRecordProjection(d, runtimeID) ||
		d.Owner == "" || d.AgentDefinitionRef == "" || d.TaskID == "" || d.TeamID == "" ||
		!safeResultPathSegment(d.Owner) ||
		len(d.ResponsibilityIDs) != 1 || d.ResponsibilityIDs[0] != "BUILD-WORK-PACKAGE" {
		return nil, nil
	}
	if _, err := time.Parse(time.RFC3339Nano, occurredAt); err != nil {
		return nil, nil
	}
	draft := map[string]any{
		"schema_version":             "1.0.0",
		"message_type":               "completion_report",
		"message_id":                 "msg-result-" + d.AssignmentID,
		"correlation_id":             "corr-result-" + d.AssignmentID,
		"runtime_id":                 runtimeID,
		"baseline_generation":        d.AssignmentRecord.BaselineGeneration,
		"agent_id":                   d.Owner,
		"agent_definition_ref":       d.AgentDefinitionRef,
		"task_id":                    d.TaskID,
		"bug_id":                     nil,
		"team_id":                    d.TeamID,
		"occurred_at":                occurredAt,
		"assignment_id":              d.AssignmentID,
		"assignment_revision":        d.AssignmentRecord.Revision,
		"assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"assignment_record_sha256":   d.AssignmentRecordSHA256,
		"summary":                    nil,
		"assertion_evidence":         nil,
		"risks_blockers":             nil,
		"scope_deviations":           nil,
	}
	questions := map[string]string{
		"/summary":            resultSummaryQuestion(d.RequiredChecks),
		"/assertion_evidence": "The original begin flow copies each accepted PLAN assertion and oracle into these rows. Preserve those values and cite actual evidence file paths in evidence_refs. A cited file proves only its bytes, not that the assertion passed.",
		"/risks_blockers":     "State actual remaining risks or blockers; any nonempty list prevents completion/integration. Put information-only limitations and resolved issues in summary/rationale, without hiding unfinished work. Use [] only after judging none remain.",
		"/scope_deviations":   "State actual deviations from the planned scope; a nonempty list prevents completion/integration. Main independently checks Git scope. Explain information-only context in summary, without hiding a real deviation.",
	}
	if d.ClosingContract != nil {
		draft["closing_assessment"] = nil
		questions["/closing_assessment"] = closingResultQuestion(d.DoneWhen, d.ClosingContract.WorkerChecks) + resultClosingTaskChecksNote
	}
	questions["/assertion_evidence"] += " " + resultOracleTiming
	return draft, questions
}

const legacyResultAssertionQuestion = "For every assertion_checks entry in the current accepted PLAN_REPORT, copy its assertion and oracle exactly once and cite actual evidence file paths. A cited file proves only its bytes, not that the assertion passed. " + resultOracleTiming
const legacyResultSummaryQuestion = "Summarize the actual delivery for the registered TASK after reading its current accepted PLAN_REPORT."
const legacyResultMessageIDQuestion = "Keep this identity for an exact replay. Choose a new unique message_id before submitting a changed professional answer."
const resultClosingTaskChecksNote = " Report the actual TASK required-check commands and results in summary as your self-check; an empty Worker runner contract does not waive this. Main's integration rerun remains separate."

func resultSummaryQuestion(requiredChecks []string) string {
	base := legacyResultSummaryQuestion
	if len(requiredChecks) == 0 {
		return base + " No TASK required_checks are registered; report any self-check commands you chose to run and their actual results, or explain why no check was run. Main's integration checks are separate."
	}
	commands := make([]string, len(requiredChecks))
	for i, command := range requiredChecks {
		commands[i] = "- `" + command + "`"
	}
	return base + " Run the exact TASK required_checks below yourself and report each actual result. If a command was not run, state the reason or blocker. Main's integration rerun is separate and does not replace your self-check. Commands:\n" + strings.Join(commands, "\n")
}

// ResultQuestionsMatch accepts the one previously published Result guidance
// shape while new contexts use the current auto-filled guidance. Frozen
// assignment contexts remain byte-bound; this comparison is only their
// documented reader compatibility seam.
func ResultQuestionsMatch(current, frozen map[string]string) bool {
	for key, question := range current {
		got, ok := frozen[key]
		if !ok {
			return false
		}
		if got == question ||
			key == "/summary" && got == legacyResultSummaryQuestion ||
			key == "/assertion_evidence" && got == legacyResultAssertionQuestion ||
			key == "/closing_assessment" && got == strings.TrimSuffix(question, resultClosingTaskChecksNote) {
			continue
		}
		return false
	}
	for key, question := range frozen {
		if _, currentKey := current[key]; currentKey {
			continue
		}
		if key != "/message_id" || question != legacyResultMessageIDQuestion {
			return false
		}
	}
	return true
}

func closingPlanQuestion(doneWhen, requiredChecks []string) string {
	indexed := make([]string, len(doneWhen))
	for i, condition := range doneWhen {
		indexed[i] = fmt.Sprintf("%d: %s", i, condition)
	}
	checks := "No required_checks are registered."
	if len(requiredChecks) > 0 {
		checks = "Registered required_checks: " + strings.Join(requiredChecks, "; ") + "."
	}
	return "Give one or more actual assertions and observable oracles. Use applicable registered checks below as verification oracles. " + checks + " Each assertion_checks item must include non-empty, unique done_when_indexes using the zero-based list below, and the union must cover every condition. " + strings.Join(indexed, "; ")
}

func closingResultQuestion(doneWhen []string, checks []assignmentrecord.WorkerCheck) string {
	indexed := make([]string, len(doneWhen))
	for i, condition := range doneWhen {
		indexed[i] = fmt.Sprintf("%d: %s", i, condition)
	}
	checkNote := " Registered Worker runner checks, if any, are separate from this professional assessment and must not be claimed as passed here."
	if len(checks) == 0 {
		checkNote = " The frozen contract contains no Worker runner checks; Main required checks remain separately owned."
	}
	return "Give disposition complete, incomplete, or blocked. The saved Result draft copies done_when_index and plan_assertion_indexes from the accepted PLAN mapping; preserve those indexes and fill each condition's judgment satisfied/incomplete/blocked and a non-empty rationale. Complete requires every judgment satisfied; incomplete or blocked must identify the corresponding unsatisfied judgment. " + strings.Join(indexed, "; ") + checkNote
}

func safeResultPathSegment(value string) bool {
	if value == "" || value == "." || value == ".." || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// ValidateResultDraft verifies that an editable Result file still names the
// immutable Record tuple captured in its context. It permits the professional
// fields and transport identity/time to change; it does not validate a
// completed answer or treat any answer as machine evidence.
func ValidateResultDraft(template map[string]any, data []byte) error {
	if template == nil || len(data) == 0 {
		return fmt.Errorf("canonical Result draft template and bytes are required")
	}
	var draft map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&draft); err != nil {
		return fmt.Errorf("decode editable canonical Result draft: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("editable canonical Result draft must contain exactly one JSON object")
	}
	if draft == nil {
		return fmt.Errorf("editable canonical Result draft must be a JSON object")
	}
	allowed := map[string]bool{}
	for key := range template {
		allowed[key] = true
	}
	for key := range draft {
		if !allowed[key] {
			return fmt.Errorf("editable canonical Result contains unsupported field %s", key)
		}
	}
	for key := range template {
		if _, exists := draft[key]; !exists {
			return fmt.Errorf("editable canonical Result omitted frozen template field %s", key)
		}
	}
	for _, key := range []string{"schema_version", "message_type", "runtime_id", "baseline_generation", "assignment_id", "assignment_revision", "assignment_record_contract", "assignment_record_sha256", "agent_id", "agent_definition_ref", "task_id", "bug_id", "team_id"} {
		want, wantOK := template[key]
		got, gotOK := draft[key]
		if !wantOK || !gotOK || !sameJSONValue(want, got) {
			return fmt.Errorf("editable canonical Result differs from its frozen %s binding", key)
		}
	}
	for _, key := range []string{"message_id", "correlation_id"} {
		value, ok := draft[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("editable canonical Result %s must remain a non-empty string", key)
		}
	}
	occurredAt, ok := draft["occurred_at"].(string)
	if !ok {
		return fmt.Errorf("editable canonical Result occurred_at must remain a timestamp")
	}
	if _, err := time.Parse(time.RFC3339Nano, occurredAt); err != nil {
		return fmt.Errorf("editable canonical Result occurred_at is invalid: %w", err)
	}
	for key, kind := range map[string]string{
		"summary": "string", "assertion_evidence": "array", "risks_blockers": "array", "scope_deviations": "array",
	} {
		value, exists := draft[key]
		if !exists || value == nil {
			continue
		}
		switch kind {
		case "string":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("editable canonical Result %s must be null or a string", key)
			}
		case "array":
			if _, ok := value.([]any); !ok {
				return fmt.Errorf("editable canonical Result %s must be null or an array", key)
			}
		case "object":
			if _, ok := value.(map[string]any); !ok {
				return fmt.Errorf("editable canonical Result %s must be null or an object", key)
			}
		}
	}
	if value, exists := draft["closing_assessment"]; exists && value != nil {
		if err := validateClosingAssessmentDraft(value); err != nil {
			return err
		}
	}
	return nil
}

func validateClosingAssessmentDraft(value any) error {
	assessment, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("editable canonical Result closing_assessment must be null or an object")
	}
	for key := range assessment {
		if key != "disposition" && key != "conditions" {
			return fmt.Errorf("editable canonical Result closing_assessment contains unsupported field %s", key)
		}
	}
	if disposition, exists := assessment["disposition"]; exists {
		if disposition == nil {
			return validateClosingConditionDrafts(assessment)
		}
		value, ok := disposition.(string)
		if !ok || value != "complete" && value != "incomplete" && value != "blocked" {
			return fmt.Errorf("editable canonical Result closing_assessment disposition is invalid")
		}
	}
	return validateClosingConditionDrafts(assessment)
}

func validateClosingConditionDrafts(assessment map[string]any) error {
	conditions, exists := assessment["conditions"]
	if !exists {
		return nil
	}
	rows, ok := conditions.([]any)
	if !ok {
		return fmt.Errorf("editable canonical Result closing_assessment conditions must be an array")
	}
	for i, raw := range rows {
		condition, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("editable canonical Result closing_assessment condition %d must be an object", i)
		}
		for key := range condition {
			if key != "done_when_index" && key != "judgment" && key != "plan_assertion_indexes" && key != "rationale" {
				return fmt.Errorf("editable canonical Result closing_assessment condition %d contains unsupported field %s", i, key)
			}
		}
		if value, exists := condition["done_when_index"]; exists {
			index, ok := jsonInteger(value)
			if !ok || index < 0 {
				return fmt.Errorf("editable canonical Result closing_assessment condition %d has an invalid done_when_index", i)
			}
		}
		if value, exists := condition["judgment"]; exists {
			if value != nil {
				judgment, ok := value.(string)
				if !ok || judgment != "satisfied" && judgment != "incomplete" && judgment != "blocked" {
					return fmt.Errorf("editable canonical Result closing_assessment condition %d has an invalid judgment", i)
				}
			}
		}
		if value, exists := condition["plan_assertion_indexes"]; exists {
			indices, ok := value.([]any)
			if !ok {
				return fmt.Errorf("editable canonical Result closing_assessment condition %d plan_assertion_indexes must be an array", i)
			}
			seen := map[int64]bool{}
			for _, rawIndex := range indices {
				index, ok := jsonInteger(rawIndex)
				if !ok || index < 0 || seen[index] {
					return fmt.Errorf("editable canonical Result closing_assessment condition %d has an invalid or duplicate PLAN assertion index", i)
				}
				seen[index] = true
			}
		}
		if value, exists := condition["rationale"]; exists {
			if value != nil {
				rationale, ok := value.(string)
				if !ok || strings.TrimSpace(rationale) == "" {
					return fmt.Errorf("editable canonical Result closing_assessment condition %d has an invalid rationale", i)
				}
			}
		}
	}
	return nil
}

func jsonInteger(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := number.Int64()
	return parsed, err == nil
}

func sameJSONValue(left, right any) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func hasRecordProjection(d Assignment) bool {
	return d.AssignmentRecord != nil || d.AssignmentRecordSHA256 != "" || d.AssignmentRevision != 0
}

func validRecordProjection(d Assignment, runtimeID string) bool {
	if d.AssignmentRecord == nil || d.AssignmentRecordSHA256 == "" || d.AssignmentRevision < 1 {
		return false
	}
	reference := d.AssignmentRecord
	if reference.RuntimeID != runtimeID || reference.BaselineGeneration < 1 || reference.AssignmentID != d.AssignmentID || reference.Revision != d.AssignmentRevision {
		return false
	}
	if d.ClosingContract == nil {
		if d.IntegrationCheckMode != "" {
			return false
		}
	} else if d.IntegrationCheckMode != "post_merge" || assignmentrecord.ValidateClosingContract(d.ClosingContract) != nil {
		return false
	}
	if len(d.AssignmentRecordSHA256) != 64 || d.AssignmentRecordSHA256 != strings.ToLower(d.AssignmentRecordSHA256) {
		return false
	}
	_, err := hex.DecodeString(d.AssignmentRecordSHA256)
	return err == nil
}

func cloneClosingContract(source *assignmentrecord.ClosingContract) (*assignmentrecord.ClosingContract, error) {
	if source == nil {
		return nil, nil
	}
	data, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var cloned assignmentrecord.ClosingContract
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func cloneStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

// InputReadingOrder is common to all dispatch paths; the caller supplies the
// actual physical prefix (empty at Main, .claude/inputs in a Worker).
func InputReadingOrder(inputs map[string]string, prefix string) []string {
	paths := []string{}
	for path := range inputs {
		if strings.HasPrefix(path, ".claude/skills/") || strings.HasPrefix(path, "skills/") {
			continue
		}
		paths = append(paths, path)
	}
	rank := func(path string) int {
		for i, p := range []string{"docs/dev/tasks/", "docs/dev/contracts/", "docs/requirements/", "docs/architecture/", "docs/design/", "docs/rules/"} {
			if strings.HasPrefix(path, p) {
				return i
			}
		}
		return 6
	}
	sort.Slice(paths, func(i, j int) bool {
		if rank(paths[i]) != rank(paths[j]) {
			return rank(paths[i]) < rank(paths[j])
		}
		return paths[i] < paths[j]
	})
	for i := range paths {
		paths[i] = prefix + paths[i]
	}
	return paths
}
