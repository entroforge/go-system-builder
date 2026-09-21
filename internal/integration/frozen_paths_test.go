package integration

import (
	"context"
	"errors"
	"testing"
)

func TestIntegrateRechecksFrozenPathsBeforeMerge(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()
	f.fr.fileContents["feature-commit:models/order.json"] = "changed"
	inspection := f.readyInspection()
	inspection.LockedPaths = []string{"models/order.json"}
	result, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: inspection,
	}, IntegrateConfig{Root: f.root})
	if !errors.Is(err, ErrLockedArtifact) {
		t.Fatalf("expected frozen-path rejection at merge boundary, got %v", err)
	}
	if result.Checkpoint.MergeCommit != "" || result.Checkpoint.State != StatePreserved {
		t.Fatalf("expected preserved checkpoint without merge: %+v", result.Checkpoint)
	}
	if got := f.fr.branchHeads["develop"]; got != "base-commit" {
		t.Fatalf("frozen-path rejection advanced target to %s", got)
	}
}
