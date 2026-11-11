package hook

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
)

// diagnosticCachePath names only a disposable diagnostic slot, never a
// workflow identity or qualification. Split the reversible encoding so a
// long platform session or agent ID cannot exceed a filename component limit.
func diagnosticCachePath(root, category string, parts ...string) string {
	data, _ := json.Marshal(parts)
	encoded := base64.RawURLEncoding.EncodeToString(data)
	path := filepath.Join(root, ".claude", category, "v2")
	for len(encoded) > 120 {
		path = filepath.Join(path, encoded[:120])
		encoded = encoded[120:]
	}
	return filepath.Join(path, encoded)
}
