package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"
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
	if assignment.ManifestRef == "" && assignment.AssignmentRecord == nil && assignment.AssignmentRecordSHA256 == "" && assignment.AssignmentRevision == 0 {
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
	currentSHA, err := current.Digest()
	if err != nil {
		return nil, err
	}
	if current.Reference() != expected.AssignmentRecord || currentSHA != expected.AssignmentRecordSHA256 {
		return nil, fmt.Errorf("checkpoint Assignment reference or digest is stale")
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
	if err := fileview.ValidateAuthority(root, snapshot.State); err != nil {
		return nil, fmt.Errorf("validate Main Runtime authority: %w", err)
	}
	return snapshot.State, nil
}

func resolveCanonicalResultState(root, runtimeID string, state map[string]any, assignment hookctx.AssignmentContext, expected *CanonicalResultBinding) (*canonicalResultFacts, error) {
	if assignment.ManifestRef == "" && assignment.AssignmentRecord == nil && assignment.AssignmentRecordSHA256 == "" && assignment.AssignmentRevision == 0 {
		return nil, nil
	}
	record, found, err := hookctx.ResolveAssignmentRecordProjection(root, state, assignment)
	if err != nil {
		return nil, fmt.Errorf("resolve canonical Assignment projection: %w", err)
	}
	if !found {
		return nil, nil
	}
	if runtimeID != "" && record.RuntimeID != runtimeID {
		return nil, fmt.Errorf("canonical Assignment Runtime %s differs from requested Runtime %s", record.RuntimeID, runtimeID)
	}
	recordSHA, err := record.Digest()
	if err != nil {
		return nil, err
	}
	if expected != nil && (record.Reference() != expected.AssignmentRecord || recordSHA != expected.AssignmentRecordSHA256) {
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
	resultBytes, err := read(resultRef)
	if err != nil {
		return nil, fmt.Errorf("read accepted Result bytes: %w", err)
	}
	resultSHA := checkexec.Hash(resultBytes)
	binding := CanonicalResultBinding{
		AssignmentRecord:       record.Reference(),
		AssignmentRecordSHA256: recordSHA,
		ResultPath:             resultRef,
		ResultSHA256:           resultSHA,
		ResultRevision:         accepted.ResultRevision,
		AnswerDigest:           accepted.AnswerDigest,
	}
	if expected != nil && binding != *expected {
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
	sha, err := record.Digest()
	if err != nil {
		return 0, err
	}
	if execution.AgentID != record.OwnerAgentID || execution.AssignmentRecord == nil || *execution.AssignmentRecord != record.Reference() || execution.AssignmentRecordSHA256 != sha || execution.Generation <= 0 {
		return 0, fmt.Errorf("registered workspace execution differs from the canonical Assignment owner or revision")
	}
	return execution.Generation, nil
}

// canonicalSourceHasPriorMainMerge detects a source branch that descends from
// a side of an earlier non-squash merge already on the current target. A
// higher Assignment revision cannot treat only the commits after that old
// source head as its complete scope: predecessor Record/Main facts must first
// be validated before any merge reuse is possible.
func canonicalSourceHasPriorMainMerge(ctx context.Context, root, targetBranch, sourceHead string) (bool, error) {
	candidates, err := canonicalPriorMainMerges(ctx, root, targetBranch, sourceHead)
	return len(candidates) > 0, err
}

func assignmentProjection(record assignmentrecord.Record) hookctx.AssignmentContext {
	ref := record.Reference()
	sha, _ := record.Digest()
	return hookctx.AssignmentContext{
		AssignmentID:           record.AssignmentID,
		AssignmentRecord:       &ref,
		AssignmentRecordSHA256: sha,
		AssignmentRevision:     record.Revision,
		BaselineGeneration:     record.BaselineGeneration,
		TaskID:                 record.Task.ID,
		OwnerAgentID:           record.OwnerAgentID,
		RoleFamily:             record.RoleFamily,
		AgentDefinitionRef:     record.AgentDefinition.Ref,
		ManifestRef:            record.Manifest.Ref,
		ResponsibilityIDs:      []string{record.ResponsibilityID},
		SkillRefs:              append([]string(nil), record.SkillRefs...),
		ReadPaths:              append([]string(nil), record.ReadPaths...),
		OutputPaths:            append([]string(nil), record.ScopeBasis.ManifestOutputPaths...),
		DependsOn:              append([]string(nil), record.DependsOn...),
		DoneWhen:               append([]string(nil), record.DoneWhen...),
		WritePaths:             append([]string(nil), record.EffectiveWritePaths...),
		RequiredChecks:         append([]string(nil), record.RequiredChecks...),
		CheckProfiles:          append([]checkexec.Profile(nil), record.CheckProfiles...),
	}
}

// ValidateCheckpointRecordBinding prevents a checkpoint copied into the
// current revision's slot from carrying predecessor verification. Main facts
// are the sole authority once present; the optional top-level Record tuple is
// only the early-checkpoint slot label used before those facts exist.
func ValidateCheckpointRecordBinding(checkpoint Checkpoint, record assignmentrecord.Record) error {
	wantRef := record.Reference()
	wantSHA, err := record.Digest()
	if err != nil {
		return err
	}
	if checkpoint.CanonicalMainFacts != nil {
		binding := checkpoint.CanonicalMainFacts.Binding
		if binding.AssignmentRecord != wantRef || binding.AssignmentRecordSHA256 != wantSHA {
			return fmt.Errorf("checkpoint Main facts belong to another Assignment Record revision")
		}
	}
	if checkpoint.AssignmentRecord != nil || checkpoint.AssignmentRecordSHA256 != "" {
		if checkpoint.AssignmentRecord == nil || *checkpoint.AssignmentRecord != wantRef || checkpoint.AssignmentRecordSHA256 != wantSHA {
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

func canonicalProfileDigest(record assignmentrecord.Record, checkIndex int, command string) (string, error) {
	var digest string
	for _, profile := range record.CheckProfiles {
		if profile.CheckIndex != checkIndex {
			continue
		}
		if digest != "" || profile.Command != command || profile.Location != "main" {
			return "", fmt.Errorf("CheckProfile does not uniquely bind its canonical Main RequiredCheck")
		}
		digest = profile.Digest()
	}
	return digest, nil
}

func captureMainCheckFact(authorityRoot, checkpointPath, receiptPath, mainRoot, testedHead string, checkIndex int, command string, binding checkexec.Binding, profileDigest string) (assignmentresult.MainCheckFact, error) {
	var fact assignmentresult.MainCheckFact
	data, err := readMainCheckpointEvidence(authorityRoot, checkpointPath, receiptPath)
	if err != nil {
		return fact, err
	}
	var receipt mainCheckReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return fact, fmt.Errorf("decode Main check receipt: %w", err)
	}
	if receipt.Command != command || receipt.Status != "pass" || receipt.ExitCode == nil || *receipt.ExitCode != 0 || receipt.CWD != mainRoot || receipt.Head != testedHead || receipt.HeadBefore != testedHead || receipt.HeadAfter != testedHead || receipt.Binding != binding {
		return fact, fmt.Errorf("Main runner receipt does not prove the exact required command and tested head")
	}
	logData, err := readMainCheckpointEvidence(authorityRoot, checkpointPath, receipt.Output)
	if err != nil {
		return fact, fmt.Errorf("read Main check output: %w", err)
	}
	var envelope struct {
		ProfileObservation *checkexec.Observation `json:"profile_observation"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fact, fmt.Errorf("decode Main check envelope: %w", err)
	}
	profileObserved := ""
	if envelope.ProfileObservation != nil {
		profileObserved = envelope.ProfileObservation.ProfileDigest
	}
	if profileObserved != profileDigest {
		return fact, fmt.Errorf("Main check receipt profile differs from the original CheckProfile")
	}
	return assignmentresult.MainCheckFact{
		CheckIndex:    checkIndex,
		Command:       command,
		Status:        receipt.Status,
		ReceiptRef:    receiptPath,
		ReceiptSHA256: checkexec.Hash(data),
		LogRef:        receipt.Output,
		LogSHA256:     checkexec.Hash(logData),
		TestedHead:    testedHead,
		ProfileDigest: profileDigest,
	}, nil
}

func readMainCheckpointEvidence(authorityRoot, checkpointPath, ref string) ([]byte, error) {
	if strings.TrimSpace(authorityRoot) == "" {
		return nil, fmt.Errorf("Main evidence requires the original Runtime authority root")
	}
	refPath := ref
	if !filepath.IsAbs(refPath) {
		localRef := filepath.FromSlash(refPath)
		if !filepath.IsLocal(localRef) {
			return nil, fmt.Errorf("relative Main receipt ref must remain within its original sidecar")
		}
		refPath = localRef
	}
	root := filepath.Join(filepath.Dir(checkpointPath), "checks")
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	authorityAbs, err := filepath.Abs(authorityRoot)
	if err != nil {
		return nil, err
	}
	lexicalAuthorityRel, err := filepath.Rel(authorityAbs, rootAbs)
	if err != nil || !filepath.IsLocal(lexicalAuthorityRel) {
		return nil, fmt.Errorf("original Main checks directory escapes the Runtime authority root")
	}
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("original Main checks directory must be a real directory")
	}
	pathAbs, err := filepath.Abs(refPath)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("Main receipt path escapes its original checkpoint")
	}
	info, err := os.Lstat(pathAbs)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > 4<<20 {
		return nil, fmt.Errorf("Main receipt must be a bounded regular file")
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, err
	}
	resolvedAuthority, err := filepath.EvalSymlinks(authorityAbs)
	if err != nil {
		return nil, err
	}
	resolvedAuthorityRel, err := filepath.Rel(resolvedAuthority, resolvedRoot)
	if err != nil || !filepath.IsLocal(resolvedAuthorityRel) {
		return nil, fmt.Errorf("resolved Main checks directory escapes the Runtime authority root")
	}
	resolvedFile, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		return nil, err
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedFile)
	if err != nil || !filepath.IsLocal(resolvedRel) {
		return nil, fmt.Errorf("Main receipt file escaped or is linked outside its checkpoint")
	}
	f, err := os.Open(pathAbs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, fmt.Errorf("Main receipt exceeds bounded read")
	}
	return data, nil
}

func validateCanonicalMainFacts(authorityRoot, checkpointPath, mainRoot string, checkpoint Checkpoint, canonical *canonicalResultFacts) error {
	if canonical == nil || checkpoint.CanonicalMainFacts == nil {
		return fmt.Errorf("checkpoint lacks canonical Main facts")
	}
	facts := *checkpoint.CanonicalMainFacts
	if facts.Binding != canonical.Binding || filepath.Clean(facts.MainRoot) != filepath.Clean(mainRoot) || facts.ExecutionGeneration != canonical.ExecutionGeneration || facts.SourceHead != checkpoint.SourceHead || facts.TargetBranch != checkpoint.TargetBranch || facts.MergeBase != checkpoint.MergeBase || facts.MergeCommit != checkpoint.MergeCommit || facts.TestedHead != checkpoint.TestedHead || checkpoint.TestedHead == "" {
		return fmt.Errorf("checkpoint Main facts differ from the verified integration coordinates")
	}
	if err := assignmentresult.ValidateMainFacts(canonical.Record, canonical.Accepted, canonical.Binding.ResultPath, canonical.Binding.ResultSHA256, facts, func(ref string) ([]byte, error) {
		return readMainCheckpointEvidence(authorityRoot, checkpointPath, ref)
	}); err != nil {
		return err
	}
	expectedBinding := mainCheckBinding(canonical)
	for _, fact := range facts.Checks {
		data, err := readMainCheckpointEvidence(authorityRoot, checkpointPath, fact.ReceiptRef)
		if err != nil {
			return err
		}
		var receipt mainCheckReceipt
		if err := json.Unmarshal(data, &receipt); err != nil || receipt.Binding != expectedBinding {
			return fmt.Errorf("Main check receipt lacks the exact original Runtime/Assignment/execution binding")
		}
	}
	return validateCanonicalToolReceipts(checkpointPath, checkpoint, canonical, facts)
}

func validateCanonicalToolReceipts(checkpointPath string, checkpoint Checkpoint, canonical *canonicalResultFacts, facts assignmentresult.MainFacts) error {
	if err := VerifyCapturedChecks(checkpointPath, checkpoint); err != nil {
		return err
	}
	mainCheckout, err := filepath.Abs(facts.MainRoot)
	if err != nil {
		return err
	}
	mainCheckout, err = filepath.EvalSymlinks(mainCheckout)
	if err != nil {
		return fmt.Errorf("resolve original Main checkout for tool receipt validation: %w", err)
	}
	expectedBinding := mainCheckBinding(canonical)
	if len(checkpoint.ToolCheckReceipts) != len(canonical.Record.CheckProfiles) {
		return fmt.Errorf("checkpoint lacks a Main tool receipt for each original CheckProfile")
	}
	for _, profile := range canonical.Record.CheckProfiles {
		matchedFacts := 0
		for _, fact := range facts.Checks {
			if fact.CheckIndex == profile.CheckIndex && fact.Command == profile.Command && fact.ProfileDigest == profile.Digest() {
				matchedFacts++
			}
		}
		if matchedFacts != 1 {
			return fmt.Errorf("Main facts omit or duplicate CheckProfile %s coverage", profile.ID)
		}
		matched := 0
		for _, receipt := range checkpoint.ToolCheckReceipts {
			if receipt.Command != profile.Command || receipt.ProfileDigest != profile.Digest() {
				continue
			}
			if receipt.Binding != expectedBinding || receipt.Checkout != mainCheckout {
				return fmt.Errorf("Main tool receipt has a different Runtime, Assignment or execution binding")
			}
			verified, verifyErr := checkexec.VerifyToolReceipt(filepath.Join(filepath.Dir(checkpointPath), "checks"), receipt)
			if verifyErr != nil || !verified.Passed || verified.Observation.ProfileDigest != profile.Digest() {
				return fmt.Errorf("Main tool receipt for CheckProfile %s is missing, damaged or failed", profile.ID)
			}
			matched++
		}
		if matched != 1 {
			return fmt.Errorf("checkpoint lacks one original Main tool receipt for CheckProfile %s", profile.ID)
		}
	}
	return nil
}

// canonicalGitChangedPaths captures the actual Git name-only diff without
// treating whitespace as empty or trimming path bytes. Git's -z output is
// NUL-delimited; NUL cannot occur in a repository path. The SHA binds the
// sorted JSON representation of the complete path set.
func canonicalGitChangedPaths(ctx context.Context, root, base, head string) ([]string, string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(base) == "" || strings.TrimSpace(head) == "" {
		return nil, "", fmt.Errorf("changed-path inspection requires root, merge base and source head")
	}
	out, err := defaultRunner.Run(ctx, root, "diff", "--no-renames", "--name-only", "-z", base+".."+head)
	if err != nil {
		return nil, "", fmt.Errorf("git diff --name-only %s..%s: %w", base, head, err)
	}
	paths, err := canonicalizeGitPathOutput(out)
	if err != nil {
		return nil, "", err
	}
	digest, err := changedPathsDigest(paths)
	if err != nil {
		return nil, "", err
	}
	return paths, digest, nil
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

func changedPathsDigest(paths []string) (string, error) {
	canonical, err := canonicalizePathSet(paths)
	if err != nil {
		return "", err
	}
	return assignmentresult.ChangedPathsDigest(canonical)
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
