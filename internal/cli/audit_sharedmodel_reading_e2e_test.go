package cli_test

// These are audit-only probes for the first shared-model contract slice. They
// deliberately live outside production packages: the review needs to prove
// what the current parser, S4 entry point, and synthetic HTTP consumer do,
// without changing their implementation.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func auditSharedModelFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("..", "..", "docs", "examples", "shared-model", "project")
	if err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

type auditSchemaLoader struct{ root string }

func (l auditSchemaLoader) Load(raw string) (any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "file" || u.Host != "" {
		return nil, fmt.Errorf("non-local schema dependency: %s", raw)
	}
	path := filepath.Clean(filepath.FromSlash(u.Path))
	rel, err := filepath.Rel(l.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("schema dependency escapes fixture: %s", raw)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}

func auditCompileSchema(t *testing.T, root, fragment string) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(auditSchemaLoader{root: root})
	compiler.DefaultDraft(jsonschema.Draft2020)
	schemaPath := filepath.Join(root, "docs", "architecture", "data-model", "orders.schema.json")
	schema, err := compiler.Compile((&url.URL{Scheme: "file", Path: schemaPath, Fragment: fragment}).String())
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// The normal acceptance case uses the same local schema as the pilot, then
// performs real HTTP request/response validation and checks both rejection
// paths for side effects. It also proves the source is consumed by code,
// rather than merely named in Markdown.
func TestAuditSharedModelReadingHTTPConsumer(t *testing.T) {
	root := auditSharedModelFixture(t)
	result := sharedmodel.Check(root, fileview.Disk{Root: root})
	if len(result.Problems) != 0 || result.Rows != 2 {
		t.Fatalf("shared-model fixture check: rows=%d problems=%v warnings=%v", result.Rows, result.Problems, result.Warnings)
	}
	requestSchema := auditCompileSchema(t, root, "/$defs/request")
	responseSchema := auditCompileSchema(t, root, "/$defs/response")

	type providerState struct {
		sync.Mutex
		status    string
		mutations int
	}
	state := &providerState{status: "open"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || requestSchema.Validate(body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		state.Lock()
		if state.status == "completed" {
			state.Unlock()
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "completed"})
			return
		}
		state.status = "cancelled"
		state.mutations++
		state.Unlock()
		response := map[string]any{"state": "cancelled"}
		if err := responseSchema.Validate(response); err != nil {
			t.Errorf("provider emitted response outside shared model: %v", err)
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	post := func(body string) *http.Response {
		t.Helper()
		response, err := http.Post(server.URL+"/cancel", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	readBody := func(response *http.Response) map[string]any {
		t.Helper()
		defer response.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			if response.StatusCode == http.StatusBadRequest {
				return nil
			}
			t.Fatal(err)
		}
		return body
	}

	response := post(`{"order_id":"order-1"}`)
	if response.StatusCode != http.StatusOK || readBody(response)["state"] != "cancelled" {
		t.Fatalf("normal cancellation did not consume shared request/response: status=%d", response.StatusCode)
	}
	state.Lock()
	beforeInvalidMutations, beforeInvalidState := state.mutations, state.status
	state.Unlock()
	response = post(`{"order_id":42}`)
	if response.StatusCode != http.StatusBadRequest {
		response.Body.Close()
		t.Fatalf("structural rejection status=%d, want 400", response.StatusCode)
	}
	response.Body.Close()
	state.Lock()
	if state.mutations != beforeInvalidMutations || state.status != beforeInvalidState {
		t.Fatalf("structural rejection had a side effect: mutations=%d/%d state=%s/%s", state.mutations, beforeInvalidMutations, state.status, beforeInvalidState)
	}
	state.status = "completed"
	beforeBusinessMutations := state.mutations
	state.Unlock()
	response = post(`{"order_id":"order-1"}`)
	if response.StatusCode != http.StatusConflict || readBody(response)["state"] != "completed" {
		t.Fatalf("business rejection status/body mismatch: status=%d", response.StatusCode)
	}
	state.Lock()
	if state.status != "completed" || state.mutations != beforeBusinessMutations {
		t.Fatalf("business rejection mutated provider state: state=%s mutations=%d/%d", state.status, state.mutations, beforeBusinessMutations)
	}
	state.Unlock()
	if err := responseSchema.Validate(map[string]any{"state": "invented-mock-state"}); err == nil {
		t.Fatal("mock enum drift was accepted by the shared response schema")
	}
}

func TestAuditSharedModelReadingLinkedV1Edges(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/dev/tasks/TASK-READ.md", `<a id="entry"></a>
# task
`)
	write("docs/dev/contracts/FE-READ.md", `<a id="first"></a>
<a id="second"></a>
Read [back](../tasks/TASK-READ.md#entry).
`)
	write("docs/dev/contracts/SYNC-READ.md", `<a id="sync"></a>
Read [back](../tasks/TASK-READ.md#entry).
`)
	base := `> Reading policy: linked-v1

## 2. Document Manifest

| Order | Kind | ID | Path | Clauses | Purpose | Mode |
|:---|:---|:---|:---|:---|:---|:---|
| 1 | contract | FE-READ | [first](../contracts/FE-READ.md#first) | §1 | read the contract | required |
| 2 | sync | SYNC-READ | [sync](../contracts/SYNC-READ.md#sync) | §1 | read the protocol | required |
| 3 | background | OLD | [historical](missing.md#old) | — | optional history | optional |
`
	if problems := sharedmodel.Reading(root, "docs/dev/tasks/TASK-READ.md", base, fileview.Disk{Root: root}); len(problems) != 0 {
		t.Fatalf("valid linked-v1 rows or navigation back-link rejected: %v", problems)
	}
	duplicate := strings.Replace(base, "| 2 | sync", "| 2 | contract | FE-READ | [second](../contracts/FE-READ.md#second) | §1 | another required slice | required |\n| 3 | sync", 1)
	if problems := sharedmodel.Reading(root, "docs/dev/tasks/TASK-READ.md", duplicate, fileview.Disk{Root: root}); len(problems) != 0 {
		t.Fatalf("different fragments of one Markdown source should be legal: %v", problems)
	}
	exactDuplicate := strings.Replace(duplicate, "[second](../contracts/FE-READ.md#second)", "[first-again](../contracts/FE-READ.md#first)", 1)
	if problems := sharedmodel.Reading(root, "docs/dev/tasks/TASK-READ.md", exactDuplicate, fileview.Disk{Root: root}); len(problems) != 1 || !strings.Contains(problems[0], "duplicate required reading") {
		t.Fatalf("exact path+fragment duplicate was not rejected: %v", problems)
	}
	badMode := strings.Replace(base, "| 3 | background", "| 3 | background | OLD | [historical](missing.md#old) | — | optional history | maybe |\n| 4 | background", 1)
	if problems := sharedmodel.Reading(root, "docs/dev/tasks/TASK-READ.md", badMode, fileview.Disk{Root: root}); len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), "unknown reading Mode") {
		t.Fatalf("illegal linked-v1 mode was accepted: %v", problems)
	}
	missingAnchor := strings.Replace(base, "#first", "#missing", 1)
	if problems := sharedmodel.Reading(root, "docs/dev/tasks/TASK-READ.md", missingAnchor, fileview.Disk{Root: root}); len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), "missing Markdown anchor") {
		t.Fatalf("missing required anchor was accepted: %v", problems)
	}
}

// This is the natural S4 entrypoint: the CLI must surface a malformed
// required reading before a task batch can be treated as ready. The test keeps
// the task/DAG check separate from recursive Markdown navigation.
func TestAuditSharedModelReadingNaturalS4Check(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/dev/contracts/CONTRACTS-READ.md", `# index
| Contract | Clause |
|:---|:---|
| BE-READ §1 | cancel |
`)
	write("docs/dev/contracts/BE-READ.md", "> Status: locked\n# contract\n")
	write("docs/dev/tasks/TASK-READ.md", `# task
> Status: complete
> Reading policy: linked-v1
> Primary contract: BE-READ

## 2. Document Manifest
| Order | Kind | ID | Path | Clauses | Purpose | Mode |
|:---|:---|:---|:---|:---|:---|:---|
| 1 | contract | BE-READ | [missing anchor](../contracts/BE-READ.md#does-not-exist) | §1 | required contract context | required |

## 3. Delivered Clauses
| Contract | Delivered clauses |
|:---|:---|
| BE-READ | §1 |

## 7. Closing Contract
assert BE-READ §1 == satisfied

## 8. Dependencies
| Dependency | Required evidence | Status |
|:---|:---|:---|
| N/A | — | satisfied |
`)
	result, err := semantic.TasksCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) == 0 || !strings.Contains(strings.Join(result.Problems, "\n"), "missing Markdown anchor") {
		t.Fatalf("TasksCheck did not consume linked-v1 reading policy: %+v", result.Problems)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"tasks", "check", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "required reading") {
		t.Fatalf("natural S4 CLI check did not reject malformed reading: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

// Same-REQ indexes share one operation/slot authority. This regression uses
// different schema files with contradictory constraints; each index passes in
// isolation, while the combined batch must reject the conflicting authority.
func TestAuditSharedModelReadingDuplicateAuthorityAcrossIndexes(t *testing.T) {
	write := func(root, rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeBatch := func(root, index, schema, valid, negative, consumerSuffix string) {
		t.Helper()
		schemas := map[string]string{
			"orders-a.schema.json": `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["order_id"],
  "properties": {"order_id": {"type": "string", "pattern": "^order-[0-9]+$"}}
}`,
			"orders-b.schema.json": `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["order_id"],
  "properties": {"order_id": {"type": "integer", "minimum": 1}}
}`,
		}
		write(root, "docs/architecture/data-model/"+schema, schemas[schema])
		stem := strings.TrimSuffix(schema, ".schema.json")
		write(root, "docs/architecture/data-model/"+stem+"-valid.json", valid)
		write(root, "docs/architecture/data-model/"+stem+"-invalid.json", negative)
		var consumers []string
		for _, kind := range []string{"FE", "BE", "SYNC"} {
			name := kind + "-" + consumerSuffix
			consumers = append(consumers, "["+name+"]("+name+".md)")
			write(root, "docs/dev/contracts/"+name+".md", fmt.Sprintf(`# %s

## Shared model inputs

- [authoritative schema](../../architecture/data-model/%s)
`, name, schema))
		}
		write(root, "docs/dev/contracts/"+index+".md", fmt.Sprintf(`# %s

> REQ: REQ-001
> Shared model policy: json-schema-v1

## Shared model baseline

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| cancelOrder | request | [request](../../architecture/data-model/%s) | %s | [valid](../../architecture/data-model/%s-valid.json) | [invalid](../../architecture/data-model/%s-invalid.json) |
`, index, schema, strings.Join(consumers, " "), stem, stem))
	}

	// The two isolated batches each pass, so any combined error is about the
	// conflicting duplicate authority rather than malformed fixtures.
	first := t.TempDir()
	writeBatch(first, "CONTRACTS-001", "orders-a.schema.json", `{"order_id":"order-1"}`, `{"order_id":42}`, "A")
	if result := sharedmodel.Check(first, fileview.Disk{Root: first}, "REQ-001"); len(result.Problems) != 0 {
		t.Fatalf("isolated first same-REQ index is malformed: problems=%v warnings=%v", result.Problems, result.Warnings)
	}
	second := t.TempDir()
	writeBatch(second, "CONTRACTS-002", "orders-b.schema.json", `{"order_id":42}`, `{"order_id":"order-1"}`, "B")
	if result := sharedmodel.Check(second, fileview.Disk{Root: second}, "REQ-001"); len(result.Problems) != 0 {
		t.Fatalf("isolated second same-REQ index is malformed: problems=%v warnings=%v", result.Problems, result.Warnings)
	}

	root := t.TempDir()
	writeBatch(root, "CONTRACTS-001", "orders-a.schema.json", `{"order_id":"order-1"}`, `{"order_id":42}`, "A")
	writeBatch(root, "CONTRACTS-002", "orders-b.schema.json", `{"order_id":42}`, `{"order_id":"order-1"}`, "B")
	result := sharedmodel.Check(root, fileview.Disk{Root: root}, "REQ-001")
	if !strings.Contains(strings.Join(result.Problems, "\n"), "duplicate operation/slot") {
		t.Fatalf("expected cross-index duplicate operation/slot diagnostic, got problems=%v warnings=%v", result.Problems, result.Warnings)
	}
}
