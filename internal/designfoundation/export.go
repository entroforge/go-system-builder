package designfoundation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func ExportPortable(root string) (string, error) {
	tf, err := LoadTokens(root)
	if err != nil {
		return "", err
	}
	kernel, err := readPortableSource(root, KernelRel)
	if err != nil {
		return "", err
	}
	grammar, err := readPortableSource(root, GrammarRel)
	if err != nil {
		return "", err
	}
	body := renderPortable(tf, string(kernel), string(grammar))
	path := filepath.Join(root, PortableRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Optional documents may be absent; an actual read error must not publish an
// apparently successful partial snapshot over the previous one.
func readPortableSource(root, rel string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("portable source %s: %w", rel, err)
	}
	return data, nil
}

func renderPortable(tf *tokenFile, kernel, grammar string) string {
	var b strings.Builder
	b.WriteString("---\nversion: alpha\nname: Project Design Foundation snapshot\n")
	b.WriteString("description: Source excerpts and token values only; not approval or project authority.\n")
	b.WriteString("omitted:\n  - section: components\n    reason: Component APIs remain in the current component source, outside this snapshot.\n")
	// Keep the existing categories, but preserve the actual token paths rather
	// than inventing a body style or mapping unrelated tokens to a fixed schema.
	for _, group := range []struct{ name, prefix string }{
		{"colors", "color."}, {"typography", "font."}, {"rounded", "rounded."}, {"spacing", "space."},
	} {
		var leaves []tokenLeaf
		if group.name == "colors" {
			leaves = tf.SemanticColors()
		} else {
			for _, leaf := range tf.leaves {
				if strings.HasPrefix(leaf.Path, group.prefix) {
					leaves = append(leaves, leaf)
				}
			}
		}
		if len(leaves) == 0 {
			fmt.Fprintf(&b, "%s: {}\n", group.name)
			continue
		}
		fmt.Fprintf(&b, "%s:\n", group.name)
		for _, leaf := range leaves {
			fmt.Fprintf(&b, "  %s: %s\n", quoteYAML(leaf.Path), quoteYAML(leaf.Value))
		}
	}
	b.WriteString("---\n\n# DESIGN.md (derived snapshot)\n\n")
	fmt.Fprintf(&b, "_Generated %s. Authority remains `%s`, `%s` and `%s`._\n\n", time.Now().UTC().Format("2006-01-02"), KernelRel, GrammarRel, TokensJSONRel)
	b.WriteString("本文件只传递下列来源片段与已解析的 Token 值，不验证发布状态、适用表面、条款激活状态、完整性或批准。未提取不表示 N/A，也不要求补造文件；缺少依据时回到原来源判断。模板初始值不会因导出变成已批准设计。\n\n")
	b.WriteString("## Token values\n\n")
	fmt.Fprintf(&b, "来源：`%s`；按原路径列值，引用沿原 Token 解析器展开。不从数值推断字体用途、强调规则或组件用法。\n\n", TokensJSONRel)
	if len(tf.leaves) == 0 {
		b.WriteString("未提取到 Token 值。\n")
	}
	for _, leaf := range tf.leaves {
		fmt.Fprintf(&b, "- `%s` = %s", leaf.Path, quoteYAML(leaf.Value))
		if leaf.Description != "" {
			fmt.Fprintf(&b, " — %s", leaf.Description)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## Kernel excerpts\n\n")
	for _, title := range []string{"Next-agent card", "Design Thesis", "Anti-principles", "Rejected direction"} {
		writePortableExcerpt(&b, KernelRel, title, extractSection(kernel, title))
	}
	b.WriteString("## Grammar excerpts\n\n")
	for _, title := range []string{"Compilation and selection rules", "Variant catalog", "Semantic roles and patterns"} {
		writePortableExcerpt(&b, GrammarRel, title, extractSection(grammar, title))
	}
	b.WriteString("### Dimension excerpts\n\n")
	fmt.Fprintf(&b, "来源：`%s` 的同名章节或旧格式维度行；保留原片段，不将 coverage 状态行解释成具体设计指令。\n\n", GrammarRel)
	var missing []string
	for _, dimension := range []string{"Information", "Composition", "Color", "Typography", "Shape & Surface", "Image & Icon", "Interaction", "Content", "Motion"} {
		if text := extractGrammarDimension(grammar, dimension); text != "" {
			fmt.Fprintf(&b, "#### %s\n\n%s\n\n", dimension, text)
		} else {
			missing = append(missing, dimension)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "未提取到同名章节或维度行：%s。其他选择规则仍须查阅原 Grammar。\n", strings.Join(missing, ", "))
	}
	return b.String()
}

func writePortableExcerpt(b *strings.Builder, source, title, text string) {
	fmt.Fprintf(b, "### %s\n\n来源：`%s` 的 `%s` 章节。\n\n", title, source, title)
	if text == "" {
		b.WriteString("未提取到该章节的内容；不生成替代设计指令。\n\n")
		return
	}
	b.WriteString(text)
	b.WriteString("\n\n")
}

func quoteYAML(v string) string {
	// JSON strings are valid YAML scalars, including escaped quotes/newlines.
	encoded, _ := json.Marshal(v)
	return string(encoded)
}

var heading = regexp.MustCompile(`(?m)^##+ `)

func extractSection(body, title string) string {
	if body == "" {
		return ""
	}
	pattern := `(?m)^##+\s*(?:\d+\.\s*)?` + regexp.QuoteMeta(title) + `\b`
	re := regexp.MustCompile(pattern)
	loc := re.FindStringIndex(body)
	if loc == nil {
		return ""
	}
	rest := body[loc[0]:]
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	}
	if next := heading.FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	return strings.TrimSpace(rest)
}

func extractGrammarDimension(grammar, dimension string) string {
	if section := extractSection(grammar, dimension); section != "" {
		return section
	}
	return grammarDimensionRows(grammar, dimension)
}

// grammarDimensionRows collects a dimension's rows from the per-Law compile
// tables when the Grammar carries no standalone heading for it. Empty cells
// return nothing so the caller reports that no excerpt was extracted.
func grammarDimensionRows(grammar, dimension string) string {
	if grammar == "" {
		return ""
	}
	pattern := regexp.MustCompile(`(?i)^\|\s*` + regexp.QuoteMeta(dimension) + `\s*\|(.+)\|\s*$`)
	var rows []string
	for _, line := range strings.Split(grammar, "\n") {
		m := pattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var cells []string
		for _, cell := range strings.Split(m[1], "|") {
			if cell = strings.TrimSpace(cell); cell != "" {
				cells = append(cells, cell)
			}
		}
		if len(cells) == 0 {
			continue
		}
		rows = append(rows, "- "+dimension+": "+strings.Join(cells, " / "))
	}
	return strings.Join(rows, "\n")
}
