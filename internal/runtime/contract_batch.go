package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/scenario"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const contractsDirectory = "docs/dev/contracts"

var (
	contractIDPattern = regexp.MustCompile(`^(FE|BE|SYNC)-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	contractIndexIDRE = regexp.MustCompile(`^CONTRACTS-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	contractTitleIDRE = regexp.MustCompile(`^(?:FE|BE|SYNC|CONTRACTS)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`)
	contractTokenRE   = regexp.MustCompile(`\b(FE|BE|SYNC)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`)
)

type contractIndexRow struct {
	id      string
	status  string
	version string
}

// ContractArtifacts resolves the current bound REQ's locked S3 batch. The
// index and its complete FE/BE/SYNC set lead the result; the original shared
// model checker then appends the exact schema/example JSON refs not already
// frozen as upstream packet inputs. Every ref is checked against the original
// source view and current checkout before it is returned.
func ContractArtifacts(root string, state map[string]any, p workpacket.ContractPacket, answer workpacket.ContractAnswer, files fileview.Reader) ([]workpacket.Reference, error) {
	if files == nil {
		return nil, fmt.Errorf("S3 requires original authoritative file view")
	}
	if err := ValidateContractPacket(root, state, p, files); err != nil {
		return nil, err
	}
	answer, err := validateContractAnswerProjection(p, answer)
	if err != nil {
		return nil, err
	}
	if err := validateBridgeBinding(root, state, p); err != nil {
		return nil, err
	}

	viewEntries, err := files.ReadDir(filepath.Join(root, filepath.FromSlash(contractsDirectory)))
	if err != nil {
		return nil, fmt.Errorf("read S3 contract directory through source view: %w", err)
	}
	if err := workpacket.CheckPath(root, filepath.ToSlash(filepath.Join(contractsDirectory, ".source-probe"))); err != nil {
		return nil, fmt.Errorf("S3 contract checkout: %w", err)
	}
	diskEntries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(contractsDirectory)))
	if err != nil {
		return nil, fmt.Errorf("read S3 contract checkout: %w", err)
	}
	viewEntries, err = contractCandidateEntries(files, viewEntries)
	if err != nil {
		return nil, err
	}
	diskEntries, err = contractCandidateEntries(fileview.Disk{Root: root}, diskEntries)
	if err != nil {
		return nil, err
	}
	if !sameContractDirectory(viewEntries, diskEntries) {
		return nil, fmt.Errorf("S3 contract source view and checkout directory differ: %s", contractDirectoryDifference(viewEntries, diskEntries))
	}

	indexPath := *answer.IndexPath
	contractPaths := append([]string(nil), answer.ContractPaths...)
	paths := append([]string{indexPath}, contractPaths...)
	if err := validateCurrentREQDeliveryCoverage(files, viewEntries, indexPath, contractPaths, p.Binding.Requirement.ID); err != nil {
		return nil, err
	}

	refs := make([]workpacket.Reference, 0, len(paths))
	byPath := map[string]workpacket.Reference{}
	byID := map[string]string{}
	for _, path := range paths {
		ref, err := readLockedContractArtifact(root, files, path)
		if err != nil {
			return nil, err
		}
		if prior, ok := byPath[ref.Path]; ok {
			return nil, fmt.Errorf("duplicate S3 delivery path %s (%s and %s)", ref.Path, prior.ID, ref.ID)
		}
		if prior, ok := byID[ref.ID]; ok {
			return nil, fmt.Errorf("duplicate S3 delivery name %s at %s and %s", ref.ID, prior, ref.Path)
		}
		byPath[ref.Path] = ref
		byID[ref.ID] = ref.Path
		refs = append(refs, ref)
	}
	if len(refs) != 1+len(contractPaths) {
		return nil, fmt.Errorf("S3 artifact identity set is incomplete")
	}

	indexData, err := files.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	if err := validateContractIndex(indexPath, string(indexData), p, answer, refs[:1+len(contractPaths)]); err != nil {
		return nil, err
	}

	modelRefs, err := contractSharedModelArtifacts(root, p, files)
	if err != nil {
		return nil, err
	}
	refs = append(refs, modelRefs...)

	if err := scenario.GuardBridgeCheckedWithFiles(root, files); err != nil {
		return nil, fmt.Errorf("scenario bridge: %w", err)
	}
	if verified, ok := files.(interface{ Verify() error }); ok {
		if err := verified.Verify(); err != nil {
			return nil, fmt.Errorf("S3 source view moved: %w", err)
		}
	}
	return refs, nil
}

func validateContractAnswerProjection(p workpacket.ContractPacket, answer workpacket.ContractAnswer) (workpacket.ContractAnswer, error) {
	draft := p.Draft
	draft.Answer = answer
	data, err := json.Marshal(draft)
	if err != nil {
		return workpacket.ContractAnswer{}, err
	}
	validated, issues := workpacket.ValidateContractAnswer(p, data)
	if len(issues) != 0 {
		return workpacket.ContractAnswer{}, fmt.Errorf("S3 answer %s: %s", issues[0].Pointer, issues[0].Expected)
	}
	return validated, nil
}

func validateBridgeBinding(root string, state map[string]any, p workpacket.ContractPacket) error {
	bound, _ := state["bound_req"].(map[string]any)
	if stringValue(bound["id"]) != p.Binding.Requirement.ID || stringValue(bound["path"]) != p.Binding.Requirement.Path {
		return fmt.Errorf("S3 bridge requirement differs from the frozen REQ")
	}
	// GuardBridgeCheckedWithFiles uses the persisted runtime to select its REQ.
	// Confirm that selector still names the packet's exact frozen requirement.
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		return fmt.Errorf("read planning state for scenario bridge: %w", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fmt.Errorf("decode planning state for scenario bridge: %w", err)
	}
	persistedBound, _ := persisted["bound_req"].(map[string]any)
	lifecycle, _ := persisted["lifecycle"].(map[string]any)
	if stringValue(persistedBound["id"]) != p.Binding.Requirement.ID || stringValue(persistedBound["path"]) != p.Binding.Requirement.Path || stringValue(lifecycle["state"]) == "" || stringValue(lifecycle["state"]) == "inactive" {
		return fmt.Errorf("persisted scenario bridge selector differs from the frozen REQ")
	}
	return nil
}

// contractCandidateEntries limits anti-omission scans to actual contract
// candidates. Notes, editor files and arbitrary subdirectories are not batch
// members. Family-prefixed names remain candidates even with malformed IDs;
// titled identities also catch descriptive filenames rather than guessing that
// those documents belong to the delivery.
func contractCandidateEntries(files fileview.Reader, entries []os.DirEntry) ([]os.DirEntry, error) {
	candidates := []os.DirEntry{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || strings.Contains(strings.ToLower(name), "template") || strings.EqualFold(name, "README.md") {
			continue
		}
		candidate := false
		for _, prefix := range []string{"FE-", "BE-", "SYNC-", "CONTRACTS-"} {
			candidate = candidate || strings.HasPrefix(name, prefix)
		}
		if !candidate {
			data, err := files.ReadFile(contractsDirectory + "/" + name)
			if err != nil {
				return nil, fmt.Errorf("inspect S3 contract candidate %s: %w", name, err)
			}
			for _, line := range strings.Split(string(contractReferenceText(data)), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "# ") {
					title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
					candidate = contractTitleIDRE.MatchString(title)
					break
				}
			}
		}
		if candidate {
			candidates = append(candidates, entry)
		}
	}
	return candidates, nil
}

// contractDirectoryDifference names only meaningful candidates after filtering.
func contractDirectoryDifference(view, disk []os.DirEntry) string {
	source, checkout := map[string]bool{}, map[string]bool{}
	for _, entry := range view {
		source[entry.Name()] = entry.IsDir()
	}
	for _, entry := range disk {
		checkout[entry.Name()] = entry.IsDir()
	}
	differences := []string{}
	for name, isDir := range source {
		other, exists := checkout[name]
		if !exists {
			differences = append(differences, "missing from checkout: "+contractsDirectory+"/"+name)
		} else if other != isDir {
			differences = append(differences, "file type differs: "+contractsDirectory+"/"+name)
		}
	}
	for name := range checkout {
		if _, exists := source[name]; !exists {
			differences = append(differences, "missing from declared source: "+contractsDirectory+"/"+name)
		}
	}
	sort.Strings(differences)
	return strings.Join(differences, "; ")
}

func sameContractDirectory(view, disk []os.DirEntry) bool {
	if len(view) != len(disk) {
		return false
	}
	byName := make(map[string]bool, len(view))
	for _, entry := range view {
		byName[entry.Name()] = entry.IsDir()
	}
	for _, entry := range disk {
		isDir, ok := byName[entry.Name()]
		if !ok || isDir != entry.IsDir() {
			return false
		}
		delete(byName, entry.Name())
	}
	return len(byName) == 0
}

func validateCurrentREQDeliveryCoverage(files fileview.Reader, entries []os.DirEntry, indexPath string, contractPaths []string, requirementID string) error {
	var err error
	entries, err = contractCandidateEntries(files, entries)
	if err != nil {
		return err
	}
	pathSet := map[string]bool{indexPath: true}
	idSet := map[string]bool{}
	for _, path := range contractPaths {
		if pathSet[path] {
			return fmt.Errorf("S3 delivery repeats path %s", path)
		}
		pathSet[path] = true
		id := strings.TrimSuffix(filepath.Base(path), ".md")
		if idSet[id] {
			return fmt.Errorf("S3 delivery repeats contract name %s", id)
		}
		idSet[id] = true
	}
	foreignPaths, err := foreignContractPaths(files, entries, indexPath, requirementID)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || strings.Contains(name, "template") || name == "README.md" {
			continue
		}
		path := contractsDirectory + "/" + name
		if pathSet[path] {
			delete(pathSet, path)
			continue
		}
		if foreignPaths[path] {
			continue
		}
		return fmt.Errorf("S3 contract directory contains an unowned document outside the current and foreign REQ batches: %s", path)
	}
	if len(pathSet) != 0 {
		missing := make([]string, 0, len(pathSet))
		for path := range pathSet {
			missing = append(missing, path)
		}
		sort.Strings(missing)
		return fmt.Errorf("S3 delivery omits current-index documents: %s", strings.Join(missing, ", "))
	}
	for id := range idSet {
		if !contractIDPattern.MatchString(id) {
			return fmt.Errorf("S3 delivery has an unsupported contract name %s", id)
		}
	}
	return nil
}

func foreignContractPaths(files fileview.Reader, entries []os.DirEntry, currentIndex, requirementID string) (map[string]bool, error) {
	foreign := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !contractIndexIDRE.MatchString(strings.TrimSuffix(name, ".md")) || !strings.HasSuffix(name, ".md") || strings.Contains(strings.ToLower(name), "template") {
			continue
		}
		path := contractsDirectory + "/" + name
		if path == currentIndex {
			continue
		}
		content, err := files.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read S3 foreign contract index %s: %w", path, err)
		}
		owner, err := sharedmodel.IndexRequirement(string(content))
		if err != nil || owner == "" {
			return nil, fmt.Errorf("S3 extra contract index lacks an explicit REQ owner: %s", path)
		}
		if owner == requirementID {
			return nil, fmt.Errorf("S3 bound REQ %s has multiple contract indexes; answer the current index explicitly: %s", requirementID, path)
		}
		foreign[path] = true
		ids, err := declaredContractIDs(string(content))
		if err != nil {
			return nil, fmt.Errorf("read S3 foreign contract declarations %s: %w", path, err)
		}
		for id := range ids {
			foreign[contractsDirectory+"/"+id+".md"] = true
		}
	}
	return foreign, nil
}

func declaredContractIDs(content string) (map[string]bool, error) {
	rows, err := contractInventoryRows(content)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for id := range rows {
		ids[id] = true
	}
	for _, heading := range []string{"联调点矩阵", "需求覆盖矩阵"} {
		for _, id := range contractIDsInSection(content, heading) {
			ids[id] = true
		}
	}
	return ids, nil
}

// ContractIndexContractIDs projects the existing S3 inventory/integration/
// coverage parser for read-only consumers. It grants no registration authority.
func ContractIndexContractIDs(content string) ([]string, error) {
	ids, err := declaredContractIDs(content)
	return keys(ids), err
}

func contractSharedModelArtifacts(root string, p workpacket.ContractPacket, files fileview.Reader) ([]workpacket.Reference, error) {
	checked := sharedmodel.Check(root, files, p.Binding.Requirement.ID)
	if len(checked.Problems) > 0 {
		return nil, fmt.Errorf("S3 shared-model source closure cannot be proven: %s", strings.Join(checked.Problems, "; "))
	}
	if len(checked.Warnings) > 0 {
		return nil, fmt.Errorf("S3 shared-model source closure has no explicit applicability decision: %s", strings.Join(checked.Warnings, "; "))
	}
	frozen := map[string]string{p.Binding.Requirement.Path: p.Binding.Requirement.SHA256}
	for _, ref := range p.Inputs.Designs {
		frozen[ref.Path] = ref.SHA256
	}
	for _, ref := range p.Inputs.Sources {
		frozen[ref.Path] = ref.SHA256
	}
	paths := make([]string, 0, len(checked.Files))
	for path := range checked.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	refs := make([]workpacket.Reference, 0, len(paths))
	for _, originalPath := range paths {
		path := filepath.ToSlash(filepath.Clean(filepath.FromSlash(originalPath)))
		if !safeContractPath(path) || path != originalPath || strings.HasPrefix(path, ".claude/") || !strings.HasSuffix(strings.ToLower(path), ".json") {
			return nil, fmt.Errorf("S3 shared-model source is not a safe repository JSON path: %s", originalPath)
		}
		data, err := readConditionalSource(root, files, path)
		if err != nil {
			return nil, s3ArtifactSourceError(root, files, path, fmt.Errorf("S3 shared-model source closure: %w", err))
		}
		if !bytes.Equal(data, checked.Files[originalPath]) {
			return nil, fmt.Errorf("S3 shared-model checker source changed while projecting evidence: %s", path)
		}
		if expected, ok := frozen[path]; ok {
			if workpacket.Digest(data) != expected {
				return nil, fmt.Errorf("S3 shared-model source differs from frozen upstream input: %s", path)
			}
			continue
		}
		refs = append(refs, workpacket.Reference{Path: path, SHA256: workpacket.Digest(data)})
	}
	return refs, nil
}

func readLockedContractArtifact(root string, files fileview.Reader, path string) (workpacket.Reference, error) {
	if !workpacketPath(path) {
		return workpacket.Reference{}, fmt.Errorf("invalid S3 contract path %s", path)
	}
	name := filepath.Base(path)
	id := strings.TrimSuffix(name, ".md")
	if !contractIndexIDRE.MatchString(id) && !contractIDPattern.MatchString(id) {
		return workpacket.Reference{}, fmt.Errorf("unsupported S3 contract name %s", id)
	}
	view, err := files.ReadFile(path)
	if err != nil {
		return workpacket.Reference{}, s3ArtifactSourceError(root, files, path, fmt.Errorf("read S3 artifact %s through source view: %w", path, err))
	}
	if len(view) > 2<<20 {
		return workpacket.Reference{}, fmt.Errorf("S3 artifact %s exceeds 2 MiB", path)
	}
	disk, err := workpacket.ReadFile(root, path, 2<<20)
	if err != nil {
		return workpacket.Reference{}, s3ArtifactSourceError(root, files, path, fmt.Errorf("read S3 artifact %s from checkout: %w", path, err))
	}
	digest := workpacket.Digest(view)
	if digest != workpacket.Digest(disk) {
		return workpacket.Reference{}, s3ArtifactSourceError(root, files, path, fmt.Errorf("S3 artifact checkout differs from source view: %s", path))
	}
	status := contractHeaderField(string(view), "状态", "Status")
	version := contractHeaderField(string(view), "版本", "Version")
	if status != "locked" || strings.TrimSpace(version) == "" || strings.ContainsAny(version, " \t\r\n") {
		return workpacket.Reference{}, fmt.Errorf("S3 artifact %s must declare status=locked and a concrete version", path)
	}
	return workpacket.Reference{ID: id, Path: path, Version: version, SHA256: digest}, nil
}

func s3ArtifactSourceError(root string, files fileview.Reader, path string, cause error) error {
	view, ok := files.(*fileview.View)
	if !ok || cause == nil {
		return cause
	}
	source, err := view.Source(path)
	if err != nil || source != "git_tree" {
		return cause
	}
	checkout, checkoutErr := workpacket.ReadFile(root, path, 4<<20)
	if checkoutErr != nil {
		return cause
	}
	fromRef, refErr := view.ReadFile(path)
	missingFromRef := refErr != nil && os.IsNotExist(refErr)
	contentDiffers := refErr == nil && !bytes.Equal(fromRef, checkout)
	if !missingFromRef && !contentDiffers {
		return cause
	}
	return fmt.Errorf("%w; S3 professional artifact is supplied by bound Git ref %s. It can be edited in the current S3 entry; commit this file to that ref, then save the original S3 answer", cause, view.Ref)
}

func workpacketPath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && filepath.ToSlash(filepath.Clean(path)) == path && filepath.ToSlash(filepath.Dir(path)) == contractsDirectory && strings.HasSuffix(path, ".md") && !strings.Contains(strings.ToLower(filepath.Base(path)), "template")
}

func contractHeaderField(content string, keys ...string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		for _, key := range keys {
			for _, sep := range []string{"：", ":"} {
				prefix := key + sep
				if strings.HasPrefix(trimmed, prefix) {
					value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
					if i := strings.IndexAny(value, "（( "); i > 0 {
						value = strings.TrimSpace(value[:i])
					}
					return value
				}
			}
		}
	}
	return ""
}

func validateContractIndex(path, content string, p workpacket.ContractPacket, answer workpacket.ContractAnswer, artifacts []workpacket.Reference) error {
	if contractHeaderField(content, "状态", "Status") != "locked" || contractHeaderField(content, "版本", "Version") == "" {
		return fmt.Errorf("S3 CONTRACTS index must declare status=locked and a version")
	}
	owner, err := sharedmodel.IndexRequirement(content)
	if err != nil {
		return err
	}
	if owner == "" {
		return fmt.Errorf("S3 CONTRACTS index missing requirement declaration for %s", p.Binding.Requirement.ID)
	}
	if owner != p.Binding.Requirement.ID {
		return fmt.Errorf("S3 CONTRACTS index requirement declaration %q does not unambiguously identify %s", owner, p.Binding.Requirement.ID)
	}
	indexRows, err := contractInventoryRows(content)
	if err != nil {
		return err
	}
	syncIDs := contractIDsInSection(content, "联调点矩阵")
	syncSet := map[string]bool{}
	for _, id := range syncIDs {
		syncSet[id] = true
	}
	declared := map[string]bool{}
	notApplicable := func(id string) bool {
		family := strings.SplitN(id, "-", 2)[0]
		return answer.Applicability[family].Disposition == "not_applicable"
	}
	for id, row := range indexRows {
		if notApplicable(id) {
			continue
		}
		declared[id] = true
		if row.status != "locked" || row.version == "" {
			return fmt.Errorf("S3 index row %s must declare locked status and version", id)
		}
	}
	for _, id := range syncIDs {
		if !notApplicable(id) {
			declared[id] = true
		}
	}
	for _, id := range contractIDsInSection(content, "需求覆盖矩阵") {
		if !notApplicable(id) {
			declared[id] = true
		}
	}
	actual := map[string]workpacket.Reference{}
	for _, ref := range artifacts[1:] {
		actual[ref.ID] = ref
	}
	actualSet := map[string]bool{}
	for id := range actual {
		actualSet[id] = true
	}
	if !sameStringSet(declared, actualSet) {
		return fmt.Errorf("S3 index contract set differs from delivered FE/BE/SYNC artifacts")
	}
	for id := range actual {
		if (strings.HasPrefix(id, "FE-") || strings.HasPrefix(id, "BE-")) && indexRows[id].id == "" {
			return fmt.Errorf("S3 index contract list omits %s", id)
		}
		if strings.HasPrefix(id, "SYNC-") && !syncSet[id] {
			return fmt.Errorf("S3 integration matrix omits %s", id)
		}
	}
	for id, row := range indexRows {
		if notApplicable(id) {
			continue
		}
		ref, ok := actual[id]
		if !ok || ref.Version != row.version {
			return fmt.Errorf("S3 index row %s version differs from the delivered contract", id)
		}
	}
	if filepath.Base(path) != artifacts[0].ID+".md" {
		return fmt.Errorf("S3 index artifact identity differs from answer path")
	}
	return nil
}

// contractIndexDisplayID reads an existing document ID through Markdown presentation.
// Repeated label/target occurrences of the same ID agree; different IDs are ambiguous.
func contractIndexDisplayID(value string, tokens *regexp.Regexp) (string, error) {
	found := ""
	for _, id := range tokens.FindAllString(value, -1) {
		if found != "" && found != id {
			return "", fmt.Errorf("conflicting document IDs %s and %s", found, id)
		}
		found = id
	}
	if found == "" {
		return "", fmt.Errorf("missing document ID")
	}
	return found, nil
}

func contractInventoryRows(content string) (map[string]contractIndexRow, error) {
	section := markdownSection(content, "合同清单")
	start := strings.Index(content, section)
	if start < 0 {
		start = 0
	}
	offset := strings.Count(content[:start], "\n")
	rows := map[string]contractIndexRow{}
	lines := strings.Split(section, "\n")
	inTable := false
	for lineIndex, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inTable {
			if !strings.HasPrefix(trimmed, "|") {
				continue
			}
			cells := splitMarkdownRow(trimmed)
			if len(cells) == 0 || !(cells[0] == "合同ID" || strings.EqualFold(cells[0], "Contract ID") || strings.EqualFold(cells[0], "ID")) {
				continue
			}
			inTable = true
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			break
		}
		cells := splitMarkdownRow(trimmed)
		if len(cells) > 0 && (strings.HasPrefix(cells[0], ":-") || strings.HasPrefix(cells[0], "--")) {
			continue
		}
		if len(cells) < 6 {
			return nil, fmt.Errorf("S3 index contract inventory line %d: expected 6 columns (ID/type/owner/status/version/integration), got %d", offset+lineIndex+1, len(cells))
		}
		id, err := contractIndexDisplayID(cells[0], contractTokenRE)
		if err != nil {
			return nil, fmt.Errorf("S3 index contract inventory line %d column 合同ID: %w", offset+lineIndex+1, err)
		}
		if _, exists := rows[id]; exists {
			return nil, fmt.Errorf("S3 index contract inventory line %d column 合同ID repeats %s", offset+lineIndex+1, id)
		}
		rows[id] = contractIndexRow{id: id, status: cells[3], version: cells[4]}
	}
	return rows, nil
}

func contractIDsInSection(content, heading string) []string {
	section := markdownSection(content, heading)
	seen := map[string]bool{}
	for _, id := range contractTokenRE.FindAllString(section, -1) {
		seen[id] = true
	}
	return keys(seen)
}

func markdownSection(content, heading string) string {
	active := false
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if active {
				break
			}
			active = name == heading || strings.TrimLeft(name, "0123456789. ") == heading
			continue
		}
		if active {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func splitMarkdownRow(line string) []string {
	cells := strings.Split(strings.Trim(line, "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(strings.Trim(cells[i], "`"))
	}
	return cells
}

func sameStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
