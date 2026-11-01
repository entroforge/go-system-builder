package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// CanonicalResultBinding shares the accepted-result DTO; integration cannot
// define a second binding authority.
type CanonicalResultBinding = assignmentresult.ResultBinding

type canonicalResultFacts struct {
	Record              assignmentrecord.Record
	Accepted            assignmentresult.AcceptedResult
	Binding             CanonicalResultBinding
	ExecutionGeneration int
}

func cloneAssignmentProjection(source hookctx.AssignmentContext) (hookctx.AssignmentContext, error) {
	data, err := json.Marshal(source)
	if err != nil {
		return hookctx.AssignmentContext{}, err
	}
	var clone hookctx.AssignmentContext
	if err := json.Unmarshal(data, &clone); err != nil {
		return hookctx.AssignmentContext{}, err
	}
	return clone, nil
}

// resolveCanonicalResult reads the original Runtime pair and current Agent
// and TASK Result pointers. A missing Record is legacy only when the original
// manifest does not opt in; explicit S6 without a Record fails closed.
func resolveCanonicalResult(root, runtimeID string, assignment hookctx.AssignmentContext) (*canonicalResultFacts, error) {
	if assignment.ManifestRef == "" && assignment.AssignmentRecord == nil && assignment.AssignmentRevision == 0 {
		return nil, nil
	}
	state, err := readCanonicalRuntimeState(root)
	if err != nil {
		return nil, err
	}
	return resolveCanonicalResultState(root, runtimeID, state, assignment, nil)
}

func resolveCanonicalResultByBinding(root, runtimeID string, expected CanonicalResultBinding) (*canonicalResultFacts, error) {
	state, err := readCanonicalRuntimeState(root)
	if err != nil {
		return nil, err
	}
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return nil, fmt.Errorf("read current Assignment registry: %w", err)
	}
	record, found, err := assignmentrecord.FindRecordRevision(records, expected.AssignmentRecord)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("checkpoint Assignment revision is no longer current")
		}
		return nil, err
	}
	if runtimeID != "" && record.RuntimeID != runtimeID {
		return nil, fmt.Errorf("checkpoint Assignment belongs to another Runtime")
	}
	current, found, err := assignmentrecord.ForOwner(state, record.OwnerAgentID)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("checkpoint Assignment owner is no longer registered")
		}
		return nil, err
	}
	if current.Reference() != expected.AssignmentRecord {
		return nil, fmt.Errorf("checkpoint Assignment reference is stale")
	}
	projection := assignmentProjection(current)
	return resolveCanonicalResultState(root, runtimeID, state, projection, &expected)
}

func readCanonicalRuntimeState(root string) (map[string]any, error) {
	if root == "" {
		return nil, fmt.Errorf("canonical Assignment resolution requires the Runtime authority root")
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return nil, fmt.Errorf("read original Runtime state+journal pair: %w", err)
	}
	return snapshot.State, nil
}

func resolveCanonicalResultState(root, runtimeID string, state map[string]any, assignment hookctx.AssignmentContext, expected *CanonicalResultBinding) (*canonicalResultFacts, error) {
	if assignment.ManifestRef == "" && assignment.AssignmentRecord == nil && assignment.AssignmentRevision == 0 {
		return nil, nil
	}
	record, found, err := assignmentrecord.ForOwner(state, assignment.OwnerAgentID)
	if err != nil {
		return nil, fmt.Errorf("resolve canonical Assignment projection: %w", err)
	}
	if !found {
		return nil, nil
	}
	if record.AssignmentID != assignment.AssignmentID || record.Task.ID != assignment.TaskID || assignment.AssignmentRecord != nil && *assignment.AssignmentRecord != record.Reference() {
		return nil, fmt.Errorf("original Assignment responsibility differs from the current Record")
	}
	if runtimeID != "" && record.RuntimeID != runtimeID {
		return nil, fmt.Errorf("canonical Assignment Runtime %s differs from requested Runtime %s", record.RuntimeID, runtimeID)
	}
	if expected != nil && record.Reference() != expected.AssignmentRecord {
		return nil, fmt.Errorf("canonical Assignment differs from the recorded checkpoint revision")
	}
	resultRef, err := currentCanonicalResultRef(state, record.OwnerAgentID, record.Task.ID)
	if err != nil {
		return nil, err
	}
	if expected == nil && (assignment.CompletionRef == "" || assignment.CompletionRef != resultRef) {
		return nil, fmt.Errorf("Assignment completion projection differs from current Agent/TASK Result reference")
	}
	if expected != nil && expected.ResultPath != resultRef {
		return nil, fmt.Errorf("checkpoint Result path is not the current Agent/TASK Result")
	}
	read := func(ref string) ([]byte, error) {
		return workpacket.ReadFile(root, ref, assignmentresult.EvidenceReadLimit)
	}
	accepted, err := assignmentresult.LoadAccepted(state, record, resultRef, read)
	if err != nil {
		return nil, fmt.Errorf("load original accepted Result: %w", err)
	}
	binding := CanonicalResultBinding{
		AssignmentRecord: record.Reference(),
		ResultPath:       resultRef,
		ResultRevision:   accepted.ResultRevision,
	}
	if expected != nil && !sameCanonicalResultBinding(binding, *expected) {
		return nil, fmt.Errorf("current canonical Result differs from the inspected binding")
	}
	executionGeneration, err := canonicalExecutionGeneration(state, record)
	if err != nil {
		return nil, err
	}
	return &canonicalResultFacts{
		Record:              record,
		Accepted:            accepted,
		Binding:             binding,
		ExecutionGeneration: executionGeneration,
	}, nil
}

// sameCanonicalResultBinding compares the actual responsibility and result revision.
func sameCanonicalResultBinding(a, b CanonicalResultBinding) bool {
	return a.AssignmentRecord == b.AssignmentRecord && a.ResultPath == b.ResultPath && a.ResultRevision == b.ResultRevision
}

func canonicalExecutionGeneration(state map[string]any, record assignmentrecord.Record) (int, error) {
	if record.Contract == assignmentrecord.NativeDeliveryContract {
		return 0, nil
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return 0, fmt.Errorf("decode original workspace execution registry: %w", err)
	}
	if registry == nil {
		return 0, nil
	}
	execution, found := registry.Execution(record.AssignmentID, record.RuntimeID, record.BaselineGeneration)
	if !found {
		return 0, nil
	}
	if execution.AgentID != record.OwnerAgentID || execution.AssignmentRecord == nil || *execution.AssignmentRecord != record.Reference() || execution.Generation <= 0 {
		return 0, fmt.Errorf("registered workspace execution differs from the canonical Assignment owner or revision")
	}
	return execution.Generation, nil
}

func assignmentProjection(record assignmentrecord.Record) hookctx.AssignmentContext {
	ref := record.Reference()
	return hookctx.AssignmentContext{
		AssignmentID:       record.AssignmentID,
		AssignmentRecord:   &ref,
		AssignmentRevision: record.Revision,
		BaselineGeneration: record.BaselineGeneration,
		TaskID:             record.Task.ID,
		OwnerAgentID:       record.OwnerAgentID,
		RoleFamily:         record.RoleFamily,
		AgentDefinitionRef: record.AgentDefinition.Ref,
		ManifestRef:        record.Manifest.Ref,
		ResponsibilityIDs:  []string{record.ResponsibilityID},
		SkillRefs:          append([]string(nil), record.SkillRefs...),
		ReadPaths:          append([]string(nil), record.ReadPaths...),
		OutputPaths:        append([]string(nil), record.ScopeBasis.ManifestOutputPaths...),
		DependsOn:          append([]string(nil), record.DependsOn...),
		DoneWhen:           append([]string(nil), record.DoneWhen...),
		WritePaths:         append([]string(nil), record.EffectiveWritePaths...),
		RequiredChecks:     append([]string(nil), record.RequiredChecks...),
		CheckProfiles:      append([]checkexec.Profile(nil), record.CheckProfiles...),
	}
}

// ValidateCheckpointRecordBinding prevents a checkpoint copied into the
// current revision's slot from carrying predecessor verification. Main facts
// are the sole authority once present; the optional top-level Record tuple is
// only the early-checkpoint slot label used before those facts exist.
func ValidateCheckpointRecordBinding(checkpoint Checkpoint, record assignmentrecord.Record) error {
	wantRef := record.Reference()
	if checkpoint.CanonicalMainFacts != nil {
		binding := checkpoint.CanonicalMainFacts.Binding
		if binding.AssignmentRecord != wantRef {
			return fmt.Errorf("checkpoint Main facts belong to another Assignment Record revision")
		}
	}
	if checkpoint.AssignmentRecord != nil {
		if *checkpoint.AssignmentRecord != wantRef {
			return fmt.Errorf("checkpoint early Record binding differs from the current Assignment revision")
		}
	}
	if checkpoint.CanonicalMainFacts == nil && checkpoint.AssignmentRecord == nil {
		return fmt.Errorf("canonical checkpoint lacks its early Assignment Record binding")
	}
	return nil
}

// currentCanonicalResultRef requires the original Runtime Agent and TASK
// pointers to agree. It deliberately does not select an older history row or
// infer a current Result from a Worker-supplied path.
func currentCanonicalResultRef(state map[string]any, owner, taskID string) (string, error) {
	entities, _ := state["entities"].(map[string]any)
	var decoded struct {
		Agents []struct {
			ID                    string   `json:"id"`
			TaskIDs               []string `json:"task_ids"`
			CompletionReportedRef *string  `json:"completion_reported_ref"`
		} `json:"agents"`
		Tasks []struct {
			ID                  string   `json:"id"`
			OwnerAgentIDs       []string `json:"owner_agent_ids"`
			CompletionReportRef string   `json:"completion_report_ref"`
		} `json:"tasks"`
	}
	data, err := json.Marshal(entities)
	if err != nil {
		return "", fmt.Errorf("encode Runtime Agent/TASK entities: %w", err)
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", fmt.Errorf("decode Runtime Agent/TASK entities: %w", err)
	}
	var agentRef string
	agentCount := 0
	for _, agent := range decoded.Agents {
		if agent.ID != owner || !containsString(agent.TaskIDs, taskID) {
			continue
		}
		agentCount++
		if agent.CompletionReportedRef != nil {
			agentRef = *agent.CompletionReportedRef
		}
	}
	var taskRef string
	taskCount := 0
	for _, task := range decoded.Tasks {
		if task.ID != taskID {
			continue
		}
		taskCount++
		if !containsString(task.OwnerAgentIDs, owner) {
			return "", fmt.Errorf("current Runtime TASK %s is not owned by Agent %s", taskID, owner)
		}
		taskRef = task.CompletionReportRef
	}
	if agentCount != 1 || taskCount != 1 || agentRef == "" || taskRef == "" || agentRef != taskRef {
		return "", fmt.Errorf("current Runtime Agent/TASK Result pointers are missing, ambiguous, or disagree")
	}
	return agentRef, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type mainCheckReceipt struct {
	Command    string            `json:"command"`
	CWD        string            `json:"cwd"`
	Head       string            `json:"head"`
	HeadBefore string            `json:"head_before"`
	HeadAfter  string            `json:"head_after"`
	Status     string            `json:"status"`
	ExitCode   *int              `json:"exit_code"`
	Output     string            `json:"output"`
	Binding    checkexec.Binding `json:"check_binding"`
}

func mainCheckBinding(facts *canonicalResultFacts) checkexec.Binding {
	return checkexec.Binding{
		RuntimeID:           facts.Record.RuntimeID,
		Generation:          facts.Record.BaselineGeneration,
		AssignmentID:        facts.Record.AssignmentID,
		Owner:               "Main",
		ExecutionGeneration: facts.ExecutionGeneration,
		Responsibility:      "main_required_integration",
	}
}

func canonicalMainCheckRunner(record assignmentrecord.Record, binding checkexec.Binding) RequiredCheckRunner {
	profiles := make(map[int]checkexec.Profile, len(record.CheckProfiles))
	for _, profile := range record.CheckProfiles {
		profiles[profile.CheckIndex] = profile
	}
	commandIndexes := make(map[string][]int)
	for index, command := range record.RequiredChecks {
		if strings.HasPrefix(command, "locked:") {
			continue
		}
		commandIndexes[command] = append(commandIndexes[command], index)
	}
	seen := make(map[string]int)
	return func(ctx context.Context, root, command string) error {
		indexes := commandIndexes[command]
		occurrence := seen[command]
		if occurrence >= len(indexes) {
			return fmt.Errorf("Main invoked a command outside the canonical RequiredChecks sequence")
		}
		seen[command] = occurrence + 1
		if profile, ok := profiles[indexes[occurrence]]; ok {
			ctx = checkexec.WithProfile(ctx, profile)
		}
		return CommandCheckRunner(checkexec.WithBinding(ctx, binding), root, command)
	}
}

func mainCheckIndexes(record assignmentrecord.Record) []struct {
	Index   int
	Command string
} {
	var out []struct {
		Index   int
		Command string
	}
	for index, command := range record.RequiredChecks {
		if strings.HasPrefix(command, "locked:") {
			continue
		}
		out = append(out, struct {
			Index   int
			Command string
		}{Index: index, Command: command})
	}
	return out
}

// canonicalGitChangedPaths captures the actual Git name-only diff without
// treating whitespace as empty or trimming path bytes. Git's -z output is
// NUL-delimited; NUL cannot occur in a repository path.
func canonicalGitChangedPaths(ctx context.Context, root, base, head string) ([]string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(base) == "" || strings.TrimSpace(head) == "" {
		return nil, fmt.Errorf("changed-path inspection requires root, merge base and source head")
	}
	out, err := defaultRunner.Run(ctx, root, "diff", "--no-renames", "--name-only", "-z", base+".."+head)
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s..%s: %w", base, head, err)
	}
	paths, err := canonicalizeGitPathOutput(out)
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// canonicalizeGitPathOutput parses `git diff --name-only -z` output and
// returns a sorted, unique repository-relative path set. It permits spaces,
// tabs and newlines inside path names, all of which are valid Git path bytes.
func canonicalizeGitPathOutput(output string) ([]string, error) {
	if output == "" {
		return []string{}, nil
	}
	if !strings.HasSuffix(output, "\x00") {
		return nil, fmt.Errorf("git changed-path output is not NUL terminated")
	}
	parts := strings.Split(output[:len(output)-1], "\x00")
	paths := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for i, path := range parts {
		if path == "" {
			return nil, fmt.Errorf("git changed-path output contains an empty path at entry %d", i)
		}
		if !utf8.ValidString(path) {
			return nil, fmt.Errorf("git changed-path output contains a non-UTF-8 path at entry %d", i)
		}
		if filepath.IsAbs(path) || !filepath.IsLocal(filepath.FromSlash(path)) {
			return nil, fmt.Errorf("git changed-path output contains non-local path %q", path)
		}
		path = filepath.ToSlash(path)
		if _, exists := seen[path]; exists {
			return nil, fmt.Errorf("git changed-path output contains duplicate path %q", path)
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func canonicalizePathSet(paths []string) ([]string, error) {
	copyOfPaths := make([]string, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for i, path := range paths {
		if path == "" || filepath.IsAbs(path) || !filepath.IsLocal(filepath.FromSlash(path)) {
			return nil, fmt.Errorf("changed-path set contains non-local or empty path %q", path)
		}
		if !utf8.ValidString(path) {
			return nil, fmt.Errorf("changed-path set contains a non-UTF-8 path")
		}
		path = filepath.ToSlash(path)
		if _, exists := seen[path]; exists {
			return nil, fmt.Errorf("changed-path set contains duplicate path %q", path)
		}
		seen[path] = struct{}{}
		copyOfPaths[i] = path
	}
	sort.Strings(copyOfPaths)
	return copyOfPaths, nil
}

func samePathSet(left, right []string) bool {
	canonicalLeft, err := canonicalizePathSet(left)
	if err != nil {
		return false
	}
	canonicalRight, err := canonicalizePathSet(right)
	if err != nil || len(canonicalLeft) != len(canonicalRight) {
		return false
	}
	for i := range canonicalLeft {
		if canonicalLeft[i] != canonicalRight[i] {
			return false
		}
	}
	return true
}
