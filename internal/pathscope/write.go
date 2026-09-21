package pathscope

import "sort"

// EffectiveWrites is the manifest write_paths/output_paths union consumed by
// registration, activation and integration. Output declarations grant the same
// reviewed write scope at every boundary.
func EffectiveWrites(writePaths, outputPaths []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, paths := range [][]string{writePaths, outputPaths} {
		for _, path := range paths {
			if path != "" && !seen[path] {
				seen[path] = true
				result = append(result, path)
			}
		}
	}
	sort.Strings(result)
	return result
}
