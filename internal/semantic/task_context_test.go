package semantic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

type countedContextFiles struct {
	fileview.Reader
	reads map[string]int
}

func (c countedContextFiles) ReadFile(p string) ([]byte, error) {
	c.reads[p]++
	return c.Reader.ReadFile(p)
}

func TestTaskContextReadingModesSlicesAndDedup(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs/dev/contracts"), 0700)
	body := "# 合同\n\n## 提交规则\n第一条。\n### 子项\n第二条。\n```text\n## 伪标题\n```\n## 后续\n其他内容。\n"
	path := "docs/dev/contracts/BE-001.md"
	os.WriteFile(filepath.Join(root, path), []byte(body), 0600)
	manifest := "## 2. Document Manifest\n| Order | Kind | ID | Path | Clauses | Purpose | Mode |\n|---|---|---|---|---|---|---|\n" +
		"| 1 | contract | BE-001 | [规则](../contracts/BE-001.md#提交规则) | §1 | required delivery | required |\n" +
		"| 2 | contract | BE-001 | [同条](../contracts/BE-001.md#提交规则) | §1 | repeated | required |\n" +
		"| 3 | contract | BE-001 | [子项](../contracts/BE-001.md#子项) | §1 | if feature enabled | conditional |\n" +
		"| 4 | contract | BE-001 | [全文](../contracts/BE-001.md) | — | background | optional |\n" +
		"## 7. Closing Contract\n```text\nassert go test ./internal/order/... == pass\nassert BE-001 §1 == satisfied\n```\n"
	task := &taskDocument{id: "TASK-001", rel: "docs/dev/tasks/TASK-001.md", status: "complete", content: manifest, writePaths: []string{"internal/order/a.go", "internal/order/b.go"}}
	reads := map[string]int{}
	summaries := analyzeTaskContexts(root, countedContextFiles{Reader: fileview.Disk{Root: root}, reads: reads}, []*taskDocument{task, task})
	s := summaries[0]
	section := body[strings.Index(body, "## 提交规则"):strings.Index(body, "## 后续")]
	if len(s.Reading) != 4 || s.TotalUniqueBytes != len([]byte(body)) || s.BytesByMode["required"] != len([]byte(section)) || len(s.CheckEntrances) != 1 || len(s.UnresolvedAssertions) != 1 {
		t.Fatalf("wrong summary: %+v", s)
	}
	if !s.Reading[2].ConditionUnknown || s.Reading[0].Selection != "heading_section" || len(s.Modules) != 1 || s.Modules[0] != "internal/order" {
		t.Fatalf("wrong slice/condition/module: %+v", s)
	}
	if reads[filepath.Join(root, path)] != 1 {
		t.Fatalf("shared file read %d times", reads[filepath.Join(root, path)])
	}
}

func TestTaskContextUnknownInputsAreNotZeroWeightSuccess(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs/design/module"), 0700)
	os.MkdirAll(filepath.Join(root, "docs/dev/contracts"), 0700)
	os.WriteFile(filepath.Join(root, "docs/dev/contracts/BE-001.md"), []byte("# Header\n## repeated\na\n## repeated\nb\n"), 0600)
	manifest := "## Document Manifest\n| Order | Kind | ID | Path | Clauses | Purpose | Mode |\n" +
		"| 1 | design | D | `docs/design/module/` | — | package | required |\n" +
		"| 2 | contract | C | `docs/missing.md` | — | maybe | conditional |\n" +
		"| 3 | contract | C | [ambiguous](../contracts/BE-001.md#repeated) | §1 | delivery | required |\n" +
		"| 4 | contract | C | [missing](../contracts/BE-001.md#absent) | §1 | context | optional |\n" +
		"| 5 | contract | C | [bad](../../../../escape.md) | — | bad | required |\n" +
		"| 6 | contract | C | `docs/dev/contracts/BE-001.md` | §1 |\n" +
		"## Closing Contract\nassert {verification command} == pass\n"
	s := analyzeTaskContexts(root, fileview.Disk{Root: root}, []*taskDocument{{id: "TASK-001", rel: "docs/dev/tasks/TASK-001.md", content: manifest}})[0]
	if s.Reading[0].Status != "directory_unexpanded" || s.Reading[0].Bytes != nil || s.Reading[1].Status != "unreadable" || s.Reading[1].Bytes != nil {
		t.Fatalf("unreadable became zero weight: %+v", s.Reading)
	}
	if !strings.Contains(s.Reading[2].Reason, "ambiguous") || s.Reading[3].Selection != "whole_file" || s.Reading[4].Status != "unresolved" || s.Reading[5].Mode != "legacy_unspecified" {
		t.Fatalf("missing conservative reasons: %+v", s.Reading)
	}
	if len(s.CheckEntrances) != 0 || len(s.UnresolvedAssertions) != 1 {
		t.Fatal("placeholder became executable oracle")
	}
}

func TestTaskGranularityAdviceOnlyNamesObservedCoupling(t *testing.T) {
	a := TaskContextSummary{TaskID: "TASK-A", TaskRef: "TASK-A.md", Writes: []string{"internal/order/shared.go"}, Reading: []TaskReadingReference{{Path: "contract.md", Anchor: "order", Mode: "required", Status: "readable"}}}
	b := a
	b.TaskID = "TASK-B"
	b.TaskRef = "TASK-B.md"
	advice := AnalyzeTaskGranularity([]TaskContextSummary{a, b})
	kinds := map[string]bool{}
	for _, item := range advice {
		kinds[item.Kind] = true
		if len(item.SourceRefs) == 0 || len(item.TaskIDs) == 0 || item.Question == "" {
			t.Fatalf("unsupported advice: %+v", item)
		}
	}
	if !kinds["same_file_writes"] || !kinds["shared_read_write_context"] {
		t.Fatalf("missing coupling observations: %+v", advice)
	}
	for _, item := range AnalyzeTaskGranularity([]TaskContextSummary{{TaskID: "TASK-SINGLE", TaskRef: "TASK-SINGLE.md", Modules: []string{"src/inventory", "tests"}}}) {
		if item.Kind == "closing_oracle_unresolved" || item.Kind == "multiple_write_modules" {
			t.Fatalf("speculative author proof request retained: %+v", item)
		}
	}
	if kinds["closing_oracle_unresolved"] || kinds["multiple_write_modules"] {
		t.Fatal("machine-recognized command or path grouping became an advisory")
	}
	b.Writes = []string{"internal/independent/a.go"}
	for _, item := range AnalyzeTaskGranularity([]TaskContextSummary{a, b}) {
		if item.Kind == "same_file_writes" || item.Kind == "shared_read_write_context" {
			t.Fatal("unrelated write sets became coupled")
		}
	}
}

func TestTaskContextLimitsAndLocalAuthority(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "private.md"), []byte("outside input"), 0600)
	if err := os.Symlink(filepath.Join(outside, "private.md"), filepath.Join(root, "escape.md")); err != nil {
		t.Skip(err)
	}
	os.WriteFile(filepath.Join(root, "large.md"), []byte(strings.Repeat("x", taskContextParseLimit+1)), 0600)
	manifest := "## Document Manifest\n| Order | Kind | ID | Path | Clauses | Purpose | Mode |\n" +
		"| 1 | model | L | `large.md` | — | delivery | required |\n" +
		"| 2 | model | E | `escape.md` | — | background | optional |\n" +
		"| 3 | model | R | [remote](https://example.com/spec) | — | external | conditional |\n"
	s := analyzeTaskContexts(root, fileview.Disk{Root: root}, []*taskDocument{{id: "TASK-001", rel: "docs/dev/tasks/TASK-001.md", content: manifest}})[0]
	if s.Reading[0].Status != "parse_limit_exceeded" || s.Reading[1].Status != "unresolved" || s.Reading[2].Status != "unresolved" {
		t.Fatalf("analysis limits/authority not enforced: %+v", s.Reading)
	}
	for _, r := range s.Reading {
		if r.Bytes != nil {
			t.Fatal("unavailable input counted as successful zero/full bytes")
		}
	}
	if s.TotalUniqueBytes != 0 {
		t.Fatal("out-of-scope/unparsed content entered load estimate")
	}
}

func TestTaskContextExplicitAnchorsAndFenceBoundaries(t *testing.T) {
	body := []byte("# Root\n<a id=\"stable\"></a>\n\n## 标题\nfirst\n### Child {#child}\nsecond\n~~~text\n## Fake {#fake}\n~~~\n## Next\nend\n")
	span, selection, reason := readingSpan(body, "contract.md", "stable")
	if selection != "heading_section" || reason != "" || string(body[span.start:span.end]) != "<a id=\"stable\"></a>\n\n## 标题\nfirst\n### Child {#child}\nsecond\n~~~text\n## Fake {#fake}\n~~~\n" {
		t.Fatalf("wrong explicit section: %+v %s %s", span, selection, reason)
	}
	_, selection, _ = readingSpan(body, "contract.md", "fake")
	if selection != "whole_file" {
		t.Fatal("code-fence anchor became a real section")
	}
	span, selection, _ = readingSpan(body, "contract.md", "child")
	if selection != "heading_section" || !strings.HasPrefix(string(body[span.start:span.end]), "### Child") {
		t.Fatal("inline anchor did not resolve")
	}
}
