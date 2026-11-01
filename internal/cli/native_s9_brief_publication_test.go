package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Two issuers can prepare concurrently, but only the original packet Writer
// selects the published reference. Unknown occupants are never claimed.
func TestS9BriefOriginalIssuanceUsesActualReferenceUnderConcurrentOccupancy(t *testing.T) {
	root, plan, _, _, dispatch := repairExecutionPlanningFixture(t)
	assignment, owner := plan.Assignments[0].AssignmentID, "backend-publication-test"
	var stdout, stderr bytes.Buffer
	if code := runRuntimeRepairDispatch([]string{"--root", root, "--assignment-id", assignment, "--agent-id", owner, "--role-family", "backend-builder"}, &stdout, &stderr); code != 0 {
		t.Fatalf("register original owner: %s %s", &stdout, &stderr)
	}
	svc := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
	var entered sync.WaitGroup
	entered.Add(2)
	ready := make(chan struct{})
	go func() { entered.Wait(); close(ready) }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var once sync.Once
	var occupyErr error
	var occupied string
	refs := make([]string, 2)
	errs := make([]error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	for i := range refs {
		go func(i int) {
			defer workers.Done()
			result, err := svc.IssueRepairPlanReportForAssignmentPrepared(ctx, owner, assignment, func(ctx context.Context, packet *workpacket.RepairPlanReportPacket) error {
				once.Do(func() {
					occupied = filepath.Join(root, nativeS9BriefPath(*packet))
					occupyErr = os.MkdirAll(filepath.Dir(occupied), 0700)
					if occupyErr == nil {
						occupyErr = os.WriteFile(occupied, []byte("unrelated occupant"), 0600)
					}
				})
				entered.Done()
				select {
				case <-ready:
				case <-ctx.Done():
					return ctx.Err()
				}
				if occupyErr != nil {
					return occupyErr
				}
				return prepareNativeS9BriefForIssue(ctx, root, packet)
			})
			if err == nil && len(result) != 1 {
				err = fmt.Errorf("unexpected result count %d", len(result))
			}
			if err == nil {
				refs[i] = result[0].Packet.BriefPath
			}
			errs[i] = err
		}(i)
	}
	workers.Wait()
	if errs[0] != nil || errs[1] != nil || refs[0] == "" || refs[0] != refs[1] {
		t.Fatalf("issuance did not converge on one original reference: refs=%q errors=%v", refs, errs)
	}
	data, err := os.ReadFile(occupied)
	if err != nil || string(data) != "unrelated occupant" {
		t.Fatalf("unknown occupant changed: %q %v", data, err)
	}
	if filepath.Join(root, refs[0]) == occupied {
		t.Fatal("unknown occupant was adopted")
	}
	brief, err := os.ReadFile(filepath.Join(root, refs[0]))
	if err != nil || !strings.Contains(string(brief), filepath.Join(root, refs[0])) {
		t.Fatalf("actual brief reference missing: %v %s", err, brief)
	}
	snapshot := repairDispatchSnapshot(t, root)
	guidance, err := repairDispatchPlanReportBrief(root, repairLeadSaveInput(root, dispatch, "Main"), snapshot.State, assignment, owner)
	if err != nil || !strings.Contains(guidance, filepath.Join(root, refs[0])) {
		t.Fatalf("original dispatch did not carry selected brief: %v %s", err, guidance)
	}
	replay, err := svc.IssueRepairPlanReportForAssignmentPrepared(context.Background(), owner, assignment, func(context.Context, *workpacket.RepairPlanReportPacket) error {
		return fmt.Errorf("registered replay attempted another publication")
	})
	if err != nil || len(replay) != 1 || replay[0].Packet.BriefPath != refs[0] {
		t.Fatalf("original replay lost selected path: %#v %v", replay, err)
	}
}
