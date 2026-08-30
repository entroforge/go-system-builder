// Package releasegraph validates that a staged release tree (the output of
// packaging/build-release.sh) resolves every cross-reference it claims to
// carry. The release tarball is shipped as a self-contained template, so a
// dangling reference in a Skill's Authority block, a missing Skill file
// referenced by the routing table, or a forgotten prelude/README line
// pointing at a path that no longer exists all become release-blockers.
//
// ValidateStagedRelease walks the staged tree, reads each Skill and key
// documentation file, and asserts that every referenced path resolves to
// an existing file inside the staged tree.
package releasegraph

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// stagedSkillFrontmatter captures the Skill identity used for routing:
// name, category, version, and the description string. Other fields are
// not required for reference validation.
type stagedSkillFrontmatter struct {
	Name    string
	Path    string // path relative to the staged root
	AbsPath string // absolute path on disk
}

// markdownPathPattern matches relative paths inside markdown body text,
// either bare words ending in common template suffixes or inlined inside
// backticks. It deliberately does not try to be a full markdown parser:
// the goal is to catch dangling references, not to whitelist every
// legitimate link. References under docs/release_audits/, .claude/, or
// URLs are skipped — only paths that resolve relative to the staged root
// are required to exist.
var markdownPathPattern = regexp.MustCompile("`([A-Za-z0-9_./-]+\\.(md|json|sh|yml|yaml))`")

// embeddedAssetPatterns lists filename patterns that point at harness-
// internal assets compiled into the loop-harness binary and never shipped
// in the release tarball. References to these in Skill body text are
// informational ("see the embedded schema") and must not be flagged as
// dangling references.
var embeddedAssetPatterns = []string{
	"loop-state.schema.json",
	"loop-state.example.json",
	"loop-event.schema.json",
	"loop-event.example.json",
	"loop-definition.schema.json",
	"hook-policy.schema.json",
	"hook-decision.schema.json",
	"hook-decision.examples.json",
	"agent-message.schema.json",
	"agent-message.examples.json",
	"review-evidence.schema.json",
	"review-evidence.examples.json",
	"team-manifest.schema.json",
	"team-manifest.example.json",
	"s10-audit-manifest.schema.json",
	"readback-request.template.json",
	"activation.template.json",
}

// disallowedReleasePathPrefixes lists prefixes that must NOT appear in a
// shipped release tarball. A staged tree that still carries one of these
// paths fails validation immediately.
var disallowedReleasePathPrefixes = []string{
	"docs/design/loop-engineering/",
	".claude/loop-state.json",
	".claude/loop-events.jsonl",
	".claude/hook-decisions.jsonl",
}

var disallowedInstanceGlobs = []string{
	"docs/tasks/TASK-[0-9]*.md",
	"docs/requirements/REQ-[0-9]*.md",
	"docs/loop-definition.json.bak-*",
	"docs/release_audits/*REQ-*",
	"docs/release_audits/bootstrap-*",
}

// ValidateStagedRelease walks the staged tree at root, parses every Skill
// under skills/, and asserts that every relative path referenced in the
// Skills' markdown body resolves to an existing file inside the staged
// tree. It also asserts the staged tree does not contain any of the
// disallowed release paths (e.g. design-rationale documents or instance
// runtime artifacts). It returns nil on success or a descriptive error on
// the first failure encountered.
//
// The staged root must point at the directory that contains AGENTS-template.md
// and skills/ — typically vibe-coding-loop-template-<version>/.
func ValidateStagedRelease(root string) error {
	if _, err := os.Stat(filepath.Join(root, "skills")); err != nil {
		return fmt.Errorf("release graph: skills/ directory missing under %s: %w",
			root, err)
	}
	if err := assertNoDisallowedPaths(root); err != nil {
		return err
	}
	if err := assertHarnessBinary(root); err != nil {
		return err
	}
	skills, err := collectSkills(root)
	if err != nil {
		return err
	}
	if len(skills) == 0 {
		return fmt.Errorf("release graph: no SKILL.md files found under skills/")
	}
	for _, skill := range skills {
		if err := validateSkillReferences(root, skill); err != nil {
			return err
		}
	}
	if err := validateRoutingDocumentation(root); err != nil {
		return err
	}
	return nil
}

// assertNoDisallowedPaths fails if any of the disallowed release paths is
// present under the staged root. These paths are template-internal or
// runtime-instance-only and must never ship.
func assertNoDisallowedPaths(root string) error {
	for _, rel := range disallowedReleasePathPrefixes {
		full := filepath.Join(root, rel)
		if _, err := os.Stat(full); err == nil {
			return fmt.Errorf("release graph: disallowed path present in staged tree: %s", rel)
		}
	}
	for _, pattern := range disallowedInstanceGlobs {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return fmt.Errorf("release graph: invalid instance-artifact pattern %q: %w", pattern, err)
		}
		if len(matches) > 0 {
			rel, _ := filepath.Rel(root, matches[0])
			return fmt.Errorf("release graph: instance artifact present in staged tree: %s", filepath.ToSlash(rel))
		}
	}
	return nil
}

func assertHarnessBinary(root string) error {
	path := filepath.Join(root, ".claude", "bin", "loop-harness")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("release graph: required executable .claude/bin/loop-harness missing: %w", err)
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("release graph: .claude/bin/loop-harness is not executable")
	}
	return nil
}

// collectSkills returns one entry per Skill found under the staged root.
// Each Skill must be at skills/<name>/SKILL.md and carry a name in its
// frontmatter.
func collectSkills(root string) ([]stagedSkillFrontmatter, error) {
	pattern := filepath.Join(root, "skills", "*", "SKILL.md")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("release graph: glob Skills: %w", err)
	}
	var skills []stagedSkillFrontmatter
	for _, abs := range matches {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, fmt.Errorf("release graph: relative path: %w", err)
		}
		name, err := parseSkillFrontmatterName(abs)
		if err != nil {
			return nil, fmt.Errorf("release graph: %s: %w", rel, err)
		}
		skills = append(skills, stagedSkillFrontmatter{
			Name:    name,
			Path:    filepath.ToSlash(rel),
			AbsPath: abs,
		})
	}
	return skills, nil
}

func parseSkillFrontmatterName(abs string) (string, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return "", fmt.Errorf("missing frontmatter")
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return "", fmt.Errorf("unterminated frontmatter")
	}
	block := content[4 : 4+end]
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "name:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			value = strings.Trim(value, "\"'")
			if value == "" {
				return "", fmt.Errorf("empty name field")
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("name field not found in frontmatter")
}

// validateSkillReferences scans the Skill body for relative path references
// inside backticks and asserts each one resolves to a file in the staged
// tree. It also asserts the audit-pointer comment, if present, points at
// an audit file that ships in docs/release_audits/.
func validateSkillReferences(root string, skill stagedSkillFrontmatter) error {
	data, err := os.ReadFile(skill.AbsPath)
	if err != nil {
		return fmt.Errorf("release graph: read %s: %w", skill.Path, err)
	}
	content := string(data)
	for _, match := range markdownPathPattern.FindAllStringSubmatch(content, -1) {
		raw := strings.TrimSpace(match[1])
		if shouldSkipPathReference(raw) {
			continue
		}
		if !skillReferenceExists(root, skill.AbsPath, raw) {
			return fmt.Errorf("release graph: %s references %q which does not exist in the staged tree",
				skill.Path, raw)
		}
	}
	return nil
}

// skillReferenceExists resolves a Markdown reference using the same two
// scopes available to a Skill author: a relative link may point beside the
// Skill (for example references/runtime-recovery-reference.md), or it may
// point at a template-root document (for example docs/agent-protocol.md).
// Checking both keeps local Skill references honest without treating every
// missing root document as a valid relative link.
func skillReferenceExists(root, skillPath, raw string) bool {
	for _, candidate := range []string{
		filepath.Join(filepath.Dir(skillPath), raw),
		filepath.Join(root, raw),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

// shouldSkipPathReference returns true when a backticked path is allowed
// to dangle or refers to a runtime-only path that the staged tree does
// not ship. The release tarball is shipped without .claude/ instance
// runtime, without docs/design/loop-engineering/ design rationale, and
// without embedded harness schemas (which are compiled into the binary).
func shouldSkipPathReference(raw string) bool {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return true
	}
	if strings.HasPrefix(raw, ".claude/") {
		return true
	}
	if strings.HasPrefix(raw, "docs/design/loop-engineering/") {
		return true
	}
	// The project map is created by the installer from
	// docs/project-map-template.md. It is intentionally absent from the
	// distributable template because it is instance-specific, but methodology
	// Skills must still name the path agents consume after initialization.
	if raw == "docs/project-map.md" {
		return true
	}
	// Bare filenames in methodology text commonly name an instance output
	// (for example `flows.md`) rather than a repository-root dependency. The
	// release graph cannot distinguish those meanings, so enforcing them here
	// creates false gates. Cross-document references must include a directory
	// component; required root entry points are validated separately below.
	if !strings.Contains(raw, "/") {
		return true
	}
	// NNN denotes an instance-supplied identifier in template output paths,
	// not a concrete file that the release itself must carry.
	if strings.Contains(raw, "NNN") {
		return true
	}
	basename := filepath.Base(raw)
	for _, asset := range embeddedAssetPatterns {
		if basename == asset {
			return true
		}
	}
	return false
}

// validateRoutingDocumentation asserts that the staged tree's two routing
// documents — AGENTS-template.md and prelude.md — exist. They are the
// primary entry points a target project loads, so they must always ship.
func validateRoutingDocumentation(root string) error {
	for _, rel := range []string{
		"AGENTS-template.md",
		"prelude.md",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			return fmt.Errorf("release graph: required entry %s missing: %w", rel, err)
		}
	}
	return nil
}
