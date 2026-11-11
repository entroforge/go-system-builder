package integration_test

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
)

// Use the real canonical S5/S6/Plan chain and the original diagnostic producer.
// A framework display failure must not itself become new product drift.
func TestAcceptancePlanningDiagnosticDoesNotInvalidateProductBaseline(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003)
	registered := registerCanonicalPlanForPacketTest(t, fixture)
	submission.RecordPlanningDiagnostic(fixture.root, registered.State, "S7 original reception", "Synthetic reception interruption; preserve the original professional answer")
	paths, err := filepath.Glob(filepath.Join(fixture.root, ".claude/diagnostics/planning/*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("original producer did not publish diagnostic: %v %v", paths, err)
	}
}
