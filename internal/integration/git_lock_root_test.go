package integration

import (
	"context"
	"errors"
	"testing"
)

func TestIntegrationLocksGitCheckoutWhenEvidenceRootDiffers(t *testing.T) {
	gitRoot := t.TempDir()
	_, release, err := LockWorkspace(context.Background(), gitRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = Integrate(context.Background(), IntegrateRequest{}, IntegrateConfig{Root: t.TempDir(), GitRoot: gitRoot})
	if !errors.Is(err, ErrIntegrationBusy) {
		t.Fatalf("did not serialize against actual Git root: %v", err)
	}
}
