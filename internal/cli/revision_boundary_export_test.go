package cli

import (
	"bytes"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Test-only access to the original snapshot-to-Writer boundary. Production
// has no injected observer, retry loop or alternate registration consumer.
func PrepareNativeRevisionAtSnapshotForTest(root, base string, snapshot runtime.Snapshot, record assignmentrecord.Record, files fileview.Reader) (int, string, string) {
	var out, diagnostic bytes.Buffer
	code := runS6PrepareRevision(s6PrepareRevisionInput{
		NativeDelivery: true, BaseCommit: base, Root: root,
		State: snapshot.State, StateRevision: snapshot.Revision, Files: files,
		IntentKey: "revision-concurrency-boundary", AssignmentID: record.AssignmentID,
		TaskID: record.Task.ID, Role: record.RoleFamily, AgentID: "native-revision-boundary-r2",
		PlatformTeamID: "native-platform-revision-boundary-r2", REQID: "REQ-039",
	}, &out, &diagnostic)
	return code, out.String(), diagnostic.String()
}
