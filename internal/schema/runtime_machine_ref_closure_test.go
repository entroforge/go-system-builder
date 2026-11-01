package schema_test

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// A Runtime Writer validates retained packets from other stages too. Follow
// referenced fragments, rather than scanning every definition in every file.
func TestRuntimeReferencedMachineMetadataHasNoQualification(t *testing.T) {
	cache := map[string]map[string]any{}
	load := func(name string) map[string]any {
		if d := cache[name]; d != nil {
			return d
		}
		data, err := schema.ReadAsset(name)
		if err != nil {
			t.Fatal(err)
		}
		var d map[string]any
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatal(err)
		}
		cache[name] = d
		return d
	}
	resolve := func(file, ref string) (string, string, map[string]any) {
		parts := strings.SplitN(ref, "#", 2)
		if parts[0] != "" {
			file = path.Base(parts[0])
		}
		fragment := ""
		var value any = load(file)
		if len(parts) == 2 {
			fragment = parts[1]
			for _, key := range strings.Split(strings.TrimPrefix(fragment, "/"), "/") {
				if key != "" {
					key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
					value = value.(map[string]any)[key]
				}
			}
		}
		return file, fragment, value.(map[string]any)
	}
	machine := regexp.MustCompile(`(?i)(^|_)(sha256|hash|digest|fingerprint)(_|$)`)
	gitCoordinates := map[string]bool{"source_head": true, "target_head": true, "tested_head": true, "merge_commit": true, "merge_base": true, "start_commit": true, "base_commit": true, "head_commit": true, "source_commit": true, "commit_oid": true, "oid": true}
	var checkScalar func(string, string, map[string]any)
	checkScalar = func(file, location string, node map[string]any) {
		if ref, ok := node["$ref"].(string); ok {
			nextFile, fragment, target := resolve(file, ref)
			checkScalar(nextFile, fragment, target)
		}
		// Container metadata such as document_fingerprints remains a real
		// structural projection; its individual machine strings are visited.
		if node["type"] == "object" || node["type"] == "array" {
			return
		}
		for _, keyword := range []string{"pattern", "minLength", "maxLength", "format", "enum", "const"} {
			if _, exists := node[keyword]; exists {
				t.Errorf("%s#%s: legacy machine value still has %s qualification", file, location, keyword)
			}
		}
		for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
			for i, raw := range asSchemaList(node[keyword]) {
				checkScalar(file, fmt.Sprintf("%s/%s/%d", location, keyword, i), raw.(map[string]any))
			}
		}
	}
	seen := map[string]bool{}
	var visit func(string, string, map[string]any)
	visit = func(file, location string, node map[string]any) {
		key := file + "#" + location
		if seen[key] {
			return
		}
		seen[key] = true
		if ref, ok := node["$ref"].(string); ok {
			nextFile, fragment, target := resolve(file, ref)
			visit(nextFile, fragment, target)
		}
		properties, _ := node["properties"].(map[string]any)
		for name, raw := range properties {
			child := raw.(map[string]any)
			childPath := location + "/properties/" + name
			if machine.MatchString(name) || gitCoordinates[name] {
				checkScalar(file, childPath, child)
				for _, required := range asSchemaList(node["required"]) {
					if required == name {
						t.Errorf("%s#%s: legacy machine metadata remains required", file, childPath)
					}
				}
			}
			visit(file, childPath, child)
		}
		for _, keyword := range []string{"additionalProperties", "unevaluatedProperties", "items", "contains", "propertyNames", "not", "if", "then", "else"} {
			if child, ok := node[keyword].(map[string]any); ok {
				visit(file, location+"/"+keyword, child)
			}
		}
		for _, keyword := range []string{"patternProperties", "dependentSchemas"} {
			children, _ := node[keyword].(map[string]any)
			for name, raw := range children {
				visit(file, location+"/"+keyword+"/"+name, raw.(map[string]any))
			}
		}
		for _, keyword := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
			for i, raw := range asSchemaList(node[keyword]) {
				visit(file, fmt.Sprintf("%s/%s/%d", location, keyword, i), raw.(map[string]any))
			}
		}
	}
	for _, file := range []string{"loop-state.schema.json", "loop-state-v1.1.schema.json", "loop-state-v1.2.schema.json", "loop-state-v1.3.schema.json"} {
		visit(file, "", load(file))
	}
}

func asSchemaList(value any) []any {
	list, _ := value.([]any)
	return list
}

func TestRuntimeHistoricalMachineScalarsAcceptAnyJSON(t *testing.T) {
	machine := regexp.MustCompile(`(?i)(^|_)(sha256|sha|hash|digest|fingerprint)(_|$)`)
	gitCoordinates := map[string]bool{"bound_head": true, "base_commit": true, "start_commit": true, "bound_commit": true, "source_head": true}
	for _, file := range []string{"loop-state.schema.json", "loop-state-v1.1.schema.json", "loop-state-v1.2.schema.json", "loop-state-v1.3.schema.json"} {
		data, err := schema.ReadAsset(file)
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]any
		if err := json.Unmarshal(data, &root); err != nil {
			t.Fatal(err)
		}
		var walk func(any, string)
		walk = func(value any, location string) {
			switch node := value.(type) {
			case map[string]any:
				if props, ok := node["properties"].(map[string]any); ok {
					for name, raw := range props {
						child, ok := raw.(map[string]any)
						if !ok || (!machine.MatchString(name) && !gitCoordinates[name]) {
							continue
						}
						if child["type"] == "object" || child["type"] == "array" {
							continue
						}
						if _, restricted := child["type"]; restricted {
							t.Errorf("%s#%s/%s still imposes a machine value type", file, location, name)
						}
					}
				}
				for key, child := range node {
					walk(child, location+"/"+key)
				}
			case []any:
				for i, child := range node {
					walk(child, fmt.Sprintf("%s/%d", location, i))
				}
			}
		}
		walk(root, "")
	}
}
