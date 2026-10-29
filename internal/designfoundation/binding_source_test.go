package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBindingSourceChecksLiteralFilesWithoutInventingComponentRegistry(t *testing.T) {
	const component = "src/ui/Confirm.tsx"
	for _, tc := range []struct {
		name, binding               string
		exists, directory, reserved bool
		missing                     bool
	}{
		{name: "missing literal file", binding: component, missing: true},
		{name: "existing file is not proof of product use", binding: component, exists: true},
		{name: "directory is not a component file", binding: component, directory: true, missing: true},
		{name: "valid token cannot hide missing trailing source", binding: "color.action.promise, " + component, missing: true},
		{name: "valid token cannot hide missing leading source", binding: component + ", color.action.promise", missing: true},
		{name: "no extension resolution", binding: "packages/ui/Confirm"},
		{name: "no new pattern registry", binding: "PAT-existing-notation"},
		{name: "no module alias resolution", binding: "@product/ui/Confirm.tsx"},
		{name: "no external URL resolution", binding: "https://example.invalid/Confirm.tsx"},
		{name: "reserved source not required", binding: component, reserved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			grammar := grammarWithRole("ROLE-action-promise", tc.binding)
			if tc.reserved {
				grammar = strings.Replace(grammar, "| "+tc.binding+" | active |", "| "+tc.binding+" | reserved |", 1)
			}
			files := map[string]string{KernelRel: kernelWithVersion("v1.0.0"), GrammarRel: grammar, TokensJSONRel: tokensJSONForTest(t)}
			mustWriteTreeGeneric(t, root, files)
			if tc.exists {
				// Deliberately not imported by any product: existence must not be
				// confused with a claim that the component is actually consumed.
				mustWriteTreeGeneric(t, root, map[string]string{component: "export const Confirm = () => null;\n"})
			} else if tc.directory {
				if err := os.MkdirAll(filepath.Join(root, component), 0755); err != nil {
					t.Fatal(err)
				}
			}
			check := func(wantMissing bool) {
				t.Helper()
				report, err := Check(root)
				if err != nil {
					t.Fatal(err)
				}
				var sourceFindings []Finding
				for _, f := range report.Findings {
					if f.Code == "semantic_role_unbound" {
						sourceFindings = append(sourceFindings, f)
					}
				}
				if !wantMissing && len(sourceFindings) > 0 {
					t.Fatalf("binding acquired unsupported resolution/registration work: %+v", sourceFindings)
				}
				if wantMissing {
					if len(sourceFindings) != 1 || sourceFindings[0].Path != GrammarRel || !strings.Contains(sourceFindings[0].Detail, component) || !strings.Contains(sourceFindings[0].Detail, "ROLE-action-promise") {
						t.Errorf("declared literal source missing without an accurate diagnosis: %+v", sourceFindings)
					}
				}
			}
			check(tc.missing)
			if tc.name == "missing literal file" {
				mustWriteTreeGeneric(t, root, map[string]string{component: "export const Confirm = () => null;\n"})
				check(false)
			}
			for rel, want := range files {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil || string(data) != want {
					t.Fatalf("binding diagnosis changed source %s: %v", rel, err)
				}
			}
		})
	}
}
