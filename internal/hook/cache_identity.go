package hook

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// diagnosticCachePath names only a disposable diagnostic slot, never a
// workflow identity or qualification. A long platform session or agent ID
// maps to one deterministic bounded directory key instead of a nested chain.
func diagnosticCachePath(root, category string, parts ...string) string {
	data, _ := json.Marshal(parts)
	encoded := base64.RawURLEncoding.EncodeToString(data)
	path := filepath.Join(root, ".claude", category, "v2")
	if len(encoded) > 200 {
		path = filepath.Join(path, "p", workpacket.BoundedPacketPathKey(encoded))
	} else {
		path = filepath.Join(path, encoded)
	}
	return path
}
