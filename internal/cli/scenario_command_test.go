package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestScenarioCLIJourneyFromGenerationToSpecGate(t *testing.T) {
	root := writeScenarioFixture(t, "investor-workbench")

	stdout, stderr, code := runCLI(t, root, "scenario", "generate", "--module", "investor-workbench", "--root", root)
	if code != 0 {
		t.Fatalf("scenario generate failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	assertJSONField(t, stdout, "module", "investor-workbench")

	stdout, stderr, code = runCLI(t, root, "scenario", "validate", "--module", "investor-workbench", "--root", root)
	if code != 0 {
		t.Fatalf("scenario validate failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	assertJSONField(t, stdout, "module", "investor-workbench")

	_, stderr, code = runCLI(t, root, "scenario", "validate", "--module", "investor-workbench", "--root", root, "--require-specs")
	if code == 0 || !strings.Contains(stderr, "browser spec coverage incomplete") || !strings.Contains(stderr, "cases 0/2") {
		t.Fatalf("missing CASE/PATH references must fail require-specs: code=%d stderr=%s", code, stderr)
	}

	specPath := filepath.Join(root, "web", "e2e", "investor-workbench", "workbench.spec.ts")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte(`import { test } from "@playwright/test";

test("module journey", async ({ page }) => {
  await page.getByTestId("CASE-ALLOW PATH-INVESTOR").click();
  await page.getByTestId("CASE-REJECT PATH-INVESTOR").click();
});
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, root, "scenario", "validate", "--module", "investor-workbench", "--root", root, "--require-specs")
	if code != 0 {
		t.Fatalf("complete module spec rejected: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	casesPath := filepath.Join(root, "docs", "design", "prototypes", "investor-workbench", "cases.json")
	cases, err := os.ReadFile(casesPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(casesPath, append(cases, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runCLI(t, root, "scenario", "validate", "--module", "investor-workbench", "--root", root)
	if code == 0 || !strings.Contains(stderr, "generated") {
		t.Fatalf("tampered generated output must fail: code=%d stderr=%s", code, stderr)
	}
}

func TestScenarioCLIRequireSpecsRejectsNonPlaywrightAndTitleOnlyReferences(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{
			name: "local fake test function",
			spec: `const test = (title: string, callback: () => void) => callback();
test("local fake", () => {
  const refs = "CASE-ALLOW CASE-REJECT PATH-INVESTOR";
  void refs;
});
`,
		},
		{
			name: "references only in title",
			spec: `import { test } from "@playwright/test";
test("CASE-ALLOW CASE-REJECT PATH-INVESTOR", async ({ page }) => {
  await page.goto("/investor-workbench");
});
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeScenarioFixture(t, "investor-workbench")
			if _, stderr, code := runCLI(t, root, "scenario", "generate", "--module", "investor-workbench", "--root", root); code != 0 {
				t.Fatalf("fixture generation failed: code=%d stderr=%s", code, stderr)
			}
			specPath := filepath.Join(root, "web", "e2e", "investor-workbench", "workbench.spec.ts")
			if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(specPath, []byte(test.spec), 0o644); err != nil {
				t.Fatal(err)
			}
			_, stderr, code := runCLI(t, root, "scenario", "validate", "--module", "investor-workbench", "--root", root, "--require-specs")
			if code == 0 || !strings.Contains(stderr, "browser spec coverage incomplete") {
				t.Fatalf("invalid Playwright evidence accepted: code=%d stderr=%s", code, stderr)
			}
		})
	}
}

func TestScenarioCLIValidateAllAndRejectsInvalidArguments(t *testing.T) {
	root := writeScenarioFixture(t, "investor-workbench")
	if _, stderr, code := runCLI(t, root, "scenario", "generate", "--module", "investor-workbench", "--root", root); code != 0 {
		t.Fatalf("fixture generation failed: code=%d stderr=%s", code, stderr)
	}

	stdout, stderr, code := runCLI(t, root, "scenario", "validate", "--all", "--root", root)
	if code != 0 {
		t.Fatalf("scenario validate --all failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "investor-workbench") {
		t.Fatalf("validate --all output must identify module: %s", stdout)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "validate requires selector", args: []string{"scenario", "validate", "--root", root}, want: "exactly one"},
		{name: "validate rejects both selectors", args: []string{"scenario", "validate", "--module", "investor-workbench", "--all", "--root", root}, want: "mutually exclusive"},
		{name: "generate rejects require specs", args: []string{"scenario", "generate", "--module", "investor-workbench", "--require-specs", "--root", root}, want: "only supported for validate"},
		{name: "generate rejects traversal", args: []string{"scenario", "generate", "--module", "../investor-workbench", "--root", root}, want: "invalid module"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, root, test.args...)
			if code == 0 || !strings.Contains(strings.ToLower(stderr), strings.ToLower(test.want)) {
				t.Fatalf("invalid invocation accepted or unclear: code=%d stderr=%s", code, stderr)
			}
		})
	}
}

func runCLI(t *testing.T, root string, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(args, strings.NewReader(""), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func assertJSONField(t *testing.T, output, field, want string) {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		t.Fatalf("CLI output is not JSON: %v; output=%s", err, output)
	}
	if got, _ := value[field].(string); got != want {
		t.Fatalf("CLI JSON field %s=%q, want %q; output=%s", field, got, want, output)
	}
}

func writeScenarioFixture(t *testing.T, module string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "design", "prototypes", module)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScenarioJSONFile(t, filepath.Join(dir, "scenario-model.json"), map[string]any{
		"module": module, "coverage_profile": "ordinary",
		"facts": []any{map[string]any{"id": "fact-investor", "partitions": []any{
			map[string]any{"id": "institutional", "value": "institutional"},
			map[string]any{"id": "individual", "value": "individual"},
		}}},
		"rules": []any{map[string]any{"id": "rule-investor", "source_refs": []any{"REQ-001"}, "risk": "ordinary", "branches": []any{
			map[string]any{
				"id": "branch-allow", "case_id": "CASE-ALLOW", "title": "Allow institutional filing", "polarity": "positive", "required": true,
				"witness":    map[string]any{"fact-investor": "institutional"},
				"oracle":     map[string]any{"visible": []any{"form"}, "terminal_state": "submitted", "persisted_effects": []any{"filing-created"}, "forbidden_side_effects": []any{"duplicate-filing"}},
				"fixture_id": "fixture-investor", "story_refs": []any{"S-001"}, "flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true,
			},
			map[string]any{
				"id": "branch-reject", "case_id": "CASE-REJECT", "title": "Reject individual filing", "polarity": "negative", "required": true,
				"witness": map[string]any{"fact-investor": "individual"},
				"oracle": map[string]any{
					"visible": []any{"validation-error"}, "terminal_state": "draft", "persisted_effects": []any{"draft-retained"},
					"rejection": "institutional-only", "expected_state": "draft", "forbidden_side_effects": []any{"filing-created"}, "recovery": "select-institutional",
				},
				"fixture_id": "fixture-investor", "story_refs": []any{"S-001"}, "flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true,
			},
		}}},
	})
	writeScenarioJSONFile(t, filepath.Join(dir, "fixture-contract.json"), map[string]any{
		"module": module, "fixtures": []any{map[string]any{"id": "fixture-investor", "persona": "operator", "synthetic": true, "setup": []any{"seed-investor"}, "cleanup": []any{"delete-investor"}}},
	})
	writeScenarioJSONFile(t, filepath.Join(dir, "cross-matrix.json"), map[string]any{
		"module": module, "entries": []any{map[string]any{
			"fact": "fact-investor", "req_ref": "REQ-001", "story": "S-001", "branch": "branch-allow",
		}},
	})
	if err := os.WriteFile(filepath.Join(dir, "stories.md"), []byte("# S-001\n\nREQ-001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flows.md"), []byte("# F-001\n\n### PATH-INVESTOR\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeScenarioJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
