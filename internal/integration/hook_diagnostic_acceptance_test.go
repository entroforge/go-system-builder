package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// Use the real canonical S5/S6/Plan chain and the original diagnostic producer.
// A framework display failure must not itself become new product drift.
func TestAcceptancePlanningDiagnosticDoesNotInvalidateProductBaseline(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003)
	registered := registerCanonicalPlanForPacketTest(t, fixture)
	if err := review.VerifyCurrentProductBaseline(fixture.root, registered.State, t.Context()); err != nil {
		t.Fatalf("baseline was invalid before diagnostic publication: %v", err)
	}
	submission.RecordPlanningDiagnostic(fixture.root, registered.State, "S7 original reception", "Synthetic reception interruption; preserve the original professional answer")
	paths, err := filepath.Glob(filepath.Join(fixture.root, ".claude/diagnostics/planning/*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("original producer did not publish diagnostic: %v %v", paths, err)
	}
	if err := review.VerifyCurrentProductBaseline(fixture.root, registered.State, t.Context()); err != nil {
		t.Errorf("framework diagnostic became product drift without any product edit: %v", err)
	}
	// The storage directory is not a blanket exemption for arbitrary neighbors.
	unknown := ".claude/diagnostics/planning/unknown-product.go"
	if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(unknown)), []byte("package unexpected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := review.VerifyCurrentProductBaseline(fixture.root, registered.State, t.Context()); err == nil || !strings.Contains(err.Error(), unknown) {
		t.Fatalf("unknown neighbor escaped original product-source checks: %v", err)
	}
}
