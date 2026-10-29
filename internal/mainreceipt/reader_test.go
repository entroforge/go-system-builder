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

func TestVerifyReadsFullMainReceiptAndGitContribution(t *testing.T) {
	f := newReaderFixture(t)
	proof, err := Verify(context.Background(), f.input)
	if err != nil {
		t.Fatalf("Verify() = %v", err)
	}
	if proof.RecordReference() != f.input.Record.Reference() || proof.ResultBinding() != f.checkpoint.CanonicalMainFacts.Binding || proof.CheckpointSHA256() == "" || proof.FinalTargetHead() != f.finalHead {
		t.Fatalf("proof lost exact source binding: %+v", proof)
	}
	if proof.MainFacts().TestedHead != f.testedHead {
		t.Fatalf("TestedHead = %q, want descendant Main head %q", proof.MainFacts().TestedHead, f.testedHead)
	}
	files := proof.ChangedFiles()
	if len(files) != 3 {
		t.Fatalf("ChangedFiles() returned %d paths, want full three-path diff: %+v", len(files), files)
	}
	byPath := map[string]ChangedFile{}
	for _, file := range files {
		byPath[file.Path] = file
	}
	deleted, ok := byPath["internal/retired.go"]
	if !ok || deleted.Status != "D" || deleted.BaseBlobSHA256 == "" || deleted.TargetBlobSHA256 == "" || deleted.SourceBlobSHA256 != "" || deleted.MergeBlobSHA256 != "" || deleted.FinalBlobSHA256 != "" {
		t.Fatalf("deleted-file Git preimage/final tree facts are incomplete: %+v", deleted)
	}
	newlinePath := "internal/new line\n.go"
	added, ok := byPath[newlinePath]
	if !ok || added.Status != "A" || added.SourceBlobSHA256 == "" || added.MergeBlobSHA256 == "" || added.FinalBlobSHA256 == "" {
		t.Fatalf("literal newline path was not preserved from Git: %+v", added)
	}
	modified, ok := byPath["internal/keep.go"]
	if !ok || modified.Status != "M" || modified.BaseBlobSHA256 == "" || modified.SourceBlobSHA256 == "" || modified.TestedBlobSHA256 == "" {
		t.Fatalf("modified-file blob facts are incomplete: %+v", modified)
	}
}

func TestVerifyRejectsUnboundOrTamperedMainSources(t *testing.T) {
	for _, name := range []string{
		"unpinned target", "tampered log", "missing runner receipt", "outside receipt directory",
		"missing tool receipt", "tampered tool receipt", "symlinked check receipt",
		"unauthorized previous root", "different branch", "nonexistent final head", "wrong merge commit",
	} {
		t.Run(name, func(t *testing.T) {
			f := newReaderFixture(t)
			in := f.input
			cp := f.checkpoint
			switch name {
			case "unpinned target":
				in.FinalTargetHead = "main"
			case "tampered log":
				if err := os.WriteFile(f.log, []byte("changed after runner receipt\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing runner receipt":
				cp.CheckReceipts = nil
				writeCheckpointFixture(t, f, cp)
			case "missing tool receipt":
				cp.ToolCheckReceipts = nil
				writeCheckpointFixture(t, f, cp)
			case "tampered tool receipt":
				cp.ToolCheckReceipts[0].SHA256 = strings.Repeat("f", 64)
				writeCheckpointFixture(t, f, cp)
			case "symlinked check receipt":
				if err := os.Remove(f.receipt); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(f.log, f.receipt); err != nil {
					t.Fatal(err)
				}
			case "outside receipt directory":
				outside := filepath.Join(t.TempDir(), "receipt.json")
				cp.CheckReceipts = []string{outside}
				cp.CanonicalMainFacts.Checks[0].ReceiptRef = outside
				writeCheckpointFixture(t, f, cp)
			case "unauthorized previous root":
				cp.PreviousMainRoots = []string{"/outside/previous-main"}
				writeCheckpointFixture(t, f, cp)
			case "different branch":
				in.ExpectedTargetBranch = "release"
			case "nonexistent final head":
				in.FinalTargetHead = strings.Repeat("f", 40)
			case "wrong merge commit":
				cp.MergeCommit = f.finalHead // ordinary descendant, not the recorded two-parent merge
				cp.CanonicalMainFacts.MergeCommit = f.finalHead
				writeCheckpointFixture(t, f, cp)
			}
			if _, err := Verify(context.Background(), in); err == nil {
				t.Fatal("Verify() accepted tampered or unbound Main evidence")
			}
		})
	}
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
	sealed, err := checkexec.SealReceipt(raw, oldLog)
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
	fact.ReceiptRef, fact.ReceiptSHA256 = oldReceipt, checkexec.Hash(sealed)
	fact.LogRef, fact.LogSHA256 = oldLog, checkexec.Hash(logBytes)
	cp.CanonicalMainFacts.MainRoot = oldMain
	cp.CheckReceipts = []string{oldReceipt}
	cp.PreviousMainRoots = []string{oldMain}
	cp.ToolCheckReceipts[0].Path = oldReceipt
	cp.ToolCheckReceipts[0].SHA256 = checkexec.Hash(sealed)
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
		Task:            assignmentrecord.ContextSource{Kind: "task", Ref: "docs/dev/tasks/TASK-mainreceipt.md", ID: "TASK-mainreceipt", Version: "v1", SHA256: strings.Repeat("a", 64)},
		Manifest:        assignmentrecord.ContextSource{Kind: "manifest", Ref: "docs/workgroups/manifest.json", ID: "team-manifest-mainreceipt", Version: "v1", SHA256: strings.Repeat("b", 64)},
		AgentDefinition: assignmentrecord.ContextSource{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder", SHA256: strings.Repeat("c", 64)},
		TeamID:          "team-mainreceipt", ResponsibilityID: "BUILD-WORK-PACKAGE", OwnerAgentID: "agent-mainreceipt",
		RoleFamily: "backend-builder", DispatchMode: "plan_checkpoint",
		ReadPaths: []string{}, SkillRefs: []string{"backend-engineering"}, DependsOn: []string{}, DoneWhen: []string{"the original task behavior is implemented"},
		RequiredChecks: []string{profile.Command}, CheckProfiles: []checkexec.Profile{profile},
		IntegrationCheckMode: "post_merge", ClosingContract: &assignmentrecord.ClosingContract{Contract: assignmentrecord.ClosingContractVersion, WorkerChecks: []assignmentrecord.WorkerCheck{}},
		ScopeBasis:          assignmentrecord.ScopeBasis{ManifestScope: []string{"internal/"}, ManifestWritePaths: []string{"internal/"}, ManifestOutputPaths: []string{}, TaskWritePaths: []string{"internal/"}},
		EffectiveWritePaths: []string{"internal"},
		ContextSources: []assignmentrecord.ContextSource{
			{Kind: "task", Ref: "docs/dev/tasks/TASK-mainreceipt.md", ID: "TASK-mainreceipt", Version: "v1", SHA256: strings.Repeat("a", 64)},
			{Kind: "manifest", Ref: "docs/workgroups/manifest.json", ID: "team-manifest-mainreceipt", Version: "v1", SHA256: strings.Repeat("b", 64)},
			{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder", SHA256: strings.Repeat("c", 64)},
			{Kind: "authorized_skill", Ref: ".claude/skills/backend-engineering/SKILL.md", ID: "backend-engineering", Version: "v1", SHA256: strings.Repeat("e", 64)},
			{Kind: "check_profile", Ref: "docs/workgroups/manifest.json#assignments/assignment-mainreceipt-test/check_profiles/CP-MAIN-TEST", ID: profile.ID, Version: profile.Version, SHA256: profile.Digest()},
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
	if _, err := record.Digest(); err != nil {
		t.Fatalf("test Assignment Record is invalid: %v", err)
	}
	recordSHA, _ := record.Digest()
	resultRef := ".claude/evidence/loop-mainreceipt-test/g1/assignments/agent-mainreceipt/result-1.json"
	resultSHA := checkexec.Hash([]byte("original accepted result bytes"))
	accepted := assignmentresult.AcceptedResult{
		Conclusion: "submitted", AssignmentRecord: record.Reference(), AssignmentRecordSHA256: recordSHA,
		ResultRevision: 1, AnswerDigest: strings.Repeat("d", 64), CreatedAt: "2026-10-14T00:00:00Z",
	}
	factsPaths := gitNameOnlyPaths(t, root, mergeBase, sourceHead)
	pathsSHA, err := assignmentresult.ChangedPathsDigest(factsPaths)
	if err != nil {
		t.Fatal(err)
	}
	facts := assignmentresult.MainFacts{
		Binding: assignmentresult.ResultBinding{
			AssignmentRecord: record.Reference(), AssignmentRecordSHA256: recordSHA,
			ResultPath: resultRef, ResultSHA256: resultSHA,
			ResultRevision: accepted.ResultRevision, AnswerDigest: accepted.AnswerDigest,
		},
		MainRoot: root, SourceHead: sourceHead, MergeBase: mergeBase, TestedHead: testedHead,
		TargetBranch: "main", MergeCommit: mergeHead, ExecutionGeneration: 0,
		ChangedPaths: factsPaths, ChangedPathsSHA256: pathsSHA,
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
		ProfileID: profile.ID, ProfileVersion: profile.Version, ProfileDigest: profile.Digest(),
		SubjectDigest: strings.Repeat("1", 64), RunnerDigest: strings.Repeat("2", 64),
		EnvironmentDigest: strings.Repeat("3", 64), DependencyDigest: strings.Repeat("4", 64),
		FixtureDigest: strings.Repeat("5", 64), Environment: []checkexec.EnvFact{},
		VerificationScope: "unit fixture captures the original Main receipt shape", ReuseEligible: false,
	}
	code := 0
	rawReceipt := map[string]any{
		"version": 2, "command": profile.Command, "cwd": root, "head": testedHead,
		"head_before": testedHead, "head_after": testedHead, "status": "pass", "exit_code": code,
		"output": logPath, "check_binding": binding, "profile_observation": observation,
	}
	sealed, err := checkexec.SealReceipt(rawReceipt, logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	fact := assignmentresult.MainCheckFact{
		CheckIndex: 0, Command: profile.Command, Status: "pass", ReceiptRef: receiptPath,
		ReceiptSHA256: checkexec.Hash(sealed), LogRef: logPath, LogSHA256: checkexec.Hash(logBytes),
		TestedHead: testedHead, ProfileDigest: profile.Digest(),
	}
	facts.Checks = []assignmentresult.MainCheckFact{fact}
	toolReceipt := checkexec.ToolReceipt{
		Path: receiptPath, SHA256: checkexec.Hash(sealed), LogSHA256: checkexec.Hash(logBytes),
		Checkout: root, Command: profile.Command, ProfileDigest: profile.Digest(), Binding: binding,
	}
	cp := checkpoint{
		TaskID: record.Task.ID, AssignmentID: record.AssignmentID, Generation: record.BaselineGeneration,
		State: "verified", VerifiedAt: "2026-10-14T00:05:00Z",
		ReportPath: resultRef, ReportSHA: resultSHA, SourceHead: sourceHead, TargetHead: targetHead,
		MergeBase: mergeBase, TargetBranch: "main", MergeCommit: mergeHead,
		TestedHead: testedHead, CheckReceipts: []string{receiptPath},
		ToolCheckReceipts: []checkexec.ToolReceipt{toolReceipt}, CanonicalMainFacts: &facts, Revision: 3,
	}
	writeCheckpointFixture(t, readerFixture{root: root, input: Input{Record: record}}, cp)
	return readerFixture{
		root: root, input: Input{
			AuthorityRoot: root, Record: record, Accepted: accepted, ResultRef: resultRef,
			ResultSHA256: resultSHA, ExecutionGeneration: 0, AuthorizedMainRoots: []string{root},
			ExpectedTargetBranch: "main", FinalTargetHead: finalHead,
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
