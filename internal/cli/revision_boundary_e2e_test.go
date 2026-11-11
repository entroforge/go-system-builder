package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	// The successor identity is derived from the TASK and the Assignment domain
	// revision only. It never depends on the Runtime revision, so an unrelated
	// Runtime commit cannot change it
	// (internal/cli/s6_prepare_revision.go:90-93; L3-S6-build.md:393/395).
	derivedAgent := fmt.Sprintf("native-%s-r%d", record.Task.ID, record.Revision+1)
	advanced := false
	committedRevision := snapshot.Revision
	changedSnapshot := ""
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
			code, _, diagnostic := runS6RevisionCLI(root, competingPath, record.Task.ID, record.Task.Ref, record.AssignmentID, "competing-revision", record.Reference())
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
			changedSnapshot = record.SourceSnapshots[0].Path
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
	switch change {
	case "same-assignment-replaced":
		// A competing registration for the same Assignment is refused by the
		// original Writer, and no successor under the derived identity exists.
		if !advanced || code == 0 || !strings.Contains(diagnostic, "S6 revision registration") {
			t.Fatalf("changed original authority was not rejected by the original consumer: change=%s advanced=%t code=%d stderr=%s stdout=%s", change, advanced, code, diagnostic, out)
		}
		next := stageSnapshot(t, root)
		_, found, err := assignmentrecord.ForOwner(next.State, derivedAgent)
		if err != nil || found || next.Revision != committedRevision {
			t.Fatalf("rejected preparation committed an extra successor: revision=%d/%d found=%t err=%v", next.Revision, committedRevision, found, err)
		}
		return
	case "original-source-snapshot-changed":
		// The frozen original-source guard is retired
		// (L4-workflow-convergence.md:293 — delete the frozen/legacy startup,
		// observation, reception and recovery wiring), so a changed source
		// snapshot must no longer be a byte gate on the current revision. The
		// current contract is asserted instead of the retired guard.
		if !advanced || code != 0 {
			t.Fatalf("a changed source snapshot blocked the current revision: advanced=%t code=%d stderr=%s stdout=%s", advanced, code, diagnostic, out)
		}
		next := stageSnapshot(t, root)
		current, found, err := assignmentrecord.ForOwner(next.State, derivedAgent)
		if err != nil || !found {
			t.Fatalf("the current revision was not issued after the source snapshot changed: %+v found=%t err=%v", current, found, err)
		}
		if current.AssignmentID != record.AssignmentID || current.Revision != record.Revision+1 || current.Contract != assignmentrecord.NativeDeliveryContract {
			t.Fatalf("the issued revision is not the exact successor of the original responsibility: %+v", current)
		}
		// The changed snapshot file is an on-disk artifact; the registered
		// SourceSnapshots rows are recorded facts and must not be rewritten by
		// it, and the new revision must not adopt the changed bytes.
		records, err := assignmentrecord.RecordsFromState(next.State)
		if err != nil {
			t.Fatal(err)
		}
		registered := []assignmentrecord.Record{}
		for _, candidate := range records {
			if candidate.AssignmentID == record.AssignmentID && candidate.Revision == record.Revision {
				registered = append(registered, candidate)
			}
		}
		if len(registered) != 1 {
			t.Fatalf("the original registration is no longer recorded exactly once: %d", len(registered))
		}
		if !reflect.DeepEqual(registered[0].SourceSnapshots, record.SourceSnapshots) {
			t.Fatalf("the changed source snapshot rewrote the registered SourceSnapshots: %+v", registered[0].SourceSnapshots)
		}
		if len(current.SourceSnapshots) == 0 {
			t.Fatal("the new revision registered no source snapshot of its own")
		}
		for _, snapshotRow := range current.SourceSnapshots {
			if snapshotRow.Path == changedSnapshot {
				t.Fatalf("the new revision reused the changed source snapshot bytes: %s", snapshotRow.Path)
			}
		}
		// The new responsibility is a distinct execution: it does not reuse the
		// original delivery location, team or owner, and the original execution
		// is not reactivated in place (L3-S6-build.md:395).
		if current.Manifest.Ref == record.Manifest.Ref || current.TeamID == record.TeamID || current.OwnerAgentID == record.OwnerAgentID {
			t.Fatalf("the new revision reused the original responsibility identity: %+v", current)
		}
		if current.RevisionIssuance == nil || current.RevisionIssuance.PreviousRecord != record.Reference() {
			t.Fatalf("the issued revision is not bound to the exact prior record: %+v", current.RevisionIssuance)
		}
		return
	}
	if code != 0 || !advanced {
		t.Fatalf("original native prepare could not continue the unchanged responsibility: code=%d advanced=%t stderr=%s stdout=%s", code, advanced, diagnostic, out)
	}
	// The unrelated Runtime commit really happened and was absorbed exactly
	// once, so the identity comparison below is not vacuous.
	if committedRevision != snapshot.Revision+1 {
		t.Fatalf("the unrelated commit was not absorbed exactly once: revision=%d/%d", committedRevision, snapshot.Revision)
	}
	next := stageSnapshot(t, root)
	current, found, err := assignmentrecord.ForOwner(next.State, derivedAgent)
	if err != nil || !found || current.AssignmentID != record.AssignmentID || current.Revision != record.Revision+1 || current.Contract != assignmentrecord.NativeDeliveryContract {
		t.Fatalf("original Writer did not issue the exact native successor: %+v found=%t err=%v", current, found, err)
	}
	// The successor identity is the one derivable from the TASK and the domain
	// revision before the unrelated commit: an unrelated Runtime commit must
	// not move it.
	if current.OwnerAgentID != derivedAgent {
		t.Fatalf("the successor identity depends on the unrelated commit: %q != %q", current.OwnerAgentID, derivedAgent)
	}
	// The successor is a distinct execution, not the original one reactivated.
	if current.Manifest.Ref == record.Manifest.Ref || current.TeamID == record.TeamID || current.OwnerAgentID == record.OwnerAgentID {
		t.Fatalf("the successor reused the original responsibility identity: %+v", current)
	}
	if current.RevisionIssuance == nil || current.RevisionIssuance.PreviousRecord != record.Reference() {
		t.Fatalf("the issued successor is not bound to the exact prior record: %+v", current.RevisionIssuance)
	}
	if next.Revision != snapshot.Revision+2 || !strings.Contains(out, "native Agent") {
		t.Fatalf("unexpected commits or dispatch output: revision=%d/%d out=%s", next.Revision, snapshot.Revision, out)
	}
}
