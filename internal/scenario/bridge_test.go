package scenario_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/scenario"
)

// bridgeFixture builds a root with a bound REQ (FR/AC/NFR tables), a
// scenario module whose rule cites REQ-001/FR-001, and a runtime state
// binding REQ-001 — the minimum for the AC bridge.
func bridgeFixture(t *testing.T, reqBody string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-001.md"), []byte(reqBody), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "docs", "design", "prototypes", "investor-workbench")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "scenario-model.json"), map[string]any{
		"module": "investor-workbench", "coverage_profile": "ordinary",
		"facts": []any{map[string]any{"id": "fact-x", "partitions": []any{
			map[string]any{"id": "p1", "value": "v1"}, map[string]any{"id": "p2", "value": "v2"},
		}}},
		"rules": []any{map[string]any{
			"id": "rule-x", "source_refs": []any{"REQ-001/FR-001"}, "risk": "ordinary",
			"branches": []any{
				map[string]any{"id": "b-pos", "case_id": "CASE-X-001", "title": "pos", "polarity": "positive", "required": true,
					"witness": map[string]any{"fact-x": "p1"}, "oracle": map[string]any{"visible": []any{"v"}, "terminal_state": "ok", "persisted_effects": []any{"e"}, "forbidden_side_effects": []any{"f"}},
					"fixture_id": "fix", "story_refs": []any{"S-001"}, "flow_refs": []any{"F-001"}, "browser_required": false},
				map[string]any{"id": "b-neg", "case_id": "CASE-X-002", "title": "neg", "polarity": "negative", "required": true,
					"witness": map[string]any{"fact-x": "p2"}, "oracle": map[string]any{"visible": []any{"e"}, "terminal_state": "rejected", "persisted_effects": []any{"n"}, "rejection": "r", "expected_state": "rejected", "forbidden_side_effects": []any{"f"}, "recovery": "fix"},
					"fixture_id": "fix", "story_refs": []any{"S-001"}, "flow_refs": []any{"F-001"}, "browser_required": false},
			},
		}},
	})
	writeJSON(t, filepath.Join(dir, "fixture-contract.json"), map[string]any{
		"module": "investor-workbench", "fixtures": []any{map[string]any{"id": "fix", "persona": "op", "synthetic": true, "setup": []any{"s"}, "cleanup": []any{"c"}}},
	})
	writeJSON(t, filepath.Join(dir, "cross-matrix.json"), map[string]any{
		"module": "investor-workbench", "entries": []any{
			map[string]any{"fact": "fact-x", "req_ref": "REQ-001/FR-001", "story": "S-001", "branch": "b-pos"},
			map[string]any{"fact": "fact-x", "req_ref": "REQ-001/FR-001", "story": "S-001", "branch": "b-neg"},
		},
	})
	if err := os.WriteFile(filepath.Join(dir, "stories.md"), []byte("# S-001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flows.md"), []byte("# F-001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(root, ".claude", "loop-state.json"), map[string]any{
		"runtime_id": "loop-REQ-001",
		"lifecycle":  map[string]any{"state": "planning", "phase": "design"},
		"bound_req":  map[string]any{"id": "REQ-001", "path": "docs/requirements/REQ-001.md"},
	})
	return root
}

const bridgeReqBody = `# REQ-001

> 状态：locked
> 版本：v1.0.0
> UI impact：changed

## §C 具体需求

| 编号 | 模块 | 需求 | 服务于 | 优先级 |
|:--|:--|:--|:--|:--|
| FR-001 | invest | 允许机构提交 | A1 | Must |
| FR-002 | invest | 审计日志 | A1 | Should |

| 编号 | 标准 | 指向 |
|:--|:--|:--|
| AC-001 | 机构可提交 | FR-001 |
| AC-002 | 性能达标 | NFR-001 |
| AC-003 | 不做移动端 | §A4-1 |

| 编号 | 类型 | 要求 | 验收标准 |
|:--|:--|:--|:--|
| NFR-001 | 性能 | p99<200ms | 压测报告 |
`

func TestBridgeSourceCheckPassesAndEndorsesNA(t *testing.T) {
	root := bridgeFixture(t, bridgeReqBody)
	result, err := scenario.RunBridge(root, false)
	if err != nil {
		t.Fatalf("bridge failed: %v", err)
	}
	if result.TotalAC != 3 || result.ReachedCases != 1 || result.EndorsedNA != 2 {
		t.Fatalf("bridge counts = total %d reached %d na %d, want 3/1/2", result.TotalAC, result.ReachedCases, result.EndorsedNA)
	}
}

func TestBridgeRejectsUnreachableFR(t *testing.T) {
	body := strings.Replace(bridgeReqBody, "| AC-003 | 不做移动端 | §A4-1 |", "| AC-003 | 审计可查 | FR-002 |", 1)
	root := bridgeFixture(t, body)
	_, err := scenario.RunBridge(root, false)
	if err == nil || !strings.Contains(err.Error(), "REQ-001/FR-002") {
		t.Fatalf("unreachable FR must be rejected with the exact chain, got: %v", err)
	}
}

func TestBridgeRejectsFreeTextNA(t *testing.T) {
	body := strings.Replace(bridgeReqBody, "| AC-003 | 不做移动端 | §A4-1 |", "| AC-003 | 手动验证 | manual-check |", 1)
	root := bridgeFixture(t, body)
	_, err := scenario.RunBridge(root, false)
	if err == nil || !strings.Contains(err.Error(), "not endorsed N/A") {
		t.Fatalf("free-text N/A must be rejected, got: %v", err)
	}
}

func TestBridgeRejectsUndeclaredNFR(t *testing.T) {
	body := strings.Replace(bridgeReqBody, "| AC-002 | 性能达标 | NFR-001 |", "| AC-002 | 性能达标 | NFR-999 |", 1)
	root := bridgeFixture(t, body)
	_, err := scenario.RunBridge(root, false)
	if err == nil || !strings.Contains(err.Error(), "NFR-999") {
		t.Fatalf("undeclared NFR endorsement must be rejected, got: %v", err)
	}
}

func TestBridgeSkipsWithoutBoundREQ(t *testing.T) {
	root := bridgeFixture(t, bridgeReqBody)
	os.Remove(filepath.Join(root, ".claude", "loop-state.json"))
	result, err := scenario.RunBridge(root, false)
	if err != nil {
		t.Fatalf("bridge must skip without a bound REQ, got %v", err)
	}
	if len(result.IgnoredEntries) == 0 {
		t.Fatal("skip must be reported, not silent")
	}
}

func TestBridgeCurrentTablesAndMultipleFRs(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"indented tab section", strings.Replace(bridgeReqBody, "## §C 具体需求", "  ##\t§C\t具体需求", 1) + "\n## §F 历史\n| AC-999 | old | FR-999 |\n", ""},
		{"history outside current section", bridgeReqBody + "\n## §F 历史\n| AC-999 | old | FR-999 |\n", ""},
		{"fenced history", bridgeReqBody + "\n~~~markdown\n| AC-999 | old | FR-999 |\n~~~\n", ""},
		{"backtick history", bridgeReqBody + "\n```markdown\n| AC-999 | old | FR-999 |\n```\n", ""},
		{"legacy with fenced section", "# REQ-001\n| FR-001 | submit | Must |\n| AC-001 | submit | FR-001 |\n```markdown\n## §C old\n| AC-999 | old | FR-999 |\n```\n", ""},
		{"legacy fixture", "# REQ-001\n| FR-001 | submit | Must |\n| AC-001 | submit | FR-001 |\n", ""},
		{"multiple covered", strings.Replace(bridgeReqBody, "| FR-002", "| FR-003", 1), ""},
		{"multiple missing coverage", strings.Replace(bridgeReqBody, "| AC-001 | 机构可提交 | FR-001 |", "| AC-001 | 机构可提交 | FR-001, FR-002 |", 1), "REQ-001/FR-002"},
		{"undeclared reference", strings.Replace(bridgeReqBody, "| AC-001 | 机构可提交 | FR-001 |", "| AC-001 | 机构可提交 | FR-001, FR-999 |", 1), "FR-999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := bridgeFixture(t, tc.body)
			if tc.name == "multiple covered" {
				path := filepath.Join(root, "docs/design/prototypes/investor-workbench/scenario-model.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = []byte(strings.Replace(string(data), `"REQ-001/FR-001"`, `"REQ-001/FR-001", "REQ-001/FR-003"`, 1))
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
				reqPath := filepath.Join(root, "docs/requirements/REQ-001.md")
				body := strings.Replace(tc.body, "| AC-001 | 机构可提交 | FR-001 |", "| AC-001 | 机构可提交 | FR-001，FR-003 |", 1)
				if err := os.WriteFile(reqPath, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := scenario.RunBridge(root, true)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want %s, got %v", tc.want, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ReachedCases != 1 {
				t.Fatalf("one AC must count once: %+v", got)
			}
		})
	}
}

func TestBridgeMixedTargetsOrderAndEveryReference(t *testing.T) {
	for _, tc := range []struct {
		name, target, want string
		reached, na        int
	}{
		{"FR first", "FR-001, NFR-001, §A4-1", "", 1, 2},
		{"NFR first", "NFR-001，§A4-1、FR-001", "", 1, 2},
		{"A4 first", "§A4-1; FR-001；NFR-001", "", 1, 2},
		{"multiple NFR", "NFR-001, NFR-002", "", 0, 3},
		{"NFR and A4", "§A4-1 NFR-001", "", 0, 3},
		{"undeclared second NFR", "NFR-001, NFR-999", "NFR-999", 0, 0},
		{"mixed missing NFR", "FR-001, NFR-999", "NFR-999", 0, 0},
		{"uncovered FR after NFR", "NFR-001, FR-002", "REQ-001/FR-002", 0, 0},
		{"uncovered FR after A4", "§A4-1, FR-002", "REQ-001/FR-002", 0, 0},
		{"undeclared FR after A4", "§A4-1, FR-999", "FR-999", 0, 0},
		{"free text after A4", "§A4-1, manual-check", "not endorsed N/A", 0, 0},
		{"separators only", ",；、", "not endorsed N/A", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(bridgeReqBody, "| AC-001 | 机构可提交 | FR-001 |", "| AC-001 | 机构可提交 | "+tc.target+" |", 1)
			body += "\n| NFR-002 | 可靠性 | stable | check |\n"
			root := bridgeFixture(t, body)
			for _, branches := range []bool{false, true} {
				got, err := scenario.RunBridge(root, branches)
				if tc.want != "" {
					if err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("want %q, got %v", tc.want, err)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if got.TotalAC != 3 || got.ReachedCases != tc.reached || got.EndorsedNA != tc.na {
					t.Fatalf("counts: %+v", got)
				}
			}
		})
	}
}

func TestBridgeGuardWithoutModulesFindsFRInEveryPosition(t *testing.T) {
	for _, target := range []string{"FR-001, NFR-001", "NFR-001, FR-001", "§A4-1, FR-001"} {
		t.Run(target, func(t *testing.T) {
			body := strings.Replace(bridgeReqBody, "| AC-001 | 机构可提交 | FR-001 |", "| AC-001 | 机构可提交 | "+target+" |", 1)
			root := bridgeFixture(t, body)
			if err := os.RemoveAll(filepath.Join(root, "docs/design/prototypes")); err != nil {
				t.Fatal(err)
			}
			err := scenario.GuardBridgeChecked(root)
			if err == nil || !strings.Contains(err.Error(), "FR-001") || !strings.Contains(err.Error(), "no module packages") {
				t.Fatalf("missing FR package must be rejected: %v", err)
			}
		})
	}
}
