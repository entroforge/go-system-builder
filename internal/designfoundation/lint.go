package designfoundation

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var hexRe = regexp.MustCompile(`(?i)#([0-9a-f]{3,8})\b`)
var rgbRe = regexp.MustCompile(`(?i)\brgba?\(\s*(\d{1,3})(?:\s*,\s*|\s+)(\d{1,3})(?:\s*,\s*|\s+)(\d{1,3})`)
var hslRe = regexp.MustCompile(`(?i)\bhsla?\(\s*([-\d.]+)(?:deg)?(?:\s*,\s*|\s+)([\d.]+)%(?:\s*,\s*|\s+)([\d.]+)%`)

func LintUnregisteredHex(root string) ([]Finding, error) {
	tf, err := LoadTokens(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Finding{{
				Code:     "tokens_missing",
				Severity: SeverityWarning,
				Path:     TokensJSONRel,
				Detail:   "packages/design-tokens/tokens.json is missing; prototypes must not invent hex values",
			}}, nil
		}
		return nil, err
	}
	allowed := tf.ColorHexes()
	designRoot := filepath.Join(root, "docs", "design")
	if _, err := os.Stat(designRoot); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var findings []Finding
	err = filepath.Walk(designRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !lintableDesignFile(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		findings = append(findings, lintColorsIn(string(data), rel, allowed)...)
		return nil
	})
	return findings, err
}

func lintColorsIn(body, rel string, allowed map[string]string) []Finding {
	var findings []Finding
	seen := map[string]bool{}
	add := func(code, raw, hex string) {
		key := code + raw
		if seen[key] {
			return
		}
		seen[key] = true
		if hex != "" {
			if _, known := allowed[hex]; known {
				return
			}
		}
		detail := raw + " is not a primitive or semantic color in " + TokensJSONRel
		if hex != "" && hex != strings.ToLower(raw) {
			detail = raw + " (" + hex + ") is not a primitive or semantic color in " + TokensJSONRel
		}
		findings = append(findings, Finding{
			Code:     code,
			Severity: SeverityWarning,
			Path:     rel,
			Detail:   detail + "; use var(--color-*) from tokens.css",
		})
	}

	for _, match := range hexRe.FindAllString(body, -1) {
		norm, ok := normalizeHex(match)
		if !ok {
			continue
		}
		add("unregistered_hex", match, norm)
	}
	for _, m := range rgbRe.FindAllStringSubmatch(body, -1) {
		r, g, b := atoiClamp(m[1]), atoiClamp(m[2]), atoiClamp(m[3])
		hex := fmt.Sprintf("#%02x%02x%02x", r, g, b)
		add("unregistered_rgb", m[0], hex)
	}
	for _, m := range hslRe.FindAllStringSubmatch(body, -1) {
		h, _ := strconv.ParseFloat(m[1], 64)
		s, _ := strconv.ParseFloat(m[2], 64)
		l, _ := strconv.ParseFloat(m[3], 64)
		hex := hslToHex(h, s/100, l/100)
		add("unregistered_hsl", m[0], hex)
	}
	return findings
}

func atoiClamp(s string) int {
	n, _ := strconv.Atoi(s)
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}

func hslToHex(h, s, l float64) string {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	return fmt.Sprintf("#%02x%02x%02x", toByte(r+m), toByte(g+m), toByte(b+m))
}

func toByte(v float64) int {
	n := int(math.Round(v * 255))
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}

func lintableDesignFile(rel string) bool {
	rel = filepath.ToSlash(rel)
	base := strings.ToLower(filepath.Base(rel))
	if strings.Contains(base, "template") {
		return false
	}
	if strings.EqualFold(base, "readme.md") {
		return false
	}
	if strings.Contains(rel, "/proof/style-tiles/") {
		return false
	}
	if strings.Contains(rel, "/proof/portable/") || strings.HasPrefix(rel, "docs/design/portable/") {
		return false
	}
	return strings.HasSuffix(rel, ".html") || strings.HasSuffix(rel, ".css")
}

func normalizeHex(raw string) (string, bool) {
	h := strings.TrimPrefix(strings.ToLower(raw), "#")
	switch len(h) {
	case 3:
		return "#" + string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]}), true
	case 6:
		return "#" + h, true
	case 8:
		return "#" + h[:6], true
	default:
		return "", false
	}
}
