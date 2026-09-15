package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var recoveryInputFiles = []string{
	".claude/loop-state.json",
	".claude/loop-events.jsonl",
	".claude/loop-state.json.commit-pending.json",
	".claude/loop-state.json.fingerprint-pending.json",
	".claude/loop-state.json.rollover-pending.json",
	".claude/loop-state.json.recovery-pending.json",
	"docs/loop-definition.json",
	"docs/hook-policy.json",
}

var recoveryInputDirectories = []string{
	".claude/evidence",
	".claude/workgroups",
	"docs/requirements",
	"docs/design",
	"docs/contracts",
	"docs/tasks",
	"docs/reports",
	"docs/release_audits",
}

// Inspect validates the explicit REQ and inventories durable recovery inputs
// without parsing the active Runtime. A malformed or BOM-prefixed state file
// is therefore still inspectable and content-addressed.
func Inspect(root, reqPath string) (Inventory, error) {
	resolvedRoot, err := resolveRepositoryRoot(root)
	if err != nil {
		return Inventory{}, err
	}

	req, reqFullPath, err := validateREQ(resolvedRoot, reqPath)
	if err != nil {
		return Inventory{}, err
	}

	inputs := make([]InventoryInput, 0)
	seen := make(map[string]struct{})
	add := func(fullPath, relativePath string) error {
		if _, exists := seen[relativePath]; exists {
			return nil
		}
		input, err := inventoryFile(resolvedRoot, fullPath, relativePath)
		if err != nil {
			return err
		}
		seen[relativePath] = struct{}{}
		inputs = append(inputs, input)
		return nil
	}

	reqRelative, err := repositoryRelativePath(resolvedRoot, reqFullPath)
	if err != nil {
		return Inventory{}, err
	}
	if err := add(reqFullPath, reqRelative); err != nil {
		return Inventory{}, fmt.Errorf("inventory selected req: %w", err)
	}

	for _, relativePath := range recoveryInputFiles {
		fullPath := filepath.Join(resolvedRoot, filepath.FromSlash(relativePath))
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return Inventory{}, fmt.Errorf("inspect recovery input %q: %w", relativePath, err)
		}
		if err := add(fullPath, relativePath); err != nil {
			return Inventory{}, fmt.Errorf("inventory recovery input %q: %w", relativePath, err)
		}
	}

	for _, relativeDirectory := range recoveryInputDirectories {
		fullDirectory := filepath.Join(resolvedRoot, filepath.FromSlash(relativeDirectory))
		if _, err := os.Stat(fullDirectory); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return Inventory{}, fmt.Errorf("inspect recovery directory %q: %w", relativeDirectory, err)
		}
		if err := filepath.WalkDir(fullDirectory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("walk recovery directory %q: %w", relativeDirectory, walkErr)
			}
			if entry.IsDir() {
				return nil
			}
			relativePath, err := repositoryRelativePath(resolvedRoot, path)
			if err != nil {
				return err
			}
			return add(path, relativePath)
		}); err != nil {
			return Inventory{}, fmt.Errorf("inventory recovery directory %q: %w", relativeDirectory, err)
		}
	}

	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].Path < inputs[j].Path
	})
	return Inventory{
		SchemaVersion: SchemaVersion,
		REQ:           req,
		Inputs:        inputs,
		Root:          resolvedRoot,
	}, nil
}

func resolveRepositoryRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", &ValidationError{Code: ErrInvalidRoot, Field: "root", Reason: "root is empty"}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve recovery root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", &ValidationError{Code: ErrInvalidRoot, Field: "root", Path: root, Reason: "root cannot be resolved", Cause: err}
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return "", &ValidationError{Code: ErrInvalidRoot, Field: "root", Path: root, Reason: "root cannot be read", Cause: err}
	}
	if !info.IsDir() {
		return "", &ValidationError{Code: ErrInvalidRoot, Field: "root", Path: root, Reason: "root is not a directory"}
	}
	return filepath.Clean(resolvedRoot), nil
}

func validateREQ(root, requestedPath string) (REQBinding, string, error) {
	fullPath, relativePath, err := resolveExistingFile(root, requestedPath, "path")
	if err != nil {
		return REQBinding{}, "", err
	}
	baseName := filepath.Base(filepath.FromSlash(relativePath))
	if !strings.HasPrefix(baseName, "REQ-") {
		return REQBinding{}, "", &ValidationError{
			Code: ErrREQFilename, Field: "filename", Path: relativePath,
			Reason: "filename must start with REQ-",
		}
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return REQBinding{}, "", &ValidationError{
			Code: ErrInvalidREQ, Field: "content", Path: relativePath,
			Reason: "req cannot be read", Cause: err,
		}
	}
	status := reqMetadataValue(string(data), "status", "状态")
	if status == "" {
		return REQBinding{}, "", &ValidationError{
			Code: ErrREQStatusMissing, Field: "status", Path: relativePath,
			Reason: "status is required",
		}
	}
	if !strings.EqualFold(status, "locked") {
		return REQBinding{}, "", &ValidationError{
			Code: ErrREQNotLocked, Field: "status", Path: relativePath,
			Reason: "status must be locked",
		}
	}
	version := reqMetadataValue(string(data), "version", "版本")
	if version == "" {
		return REQBinding{}, "", &ValidationError{
			Code: ErrREQVersionMissing, Field: "version", Path: relativePath,
			Reason: "version is required",
		}
	}
	reqID := reqIDFromFilename(baseName)
	if reqID == "" {
		return REQBinding{}, "", &ValidationError{
			Code: ErrREQFilename, Field: "filename", Path: relativePath,
			Reason: "filename must start with REQ- followed by at least three digits",
		}
	}
	return REQBinding{
		ID:      reqID,
		Path:    relativePath,
		Status:  strings.ToLower(status),
		Version: version,
		SHA256:  sha256Hex(data),
	}, fullPath, nil
}

func reqIDFromFilename(baseName string) string {
	stem := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	if !strings.HasPrefix(stem, "REQ-") {
		return ""
	}
	digits := stem[len("REQ-"):]
	end := 0
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	if end < 3 {
		return ""
	}
	return "REQ-" + digits[:end]
}

func reqMetadataValue(content string, keys ...string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		separator := strings.Index(line, ":")
		separatorWidth := 1
		fullWidthSeparator := strings.Index(line, "：")
		if separator < 0 || (fullWidthSeparator >= 0 && fullWidthSeparator < separator) {
			separator = fullWidthSeparator
			separatorWidth = len("：")
		}
		if separator < 0 {
			continue
		}
		label := strings.Trim(strings.TrimSpace(line[:separator]), "*_`")
		matched := false
		for _, key := range keys {
			if strings.EqualFold(label, key) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		value := strings.Trim(strings.TrimSpace(line[separator+separatorWidth:]), "*_`|")
		if fields := strings.Fields(value); len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

func resolveExistingFile(root, requestedPath, field string) (string, string, error) {
	if strings.TrimSpace(requestedPath) == "" {
		return "", "", &ValidationError{Code: ErrInvalidREQ, Field: field, Reason: "path is empty"}
	}
	absPath := requestedPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(root, filepath.FromSlash(requestedPath))
	}
	absPath, err := filepath.Abs(absPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve recovery %s path: %w", field, err)
	}
	relativePath, err := repositoryRelativePath(root, absPath)
	if err != nil {
		return "", "", &ValidationError{Code: ErrPathOutsideRepository, Field: field, Path: requestedPath, Reason: "path must remain inside repository", Cause: err}
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", "", &ValidationError{Code: ErrInvalidREQ, Field: field, Path: relativePath, Reason: "path cannot be read", Cause: err}
	}
	if info.IsDir() {
		return "", "", &ValidationError{Code: ErrInvalidREQ, Field: field, Path: relativePath, Reason: "path must be a file"}
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", "", &ValidationError{Code: ErrInvalidREQ, Field: field, Path: relativePath, Reason: "path cannot be resolved", Cause: err}
	}
	if _, err := repositoryRelativePath(root, resolvedPath); err != nil {
		return "", "", &ValidationError{Code: ErrPathOutsideRepository, Field: field, Path: relativePath, Reason: "resolved path must remain inside repository", Cause: err}
	}
	return absPath, relativePath, nil
}

func repositoryRelativePath(root, candidate string) (string, error) {
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	relativePath, err := filepath.Rel(root, absCandidate)
	if err != nil {
		return "", fmt.Errorf("compute repository-relative path: %w", err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
		return "", ErrPathOutsideRepository
	}
	return filepath.ToSlash(relativePath), nil
}

func inventoryFile(root, fullPath, relativePath string) (InventoryInput, error) {
	resolvedPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return InventoryInput{}, fmt.Errorf("resolve inventory input %q: %w", relativePath, err)
	}
	if _, err := repositoryRelativePath(root, resolvedPath); err != nil {
		return InventoryInput{}, &ValidationError{Code: ErrPathOutsideRepository, Field: "input", Path: relativePath, Reason: "resolved input must remain inside repository", Cause: err}
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return InventoryInput{}, fmt.Errorf("read inventory input %q: %w", relativePath, err)
	}
	return InventoryInput{
		Path:   relativePath,
		Kind:   inputKind(relativePath),
		SHA256: sha256Hex(data),
		Size:   int64(len(data)),
	}, nil
}

func inputKind(relativePath string) string {
	switch relativePath {
	case ".claude/loop-state.json":
		return InputKindRuntimeState
	case ".claude/loop-events.jsonl":
		return InputKindRuntimeJournal
	case ".claude/loop-state.json.commit-pending.json", ".claude/loop-state.json.fingerprint-pending.json", ".claude/loop-state.json.rollover-pending.json", ".claude/loop-state.json.recovery-pending.json":
		return InputKindRuntimePending
	default:
		if strings.HasPrefix(relativePath, "docs/requirements/") && strings.HasPrefix(filepath.Base(filepath.FromSlash(relativePath)), "REQ-") {
			return InputKindREQ
		}
		return InputKindArtifact
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
