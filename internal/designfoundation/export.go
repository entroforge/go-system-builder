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
	idx := strings.Index(body, "## "+title)
	if idx < 0 {
		idx = strings.Index(body, "### "+title)
	}
	if idx < 0 {
		return ""
	}
	rest := body[idx:]
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	}
	if loc := heading.FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	return strings.TrimSpace(rest)
}

func extractGrammarDimension(grammar, dimension string) string {
	return extractSection(grammar, dimension)
}
