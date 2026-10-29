package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/designfoundation"
)

func TestDesignFoundationNAReportsOnlyLockedUIChangedContradiction(t *testing.T) {
	for _, declaration := range []string{"| Design Foundation | N/A |", "| Product surfaces | none |"} {
		t.Run(declaration, func(t *testing.T) {
			for _, tc := range []struct {
				name, header string
				conflict     bool
			}{
				{"locked changed", "> 状态：locked\n> UI impact：changed", true},
				{"english locked changed", "> status: locked\n> UI impact: changed", true},
				{"draft changed", "> 状态：draft\n> UI impact：changed", false},
				{"locked none", "> 状态：locked\n> UI impact：none", false},
				{"locked unknown", "> 状态：locked\n> UI impact：unknown", false},
				{"prose only", "> 状态：locked\n\nExample: UI impact changed", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := t.TempDir()
					for rel, body := range map[string]string{
						"docs/project-map.md":          "# Map\n\n" + declaration + "\n",
						"docs/requirements/REQ-731.md": "# REQ-731\n\n" + tc.header + "\n",
					} {
						path := filepath.Join(root, rel)
						if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, []byte(body), 0600); err != nil {
							t.Fatal(err)
						}
					}
					before := dryRunTree(t, root)
					for _, strict := range []bool{false, true} {
						args := []string{"design-foundation", "check", "--root", root, "--json"}
						if strict {
							args = append(args, "--strict")
						}
						out, diagnostic, code := runCLI(t, root, args...)
						wantCode := 0
						if strict && tc.conflict {
							wantCode = 1
						}
						if code != wantCode {
							t.Errorf("NA contradiction exit scope wrong: strict=%v code=%d want=%d stderr=%s", strict, code, wantCode, diagnostic)
						}
						var report designfoundation.Report
						if err := json.Unmarshal([]byte(out), &report); err != nil {
							t.Fatal(err)
						}
						if !report.Skipped {
							t.Error("NA unexpectedly ran all UI language checks")
						}
						warnings := report.Warnings()
						if tc.conflict {
							if len(warnings) != 1 || warnings[0].Code != "foundation_na_conflict" || warnings[0].Path != "docs/requirements/REQ-731.md" || !strings.Contains(warnings[0].Detail, "docs/project-map.md") || !strings.Contains(warnings[0].Detail, "Main") {
								t.Errorf("locked UI change silently skipped or vague next owner: %+v", report)
							}
						} else if len(warnings) != 0 {
							t.Errorf("NA without actual locked UI change acquired needless work: %+v", warnings)
						}
						if !reflect.DeepEqual(before, dryRunTree(t, root)) {
							t.Fatal("advisory applicability check wrote project files")
						}
					}
				})
			}
		})
	}
}
