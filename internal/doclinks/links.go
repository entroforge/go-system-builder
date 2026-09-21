// Package doclinks validates local Markdown references in source, release and
// installed trees. Placeholder links and fenced examples are not live inputs.
package doclinks

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var codeSpan = regexp.MustCompile("`+[^`]*`+")

var inline = regexp.MustCompile(`\]\((<[^>\n]+>|[^\s)]+)(?:\s+"[^"]*")?\)`)
var definition = regexp.MustCompile(`^\s*\[[^\]]+\]:\s*(<[^>\n]+>|[^\s]+)`)
var htmlLink = regexp.MustCompile(`(?i)href=["']([^"']+)["']`)
var explicitID = regexp.MustCompile(`(?:\{#([^}]+)\}|(?i:id|name)=["']([^"']+)["'])`)
var heading = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)

func files(root string) ([]string, error) {
	var result []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" || rel == "dist" || rel == "bin" || rel == "build" {
				return filepath.SkipDir
			}
			if rel == ".claude" {
				return nil
			}
			if strings.HasPrefix(rel, ".claude/") && !(rel == ".claude/skills" || strings.HasPrefix(rel, ".claude/skills/") || rel == ".claude/agents" || rel == ".claude/bin") {
				return filepath.SkipDir
			}
			return nil
		}
		isDocument := strings.HasSuffix(rel, ".md") || strings.HasSuffix(rel, ".html")
		if entry.Type()&os.ModeSymlink != 0 {
			// Unrelated source-code links are outside this document scanner.
			// Document/install assets themselves must remain real files.
			if rel == "." || rel == "docs" || rel == "blueprint" || strings.HasPrefix(rel, "blueprint/") || rel == "skills" || rel == "agents" || rel == ".claude" || isDocument || strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, "skills/") || strings.HasPrefix(rel, "agents/") || strings.HasPrefix(rel, ".claude/") {
				return fmt.Errorf("doc links: symlink is not a document source: %s", rel)
			}
			return nil
		}
		if isDocument {
			result = append(result, rel)
		}
		return nil
	})
	return result, err
}

func external(href string) bool {
	return strings.HasPrefix(href, "//") || regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`).MatchString(href)
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func anchors(data string) map[string]bool {
	result := map[string]bool{}
	seen := map[string]int{}
	fenced := false
	for _, line := range strings.Split(data, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		for _, m := range explicitID.FindAllStringSubmatch(codeSpan.ReplaceAllString(line, ""), -1) {
			if m[1] != "" {
				result[m[1]] = true
			}
			if m[2] != "" {
				result[m[2]] = true
			}
		}
		if m := heading.FindStringSubmatch(line); m != nil {
			base := slug(m[1])
			name := base
			if n := seen[base]; n > 0 {
				name = fmt.Sprintf("%s-%d", base, n)
			}
			seen[base]++
			result[name] = true
		}
	}
	return result
}

// Validate checks live local links and fragments, not network URLs or rendered
// layout. Runtime artifact references are validated by their own schemas/loaders.
func Validate(root string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realRoot, err = filepath.Abs(realRoot)
	if err != nil {
		return err
	}
	paths, err := files(root)
	if err != nil {
		return err
	}
	var problems []string
	cache := map[string]map[string]bool{}
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return err
		}
		fenced := false
		for i, line := range strings.Split(string(data), "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
				fenced = !fenced
				continue
			}
			if fenced {
				continue
			}
			line = codeSpan.ReplaceAllString(line, "")
			refs := []string{}
			for _, m := range inline.FindAllStringSubmatch(line, -1) {
				refs = append(refs, m[1])
			}
			if m := definition.FindStringSubmatch(line); m != nil {
				refs = append(refs, m[1])
			}
			for _, m := range htmlLink.FindAllStringSubmatch(line, -1) {
				refs = append(refs, m[1])
			}
			for _, href := range refs {
				href = strings.TrimSuffix(strings.TrimPrefix(href, "<"), ">")
				if external(href) || strings.ContainsAny(href, "{}<>") {
					continue
				}
				parts := strings.SplitN(href, "#", 2)
				target := parts[0]
				if v, e := url.PathUnescape(target); e == nil {
					target = v
				}
				if target == "" {
					target = rel
				} else if strings.HasPrefix(target, "/") {
					target = strings.TrimPrefix(target, "/")
				} else {
					base := rel
					// These two source profiles are authored for their explicit packaged destinations.
					if rel == "packaging/README.installed.md" {
						base = "docs/README.md"
					}
					if rel == "packaging/DOCUMENT-MAP.installed.md" {
						base = "docs/DOCUMENT-MAP.md"
					}
					target = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(base), target)))
				}
				target = filepath.ToSlash(filepath.Clean(target))
				if target == ".." || strings.HasPrefix(target, "../") {
					problems = append(problems, fmt.Sprintf("%s:%d: link escapes root: %s", rel, i+1, href))
					continue
				}
				info, e := os.Stat(filepath.Join(root, target))
				if e != nil {
					problems = append(problems, fmt.Sprintf("%s:%d: missing %s", rel, i+1, href))
					continue
				}
				realTarget, resolveErr := filepath.EvalSymlinks(filepath.Join(root, target))
				if resolveErr == nil {
					realTarget, resolveErr = filepath.Abs(realTarget)
				}
				contained, scopeErr := filepath.Rel(realRoot, realTarget)
				if resolveErr != nil || scopeErr != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
					problems = append(problems, fmt.Sprintf("%s:%d: link escapes root or cannot resolve: %s", rel, i+1, href))
					continue
				}
				if len(parts) == 2 && parts[1] != "" && !info.IsDir() && (strings.HasSuffix(target, ".md") || strings.HasSuffix(target, ".html")) {
					if cache[target] == nil {
						b, e := os.ReadFile(filepath.Join(root, target))
						if e != nil {
							return e
						}
						cache[target] = anchors(string(b))
					}
					fragment, _ := url.PathUnescape(parts[1])
					if !cache[target][fragment] {
						problems = append(problems, fmt.Sprintf("%s:%d: missing anchor %s", rel, i+1, href))
					}
				}
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("document links (%d):\n%s", len(problems), strings.Join(problems, "\n"))
	}
	return nil
}

// Relocate rewrites actual Markdown links when installing a source file at a
// different path. Code spans and Runtime path/SHA records are never rewritten.
func Relocate(data []byte, oldPath, newPath string, mapPath func(string) string) []byte {
	rewrite := func(href string) string {
		if external(href) || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "/") {
			return href
		}
		parts := strings.SplitN(href, "#", 2)
		target := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(oldPath), parts[0])))
		target = mapPath(target)
		r, e := filepath.Rel(filepath.Dir(newPath), target)
		if e != nil {
			return href
		}
		if len(parts) == 2 {
			r += "#" + parts[1]
		}
		return filepath.ToSlash(r)
	}

	rewriteDestination := func(href string) string {
		if strings.HasPrefix(href, "<") && strings.HasSuffix(href, ">") {
			return "<" + rewrite(strings.TrimSuffix(strings.TrimPrefix(href, "<"), ">")) + ">"
		}
		return rewrite(href)
	}
	rewriteText := func(text string) string {
		text = inline.ReplaceAllStringFunc(text, func(m string) string {
			sub := inline.FindStringSubmatch(m)
			return strings.Replace(m, sub[1], rewriteDestination(sub[1]), 1)
		})
		if m := definition.FindStringSubmatch(text); m != nil {
			text = strings.Replace(text, m[1], rewriteDestination(m[1]), 1)
		}
		return htmlLink.ReplaceAllStringFunc(text, func(m string) string {
			sub := htmlLink.FindStringSubmatch(m)
			return strings.Replace(m, sub[1], rewriteDestination(sub[1]), 1)
		})
	}
	lines := strings.Split(string(data), "\n")
	fenced := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		var out strings.Builder
		start := 0
		for _, span := range codeSpan.FindAllStringIndex(line, -1) {
			out.WriteString(rewriteText(line[start:span[0]]))
			out.WriteString(line[span[0]:span[1]])
			start = span[1]
		}
		out.WriteString(rewriteText(line[start:]))
		lines[i] = out.String()
	}
	return []byte(strings.Join(lines, "\n"))
}
