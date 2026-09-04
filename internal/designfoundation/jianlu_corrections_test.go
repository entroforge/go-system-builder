package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func warningCodes(report Report) map[string]bool {
	out := map[string]bool{}
	for _, f := range report.Warnings() {
		out[f.Code] = true
	}
	return out
}

func TestJianluPromptLayerCorrections(t *testing.T) {
	root := repoRoot(t)

	kernel := mustRead(t, filepath.Join(root, "docs", "design", "DESIGN-template.md"))
	if !strings.Contains(kernel, "`inline`") {
		t.Fatal("DESIGN-template §8 must default Profile/version to inline, not a profile file")
	}
	if strings.Contains(kernel, "| SUR-01 | {consumer} | `surface-profiles/") {
		t.Fatal("DESIGN-template SUR-01 must not point at a profile file")
	}

	skill := mustRead(t, filepath.Join(root, "skills", "design-foundation", "SKILL.md"))
	if !strings.Contains(skill, "version: 1.3.0") {
		t.Fatal("design-foundation Skill must be 1.3.0 after 笺录")
	}
	for _, want := range []string{
		"**Local**",
		"Do **not** publish a Project Foundation",
		"Do **not** set `published`",
		"status tags",
		"buttons and which may only be sentences",
	} {
		if !strings.Contains(skill, want) {
			t.Fatalf("Skill F0/F3/F6 must encode 笺录 entry rules, missing %q", want)
		}
	}

	rule := mustRead(t, filepath.Join(root, "docs", "rules", "design-foundation.md"))
	if strings.Contains(rule, "must have a published Project Design Foundation before the first") {
		t.Fatal("rule §1 must not still require a published Foundation before every first UI REQ")
	}
	if !strings.Contains(rule, "Local one-shot") {
		t.Fatal("rule §1 must allow Local without a Project Foundation")
	}

	deriv := mustRead(t, filepath.Join(root, "docs", "design", "derivation", "DERIVATION-template.md"))
	if !strings.Contains(deriv, "inherit 时不得无 `extend`") {
		t.Fatal("Derivation template must forbid a new information posture on inherit")
	}
}

func TestCheckProvisionalIsNotAPublishLock(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json": mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/project-map.md":                "| design investment | core | handoff | upgrade when: second REQ |\n",
		"docs/design/DESIGN.md":              "# Kernel\n\n> 状态：provisional\n> 确认记录：方向 PENDING · 内核 PENDING · 发布 PENDING\n\n## 0. Next-agent card\n\n| ID | Do |\n|:--|:--|\n| LAW-01 | retell |\n",
		"docs/requirements/REQ-001.md":       "# REQ-001\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v0.1.0 |\n",
		"docs/design/derivation/REQ-001.md":  "# Derivation\n\n> Foundation：docs/design/DESIGN.md@v0.1.0\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	codes := warningCodes(report)
	if !codes["foundation_provisional"] {
		t.Fatalf("provisional Kernel must warn foundation_provisional, got %#v", report.Findings)
	}
	if codes["foundation_unpublished"] {
		t.Fatalf("provisional must not be billed as unpublished-draft, got %#v", report.Findings)
	}
	if codes["grammar_missing"] {
		t.Fatalf("provisional must not owe Grammar, got %#v", report.Findings)
	}
}

func TestCheckPublishedPendingIsFakeLock(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json": mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/project-map.md":                "| design investment | core | handoff | upgrade when: second REQ |\n",
		"docs/design/DESIGN.md":              "# Kernel\n\n> 状态：published\n> 确认记录：方向 PENDING · 内核 PENDING · 发布 PENDING\n\n## 0. Next-agent card\n\n| ID | Do |\n|:--|:--|\n| LAW-01 | retell |\n",
		"docs/requirements/REQ-001.md":       "# REQ-001\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v0.1.0 |\n",
		"docs/design/derivation/REQ-001.md":  "# Derivation\n\n> Foundation：docs/design/DESIGN.md@v0.1.0\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	codes := warningCodes(report)
	if !codes["foundation_fake_lock"] {
		t.Fatalf("published+PENDING must warn foundation_fake_lock, got %#v", report.Findings)
	}
	if codes["grammar_missing"] {
		t.Fatalf("fake lock must not pile Grammar tax, got %#v", report.Findings)
	}
}

func TestCheckCorePublishedAllowsGrammarDebt(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json": mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/project-map.md":                "| design investment | core | handoff | upgrade when: second REQ |\n",
		"docs/design/DESIGN.md":              "# Kernel\n\n> 状态：published\n> 确认记录：方向 2026-09-04 · 内核 2026-09-04 · 发布 2026-09-04\n\n## 0. Next-agent card\n\n| ID | Do |\n|:--|:--|\n| LAW-01 | retell |\n",
		"docs/requirements/REQ-001.md":       "# REQ-001\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v1.0.0 |\n",
		"docs/design/derivation/REQ-001.md":  "# Derivation\n\n> Foundation：docs/design/DESIGN.md@v1.0.0\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if warningCodes(report)["grammar_missing"] {
		t.Fatalf("Core+thin may omit design-language.md, got %#v", report.Findings)
	}
}

func TestCheckExtendedPublishedStillOwesGrammar(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json": mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/project-map.md":                "| design investment | extended | two surfaces | upgrade when: design system |\n",
		"docs/design/DESIGN.md":              "# Kernel\n\n> 状态：published\n> 确认记录：方向 2026-09-04 · 内核 2026-09-04 · 发布 2026-09-04\n\n## 0. Next-agent card\n\n| ID | Do |\n|:--|:--|\n| LAW-01 | retell |\n",
		"docs/requirements/REQ-001.md":       "# REQ-001\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v1.0.0 |\n",
		"docs/design/derivation/REQ-001.md":  "# Derivation\n\n> Foundation：docs/design/DESIGN.md@v1.0.0\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if !warningCodes(report)["grammar_missing"] {
		t.Fatalf("Extended publish still owes design-language.md, got %#v", report.Findings)
	}
}

func TestKernelStatusAndInvestmentHelpers(t *testing.T) {
	if got := kernelStatus("> 状态：provisional\n"); got != "provisional" {
		t.Fatalf("kernelStatus provisional = %q", got)
	}
	if !kernelConfirmationPending("> 确认记录：方向 PENDING · 内核 2026-09-04\n") {
		t.Fatal("header PENDING must count as unconfirmed")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "project-map.md"), []byte("| design investment | extended | x | y |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := designInvestment(root); got != "extended" {
		t.Fatalf("designInvestment = %q", got)
	}
}
