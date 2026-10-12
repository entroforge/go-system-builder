// Package testfixture builds disposable protocol inputs; it never runs a business browser.
package testfixture

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func Digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func Write(t *testing.T, root, p string, v any) []byte {
	t.Helper()
	var b []byte
	switch x := v.(type) {
	case []byte:
		b = x
	case string:
		b = []byte(x)
	default:
		var e error
		b, e = json.MarshalIndent(v, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
	}
	full := filepath.Join(root, p)
	if e := os.MkdirAll(filepath.Dir(full), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(full, b, 0644); e != nil {
		t.Fatal(e)
	}
	return b
}
func Git(t *testing.T, root string, args ...string) string {
	t.Helper()
	b, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return strings.TrimSpace(string(b))
}
func RelatedModules(t *testing.T, root string, state map[string]any, graphBytes []byte) (string, string) {
	t.Helper()
	def, e := os.ReadFile("../../docs/control/loop-definition.json")
	if e != nil {
		t.Fatal(e)
	}
	Write(t, root, "docs/control/loop-definition.json", def)
	sp := filepath.Join(root, ".claude/loop-state.json")
	jp := filepath.Join(root, ".claude/loop-events.jsonl")
	req := Write(t, root, "docs/requirements/REQ-001.md", "REQ references docs/design/prototypes/customers/ only\n")
	Write(t, root, "server/customers/status.go", "package customers\n// before\n")
	contract := Write(t, root, "docs/dev/contracts/SYNC-001.md", "customer-status API is consumed by orders\n")
	graph := Write(t, root, "docs/architecture/module-relations.json", graphBytes)
	for _, id := range []string{"customers", "orders"} {
		Write(t, root, "docs/design/prototypes/"+id+"/cases.json", map[string]any{"module": id, "cases": []any{map[string]any{"id": "CASE-001", "title": id + " flow", "required": true, "browser_required": true, "flow_refs": []string{"PATH-001"}, "oracle": map[string]any{"visible": []string{id + " behavior preserved"}}}}})
	}
	Git(t, root, "init", "-b", "dev")
	Git(t, root, "add", "docs", "server")
	Git(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "baseline")
	base := Git(t, root, "rev-parse", "HEAD")
	state["bound_req"] = map[string]any{"path": "docs/requirements/REQ-001.md", "id": "REQ-001", "version": "1.0.0", "sha256": Digest(req), "status": "locked", "approved_by": "user", "approved_at": "2026-10-12T00:00:00Z", "metadata": map[string]any{"ui_impact": "changed"}, "workspace": map[string]any{"project_root": root, "dev_branch": "dev", "release_upstream": "main", "bound_commit": base}}
	state["documents"] = []any{map[string]any{"id": "MODULE-RELATIONS", "kind": "design", "path": "docs/architecture/module-relations.json", "version": "1.0.0", "sha256": Digest(graph), "status": "locked", "generation": 1, "author_agent_id": "architect"}, map[string]any{"id": "SYNC-001", "kind": "contract", "path": "docs/dev/contracts/SYNC-001.md", "version": "1.0.0", "sha256": Digest(contract), "status": "locked", "generation": 1, "author_agent_id": "contract-author"}}
	for _, module := range []string{"customers", "orders"} {
		p := "docs/design/prototypes/" + module + "/cases.json"
		bytes, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		state["documents"] = append(state["documents"].([]any), map[string]any{"id": "MODULE-SCENARIO-" + module, "kind": "design", "path": p, "version": "1.0.0", "sha256": Digest(bytes), "status": "locked", "generation": 1, "author_agent_id": "architect"})
	}
	Write(t, root, "server/customers/status.go", "package customers\n// changed status API\n")
	Git(t, root, "add", "server")
	Git(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "product change")
	completion := Write(t, root, ".claude/evidence/completion.json", map[string]any{"kind": "completion_report", "changed_paths": []string{"server/customers/status.go"}})
	state["evidence"] = []any{map[string]any{"id": "completion-1", "kind": "completion_report", "path": ".claude/evidence/completion.json", "sha256": Digest(completion), "status": "valid", "baseline_generation": 1, "review_round": 1, "produced_by": []any{"builder"}, "responsibility_id": "Builder", "scope_refs": []any{"server/customers/status.go"}, "invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil}}
	Write(t, root, ".claude/loop-state.json", state)

	return sp, jp
}
