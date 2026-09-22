package semantic

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateAgentDefinitions checks the required metadata of bundled role files.
// This is a structural installation check, not proof of a real Claude launch.
// Other custom agents and platform-specific optional YAML fields are untouched.
func ValidateAgentDefinitions(root string) error {
	dir := filepath.Join(root, ".claude", "agents")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		dir = filepath.Join(root, "agents")
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	for _, role := range []string{"backend-builder", "frontend-builder", "test-builder", "delivery-verifier", "document-verifier", "e2e-tester", "qa", "investigator"} {
		file := filepath.Join(dir, role+".md")
		raw, err := os.ReadFile(file)
		// Minimal fixtures may intentionally omit roles. Every shipped role is
		// separately required by the source/release regression test.
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		if len(lines) < 3 || lines[0] != "---" {
			return fmt.Errorf("agent definition %s: missing YAML frontmatter", file)
		}
		fields := map[string]string{}
		closed := false
		for _, line := range lines[1:] {
			if line == "---" {
				closed = true
				break
			}
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			if _, found := fields[k]; found {
				return fmt.Errorf("agent definition %s: duplicate metadata %s", file, k)
			}
			fields[k] = strings.Trim(strings.TrimSpace(v), "\"'")
		}
		if !closed {
			return fmt.Errorf("agent definition %s: unclosed YAML frontmatter", file)
		}
		if fields["name"] != role || fields["description"] == "" {
			return fmt.Errorf("agent definition %s: require name=%s and nonempty description", file, role)
		}
	}
	return nil
}
