package transition

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/testfixture"
)

func TestRelatedModuleS2PinsGraphAndScenarioSubjects(t *testing.T) {
	root := t.TempDir()
	raw, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	graph := testfixture.Write(t, root, "graph-input.json", map[string]any{
		"schema_version": "1.0.0", "modules": []any{
			map[string]any{"id": "customers", "owned_paths": []string{"server/customers/"}, "cases_path": "docs/design/prototypes/customers/cases.json", "contract_refs": []string{}, "browser_applicability": "required"},
			map[string]any{"id": "orders", "owned_paths": []string{"server/orders/"}, "cases_path": "docs/design/prototypes/orders/cases.json", "contract_refs": []string{}, "browser_applicability": "required"},
		}, "resources": []any{},
	})
	testfixture.RelatedModules(t, root, state, graph)
	testfixture.Write(t, root, "docs/architecture/ARCHITECTURE-001.md", "# Architecture\n> Status: locked\n> Version: v1.0.0\n")
	flow := testfixture.Write(t, root, "docs/design/prototypes/orders/flows.md", "# PATH-001\nOrder reflects customer status\n")
	testfixture.Git(t, root, "add", "docs")
	testfixture.Git(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "locked design")
	view, err := fileview.ForState(root, state)
	if err != nil {
		t.Fatal(err)
	}
	// A working-tree rewrite must not silently replace the reviewed Git view.
	testfixture.Write(t, root, "docs/design/prototypes/orders/flows.md", "uncommitted weakened oracle")
	state["documents"] = []any{}
	result, err := actionRegisterDesignDocuments(state, &ActionContext{Root: root, OccurredAt: time.Now().UTC(), Request: &Request{Actor: "architect", Files: view}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "committed" {
		t.Fatal(result)
	}
	expected := map[string]string{
		"docs/architecture/module-relations.json": testfixture.Digest(graph),
		"docs/design/prototypes/orders/flows.md":  testfixture.Digest(flow),
	}
	for _, module := range []string{"customers", "orders"} {
		path := "docs/design/prototypes/" + module + "/cases.json"
		b, err := view.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		expected[path] = testfixture.Digest(b)
	}
	for _, raw := range state["documents"].([]any) {
		d := raw.(map[string]any)
		path, _ := d["path"].(string)
		if hash, ok := expected[path]; ok {
			if d["sha256"] != hash || d["kind"] != "design" || d["status"] != "locked" || integerOf(d["generation"]) != 1 || d["author_agent_id"] != "architect" {
				t.Fatalf("unlocked design input %s: %#v", filepath.Base(path), d)
			}
			delete(expected, path)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("S5 design manifest omitted related inputs: %v", expected)
	}
}
