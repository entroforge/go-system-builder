package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS6MainCommandRecoversActualLostMergeAndLaterTarget(t *testing.T) {
	root, worker, record := nativeDeliveryFixture(t)
	brief, _, resultPath := assignmentrecord.NativePaths(record)
	nativeCommit(t, worker)
	nativeAnswer(t, worker, record)
	if code, output := nativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, resultPath), false); code != 0 {
		t.Fatalf("original native S6 delivery: %d %s", code, output)
	}
	loaded, err := hookctx.LoadFull(root, record.OwnerAgentID)
	if err != nil {
		t.Fatal(err)
	}
	var assignment hookctx.AssignmentContext
	for _, row := range loaded.Assignments {
		if row.AssignmentID == record.AssignmentID && row.OwnerAgentID == record.OwnerAgentID {
			assignment = row
		}
	}
	if assignment.WorktreePath != worker || assignment.Branch == "" {
		t.Fatalf("native return did not record actual Main integration coordinates: %+v", assignment)
	}
	inspectConfig, err := integration.AssignmentInspectConfig(assignment)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := integration.Inspect(t.Context(), integration.InspectRequest{Root: root, Assignment: assignment, RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration}, inspectConfig)
	if err != nil || !inspection.Ready {
		t.Fatalf("original native S6 Inspect: %+v %v", inspection, err)
	}
	const interrupted = "actual native S6 merge response lost"
	writes, caught := 0, false
	store := &integration.CheckpointStore{Clock: func() time.Time {
		writes++
		if writes == 2 {
			panic(interrupted)
		}
		return time.Now().UTC()
	}, NowString: func(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }}
	func() {
		defer func() {
			if p := recover(); p != nil {
				if p != interrupted {
					panic(p)
				}
				caught = true
			}
		}()
		_, err := integration.Integrate(t.Context(), integration.IntegrateRequest{Inspection: inspection}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: record.RuntimeID, CheckpointStore: store})
		t.Fatalf("did not interrupt actual native S6 merge/receipt window: %v", err)
	}()
	slot := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
	ready, found, err := store.Load(slot)
	if !caught || err != nil || !found || ready.State != integration.StateReady || ready.MergeCommit != "" {
		t.Fatalf("original interrupted ready: %+v %v", ready, err)
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := workspace.Git(t.Context(), root, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	merge := git("rev-parse", "HEAD")
	if merge == ready.TargetHead {
		t.Fatal("actual Git merge never happened")
	}
	if err := os.WriteFile(filepath.Join(root, "src/pkg/later.go"), []byte("package pkg\n\nconst LaterMainContribution = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "src/pkg/later.go")
	git("commit", "-m", "Later Main contribution after lost native merge response")
	head, count := git("rev-parse", "HEAD"), git("rev-list", "--all", "--count")
	var output, diagnostic bytes.Buffer
	args := []string{"runtime", "task-integrate", "--root", root, "--assignment-id", record.AssignmentID, "--agent-id", record.OwnerAgentID}
	if code := cli.Run(args, nil, &output, &diagnostic); code != 0 {
		t.Fatalf("original native S6 Main recovery: %d %s %s", code, &output, &diagnostic)
	}
	complete, found, err := store.Load(slot)
	if err != nil || !found || complete.State != integration.StateComplete || complete.MergeCommit != merge || complete.TestedHead != head || complete.CanonicalMainFacts == nil || len(complete.CheckReceipts) != 1 {
		t.Fatalf("public Main recovery did not preserve original merge/current Main checks: %+v %v; %s %s", complete, err, &output, &diagnostic)
	}
	if git("rev-parse", "HEAD") != head || git("rev-list", "--all", "--count") != count {
		t.Fatal("Main recovery repeated the Git merge")
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("original Main cleanup incomplete: %v", err)
	}
	before, err := os.ReadFile(slot)
	if err != nil {
		t.Fatal(err)
	}
	if code := cli.Run(args, nil, &bytes.Buffer{}, &diagnostic); code != 0 {
		t.Fatalf("completed Main recovery was not replay-safe: %d %s", code, &diagnostic)
	}
	after, err := os.ReadFile(slot)
	if err != nil || !bytes.Equal(before, after) || git("rev-parse", "HEAD") != head || git("rev-list", "--all", "--count") != count {
		t.Fatalf("Main command replay repeated merge/checks or rewrote their checkpoint: %v", err)
	}
}
