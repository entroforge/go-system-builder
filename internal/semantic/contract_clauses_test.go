package semantic_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// Exercise the current batch reader, not just the declaration parser: a
// reference in the real index must resolve to this contract's own table.
func TestContractClauseReferencesRequireOwnTableDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name, target, citation string
		valid                  bool
	}{
		{"bare declaration", "| 本合同条款 | 承诺 |\n|---|---|\n| §1 | Keep the agreed behavior |\n", "BE-001 §1", true},
		{"qualified declaration reordered column", "| 承诺 | **本合同条款** |\n|---|---|\n| Keep behavior | `BE-001 §1` |\n", "BE-001 §1", true},
		{"original decorated header", "| 本合同条款§ | 承诺 |\n|---|---|\n| §1 | Keep behavior |\n", "BE-001 §1", true},
		{"multiple declarations and tables", "| 本合同条款 | 承诺 |\n|---|---|\n| §1, BE-001 §2 | Two obligations |\n\n## Another mapping\n| 本合同条款 | 承诺 |\n|---|---|\n| §10 | Another obligation |\n", "BE-001 §1, BE-001 §2, BE-001 §10", true},
		{"prose mention", "See §1 for a future promise.\n", "BE-001 §1", false},
		{"other column", "| 本合同条款 | 参考 |\n|---|---|\n| §2 | See §1 |\n", "BE-001 §1", false},
		{"foreign declaration", "| 本合同条款 | 承诺 |\n|---|---|\n| BE-002 §1 | A historical contract |\n", "BE-001 §1", false},
		{"fenced example", "```markdown\n| 本合同条款 | 承诺 |\n|---|---|\n| §1 | An example only |\n```\n", "BE-001 §1", false},
		{"tilde fenced example", "~~~markdown\n| 本合同条款 | 承诺 |\n|---|---|\n| §1 | An example only |\n~~~\n", "BE-001 §1", false},
		{"missing table separator", "| 本合同条款 | 承诺 |\n| §1 | Not a declared Markdown table |\n", "BE-001 §1", false},
		{"unrelated later table", "| 本合同条款 | 承诺 |\n|---|---|\n| §2 | An actual declaration |\n\n| 参考 | 备注 |\n|---|---|\n| §1 | Reference only |\n", "BE-001 §1", false},
		{"exact number", "| 本合同条款 | 承诺 |\n|---|---|\n| §10 | Tenth obligation |\n", "BE-001 §1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := contractScopeFixture(t)
			scopePut(t, root, "docs/dev/contracts/CONTRACTS-001.md", scopeIndex("REQ-001", "BE-001")+"\n## 需求覆盖矩阵\n| REQ | 条款 |\n|---|---|\n| REQ-001/FR-001 | "+tc.citation+" |\n")
			scopePut(t, root, "docs/dev/contracts/BE-001.md", "# Current\nCASE-CURRENT FR-001\n\n"+tc.target)
			result, err := semantic.ContractsCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-001")
			if err != nil {
				t.Fatal(err)
			}
			problems := strings.Join(result.Problems, "\n")
			if tc.valid {
				if problems != "" || result.Clauses == 0 {
					t.Fatalf("real declaration rejected or unexamined: %+v", result)
				}
			} else if !strings.Contains(problems, "clause cell") || !strings.Contains(problems, "BE-001") {
				t.Fatalf("non-declaration satisfied current batch citation: %+v", result)
			}
		})
	}
}
