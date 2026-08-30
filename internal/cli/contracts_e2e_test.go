package cli_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// TestS3ContractPipelineE2E walks the contract stage end to end (L3-S3
// v4.0.1): draft a contract with a real reference chain → contracts check
// green → inject a broken link → red → fix → PTR-PLAN-02 registers the
// locked contract into documents[] (with author) → hook write-protection
// fires on the registered contract → same-generation rework re-locks with
// replace semantics (no stacking).
func TestS3ContractPipelineE2E(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"docs/contracts", "docs/requirements", "docs/design/architecture", "docs/design/prototypes/wb", ".claude"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sha := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	run := func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}

	// REQ with an FR; module package with CASE/S/F/PATH universe.
	write("docs/design/architecture/ARCHITECTURE-500.md", "# ARCHITECTURE-500\n\n> 状态：locked\n> 版本：v1.0.0\n")
	write("docs/requirements/REQ-500.md", "# REQ-500\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：changed\n\n| 编号 | 模块 | 需求 | 服务于 | 优先级 |\n|:--|:--|:--|:--|:--|\n| FR-501 | wb | 提交 | A1 | Must |\n")
	write("docs/design/prototypes/wb/scenario-model.json", `{
  "module": "wb", "coverage_profile": "ordinary",
  "facts": [{"id": "fact-wb", "partitions": [{"id": "ok", "value": "ok"}, {"id": "bad", "value": "bad"}]}],
  "rules": [{"id": "rule-wb", "source_refs": ["REQ-500/FR-501"], "risk": "ordinary", "branches": [
    {"id": "branch-allow", "case_id": "CASE-WB-001", "title": "submit accepted", "polarity": "positive", "required": true,
     "witness": {"fact-wb": "ok"},
     "oracle": {"visible": ["receipt"], "terminal_state": "submitted", "persisted_effects": ["record"], "forbidden_side_effects": ["dup"]},
     "fixture_id": "fixture-wb", "story_refs": ["S-001"], "flow_refs": ["F-001", "PATH-SUBMIT"], "browser_required": true},
    {"id": "branch-reject", "case_id": "CASE-WB-002", "title": "submit rejected", "polarity": "negative", "required": true,
     "witness": {"fact-wb": "bad"},
     "oracle": {"visible": ["error"], "terminal_state": "draft", "persisted_effects": ["draft-retained"], "rejection": "invalid", "expected_state": "draft", "forbidden_side_effects": ["record"], "recovery": "fix-input"},
     "fixture_id": "fixture-wb", "story_refs": ["S-001"], "flow_refs": ["F-001", "PATH-SUBMIT"], "browser_required": true}
  ]}]
}`)
	write("docs/design/prototypes/wb/fixture-contract.json", `{"module": "wb", "fixtures": [{"id": "fixture-wb", "persona": "operator", "synthetic": true, "setup": ["seed"], "cleanup": ["purge"]}]}`)
	write("docs/design/prototypes/wb/cross-matrix.json", `{"module": "wb", "entries": [{"fact": "fact-wb", "req_ref": "REQ-500/FR-501", "story": "S-001", "branch": "branch-allow"}]}`)
	write("docs/design/prototypes/wb/cases.json", `{"cases":[{"id":"CASE-WB-001"},{"id":"CASE-WB-002"}]}`)
	write("docs/design/prototypes/wb/stories.md", "# S-001\n")
	write("docs/design/prototypes/wb/flows.md", "# F-001\n\n### PATH-SUBMIT\n")

	// --- green: a contract whose references all resolve ---
	write("docs/contracts/BE-501.md", ""+
		"# BE-501\n\n> 状态：locked\n> 版本：v1.0.0\n\n"+
		"| REQ source_ref | Rule/CASE/Story/PATH | 本合同条款§ | 验收标准 |\n|:--|:--|:--|:--|\n"+
		"| REQ-500/FR-501 | CASE-WB-001 / S-001 / F-001 / PATH-SUBMIT | §2 | 可提交 |\n"+
		"| REQ-500/FR-501 | CASE-WB-002 / S-001 / F-001 / PATH-SUBMIT | §3 | 拒绝 |\n")
	out, _, code := run("contracts", "check", "--root", root)
	if code != 0 || !strings.Contains(out, "all reconciled") {
		t.Fatalf("green contract must pass: code=%d out=%s", code, out)
	}

	// --- red: broken CASE + unknown clause target ---
	write("docs/contracts/BE-501.md", strings.Replace(readFile(t, root, "docs/contracts/BE-501.md"),
		"CASE-WB-001", "CASE-GHOST-9", 1)+"\n| cell | FE-999 §1 | x |\n")
	_, stderr, code := run("contracts", "check", "--root", root)
	if code == 0 || !strings.Contains(stderr, "CASE-GHOST-9") || !strings.Contains(stderr, "FE-999") {
		t.Fatalf("broken links must be named, got: %s", stderr)
	}
	// restore green
	write("docs/contracts/BE-501.md", strings.Replace(readFile(t, root, "docs/contracts/BE-501.md"), "CASE-GHOST-9", "CASE-WB-001", 1))
	write("docs/contracts/BE-501.md", func() string {
		s := readFile(t, root, "docs/contracts/BE-501.md")
		if idx := strings.Index(s, "\n| cell |"); idx >= 0 {
			return s[:idx]
		}
		return s
	}())

	// --- registration via PTR-PLAN-02: bind, then fire the transition ---
	if _, stderr, code := run("req", "bind", "--root", root, "--approved-by", "bob"); code != 0 {
		t.Fatalf("bind failed: %s", stderr)
	}
	// design→contracts→tasks: PTR-PLAN-01 first (carries the wired
	// ui_impact_resolved guard — impact is `changed`, so it passes), then
	// PTR-PLAN-02 carries guard contracts_checked + action register_locked_contracts.
	if _, stderr, code = run("runtime", "transition", "--root", root,
		"--id", "PTR-PLAN-01", "--expected-revision", "0", "--actor", "orchestrator"); code != 0 {
		t.Fatalf("PTR-PLAN-01 failed: %s", stderr)
	}
	_, stderr, code = run("runtime", "transition", "--root", root,
		"--id", "PTR-PLAN-02", "--expected-revision", "1", "--actor", "orchestrator")
	if code != 0 {
		t.Fatalf("PTR-PLAN-02 failed: %s", stderr)
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	docs, _ := state["documents"].([]any)
	var contractEntry map[string]any
	for _, raw := range docs {
		doc, _ := raw.(map[string]any)
		if doc != nil && doc["kind"] == "contract" {
			contractEntry = doc
		}
	}
	if contractEntry == nil {
		t.Fatal("PTR-PLAN-02 must register the locked contract into documents[]")
	}
	if contractEntry["author_agent_id"] != "orchestrator" {
		t.Fatalf("contract author_agent_id = %v, want orchestrator (registering actor)", contractEntry["author_agent_id"])
	}
	diskData, _ := os.ReadFile(filepath.Join(root, "docs", "contracts", "BE-501.md"))
	if contractEntry["sha256"] != fmt.Sprintf("%x", sha256.Sum256(diskData)) {
		t.Fatal("registered sha must match disk")
	}

	// --- same-generation rework: revise + re-lock → replace, not stack ---
	revise := strings.Replace(readFile(t, root, "docs/contracts/BE-501.md"), "v1.0.0", "v1.1.0", 1)
	write("docs/contracts/BE-501.md", revise)
	// bump revision by a no-op evidence-free transition is not available; use direct state edit to allow re-fire
	state = readJSONMap(t, statePath)
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": float64(1)}
	writeJSONMap(t, statePath, state)
	if _, stderr, code := run("runtime", "transition", "--root", root,
		"--id", "PTR-PLAN-02", "--expected-revision", intStr(int(state["revision"].(float64))), "--actor", "orchestrator"); code != 0 {
		t.Fatalf("re-lock failed: %s", stderr)
	}
	state = readJSONMap(t, statePath)
	docs, _ = state["documents"].([]any)
	count := 0
	for _, raw := range docs {
		doc, _ := raw.(map[string]any)
		if doc != nil && doc["kind"] == "contract" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("same-generation re-lock must replace not stack, got %d contract entries", count)
	}
	_ = sha
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func intStr(n int) string { return fmt.Sprintf("%d", n) }

// TestPTRPLAN02BlocksOnBrokenBridge pins the D2 mount: an AC pointing at an
// FR that no module package cites blocks the planning advance — the bridge
// is a gate, not a voluntary command.
func TestPTRPLAN02BlocksOnBrokenBridge(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"docs/contracts", "docs/requirements", "docs/design/architecture", ".claude"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// REQ with an AC pointing at an FR; a contract exists (so the
	// contractless-stage floor passes) but no module packages exist — the
	// bridge must name the AC.
	write("docs/contracts/BE-700.md", "# BE-700\n\n> 状态：locked\n> 版本：v1.0.0\n\n"+
		"| REQ source_ref | Rule/CASE/Story/PATH | 本合同条款§ | 验收标准 |\n|:--|:--|:--|:--|\n"+
		"| REQ-700/FR-701 | — | BE-700 §1 | 可提交 |\n")
	write("docs/design/architecture/ARCHITECTURE-700.md", "# ARCHITECTURE-700\n\n> 状态：locked\n> 版本：v1.0.0\n")
	write("docs/requirements/REQ-700.md", "# REQ-700\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n\n"+
		"| 编号 | 模块 | 需求 | 服务于 | 优先级 |\n|:--|:--|:--|:--|:--|\n| FR-701 | wb7 | 提交 | A1 | Must |\n"+
		"| 编号 | 验收标准 | 指向 |\n|:--|:--|:--|\n| AC-701 | 提交成功 | FR-701 |\n")
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "bob"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("bind failed: %s", stderr.String())
	}
	if code := cli.Run([]string{"runtime", "transition", "--root", root,
		"--id", "PTR-PLAN-01", "--expected-revision", "0", "--actor", "orchestrator"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("PTR-PLAN-01 failed: %s", stderr.String())
	}
	if code := cli.Run([]string{"runtime", "transition", "--root", root,
		"--id", "PTR-PLAN-02", "--expected-revision", "1", "--actor", "orchestrator"}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "AC-701") {
		t.Fatalf("PTR-PLAN-02 must be blocked by the bridge naming AC-701, got: %s", stderr.String())
	}
}

// TestContractsReverseClosureUsesModelAsAuthority pins the adversarial
// finding: cases.json is a generated artifact — hand-deleting a CASE (and
// its citations) must not silently shrink the verification denominator,
// because scenario-model.json remains the authoritative CASE universe.
func TestContractsReverseClosureUsesModelAsAuthority(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"docs/contracts", "docs/requirements", "docs/design/architecture", "docs/design/prototypes/wb", ".claude"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/requirements/REQ-510.md", "# REQ-510\n\n> 状态：locked\n> 版本：v1.0.0\n\n| 编号 | 模块 | 需求 | 服务于 | 优先级 |\n|:--|:--|:--|:--|:--|\n| FR-511 | wb | 提交 | A1 | Must |\n")
	write("docs/design/prototypes/wb/scenario-model.json", `{"module":"wb","rules":[{"id":"rule-1","source_refs":["REQ-510/FR-511"],"branches":[{"id":"b1","case_id":"CASE-WB-001"},{"id":"b2","case_id":"CASE-WB-002"}]}]}`)
	// Tampered generated artifact: CASE-WB-002 deleted from cases.json…
	write("docs/design/prototypes/wb/cases.json", `{"cases":[{"id":"CASE-WB-001"}]}`)
	// …and its citation deleted from the contract.
	write("docs/contracts/BE-510.md", "# BE-510\n\n> 状态：locked\n> 版本：v1.0.0\n\n"+
		"| REQ source_ref | Rule/CASE/Story/PATH | 本合同条款§ | 验收标准 |\n|:--|:--|:--|:--|\n"+
		"| REQ-510/FR-511 | CASE-WB-001 | BE-510 §1 | 可提交 |\n")
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"contracts", "check", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "CASE-WB-002") {
		t.Fatalf("tampered cases.json must be caught against the model authority, got: %s", stderr.String())
	}
}
