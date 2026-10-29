package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestCanonicalCheckpointRejectsPredecessorFactsAndUnboundEarlyRevision(t *testing.T) {
	first := revisionBindingTestRecord()
	firstSHA, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Revision = 2
	secondSHA, err := second.Digest()
	if err != nil {
		t.Fatal(err)
	}
	oldFacts := &assignmentresult.MainFacts{Binding: assignmentresult.ResultBinding{
		AssignmentRecord: first.Reference(), AssignmentRecordSHA256: firstSHA,
	}}
	if err := ValidateCheckpointRecordBinding(Checkpoint{CanonicalMainFacts: oldFacts}, second); err == nil {
		t.Fatal("r2 accepted predecessor r1 Main facts")
	}
	if err := ValidateCheckpointRecordBinding(Checkpoint{}, second); err == nil {
		t.Fatal("r2 accepted an early checkpoint without its current Record binding")
	}
	ref := second.Reference()
	if err := ValidateCheckpointRecordBinding(Checkpoint{AssignmentRecord: &ref, AssignmentRecordSHA256: secondSHA}, second); err != nil {
		t.Fatalf("current r2 early checkpoint rejected: %v", err)
	}
	legacyEarly := Checkpoint{}
	if err := ValidateCheckpointRecordBinding(legacyEarly, first); err == nil {
		t.Fatal("unbound r1 checkpoint was implicitly migrated into canonical Record authority")
	}
}

func TestRuntimeProjectionRecognizesOnlyRevisionCheckpointSlots(t *testing.T) {
	for _, path := range []string{
		".claude/evidence/loop-test/g1/worktree/assignment-test/checkpoint.json",
		".claude/evidence/loop-test/g1/worktree/assignment-test/a2/checkpoint.json",
		".claude/evidence/loop-test/g1/worktree/assignment-test/a10/checkpoint.json",
		".claude/runtime-checkpoints/index.json",
		".claude/runtime-checkpoints/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/events.jsonl",
		".claude/runtime-checkpoints/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/manifest.json",
		".claude/runtime-checkpoints/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/state.json",
	} {
		if !runtimeProjection(path) {
			t.Errorf("runtimeProjection(%q) = false, want revision checkpoint allowlisted", path)
		}
	}
	for _, path := range []string{
		".claude/evidence/loop-test/g1/worktree/assignment-test/a0/checkpoint.json",
		".claude/evidence/loop-test/g1/worktree/assignment-test/a1/checkpoint.json",
		".claude/evidence/loop-test/g1/worktree/assignment-test/a02/checkpoint.json",
		".claude/evidence/loop-test/g1/worktree/assignment-test/a2/product.go",
		".claude/evidence/loop-test/g1/worktree/assignment-test/a2/other.json",
		".claude/runtime-checkpoints/ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789/state.json",
		".claude/runtime-checkpoints/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/product.go",
		".claude/runtime-checkpoints/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/nested/state.json",
	} {
		if runtimeProjection(path) {
			t.Errorf("runtimeProjection(%q) = true, want product/invalid path rejected", path)
		}
	}
}

func TestCanonicalSourcePriorMergeRequiresPredecessorFactsForReusedHistory(t *testing.T) {
	root := newRevisionMergeFixture(t)
	gitRevisionTest(t, root, "checkout", "worker")
	if err := os.WriteFile(filepath.Join(root, "worker.txt"), []byte("second revision change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, root, "add", "worker.txt")
	gitRevisionTest(t, root, "commit", "-m", "second worker change")
	sourceHead := gitRevisionTest(t, root, "rev-parse", "worker")
	if found, err := canonicalSourceHasPriorMainMerge(context.Background(), root, "main", sourceHead); err != nil || !found {
		t.Fatalf("prior source merge = %t, err %v; want predecessor Main facts validation", found, err)
	}
}

func TestCanonicalSourceStartedFromCurrentMainDoesNotLookLikeReusedPredecessorSource(t *testing.T) {
	root := newRevisionMergeFixture(t)
	gitRevisionTest(t, root, "checkout", "main")
	gitRevisionTest(t, root, "checkout", "-b", "fresh-worker")
	if err := os.WriteFile(filepath.Join(root, "fresh.txt"), []byte("fresh branch change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, root, "add", "fresh.txt")
	gitRevisionTest(t, root, "commit", "-m", "fresh worker change")
	sourceHead := gitRevisionTest(t, root, "rev-parse", "fresh-worker")
	if found, err := canonicalSourceHasPriorMainMerge(context.Background(), root, "main", sourceHead); err != nil || found {
		t.Fatalf("fresh Main-derived source prior merge = %t, err %v; want no predecessor source merge", found, err)
	}
}

func newRevisionMergeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitRevisionTest(t, root, "init", "-b", "main")
	gitRevisionTest(t, root, "config", "user.name", "S6 revision test")
	gitRevisionTest(t, root, "config", "user.email", "s6-revision@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, root, "add", "base.txt")
	gitRevisionTest(t, root, "commit", "-m", "base")
	gitRevisionTest(t, root, "checkout", "-b", "worker")
	if err := os.WriteFile(filepath.Join(root, "worker.txt"), []byte("first worker change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, root, "add", "worker.txt")
	gitRevisionTest(t, root, "commit", "-m", "first worker change")
	gitRevisionTest(t, root, "checkout", "main")
	gitRevisionTest(t, root, "merge", "--no-ff", "worker", "-m", "integrate first worker change")
	return root
}

func gitRevisionTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func revisionBindingTestRecord() assignmentrecord.Record {
	hashA, hashB, hashC := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	manifest := assignmentrecord.ContextSource{Kind: "manifest", Ref: "docs/workgroups/manifest.json", ID: "team-manifest", Version: "v1.0.0", SHA256: hashA}
	task := assignmentrecord.ContextSource{Kind: "task", Ref: "docs/dev/tasks/TASK-001-01.md", ID: "TASK-001-01", Version: "v1.0.0", SHA256: hashB}
	role := assignmentrecord.ContextSource{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder", SHA256: hashC}
	scope := assignmentrecord.ScopeBasis{
		ManifestScope: []string{"src/pkg"}, ManifestWritePaths: []string{"src/pkg"},
		ManifestOutputPaths: []string{}, TaskWritePaths: []string{"src/pkg"},
	}
	effective, _ := assignmentrecord.EffectiveScope(scope)
	return assignmentrecord.Record{
		SchemaVersion: assignmentrecord.AssignmentRecordSchema, Contract: assignmentrecord.AssignmentRecordContract,
		RuntimeID: "loop-revision-binding", BaselineGeneration: 1, RegistrationRevision: 2,
		AssignmentID: "assignment-revision-binding", Revision: 1, Task: task, Manifest: manifest,
		AgentDefinition: role, TeamID: "workgroup-revision-binding", ResponsibilityID: "BUILD-WORK-PACKAGE",
		OwnerAgentID: "agent-revision-binding", RoleFamily: "backend-builder", DispatchMode: "plan_checkpoint",
		ReadPaths: []string{}, SkillRefs: []string{"backend-engineering"}, DependsOn: []string{}, DoneWhen: []string{"scoped output exists"},
		RequiredChecks: []string{}, CheckProfiles: []checkexec.Profile{}, ScopeBasis: scope,
		EffectiveWritePaths: effective, ContextSources: []assignmentrecord.ContextSource{role, manifest, task},
	}
}
