package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/integration"
)

// A real deleted product path reaches Main and the S10 material view without
// requiring a Git-object proof or a separate review-plan baseline.
func TestMainDeletedPathReachesAcceptanceMaterials(t *testing.T) {
	const removed = "src/pkg/input.go"
	fixture := newCanonicalMainFixtureWithWorkerSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003, func(t *testing.T, worker string) {
		if err := os.Remove(filepath.Join(worker, removed)); err != nil {
			t.Fatal(err)
		}
	})
	inspection := fixture.inspect(t, "")
	if !inspection.Ready || !containsString(inspection.ChangedPaths, removed) {
		t.Fatalf("Main inspection must include the real deletion: %+v", inspection)
	}
	integrated, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig())
	if err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("integrate deletion: %+v %v", integrated.Checkpoint, err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	surface, err := buildsurface.Read(fixture.root, current.State)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range surface.Paths {
		if path.Path == removed {
			found = true
		}
	}
	if !found {
		t.Fatalf("original Main facts omitted the deletion: %+v", surface.Paths)
	}
	baseline, err := acceptance.BuildS10ExternalBaseline(fixture.root, current.State, nil)
	if err != nil || !containsString(baseline.ChangedPaths, removed) {
		t.Fatalf("S10 must receive the actual deleted path: %+v %v", baseline, err)
	}
}
