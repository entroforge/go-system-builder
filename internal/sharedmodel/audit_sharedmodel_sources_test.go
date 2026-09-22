package sharedmodel_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
)

// TestAuditSharedModelCLIAndPinnedGitTree runs the authoring command and the
// formal reader against the same isolated pilot. Disk is allowed to diagnose a
// later dirty edit; the pinned Git view must continue to see the committed
// model until its ref moves.
func TestAuditSharedModelCLIAndPinnedGitTree(t *testing.T) {
	root := auditPilot(t)
	if code, out, errOut := auditCLI(t, root); code != 0 {
		t.Fatalf("pilot contracts check code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	auditGit(t, root, "init", "-qb", "development")
	auditGit(t, root, "config", "user.name", "shared-model-audit")
	auditGit(t, root, "config", "user.email", "shared-model-audit@example.invalid")
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-qm", "shared model baseline")
	view, err := fileview.New(root, "refs/heads/development", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := semantic.ContractsCheckWithFiles(root, view, "REQ-001"); err != nil || len(result.Problems) != 0 {
		t.Fatalf("committed Git view rejected the pilot: err=%v result=%+v", err, result)
	}

	// The authoring checkout now contains a malformed valid example, but the
	// formal view remains pinned to the committed valid bytes.
	auditPut(t, root, "docs/architecture/data-model/request.json", `{"order_id":42}`)
	if result, err := semantic.ContractsCheckWithFiles(root, view, "REQ-001"); err != nil || len(result.Problems) != 0 {
		t.Fatalf("pinned Git view consumed dirty bytes: err=%v result=%+v", err, result)
	}
	if code, out, errOut := auditCLI(t, root); code != 1 || !strings.Contains(out+errOut, "valid example") {
		t.Fatalf("disk authoring check must report dirty invalid example: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	auditGit(t, root, "add", "docs/architecture/data-model/request.json")
	auditGit(t, root, "commit", "-qm", "dirty example")
	if err := view.Verify(); err == nil {
		t.Fatal("formal Git view accepted a moved development ref")
	}
}

// TestAuditSharedModelREQScopeKeepsOtherIndexOutOfBatch proves that the
// shared-model adapter filters by the bound REQ while a no-REQ authoring check
// still diagnoses every explicitly adopted index.
func TestAuditSharedModelREQScopeKeepsOtherIndexOutOfBatch(t *testing.T) {
	root := auditPilot(t)
	auditPut(t, root, "docs/dev/contracts/CONTRACTS-002.md", "> Status: locked\n> REQ: REQ-002\n> Shared model policy: json-schema-v1\n\n## Shared model baseline\n\n| Operation | Slot | Schema | Consumers | Valid example | Structural negative |\n|:---|:---|:---|:---|:---|:---|\n| broken | request | [missing](../../architecture/data-model/missing.json) | [FE](FE-001.md) | [valid](../../architecture/data-model/missing.json) | N/A |\n")
	if result := sharedmodel.Check(root, fileview.Disk{Root: root}, "REQ-001"); len(result.Problems) != 0 {
		t.Fatalf("REQ-001 check leaked malformed REQ-002 index: %#v", result.Problems)
	}
	if result := sharedmodel.Check(root, fileview.Disk{Root: root}); len(result.Problems) == 0 {
		t.Fatal("all-index authoring check missed malformed adopted index")
	}
}

// TestAuditSharedModelNativeAnchorAndRecursion uses a native $anchor and a
// recursive local $ref in one schema. These are JSON Schema semantics and
// must remain legal without a custom registry or graph checker.
func TestAuditSharedModelNativeAnchorAndRecursion(t *testing.T) {
	root := auditPilot(t)
	schemaPath := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	schema := auditJSONMap(t, schemaPath)
	schema["$id"] = "https://example.invalid/shared/orders-v1"
	defs := schema["$defs"].(map[string]any)
	request := defs["request"].(map[string]any)
	request["$anchor"] = "request"
	request["properties"].(map[string]any)["next"] = map[string]any{"$ref": "#request"}
	auditJSONPut(t, schemaPath, schema)
	for _, name := range []string{"FE-001.md", "BE-001.md", "SYNC-001.md"} {
		path := filepath.Join(root, "docs/dev/contracts", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		updated := strings.ReplaceAll(string(data), "orders.schema.json#/$defs/request", "orders.schema.json#request")
		auditPut(t, root, filepath.ToSlash(filepath.Join("docs/dev/contracts", name)), updated)
	}
	index := filepath.Join(root, "docs/dev/contracts/CONTRACTS-001.md")
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	auditPut(t, root, "docs/dev/contracts/CONTRACTS-001.md", strings.ReplaceAll(string(data), "orders.schema.json#/$defs/request", "orders.schema.json#request"))
	if result := sharedmodel.Check(root, fileview.Disk{Root: root}); len(result.Problems) != 0 {
		t.Fatalf("native anchor/recursive schema rejected: %#v", result.Problems)
	}
}

// TestAuditSharedModelRejectsNetworkSchemaReference is a default negative
// acceptance case. The real CLI must fail closed when a local schema asks the
// compiler to fetch a remote dependency.
func TestAuditSharedModelRejectsNetworkSchemaReference(t *testing.T) {
	root := auditPilot(t)
	schemaPath := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	schema := auditJSONMap(t, schemaPath)
	request := schema["$defs"].(map[string]any)["request"].(map[string]any)
	request["properties"].(map[string]any)["order_id"] = map[string]any{"$ref": "https://example.invalid/remote.json"}
	auditJSONPut(t, schemaPath, schema)
	code, out, errOut := auditCLI(t, root)
	if code != 1 || !strings.Contains(out+errOut, "remote schema dependency") {
		t.Fatalf("remote schema dependency was not rejected: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
}

// TestAuditSharedModelFormalGitRejectsModelSymlink is the formal counterpart
// to the disk worktree probe below. Git view must reject a model path that is a
// symlink, even if the target bytes happen to be valid.
func TestAuditSharedModelFormalGitRejectsModelSymlink(t *testing.T) {
	root := auditPilot(t)
	auditGit(t, root, "init", "-qb", "development")
	auditGit(t, root, "config", "user.name", "shared-model-audit")
	auditGit(t, root, "config", "user.email", "shared-model-audit@example.invalid")
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-qm", "shared model baseline")
	worker := filepath.Join(root, ".custom-worker")
	auditGit(t, root, "worktree", "add", "-b", "worker", worker)
	schemaPath := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	if err := os.Remove(schemaPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(worker, "docs/architecture/data-model/orders.schema.json"), schemaPath); err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, "refs/heads/development", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := semantic.ContractsCheckWithFiles(root, view, "REQ-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) == 0 {
		t.Fatal("formal Git view accepted symlinked model input")
	}
}

// TestAuditSharedModelDiskRejectsInternalWorktreeSymlink verifies that
// editing-time disk reads do not consume bytes from a registered internal
// worktree through a root-contained symlink.
func TestAuditSharedModelDiskRejectsInternalWorktreeSymlink(t *testing.T) {
	root := auditPilot(t)
	auditGit(t, root, "init", "-qb", "development")
	auditGit(t, root, "config", "user.name", "shared-model-audit")
	auditGit(t, root, "config", "user.email", "shared-model-audit@example.invalid")
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-qm", "shared model baseline")
	worker := filepath.Join(root, ".custom-worker")
	auditGit(t, root, "worktree", "add", "-b", "worker", worker)
	schemaPath := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	if err := os.Remove(schemaPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(worker, "docs/architecture/data-model/orders.schema.json"), schemaPath); err != nil {
		t.Fatal(err)
	}
	result := sharedmodel.Check(root, fileview.Disk{Root: root})
	if len(result.Problems) == 0 {
		t.Fatalf("root-contained internal worktree symlink was accepted; problems=%#v", result.Problems)
	}
}

// TestAuditSharedModelDuplicateStableID is the A03 duplicate-authority
// acceptance case. Different schema files may not claim the same stable ID.
func TestAuditSharedModelDuplicateStableIDProbe(t *testing.T) {
	root := auditPilot(t)
	schemaPath := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	schema := auditJSONMap(t, schemaPath)
	// Put the duplicate stable identifier on the exact authoritative subschema
	// selected by the first row, not only on its containing document.
	request := schema["$defs"].(map[string]any)["request"].(map[string]any)
	request["$id"] = "https://example.invalid/shared/stable-id"
	auditJSONPut(t, schemaPath, schema)
	auditPut(t, root, "docs/architecture/data-model/other.schema.json", `{"$id":"https://example.invalid/shared/stable-id","type":"object","required":["state"],"properties":{"state":{"type":"string"}},"additionalProperties":false}`)
	auditPut(t, root, "docs/architecture/data-model/other.json", `{"state":"cancelled"}`)
	indexPath := filepath.Join(root, "docs/dev/contracts/CONTRACTS-001.md")
	index, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	row := "| cancelOrder | response-409 | [other](../../architecture/data-model/other.schema.json) | [FE](FE-001.md) [BE](BE-001.md) [SYNC](SYNC-001.md) | [valid](../../architecture/data-model/other.json) | N/A |"
	auditPut(t, root, "docs/dev/contracts/CONTRACTS-001.md", strings.Replace(string(index), "\n## Coverage", "\n"+row+"\n\n## Coverage", 1))
	for _, name := range []string{"FE-001.md", "BE-001.md", "SYNC-001.md"} {
		path := filepath.Join(root, "docs/dev/contracts", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		updated := strings.Replace(string(data), "\n<a id=", "\n- [Other](../../architecture/data-model/other.schema.json)\n\n<a id=", 1)
		auditPut(t, root, filepath.ToSlash(filepath.Join("docs/dev/contracts", name)), updated)
	}
	result := sharedmodel.Check(root, fileview.Disk{Root: root})
	if !strings.Contains(strings.Join(result.Problems, "\n"), "duplicate") {
		t.Fatalf("A03 expected duplicate stable-$id rejection, got %#v", result.Problems)
	}
}

// Same source reuse is legal, including when the declarations live in two
// indexes for the same REQ. This also exercises the filename fallback on the
// pilot's CONTRACTS-001.md index.
func TestAuditSharedModelSameSourceReuseAcrossIndexes(t *testing.T) {
	root := auditPilot(t)
	auditPut(t, root, "docs/dev/contracts/CONTRACTS-001-extra.md", `# Supplemental cancellation contracts

> REQ: REQ-001
> Shared model policy: json-schema-v1

## Shared model baseline

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| cancelOrder | request | [request](../../architecture/data-model/orders.schema.json#/$defs/request) | [FE](FE-001.md) [BE](BE-001.md) [SYNC](SYNC-001.md) | [valid](../../architecture/data-model/request.json) | [structural negative](../../architecture/data-model/invalid-request.json) |
`)
	if result := sharedmodel.Check(root, fileview.Disk{Root: root}, "REQ-001"); len(result.Problems) != 0 {
		t.Fatalf("same schema source was rejected across same-REQ indexes: %#v", result.Problems)
	}
}

// Different REQs are separate authority batches. A conflicting slot and
// stable schema ID in REQ-002 must not poison a REQ-001 check or an all-index
// authoring check when the declarations are scoped correctly.
func TestAuditSharedModelDifferentREQIsolation(t *testing.T) {
	root := auditPilot(t)
	ordersPath := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	orders := auditJSONMap(t, ordersPath)
	orders["$defs"].(map[string]any)["request"].(map[string]any)["$id"] = "https://example.invalid/shared/same-id"
	auditJSONPut(t, ordersPath, orders)
	auditPut(t, root, "docs/architecture/data-model/other.schema.json", `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://example.invalid/shared/same-id","type":"object","required":["order_id"],"properties":{"order_id":{"type":"integer","minimum":1}},"additionalProperties":false}`)
	auditPut(t, root, "docs/architecture/data-model/other.json", `{"order_id":1}`)
	for _, name := range []string{"FE-002.md", "BE-002.md", "SYNC-002.md"} {
		auditPut(t, root, "docs/dev/contracts/"+name, `# REQ-002 consumer

## Shared model inputs

- [request](../../architecture/data-model/other.schema.json)
`)
	}
	auditPut(t, root, "docs/dev/contracts/CONTRACTS-002.md", `# Separate requirement

> REQ: REQ-002
> Shared model policy: json-schema-v1

## Shared model baseline

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| cancelOrder | request | [request](../../architecture/data-model/other.schema.json) | [FE](FE-002.md) [BE](BE-002.md) [SYNC](SYNC-002.md) | [valid](../../architecture/data-model/other.json) | N/A |
`)
	for _, req := range []string{"REQ-001", ""} {
		if result := sharedmodel.Check(root, fileview.Disk{Root: root}, req); len(result.Problems) != 0 {
			t.Fatalf("different REQ authority leaked into scope %q: %#v", req, result.Problems)
		}
	}
}

// Only compiler-loaded schemas participate in stable-ID authority. The test
// includes nested and transitive schema resources, while an instance object
// carries an ordinary "$id" property that must remain data.
func TestAuditSharedModelSchemaIDClosureSkipsInstanceIDs(t *testing.T) {
	root := auditPilot(t)
	schemaPath := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	schema := auditJSONMap(t, schemaPath)
	request := schema["$defs"].(map[string]any)["request"].(map[string]any)
	request["$id"] = "request-resource.json"
	properties := request["properties"].(map[string]any)
	properties["metadata"] = map[string]any{"type": "object"}
	properties["next"] = map[string]any{"$ref": "next.schema.json"}
	auditJSONPut(t, schemaPath, schema)
	auditPut(t, root, "docs/architecture/data-model/next.schema.json", `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://example.invalid/shared/next-v1","type":"object","$defs":{"nested":{"$id":"https://example.invalid/shared/nested-v1","type":"object"}}}`)
	auditPut(t, root, "docs/architecture/data-model/request.json", `{"order_id":"order-1","metadata":{"$id":"https://example.invalid/shared/nested-v1"}}`)
	if result := sharedmodel.Check(root, fileview.Disk{Root: root}); len(result.Problems) != 0 {
		t.Fatalf("schema closure or instance $id handling rejected valid model: %#v", result.Problems)
	}
}

func TestAuditSharedModelTransitiveNestedStableIDConflict(t *testing.T) {
	root := auditPilot(t)
	schemaPath := filepath.Join(root, "docs/architecture/data-model/orders.schema.json")
	schema := auditJSONMap(t, schemaPath)
	request := schema["$defs"].(map[string]any)["request"].(map[string]any)
	request["$id"] = "request-resource.json"
	request["properties"].(map[string]any)["next"] = map[string]any{"$ref": "next.schema.json"}
	auditJSONPut(t, schemaPath, schema)
	auditPut(t, root, "docs/architecture/data-model/next.schema.json", `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","$defs":{"nested":{"$id":"request-resource.json","type":"object"}}}`)
	result := sharedmodel.Check(root, fileview.Disk{Root: root})
	if !strings.Contains(strings.Join(result.Problems, "\n"), "duplicate schema $id") {
		t.Fatalf("nested transitive schema $id conflict was accepted: %#v", result.Problems)
	}
}

func auditPilot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join("..", "..", "docs", "examples", "shared-model", "project")
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func auditPut(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func auditJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func auditJSONPut(t *testing.T, path string, value map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func auditGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func auditCLI(t *testing.T, root string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"contracts", "check", "--root", root, "--json"}, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}
