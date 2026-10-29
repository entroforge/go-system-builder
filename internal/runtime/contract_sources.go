package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const contractScenarioRoot = "docs/design/prototypes"

var (
	contractModuleName   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	contractMarkdownLink = regexp.MustCompile(`\[([^\]]+)\]\(([^\s)]+)\)`)
	contractKnownPath    = regexp.MustCompile(`docs/design/decisions/ADR-[A-Za-z0-9._/-]+`)
	contractREQTableID   = regexp.MustCompile(`^(AC|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	contractADRIdentity  = regexp.MustCompile(`ADR-[A-Z0-9]+(?:-[A-Z0-9]+)*`)
	contractADRStatus    = regexp.MustCompile(`(?m)^>\s*(?:状态|Status)\s*[:：]\s*([^\s]+)`)
	contractADRReq       = regexp.MustCompile("(?m)^>\\s*(?:关联需求|REQ|Requirement)\\s*[:：]\\s*`?([^`\\r\\n]+?)`?\\s*$")
)

// ResolveContractInputs closes the original S3 source set from the bound REQ,
// current-generation registered designs, and the same module-package root
// enumerated by the S2 scenario bridge. It reads source bytes only through the
// caller's file view, and requires those bytes to match the live authority
// checkout before returning an issuable set.
func ResolveContractInputs(root string, state map[string]any, files fileview.Reader) (workpacket.ContractInputs, error) {
	if files == nil {
		return workpacket.ContractInputs{}, fmt.Errorf("S3 requires original authoritative file view")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return workpacket.ContractInputs{}, err
	}
	if err := fileview.ValidateAuthority(absoluteRoot, state); err != nil {
		return workpacket.ContractInputs{}, err
	}
	binding, designs, err := CurrentContractBinding(state)
	if err != nil {
		return workpacket.ContractInputs{}, err
	}
	inputs := workpacket.ContractInputs{
		Designs:     append([]workpacket.Reference(nil), designs...),
		Sources:     []workpacket.Subject{},
		Directories: map[string][]string{},
	}
	known := map[string]bool{binding.Requirement.Path: true}
	for _, design := range designs {
		if known[design.Path] {
			return workpacket.ContractInputs{}, fmt.Errorf("S3 registered design duplicates another frozen input: %s", design.Path)
		}
		known[design.Path] = true
	}
	loaded := map[string][]byte{}
	readFrozen := func(path, expectedSHA string) ([]byte, error) {
		if !safeContractPath(path) {
			return nil, fmt.Errorf("S3 source path is not repository-relative: %s", path)
		}
		viewBytes, err := files.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read S3 source through original file view %s: %w", path, err)
		}
		diskBytes, err := workpacket.ReadFile(absoluteRoot, path, 4<<20)
		if err != nil {
			return nil, fmt.Errorf("read S3 source from authority checkout %s: %w", path, err)
		}
		if (expectedSHA != "" && (workpacket.Digest(viewBytes) != expectedSHA || workpacket.Digest(diskBytes) != expectedSHA)) || !bytes.Equal(viewBytes, diskBytes) {
			return nil, fmt.Errorf("S3 frozen input differs from original checkout SHA: %s", path)
		}
		loaded[path] = append([]byte(nil), viewBytes...)
		return viewBytes, nil
	}
	if _, err := readFrozen(binding.Requirement.Path, binding.Requirement.SHA256); err != nil {
		return workpacket.ContractInputs{}, err
	}
	// S2 records which design was actually reviewed. Its historical bytes do
	// not freeze a later committed explanation before S3 has been issued.
	for i, design := range designs {
		data, err := readFrozen(design.Path, "")
		if err != nil {
			return workpacket.ContractInputs{}, err
		}
		inputs.Designs[i].SHA256 = workpacket.Digest(data)
	}

	// The bridge's actual source rule is project-wide: it enumerates every
	// module directory directly under docs/design/prototypes. Freeze that
	// membership first, then each package's recursive file and directory set.
	modules, err := contractModuleDirectories(absoluteRoot, files, inputs.Directories)
	if err != nil {
		return workpacket.ContractInputs{}, err
	}
	if len(modules) == 0 {
		if err := requireExplicitNoScenarioExit(loaded[binding.Requirement.Path]); err != nil {
			return workpacket.ContractInputs{}, err
		}
	}
	for _, module := range modules {
		if err := collectContractPackage(absoluteRoot, files, module, &inputs, known, loaded); err != nil {
			return workpacket.ContractInputs{}, err
		}
	}
	adrs, err := contractADRsForRequirement(absoluteRoot, files, binding.Requirement)
	if err != nil {
		return workpacket.ContractInputs{}, err
	}
	for path, data := range adrs {
		if !known[path] {
			known[path] = true
			inputs.Sources = append(inputs.Sources, workpacket.Subject{Path: path, SHA256: workpacket.Digest(data)})
		}
		loaded[path] = data
	}
	// Only explicit local design references are followed. This finds ADRs and
	// architecture data-model/state sources named by the registered designs or
	// module package; linked ADR/model documents are then followed transitively.
	queue := []string{}
	for path, content := range loaded {
		queue = append(queue, contractInitialDependencies(path, content)...)
	}
	sort.Strings(queue)
	queued := map[string]bool{}
	schemaSources := map[string]bool{}
	for path, content := range loaded {
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			continue
		}
		for _, match := range contractMarkdownLink.FindAllStringSubmatch(string(contractReferenceText(content)), -1) {
			candidate, ok := resolveContractLink(path, match[2])
			if ok && contractLinkNamesSchema(match[1], candidate) {
				schemaSources[candidate] = true
			}
		}
	}
	for _, path := range queue {
		queued[path] = true
	}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		if err := validateContractDependencyPath(path); err != nil {
			return workpacket.ContractInputs{}, err
		}
		if known[path] {
			continue
		}
		known[path] = true
		data, err := readConditionalSource(absoluteRoot, files, path)
		if err != nil {
			return workpacket.ContractInputs{}, err
		}
		inputs.Sources = append(inputs.Sources, workpacket.Subject{Path: path, SHA256: workpacket.Digest(data)})
		loaded[path] = data
		for _, dependency := range contractTransitiveDependencies(path, data, schemaSources) {
			if !queued[dependency] {
				queued[dependency] = true
				queue = append(queue, dependency)
			}
		}
	}

	sort.Slice(inputs.Sources, func(i, j int) bool { return inputs.Sources[i].Path < inputs.Sources[j].Path })
	if _, err := workpacket.BuildContracts(binding, inputs); err != nil {
		return workpacket.ContractInputs{}, fmt.Errorf("S3 source closure is not representable by the registered contract packet: %w", err)
	}
	if verified, ok := files.(interface{ Verify() error }); ok {
		if err := verified.Verify(); err != nil {
			return workpacket.ContractInputs{}, fmt.Errorf("S3 original file view moved while resolving sources: %w", err)
		}
	}
	return inputs, nil
}

// contractModuleDirectories mirrors scenario's module source enumeration.
// If the prototypes root is absent, the docs/design manifest records
// that absence without requiring a schema field for missing paths.
func contractModuleDirectories(root string, files fileview.Reader, manifests map[string][]string) ([]string, error) {
	entries, err := files.ReadDir(contractScenarioRoot)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("enumerate S2 module source root %s: %w", contractScenarioRoot, err)
		}
		parent := filepath.ToSlash(filepath.Dir(contractScenarioRoot))
		parentEntries, parentErr := files.ReadDir(parent)
		if parentErr != nil {
			return nil, fmt.Errorf("cannot prove absence of S2 module source root without docs/design manifest: %w", parentErr)
		}
		if err := recordContractDirectory(root, files, parent, parentEntries, manifests); err != nil {
			return nil, err
		}
		return []string{}, nil
	}
	if err := recordContractDirectory(root, files, contractScenarioRoot, entries, manifests); err != nil {
		return nil, err
	}
	return ContractModulePaths(entries)
}

// ContractModulePaths selects the module paths consumed by the original S3
// source resolver. Guidance may use the same selection without reading source
// contents or claiming that a frozen source closure has been validated.
func ContractModulePaths(entries []os.DirEntry) ([]string, error) {
	modules := []string{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("S3 module source root contains a symlink: %s", entry.Name())
		}
		if !entry.IsDir() || entry.Name() == "template" || entry.Name() == "templates" {
			continue
		}
		if !contractModuleName.MatchString(entry.Name()) {
			return nil, fmt.Errorf("S3 scenario module directory has invalid source identity: %s", entry.Name())
		}
		modules = append(modules, filepath.ToSlash(filepath.Join(contractScenarioRoot, entry.Name())))
	}
	sort.Strings(modules)
	return modules, nil
}

func collectContractPackage(root string, files fileview.Reader, modulePath string, inputs *workpacket.ContractInputs, known map[string]bool, loaded map[string][]byte) error {
	var walk func(string) error
	walk = func(dir string) error {
		entries, err := files.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("enumerate S2 scenario package %s: %w", dir, err)
		}
		if err := recordContractDirectory(root, files, dir, entries, inputs.Directories); err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("S3 scenario package contains a symlink: %s", path)
			}
			if entry.IsDir() {
				if err := walk(path); err != nil {
					return err
				}
				continue
			}
			if entry.Type().IsRegular() == false && entry.Type() != 0 {
				return fmt.Errorf("S3 scenario source is not a regular file: %s", path)
			}
			if known[path] {
				return fmt.Errorf("S3 scenario package repeats a registered source: %s", path)
			}
			data, err := readConditionalSource(root, files, path)
			if err != nil {
				return err
			}
			known[path] = true
			loaded[path] = data
			inputs.Sources = append(inputs.Sources, workpacket.Subject{Path: path, SHA256: workpacket.Digest(data)})
		}
		return nil
	}
	return walk(modulePath)
}

func recordContractDirectory(root string, files fileview.Reader, path string, entries []os.DirEntry, manifests map[string][]string) error {
	if previous, ok := manifests[path]; ok {
		return fmt.Errorf("S3 source directory was selected more than once: %s (%d existing members)", path, len(previous))
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	liveEntries, err := liveContractDirectoryEntries(root, path)
	if err != nil {
		return fmt.Errorf("inspect S3 source directory in authority checkout %s: %w", path, err)
	}
	if !sameDirectoryEntries(entries, liveEntries) {
		return fmt.Errorf("S3 source directory differs between original file view and authority checkout: %s", path)
	}
	manifests[path] = names
	return nil
}

func liveContractDirectoryEntries(root, path string) ([]os.DirEntry, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("directory escapes authority root")
	}
	current := root
	if clean == "." {
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("authority root is a symlink or non-directory")
		}
	} else {
		for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
			current = filepath.Join(current, filepath.FromSlash(part))
			info, err := os.Lstat(current)
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("directory path contains a symlink or non-directory component")
			}
		}
	}
	entries, err := os.ReadDir(current)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func sameDirectoryEntries(a, b []os.DirEntry) bool {
	if len(a) != len(b) {
		return false
	}
	copyEntries := func(entries []os.DirEntry) []os.DirEntry {
		copy := append([]os.DirEntry(nil), entries...)
		sort.Slice(copy, func(i, j int) bool { return copy[i].Name() < copy[j].Name() })
		return copy
	}
	a = copyEntries(a)
	b = copyEntries(b)
	for i := range a {
		if a[i].Name() != b[i].Name() || a[i].IsDir() != b[i].IsDir() || a[i].Type() != b[i].Type() {
			return false
		}
	}
	return true
}

func readConditionalSource(root string, files fileview.Reader, path string) ([]byte, error) {
	if !safeContractPath(path) {
		return nil, fmt.Errorf("S3 conditional source path escapes authority: %s", path)
	}
	viewBytes, err := files.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read S3 conditional source through original file view %s: %w", path, err)
	}
	diskBytes, err := workpacket.ReadFile(root, path, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("read S3 conditional source from authority checkout %s: %w", path, err)
	}
	if !bytes.Equal(viewBytes, diskBytes) {
		return nil, fmt.Errorf("S3 conditional source differs from original checkout: %s", path)
	}
	return viewBytes, nil
}

// contractReferenceText excludes Markdown examples, not natural-language claims.
// Model/state dependencies are expressed by links, never by bare path mentions.
func contractReferenceText(data []byte) []byte {
	var out strings.Builder
	var fence byte
	fenceLength := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		marker, length := byte(0), 0
		if len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			marker = trimmed[0]
			for length < len(trimmed) && trimmed[length] == marker {
				length++
			}
		}
		if fence != 0 {
			if marker == fence && length >= fenceLength && strings.TrimSpace(trimmed[length:]) == "" {
				fence = 0
			}
			continue
		}
		if length >= 3 {
			fence, fenceLength = marker, length
			continue
		}
		for len(line) > 0 {
			start := strings.IndexByte(line, '`')
			if start < 0 {
				out.WriteString(line)
				break
			}
			out.WriteString(line[:start])
			width := 1
			for start+width < len(line) && line[start+width] == '`' {
				width++
			}
			delimiter := line[start : start+width]
			end := strings.Index(line[start+width:], delimiter)
			if end < 0 {
				out.WriteString(line[start:])
				break
			}
			out.WriteByte(' ')
			line = line[start+width+end+width:]
		}
		out.WriteByte('\n')
	}
	return []byte(out.String())
}

func contractInitialDependencies(from string, data []byte) []string {
	set := map[string]bool{}
	add := func(path string) {
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path != "." && contractInitialDependency(path) {
			set[path] = true
		}
	}
	for _, match := range contractKnownPath.FindAll(data, -1) {
		candidate := strings.TrimRight(string(match), ".,;:!?)]}")
		add(candidate)
	}
	for _, match := range contractADRIdentity.FindAll(data, -1) {
		add(filepath.ToSlash(filepath.Join("docs/design/decisions", string(match)+".md")))
	}
	if strings.HasSuffix(strings.ToLower(from), ".md") {
		for _, match := range contractMarkdownLink.FindAllStringSubmatch(string(contractReferenceText(data)), -1) {
			candidate, ok := resolveContractLink(from, match[2])
			if ok && contractInitialDependency(candidate) {
				add(candidate)
			}
		}
	}
	return sortedKeys(set)
}

func contractTransitiveDependencies(from string, data []byte, schemaSources map[string]bool) []string {
	set := map[string]bool{}
	add := func(path string) {
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path != "." && path != from && safeContractDependency(path) {
			set[path] = true
		}
	}
	if strings.HasSuffix(strings.ToLower(from), ".json") && (schemaSources[from] || strings.HasSuffix(strings.ToLower(from), ".schema.json")) {
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			set["!invalid-model-json:"+from] = true
		} else {
			for _, raw := range contractJSONRefs(value) {
				path, err := resolveContractJSONRef(from, raw)
				if err != nil {
					// Remote schema references are not a local source closure. Fail
					// closed by surfacing the rejected dependency as an invalid path.
					set["!invalid-schema-ref:"+raw] = true
					continue
				}
				if path != "" {
					schemaSources[path] = true
					add(path)
				}
			}
		}
	}
	if strings.HasSuffix(strings.ToLower(from), ".md") && (strings.HasPrefix(from, "docs/design/decisions/ADR-") || strings.HasPrefix(from, "docs/architecture/data-model/") || strings.HasPrefix(from, "docs/architecture/state/")) {
		data = contractReferenceText(data)
		for _, match := range contractMarkdownLink.FindAllStringSubmatch(string(data), -1) {
			candidate, ok := resolveContractLink(from, match[2])
			if ok && candidate != from && !strings.HasPrefix(candidate, "docs/dev/contracts/") {
				if contractLinkNamesSchema(match[1], candidate) {
					schemaSources[candidate] = true
				}
				add(candidate)
			}
		}
		for _, match := range contractKnownPath.FindAll(data, -1) {
			candidate := strings.TrimRight(string(match), ".,;:!?)]}")
			add(candidate)
		}
		for _, match := range contractADRIdentity.FindAll(data, -1) {
			add(filepath.ToSlash(filepath.Join("docs/design/decisions", string(match)+".md")))
		}
	}
	return sortedKeys(set)
}

func safeContractDependency(path string) bool {
	if !safeContractPath(path) || strings.HasPrefix(path, "docs/dev/contracts/") || strings.HasPrefix(path, ".claude/") {
		return false
	}
	return true
}

func resolveContractLink(from, href string) (string, bool) {
	u, err := url.Parse(href)
	if err != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" || strings.Contains(u.Path, `\`) {
		return "", false
	}
	if u.Path == "" {
		return "", false
	}
	var path string
	if strings.HasPrefix(u.Path, "docs/") {
		path = filepath.Clean(filepath.FromSlash(u.Path))
	} else {
		path = filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(from)), filepath.FromSlash(u.Path)))
	}
	path = filepath.ToSlash(path)
	if !safeContractPath(path) {
		return "", false
	}
	return path, true
}

func resolveContractJSONRef(from, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "" || u.Host != "" || u.RawQuery != "" || strings.Contains(u.Path, `\`) || filepath.IsAbs(u.Path) {
		return "", fmt.Errorf("schema ref must be local: %s", raw)
	}
	if u.Path == "" {
		return "", nil
	}
	path := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(from)), filepath.FromSlash(u.Path))))
	if !safeContractPath(path) || strings.HasPrefix(path, "docs/dev/contracts/") {
		return "", fmt.Errorf("schema ref escapes frozen design sources: %s", raw)
	}
	return path, nil
}

func contractJSONRefs(value any) []string {
	refs := []string{}
	var walk func(any)
	walk = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "$ref" {
					if raw, ok := child.(string); ok {
						refs = append(refs, raw)
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(value)
	return refs
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ResolveContractInputs reports malformed schema references during the normal
// dependency traversal, where the offending source path can be named clearly.
func validateContractDependencyPath(path string) error {
	if strings.HasPrefix(path, "!invalid-model-json:") {
		return fmt.Errorf("S3 shared-model dependency is not valid JSON: %s", strings.TrimPrefix(path, "!invalid-model-json:"))
	}
	if strings.HasPrefix(path, "!invalid-schema-ref:") {
		return fmt.Errorf("S3 shared-model dependency is not pinned locally: %s", strings.TrimPrefix(path, "!invalid-schema-ref:"))
	}
	if !safeContractDependency(path) {
		return fmt.Errorf("S3 conditional dependency is outside the frozen design source scope: %s", path)
	}
	return nil
}

func safeContractPath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && !strings.ContainsAny(path, `\:#`) && filepath.ToSlash(filepath.Clean(path)) == path && path != ".." && !strings.HasPrefix(path, "../")
}

func contractInitialDependency(path string) bool {
	return strings.HasPrefix(path, "docs/design/decisions/ADR-") || strings.HasPrefix(path, "docs/architecture/data-model/") || strings.HasPrefix(path, "docs/architecture/state/")
}

func contractLinkNamesSchema(label, path string) bool {
	if !strings.HasSuffix(strings.ToLower(path), ".json") {
		return false
	}
	label = strings.ToLower(label)
	return strings.Contains(label, "schema") || strings.Contains(label, "模型") || strings.HasSuffix(strings.ToLower(path), ".schema.json")
}

// ADRs declare their REQ relation in the source itself. Select accepted ADRs
// for this exact REQ even when the architecture document does not link back to
// them; a still-proposed related ADR means the S2 decision source is unresolved.
func contractADRsForRequirement(root string, files fileview.Reader, req workpacket.Reference) (map[string][]byte, error) {
	result := map[string][]byte{}
	entries, err := files.ReadDir("docs/design/decisions")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return nil, fmt.Errorf("enumerate S2 decision sources: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "ADR-") || !strings.HasSuffix(name, ".md") || strings.EqualFold(name, "ADR-template.md") {
			continue
		}
		path := filepath.ToSlash(filepath.Join("docs/design/decisions", name))
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, fmt.Errorf("S2 ADR candidate is not a regular file: %s", path)
		}
		if entry.Type().IsRegular() == false && entry.Type() != 0 {
			return nil, fmt.Errorf("S2 ADR candidate is not a regular file: %s", path)
		}
		data, err := files.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read S2 decision source through original file view %s: %w", path, err)
		}
		association := contractADRReq.FindStringSubmatch(string(data))
		if association == nil {
			continue
		}
		associatedREQ := strings.Trim(strings.TrimSpace(association[1]), "`")
		if associatedREQ != req.Path && associatedREQ != req.ID {
			continue
		}
		const statusHelp = "; see docs/design/decisions/ADR-template.md: proposed / accepted / rejected / deprecated; record accepted only when the actual direction decision accepts this ADR"
		status := contractADRStatus.FindStringSubmatch(string(data))
		if status == nil {
			return nil, fmt.Errorf("S2 ADR associated with %s has no explicit status: %s%s", req.ID, path, statusHelp)
		}
		switch strings.ToLower(strings.TrimSpace(status[1])) {
		case "accepted":
			checked, err := readConditionalSource(root, files, path)
			if err != nil {
				return nil, err
			}
			result[path] = checked
		case "rejected", "deprecated":
			// These are historical records, not active S3 inputs. An explicit
			// link from an upstream source can still freeze one as context.
		case "proposed":
			return nil, fmt.Errorf("S2 ADR associated with %s is still proposed: %s%s", req.ID, path, statusHelp)
		default:
			return nil, fmt.Errorf("S2 ADR associated with %s has unsupported status %q: %s%s", req.ID, status[1], path, statusHelp)
		}
	}
	return result, nil
}

// When the same module enumeration used by the original AC bridge finds no
// packages, only explicit N/A targets in the bound REQ can make the empty set
// complete. This checks source declarations; it does not invent applicability.
func requireExplicitNoScenarioExit(req []byte) error {
	rows := map[string]string{}
	nfrs := map[string]bool{}
	for _, line := range strings.Split(string(req), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) == 0 {
			continue
		}
		id := cells[0]
		matched := contractREQTableID.FindStringSubmatch(id)
		if matched == nil {
			continue
		}
		if matched[1] == "NFR" {
			nfrs[id] = true
		}
		if matched[1] == "AC" {
			target := ""
			if len(cells) >= 3 {
				target = cells[len(cells)-1]
			}
			rows[id] = target
		}
	}
	for id, target := range rows {
		switch {
		case strings.HasPrefix(target, "FR-"):
			return fmt.Errorf("S3 scenario source closure is incomplete: %s targets %s but no module package exists", id, target)
		case strings.HasPrefix(target, "NFR-"):
			if !nfrs[target] {
				return fmt.Errorf("S3 cannot accept empty scenario inputs: %s target %s is not a declared REQ NFR", id, target)
			}
		case strings.HasPrefix(target, "§A4"):
			// Explicit negative-space reference in the bound REQ.
		case target == "":
			return fmt.Errorf("S3 cannot accept empty scenario inputs: %s has no explicit N/A target", id)
		default:
			return fmt.Errorf("S3 cannot accept empty scenario inputs: %s target %q is not an explicit NFR or §A4 N/A", id, target)
		}
	}
	return nil
}
