package buildsurface

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	loopruntime "github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/mainreceipt"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Read returns the original S6 responsibility and Main's saved integration
// facts. It does not authenticate source bytes, Git objects or review scope.
func Read(root string, state map[string]any) (Snapshot, error) {
	return ReadContext(context.Background(), root, state)
}

func ReadContext(ctx context.Context, root string, state map[string]any) (Snapshot, error) {
	var out Snapshot
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if root == "" || !HasCanonical(state) {
		return out, fmt.Errorf("S6 original responsibility is unavailable")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return out, err
	}
	branch, err := fileview.DevelopmentRef(state)
	if err != nil {
		return out, err
	}
	branch = strings.TrimPrefix(branch, "refs/heads/")
	baseline, _ := state["baseline"].(map[string]any)
	out = Snapshot{Contract: Contract, RuntimeID: textField(state["runtime_id"]), BaselineGeneration: number(baseline["generation"]), ProjectRoot: rootAbs, DevelopmentBranch: branch, Tasks: []Task{}, Contributions: []Contribution{}, Paths: []Path{}}
	// The current batch recheck is premised on the original TR-003 common
	// Main baseline of this generation. That premise is a structural fact of
	// the original stage, so a missing baseline is reported as fact. No
	// current branch, review round or runtime revision may stand in for it.
	journal, err := loopruntime.ReadJournalForStateContext(ctx, rootAbs, state)
	if err != nil {
		return Snapshot{}, fmt.Errorf("current S6 Main baseline journal: %w", err)
	}
	if _, err := loopruntime.RequireCurrentS6MainStartContext(ctx, state, journal); err != nil {
		return Snapshot{}, err
	}
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return Snapshot{}, err
	}
	registry, err := readExecutions(state)
	if err != nil {
		return Snapshot{}, err
	}
	seen := map[string]bool{}
	pathSeen := map[string]bool{}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		if record.RuntimeID != out.RuntimeID || record.BaselineGeneration != out.BaselineGeneration || seen[record.AssignmentID] {
			continue
		}
		current, found, err := assignmentrecord.FindRecord(records, out.RuntimeID, out.BaselineGeneration, record.AssignmentID)
		if err != nil || !found {
			return Snapshot{}, fmt.Errorf("current original Assignment unavailable: %v", err)
		}
		seen[record.AssignmentID] = true
		task := Task{ID: current.Task.ID, Path: current.Task.Ref, Version: current.Task.Version}
		out.Tasks = append(out.Tasks, task)
		agent, err := entity(state, "agents", current.OwnerAgentID)
		if err != nil {
			return Snapshot{}, err
		}
		ref := textField(agent["completion_reported_ref"])
		if ref == "" {
			return Snapshot{}, fmt.Errorf("original Result unavailable for %s", current.AssignmentID)
		}
		read := func(ref string) ([]byte, error) {
			return workpacket.ReadFile(root, ref, assignmentresult.EvidenceReadLimit)
		}
		accepted, err := assignmentresult.LoadContribution(state, current, ref, read)
		if err != nil {
			return Snapshot{}, fmt.Errorf("original Result %s: %w", current.AssignmentID, err)
		}
		generation, err := executionGeneration(registry, current, false)
		if err != nil {
			return Snapshot{}, err
		}
		proof, err := mainreceipt.Verify(ctx, mainreceipt.Input{AuthorityRoot: root, Record: current, Accepted: accepted, ResultRef: ref, ExecutionGeneration: generation, AuthorizedMainRoots: []string{rootAbs}, ExpectedTargetBranch: branch})
		if err != nil {
			return Snapshot{}, fmt.Errorf("original Main integration %s: %w", current.AssignmentID, err)
		}
		cpRef, err := checkpointRelative(rootAbs, proof.CheckpointPath())
		if err != nil {
			return Snapshot{}, err
		}
		out.Contributions = append(out.Contributions, Contribution{AssignmentRecord: current.Reference(), ResultRef: ref, CheckpointRef: cpRef})
		for _, name := range proof.MainFacts().ChangedPaths {
			if name != "" && !pathSeen[name] {
				out.Paths = append(out.Paths, Path{Path: name})
				pathSeen[name] = true
			}
		}
	}
	sort.Slice(out.Tasks, func(i, j int) bool { return out.Tasks[i].ID < out.Tasks[j].ID })
	sort.Slice(out.Paths, func(i, j int) bool { return out.Paths[i].Path < out.Paths[j].Path })
	return out, nil
}

// Verify is retained for explicit legacy callers. Actual responsibility and
// saved Main facts, rather than an old content digest, define the projection.
func Verify(root string, state map[string]any, expected Snapshot) error {
	return VerifyContext(context.Background(), root, state, expected)
}
func VerifyContext(ctx context.Context, root string, state map[string]any, expected Snapshot) error {
	actual, err := ReadContext(ctx, root, state)
	if err != nil {
		return err
	}
	if expected.RuntimeID != "" && expected.RuntimeID != actual.RuntimeID {
		return fmt.Errorf("S6 projection belongs to a different Runtime")
	}
	if expected.BaselineGeneration != 0 && expected.BaselineGeneration != actual.BaselineGeneration {
		return fmt.Errorf("S6 projection belongs to a different responsibility generation")
	}
	return nil
}
