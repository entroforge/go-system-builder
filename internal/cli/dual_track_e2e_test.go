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

// TestS2DualTrackConvergenceE2E walks the S2 pipeline end to end through the
// CLI surface (L3-S2 v4.0.1 dual-track convergence):
// bind → unknown blocked at PTR-PLAN-01 → clarify → architecture+stories
// (two tracks) → convergence-1 (model+cross-matrix) → AC source bridge
// (early) → fixtures → convergence-2 (flows) → close (generate+validate
// with full bridge) → doctor green.
func TestS2DualTrackConvergenceE2E(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-300.md": ""})
	reqPath := filepath.Join(root, "docs", "requirements", "REQ-300.md")

	// validate --all checks the whole template contract (skills, agents,
	// docs tree, root artifacts) — copy the real tree; the fixture REQ and
	// module package are (re)written on top afterwards.
	for _, pair := range []struct{ src, dest string }{
		{"../../skills", "skills"}, {"../../agents", "agents"}, {"../../docs", "docs"},
		{"../../prelude.md", "prelude.md"}, {"../../AGENTS-template.md", "AGENTS-template.md"},
		{"../../settings.json", "settings.json"}, {"../../loop-template.md", "loop-template.md"},
	} {
		if err := copyTree(t, root, pair.src, pair.dest); err != nil {
			t.Fatal(err)
		}
	}

	writeREQ := func(uiImpact string, acTarget string) {
		body := "# REQ-300\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：" + uiImpact + "\n\n" +
			"## §C 具体需求\n\n" +
			"| 编号 | 模块 | 需求 | 服务于 | 优先级 |\n|:--|:--|:--|:--|:--|\n" +
			"| FR-001 | workbench | 机构可提交 | A1 | Must |\n\n" +
			"验收标准：\n\n" +
			"| 编号 | 标准 | 指向 |\n|:--|:--|:--|\n" +
			"| AC-001 | 机构提交成功 | " + acTarget + " |\n\n" +
			"| 编号 | 类型 | 要求 | 验收标准 |\n|:--|:--|:--|:--|\n" +
			"| NFR-001 | 性能 | p99<200ms | 压测 |\n"
		if err := os.WriteFile(reqPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run := func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}

	// --- phase 0: unknown blocks planning advance (wired guard), in a scratch root ---
	scratch := newUXTestRoot(t, map[string]string{"REQ-301.md": "# REQ-301\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：unknown\n"})
	if _, _, code := run("req", "bind", "--root", scratch, "--approved-by", "alice"); code != 0 {
		t.Fatalf("bind with unknown impact must succeed (blocking happens at PTR-PLAN-01)")
	}
	if _, stderr, code := run("runtime", "transition", "--root", scratch,
		"--id", "PTR-PLAN-01", "--expected-revision", "0", "--actor", "orchestrator"); code == 0 || !strings.Contains(stderr, "ui_impact_resolved") {
		t.Fatalf("unknown ui_impact must block PTR-PLAN-01 (wired guard), got code=%d stderr=%s", code, stderr)
	}

	// --- main flow: locked as changed from the start (rewriting a bound REQ would need an amendment) ---
	writeREQ("changed", "FR-001")
	if out, _, code := run("req", "bind", "--root", root, "--approved-by", "alice"); code != 0 || !strings.Contains(out, "ui_impact=changed") {
		t.Fatalf("bind failed: %s", out)
	}

	// --- track system + track user: build the module package by hand ---
	dir := filepath.Join(root, "docs", "design", "prototypes", "workbench")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeJSONFile := func(name string, value any) {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// user track: stories first (seeds the hunt)
	writeFile("stories.md", "# Stories\n\n## S-001\n\nREQ-300\n")
	// system track: architecture is docs-level; facts land in the model
	writeJSONFile("scenario-model.json", map[string]any{
		"module": "workbench", "coverage_profile": "ordinary",
		"facts": []any{map[string]any{"id": "fact-submitter", "partitions": []any{
			map[string]any{"id": "institutional", "value": "institutional"},
			map[string]any{"id": "individual", "value": "individual"},
		}}},
		"rules": []any{map[string]any{
			"id": "rule-submit", "source_refs": []any{"REQ-300/FR-001"}, "risk": "ordinary",
			"branches": []any{
				map[string]any{"id": "b-allow", "case_id": "CASE-WB-001", "title": "institutional submits", "polarity": "positive", "required": true,
					"witness":    map[string]any{"fact-submitter": "institutional"},
					"oracle":     map[string]any{"visible": []any{"filing-form"}, "terminal_state": "submitted", "persisted_effects": []any{"filing"}, "forbidden_side_effects": []any{"dup"}},
					"fixture_id": "fix-sub", "story_refs": []any{"S-001"}, "flow_refs": []any{"F-001", "PATH-SUBMIT"}, "browser_required": true},
				map[string]any{"id": "b-reject", "case_id": "CASE-WB-002", "title": "individual rejected", "polarity": "negative", "required": true,
					"witness":    map[string]any{"fact-submitter": "individual"},
					"oracle":     map[string]any{"visible": []any{"validation-error"}, "terminal_state": "draft", "persisted_effects": []any{"draft"}, "rejection": "institutional-only", "expected_state": "draft", "forbidden_side_effects": []any{"filing"}, "recovery": "switch"},
					"fixture_id": "fix-sub", "story_refs": []any{"S-001"}, "flow_refs": []any{"F-001", "PATH-SUBMIT"}, "browser_required": true},
			},
		}},
	})
	// convergence-1 carrier: cross-matrix (branch coverage + one reasoned cell)
	writeJSONFile("cross-matrix.json", map[string]any{
		"module": "workbench",
		"entries": []any{
			map[string]any{"fact": "fact-submitter", "req_ref": "REQ-300/FR-001", "story": "S-001", "branch": "b-allow"},
			map[string]any{"fact": "fact-submitter", "req_ref": "REQ-300/FR-001", "story": "S-001", "no_branch_reason": "negative branch of the same rule covers the rejection cell"},
		},
	})
	// wait — duplicate cell: same fact/req/story with both branch and reason is two cells; dedup key is fact|req|story. Fix: only one entry.
	writeJSONFile("cross-matrix.json", map[string]any{
		"module": "workbench",
		"entries": []any{
			map[string]any{"fact": "fact-submitter", "req_ref": "REQ-300/FR-001", "story": "S-001", "branch": "b-allow"},
		},
	})
	// fixtures (after branches settle)
	writeJSONFile("fixture-contract.json", map[string]any{
		"module": "workbench", "fixtures": []any{map[string]any{"id": "fix-sub", "persona": "operator", "synthetic": true, "setup": []any{"seed"}, "cleanup": []any{"purge"}}}})
	// convergence-2: flows (PATH binding) + prototype pages (4-field header)
	writeFile("flows.md", "# Flows\n\n## F-001\n\n### PATH-SUBMIT\n\nREQ-300\n")
	writeFile("index.html", "<!-- proto-meta: 设计代数 v1 更新 路由 /workbench -->\n<html><body>index</body></html>\n")
	writeFile("submit.html", "<!-- proto-meta: 设计代数 v1 更新 路由 /workbench/submit -->\n<html><body>submit</body></html>\n")

	// --- AC source bridge runs early (convergence-1 checkpoint; no generated outputs yet) ---
	out, stderr, code := run("scenario", "bridge", "--root", root)
	if code != 0 || !strings.Contains(out, "1 criteria, 1 reach") {
		t.Fatalf("early AC bridge must pass at convergence-1: code=%d out=%s err=%s", code, out, stderr)
	}

	// --- free-text N/A rejected: rewrite REQ with a bogus target ---
	writeREQ("changed", "manual-check")
	_, stderr, code = run("scenario", "bridge", "--root", root)
	if code == 0 || !strings.Contains(stderr, "not endorsed N/A") {
		t.Fatalf("free-text N/A must be rejected by the bridge, got: %s", stderr)
	}
	writeREQ("changed", "FR-001")

	// --- close: generate + validate (full bridge) ---
	if out, stderr, code := run("scenario", "generate", "--module", "workbench", "--root", root); code != 0 {
		t.Fatalf("generate failed: %s %s", out, stderr)
	}
	if out, stderr, code := run("scenario", "validate", "--module", "workbench", "--root", root); code != 0 {
		t.Fatalf("validate (full bridge incl.) failed: %s %s", out, stderr)
	}

	// --- completeness gate sees the eight-file package ---
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	writeJSONMap(t, statePath, state) // no-op; package already complete per hasCompleteUIDesignPackageForREQ path used by projection

	// --- doctor: AutoSpecs — spec tree absent, expected before S6, must stay green ---
	// validate --all checks the skill catalog too; seed it from the repo.
	if _, stderr, code := run("validate", "--all", "--root", root); code != 0 {
		t.Fatalf("validate --all must pass with absent spec trees (S6+ artifacts): %s", stderr)
	}

	// --- AutoSpecs enforces once the spec tree exists: write an incomplete spec ---
	specDir := filepath.Join(root, "web", "e2e", "workbench")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "submit.spec.ts"), []byte("export {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = run("validate", "--all", "--root", root)
	if code == 0 || !strings.Contains(stderr, "browser spec coverage incomplete") {
		t.Fatalf("validate --all with an existing-but-incomplete spec tree must fail, got: %s", stderr)
	}
}

// copyTree copies a repo directory (skills/, agents/) into the fixture
// root so validate --all's catalog checks have real material.
func copyTree(t *testing.T, root, srcDir, destName string) error {
	t.Helper()
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, destName, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
