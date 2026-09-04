package designfoundation

import (
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
	kernel, _ := os.ReadFile(filepath.Join(root, KernelRel))
	grammar, _ := os.ReadFile(filepath.Join(root, GrammarRel))
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

func renderPortable(tf *tokenFile, kernel, grammar string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("version: alpha\n")
	b.WriteString("name: Project Design Foundation snapshot\n")
	b.WriteString("description: Derived from DESIGN.md + tokens.json. Not the project authority. Do not put component APIs here.\n")
	b.WriteString("omitted:\n")
	b.WriteString("  - section: components\n")
	b.WriteString("    reason: Query the live UI Lab / Storybook MCP instead of re-implementing components from prose\n")
	b.WriteString("colors:\n")
	for _, leaf := range tf.SemanticColors() {
		name := strings.TrimPrefix(leaf.Path, "color.")
		name = strings.ReplaceAll(name, ".", "-")
		b.WriteString("  ")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(quoteYAML(leaf.Value))
		b.WriteString("\n")
	}
	b.WriteString("typography:\n")
	b.WriteString("  body:\n")
	b.WriteString("    fontFamily: system-ui\n")
	b.WriteString("    fontSize: 16px\n")
	b.WriteString("    fontWeight: 400\n")
	b.WriteString("    lineHeight: 1.5\n")
	b.WriteString("rounded:\n")
	for _, leaf := range tf.leaves {
		if !strings.HasPrefix(leaf.Path, "rounded.") {
			continue
		}
		b.WriteString("  ")
		b.WriteString(strings.TrimPrefix(leaf.Path, "rounded."))
		b.WriteString(": ")
		b.WriteString(leaf.Value)
		b.WriteString("\n")
	}
	b.WriteString("spacing:\n")
	for _, leaf := range tf.leaves {
		if !strings.HasPrefix(leaf.Path, "space.") {
			continue
		}
		b.WriteString("  ")
		b.WriteString(strings.TrimPrefix(leaf.Path, "space."))
		b.WriteString(": ")
		b.WriteString(leaf.Value)
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	b.WriteString("# DESIGN.md (derived snapshot)\n\n")
	b.WriteString("_Generated ")
	b.WriteString(time.Now().UTC().Format("2006-01-02"))
	b.WriteString(". Authority remains `docs/design/DESIGN.md` and `packages/design-tokens/tokens.json`._\n\n")
	b.WriteString("## Overview\n\n")
	if thesis := extractSection(kernel, "Design Thesis"); thesis != "" {
		b.WriteString(thesis)
		b.WriteString("\n\n")
	} else {
		b.WriteString("Starter snapshot. Publish Project Design Foundation (F6) before treating this file as brand direction.\n\n")
	}
	b.WriteString("## Colors\n\n")
	b.WriteString("Semantic roles only. Emphasis is scarce: `action.promise` is the single promise-type action color.\n\n")
	for _, leaf := range tf.SemanticColors() {
		fmt.Fprintf(&b, "- **%s (%s):** %s\n", leaf.Path, leaf.Value, leaf.Description)
	}
	b.WriteString("\n## Typography\n\n")
	b.WriteString("Body stays readable; headlines carry hierarchy, not decoration. Metadata uses the meta size and content.meta color.\n\n")
	b.WriteString("## Layout\n\n")
	if layout := extractGrammarDimension(grammar, "Composition"); layout != "" {
		b.WriteString(layout)
		b.WriteString("\n\n")
	} else {
		b.WriteString("Use the space scale (xs–xl). Do not invent page-level spacing tokens.\n\n")
	}
	b.WriteString("## Elevation & Depth\n\n")
	b.WriteString("Prefer tonal layers (`surface.page` vs `surface.raised`) over heavy shadows unless Grammar says otherwise.\n\n")
	b.WriteString("## Shapes\n\n")
	b.WriteString("Corner radius comes from `rounded.*`. Mixing sharp and fully round in one view needs a Grammar exception.\n\n")
	b.WriteString("## Do's and Don'ts\n\n")
	if anti := extractSection(kernel, "Anti-principles"); anti != "" {
		b.WriteString(anti)
		b.WriteString("\n\n")
	} else if rej := extractSection(kernel, "Rejected direction"); rej != "" {
		b.WriteString(rej)
		b.WriteString("\n\n")
	}
	b.WriteString("- Do use `action.promise` for at most one primary commitment per screen.\n")
	b.WriteString("- Don't introduce hex values that are absent from tokens.json.\n")
	b.WriteString("- Don't re-implement a live Storybook component from this file.\n")
	b.WriteString("- Don't treat pixel snapshot equality as proof the Design Thesis is correct.\n")
	return b.String()
}

func quoteYAML(v string) string {
	if strings.ContainsAny(v, ":#{}[]&*?|>'\"%@`") || strings.HasPrefix(v, "#") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	if strings.HasPrefix(v, "#") {
		return `"` + v + `"`
	}
	return `"` + v + `"`
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
// return nothing so the caller keeps its honest fallback text.
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
