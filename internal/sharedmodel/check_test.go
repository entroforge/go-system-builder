package sharedmodel

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func pilot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	src := "../../docs/examples/shared-model/project"
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(target, b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func put(t *testing.T, root, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestSharedModelFailures(t *testing.T) {
	for _, tc := range []struct{ name, path, text, want string }{
		{"consumer drift", "docs/dev/contracts/FE-001.md", "## Shared model inputs\n[wrong](../../architecture/data-model/other.json)", "must reference"},
		{"bad valid", "docs/architecture/data-model/request.json", `{"order_id":42}`, "valid example"},
		{"business negative", "docs/architecture/data-model/invalid-request.json", `{"order_id":"completed"}`, "unexpectedly passes"},
		{"network dependency", "docs/architecture/data-model/orders.schema.json", `{"$defs":{"request":{"$ref":"https://example.invalid/model.json"}}}`, "not pinned locally"},
		{"escape", "docs/architecture/data-model/orders.schema.json", `{"$defs":{"request":{"$ref":"../../../../../../secret.json"}}}`, "escapes repository"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := pilot(t)
			put(t, root, tc.path, tc.text)
			r := Check(root, fileview.Disk{Root: root})
			if !strings.Contains(strings.Join(r.Problems, "\n"), tc.want) {
				t.Fatalf("want %s, got %+v", tc.want, r)
			}
		})
	}
}

func TestSharedModelDecoratedREQOwnerUsesOriginalCurrentBatch(t *testing.T) {
	root := pilot(t)
	old := filepath.Join(root, "docs/dev/contracts/CONTRACTS-001.md")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	put(t, root, "docs/dev/contracts/CONTRACTS-CUSTOM.md", "> 需求: [REQ-001](../../requirements/REQ-001.md)\n"+string(data))
	for _, name := range []string{"FE-001.md", "BE-001.md", "SYNC-001.md"} {
		p := filepath.Join(root, "docs/dev/contracts", name)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		put(t, root, "docs/dev/contracts/"+name, strings.ReplaceAll(string(data), "CONTRACTS-001.md", "CONTRACTS-CUSTOM.md"))
	}
	r := Check(root, fileview.Disk{Root: root}, "REQ-001")
	if len(r.Problems) != 0 || r.Rows != 2 {
		t.Fatalf("original decorated batch was skipped or rejected: %+v", r)
	}
	put(t, root, "docs/architecture/data-model/request.json", `{"order_id":42}`)
	r = Check(root, fileview.Disk{Root: root}, "REQ-001")
	if !strings.Contains(strings.Join(r.Problems, "\n"), "valid example") {
		t.Fatalf("selected current source did not check its real example: %+v", r)
	}
}
func TestSharedModelClosureAndRecursion(t *testing.T) {
	root := pilot(t)
	put(t, root, "docs/architecture/data-model/orders.schema.json", `{"$defs":{"request":{"$ref":"request.schema.json"},"response":{"type":"object"}}}`)
	put(t, root, "docs/architecture/data-model/request.schema.json", `{"type":"object","required":["order_id"],"properties":{"order_id":{"type":"string"},"next":{"$ref":"#"}}}`)
	r := Check(root, fileview.Disk{Root: root})
	if len(r.Problems) > 0 {
		t.Fatal(r.Problems)
	}
	if _, ok := r.Files["docs/architecture/data-model/request.schema.json"]; !ok {
		t.Fatal("missing transitive subject")
	}
}
func TestRequiredReading(t *testing.T) {
	root := t.TempDir()
	put(t, root, "docs/dev/contracts/FE-1.md", "<a id=\"operation\"></a>\n## Operation")
	table := "## 2. Document Manifest\n| 1 | contract | FE-1 | [read](../contracts/FE-1.md#operation) | §1 | goal | required |\n| 2 | background | x | [optional](missing.md) | all | background | optional |"
	if p := Reading(root, "docs/dev/tasks/TASK-1.md", table, fileview.Disk{Root: root}); len(p) > 0 {
		t.Fatal(p)
	}
	if p := Reading(root, "docs/dev/tasks/TASK-1.md", strings.ReplaceAll(table, "#operation", "#absent"), fileview.Disk{Root: root}); len(p) != 1 {
		t.Fatal(p)
	}
}
func TestConsumerProviderPilot(t *testing.T) {
	root := pilot(t)
	files := fileview.Disk{Root: root}
	r := Check(root, files)
	if len(r.Problems) > 0 || r.Rows != 2 {
		t.Fatalf("%+v", r)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(&viewLoader{root: root, files: files, loaded: map[string][]byte{}})
	compile := func(fragment string) *jsonschema.Schema {
		t.Helper()
		s, e := c.Compile(schemaFileURL(filepath.Join(root, "docs/architecture/data-model/orders.schema.json"), fragment))
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	request, response := compile("/$defs/request"), compile("/$defs/response")
	var mu sync.Mutex
	state := "open"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var v any
		if json.NewDecoder(r.Body).Decode(&v) != nil || request.Validate(v) != nil {
			w.WriteHeader(400)
			return
		}
		code := 200
		if state == "completed" {
			code = 409
		} else {
			state = "cancelled"
		}
		data := map[string]any{"state": state}
		if err := response.Validate(data); err != nil {
			t.Error(err)
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()
	call := func(body string, code int) {
		t.Helper()
		res, e := http.Post(server.URL+"/cancel", "application/json", bytes.NewBufferString(body))
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != code {
			t.Fatalf("status=%d body=%s", res.StatusCode, b)
		}
		if code != 400 {
			var v any
			if e = json.Unmarshal(b, &v); e != nil {
				t.Fatal(e)
			}
			if e = response.Validate(v); e != nil {
				t.Fatal(e)
			}
		}
	}
	var clientRequest any
	_ = json.Unmarshal([]byte(`{"order_id":"order-1"}`), &clientRequest)
	if e := request.Validate(clientRequest); e != nil {
		t.Fatal(e)
	}
	call(`{"order_id":"order-1"}`, 200)
	call(`{"order_id":42}`, 400)
	mu.Lock()
	state = "completed"
	mu.Unlock()
	call(`{"order_id":"order-1"}`, 409)
	mu.Lock()
	defer mu.Unlock()
	if state != "completed" {
		t.Fatal("business rejection mutated state")
	}
	if e := response.Validate(map[string]any{"state": "invented-mock-state"}); e == nil {
		t.Fatal("mock drift accepted")
	}
}

func TestSharedInputsUsePinnedGitView(t *testing.T) {
	root := pilot(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git: %s %v", b, e)
		}
	}
	git("init", "-qb", "development")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "design")
	view, e := fileview.New(root, "refs/heads/development", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if e != nil {
		t.Fatal(e)
	}
	put(t, root, "docs/architecture/data-model/request.json", `{"order_id":42}`)
	if r := Check(root, view); len(r.Problems) != 0 {
		t.Fatal(r.Problems)
	}
	if r := Check(root, fileview.Disk{Root: root}); len(r.Problems) == 0 {
		t.Fatal("disk did not see invalid example")
	}
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "changed example")
	if view.Verify() == nil {
		t.Fatal("moving input ref accepted")
	}
}

func TestStrictReadingPolicy(t *testing.T) {
	root := t.TempDir()
	for _, s := range []string{
		"> Reading policy: linked-v1\n## Document Manifest\n",
		"> Reading policy: linked-v1\n## Document Manifest\n| 1 | contract | FE-1 | plain-path | all | purpose | required |",
		"> Reading policy: linked-v1\n## Document Manifest\n| 1 | contract | FE-1 | [read](missing.md) | all | purpose | requried |",
	} {
		if p := Reading(root, "docs/dev/tasks/TASK-1.md", s, fileview.Disk{Root: root}); len(p) == 0 {
			t.Fatal("malformed reading accepted")
		}
	}
}

func TestScopedBatchIgnoresUnrelatedModel(t *testing.T) {
	root := pilot(t)
	put(t, root, "docs/dev/contracts/CONTRACTS-999.md", "> Shared model policy: json-schema-v1\n")
	if r := Check(root, fileview.Disk{Root: root}, "REQ-001"); len(r.Problems) > 0 {
		t.Fatal(r.Problems)
	}
	if r := Check(root, fileview.Disk{Root: root}); len(r.Problems) == 0 {
		t.Fatal("explicit all-index authoring check missed bad batch")
	}
}

func TestDiskRejectsEscapingSchemaSymlink(t *testing.T) {
	root := pilot(t)
	outside := filepath.Join(t.TempDir(), "schema.json")
	b, e := os.ReadFile(filepath.Join(root, "docs/architecture/data-model/orders.schema.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(outside, b, 0644); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, p); e != nil {
		t.Fatal(e)
	}
	r := Check(root, fileview.Disk{Root: root})
	if !strings.Contains(strings.Join(r.Problems, "\n"), "escapes authority root") {
		t.Fatal(r.Problems)
	}
}

type diskRuleView struct{ fileview.Disk }

func (diskRuleView) Source(string) (string, error) { return "disk", nil }
func TestProductionEvidenceCannotStandInForModel(t *testing.T) {
	root := pilot(t)
	r := Check(root, diskRuleView{fileview.Disk{Root: root}})
	if !strings.Contains(strings.Join(r.Problems, "\n"), "require git_tree") {
		t.Fatal(r.Problems)
	}
}

func TestSharedModelConsumerRegisteredExamples(t *testing.T) {
	for _, tc := range []struct{ name, extra, path, data, want string }{
		{name: "registered valid and structural negative"},
		{name: "unknown schema", extra: "[foreign](../../architecture/data-model/foreign.json)\n", path: "docs/architecture/data-model/foreign.json", data: `{"type":"object"}`, want: "undeclared shared model input"},
		{name: "example fragment is not registered", extra: "[fragment](../../architecture/data-model/request.json#/order_id)\n", want: "undeclared shared model input"},
		{name: "missing registered example", path: "docs/architecture/data-model/request.json", data: "__missing__", want: "request.json"},
		{name: "invalid registered valid", path: "docs/architecture/data-model/request.json", data: `{"order_id":42}`, want: "valid example"},
		{name: "passing registered negative", path: "docs/architecture/data-model/invalid-request.json", data: `{"order_id":"completed"}`, want: "unexpectedly passes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := pilot(t)
			contract := "docs/dev/contracts/FE-001.md"
			data, err := os.ReadFile(filepath.Join(root, contract))
			if err != nil {
				t.Fatal(err)
			}
			// Keep schema references mandatory while also consuming registered instances.
			content := strings.Replace(string(data), "## Shared model inputs", "## Shared model inputs\n[valid](../../architecture/data-model/request.json)\n[negative](../../architecture/data-model/invalid-request.json)\n"+tc.extra, 1)
			put(t, root, contract, content)
			if tc.path != "" {
				if tc.data == "__missing__" {
					if err := os.Remove(filepath.Join(root, tc.path)); err != nil {
						t.Fatal(err)
					}
				} else {
					put(t, root, tc.path, tc.data)
				}
			}
			result := Check(root, fileview.Disk{Root: root})
			if tc.want != "" {
				if !strings.Contains(strings.Join(result.Problems, "\n"), tc.want) {
					t.Fatalf("want %s, got %v", tc.want, result.Problems)
				}
				return
			}
			if len(result.Problems) > 0 {
				t.Fatal(result.Problems)
			}
		})
	}
	t.Run("examples belong to their row consumers", func(t *testing.T) {
		root := pilot(t)
		index := "docs/dev/contracts/CONTRACTS-001.md"
		data, err := os.ReadFile(filepath.Join(root, index))
		if err != nil {
			t.Fatal(err)
		}
		put(t, root, index, strings.Replace(string(data), "[FE](FE-001.md) [BE](BE-001.md) [SYNC](SYNC-001.md)", "[BE](BE-001.md) [SYNC](SYNC-001.md)", 1))
		contract := "docs/dev/contracts/FE-001.md"
		data, err = os.ReadFile(filepath.Join(root, contract))
		if err != nil {
			t.Fatal(err)
		}
		put(t, root, contract, strings.Replace(string(data), "## Shared model inputs", "## Shared model inputs\n[valid](../../architecture/data-model/request.json)\n", 1))
		result := Check(root, fileview.Disk{Root: root})
		if !strings.Contains(strings.Join(result.Problems, "\n"), "undeclared shared model input docs/architecture/data-model/request.json") {
			t.Fatal(result.Problems)
		}
	})
}

func TestSharedModelNonePolicyReason(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		reject      bool
	}{
		{"real Chinese object and array rationale", "> Shared model reason: 本服务只有一个提供方，无第二消费方；权威形状为 {\"quantity\":0,\"version\":0}，批量示例为 [{\"quantity\":1,\"version\":1}]，另建共享模型会制造重复权威。\n", false},
		{"missing reason", "", true},
		{"empty reason", "> Shared model reason:\n", true},
		{"whitespace reason", "> Shared model reason:   \t\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := pilot(t)
			put(t, root, "docs/dev/contracts/CONTRACTS-001.md", "> Shared model policy: none\n"+tc.field)
			r := Check(root, fileview.Disk{Root: root})
			problems := strings.Join(r.Problems, "\n")
			if tc.reject {
				if !strings.Contains(problems, "policy none requires a concrete Shared model reason") {
					t.Fatalf("missing/empty reason accepted: %+v", r)
				}
			} else if len(r.Problems) != 0 {
				t.Fatalf("real JSON punctuation rejected: %v", r.Problems)
			}
		})
	}
}
