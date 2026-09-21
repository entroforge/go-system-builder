// Package sharedmodel checks explicit shared-model inputs using the caller's
// file view. It owns no runtime state and never fetches network resources.
package sharedmodel

import (
	"bytes"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/entroforge/go-system-builder/internal/fileview"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type Result struct {
	Files    map[string][]byte
	Problems []string
	Warnings []string
	Rows     int
}

var linkRE = regexp.MustCompile(`\[[^\]]+\]\(([^\s)]+)\)`)
var fenceRE = regexp.MustCompile("(?ms)^```[^\n]*\n.*?^```[^\n]*$")
var fieldRE = regexp.MustCompile(`(?m)^>\s*Shared model policy:\s*(\S+)\s*$`)
var requirementRE = regexp.MustCompile(`(?m)^>\s*(?:需求|REQ|Requirement)\s*[:：]\s*(REQ-[A-Z0-9-]+)\s*$`)

// Section returns an H2 section, including numbered H2 headings.
func Section(s, name string) string {
	s = fenceRE.ReplaceAllString(s, "")
	active := false
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			heading = strings.TrimLeftFunc(heading, func(r rune) bool { return unicode.IsDigit(r) || r == '.' || r == ' ' })
			if active {
				break
			}
			active = heading == name
			continue
		}
		if active {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// Resolve only accepts local repository files, with optional URL fragments.
func Resolve(root, from, href string) (string, string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "" || u.Host != "" || u.RawQuery != "" || filepath.IsAbs(u.Path) || strings.Contains(u.Path, "\\") {
		return "", "", fmt.Errorf("use a repository-relative file link: %s", href)
	}
	p := from
	if u.Path != "" {
		p = filepath.Join(filepath.Dir(from), filepath.FromSlash(u.Path))
	}
	p = filepath.Clean(p)
	if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("link escapes repository: %s", href)
	}
	return filepath.ToSlash(p), u.Fragment, nil
}

func refs(s string) []string {
	var out []string
	for _, m := range linkRE.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}
func one(root, from, cell string) (string, string, error) {
	r := refs(cell)
	if len(r) != 1 {
		return "", "", fmt.Errorf("expected one Markdown file link in %q", cell)
	}
	return Resolve(root, from, r[0])
}
func key(p, f string) string {
	if f == "" {
		return p
	}
	return p + "#" + f
}

// Check inspects only indexes declaring the policy. Legacy batches are visible
// as warnings; explicit malformed/new policies fail rather than falling back.
func Check(root string, files fileview.Reader, reqIDs ...string) Result {
	r := Result{Files: map[string][]byte{}}
	root, _ = filepath.Abs(root)
	files = bounded(root, files)
	entries, err := files.ReadDir(filepath.Join(root, projectlayout.Contracts))
	if err != nil {
		if !os.IsNotExist(err) {
			r.Problems = append(r.Problems, "read contracts: "+err.Error())
		}
		return r
	}
	expected := map[string]map[string]bool{}
	// A contract index is not necessarily a one-file batch. Keep authority
	// checks grouped by the effective REQ so unrelated requirements do not
	// reject one another during an all-index authoring check.
	slotAuthorities := map[string]map[string]string{}
	schemaAuthorities := map[string]map[string]string{}
	schemaConflicts := map[string]map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "CONTRACTS-") || !strings.HasSuffix(e.Name(), ".md") || strings.Contains(strings.ToLower(e.Name()), "template") {
			continue
		}
		rel := "docs/dev/contracts/" + e.Name()
		b, err := files.ReadFile(filepath.Join(root, rel))
		if err != nil {
			r.Problems = append(r.Problems, err.Error())
			continue
		}
		content := string(b)
		declaredReq := requirementRE.FindStringSubmatch(content)
		reqScope := fallbackRequirement(e.Name())
		if declaredReq != nil {
			reqScope = declaredReq[1]
		}
		if len(reqIDs) > 0 && reqIDs[0] != "" {
			req := reqIDs[0]
			if declaredReq != nil {
				if declaredReq[1] != req {
					continue
				}
			} else if e.Name() != "CONTRACTS-"+strings.TrimPrefix(req, "REQ-")+".md" {
				continue
			}
			// A filename fallback is the bound batch's effective REQ. It is
			// deliberately applied only after the existing selection rule above.
			if declaredReq == nil {
				reqScope = req
			}
		}
		m := fieldRE.FindStringSubmatch(content)
		if m == nil {
			if regexp.MustCompile(`(?m)^>\s*Shared model policy:`).MatchString(content) {
				r.Problems = append(r.Problems, rel+": malformed Shared model policy")
				continue
			}
			r.Warnings = append(r.Warnings, rel+": legacy batch has no Shared model policy; shared-model consistency is not verified")
			continue
		}
		if m[1] == "none" {
			reason := regexp.MustCompile(`(?m)^>\s*Shared model reason:\s*(.+)$`).FindStringSubmatch(content)
			if reason == nil || strings.TrimSpace(reason[1]) == "" || strings.Contains(reason[1], "{") {
				r.Problems = append(r.Problems, rel+": policy none requires a concrete Shared model reason")
			}
			continue
		}
		if m[1] != "json-schema-v1" {
			r.Problems = append(r.Problems, rel+": unsupported Shared model policy "+m[1])
			continue
		}
		before := r.Rows
		slots := slotAuthorities[reqScope]
		if slots == nil {
			slots = map[string]string{}
			slotAuthorities[reqScope] = slots
		}
		for _, line := range strings.Split(Section(content, "Shared model baseline"), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "|") {
				continue
			}
			cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
			if len(cells) == 0 || cells[0] == "Operation" || strings.HasPrefix(cells[0], ":-") || strings.HasPrefix(cells[0], "--") {
				continue
			}
			r.Rows++
			if len(cells) != 6 {
				r.Problems = append(r.Problems, rel+": shared baseline needs six columns")
				continue
			}
			label := rel + " " + cells[0] + "/" + cells[1]
			fail := func(err error) { r.Problems = append(r.Problems, label+": "+err.Error()) }
			if cells[0] == "" || cells[1] == "" || strings.Contains(cells[0]+cells[1], "{") {
				fail(fmt.Errorf("declare a concrete operation and slot"))
				continue
			}
			p, f, err := one(root, rel, cells[2])
			if err != nil {
				fail(err)
				continue
			}
			slot := cells[0] + "/" + cells[1]
			authority := key(p, f)
			if previous, ok := slots[slot]; ok && previous != authority {
				fail(fmt.Errorf("duplicate operation/slot authority in %s: %s conflicts with %s", reqScope, authority, previous))
				continue
			}
			// Reusing the same source for a slot is intentional and legal. This
			// also makes repeated declarations across same-REQ indexes harmless.
			slots[slot] = authority
			loader := &viewLoader{root: root, files: files, loaded: r.Files}
			compiler := jsonschema.NewCompiler()
			compiler.UseLoader(loader)
			compiler.DefaultDraft(jsonschema.Draft2020)
			schemaURL := (&url.URL{Scheme: "file", Path: filepath.Join(root, p), Fragment: f}).String()
			sch, err := compiler.Compile(schemaURL)
			if err != nil {
				fail(fmt.Errorf("schema %s: %w", key(p, f), err))
				continue
			}
			recordSchemaAuthorities(reqScope, root, loader.schemaDocs, schemaAuthorities, schemaConflicts, &r.Problems)
			consumers := refs(cells[3])
			if len(consumers) == 0 {
				fail(fmt.Errorf("declare consumer contract links"))
			}
			for _, ref := range consumers {
				cp, cf, err := Resolve(root, rel, ref)
				if err != nil {
					fail(err)
					continue
				}
				base := filepath.Base(cp)
				if filepath.Dir(cp) != projectlayout.Contracts || !strings.HasSuffix(base, ".md") || !(strings.HasPrefix(base, "FE-") || strings.HasPrefix(base, "BE-") || strings.HasPrefix(base, "SYNC-")) {
					fail(fmt.Errorf("consumer must be a FE/BE/SYNC contract: %s", cp))
					continue
				}
				if expected[cp] == nil {
					expected[cp] = map[string]bool{}
				}
				expected[cp][key(p, f)] = true
				cb, err := files.ReadFile(filepath.Join(root, cp))
				if err != nil {
					fail(err)
					continue
				}
				if cf != "" && !hasAnchor(string(cb), cf) {
					fail(fmt.Errorf("missing consumer anchor %s#%s", cp, cf))
				}
				found := false
				for _, cr := range refs(Section(string(cb), "Shared model inputs")) {
					rp, rf, err := Resolve(root, cp, cr)
					if err == nil && key(rp, rf) == key(p, f) {
						found = true
					}
				}
				if !found {
					fail(fmt.Errorf("%s must reference %s in Shared model inputs", cp, key(p, f)))
				}
			}
			for i := 4; i <= 5; i++ {
				if i == 5 && cells[i] == "N/A" {
					continue
				}
				ep, ef, err := one(root, rel, cells[i])
				if err != nil {
					fail(err)
					continue
				}
				if ef != "" {
					fail(fmt.Errorf("example must link a whole JSON file"))
					continue
				}
				b, err := files.ReadFile(filepath.Join(root, ep))
				if err != nil {
					fail(err)
					continue
				}
				r.Files[ep] = b
				instance, parseErr := jsonschema.UnmarshalJSON(bytes.NewReader(b))
				if err := parseErr; err != nil {
					fail(fmt.Errorf("example %s must be valid JSON: %w", ep, err))
					continue
				}
				err = sch.Validate(instance)
				if i == 4 && err != nil {
					fail(fmt.Errorf("valid example %s rejected: %w", ep, err))
				}
				if i == 5 && err == nil {
					fail(fmt.Errorf("structural negative %s unexpectedly passes; business negatives belong in CASE/CT", ep))
				}
			}
		}
		if r.Rows == before {
			r.Problems = append(r.Problems, rel+": json-schema-v1 requires a Shared model baseline table")
		}
	}
	for cp, definitions := range expected {
		cb, err := files.ReadFile(filepath.Join(root, cp))
		if err != nil {
			continue
		}
		for _, ref := range refs(Section(string(cb), "Shared model inputs")) {
			rp, rf, err := Resolve(root, cp, ref)
			if err != nil {
				r.Problems = append(r.Problems, cp+": "+err.Error())
				continue
			}
			if strings.HasSuffix(rp, ".json") && !definitions[key(rp, rf)] {
				r.Problems = append(r.Problems, cp+": undeclared shared model input "+key(rp, rf))
			}
		}
	}
	// Shared design declares committed inputs at this upstream boundary;
	// explicit disk authoring remains supported, but production source rules
	// cannot relabel a model/example as mutable evidence.
	if view, ok := files.(interface{ Source(string) (string, error) }); ok {
		for path := range r.Files {
			source, err := view.Source(path)
			if err != nil || source != "git_tree" {
				r.Problems = append(r.Problems, path+": shared design inputs require git_tree; disk evidence cannot satisfy the model baseline")
			}
		}
	}
	sort.Strings(r.Problems)
	sort.Strings(r.Warnings)
	return r
}

type viewLoader struct {
	root       string
	files      fileview.Reader
	loaded     map[string][]byte
	schemaDocs map[string][]byte
}

func (l *viewLoader) Load(raw string) (any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "file" || u.Host != "" {
		return nil, fmt.Errorf("remote schema dependency is not pinned locally: %s", raw)
	}
	rel, err := filepath.Rel(l.root, filepath.FromSlash(u.Path))
	if err != nil {
		return nil, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("schema dependency escapes repository")
	}
	b, err := l.files.ReadFile(filepath.Join(l.root, rel))
	if err != nil {
		return nil, err
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	l.loaded[rel] = b
	if l.schemaDocs == nil {
		l.schemaDocs = map[string][]byte{}
	}
	l.schemaDocs[rel] = b
	return v, nil
}

// recordSchemaAuthorities records only documents the JSON Schema compiler
// actually loaded. Examples and other Result.Files entries are data instances,
// so an ordinary "$id" field in them must not become a schema authority.
func recordSchemaAuthorities(scope, root string, docs map[string][]byte, authorities map[string]map[string]string, conflicts map[string]map[string]bool, problems *[]string) {
	if len(docs) == 0 {
		return
	}
	ids := authorities[scope]
	if ids == nil {
		ids = map[string]string{}
		authorities[scope] = ids
	}
	seenConflicts := conflicts[scope]
	if seenConflicts == nil {
		seenConflicts = map[string]bool{}
		conflicts[scope] = seenConflicts
	}
	docPaths := make([]string, 0, len(docs))
	for rel := range docs {
		docPaths = append(docPaths, rel)
	}
	sort.Strings(docPaths)
	for _, rel := range docPaths {
		data := docs[rel]
		resourceIDs, err := schemaResourceIDs(root, rel, data)
		if err != nil {
			*problems = append(*problems, fmt.Sprintf("%s: cannot inspect schema resources: %v", rel, err))
			continue
		}
		resourceIDList := make([]string, 0, len(resourceIDs))
		for id := range resourceIDs {
			resourceIDList = append(resourceIDList, id)
		}
		sort.Strings(resourceIDList)
		for _, id := range resourceIDList {
			if previous, ok := ids[id]; ok {
				if previous == rel || seenConflicts[id] {
					continue
				}
				seenConflicts[id] = true
				*problems = append(*problems, fmt.Sprintf("duplicate schema $id %q in %s and %s for %s", id, previous, rel, scope))
				continue
			}
			ids[id] = rel
		}
	}
}

// schemaResourceIDs follows the standard schema-bearing locations. It does
// not walk arbitrary JSON objects: values under examples/default/const/enum
// are instances and may legitimately contain a property named "$id".
func schemaResourceIDs(root, rel string, data []byte) (map[string]bool, error) {
	var value any
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	base := (&url.URL{Scheme: "file", Path: filepath.Join(root, filepath.FromSlash(rel))}).String()
	ids := map[string]bool{}
	collectSchemaResourceIDs(value, base, 2020, true, ids)
	return ids, nil
}

// Match jsonschema/v6's resource discovery: only resource roots can change
// dialect, and pre-2019 $ref siblings and fragment-only IDs are not resources.
func collectSchemaResourceIDs(value any, base string, inheritedDraft int, root bool, ids map[string]bool) {
	obj, ok := value.(map[string]any)
	if !ok {
		return
	}
	draft := schemaDraft(obj, inheritedDraft)
	raw := schemaResourceID(obj, draft)
	if raw == "" && !root {
		draft = inheritedDraft
		raw = schemaResourceID(obj, draft)
	}
	childDraft := inheritedDraft
	if root || raw != "" {
		childDraft = draft
	}
	if raw != "" {
		if resolved, ok := resolveSchemaID(base, raw); ok {
			ids[resolved] = true
			base = resolved
		}
	}
	for _, child := range schemaChildren(obj, draft) {
		switch v := child.(type) {
		case map[string]any:
			collectSchemaResourceIDs(v, base, childDraft, false, ids)
		case []any:
			for _, item := range v {
				collectSchemaResourceIDs(item, base, childDraft, false, ids)
			}
		}
	}
}

func schemaResourceID(obj map[string]any, draft int) string {
	if draft < 2019 {
		if _, hasRef := obj["$ref"]; hasRef {
			return ""
		}
	}
	key := "$id"
	if draft == 4 {
		key = "id"
	}
	raw, _ := obj[key].(string)
	id, _, _ := strings.Cut(raw, "#")
	return id
}

func schemaDraft(obj map[string]any, inherited int) int {
	raw, ok := obj["$schema"].(string)
	if !ok {
		return inherited
	}
	switch {
	case strings.Contains(raw, "draft-04"):
		return 4
	case strings.Contains(raw, "draft-06"):
		return 6
	case strings.Contains(raw, "draft-07"):
		return 7
	case strings.Contains(raw, "draft/2019-09"):
		return 2019
	case strings.Contains(raw, "draft/2020-12"):
		return 2020
	default:
		return inherited
	}
}

func resolveSchemaID(base, raw string) (string, bool) {
	// schemaResourceID already separated anchor fragments from resource IDs.
	ref, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	parent, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	return parent.ResolveReference(ref).String(), true
}

func fallbackRequirement(name string) string {
	const prefix = "CONTRACTS-"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".md") {
		return "index:" + name
	}
	suffix := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".md")
	if suffix == "" {
		return "index:" + name
	}
	return "REQ-" + suffix
}

func schemaChildren(obj map[string]any, draft int) []any {
	// Keep this list aligned with the jsonschema/v6 Draft.subschemas tables:
	// later drafts inherit the legacy locations even where the specification
	// has made some of them obsolete. That is the compiler's actual resource
	// recognition boundary for this adapter.
	keys := []string{"definitions", "properties", "patternProperties", "additionalProperties", "items", "additionalItems", "dependencies", "allOf", "anyOf", "oneOf", "not"}
	if draft >= 6 {
		keys = append(keys, "propertyNames", "contains")
	}
	if draft >= 7 {
		keys = append(keys, "if", "then", "else")
	}
	if draft >= 2019 {
		keys = append(keys, "$defs", "dependentSchemas", "unevaluatedProperties", "unevaluatedItems", "contentSchema")
	}
	if draft >= 2020 {
		keys = append(keys, "prefixItems")
	}
	var children []any
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			switch value := value.(type) {
			case map[string]any:
				// Map-valued schema dictionaries contain one schema per value;
				// the map itself is not a schema object.
				if key == "$defs" || key == "definitions" || key == "properties" || key == "patternProperties" || key == "dependentSchemas" || key == "dependencies" {
					for _, child := range value {
						children = append(children, child)
					}
				} else {
					children = append(children, value)
				}
			default:
				children = append(children, value)
			}
		}
	}
	return children
}

// Reading checks only links in TASK Document Manifest (not background links).
// Optional/conditional rows are advisory and therefore omitted here.
func Reading(root, rel, content string, files fileview.Reader) []string {
	if !strings.Contains(Section(content, "Document Manifest"), "](") && !strings.Contains(content, "> Reading policy:") {
		return nil
	}
	root, _ = filepath.Abs(root)
	files = bounded(root, files)
	var problems []string
	seen := map[string]bool{}
	policy := regexp.MustCompile(`(?m)^>\s*Reading policy:\s*(\S+)`).FindStringSubmatch(content)
	strict := policy != nil
	if strict && policy[1] != "linked-v1" {
		return []string{rel + ": unknown Reading policy " + policy[1]}
	}
	required := 0
	for _, line := range strings.Split(Section(content, "Document Manifest"), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) == 0 || cells[0] == "Order" || strings.HasPrefix(cells[0], ":-") || strings.HasPrefix(cells[0], "--") {
			continue
		}
		if strict {
			if len(cells) != 7 || len(refs(cells[3])) != 1 || cells[5] == "" {
				problems = append(problems, rel+": linked-v1 requires seven columns, one Path link and a reading Purpose")
				continue
			}
		}
		if len(cells) >= 7 {
			switch cells[6] {
			case "optional", "conditional":
				continue
			case "required":
			default:
				problems = append(problems, rel+": unknown reading Mode "+cells[6])
				continue
			}
		}
		var links []string
		if len(cells) >= 4 {
			links = refs(cells[3])
		}
		for _, ref := range links {
			required++
			p, f, err := Resolve(root, rel, ref)
			if err == nil {
				k := key(p, f)
				if seen[k] {
					problems = append(problems, rel+": duplicate required reading "+k)
					continue
				}
				seen[k] = true
				var b []byte
				b, err = files.ReadFile(filepath.Join(root, p))
				if err == nil && f != "" {
					if strings.HasSuffix(p, ".json") {
						compiler := jsonschema.NewCompiler()
						compiler.UseLoader(&viewLoader{root: root, files: files, loaded: map[string][]byte{}})
						abs, _ := filepath.Abs(filepath.Join(root, p))
						_, err = compiler.Compile((&url.URL{Scheme: "file", Path: abs, Fragment: f}).String())
					} else if !hasAnchor(string(b), f) {
						err = fmt.Errorf("missing Markdown anchor %s#%s (use explicit stable anchors)", p, f)
					}
				}
			}
			if err != nil {
				problems = append(problems, rel+": required reading: "+err.Error())
			}
		}
	}
	if strict && required == 0 {
		problems = append(problems, rel+": linked-v1 requires at least one required reading link")
	}
	return problems
}

// ReadingPath lets existing context-load estimates count linked files without
// maintaining another manifest. A legacy bare path remains repository-relative.
func ReadingPath(root, rel, cell string) string {
	if r := refs(cell); len(r) == 1 {
		p, _, err := Resolve(root, rel, r[0])
		if err == nil {
			return p
		}
		return ""
	}
	return strings.Trim(cell, "` ")
}
func hasAnchor(s, anchor string) bool {
	s = fenceRE.ReplaceAllString(s, "")
	if strings.Contains(s, `id="`+anchor+`"`) || strings.Contains(s, `id='`+anchor+`'`) || strings.Contains(s, "{#"+anchor+"}") {
		return true
	}
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		h := strings.TrimSpace(strings.TrimLeft(line, "#"))
		h = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
				return unicode.ToLower(r)
			}
			if r == ' ' {
				return '-'
			}
			return -1
		}, h)
		if h == anchor {
			return true
		}
	}
	return false
}
