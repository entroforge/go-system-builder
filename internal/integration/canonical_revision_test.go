package integration

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestCanonicalCheckpointRejectsPredecessorFactsAndUnboundEarlyRevision(t *testing.T) {
	first := revisionBindingTestRecord()
	second := first
	second.Revision = 2
	oldFacts := &assignmentresult.MainFacts{Binding: assignmentresult.ResultBinding{
		AssignmentRecord: first.Reference(),
	}}
	if err := ValidateCheckpointRecordBinding(Checkpoint{CanonicalMainFacts: oldFacts}, second); err == nil {
		t.Fatal("r2 accepted predecessor r1 Main facts")
	}
	if err := ValidateCheckpointRecordBinding(Checkpoint{}, second); err == nil {
		t.Fatal("r2 accepted an early checkpoint without its current Record binding")
	}
	ref := second.Reference()
	if err := ValidateCheckpointRecordBinding(Checkpoint{AssignmentRecord: &ref}, second); err != nil {
		t.Fatalf("current r2 early checkpoint rejected: %v", err)
	}
	legacyEarly := Checkpoint{}
	if err := ValidateCheckpointRecordBinding(legacyEarly, first); err == nil {
		t.Fatal("unbound r1 checkpoint was implicitly migrated into canonical Record authority")
	}
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
	manifest := assignmentrecord.ContextSource{Kind: "manifest", Ref: "docs/workgroups/manifest.json", ID: "team-manifest", Version: "v1.0.0"}
	task := assignmentrecord.ContextSource{Kind: "task", Ref: "docs/dev/tasks/TASK-001-01.md", ID: "TASK-001-01", Version: "v1.0.0"}
	role := assignmentrecord.ContextSource{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder"}
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
