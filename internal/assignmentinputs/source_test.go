package assignmentinputs_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestReadEmbeddedAssignmentSourcesUsesExactBinaryAssets(t *testing.T) {
	root := t.TempDir()
	if _, err := os.Stat(filepath.Join(root, "internal")); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly contains an internal source tree: %v", err)
	}

	for _, name := range []string{"agent-message.schema.json", "assignment-record.schema.json"} {
		want, err := schema.ReadAsset(name)
		if err != nil {
			t.Fatalf("read binary schema %s: %v", name, err)
		}
		got, err := assignmentinputs.Read(root, assignmentinputs.SchemaPrefix+name)
		if err != nil {
			t.Fatalf("read embedded schema namespace %s: %v", name, err)
		}
		if !bytes.Equal(got, want) || sourceTestSHA(got) != sourceTestSHA(want) {
			t.Fatalf("embedded schema %s differs from executing binary bytes", name)
		}
	}

	wantProfile := agentcontext.ProfileBytes()
	gotProfile, err := assignmentinputs.Read(root, assignmentinputs.ContextProfileRef)
	if err != nil {
		t.Fatalf("read embedded Agent context profile: %v", err)
	}
	if !bytes.Equal(gotProfile, wantProfile) || sourceTestSHA(gotProfile) != sourceTestSHA(wantProfile) {
		t.Fatal("embedded context profile differs from executing binary bytes")
	}
	gotProfile[0] ^= 0xff
	secondRead, err := assignmentinputs.Read(root, assignmentinputs.ContextProfileRef)
	if err != nil || !bytes.Equal(secondRead, wantProfile) {
		t.Fatalf("caller mutation changed the embedded profile source: %v", err)
	}
}

func TestReadAssignmentSourcesRejectsUnknownNamespaceAndPathEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{
		"gsb-embedded/unknown-source.json",
		assignmentinputs.SchemaPrefix + "missing-schema.json",
		assignmentinputs.SchemaPrefix + "nested/agent-message.schema.json",
		assignmentinputs.SchemaPrefix + "../agent-message.schema.json",
		"../outside.txt",
		outside,
	} {
		t.Run(ref, func(t *testing.T) {
			if _, err := assignmentinputs.Read(root, ref); err == nil {
				t.Fatalf("unsafe or unknown source %q was accepted", ref)
			}
		})
	}
}

func TestVerifySourcesReadsTypedEmbeddedRecordReferences(t *testing.T) {
	root := t.TempDir()
	if _, err := os.Stat(filepath.Join(root, "internal")); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly contains an internal source tree: %v", err)
	}

	manifestBytes := []byte("frozen manifest bytes\n")
	taskBytes := []byte("frozen TASK bytes\n")
	roleBytes := []byte("frozen Agent definition bytes\n")
	skillBytes := []byte("frozen authorized Skill bytes\n")
	for ref, data := range map[string][]byte{
		"docs/workgroups/s6/manifest.json":    manifestBytes,
		"docs/dev/tasks/TASK-001.md":          taskBytes,
		"agents/backend-builder.md":           roleBytes,
		"skills/backend-engineering/SKILL.md": skillBytes,
	} {
		writeSourceTestFile(t, root, ref, data)
	}
	schemaBytes, err := schema.ReadAsset("agent-message.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	profileBytes := agentcontext.ProfileBytes()
	manifest := assignmentrecord.ContextSource{Kind: "manifest", Ref: "docs/workgroups/s6/manifest.json", ID: "team-manifest-assignmentinputs", Version: "1.0.0", SHA256: sourceTestSHA(manifestBytes)}
	task := assignmentrecord.ContextSource{Kind: "task", Ref: "docs/dev/tasks/TASK-001.md", ID: "TASK-001", Version: "1.0.0", SHA256: sourceTestSHA(taskBytes)}
	role := assignmentrecord.ContextSource{Kind: "agent_definition", Ref: "agents/backend-builder.md", ID: "backend-builder", SHA256: sourceTestSHA(roleBytes)}
	embeddedProfile := assignmentrecord.ContextSource{Kind: "embedded_context_profile", Ref: assignmentinputs.ContextProfileRef, ID: "agent-context-profiles", Version: "1.0.0", SHA256: sourceTestSHA(profileBytes)}
	embeddedSchema := assignmentrecord.ContextSource{Kind: "embedded_schema", Ref: assignmentinputs.SchemaPrefix + "agent-message.schema.json", ID: "agent-message.schema.json", Version: "1.0.0", SHA256: sourceTestSHA(schemaBytes)}
	record := testAssignmentRecord(manifest, task, role, embeddedProfile, embeddedSchema, skillBytes)
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		t.Fatalf("fixture Record is invalid: %v", err)
	}
	if err := assignmentrecord.VerifySources(record, func(ref string) ([]byte, error) {
		return assignmentinputs.Read(root, ref)
	}); err != nil {
		t.Fatalf("VerifySources rejected real filesystem and embedded sources: %v", err)
	}

	tampered := record
	tampered.ContextSources = append([]assignmentrecord.ContextSource{}, record.ContextSources...)
	for i := range tampered.ContextSources {
		if tampered.ContextSources[i].Kind == "embedded_schema" {
			tampered.ContextSources[i].SHA256 = sourceTestSHA([]byte("different embedded schema bytes"))
		}
	}
	if err := assignmentrecord.VerifySources(tampered, func(ref string) ([]byte, error) {
		return assignmentinputs.Read(root, ref)
	}); err == nil {
		t.Fatal("VerifySources accepted an embedded-schema digest that differs from binary bytes")
	}
}

func TestReadAssignmentInputsBoundsOrdinaryFilesystemSources(t *testing.T) {
	root := t.TempDir()
	path := "docs/frozen-input.txt"
	want := []byte("ordinary frozen source\n")
	writeSourceTestFile(t, root, path, want)
	got, err := assignmentinputs.Read(root, path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("ordinary source read = %q, %v", got, err)
	}
	writeSourceTestFile(t, root, "docs/oversize-input.bin", bytes.Repeat([]byte("x"), 4<<20+1))
	if _, err := assignmentinputs.Read(root, "docs/oversize-input.bin"); err == nil {
		t.Fatal("ordinary source exceeding the bounded read size was accepted")
	}
}

func testAssignmentRecord(manifest, task, role, embeddedProfile, embeddedSchema assignmentrecord.ContextSource, skillBytes []byte) assignmentrecord.Record {
	skill := assignmentrecord.ContextSource{Kind: "authorized_skill", Ref: "skills/backend-engineering/SKILL.md", ID: "backend-engineering", Version: "1.0.0", SHA256: sourceTestSHA(skillBytes)}
	scope := assignmentrecord.ScopeBasis{
		ManifestScope: []string{"internal"}, ManifestWritePaths: []string{"internal/pkg"},
		ManifestOutputPaths: []string{}, TaskWritePaths: []string{"internal"},
	}
	effective, _ := assignmentrecord.EffectiveScope(scope)
	return assignmentrecord.Record{
		SchemaVersion: assignmentrecord.AssignmentRecordSchema, Contract: assignmentrecord.AssignmentRecordContract,
		RuntimeID: "loop-assignmentinputs-test", BaselineGeneration: 2, RegistrationRevision: 6,
		AssignmentID: "assignment-assignmentinputs-test", Revision: 1,
		Task: task, Manifest: manifest, AgentDefinition: role,
		TeamID: "workgroup-assignmentinputs-test", ResponsibilityID: "BUILD-WORK-PACKAGE",
		OwnerAgentID: "agent-assignmentinputs-test", RoleFamily: "backend-builder", DispatchMode: "plan_checkpoint",
		ReadPaths: []string{}, SkillRefs: []string{"backend-engineering"}, DependsOn: []string{},
		DoneWhen: []string{"the bounded assignment is finished"}, RequiredChecks: []string{},
		CheckProfiles: []checkexec.Profile{}, ScopeBasis: scope, EffectiveWritePaths: effective,
		ContextSources: []assignmentrecord.ContextSource{role, skill, embeddedProfile, embeddedSchema, manifest, task},
	}
}

func writeSourceTestFile(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func sourceTestSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
