package integration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const testCompletionReportRel = ".claude/evidence/loop-REQ-039/g1/assignments/assignment-test/completion.json"

func writeBoundCompletionReport(t *testing.T, root, body string) (string, string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(testCompletionReportRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(body)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(testCompletionReportRel), fmt.Sprintf("%x", sha256.Sum256(data))
}

func bindInspectionReport(insp *Inspection, relPath, sha string) {
	insp.CompletionReportPath = relPath
	insp.CompletionReportSHA256 = sha
}

func integrationResultBindingConfig(f *integrationFixture) IntegrateConfig {
	return IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	}
}

func TestInspectRecordsCompletionReportBinding(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	relPath, wantSHA := writeBoundCompletionReport(t, f.root, `{"message_type":"completion_report","status":"completed"}`)
	insp, err := Inspect(context.Background(), InspectRequest{
		Root:               f.root,
		Assignment:         assignmentContext(f.wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}, InspectConfig{})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !insp.Ready {
		t.Fatalf("expected ready, blockers=%v", insp.Blockers)
	}
	if insp.CompletionReportPath != relPath || insp.CompletionReportSHA256 != wantSHA {
		t.Fatalf("inspect did not bind the exact report: path=%q sha=%q", insp.CompletionReportPath, insp.CompletionReportSHA256)
	}
}

func TestIntegrateReverifiesChangedCompletionReportWithoutMergingAgain(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	relPath, firstSHA := writeBoundCompletionReport(t, f.root, `{"message_type":"completion_report","status":"completed"}`)
	insp := f.readyInspection()
	bindInspectionReport(&insp, relPath, firstSHA)
	checks := 0
	cfg := integrationResultBindingConfig(f)
	cfg.RequiredChecks = []string{"result-check"}
	cfg.CheckRunner = func(context.Context, string, string) error {
		checks++
		return nil
	}

	first, err := Integrate(context.Background(), IntegrateRequest{Inspection: insp}, cfg)
	if err != nil {
		t.Fatalf("first integrate: %v", err)
	}
	if first.Checkpoint.State != StateVerified || first.Checkpoint.CompletionReportSHA256 != firstSHA {
		t.Fatalf("first verification did not persist report identity: %+v", first.Checkpoint)
	}
	mergeCommit := first.Checkpoint.MergeCommit
	branchHead := f.fr.branchHeads["develop"]

	secondBody := `{"message_type":"completion_report","status":"completed","changed_paths":["internal/refreshed.go"]}`
	_, secondSHA := writeBoundCompletionReport(t, f.root, secondBody)
	bindInspectionReport(&insp, relPath, secondSHA)
	second, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
	}, cfg)
	if err != nil {
		t.Fatalf("changed-result reverify: %v", err)
	}
	if second.Checkpoint.State != StateAcknowledged {
		t.Fatalf("expected acknowledged after reverify, got %s", second.Checkpoint.State)
	}
	if second.Checkpoint.MergeCommit != mergeCommit || f.fr.branchHeads["develop"] != branchHead {
		t.Fatalf("changed-result reverify merged again: old=%q new=%q branch=%q", mergeCommit, second.Checkpoint.MergeCommit, f.fr.branchHeads["develop"])
	}
	if second.Checkpoint.CompletionReportSHA256 != secondSHA {
		t.Fatalf("reverify did not replace the bound hash: got %q want %q", second.Checkpoint.CompletionReportSHA256, secondSHA)
	}
	if checks != 2 {
		t.Fatalf("changed Result must rerun checks exactly once: checks=%d", checks)
	}

	third, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
		Cleanup:     true,
	}, cfg)
	if err != nil {
		t.Fatalf("cleanup after reverify: %v", err)
	}
	if third.Checkpoint.State != StateComplete || third.Checkpoint.CompletionReportSHA256 != secondSHA {
		t.Fatalf("cleanup changed verified report identity: %+v", third.Checkpoint)
	}
	if checks != 2 {
		t.Fatalf("ack/cleanup must not rerun checks: checks=%d", checks)
	}
}

func TestIntegrateRejectsMissingBoundCompletionReportWithoutChangingCheckpoint(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	relPath, reportSHA := writeBoundCompletionReport(t, f.root, `{"message_type":"completion_report","status":"completed"}`)
	insp := f.readyInspection()
	bindInspectionReport(&insp, relPath, reportSHA)
	cfg := integrationResultBindingConfig(f)

	first, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
		Cleanup:     true,
	}, cfg)
	if err != nil {
		t.Fatalf("initial integrate: %v", err)
	}
	if first.Checkpoint.State != StateComplete {
		t.Fatalf("expected complete, got %s", first.Checkpoint.State)
	}
	checkpointBefore, err := os.ReadFile(cfg.CheckpointDir)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(relPath))); err != nil {
		t.Fatalf("remove report: %v", err)
	}

	second, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
		Cleanup:     true,
	}, cfg)
	if !errors.Is(err, ErrMissingCompletion) {
		t.Fatalf("missing bound report must fail closed with ErrMissingCompletion: result=%+v err=%v", second, err)
	}
	checkpointAfter, readErr := os.ReadFile(cfg.CheckpointDir)
	if readErr != nil {
		t.Fatalf("read checkpoint after rejection: %v", readErr)
	}
	if string(checkpointAfter) != string(checkpointBefore) {
		t.Fatalf("missing report changed durable checkpoint:\nbefore=%safter=%s", checkpointBefore, checkpointAfter)
	}
}

func TestIntegrateRejectsReportChangedDuringVerification(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	relPath, firstSHA := writeBoundCompletionReport(t, f.root, `{"message_type":"completion_report","status":"completed"}`)
	insp := f.readyInspection()
	bindInspectionReport(&insp, relPath, firstSHA)
	cfg := integrationResultBindingConfig(f)
	cfg.RequiredChecks = []string{"mutating-check"}
	cfg.CheckRunner = func(context.Context, string, string) error {
		_, _ = writeBoundCompletionReport(t, f.root, `{"message_type":"completion_report","status":"completed","mutated":true}`)
		return nil
	}

	result, err := Integrate(context.Background(), IntegrateRequest{Inspection: insp}, cfg)
	if !errors.Is(err, ErrCompletionReportChanged) {
		t.Fatalf("expected report-change rejection, result=%+v err=%v", result, err)
	}
	if result.Checkpoint.State != StatePreserved {
		t.Fatalf("changed report during checks must preserve checkpoint, got %s", result.Checkpoint.State)
	}
	if result.Checkpoint.CompletionReportPath != "" || result.Checkpoint.CompletionReportSHA256 != "" {
		t.Fatalf("failed verification must not stamp a new report identity: %+v", result.Checkpoint)
	}
}

func TestIntegrateReverifyRequiresRecordedMergeOnTarget(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	relPath, firstSHA := writeBoundCompletionReport(t, f.root, `{"message_type":"completion_report","status":"completed"}`)
	insp := f.readyInspection()
	bindInspectionReport(&insp, relPath, firstSHA)
	cfg := integrationResultBindingConfig(f)
	first, err := Integrate(context.Background(), IntegrateRequest{Inspection: insp}, cfg)
	if err != nil {
		t.Fatalf("initial integrate: %v", err)
	}

	_, secondSHA := writeBoundCompletionReport(t, f.root, `{"message_type":"completion_report","status":"completed","changed":true}`)
	bindInspectionReport(&insp, relPath, secondSHA)
	// Simulate the target ref being rewritten after the original merge. The
	// reverify path must refuse to certify checks on an unrelated target
	// history instead of silently trusting the old merge commit.
	f.fr.setBranch("develop", "base-commit")
	result, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
	}, cfg)
	if err == nil || result.Checkpoint.MergeCommit != first.Checkpoint.MergeCommit {
		t.Fatalf("reverify should reject unreachable merge without replacing durable merge: result=%+v err=%v", result, err)
	}
}
