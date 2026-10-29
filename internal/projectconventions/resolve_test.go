package projectconventions

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func fixture(t *testing.T) (string, Entry, Request) {
	t.Helper()
	root := t.TempDir()
	write(t, root, IndexPath, "# Rules\n\n---\nstatus: locked\n---\n\n| Rule | File | Trigger |\n|---|---|---|\n| API | `api-design.md` | API |\n| Errors | `error-handling.md` | errors |\n")
	body := "# API\n\n---\nrule_id: R-API-01\nstatus: locked\n---\n\n## Defaults\nUse stable error codes.\nUse cursor pagination.\n\n## Other\nUse a separate example.\n"
	write(t, root, "docs/rules/api-design.md", body)
	entry := Entry{ID: "CNV-API-01", Topic: "api", Scope: []string{"src/api"}, Default: "Use stable error codes.", SourceRefs: []Reference{{Kind: "rule", ID: "R-API-01", Path: "docs/rules/api-design.md", Anchor: "defaults", SHA256: digest([]byte(body))}}, ExampleRefs: []Reference{}, Exceptions: []string{}, Status: "approved"}
	req := Request{TaskID: "TASK-001", TaskRef: "docs/dev/tasks/TASK-001.md", Scope: []string{"src/api/handler.go"}}
	return root, entry, req
}
func write(t *testing.T, root, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func registry(t *testing.T, root string, entries ...Entry) {
	t.Helper()
	data, err := json.Marshal(Registry{SchemaVersion: Version, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, RegistryPath, string(data))
}
func projection(t *testing.T, root string, req Request) Projection {
	t.Helper()
	p := Resolve(root, fileview.Disk{Root: root}, req)
	data, _ := json.Marshal(p)
	if err := schema.NewEmbeddedValidator().ValidateBytes("task-conventions.schema.json", data); err != nil {
		t.Fatalf("invalid projection: %v\n%s", err, data)
	}
	return p
}
func TestExplicitConventionsSourcesAndAbsentCandidates(t *testing.T) {
	root, entry, req := fixture(t)
	p := projection(t, root, req)
	if p.RegistryStatus != "absent" || len(p.Entries) != 0 {
		t.Fatal("absence inferred defaults", p)
	}
	req.Reading = entry.SourceRefs
	p = projection(t, root, req)
	if len(p.Entries) != 1 || p.Entries[0].Status != "candidate" || p.Entries[0].Entry.Default != "" {
		t.Fatal("candidate became approval", p)
	}
	registry(t, root, entry)
	p = projection(t, root, req)
	if len(p.Entries) != 1 || p.Entries[0].Status != "declared_default" || len(p.Sources) != 3 {
		t.Fatal("original indexed source was not proven", p)
	}
	for _, s := range p.Sources {
		if s.Status != "readable" || s.SourceMode != "disk" || len(s.SHA256) != 64 {
			t.Fatal("wrong authoring source", s)
		}
	}
	entry.Default = "Invent an authorization grant."
	registry(t, root, entry)
	p = projection(t, root, req)
	if p.Entries[0].Status == "declared_default" {
		t.Fatal("registry approved its own invented decision")
	}
	entry.Default = "Use a separate example."
	registry(t, root, entry)
	p = projection(t, root, req)
	if p.Entries[0].Status == "declared_default" {
		t.Fatal("choice outside source anchor was approved")
	}
	entry.SourceRefs[0].Anchor = "missing"
	registry(t, root, entry)
	p = projection(t, root, req)
	if p.Entries[0].Status == "declared_default" {
		t.Fatal("missing anchor became full-file authority")
	}
}
func TestConventionsConflictsScopesMigrationAndStaleExamples(t *testing.T) {
	root, first, req := fixture(t)
	second := first
	second.ID = "CNV-API-02"
	second.Default = "Use cursor pagination."
	registry(t, root, first, second)
	p := projection(t, root, req)
	if len(p.Entries) != 2 || p.Entries[0].Status != "conflict" || p.Entries[1].Status != "conflict" {
		t.Fatal("conflict chose winner", p)
	}
	second.Default = first.Default
	second.ID = first.ID
	registry(t, root, first, second)
	p = projection(t, root, req)
	if p.Entries[0].Status != "conflict" {
		t.Fatal("duplicate ID was silently merged")
	}
	second.ID = "CNV-API-02"
	second.Scope = []string{"src/payments"}
	second.Default = "Use cursor pagination."
	req.Scope = []string{"src"}
	registry(t, root, first, second)
	p = projection(t, root, req)
	if len(p.Entries) != 2 || p.Entries[0].Status != "declared_default" || p.Entries[1].Status != "declared_default" {
		t.Fatal("disjoint module defaults conflicted", p)
	}
	req.Scope = []string{"src/apiculture/handler.go"}
	if p = projection(t, root, req); len(p.Entries) != 0 {
		t.Fatal("scope ignored path boundary")
	}
	req.Scope = []string{"src/api/handler.go"}
	for _, status := range []string{"migration", "superseded"} {
		first.Status = status
		registry(t, root, first)
		p = projection(t, root, req)
		if p.Entries[0].Status != status {
			t.Fatal("historical choice became current default")
		}
	}
	first.Status = "approved"
	first.ExampleRefs = []Reference{{Path: "src/api/handler.go", SHA256: digest([]byte("original implementation"))}}
	registry(t, root, first)
	p = projection(t, root, req)
	if p.Entries[0].Status != "stale" {
		t.Fatal("missing example remained current")
	}
	write(t, root, "src/api/handler.go", "original implementation")
	p = projection(t, root, req)
	if p.Entries[0].Status != "declared_default" {
		t.Fatal("valid example rejected", p)
	}
	write(t, root, "src/api/handler.go", "changed implementation")
	p = projection(t, root, req)
	if p.Entries[0].Status != "stale" {
		t.Fatal("changed example remained current")
	}
}
func TestConventionsRequireOriginalRegisteredDocumentProof(t *testing.T) {
	root, entry, req := fixture(t)
	body := "# ARCH-001\n\n> 状态：locked\n> 版本：v1.0.0\n\n## Choice\nUse stable error codes.\n"
	path := "docs/architecture/ARCH-001.md"
	write(t, root, path, body)
	ref := Reference{Kind: "architecture", ID: "ARCH-001", Path: path, Version: "v1.0.0", Anchor: "choice", SHA256: digest([]byte(body))}
	entry.SourceRefs = []Reference{ref}
	registry(t, root, entry)
	if p := projection(t, root, req); p.Entries[0].Status != "candidate" {
		t.Fatal("locked prose forged registration", p)
	}
	req.ApprovedRefs = []Reference{ref}
	if p := projection(t, root, req); p.Entries[0].Status != "declared_default" {
		t.Fatal("original exact proof rejected", p)
	}
	req.ApprovedRefs[0].Version = "v2.0.0"
	if p := projection(t, root, req); p.Entries[0].Status != "candidate" {
		t.Fatal("version mismatch accepted")
	}
}

type countedReader struct {
	fileview.Reader
	reads map[string]int
}

func (r *countedReader) ReadFile(path string) ([]byte, error) {
	r.reads[filepath.ToSlash(path)]++
	return r.Reader.ReadFile(path)
}
func TestConventionsCacheAndForbiddenSourcePaths(t *testing.T) {
	root, entry, req := fixture(t)
	registry(t, root, entry)
	reader := &countedReader{Reader: fileview.Disk{Root: root}, reads: map[string]int{}}
	rows := ResolveMany(root, reader, []Request{req, req})
	if len(rows) != 2 || rows[0].Entries[0].Status != "declared_default" || len(rows[1].Sources) != 3 {
		t.Fatal("shared cache changed task-specific provenance")
	}
	for path, n := range reader.reads {
		if n != 1 {
			t.Fatalf("source reread %s %d times", path, n)
		}
	}
	entry.SourceRefs[0].Path = ".claude/credentials"
	entry.ExampleRefs = []Reference{{Path: ".claude/credentials", SHA256: strings.Repeat("a", 64)}}
	registry(t, root, entry)
	reader.reads = map[string]int{}
	p := Resolve(root, reader, req)
	if p.Entries[0].Status == "declared_default" {
		t.Fatal("forbidden source approved")
	}
	for path := range reader.reads {
		if strings.Contains(path, "credentials") {
			t.Fatal("rejected reference read hidden runtime source")
		}
	}
}
func TestConventionsDiskContainmentAndCommittedView(t *testing.T) {
	root, entry, req := fixture(t)
	registry(t, root, entry)
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "docs"}, {"commit", "-qm", "Freeze original convention sources"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	view, err := fileview.New(root, "HEAD", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, entry.SourceRefs[0].Path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(external, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	if p := projection(t, root, req); p.Entries[0].Status == "declared_default" {
		t.Fatal("disk symlink escaped root")
	}
	p := Resolve(root, view, req)
	if p.Entries[0].Status != "declared_default" {
		t.Fatal("committed source was replaced by disk symlink", p)
	}
	for _, s := range p.Sources {
		if s.SourceMode != "git_tree" {
			t.Fatal("committed source mislabeled", s)
		}
	}
}
func TestConventionsSchemaUnknownFieldsAndSourceStatus(t *testing.T) {
	root, entry, req := fixture(t)
	registry(t, root, entry)
	data, err := os.ReadFile(filepath.Join(root, RegistryPath))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value["runtime_state"] = "working"
	bad, _ := json.Marshal(value)
	write(t, root, RegistryPath, string(bad))
	if p := projection(t, root, req); p.RegistryStatus != "invalid" || len(p.Entries) != 0 {
		t.Fatal("unknown registry field became authority", p)
	}
	registry(t, root, entry)
	write(t, root, IndexPath, "# Index\n---\nstatus: draft\n---\n| API | `api-design.md` | API |\n")
	if p := projection(t, root, req); p.Entries[0].Status == "declared_default" {
		t.Fatal("draft rule index approved a default")
	}
}

func TestFormalConventionExamplesRequireCommittedSource(t *testing.T) {
	root, entry, req := fixture(t)
	body := "owned implementation"
	write(t, root, "src/api/handler.go", body)
	entry.ExampleRefs = []Reference{{Path: "src/api/handler.go", SHA256: digest([]byte(body))}}
	registry(t, root, entry)
	req.ApprovedRefs = []Reference{{ID: "TASK-001", Path: req.TaskRef, Version: "v1", SHA256: strings.Repeat("a", 64)}}
	p := projection(t, root, req)
	if p.Entries[0].Status != "stale" {
		t.Fatal("formal source accepted an uncommitted disk example", p)
	}
	for _, source := range p.Sources {
		if source.Path == entry.ExampleRefs[0].Path {
			t.Fatal("formal rejected disk example was read or frozen")
		}
	}
}
