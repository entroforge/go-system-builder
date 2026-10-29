package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestNativePrivateAreaInspectAndVerifiedCleanup(t *testing.T) {
	ctx := context.Background()
	root, worker, baseInspection, cfg, git := newRecoveryGitRepo(t)
	area := ".loop-submissions/native-s9/packet-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	put := func(rel, body string) {
		t.Helper()
		path := filepath.Join(worker, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, result := area+"/plan.json", area+"/result.json"
	log := area + "/logs/referenced.json"
	put(plan, "original plan")
	put(result, "original result")
	put(log, "captured evidence")
	put(area+"/logs/unreferenced.txt", "private scratch")
	put(area+"/nested/no-extension", "other private scratch")
	put(area+"/ignored/any.bin", "ignored private scratch")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside owner data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(worker, area, "nested", "link")); err != nil {
		t.Fatal(err)
	}
	exclude := git(worker, "rev-parse", "--git-path", "info/exclude")
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\n/" + area + "/ignored/\n")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	private := map[string]string{plan: workpacket.Digest([]byte("original plan")), result: workpacket.Digest([]byte("original result"))}
	logs := map[string]string{log: workpacket.Digest([]byte("captured evidence"))}
	inspectCfg := InspectConfig{ValidateDelivery: func(context.Context, Inspection) error { return nil }, WorkerPrivateArea: area,
		WorkerPrivateBaseCommit: baseInspection.MergeBase, WorkerPrivateWritePaths: []string{"change"}, WorkerPrivateEvidence: private, WorkerLogEvidence: logs}
	request := InspectRequest{Root: root, RuntimeID: "review", TargetBranch: "test2", BaselineGeneration: 1,
		Assignment: hookctx.AssignmentContext{AssignmentID: "review", WorktreePath: worker, Branch: "feature", TargetBranch: "test2", WritePaths: []string{"change"}}}

	put(log, "changed captured evidence")
	bad, err := Inspect(ctx, request, inspectCfg)
	if err != nil || bad.Ready {
		t.Fatalf("changed captured evidence became scratch: ready=%t blockers=%v err=%v", bad.Ready, bad.Blockers, err)
	}
	put(log, "captured evidence")
	inspection, err := Inspect(ctx, request, inspectCfg)
	if err != nil || !inspection.Ready {
		t.Fatalf("private scratch blocked original Inspect: %+v %v", inspection, err)
	}
	cfg.WorkerPrivateArea, cfg.WorkerPrivateBaseCommit = area, baseInspection.MergeBase
	cfg.WorkerPrivateWritePaths, cfg.WorkerPrivateEvidence, cfg.WorkerLogEvidence = []string{"change"}, private, logs
	cfg.RequiredChecks = []string{"test -f change"}
	cfg.CheckRunner = CommandCheckRunner
	verified, err := Integrate(ctx, IntegrateRequest{Inspection: inspection}, cfg)
	if err != nil || verified.Checkpoint.State != StateVerified {
		t.Fatalf("original integration did not verify: %+v %v", verified, err)
	}
	if _, err := os.Stat(filepath.Join(worker, area, "logs/unreferenced.txt")); err != nil {
		t.Fatalf("Inspect/verify removed scratch before cleanup authorization: %v", err)
	}
	put("unrelated-private.txt", "preserve")
	blocked, err := Integrate(ctx, IntegrateRequest{Inspection: InspectionFromCheckpoint(verified.Checkpoint), Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, ErrCleanupPending) || blocked.Checkpoint.State != StateCleanupPending {
		t.Fatalf("unrelated file was not preserved: %+v %v", blocked, err)
	}
	if _, err := os.Stat(filepath.Join(worker, area, "logs/unreferenced.txt")); err != nil {
		t.Fatalf("blocked cleanup removed private source: %v", err)
	}
	if err := os.Remove(filepath.Join(worker, "unrelated-private.txt")); err != nil {
		t.Fatal(err)
	}
	completed, err := Integrate(ctx, IntegrateRequest{Inspection: InspectionFromCheckpoint(blocked.Checkpoint), Acknowledge: true, Cleanup: true}, cfg)
	if err != nil || completed.Checkpoint.State != StateComplete {
		t.Fatalf("authorized scratch cleanup failed: %+v %v", completed, err)
	}
	if _, err := os.Lstat(worker); !os.IsNotExist(err) {
		t.Fatalf("original Worker was not removed: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside owner data" {
		t.Fatalf("scratch symlink target changed: %q %v", data, err)
	}
}

func TestNativePrivateAreaKeepsProductAndOtherResponsibility(t *testing.T) {
	ctx := context.Background()
	_, worker, in, _, git := newRecoveryGitRepo(t)
	area := ".loop-submissions/native-s9/packet-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	put := func(rel string) {
		t.Helper()
		path := filepath.Join(worker, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(rel), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scratch, scoped, baseline, staged := area+"/x", area+"/logs/scoped.txt", area+"/logs/baseline.txt", area+"/logs/staged.txt"
	for _, path := range []string{scratch, scoped, baseline, staged} {
		put(path)
	}
	git(worker, "add", staged)
	projection, err := ObserveWorkerPrivateArea(ctx, worker, WorkerPrivateAreaSpec{Path: area, BaseCommit: in.MergeBase, WritePaths: []string{scoped}, BaselinePaths: []string{baseline}})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := projection.Allows(scratch); err != nil || !ok {
		t.Fatalf("ordinary scratch not classified: %t %v", ok, err)
	}
	for _, path := range []string{scoped, baseline, staged, ".loop-submissions/native-s9/packet-other/x", area + "/../escape"} {
		if ok, err := projection.Allows(path); ok || err != nil {
			t.Fatalf("protected or unrelated path %q classified as scratch: %t %v", path, ok, err)
		}
	}
	other := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(worker, area, "redirect")); err != nil {
		t.Fatal(err)
	}
	if ok, err := projection.Allows(area + "/redirect/nested"); ok || err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink ancestor was followed: %t %v", ok, err)
	}
}
