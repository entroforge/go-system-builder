package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type revisionBoundaryFiles struct {
	fileview.Reader
	advance func() error
}

func (f *revisionBoundaryFiles) Verify() error {
	if verifier, ok := f.Reader.(interface{ Verify() error }); ok {
		if err := verifier.Verify(); err != nil {
			return err
		}
	}
	if f.advance != nil {
		advance := f.advance
		f.advance = nil
		return advance()
	}
	return nil
}

func TestNativeS6RevisionKeepsObjectGuardsAcrossUnrelatedRuntimeCommit(t *testing.T) {
	for _, change := range []string{"unrelated", "same-assignment-replaced", "original-source-snapshot-changed"} {
		t.Run(change, func(t *testing.T) { testNativeRevisionBoundary(t, change) })
	}
}

func testNativeRevisionBoundary(t *testing.T, change string) {
	t.Helper()
	root, _, record := nativeDeliveryFixture(t)
	writeCanonicalS6PendingReworkFixture(t, s6CanonicalResultCLIFixture{root: root, record: record})
	snapshot := stageSnapshot(t, root)
	base, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := fileview.DevelopmentRef(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, ref, []fileview.Rule{{Path: "docs", Source: "git_tree"}, {Path: ".claude", Source: "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	advanced := false
	committedRevision := snapshot.Revision
	files := &revisionBoundaryFiles{Reader: view, advance: func() error {
		if change == "same-assignment-replaced" {
			data, err := os.ReadFile(filepath.Join(root, record.Manifest.Ref))
			if err != nil {
				return err
			}
			var manifest map[string]any
			if err := json.Unmarshal(data, &manifest); err != nil {
				return err
			}
			manifest["manifest_id"] = "team-manifest-native-competing-revision"
			manifest["workgroup_id"] = "workgroup-native-competing-revision"
			manifest["platform_team_id"] = "platform-native-competing-revision"
			manifest["assignments"].([]any)[0].(map[string]any)["agent_id"] = "native-competing-revision"
			const competingPath = ".claude/workgroups/manifest-native-competing-revision.json"
			stageJSON(t, root, competingPath, manifest)
			digest, err := record.Digest()
			if err != nil {
				return err
			}
			code, _, diagnostic := runS6RevisionCLI(root, competingPath, record.Task.ID, record.Task.Ref, record.AssignmentID, "competing-revision", record.Reference(), digest)
			if code != 0 {
				return fmt.Errorf("original competing Record registration: %s", diagnostic)
			}
			committedRevision = stageSnapshot(t, root).Revision
			advanced = true
			return nil
		}
		if change == "original-source-snapshot-changed" {
			if len(record.SourceSnapshots) == 0 {
				return fmt.Errorf("fixture has no original immutable source snapshots")
			}
			path := filepath.Join(root, record.SourceSnapshots[0].Path)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			advanced = true
			return os.WriteFile(path, append(data, '\n'), 0600)
		}
		writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
		next, err := writer.UpdateCurrent(runtime.Mutation{
			EventID: "evt-unrelated-revision-observation", TransitionID: "MILESTONE-REFRESH",
			Event: "milestone_refreshed", Actor: "Main", IdempotencyKey: "unrelated-revision-observation",
			RetainLastTransition: true,
		})
		if err == nil && next.Revision != snapshot.Revision+1 {
			return fmt.Errorf("unexpected unrelated commit revision %d", next.Revision)
		}
		advanced = err == nil
		committedRevision = next.Revision
		return err
	}}
	code, out, diagnostic := cli.PrepareNativeRevisionAtSnapshotForTest(root, strings.TrimSpace(base), snapshot, record, files)
	if change != "unrelated" {
		if !advanced || code == 0 || !strings.Contains(diagnostic, "S6 revision registration") {
			t.Fatalf("changed original authority was not rejected by the original consumer: change=%s advanced=%t code=%d stderr=%s stdout=%s", change, advanced, code, diagnostic, out)
		}
		next := stageSnapshot(t, root)
		_, found, err := assignmentrecord.ForOwner(next.State, "native-revision-boundary-r2")
		if err != nil || found || next.Revision != committedRevision {
			t.Fatalf("rejected preparation committed an extra successor: revision=%d/%d found=%t err=%v", next.Revision, committedRevision, found, err)
		}
		return
	}
	if code != 0 || !advanced {
		t.Fatalf("original native prepare could not continue the unchanged responsibility: code=%d advanced=%t stderr=%s stdout=%s", code, advanced, diagnostic, out)
	}
	next := stageSnapshot(t, root)
	current, found, err := assignmentrecord.ForOwner(next.State, "native-revision-boundary-r2")
	if err != nil || !found || current.AssignmentID != record.AssignmentID || current.Revision != record.Revision+1 || current.Contract != assignmentrecord.NativeDeliveryContract {
		t.Fatalf("original Writer did not issue the exact native successor: %+v found=%t err=%v", current, found, err)
	}
	if next.Revision != snapshot.Revision+2 || !strings.Contains(out, "native Agent") {
		t.Fatalf("unexpected commits or dispatch output: revision=%d/%d out=%s", next.Revision, snapshot.Revision, out)
	}
}
