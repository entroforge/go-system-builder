package projectconventions

import (
	"reflect"
	"testing"
)

func TestBundleBindingsAndCommonScope(t *testing.T) {
	if got := CommonScope([]string{"src/api/handler.go", "src/other"}, []string{"src/api/**", "docs/reports/report.json"}); !reflect.DeepEqual(got, []string{"src/api/handler.go"}) {
		t.Fatal("scope widened", got)
	}
	if got := CommonScope([]string{"src/api"}, []string{"src/api2/**"}); len(got) != 0 {
		t.Fatal("prefix ignored path boundary", got)
	}
	if got := CommonScope([]string{"src/*.go"}, []string{"src/**"}); len(got) != 0 {
		t.Fatal("unproved wildcard intersection became authority")
	}
	root, entry, req := fixture(t)
	registry(t, root, entry)
	p := projection(t, root, req)
	b := &Bundle{ResolverVersion: Version, TaskConventions: []Projection{p}}
	inputs := map[string]string{req.TaskRef: "formal-task-hash"}
	for _, s := range p.Sources {
		if s.Status == "readable" {
			inputs[s.Path] = s.SHA256
		}
	}
	if err := b.ValidateInputs(inputs); err != nil {
		t.Fatal(err)
	}
	delete(inputs, RegistryPath)
	if err := b.ValidateInputs(inputs); err == nil {
		t.Fatal("missing source accepted")
	}
	if _, err := b.Sources(map[string]bool{"docs/dev/tasks/TASK-OTHER.md": true}); err == nil {
		t.Fatal("foreign task accepted")
	}
	b.ResolverVersion = "new"
	if err := b.ValidateInputs(inputs); err == nil {
		t.Fatal("unbound version accepted")
	}
}
