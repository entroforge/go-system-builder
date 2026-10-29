// Package builderbatch contains the shared legacy S6 batch projection used by
// both the quality gate and the TR-006 Writer validation. It owns no Runtime
// mutation or transition authority.
package builderbatch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type FileView interface {
	ReadFile(string) ([]byte, error)
}

type DirectoryReader interface {
	ReadDir(string) ([]os.DirEntry, error)
}

type Result struct {
	Ready       bool
	Missing     []string
	EvidenceIDs []string
	Conflicts   []string
}

// TaskProgress is the shared legacy Builder projection consumed by the batch
// gate and the quality-gate board. Canonical Assignment progress remains in
// buildsurface and is not represented here.
type TaskProgress struct {
	State            workstate.Status
	Integrated       bool
	DeliveryRecorded bool
	Reason           string
	EvidenceID       string
	Tracked          bool
}

type document struct {
	ID         string
	Kind       string
	Path       string
	Version    string
	SHA256     string
	Generation int
}

type subject struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type check struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Result  string `json:"result"`
}

type completion struct {
	SchemaVersion          string    `json:"schema_version"`
	EvidenceID             string    `json:"evidence_id"`
	Kind                   string    `json:"kind"`
	RuntimeID              string    `json:"runtime_id"`
	BaselineGeneration     int       `json:"baseline_generation"`
	ProducerAgentID        string    `json:"producer_agent_id"`
	ProducerResponsibility string    `json:"producer_responsibility"`
	TaskID                 string    `json:"task_id"`
	SubjectRefs            []subject `json:"subject_refs"`
	Conclusion             string    `json:"conclusion"`
	CreatedAt              string    `json:"created_at"`
	InvalidatedBy          string    `json:"invalidated_by"`
	Checks                 []check   `json:"checks,omitempty"`
	ScopeDeviations        []string  `json:"scope_deviations,omitempty"`
}

type Checkpoint struct {
	AssignmentID string `json:"assignment_id"`
	TaskID       string `json:"task_id"`
	Generation   int    `json:"baseline_generation"`
	State        string `json:"state"`
	Updated      string `json:"verified_at"`
	ReportPath   string `json:"completion_report_path"`
	ReportSHA    string `json:"completion_report_sha256"`
	TargetBranch string `json:"target_branch"`
	MergeCommit  string `json:"merge_commit"`
}

// HasDispatchPlan reports whether the current generation has a registered
// dispatch plan.
func HasDispatchPlan(state map[string]any) bool {
	generation := intField(object(state["baseline"])["generation"])
	for _, raw := range rows(state["documents"]) {
		if raw["kind"] == "dispatch_plan" && intField(raw["generation"]) == generation {
			return true
		}
	}
	return false
}

// EvaluateLegacy applies the existing noncanonical S6 batch gate: one
// qualified completion for every current-generation TASK, passing checks, no
// scope deviations, and a durable verified integration checkpoint for every
// TASK in the exact registered batch. Dispatch-plan batches additionally bind
// the completion and checkpoint to the original registered Builder assignment.
func EvaluateLegacy(root string, state map[string]any, files FileView) Result {
	result := Result{Missing: []string{}, EvidenceIDs: []string{}, Conflicts: []string{}}
	generation := intField(object(state["baseline"])["generation"])
	runtimeID, _ := state["runtime_id"].(string)
	batch := make([]document, 0)
	verified := make([]document, 0)
	for _, raw := range rows(state["documents"]) {
		doc := document{
			ID: rawString(raw["id"]), Kind: rawString(raw["kind"]), Path: rawString(raw["path"]),
			Version: rawString(raw["version"]), SHA256: rawString(raw["sha256"]),
			Generation: intField(raw["generation"]),
		}
		if doc.Generation != generation {
			continue
		}
		if doc.Kind == "task" && doc.ID != "" {
			batch = append(batch, doc)
		}
		if files == nil || doc.Path == "" || doc.SHA256 == "" {
			continue
		}
		data, err := files.ReadFile(doc.Path)
		if err == nil && digest(data) == doc.SHA256 {
			verified = append(verified, doc)
		}
	}
	for _, task := range batch {
		if explicitCanonicalOwner(state, task.ID, files) {
			result.Missing = append(result.Missing, "canonical_builder_result:"+task.ID)
		}
	}
	if len(result.Missing) > 0 {
		result.Missing = sortedUnique(result.Missing)
		return result
	}

	evidenceIDs, conflicts, mismatched := qualifiedCompletions(state, files, runtimeID, generation, verified)
	if len(conflicts) > 0 {
		result.Conflicts = sortedUnique(conflicts)
		return result
	}
	if len(evidenceIDs) == 0 && len(mismatched) > 0 {
		result.Conflicts = sortedUnique(mismatched)
		return result
	}
	result.EvidenceIDs = append(result.EvidenceIDs, evidenceIDs...)
	completions := map[string]completion{}
	for _, id := range evidenceIDs {
		if envelope, ok := readCompletion(state, files, id); ok && acceptsCompletion(envelope.Kind) && envelope.TaskID != "" {
			completions[envelope.TaskID] = envelope
		}
	}
	if len(batch) == 0 {
		if len(evidenceIDs) == 0 {
			result.Missing = append(result.Missing, "evidence:completion_report")
		}
		result.Missing = append(result.Missing, "batch:execution_batch_empty")
		result.Missing = sortedUnique(result.Missing)
		return result
	}

	if HasDispatchPlan(state) {
		progressByTask := ProjectLegacyTasks(root, state, files)
		for _, task := range batch {
			_, found := completions[task.ID]
			if !found {
				result.Missing = append(result.Missing, "evidence:completion_report:"+task.ID)
			}
			progress := progressByTask[task.ID]
			if !progress.Integrated || progress.State != workstate.Done {
				result.Missing = append(result.Missing, "integration_checkpoint:"+task.ID+":"+progress.Reason)
			}
		}
		result.EvidenceIDs = sortedUnique(result.EvidenceIDs)
		result.Missing = sortedUnique(result.Missing)
		result.Ready = len(result.Missing) == 0 && len(result.Conflicts) == 0
		return result
	}

	integrated := verifiedIntegrationTaskIDs(root, state, files)
	for _, task := range batch {
		envelope, ok := completions[task.ID]
		if !ok {
			result.Missing = append(result.Missing, "evidence:completion_report:"+task.ID)
		} else {
			for _, item := range envelope.Checks {
				if item.Result != "pass" {
					label := item.Name
					if label == "" {
						label = item.Command
					}
					result.Missing = append(result.Missing, "checks:"+task.ID+":"+label+"="+item.Result)
				}
			}
			for _, scope := range envelope.ScopeDeviations {
				result.Missing = append(result.Missing, "scope_deviations:"+task.ID+":"+scope)
			}
		}
		if !integrated[task.ID] {
			result.Missing = append(result.Missing, "integration_checkpoint:"+task.ID)
		}
	}
	result.EvidenceIDs = sortedUnique(result.EvidenceIDs)
	result.Missing = sortedUnique(result.Missing)
	result.Ready = len(result.Missing) == 0 && len(result.Conflicts) == 0
	return result
}

// ProjectLegacyTasks computes the original noncanonical task/assignment
// progress once for callers that need a board projection. The gate calls the
// same projection, so a displayed legacy completion cannot disagree with
// TR-006.
func ProjectLegacyTasks(root string, state map[string]any, files FileView) map[string]TaskProgress {
	result := map[string]TaskProgress{}
	entities := object(state["entities"])
	generation := intField(object(state["baseline"])["generation"])
	runtimeID := rawString(state["runtime_id"])
	stage := rawString(object(state["lifecycle"])["state"])
	for _, task := range rows(entities["tasks"]) {
		taskID := rawString(task["id"])
		if taskID == "" {
			continue
		}
		progress := plannedLegacyProgress(root, state, files, stage, task, generation, runtimeID)
		if progress.Tracked {
			result[taskID] = progress
		}
	}
	return result
}

func plannedLegacyProgress(root string, state map[string]any, files FileView, stage string, task map[string]any, generation int, runtimeID string) TaskProgress {
	taskID := rawString(task["id"])
	activity := rawString(workstate.TaskActivity(task))
	if activity == "cancelled" {
		return TaskProgress{State: workstate.Cancelled, Reason: "TASK cancelled", Tracked: true}
	}
	if stage != "building" || activity == "candidate" || activity == "reviewed" || activity == "locked" {
		return TaskProgress{}
	}
	owners, ownerState := legacyBuilderOwners(state, task, files)
	if ownerState == "none" {
		return TaskProgress{}
	}
	if ownerState != "builder" {
		return TaskProgress{State: workstate.Active, Reason: "Builder owner identity unresolved", Tracked: true}
	}
	if activity == "blocked" {
		return TaskProgress{State: workstate.Blocked, Reason: "TASK blocked", Tracked: true}
	}
	report := rawString(task["completion_report_ref"])
	if report == "" {
		return TaskProgress{State: workstate.Active, Reason: "registered Builder owner", Tracked: true}
	}
	current := TaskProgress{State: workstate.Active, DeliveryRecorded: true, Reason: "current result/integration not verified", Tracked: true}
	var envelope completion
	var evidenceSHA string
	valid := false
	for _, index := range rows(state["evidence"]) {
		if rawString(index["path"]) != report || index["kind"] != "completion_report" || index["status"] != "valid" || index["invalidated_by"] != nil || intField(index["baseline_generation"]) != generation {
			continue
		}
		data, err := files.ReadFile(report)
		if err != nil || digest(data) != rawString(index["sha256"]) || json.Unmarshal(data, &envelope) != nil {
			continue
		}
		evidenceSHA = digest(data)
		valid = envelope.TaskID == taskID && envelope.RuntimeID == runtimeID && envelope.BaselineGeneration == generation && envelope.EvidenceID == rawString(index["id"]) && envelope.InvalidatedBy == "" && envelope.Conclusion == "completed" && len(envelope.Checks) > 0 && passingChecks(envelope.Checks) && len(envelope.ScopeDeviations) == 0
	}
	for _, doc := range currentDocuments(state, generation) {
		if doc.Kind == "task" && doc.ID == taskID {
			valid = valid && exactSubject(envelope.SubjectRefs, doc)
		}
	}
	if !valid || !containsString(owners, envelope.ProducerAgentID) {
		return current
	}
	current.EvidenceID = envelope.EvidenceID
	current.Reason = "current result/integration not verified"
	agent, found := uniqueEntity(state, "agents", envelope.ProducerAgentID)
	if !found {
		return current
	}
	agentState := rawString(workstate.AgentActivity(agent))
	if agentState != "reported" && agentState != "done" && agentState != "stopped" {
		return current
	}
	_, assignmentID, ok := strings.Cut(rawString(agent["prompt_ref"]), "#")
	if !ok || assignmentID == "" || path.Base(assignmentID) != assignmentID {
		return current
	}
	checkpointPath := path.Join(".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation), "worktree", assignmentID, "checkpoint.json")
	data, err := files.ReadFile(checkpointPath)
	var checkpointRow Checkpoint
	if err != nil || json.Unmarshal(data, &checkpointRow) != nil || checkpointRow.TaskID != taskID || checkpointRow.AssignmentID != assignmentID || checkpointRow.Generation != generation || checkpointRow.ReportPath != report || checkpointRow.ReportSHA == "" || checkpointRow.ReportSHA != evidenceSHA {
		return current
	}
	created, createdErr := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	updated, updatedErr := time.Parse(time.RFC3339Nano, checkpointRow.Updated)
	if createdErr != nil || updatedErr != nil || updated.Before(created) || !VerifiedCheckpointState(checkpointRow.State) || !CheckpointMergeValid(root, state, checkpointRow) {
		return current
	}
	return TaskProgress{State: workstate.Done, Integrated: true, DeliveryRecorded: true, Reason: "current result and assignment integration verified", EvidenceID: envelope.EvidenceID, Tracked: true}
}

func legacyBuilderOwners(state, task map[string]any, files FileView) ([]string, string) {
	taskID := rawString(task["id"])
	owners := anyStrings(task["owner_agent_ids"])
	if taskID == "" || len(owners) == 0 {
		return nil, "unresolved"
	}
	var builders []string
	reviewer := false
	for _, owner := range owners {
		agent, found := uniqueEntity(state, "agents", owner)
		if !found || !containsString(anyStrings(agent["task_ids"]), taskID) {
			return nil, "unresolved"
		}
		teamID := rawString(agent["team_id"])
		team, found := uniqueEntity(state, "teams", teamID)
		if !found || !containsString(anyStrings(team["agent_ids"]), owner) {
			return nil, "unresolved"
		}
		manifestRef := rawString(team["manifest_ref"])
		definitionRef := rawString(agent["definition_ref"])
		promptManifest, assignmentID, hasAssignment := strings.Cut(rawString(agent["prompt_ref"]), "#")
		if manifestRef == "" || definitionRef == "" || !hasAssignment || promptManifest != manifestRef || assignmentID == "" || path.Base(assignmentID) != assignmentID {
			return nil, "unresolved"
		}
		// An explicit canonical marker opts this task out of the legacy path,
		// even if the canonical Assignment Record has not been registered yet.
		if files == nil {
			return nil, "unresolved"
		}
		data, err := files.ReadFile(manifestRef)
		if err == nil {
			if enabled, markerErr := assignmentrecord.ManifestAssignmentRecordEnabled(data); markerErr != nil || enabled {
				return nil, "unresolved"
			}
		} else {
			return nil, "unresolved"
		}
		role := rawString(agent["role"])
		kind := rawString(team["kind"])
		if kind == "builder" && (role == "backend-builder" || role == "frontend-builder" || role == "test-builder") {
			if team["status"] != "planned" && team["status"] != "active" {
				return nil, "unresolved"
			}
			builders = append(builders, owner)
			continue
		}
		if knownReviewer(kind, role) {
			// Keep the original live-Reviewer classification. Historical
			// non-active Teams need their source assignment to distinguish a
			// retired review owner from unresolved Builder ownership.
			if team["status"] == "planned" || team["status"] == "active" || manifestAssignmentMatches(data, kind, assignmentID, owner, role) {
				reviewer = true
				continue
			}
		}
		return nil, "unresolved"
	}
	if len(builders) > 0 {
		return sortedUnique(builders), "builder"
	}
	if reviewer {
		return nil, "none"
	}
	return nil, "unresolved"
}

// manifestAssignmentMatches uses the original registered manifest to classify
// a historical non-Builder owner. Retired review Teams remain recognizable
// only when their exact prompt assignment still names this Agent and role.
func manifestAssignmentMatches(data []byte, workgroupKind, assignmentID, agentID, role string) bool {
	var source struct {
		WorkgroupKind string `json:"workgroup_kind"`
		Assignments   []struct {
			AssignmentID string `json:"assignment_id"`
			AgentID      string `json:"agent_id"`
			RoleFamily   string `json:"role_family"`
		} `json:"assignments"`
	}
	if json.Unmarshal(data, &source) != nil || source.WorkgroupKind != workgroupKind || assignmentID == "" || agentID == "" || role == "" {
		return false
	}
	matches := 0
	for _, assignment := range source.Assignments {
		if assignment.AssignmentID != assignmentID {
			continue
		}
		matches++
		if assignment.AgentID != agentID || assignment.RoleFamily != role {
			return false
		}
	}
	return matches == 1
}

func qualifiedCompletions(state map[string]any, files FileView, runtimeID string, generation int, documents []document) ([]string, []string, []string) {
	var valid, conflicts, mismatched []string
	for _, index := range rows(state["evidence"]) {
		if !acceptsCompletion(rawString(index["kind"])) || rawString(index["status"]) != "valid" || intField(index["baseline_generation"]) != generation || index["invalidated_by"] != nil {
			continue
		}
		if files == nil || rawString(index["path"]) == "" {
			conflicts = append(conflicts, "evidence:"+rawString(index["id"])+":unreadable")
			continue
		}
		data, err := files.ReadFile(rawString(index["path"]))
		if err != nil {
			conflicts = append(conflicts, "evidence:"+rawString(index["id"])+":unreadable")
			continue
		}
		if digest(data) != rawString(index["sha256"]) {
			continue
		}
		var envelope completion
		if err := json.Unmarshal(data, &envelope); err != nil {
			conflicts = append(conflicts, "evidence:"+rawString(index["id"])+":schema")
			continue
		}
		producer := envelope.ProducerAgentID
		if envelope.SchemaVersion == "" || envelope.EvidenceID != rawString(index["id"]) || !acceptsCompletion(envelope.Kind) || envelope.RuntimeID != runtimeID || envelope.BaselineGeneration != generation || producer == "" || envelope.ProducerResponsibility != rawString(index["responsibility_id"]) || !containsString(anyStrings(index["produced_by"]), producer) {
			conflicts = append(conflicts, "evidence:"+rawString(index["id"])+":schema")
			continue
		}
		if envelope.InvalidatedBy != "" {
			continue
		}
		if envelope.Conclusion != "completed" {
			mismatched = append(mismatched, "evidence:"+rawString(index["id"])+":conclusion_mismatch:"+envelope.Conclusion)
			continue
		}
		if envelope.ProducerResponsibility != "BUILD-WORK-PACKAGE" && envelope.ProducerResponsibility != "Builder" {
			continue
		}
		if !subjectsMatch(envelope.SubjectRefs, documents) {
			continue
		}
		valid = append(valid, envelope.EvidenceID)
	}
	return sortedUnique(valid), sortedUnique(conflicts), sortedUnique(mismatched)
}

func readCompletion(state map[string]any, files FileView, id string) (completion, bool) {
	for _, index := range rows(state["evidence"]) {
		if rawString(index["id"]) != id || !acceptsCompletion(rawString(index["kind"])) || files == nil {
			continue
		}
		data, err := files.ReadFile(rawString(index["path"]))
		if err != nil || digest(data) != rawString(index["sha256"]) {
			continue
		}
		var envelope completion
		if json.Unmarshal(data, &envelope) == nil {
			return envelope, true
		}
	}
	return completion{}, false
}

func verifiedIntegrationTaskIDs(root string, state map[string]any, files FileView) map[string]bool {
	integrated := map[string]bool{}
	lister, ok := files.(DirectoryReader)
	if !ok || files == nil {
		return integrated
	}
	runtimeID, _ := state["runtime_id"].(string)
	generation := intField(object(state["baseline"])["generation"])
	dir := path.Join(".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation), "worktree")
	entries, err := lister.ReadDir(dir)
	if err != nil {
		return integrated
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := files.ReadFile(path.Join(dir, entry.Name(), "checkpoint.json"))
		if err != nil {
			continue
		}
		var row Checkpoint
		if json.Unmarshal(data, &row) != nil || row.TaskID == "" || !VerifiedCheckpointState(row.State) || !CheckpointMergeValid(root, state, row) {
			continue
		}
		integrated[row.TaskID] = true
	}
	return integrated
}

// CheckpointMergeValid is the shared legacy checkpoint source check used by
// the gate, TR-006 and quality-gate responsibility projections.
func CheckpointMergeValid(root string, state map[string]any, row Checkpoint) bool {
	reportBound := strings.TrimSpace(row.ReportPath) != "" || strings.TrimSpace(row.ReportSHA) != ""
	mergeCommit := strings.TrimSpace(row.MergeCommit)
	targetBranch := strings.TrimSpace(row.TargetBranch)
	if mergeCommit == "" {
		return !reportBound
	}
	if targetBranch == "" || (reportBound && (strings.TrimSpace(row.ReportPath) == "" || strings.TrimSpace(row.ReportSHA) == "")) {
		return false
	}
	bound, _ := object(state["bound_req"])["workspace"].(map[string]any)
	if branch := strings.TrimSpace(rawString(bound["dev_branch"])); branch == "" || targetBranch != branch {
		return false
	}
	if strings.TrimSpace(root) == "" {
		return false
	}
	return exec.Command("git", "-C", root, "merge-base", "--is-ancestor", mergeCommit, targetBranch).Run() == nil
}

func VerifiedCheckpointState(value string) bool {
	switch value {
	case "verified", "acknowledged", "cleanup_pending", "complete":
		return true
	default:
		return false
	}
}

func subjectsMatch(subjects []subject, documents []document) bool {
	for _, item := range subjects {
		matched := false
		for _, doc := range documents {
			if item.Path == doc.Path && item.Version == doc.Version && item.SHA256 == doc.SHA256 {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func acceptsCompletion(kind string) bool {
	return kind == "completion_report" || kind == "agent_completion"
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func rows(value any) []map[string]any {
	items, _ := value.([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if row, ok := item.(map[string]any); ok && row != nil {
			result = append(result, row)
		}
	}
	return result
}

func object(value any) map[string]any {
	row, _ := value.(map[string]any)
	return row
}

func rawString(value any) string {
	text, _ := value.(string)
	return text
}

func intField(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		n, _ := number.Int64()
		return int(n)
	default:
		return 0
	}
}

func anyStrings(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func currentDocuments(state map[string]any, generation int) []document {
	var result []document
	for _, raw := range rows(state["documents"]) {
		if intField(raw["generation"]) != generation {
			continue
		}
		result = append(result, document{
			ID: rawString(raw["id"]), Kind: rawString(raw["kind"]), Path: rawString(raw["path"]),
			Version: rawString(raw["version"]), SHA256: rawString(raw["sha256"]), Generation: generation,
		})
	}
	return result
}

func exactSubject(subjects []subject, doc document) bool {
	return len(subjects) == 1 && subjects[0].Path == doc.Path && subjects[0].Version == doc.Version && subjects[0].SHA256 == doc.SHA256
}

func passingChecks(checks []check) bool {
	if len(checks) == 0 {
		return false
	}
	for _, item := range checks {
		if item.Result != "pass" {
			return false
		}
	}
	return true
}

func uniqueEntity(state map[string]any, collection, id string) (map[string]any, bool) {
	var found map[string]any
	for _, row := range rows(object(state["entities"])[collection]) {
		if rawString(row["id"]) != id {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = row
	}
	return found, found != nil
}

func knownReviewer(kind, role string) bool {
	switch kind {
	case "document_verifier":
		return role == "document-verifier"
	case "delivery_verifier":
		return role == "delivery-verifier"
	case "qa":
		return role == "qa"
	case "e2e_browser":
		return role == "e2e-tester"
	case "investigator":
		return role == "investigator"
	default:
		return false
	}
}

// explicitCanonicalOwner mirrors the gate's canonical-mode selection for a
// Runtime owner whose Team manifest opts into Assignment Records. The caller
// is already on the legacy transition path, so any such marker must keep a
// legacy completion from satisfying TR-006.
func explicitCanonicalOwner(state map[string]any, taskID string, files FileView) bool {
	task, found := uniqueEntity(state, "tasks", taskID)
	if !found {
		return false
	}
	for _, owner := range anyStrings(task["owner_agent_ids"]) {
		agent, found, ambiguous := uniqueEntityStatus(state, "agents", owner)
		if ambiguous {
			return true
		}
		if !found {
			continue
		}
		teamID := rawString(agent["team_id"])
		if teamID == "" {
			continue
		}
		team, found, ambiguous := uniqueEntityStatus(state, "teams", teamID)
		if ambiguous {
			return true
		}
		if !found {
			continue
		}
		manifestRef := rawString(team["manifest_ref"])
		if manifestRef == "" || files == nil {
			return true
		}
		data, err := files.ReadFile(manifestRef)
		if err != nil {
			return true
		}
		enabled, err := assignmentrecord.ManifestAssignmentRecordEnabled(data)
		if err != nil || enabled {
			return true
		}
	}
	return false
}

func uniqueEntityStatus(state map[string]any, collection, id string) (map[string]any, bool, bool) {
	var found map[string]any
	for _, row := range rows(object(state["entities"])[collection]) {
		if rawString(row["id"]) != id {
			continue
		}
		if found != nil {
			return nil, false, true
		}
		found = row
	}
	return found, found != nil, false
}
