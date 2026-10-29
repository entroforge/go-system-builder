package projectconventions

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func safePath(path string, scope bool) bool {
	if path == "." {
		return scope
	}
	return path != "" && !filepath.IsAbs(path) && filepath.ToSlash(filepath.Clean(path)) == path && path != ".." && !strings.HasPrefix(path, "../") && !strings.ContainsAny(path, "\\:*?[]{}\x00")
}
func overlaps(a, b string) bool {
	return a == "." || b == "." || a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
func taskPrefix(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.ContainsAny(p, "*?[{") {
			if i == 0 {
				return ""
			}
			return strings.Join(parts[:i], "/")
		}
	}
	return path
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

var explicitAnchor = regexp.MustCompile(`(?:\{#([^}]+)\}|<a\s+(?:id|name)=["']([^"']+)["'][^>]*>\s*</a>)`)

func section(data []byte, anchor string) ([]byte, error) {
	if anchor == "" {
		return data, nil
	}
	type heading struct {
		start, end, level int
		anchors           []string
	}
	headings := []heading{}
	offset := 0
	fence := ""
	pending := []string{}
	for _, line := range strings.SplitAfter(string(data), "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~") {
			marker := text[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			offset += len(line)
			continue
		}
		if fence != "" {
			offset += len(line)
			continue
		}
		names := []string{}
		for _, m := range explicitAnchor.FindAllStringSubmatch(text, -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			names = append(names, name)
		}
		level := 0
		for level < len(text) && text[level] == '#' {
			level++
		}
		if level > 0 && level <= 6 && level < len(text) && text[level] == ' ' {
			title := strings.TrimSpace(text[level:])
			title = explicitAnchor.ReplaceAllString(title, "")
			title = strings.TrimSpace(strings.TrimRight(title, "#"))
			names = append(names, slug(title))
			names = append(names, pending...)
			pending = nil
			headings = append(headings, heading{start: offset, end: len(data), level: level, anchors: names})
		} else if len(names) > 0 {
			pending = names
		} else if text != "" {
			pending = nil
		}
		offset += len(line)
	}
	selected := -1
	for i, h := range headings {
		match := false
		for _, name := range h.anchors {
			if name == anchor {
				match = true
			}
		}
		if match {
			if selected >= 0 {
				return nil, fmt.Errorf("ambiguous source anchor %s", anchor)
			}
			selected = i
		}
	}
	if selected < 0 {
		return nil, fmt.Errorf("source anchor %s is unavailable", anchor)
	}
	h := headings[selected]
	for _, next := range headings[selected+1:] {
		if next.level <= h.level {
			h.end = next.start
			break
		}
	}
	return data[h.start:h.end], nil
}
func metadata(data []byte, key string) string {
	// Rules put their metadata between the first standalone --- pair; formal
	// documents use the original leading blockquote metadata fields.
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(line)
		if text == "---" {
			if in {
				break
			}
			in = true
			continue
		}
		if in {
			k, v, ok := strings.Cut(text, ":")
			if ok && k == key {
				return strings.TrimSpace(v)
			}
		}
	}
	names := map[string][]string{"status": {"Status", "状态", "status"}, "version": {"Version", "版本", "version"}}
	for _, line := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(line)
		if !strings.HasPrefix(text, ">") {
			continue
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, ">"))
		text = strings.ReplaceAll(text, "：", ":")
		k, v, ok := strings.Cut(text, ":")
		for _, name := range names[key] {
			if ok && strings.TrimSpace(k) == name {
				return strings.Trim(strings.TrimSpace(v), "` ")
			}
		}
	}
	return ""
}
