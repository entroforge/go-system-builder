package evidence

import "strings"

// ParsePathReference locates the repository path before an optional fragment.
// Fragment text is neither interpreted nor recorded as proof; the caller's
// professional reference remains unchanged.
func ParsePathReference(ref string) (string, error) {
	rel := strings.TrimPrefix(ref, "path:")
	rel, _, _ = strings.Cut(rel, "#")
	return rel, nil
}
