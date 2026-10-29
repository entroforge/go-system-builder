package agentcontext

import (
	"fmt"
	"strings"
)

// RenderCoreRole prepares the first Builder role from its actual original
// bytes. Only the skills frontmatter block changes; every other field and
// the whole responsibility/permission body remain byte-for-byte intact.
// Calling this generator does not enable an installed or running role.
func RenderCoreRole(role string, source []byte) ([]byte, error) {
	if role != "backend-builder" {
		return nil, fmt.Errorf("only the first backend Builder core profile is supported")
	}
	p, ok := profiles.Roles[role]
	if !ok || len(p.Core) == 0 {
		return nil, fmt.Errorf("role has no core profile")
	}
	newline := "\n"
	if strings.Contains(string(source), "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(string(source), "\n")
	text := func(i int) string { return strings.TrimSuffix(lines[i], "\r") }
	if len(lines) < 3 || text(0) != "---" {
		return nil, fmt.Errorf("role requires original YAML frontmatter")
	}
	close := -1
	for i := 1; i < len(lines); i++ {
		if text(i) == "---" {
			close = i
			break
		}
	}
	if close < 0 {
		return nil, fmt.Errorf("role frontmatter is unterminated")
	}
	start, end := -1, -1
	names := 0
	for i := 1; i < close; i++ {
		line := text(i)
		if strings.HasPrefix(line, "name:") {
			names++
			name := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"'")
			if name != role {
				return nil, fmt.Errorf("original role name differs")
			}
		}
		if strings.HasPrefix(line, "skills:") {
			if start >= 0 || line != "skills:" {
				return nil, fmt.Errorf("role skills must be one explicit list")
			}
			start = i
		}
	}
	if names != 1 || start < 0 {
		return nil, fmt.Errorf("original role name/skills are missing or duplicated")
	}
	end = start + 1
	for end < close {
		line := text(end)
		if line != "" && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "- ") {
			return nil, fmt.Errorf("unsupported nested skills frontmatter")
		}
		end++
	}
	prefix := strings.Join(lines[:start+1], "\n") + "\n"
	var core strings.Builder
	for _, name := range p.Core {
		core.WriteString("  - " + name + newline)
	}
	result := prefix + core.String() + strings.Join(lines[end:], "\n")
	return []byte(result), nil
}

func CoreSkills(role string) []string {
	p, ok := profiles.Roles[role]
	if !ok {
		return nil
	}
	return append([]string{}, p.Core...)
}

// VerifyCoreRole checks a previously frozen result without selecting today's
// profile. Remove only its list block and compare all remaining bytes.
func VerifyCoreRole(original, generated []byte, core []string) error {
	strip := func(data []byte) (string, []string, error) {
		lines := strings.Split(string(data), "\n")
		close := -1
		start := -1
		if len(lines) < 3 || strings.TrimSuffix(lines[0], "\r") != "---" {
			return "", nil, fmt.Errorf("invalid frozen role frontmatter")
		}
		for i := 1; i < len(lines); i++ {
			line := strings.TrimSuffix(lines[i], "\r")
			if line == "---" {
				close = i
				break
			}
			if strings.HasPrefix(line, "skills:") {
				if start >= 0 || line != "skills:" {
					return "", nil, fmt.Errorf("ambiguous frozen role skills")
				}
				start = i
			}
		}
		if close < 0 || start < 0 {
			return "", nil, fmt.Errorf("missing frozen role skills")
		}
		end := start + 1
		names := []string{}
		for end < close {
			line := strings.TrimSuffix(lines[end], "\r")
			if line != "" && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
				break
			}
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "- ") {
				names = append(names, strings.TrimPrefix(trim, "- "))
			} else if trim != "" && !strings.HasPrefix(trim, "#") {
				return "", nil, fmt.Errorf("unsupported frozen skill structure")
			}
			end++
		}
		return strings.Join(lines[:start+1], "\n") + "\n" + strings.Join(lines[end:], "\n"), names, nil
	}
	left, _, err := strip(original)
	if err != nil {
		return err
	}
	right, names, err := strip(generated)
	if err != nil {
		return err
	}
	if left != right || len(core) == 0 || len(core) != len(names) {
		return fmt.Errorf("generated core role changes original authority/body or core binding")
	}
	for i := range core {
		if core[i] != names[i] {
			return fmt.Errorf("generated core role list differs from its frozen profile")
		}
	}
	return nil
}
