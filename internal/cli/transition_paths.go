package cli

import (
	"fmt"
	"path"
	"strings"
)

// Paths are lexical repository-relative identities, including deleted files.
// Do not infer a full sweep or expand filesystem globs from an empty list.
func normalizeTransitionAffectedPaths(values []string) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	for _, value := range values {
		if value == "all" {
			if len(values) != 1 {
				return nil, fmt.Errorf("--affected-paths all must be used alone")
			}
			return []string{"all"}, nil
		}
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\:*?[]\x00\r\n\t") || strings.HasPrefix(value, "/") {
			return nil, fmt.Errorf("invalid --affected-paths %q: require a literal repository-relative path", value)
		}
		for _, part := range strings.Split(value, "/") {
			if part == ".." {
				return nil, fmt.Errorf("invalid --affected-paths %q: parent traversal is forbidden", value)
			}
		}
		cleaned := path.Clean(value)
		if cleaned == "." || cleaned == "all" {
			return nil, fmt.Errorf("use --affected-paths all explicitly for a full sweep")
		}
		if !seen[cleaned] {
			result = append(result, cleaned)
			seen[cleaned] = true
		}
	}
	return result, nil
}
