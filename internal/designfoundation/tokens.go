package designfoundation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	TokensJSONRel = "packages/design-tokens/tokens.json"
	TokensCSSRel  = "packages/design-tokens/tokens.css"
	PortableRel   = "docs/design/proof/portable/DESIGN.md"
	KernelRel     = "docs/design/DESIGN.md"
	GrammarRel    = "docs/design/design-language.md"
)

// LegacyPortableRel is the pre-convergence location kept for backward
// compatibility in checks/lint (read both, write to PortableRel only).
const LegacyPortableRel = "docs/design/portable/DESIGN.md"

type tokenLeaf struct {
	Path        string
	Type        string
	Value       string
	Description string
}

type tokenFile struct {
	leaves []tokenLeaf
	raw    map[string]any
}

func LoadTokens(root string) (*tokenFile, error) {
	data, err := os.ReadFile(filepath.Join(root, TokensJSONRel))
	if err != nil {
		return nil, fmt.Errorf("design tokens: %w", err)
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("design tokens: decode: %w", err)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("design tokens: root must be an object")
	}
	tf := &tokenFile{raw: obj}
	walkTokens(nil, obj, &tf.leaves)
	if err := tf.resolve(); err != nil {
		return nil, err
	}
	sort.Slice(tf.leaves, func(i, j int) bool { return tf.leaves[i].Path < tf.leaves[j].Path })
	return tf, nil
}

func walkTokens(prefix []string, node map[string]any, out *[]tokenLeaf) {
	if typ, ok := node["$type"].(string); ok {
		leaf := tokenLeaf{Path: strings.Join(prefix, "."), Type: typ, Description: stringField(node, "$description")}
		leaf.Value = encodeValue(node["$value"])
		*out = append(*out, leaf)
		return
	}
	keys := make([]string, 0, len(node))
	for k := range node {
		if strings.HasPrefix(k, "$") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		child, ok := node[k].(map[string]any)
		if !ok {
			continue
		}
		walkTokens(append(append([]string{}, prefix...), k), child, out)
	}
}

func encodeValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int(t)) {
			return strconv.Itoa(int(t))
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			s := encodeValue(item)
			if strings.ContainsAny(s, " \t,") {
				s = strconv.Quote(s)
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ", ")
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func stringField(node map[string]any, key string) string {
	s, _ := node[key].(string)
	return s
}

func (tf *tokenFile) resolve() error {
	byPath := map[string]tokenLeaf{}
	for _, leaf := range tf.leaves {
		byPath[leaf.Path] = leaf
	}
	for i := range tf.leaves {
		resolved, err := resolveRef(tf.leaves[i].Value, byPath, nil)
		if err != nil {
			return fmt.Errorf("token %s: %w", tf.leaves[i].Path, err)
		}
		tf.leaves[i].Value = resolved
		byPath[tf.leaves[i].Path] = tf.leaves[i]
	}
	return nil
}

func resolveRef(value string, byPath map[string]tokenLeaf, stack []string) (string, error) {
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return value, nil
	}
	path := strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}")
	for _, seen := range stack {
		if seen == path {
			return "", fmt.Errorf("cycle at %s", path)
		}
	}
	leaf, ok := byPath[path]
	if !ok {
		return "", fmt.Errorf("missing reference {%s}", path)
	}
	return resolveRef(leaf.Value, byPath, append(stack, path))
}

func (tf *tokenFile) CSS() string {
	var b strings.Builder
	b.WriteString("/* Generated from packages/design-tokens/tokens.json. Do not hand-edit.\n")
	b.WriteString("   loop-harness design-foundation emit-css --root . */\n")
	b.WriteString(":root {\n")
	for _, leaf := range tf.leaves {
		name := cssName(leaf.Path)
		b.WriteString("  ")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(leaf.Value)
		b.WriteString(";\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func cssName(path string) string {
	return "--" + strings.ReplaceAll(path, ".", "-")
}

func (tf *tokenFile) ColorHexes() map[string]string {
	out := map[string]string{}
	for _, leaf := range tf.leaves {
		if leaf.Type != "color" {
			continue
		}
		if n, ok := normalizeHex(leaf.Value); ok {
			out[n] = leaf.Path
		}
	}
	return out
}

func (tf *tokenFile) SemanticColors() []tokenLeaf {
	var out []tokenLeaf
	for _, leaf := range tf.leaves {
		if leaf.Type != "color" {
			continue
		}
		if strings.HasPrefix(leaf.Path, "color.primitive.") {
			continue
		}
		out = append(out, leaf)
	}
	return out
}

func EmitCSSFile(root string) (string, error) {
	tf, err := LoadTokens(root)
	if err != nil {
		return "", err
	}
	css := tf.CSS()
	path := filepath.Join(root, TokensCSSRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(css), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
