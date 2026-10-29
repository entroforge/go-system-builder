package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestMainHarnessProjectionPathsStayNarrow(t *testing.T) {
	hash := strings.Repeat("a", 64)
	allowed := []string{
		".claude/assignment-context/" + hash + "/g1/assignment-task-001-build-e4.json",
		".claude/assignment-context/native/" + hash + "/g1/" + strings.Repeat("b", 64) + ".json",
		".claude/assignment-context/roles/" + hash + ".md",
		".claude/assignment-context/schemas/agent-message-" + hash + ".json",
		".claude/workgroups/REQ-001/build/manifest.json",
		".claude/workgroups/REQ-001/TASK-001/manifest.draft.json",
		".claude/workgroups/REQ-001/build/manifest-s6-r4.json",
		".claude/workgroups/REQ-001/build/readback-request.json",
		".claude/workgroups/REQ-001/review/manifest.json",
		".claude/workspace-launch/0355bdca-13f6-400c-af40-23174b3e9bac/attempt-4165801790.log",
		".claude/workspace-launch/0355bdca-13f6-400c-af40-23174b3e9bac/attempt-4165801790.log.json",
		".claude/workspace-launch/loop-REQ-001/g1-assignment-task-001-build-e4.lock",
		".claude/worktree-reminders/" + hash + ".txt",
		".worktrees/assignment-task-001-build-g1-e4",
	}
	for _, path := range allowed {
		if !mainHarnessProjectionPath(path) {
			t.Errorf("mainHarnessProjectionPath(%q) = false", path)
		}
	}
	rejected := []string{
		".claude/assignment-context/" + hash + "/g1/product.json",
		".claude/assignment-context/" + hash + "/g1/e4.json",
		".claude/workgroups/REQ-001/build/notes.txt",
		".claude/workgroups/REQ-001/build/manifest-s6-r0.json",
		".claude/workspace-launch/not-a-uuid/attempt-1.log",
		".claude/workspace-launch/0355bdca-13f6-400c-af40-23174b3e9bac/attempt-latest.log",
		".claude/worktree-reminders/short.txt",
		".worktrees/../product.go",
		".worktrees/assignment-task-001-build-g1-e4/product.go",
	}
	for _, path := range rejected {
		if mainHarnessProjectionPath(path) {
			t.Errorf("mainHarnessProjectionPath(%q) = true", path)
		}
	}
}

func TestPinnedBootstrapProjectionIntegratesAndRestoresOnlyReceiptAssets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	worker := filepath.Join(root, ".worktrees", "assignment-test-g1-e1")
	oldRunner := defaultRunner
	defaultRunner = execRunner{}
	defer func() { defaultRunner = oldRunner }()

	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(path, value string) {
		t.Helper()
		path = filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git(root, "init", "-b", "develop")
	git(root, "config", "user.name", "Integration Test")
	git(root, "config", "user.email", "integration@example.invalid")
	write("product.txt", "base\n")
	write(".claude/agents/backend-builder.md", "source role\n")
	git(root, "add", "product.txt", ".claude/agents/backend-builder.md")
	git(root, "commit", "-m", "base")
	base := git(root, "rev-parse", "HEAD")
	if err := os.MkdirAll(filepath.Dir(worker), 0755); err != nil {
		t.Fatal(err)
	}
	git(root, "worktree", "add", "-b", "feature", worker, base)
	writeWorker := func(rel, value string) {
		t.Helper()
		path := filepath.Join(worker, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeWorker("product.txt", "feature\n")
	git(worker, "add", "product.txt")
	git(worker, "commit", "-m", "feature")

	trackedRel := ".claude/agents/backend-builder.md"
	untrackedRel := ".claude/inputs/assignment-context.json"
	writeWorker(trackedRel, "installed role\n")
	writeWorker(untrackedRel, "frozen context\n")
	trackedDigest := digestText("installed role\n")
	untrackedDigest := digestText("frozen context\n")
	receipt := workspace.BootstrapReceipt{
		Version: 1, RuntimeID: "loop-test", AssignmentID: "assignment-test", Generation: 1,
		Files: map[string]string{trackedRel: trackedDigest, untrackedRel: untrackedDigest},
	}
	receiptBytes, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	receiptBytes = append(receiptBytes, '\n')
	if err := os.WriteFile(filepath.Join(worker, ".claude", "loop-bootstrap.json"), receiptBytes, 0600); err != nil {
		t.Fatal(err)
	}
	common := git(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	registry := &workspace.ExecutionRegistry{
		Version: 1, MainRoot: root, CommonDir: common, Branch: "develop", BoundHead: base,
		Executions: map[string]workspace.Execution{},
	}
	e := workspace.Execution{
		AssignmentID: "assignment-test", RuntimeID: "loop-test", BaselineGeneration: 1,
		Generation: 1, AgentID: "builder", Path: worker, Branch: "feature",
		TargetBranch: "develop", BaseCommit: base, Status: "ready",
		WritePaths: []string{"product.txt"}, BootstrapSHA256: digestBytes(receiptBytes),
	}
	registry.Executions[e.AssignmentID] = e
	state := map[string]any{
		"bound_req":  map[string]any{"workspace": (workspace.Binding{ProjectRoot: root, DevBranch: "develop", ReleaseUpstream: "main", BoundCommit: base}).Map()},
		"runtime_id": "loop-test", "revision": 0, "baseline": map[string]any{"generation": 1},
		"workspace": workspace.Encode(registry),
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), stateBytes, 0600); err != nil {
		t.Fatal(err)
	}
	pointerBytes, err := json.Marshal(map[string]any{
		"control_root": root, "assignment_id": e.AssignmentID,
		"runtime_id": e.RuntimeID, "execution_generation": e.Generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, ".claude", "loop-workspace.json"), pointerBytes, 0600); err != nil {
		t.Fatal(err)
	}
	pointerPath := filepath.Join(worker, ".claude", "loop-workspace.json")
	if err := os.Remove(pointerPath); err != nil {
		t.Fatal(err)
	}
	if clean, err := workerWorktreeClean(ctx, root, worker); err == nil || clean {
		t.Fatalf("pinned bootstrap without its pointer must block: clean=%v err=%v", clean, err)
	}
	if err := os.WriteFile(pointerPath, pointerBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateExecution(ctx, e, false); err != nil {
		t.Fatalf("validate fixture execution: %v", err)
	}
	mainContext := ".claude/assignment-context/" + strings.Repeat("c", 64) + "/g1/assignment-test-e1.json"
	write(mainContext, "{}\n")
	mainDraft := ".claude/workgroups/REQ-001/TASK-001/manifest.draft.json"
	write(mainDraft, "{}\n")
	if clean, err := worktreeClean(ctx, root); err != nil || !clean {
		t.Fatalf("known Main Harness context and S6 manifest draft should be inspect-only: clean=%v err=%v", clean, err)
	}
	git(root, "add", mainContext)
	if clean, err := worktreeClean(ctx, root); err != nil || clean {
		t.Fatalf("staged Harness path must still block: clean=%v err=%v", clean, err)
	}
	git(root, "reset", "--", mainContext)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(mainContext))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(mainDraft))); err != nil {
		t.Fatal(err)
	}
	mainProduct := ".claude/workgroups/REQ-001/build/product.go"
	write(mainProduct, "product\n")
	if clean, err := worktreeClean(ctx, root); err != nil || clean {
		t.Fatalf("unknown Main product path must block: clean=%v err=%v", clean, err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(mainProduct))); err != nil {
		t.Fatal(err)
	}
	rogue := filepath.Join(root, ".worktrees", "assignment-unregistered")
	if err := os.MkdirAll(rogue, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rogue, "product.txt"), []byte("keep\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if clean, err := worktreeClean(ctx, root); err != nil || clean {
		t.Fatalf("unregistered same-name directory must block: clean=%v err=%v", clean, err)
	}
	if err := os.RemoveAll(rogue); err != nil {
		t.Fatal(err)
	}
	if clean, err := worktreeClean(ctx, worker); err != nil || !clean {
		t.Fatalf("pinned bootstrap files should be projected: clean=%v err=%v", clean, err)
	}
	writeWorker(".claude/workgroups/REQ-001/build/manifest.json", "unexpected Worker file\n")
	if clean, err := workerWorktreeClean(ctx, root, worker); err != nil || clean {
		t.Fatalf("Main-only Harness path must remain Worker dirt: clean=%v err=%v", clean, err)
	}
	if err := os.RemoveAll(filepath.Join(worker, ".claude", "workgroups")); err != nil {
		t.Fatal(err)
	}
	writeWorker("product-dirty.txt", "retain me\n")
	if clean, err := worktreeClean(ctx, worker); err != nil || clean {
		t.Fatalf("product dirt must still block: clean=%v err=%v", clean, err)
	}
	if err := os.Remove(filepath.Join(worker, "product-dirty.txt")); err != nil {
		t.Fatal(err)
	}

	assignment := hookctx.AssignmentContext{
		AssignmentID: e.AssignmentID, TaskID: "TASK-TEST", WorktreePath: worker,
		Branch: e.Branch, TargetBranch: e.TargetBranch, WritePaths: e.WritePaths,
	}
	inspection, err := Inspect(ctx, InspectRequest{Root: root, RuntimeID: e.RuntimeID, Assignment: assignment, TargetBranch: e.TargetBranch, BaselineGeneration: 1}, InspectConfig{SkipCompletionCheck: true, RequiredChecks: []string{"integration-check"}, CheckRunner: func(context.Context, string, string) error { return nil }})
	if err != nil || !inspection.Ready {
		t.Fatalf("inspect pinned Worker: ready=%v blockers=%v err=%v", inspection.Ready, inspection.Blockers, err)
	}
	checkpointPath := filepath.Join(t.TempDir(), "integration-checkpoint.json")
	cfg := IntegrateConfig{
		Root: root, GitRoot: root, RuntimeID: e.RuntimeID, CheckpointDir: checkpointPath,
		RequiredChecks: []string{"integration-check"},
		CheckRunner:    func(context.Context, string, string) error { return nil },
	}
	// Installed files may be ignored by the project. Their exact original
	// Main-pinned receipt remains the proof; no .claude directory exemption.
	exclude := git(worker, "rev-parse", "--git-path", "info/exclude")
	excluded, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, append(excluded, []byte("\n.claude/inputs/assignment-context.json\n.claude/loop-bootstrap.json\n.claude/loop-workspace.json\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	ignoredProjection, err := workerBootstrapProjectionAt(ctx, worker, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := guardIgnoredCleanup(ctx, worker, ignoredProjection); err != nil {
		t.Fatalf("exact installed ignored assets blocked: %v", err)
	}
	if err := os.WriteFile(exclude, excluded, 0600); err != nil {
		t.Fatal(err)
	}
	failure := &bootstrapCleanupFailureRunner{gitRunner: execRunner{}, failAsset: untrackedRel}
	defaultRunner = failure
	result, err := Integrate(ctx, IntegrateRequest{Inspection: inspection, Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, ErrCleanupPending) || result.Checkpoint.State != StateCleanupPending {
		t.Fatalf("partial restoration did not remain recoverable: checkpoint=%+v err=%v", result.Checkpoint, err)
	}
	if _, err := os.Stat(filepath.Join(worker, ".claude/loop-bootstrap.json")); err != nil {
		t.Fatalf("partial restoration lost its original receipt: %v", err)
	}
	failure.failAsset = ".claude/loop-workspace.json"
	result, err = Integrate(ctx, IntegrateRequest{Inspection: InspectionFromCheckpoint(result.Checkpoint), Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, ErrCleanupPending) || result.Checkpoint.State != StateCleanupPending {
		t.Fatalf("pointer restoration interruption did not remain recoverable: checkpoint=%+v err=%v", result.Checkpoint, err)
	}
	if _, err := os.Stat(filepath.Join(worker, ".claude/loop-bootstrap.json")); !os.IsNotExist(err) {
		t.Fatalf("fixture did not reach the receipt/pointer interruption boundary: %v", err)
	}
	failure.failRemove = true
	result, err = Integrate(ctx, IntegrateRequest{Inspection: InspectionFromCheckpoint(result.Checkpoint), Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, ErrCleanupPending) || result.Checkpoint.State != StateCleanupPending {
		t.Fatalf("Git removal failure did not remain recoverable: checkpoint=%+v err=%v", result.Checkpoint, err)
	}
	result, err = Integrate(ctx, IntegrateRequest{Inspection: InspectionFromCheckpoint(result.Checkpoint), Acknowledge: true, Cleanup: true}, cfg)
	if err != nil || result.Checkpoint.State != StateComplete {
		t.Fatalf("integrate and cleanup: checkpoint=%+v err=%v", result.Checkpoint, err)
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("Worker worktree was not removed: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(trackedRel))); err != nil || string(got) != "source role\n" {
		t.Fatalf("Main bootstrap source was changed: bytes=%q err=%v", got, err)
	}
	if git(root, "show", "develop:product.txt") != "feature" {
		t.Fatal("product source was not merged to Main")
	}
}

type bootstrapCleanupFailureRunner struct {
	gitRunner
	failAsset  string
	failRemove bool
}

func (r *bootstrapCleanupFailureRunner) Run(ctx context.Context, root string, args ...string) (string, error) {
	if len(args) == 4 && args[0] == "ls-files" && args[3] == r.failAsset {
		r.failAsset = ""
		return "", errors.New("simulated interruption during bootstrap restoration")
	}
	if len(args) > 1 && args[0] == "worktree" && args[1] == "remove" && r.failRemove {
		r.failRemove = false
		return "", errors.New("simulated ordinary Git worktree removal failure")
	}
	return r.gitRunner.Run(ctx, root, args...)
}

func digestText(value string) string { return digestBytes([]byte(value)) }

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
