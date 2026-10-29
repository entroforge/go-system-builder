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
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS5ConventionsFreezeOriginalRulesInstallAndRejectDrift(t *testing.T) {
	f := newStageAnswerFixtureMode(t, true, func(root string, state map[string]any) {
		path := "docs/rules/error-handling.md"
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		entry := projectconventions.Entry{ID: "CNV-ERROR-01", Topic: "error-handling", Scope: []string{"src"}, Default: "Do not swallow errors.", SourceRefs: []projectconventions.Reference{{Kind: "rule", ID: "R-ERR-01", Path: path, Anchor: "3-hard-rules", SHA256: workpacket.Digest(body)}}, ExampleRefs: []projectconventions.Reference{}, Exceptions: []string{}, Status: "approved"}
		example := []byte("package reference\nimport \"errors\"\nfunc Translate() error { return errors.New(\"E_DEPENDENCY\") }\n")
		if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "src/reference-errors.go"), example, 0600); err != nil {
			t.Fatal(err)
		}
		entry.ExampleRefs = []projectconventions.Reference{{Path: "src/reference-errors.go", SHA256: workpacket.Digest(example)}}
		stageJSON(t, root, projectconventions.RegistryPath, projectconventions.Registry{SchemaVersion: "1.0.0", Entries: []projectconventions.Entry{entry}})
		for _, raw := range state["documents"].([]any) {
			doc := raw.(map[string]any)
			if doc["kind"] != "task" {
				continue
			}
			path := doc["path"].(string)
			body, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			body = append(body, []byte("\n## 4. Scope\n| Type | Paths |\n|---|---|\n| prospective write paths | src/errors.go |\n")...)
			if err := os.WriteFile(filepath.Join(root, path), body, 0600); err != nil {
				t.Fatal(err)
			}
			doc["sha256"] = workpacket.Digest(body)
		}
	})
	p := f.packets[0]
	artifact, err := workpacket.ReadTaskAnalysis(f.root, p)
	if err != nil {
		t.Fatal(err)
	}
	var frozen []projectconventions.Projection
	if err := json.Unmarshal(artifact.TaskConventions, &frozen); err != nil {
		t.Fatal(err)
	}
	if artifact.ConventionsResolverVersion != "1.0.0" || len(frozen) != 1 || len(frozen[0].Entries) != 1 || frozen[0].Entries[0].Status != "declared_default" {
		t.Fatalf("current Rules default absent: %+v", frozen)
	}
	if len(frozen[0].Sources) != 4 {
		t.Fatal("registry/Rules index/source closure incomplete")
	}
	registry := filepath.Join(f.root, projectconventions.RegistryPath)
	original, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry, append(original, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, f.root)
	var out, errOut bytes.Buffer
	code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", f.root, "--assignment", p.Binding.AssignmentID, "--agent", p.Binding.Owner}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "uncommitted inputs must be resolved") || !strings.Contains(errOut.String(), projectconventions.RegistryPath) {
		t.Fatalf("dirty registry prepared as frozen: %d %s", code, errOut.String())
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("failed frozen preparation wrote Runtime")
	}
	if err := os.WriteFile(registry, original, 0600); err != nil {
		t.Fatal(err)
	}
	e := f.prepare(t, p)
	for _, source := range frozen[0].Sources {
		data, err := os.ReadFile(filepath.Join(e.Path, ".claude/inputs", source.Path))
		if err != nil || workpacket.Digest(data) != source.SHA256 {
			t.Fatal("convention source was not frozen in Worker", source, err)
		}
		if source.SourceMode != "git_tree" {
			t.Fatal("formal source mislabeled", source)
		}
	}
	out.Reset()
	errOut.Reset()
	code = cli.Run([]string{"runtime", "workspace", "review-check", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
	var result semantic.TaskCheckResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("review-check: %d %v %s", code, err, errOut.String())
	}
	if !reflect.DeepEqual(result.TaskConventions, frozen) {
		t.Fatal("review-check recomputed frozen conventions")
	}
	f.begin(t, p, &e)
	if err := os.WriteFile(registry, append(original, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, e.Path, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	before = stageSnapshot(t, f.root)
	out.Reset()
	errOut.Reset()
	code = cli.Run([]string{"runtime", "workspace", "answer", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "frozen execution input changed") {
		t.Fatalf("changed convention source received: %d %s", code, errOut.String())
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("drifted convention reception wrote Runtime")
	}
	if err := os.WriteFile(registry, original, 0600); err != nil {
		t.Fatal(err)
	}
	receipt := saveStageHook(t, f.root, e.Path, e.AgentID, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	if !receipt.WorkComplete {
		t.Fatal("restored original receiver failed", receipt)
	}
	nextJSON := stageCLI(t, "runtime", "stage-answer", "revise", "--root", f.root, "--previous", receipt.SubmissionKey, "--reason", "Recheck the original indexed rule choice")
	var next workpacket.Packet
	if err := json.Unmarshal([]byte(nextJSON), &next); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.TaskAnalysis, p.TaskAnalysis) {
		t.Fatal("revision changed original convention snapshot")
	}
}
