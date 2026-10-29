package qualitygate

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func TestCanonicalMainCheckArtifactRequiresCurrentRevisionSlot(t *testing.T) {
	root := t.TempDir()
	record := assignmentrecord.Record{RuntimeID: "loop-revision-test", BaselineGeneration: 1, AssignmentID: "assignment-revision-test", Revision: 2}
	current := filepath.Dir(assignmentrecord.IntegrationCheckpointPath(root, record.Reference()))
	receipt := filepath.Join(current, "checks", "loop-integration-check-test", "receipt.json")
	if _, err := canonicalMainCheckArtifactRel(root, record, root, nil, receipt); err != nil {
		t.Fatalf("current revision check reference: %v", err)
	}
	oldRef := record.Reference()
	oldRef.Revision = 1
	old := filepath.Join(filepath.Dir(assignmentrecord.IntegrationCheckpointPath(root, oldRef)), "checks", "loop-integration-check-test", "receipt.json")
	if _, err := canonicalMainCheckArtifactRel(root, record, root, nil, old); err == nil {
		t.Fatal("prior revision check receipt must not satisfy new charter")
	}
}
