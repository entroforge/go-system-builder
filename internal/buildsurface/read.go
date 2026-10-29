package buildsurface

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	loopruntime "github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/mainreceipt"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Read reconstructs the entire machine projection from original registered
// sources. It does not accept a caller-provided Git baseline or check verdict.
func Read(root string, state map[string]any) (Snapshot, error) {
	return ReadContext(context.Background(), root, state)
}

// ReadContext retains live authority, checkout and disk checks while reusing only pinned sources.
func ReadContext(ctx context.Context, root string, state map[string]any) (out Snapshot, err error) {
	ctx = WithReadCache(ctx)
	defer func() {
		if ctx.Err() != nil {
			out, err = Snapshot{}, ctx.Err()
		}
	}()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if root == "" || !HasCanonical(state) {
		return out, fmt.Errorf("canonical S6 product baseline requires the original Runtime root and Assignment registry")
	}
	canonicalRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		return out, rootErr
	}
	root, rootErr = filepath.Abs(canonicalRoot)
	if rootErr != nil {
		return out, rootErr
	}
	if err := fileview.ValidateAuthority(root, state); err != nil {
		return out, err
	}
	journal, err := loopruntime.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return out, err
	}
	start, err := loopruntime.RequireCurrentS6MainStartContext(ctx, state, journal)
	if err != nil {
		return out, err
	}
	if err := loopruntime.ValidateAssignmentRevisionReceiptsContext(ctx, state, journal); err != nil {
		return out, err
	}
	view, err := sourceViewContext(ctx, root, state)
	if err != nil {
		return out, err
	}
	branch, err := fileview.DevelopmentRef(state)
	if err != nil {
		return out, err
	}
	branch = strings.TrimPrefix(branch, "refs/heads/")
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return out, err
	}
	startRoot, startRootErr := filepath.EvalSymlinks(start.ProjectRoot)
	if startRootErr != nil || filepath.Clean(startRoot) != filepath.Clean(rootAbs) || start.DevelopmentBranch != branch || view.Commit == "" {
		return out, fmt.Errorf("original product start differs from current authority root/branch")
	}
	if err := verifyCheckoutContext(ctx, root, view.Commit); err != nil {
		return out, err
	}
	paths, err := changedPathsContext(ctx, root, start.StartCommit, view.Commit)
	if err != nil {
		return out, err
	}
	tasks, err := currentTasks(ctx, state, view)
	if err != nil {
		return out, err
	}
	if err := verifyLockedTaskSet(start.TaskSet, tasks); err != nil {
		return out, err
	}
	out = Snapshot{Contract: Contract, RuntimeID: start.RuntimeID, BaselineGeneration: start.BaselineGeneration, StartReceiptSHA256: start.ReceiptSHA256, StartCommit: start.StartCommit, FinalCommit: view.Commit, ProjectRoot: rootAbs, DevelopmentBranch: branch, Tasks: tasks, Contributions: []Contribution{}, Paths: paths}
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return Snapshot{}, err
	}
	byAssignment := map[string][]assignmentrecord.Record{}
	for _, record := range records {
		if record.RuntimeID == out.RuntimeID && record.BaselineGeneration == out.BaselineGeneration {
			byAssignment[record.AssignmentID] = append(byAssignment[record.AssignmentID], record)
		}
	}
	ids := make([]string, 0, len(byAssignment))
	for id := range byAssignment {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	covered := map[string]bool{}
	attributed := map[string]bool{}
	taskIndex := map[string]Task{}
	for _, task := range tasks {
		taskIndex[task.ID] = task
	}
	registry, err := readExecutions(state)
	if err != nil {
		return Snapshot{}, err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		group := byAssignment[id]
		sort.Slice(group, func(i, j int) bool { return group[i].Revision < group[j].Revision })
		current, found, err := assignmentrecord.FindRecord(records, out.RuntimeID, out.BaselineGeneration, id)
		if err != nil || !found {
			return Snapshot{}, fmt.Errorf("invalid current Assignment chain: %v", err)
		}
		task, exists := taskIndex[current.Task.ID]
		if !exists || covered[task.ID] || task.Path != current.Task.Ref || task.Version != current.Task.Version || task.SHA256 != current.Task.SHA256 {
			return Snapshot{}, fmt.Errorf("canonical Assignment does not uniquely cover exact current locked TASK %s", current.Task.ID)
		}
		covered[task.ID] = true
		for i, record := range group {
			ref := ""
			var accepted assignmentresult.AcceptedResult
			read := func(ref string) ([]byte, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return workpacket.ReadFile(root, ref, assignmentresult.EvidenceReadLimit)
			}
			if record.Reference() == current.Reference() {
				ref, err = currentResultRef(ctx, root, state, record, view)
				if err == nil {
					accepted, err = assignmentresult.LoadContribution(state, record, ref, read)
				}
				if err == nil {
					history, historyErr := assignmentresult.ContributionHistory(state, record, read)
					if historyErr != nil || len(history) == 0 || history[len(history)-1].EvidenceID != accepted.EvidenceID {
						err = fmt.Errorf("current Result is not the latest original registered answer: %v", historyErr)
					}
				}
				if err == nil && (record.ClosingContract == nil || !assignmentresult.ClosingAssessmentComplete(accepted.Answer) || !assignmentresult.WorkerClosureComplete(record, accepted) || len(accepted.Answer.RisksBlockers) > 0 || len(accepted.Answer.ScopeDeviations) > 0) {
					err = fmt.Errorf("current canonical owner professional/owned-check closure is incomplete")
				}
			} else {
				if i+1 >= len(group) {
					return Snapshot{}, fmt.Errorf("historical Assignment has no successor")
				}
				successor := group[i+1]
				event, receiptErr := revisionEvent(state, record, successor)
				if receiptErr != nil {
					return Snapshot{}, receiptErr
				}
				ref, err = supersededResultRef(state, record, event)
				if err == nil {
					accepted, err = assignmentresult.LoadSupersededAccepted(state, record, successor, event, ref, read)
				}
				if err == nil {
					err = verifySourceSnapshots(ctx, root, record)
				}
			}
			if err != nil {
				return Snapshot{}, fmt.Errorf("TASK %s Assignment r%d contribution: %w", record.Task.ID, record.Revision, err)
			}
			body, err := read(ref)
			if err != nil {
				return Snapshot{}, err
			}
			generation, err := executionGeneration(registry, record, record.Reference() != current.Reference())
			if err != nil {
				return Snapshot{}, err
			}
			proof, err := mainreceipt.Verify(ctx, mainreceipt.Input{AuthorityRoot: root, Record: record, Accepted: accepted, ResultRef: ref, ResultSHA256: hashBytes(body), ExecutionGeneration: generation, AuthorizedMainRoots: []string{rootAbs}, ExpectedTargetBranch: branch, FinalTargetHead: view.Commit})
			if record.Reference() != current.Reference() && errors.Is(err, mainreceipt.ErrNotVerified) {
				// Keep the original failed attempt unchanged. The full start-to-final
				// Git path set still covers its surviving product changes for S7;
				// only successful Main receipts count as verified contributions.
				continue
			}
			if err != nil {
				return Snapshot{}, fmt.Errorf("original Main contribution %s r%d: %w", id, record.Revision, err)
			}
			facts := proof.MainFacts()
			for _, file := range facts.ChangedPaths {
				attributed[file] = true
			}
			factsBytes, _ := json.Marshal(facts)
			cpRef, err := filepath.Rel(rootAbs, proof.CheckpointPath())
			if err != nil || !filepath.IsLocal(cpRef) {
				return Snapshot{}, fmt.Errorf("Main checkpoint escapes original authority")
			}
			sha, _ := record.Digest()
			out.Contributions = append(out.Contributions, Contribution{AssignmentRecord: record.Reference(), AssignmentRecordSHA256: sha, ResultRef: ref, ResultSHA256: hashBytes(body), CheckpointRef: filepath.ToSlash(cpRef), CheckpointSHA256: proof.CheckpointSHA256(), MergeCommit: facts.MergeCommit, TestedHead: facts.TestedHead, MainFactsSHA256: hashBytes(factsBytes)})
		}
	}
	if len(covered) != len(tasks) {
		return Snapshot{}, fmt.Errorf("canonical Main history lacks exact locked TASK coverage")
	}
	for _, file := range paths {
		if !attributed[file.Path] {
			out.UnattributedPaths = append(out.UnattributedPaths, file.Path)
		}
	}
	if err := view.Verify(); err != nil {
		return Snapshot{}, err
	}
	if err := verifyCheckoutContext(ctx, root, view.Commit); err != nil {
		return Snapshot{}, err
	}
	latestJournal, err := loopruntime.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return Snapshot{}, fmt.Errorf("recheck Runtime journal during product baseline read: %w", err)
	}
	if !bytes.Equal(journal, latestJournal) {
		return Snapshot{}, fmt.Errorf("Runtime source changed during product baseline read")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	out.Digest, err = Digest(out)
	return out, err
}

// Verify requires an exact fresh reconstruction, including the original
// start receipt, all task/revision contributions and pinned final Main HEAD.
func Verify(root string, state map[string]any, expected Snapshot) error {
	return VerifyContext(context.Background(), root, state, expected)
}

func VerifyContext(ctx context.Context, root string, state map[string]any, expected Snapshot) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if expected.Digest == "" {
		return fmt.Errorf("product baseline digest is missing")
	}
	actual, err := ReadContext(ctx, root, state)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("product baseline differs from original sources or pinned final Main")
	}
	return nil
}
