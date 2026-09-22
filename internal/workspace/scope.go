package workspace

import "strings"

// PathMatchesScope reports whether a slash-separated repo-relative file
// path matches one write-path pattern. A pattern ending in "/**" (or a
// bare directory without wildcards) covers the whole subtree.
func PathMatchesScope(file, pattern string) bool {
	file = strings.Trim(file, "/")
	pattern = strings.Trim(pattern, "/")
	if file == "" || pattern == "" {
		return false
	}
	if pattern == "**" {
		return true
	}
	fileParts := strings.Split(file, "/")
	patternParts := strings.Split(pattern, "/")
	// A directory-only pattern (no extension and no wildcard in the last
	// segment) is a prefix match: "internal/order" covers
	// internal/order/anything.go.
	if !strings.Contains(patternParts[len(patternParts)-1], "*") &&
		!strings.Contains(patternParts[len(patternParts)-1], ".") {
		return pathUnderDirectory(fileParts, patternParts)
	}
	return segmentsMatch(fileParts, 0, patternParts, 0)
}

func pathUnderDirectory(fileParts []string, dirParts []string) bool {
	if len(fileParts) <= len(dirParts) {
		return false
	}
	for i, part := range dirParts {
		if !segmentGlob(part, fileParts[i]) {
			return false
		}
	}
	return true
}

// segmentsMatch implements `**` (any number of segments) and `*`
// (within one segment) glob matching over slash-split paths.
func segmentsMatch(file []string, fi int, pattern []string, pi int) bool {
	if pi == len(pattern) {
		return fi == len(file)
	}
	if pattern[pi] == "**" {
		for skip := fi; skip <= len(file); skip++ {
			if segmentsMatch(file, skip, pattern, pi+1) {
				return true
			}
		}
		return false
	}
	if fi == len(file) {
		return false
	}
	if !segmentGlob(pattern[pi], file[fi]) {
		return false
	}
	return segmentsMatch(file, fi+1, pattern, pi+1)
}

// segmentGlob matches one path segment with `*` wildcards.
func segmentGlob(pattern, segment string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == segment
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == segment
	}
	if !strings.HasPrefix(segment, parts[0]) || !strings.HasSuffix(segment, parts[len(parts)-1]) {
		return false
	}
	rest := segment
	if len(parts[0]) > 0 {
		rest = strings.TrimPrefix(rest, parts[0])
	}
	for _, part := range parts[1 : len(parts)-1] {
		idx := strings.Index(rest, part)
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(part):]
	}
	if tail := parts[len(parts)-1]; len(tail) > 0 {
		return strings.HasSuffix(rest, tail) || strings.Contains(rest, tail)
	}
	return true
}
