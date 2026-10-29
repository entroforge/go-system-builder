package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const (
	predecessorSnapshotFileLimit  = 16 << 20
	predecessorSnapshotTotalLimit = 64 << 20
	predecessorCheckpointLimit    = 16 << 20
)

type canonicalPriorMainMerge struct {
	MergeCommit string
	FirstParent string
	SideParent  string
}

type canonicalPredecessorMain struct {
	Record         assignmentrecord.Record
	Accepted       assignmentresult.AcceptedResult
	Binding        CanonicalResultBinding
	Checkpoint     Checkpoint
	CheckpointPath string
	CheckpointSHA  string
	Receipt        loopruntime.AssignmentRevisionReceipt
	Merge          canonicalPriorMainMerge
}

// canonicalPriorMainMerges locates first-parent Main merges whose side parent
// is an ancestor of sourceHead while the merge commit itself is not. It finds
// candidates only; it grants no merge authority until the exact predecessor
// Record, accepted Result, checkpoint and Git parents are validated.
func canonicalPriorMainMerges(ctx context.Context, root, targetBranch, sourceHead string) ([]canonicalPriorMainMerge, error) {
	merges, err := defaultRunner.Run(ctx, root, "rev-list", "--first-parent", "--merges", targetBranch)
	if err != nil {
		return nil, fmt.Errorf("list target first-parent merges: %w", err)
	}
	var candidates []canonicalPriorMainMerge
	for _, merge := range strings.Fields(merges) {
		parentsOutput, err := defaultRunner.Run(ctx, root, "show", "-s", "--format=%P", merge)
		if err != nil {
			return nil, fmt.Errorf("read parents of target merge %s: %w", merge, err)
		}
		parents := strings.Fields(parentsOutput)
		if len(parents) != 2 {
			continue
		}
		sourceBase, err := mergeBase(ctx, root, parents[1], sourceHead)
		if err != nil {
			return nil, fmt.Errorf("compare predecessor source merge parent %s: %w", parents[1], err)
		}
		if sourceBase != parents[1] {
			continue
		}
		mergeBaseInSource, err := mergeBase(ctx, root, merge, sourceHead)
		if err != nil {
			return nil, fmt.Errorf("compare predecessor Main merge %s with source %s: %w", merge, sourceHead, err)
		}
		if mergeBaseInSource == merge {
			continue
		}
		mainBase, err := mergeBase(ctx, root, parents[1], parents[0])
		if err != nil {
			return nil, fmt.Errorf("compare prior Main merge parents: %w", err)
		}
		if mainBase == parents[1] {
			continue
		}
		candidates = append(candidates, canonicalPriorMainMerge{MergeCommit: merge, FirstParent: parents[0], SideParent: parents[1]})
	}
	return candidates, nil
}

// resolveCanonicalPredecessorMain validates all immutable predecessor sources
// before an older side-parent branch may enter Integrate. It does not transfer
// old verification: Integrate still creates the current Record's checkpoint
// and runs every current Main check after the new merge.
func resolveCanonicalPredecessorMain(ctx context.Context, root, mainRoot, targetBranch, sourceHead, expectedMergeBase string, successor assignmentrecord.Record, store *CheckpointStore, candidates []canonicalPriorMainMerge) (*canonicalPredecessorMain, error) {
	if successor.Revision < 2 || successor.RevisionIssuance == nil || store == nil {
		return nil, fmt.Errorf("current Assignment has no revision predecessor authority")
	}
	state, journal, err := readCanonicalRuntimePair(root)
	if err != nil {
		return nil, err
	}
	if err := fileview.ValidateAuthority(root, state); err != nil {
		return nil, fmt.Errorf("validate predecessor Runtime authority: %w", err)
	}
	if err := loopruntime.ValidateAssignmentRevisionReceipts(state, journal); err != nil {
		return nil, fmt.Errorf("validate complete Assignment revision receipt/journal chain: %w", err)
	}
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return nil, fmt.Errorf("read predecessor Record history: %w", err)
	}
	current, found, err := assignmentrecord.FindRecordRevision(records, successor.Reference())
	if err != nil || !found {
		return nil, fmt.Errorf("current Assignment disappeared from original Runtime history: %v", err)
	}
	currentSHA, err := current.Digest()
	if err != nil {
		return nil, err
	}
	successorSHA, err := successor.Digest()
	if err != nil || currentSHA != successorSHA {
		return nil, fmt.Errorf("current Assignment changed while resolving predecessor provenance")
	}
	latest, found, err := assignmentrecord.FindRecord(records, successor.RuntimeID, successor.BaselineGeneration, successor.AssignmentID)
	if err != nil || !found || latest.Reference() != successor.Reference() {
		return nil, fmt.Errorf("successor Record is no longer the current Assignment revision")
	}
	previousRef := successor.RevisionIssuance.PreviousRecord
	previous, found, err := assignmentrecord.FindRecordRevision(records, previousRef)
	if err != nil || !found {
		return nil, fmt.Errorf("immediate predecessor Record is missing: %v", err)
	}
	previousSHA, err := previous.Digest()
	if err != nil || previousSHA != successor.RevisionIssuance.PreviousRecordSHA256 {
		return nil, fmt.Errorf("immediate predecessor Record digest differs from issuance")
	}
	if previous.RuntimeID != successor.RuntimeID || previous.BaselineGeneration != successor.BaselineGeneration || previous.AssignmentID != successor.AssignmentID || previous.Revision+1 != successor.Revision {
		return nil, fmt.Errorf("predecessor is not the immediate prior revision in the same namespace")
	}
	if len(previous.SourceSnapshots) == 0 {
		return nil, fmt.Errorf("predecessor Record has no complete source snapshots; side-parent reuse remains pending")
	}
	if err := verifyPredecessorSourceSnapshots(root, previous); err != nil {
		return nil, err
	}
	receipt, err := exactAssignmentRevisionReceipt(state, successor)
	if err != nil {
		return nil, err
	}
	resultRef, err := supersededPredecessorResultRef(state, previous, receipt.RuntimeEventID)
	if err != nil {
		return nil, err
	}
	read := func(ref string) ([]byte, error) {
		return workpacket.ReadFile(root, ref, assignmentresult.EvidenceReadLimit)
	}
	accepted, err := assignmentresult.LoadSupersededAccepted(state, previous, successor, receipt.RuntimeEventID, resultRef, read)
	if err != nil {
		return nil, fmt.Errorf("validate original superseded predecessor Result: %w", err)
	}
	resultBytes, err := read(resultRef)
	if err != nil {
		return nil, fmt.Errorf("read predecessor Result source: %w", err)
	}
	resultSHA := checkexec.Hash(resultBytes)
	binding := CanonicalResultBinding{
		AssignmentRecord:       previous.Reference(),
		AssignmentRecordSHA256: previousSHA,
		ResultPath:             resultRef,
		ResultSHA256:           resultSHA,
		ResultRevision:         accepted.ResultRevision,
		AnswerDigest:           accepted.AnswerDigest,
	}
	checkpointPath := store.PathForRecord(root, previous.Reference())
	if checkpointPath == "" {
		return nil, fmt.Errorf("predecessor Record has no deterministic checkpoint path")
	}
	checkpointBytes, checkpoint, err := readCanonicalCheckpoint(root, checkpointPath)
	if err != nil {
		return nil, fmt.Errorf("read predecessor checkpoint: %w", err)
	}
	if checkpoint.AssignmentID != previous.AssignmentID || checkpoint.TaskID != previous.Task.ID || checkpoint.BaselineGeneration != previous.BaselineGeneration || checkpoint.TargetBranch != targetBranch {
		return nil, fmt.Errorf("predecessor checkpoint identity differs from the issued Record or current target")
	}
	if checkpoint.State != StateVerified && checkpoint.State != StateAcknowledged && checkpoint.State != StateCleanupPending && checkpoint.State != StateComplete {
		return nil, fmt.Errorf("predecessor checkpoint was not durably Main-verified")
	}
	if err := ValidateCheckpointRecordBinding(checkpoint, previous); err != nil {
		return nil, fmt.Errorf("predecessor checkpoint Record binding: %w", err)
	}
	if checkpoint.CanonicalMainFacts == nil || checkpoint.CanonicalMainFacts.Binding != binding {
		return nil, fmt.Errorf("predecessor Main facts do not bind the exact superseded Result")
	}
	executionGeneration, err := canonicalPredecessorExecutionGeneration(state, previous)
	if err != nil {
		return nil, err
	}
	if checkpoint.CanonicalMainFacts.ExecutionGeneration != executionGeneration {
		return nil, fmt.Errorf("predecessor Main facts execution generation differs from its original Runtime workspace binding")
	}
	oldCanonical := &canonicalResultFacts{
		Record:              previous,
		Accepted:            accepted,
		Binding:             binding,
		ExecutionGeneration: executionGeneration,
	}
	if err := validateCanonicalMainFacts(root, checkpointPath, mainRoot, checkpoint, oldCanonical); err != nil {
		return nil, fmt.Errorf("validate predecessor Main receipts and facts: %w", err)
	}
	oldFacts := checkpoint.CanonicalMainFacts
	if oldFacts.TestedHead != checkpoint.MergeCommit {
		return nil, fmt.Errorf("predecessor Main checks were not tested at its recorded merge commit")
	}
	oldPaths, oldPathsSHA, err := canonicalGitChangedPaths(ctx, mainRoot, oldFacts.MergeBase, oldFacts.SourceHead)
	if err != nil || oldPathsSHA != oldFacts.ChangedPathsSHA256 || !samePathSet(oldPaths, oldFacts.ChangedPaths) {
		return nil, fmt.Errorf("predecessor Main changed-path facts differ from its original Git range: %v", err)
	}
	if len(outsideWriteScope(oldPaths, previous.EffectiveWritePaths)) != 0 {
		return nil, fmt.Errorf("predecessor source diff escapes its original Record scope")
	}
	if len(candidates) != 1 {
		return nil, fmt.Errorf("source branch has %d predecessor Main merge candidates; exactly one is required", len(candidates))
	}
	merge := candidates[0]
	if merge.MergeCommit != checkpoint.MergeCommit || merge.FirstParent != checkpoint.TargetHead || merge.SideParent != oldFacts.SourceHead {
		return nil, fmt.Errorf("source branch side-parent does not match the exact immediate predecessor Main merge")
	}
	actualMergeBase, err := mergeBase(ctx, mainRoot, targetBranch, sourceHead)
	if err != nil || actualMergeBase != oldFacts.SourceHead || expectedMergeBase != oldFacts.SourceHead {
		return nil, fmt.Errorf("source branch merge-base must equal immediate predecessor SourceHead: %v", err)
	}
	if sourceHead == oldFacts.SourceHead {
		return nil, fmt.Errorf("source branch has no new commit beyond predecessor SourceHead")
	}
	targetBase, err := mergeBase(ctx, mainRoot, checkpoint.MergeCommit, targetBranch)
	if err != nil || targetBase != checkpoint.MergeCommit {
		return nil, fmt.Errorf("current target no longer contains predecessor MergeCommit: %v", err)
	}
	mergeParents, err := defaultRunner.Run(ctx, mainRoot, "show", "-s", "--format=%P", checkpoint.MergeCommit)
	if err != nil || strings.Join(strings.Fields(mergeParents), " ") != checkpoint.TargetHead+" "+oldFacts.SourceHead {
		return nil, fmt.Errorf("predecessor MergeCommit is not the original two-parent Main merge: %v", err)
	}
	return &canonicalPredecessorMain{
		Record: previous, Accepted: accepted, Binding: binding,
		Checkpoint: checkpoint, CheckpointPath: checkpointPath,
		CheckpointSHA: checkexec.Hash(checkpointBytes), Receipt: receipt, Merge: merge,
	}, nil
}

func canonicalPredecessorExecutionGeneration(state map[string]any, previous assignmentrecord.Record) (int, error) {
	registry, err := workspace.Decode(state)
	if err != nil {
		return 0, fmt.Errorf("decode predecessor Runtime workspace authority: %w", err)
	}
	if registry == nil {
		return 0, nil
	}
	previousSHA, err := previous.Digest()
	if err != nil {
		return 0, fmt.Errorf("digest predecessor Assignment for workspace binding: %w", err)
	}
	previousRef := previous.Reference()
	var matched *workspace.Execution
	for _, execution := range registry.AllExecutions() {
		sameRecordRef := execution.AssignmentRecord != nil && *execution.AssignmentRecord == previousRef
		sameOwnerTuple := execution.AssignmentID == previous.AssignmentID && execution.RuntimeID == previous.RuntimeID && execution.BaselineGeneration == previous.BaselineGeneration && execution.AgentID == previous.OwnerAgentID
		if !sameRecordRef && !sameOwnerTuple {
			continue
		}
		if execution.AssignmentID != previous.AssignmentID || execution.RuntimeID != previous.RuntimeID || execution.BaselineGeneration != previous.BaselineGeneration || execution.AgentID != previous.OwnerAgentID || execution.AssignmentRecord == nil || *execution.AssignmentRecord != previousRef || execution.AssignmentRecordSHA256 != previousSHA || execution.Generation < 1 || execution.Status != "retired" {
			return 0, fmt.Errorf("predecessor workspace execution does not bind the exact retired Record, owner, and generation")
		}
		if matched != nil {
			return 0, fmt.Errorf("predecessor Runtime workspace has duplicate historical executions for one Record")
		}
		copy := execution
		matched = &copy
	}
	if matched == nil {
		return 0, nil
	}
	return matched.Generation, nil
}

func readCanonicalRuntimePair(root string) (map[string]any, []byte, error) {
	if root == "" {
		return nil, nil, fmt.Errorf("predecessor provenance requires the Runtime authority root")
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	store := loopruntime.NewStore(statePath, journalPath)
	before, err := store.Snapshot()
	if err != nil {
		return nil, nil, fmt.Errorf("read original Runtime state before journal: %w", err)
	}
	segments, err := loopruntime.JournalSegmentPaths(journalPath)
	if err != nil {
		return nil, nil, fmt.Errorf("list original Runtime journal segments: %w", err)
	}
	var journal bytes.Buffer
	for _, segment := range segments {
		if err := appendJournalFile(&journal, segment); err != nil {
			return nil, nil, err
		}
	}
	if err := appendJournalFile(&journal, journalPath); err != nil {
		return nil, nil, err
	}
	after, err := store.Snapshot()
	if err != nil {
		return nil, nil, fmt.Errorf("re-read original Runtime state after journal: %w", err)
	}
	if before.Revision != after.Revision {
		return nil, nil, fmt.Errorf("Runtime changed while reading predecessor revision journal")
	}
	return before.State, journal.Bytes(), nil
}

func appendJournalFile(dst *bytes.Buffer, path string) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open original Runtime journal %s: %w", filepath.Base(path), err)
	}
	defer file.Close()
	if _, err := io.Copy(dst, file); err != nil {
		return fmt.Errorf("read original Runtime journal %s: %w", filepath.Base(path), err)
	}
	return nil
}

func exactAssignmentRevisionReceipt(state map[string]any, successor assignmentrecord.Record) (loopruntime.AssignmentRevisionReceipt, error) {
	var receipts []loopruntime.AssignmentRevisionReceipt
	entities, _ := state["entities"].(map[string]any)
	data, err := json.Marshal(entities["assignment_revision_receipts"])
	if err != nil || json.Unmarshal(data, &receipts) != nil || receipts == nil {
		return loopruntime.AssignmentRevisionReceipt{}, fmt.Errorf("decode original Assignment revision receipts")
	}
	successorSHA, err := successor.Digest()
	if err != nil {
		return loopruntime.AssignmentRevisionReceipt{}, err
	}
	var found *loopruntime.AssignmentRevisionReceipt
	for i := range receipts {
		row := &receipts[i]
		if row.AssignmentRecord != successor.Reference() {
			continue
		}
		if found != nil {
			return loopruntime.AssignmentRevisionReceipt{}, fmt.Errorf("current Assignment has duplicate revision receipts")
		}
		if row.AssignmentRecordSHA256 != successorSHA || row.PreviousRecord != successor.RevisionIssuance.PreviousRecord || row.PreviousRecordSHA256 != successor.RevisionIssuance.PreviousRecordSHA256 || row.IntentKey != successor.RevisionIssuance.IntentKey || row.RequestDigest != successor.RevisionIssuance.RequestDigest || row.RuntimeEventID == "" {
			return loopruntime.AssignmentRevisionReceipt{}, fmt.Errorf("original revision receipt differs from current Record issuance")
		}
		found = row
	}
	if found == nil {
		return loopruntime.AssignmentRevisionReceipt{}, fmt.Errorf("current Assignment has no original revision receipt")
	}
	return *found, nil
}

func supersededPredecessorResultRef(state map[string]any, previous assignmentrecord.Record, eventID string) (string, error) {
	rows, _ := state["evidence"].([]any)
	var result string
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["kind"] != "completion_report" || row["responsibility_id"] != "BUILD-WORK-PACKAGE" || row["status"] != "superseded" || row["invalidated_by"] != eventID || row["invalidation_rule"] != "assignment_revision_issued" || predecessorEvidenceNumber(row["baseline_generation"]) != previous.BaselineGeneration || !predecessorEvidenceHasOwner(row["produced_by"], previous.OwnerAgentID) {
			continue
		}
		ref, _ := row["path"].(string)
		if ref == "" || result != "" {
			return "", fmt.Errorf("revision event does not identify exactly one superseded predecessor Result")
		}
		result = ref
	}
	if result == "" {
		return "", fmt.Errorf("revision event has no superseded predecessor Result")
	}
	return result, nil
}

func predecessorEvidenceNumber(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := number.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func predecessorEvidenceHasOwner(value any, owner string) bool {
	switch rows := value.(type) {
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

func verifyPredecessorSourceSnapshots(root string, record assignmentrecord.Record) error {
	if len(record.SourceSnapshots) == 0 {
		return fmt.Errorf("predecessor Record source snapshots are required")
	}
	var total int64
	for _, snapshot := range record.SourceSnapshots {
		data, err := workpacket.ReadFile(root, snapshot.Path, predecessorSnapshotFileLimit)
		if err != nil || checkexec.Hash(data) != snapshot.SHA256 {
			return fmt.Errorf("predecessor frozen source snapshot %s is missing or changed", snapshot.SourceRef)
		}
		total += int64(len(data))
		if total > predecessorSnapshotTotalLimit {
			return fmt.Errorf("predecessor frozen source snapshots exceed their original size budget")
		}
	}
	return nil
}

func readCanonicalCheckpoint(root, path string) ([]byte, Checkpoint, error) {
	var checkpoint Checkpoint
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, checkpoint, err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return nil, checkpoint, err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, checkpoint, fmt.Errorf("checkpoint escapes the Runtime authority root")
	}
	cursor := rootAbs
	for _, segment := range strings.Split(rel, string(filepath.Separator)) {
		cursor = filepath.Join(cursor, segment)
		info, statErr := os.Lstat(cursor)
		if statErr != nil {
			return nil, checkpoint, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, checkpoint, fmt.Errorf("checkpoint path contains a symlink")
		}
	}
	info, err := os.Stat(pathAbs)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > predecessorCheckpointLimit {
		return nil, checkpoint, fmt.Errorf("checkpoint must be a bounded regular file")
	}
	data, err := os.ReadFile(pathAbs)
	if err != nil {
		return nil, checkpoint, err
	}
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, checkpoint, err
	}
	return data, checkpoint, nil
}

func predecessorCheckpointSHA(data []byte) string {
	return checkexec.Hash(data)
}
