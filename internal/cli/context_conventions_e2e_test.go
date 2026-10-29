package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/projectconventions"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func configureContextConventions(t *testing.T) func(string, map[string]any) {
	return func(root string, state map[string]any) {
		t.Helper()
		rule := "docs/rules/error-handling.md"
		body, err := os.ReadFile(filepath.Join(root, rule))
		if err != nil {
			t.Fatal(err)
		}
		code := []byte("package src\nfunc Value() int { return 1 }\n")
		if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "src/value.go"), code, 0600); err != nil {
			t.Fatal(err)
		}
		entry := projectconventions.Entry{ID: "CNV-ERROR-01", Topic: "error-handling", Scope: []string{"src"}, Default: "Do not swallow errors.", SourceRefs: []projectconventions.Reference{{Kind: "rule", ID: "R-ERR-01", Path: rule, Anchor: "3-hard-rules", SHA256: workpacket.Digest(body)}}, ExampleRefs: []projectconventions.Reference{{Path: "src/value.go", SHA256: workpacket.Digest(code)}}, Exceptions: []string{}, Status: "approved"}
		stageJSON(t, root, projectconventions.RegistryPath, projectconventions.Registry{SchemaVersion: "1.0.0", Entries: []projectconventions.Entry{entry}})
		for _, raw := range state["documents"].([]any) {
			d := raw.(map[string]any)
			if d["kind"] != "task" {
				continue
			}
			path := d["path"].(string)
			data, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, []byte("\n## 4. Scope\n| Type | Paths |\n|---|---|\n| prospective write paths | src/value.go, src/value_test.go |\n")...)
			if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
				t.Fatal(err)
			}
			d["sha256"] = workpacket.Digest(data)
		}
	}
}
func TestBuilderConventionsOwnedExampleSurvivesActualVerifiedIntegration(t *testing.T) {
	f := coreBuilderFixture(t, configureContextConventions(t))
	runCoreBuilderIntegration(t, f)
	b, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	e := b.Executions["assignment-core-builder"]
	c, err := b.ReadDispatchContext(e)
	if err != nil {
		t.Fatal(err)
	}
	if c.Conventions == nil || len(c.Conventions.TaskConventions) != 1 || c.Conventions.TaskConventions[0].Entries[0].Status != "declared_default" {
		t.Fatal("actual Builder did not receive current default", c.Conventions)
	}
}
func TestNativeS5ConventionsReuseFrozenAnalysisBindingAndRejectDrift(t *testing.T) {
	f := newStageAnswerFixtureMode(t, true, configureContextConventions(t))
	p := f.packets[0]
	artifact, err := workpacket.ReadTaskAnalysis(f.root, p)
	if err != nil {
		t.Fatal(err)
	}
	output := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner)
	var binding struct {
		Context struct {
			Path string `json:"path"`
		} `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(output), &binding); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.root, binding.Context.Path))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Conventions *projectconventions.Bundle `json:"conventions"`
		Inputs      map[string]string          `json:"frozen_inputs"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(c.Conventions.TaskConventions)
	var left, right any
	json.Unmarshal(expected, &left)
	json.Unmarshal(artifact.TaskConventions, &right)
	if !reflect.DeepEqual(left, right) {
		t.Fatal("native recomputed S5 frozen defaults")
	}
	if err := c.Conventions.ValidateInputs(c.Inputs); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, f.root)
	replay := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner)
	if replay != output || stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("fixed native replay refreshed or wrote")
	}
	path := filepath.Join(f.root, projectconventions.RegistryPath)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", p.Binding.Owner}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "source changed") {
		t.Fatalf("native convention drift permitted: %d %s", code, errOut.String())
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("failed native replay mutated Runtime")
	}
}

func TestNativeBuilderConventionsFixedReplayAndSourceDrift(t *testing.T) {
	f := coreBuilderFixture(t, configureContextConventions(t))
	output := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", "core-builder")
	var binding struct {
		Context struct {
			Path string `json:"path"`
		} `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(output), &binding); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.root, binding.Context.Path))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Conventions *projectconventions.Bundle `json:"conventions"`
		Inputs      map[string]string          `json:"frozen_inputs"`
		Reading     []string                   `json:"input_read_order"`
		Writes      []string                   `json:"product_write_paths"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.Conventions == nil || len(c.Conventions.TaskConventions) != 1 || c.Conventions.TaskConventions[0].Entries[0].Status != "declared_default" {
		t.Fatal("native Builder lacks current scoped choice")
	}
	if err := c.Conventions.ValidateInputs(c.Inputs); err != nil {
		t.Fatal(err)
	}
	read := false
	for _, path := range c.Reading {
		if path == "src/value.go" {
			read = true
		}
	}
	if !read {
		t.Fatal("native actual example omitted from reading order")
	}
	before := stageSnapshot(t, f.root)
	if replay := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", "core-builder"); replay != output || stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("native replay refreshed fixed choice")
	}
	rule := filepath.Join(f.root, "docs/rules/error-handling.md")
	body, err := os.ReadFile(rule)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rule, append(body, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", f.root, "--agent-id", "core-builder"}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "source changed") {
		t.Fatalf("drifted native default replayed: %d %s", code, errOut.String())
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("failed native Builder replay wrote Runtime")
	}
}
