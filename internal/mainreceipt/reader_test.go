package mainreceipt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

type readerFixture struct {
	root       string
	input      Input
	checkpoint checkpoint
	profile    checkexec.Profile
	receipt    string
	log        string
	mergeBase  string
	targetHead string
	sourceHead string
	mergeHead  string
	testedHead string
	finalHead  string
}

func TestVerifyMapsAuthorizedOldAbsoluteRefsIntoCurrentAuthority(t *testing.T) {
	f := newReaderFixture(t)
	cp := f.checkpoint
	oldMain := filepath.Join(filepath.Dir(f.root), "relocated-old-main")
	checksRel := filepath.ToSlash(filepath.Join(filepath.Dir(assignmentrecord.IntegrationCheckpointPath("", f.input.Record.Reference())), "checks"))
	currentReceipt, err := os.ReadFile(f.receipt)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(currentReceipt, &raw); err != nil {
		t.Fatal(err)
	}
	oldAttempt := filepath.Join(oldMain, filepath.FromSlash(checksRel), filepath.Base(filepath.Dir(f.receipt)))
	if err := os.MkdirAll(oldAttempt, 0o700); err != nil {
		t.Fatal(err)
	}
	oldLog := filepath.Join(oldAttempt, "output.log")
	logBytes, err := os.ReadFile(f.log)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldLog, logBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	oldReceipt := filepath.Join(oldAttempt, "receipt.json")
	raw["cwd"], raw["output"] = oldMain, oldLog
	sealed, err := checkexec.SealReceipt(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.receipt, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(oldMain); err != nil {
		t.Fatal(err)
	}
	fact := &cp.CanonicalMainFacts.Checks[0]
	fact.ReceiptRef = oldReceipt
	fact.LogRef = oldLog
	cp.CanonicalMainFacts.MainRoot = oldMain
	cp.CheckReceipts = []string{oldReceipt}
	cp.PreviousMainRoots = []string{oldMain}
	cp.ToolCheckReceipts[0].Path = oldReceipt
	cp.ToolCheckReceipts[0].Checkout = oldMain
	f.input.AuthorizedMainRoots = []string{f.root, oldMain}
	writeCheckpointFixture(t, f, cp)
	proof, err := Verify(context.Background(), f.input)
	if err != nil {
		t.Fatalf("Verify() after explicit Main-root relocation = %v", err)
	}
	if proof.MainFacts().MainRoot != oldMain || len(proof.ChangedFiles()) != 3 {
		t.Fatalf("relocated proof lost source root or contribution: %+v", proof.MainFacts())
	}
}

func newReaderFixture(t *testing.T) readerFixture {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "checkout", "-b", "main")
	runGit(t, root, "config", "user.email", "mainreceipt@example.test")
	runGit(t, root, "config", "user.name", "Main Receipt Test")
	writeRepoFile(t, root, "internal/keep.go", []byte("package sample\n\nconst Value = \"base\"\n"))
	writeRepoFile(t, root, "internal/retired.go", []byte("package sample\n\nconst Retired = true\n"))
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", "base")
	mergeBase := gitText(t, root, "rev-parse", "HEAD")

	runGit(t, root, "checkout", "-b", "worker")
	writeRepoFile(t, root, "internal/keep.go", []byte("package sample\n\nconst Value = \"worker\"\n"))
	writeRepoFile(t, root, "internal/new line\n.go", []byte("package sample\n\nconst NewlinePath = true\n"))
	if err := os.Remove(filepath.Join(root, "internal/retired.go")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", "worker contribution")
	sourceHead := gitText(t, root, "rev-parse", "HEAD")

	runGit(t, root, "checkout", "main")
	writeRepoFile(t, root, "internal/target.go", []byte("package sample\n\nconst Target = true\n"))
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", "target change")
	targetHead := gitText(t, root, "rev-parse", "HEAD")
	runGit(t, root, "merge", "--no-ff", "worker", "-m", "non squash integration")
	mergeHead := gitText(t, root, "rev-parse", "HEAD")
	writeRepoFile(t, root, "internal/keep.go", []byte("package sample\n\nconst Value = \"tested later\"\n"))
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", "main recheck commit")
	testedHead := gitText(t, root, "rev-parse", "HEAD")
	writeRepoFile(t, root, "internal/final.go", []byte("package sample\n\nconst Final = true\n"))
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", "later pinned target commit")
	finalHead := gitText(t, root, "rev-parse", "HEAD")

	profile := checkexec.Profile{
		ID: "CP-MAIN-TEST", Version: "v1", CheckIndex: 0, Command: "true", Location: "main",
		TimeoutSeconds: 30, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{},
		Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree",
		SideEffects: "none", ReusePolicy: "never",
	}
	record := assignmentrecord.Record{
		SchemaVersion: assignmentrecord.AssignmentRecordSchema, Contract: assignmentrecord.AssignmentRecordContract,
		RuntimeID: "loop-mainreceipt-test", BaselineGeneration: 1, RegistrationRevision: 7,
		AssignmentID: "assignment-mainreceipt-test", Revision: 1,
		Task:            assignmentrecord.ContextSource{Kind: "task", Ref: "docs/dev/tasks/TASK-mainreceipt.md", ID: "TASK-mainreceipt", Version: "v1"},
		Manifest:        assignmentrecord.ContextSource{Kind: "manifest", Ref: "docs/workgroups/manifest.json", ID: "team-manifest-mainreceipt", Version: "v1"},
		AgentDefinition: assignmentrecord.ContextSource{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder"},
		TeamID:          "team-mainreceipt", ResponsibilityID: "BUILD-WORK-PACKAGE", OwnerAgentID: "agent-mainreceipt",
		RoleFamily: "backend-builder", DispatchMode: "plan_checkpoint",
		ReadPaths: []string{}, SkillRefs: []string{"backend-engineering"}, DependsOn: []string{}, DoneWhen: []string{"the original task behavior is implemented"},
		RequiredChecks: []string{profile.Command}, CheckProfiles: []checkexec.Profile{profile},
		IntegrationCheckMode: "post_merge", ClosingContract: &assignmentrecord.ClosingContract{Contract: assignmentrecord.ClosingContractVersion, WorkerChecks: []assignmentrecord.WorkerCheck{}},
		ScopeBasis:          assignmentrecord.ScopeBasis{ManifestScope: []string{"internal/"}, ManifestWritePaths: []string{"internal/"}, ManifestOutputPaths: []string{}, TaskWritePaths: []string{"internal/"}},
		EffectiveWritePaths: []string{"internal"},
		ContextSources: []assignmentrecord.ContextSource{
			{Kind: "task", Ref: "docs/dev/tasks/TASK-mainreceipt.md", ID: "TASK-mainreceipt", Version: "v1"},
			{Kind: "manifest", Ref: "docs/workgroups/manifest.json", ID: "team-manifest-mainreceipt", Version: "v1"},
			{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder"},
			{Kind: "authorized_skill", Ref: ".claude/skills/backend-engineering/SKILL.md", ID: "backend-engineering", Version: "v1"},
			{Kind: "check_profile", Ref: "docs/workgroups/manifest.json#assignments/assignment-mainreceipt-test/check_profiles/CP-MAIN-TEST", ID: profile.ID, Version: profile.Version},
		},
	}
	sort.Slice(record.ContextSources, func(i, j int) bool {
		left, right := record.ContextSources[i], record.ContextSources[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Ref != right.Ref {
			return left.Ref < right.Ref
		}
		return left.ID < right.ID
	})
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		t.Fatalf("test Assignment Record is invalid: %v", err)
	}
	resultRef := ".claude/evidence/loop-mainreceipt-test/g1/assignments/agent-mainreceipt/result-1.json"
	accepted := assignmentresult.AcceptedResult{
		Conclusion: "submitted", AssignmentRecord: record.Reference(),
		ResultRevision: 1, CreatedAt: "2026-10-14T00:00:00Z",
	}
	factsPaths := gitNameOnlyPaths(t, root, mergeBase, sourceHead)
	if err := assignmentresult.ValidateChangedPaths(factsPaths); err != nil {
		t.Fatal(err)
	}
	facts := assignmentresult.MainFacts{
		Binding: assignmentresult.ResultBinding{
			AssignmentRecord: record.Reference(), ResultPath: resultRef, ResultRevision: accepted.ResultRevision,
		},
		MainRoot: root, TargetBranch: "main", ExecutionGeneration: 0, ChangedPaths: factsPaths,
	}
	checksDir := filepath.Join(root, ".claude/evidence/loop-mainreceipt-test/g1/worktree/assignment-mainreceipt-test/checks")
	checkAttemptDir := filepath.Join(checksDir, "loop-integration-check-mainreceipt")
	if err := os.MkdirAll(checkAttemptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(checkAttemptDir, "receipt.json")
	logPath := filepath.Join(checkAttemptDir, "output.log")
	logBytes := []byte("Main required check: true\n")
	if err := os.WriteFile(logPath, logBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	binding := checkexec.Binding{RuntimeID: record.RuntimeID, Generation: record.BaselineGeneration, AssignmentID: record.AssignmentID, Owner: "Main", ExecutionGeneration: 0, Responsibility: "main_required_integration"}
	observation := checkexec.Observation{
		ProfileID: profile.ID, ProfileVersion: profile.Version,
		Environment:       []checkexec.EnvFact{},
		VerificationScope: "unit fixture captures the original Main receipt shape", ReuseEligible: false,
	}
	code := 0
	rawReceipt := map[string]any{
		"version": 2, "command": profile.Command, "cwd": root, "head": testedHead,
		"head_before": testedHead, "head_after": testedHead, "status": "pass", "exit_code": code,
		"output": logPath, "check_binding": binding, "profile_observation": observation,
	}
	sealed, err := checkexec.SealReceipt(rawReceipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	fact := assignmentresult.MainCheckFact{
		CheckIndex: 0, Command: profile.Command, Status: "pass", ReceiptRef: receiptPath,
		LogRef: logPath,
	}
	facts.Checks = []assignmentresult.MainCheckFact{fact}
	toolReceipt := checkexec.ToolReceipt{
		Path:     receiptPath,
		Checkout: root, Command: profile.Command, Binding: binding,
	}
	cp := checkpoint{
		TaskID: record.Task.ID, AssignmentID: record.AssignmentID, Generation: record.BaselineGeneration,
		State: "verified", VerifiedAt: "2026-10-14T00:05:00Z",
		ReportPath: resultRef, TargetBranch: "main", CheckReceipts: []string{receiptPath},
		ToolCheckReceipts: []checkexec.ToolReceipt{toolReceipt}, CanonicalMainFacts: &facts, Revision: 3,
	}
	writeCheckpointFixture(t, readerFixture{root: root, input: Input{Record: record}}, cp)
	return readerFixture{
		root: root, input: Input{
			AuthorityRoot: root, Record: record, Accepted: accepted, ResultRef: resultRef,
			ExecutionGeneration: 0, AuthorizedMainRoots: []string{root},
			ExpectedTargetBranch: "main",
		}, checkpoint: cp, profile: profile, receipt: receiptPath, log: logPath,
		mergeBase: mergeBase, targetHead: targetHead, sourceHead: sourceHead,
		mergeHead: mergeHead, testedHead: testedHead, finalHead: finalHead,
	}
}

func writeCheckpointFixture(t *testing.T, f readerFixture, cp checkpoint) {
	t.Helper()
	root := f.root
	if root == "" {
		root = f.input.AuthorityRoot
	}
	path := assignmentrecord.IntegrationCheckpointPath(root, f.input.Record.Reference())
	if f.input.Record.AssignmentID == "" {
		t.Fatalf("checkpoint fixture writer requires its Record")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRepoFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, data)
	}
	return strings.TrimSpace(string(data))
}

func gitText(t *testing.T, root string, args ...string) string {
	t.Helper()
	return runGit(t, root, args...)
}

func gitNameOnlyPaths(t *testing.T, root, base, source string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "diff", "--no-renames", "--name-only", "-z", base, source, "--")
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, part := range strings.Split(string(data), "\x00") {
		if part != "" {
			paths = append(paths, part)
		}
	}
	sort.Strings(paths)
	return paths
}

func TestCheckpointDirectoryIsRevisionSpecific(t *testing.T) {
	for _, revision := range []int{1, 2, 10} {
		ref := assignmentrecord.RecordReference{RuntimeID: "loop-mainreceipt", BaselineGeneration: 2, AssignmentID: "assignment-mainreceipt", Revision: revision}
		got := assignmentrecord.IntegrationCheckpointPath("/repo", ref)
		wantRevision := ""
		if revision > 1 {
			wantRevision = fmt.Sprintf("/a%d", revision)
		}
		if !strings.Contains(got, "/worktree/assignment-mainreceipt"+wantRevision+"/checkpoint.json") {
			t.Fatalf("revision %d selected wrong checkpoint slot: %s", revision, got)
		}
	}
}
