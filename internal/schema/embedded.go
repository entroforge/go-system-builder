package schema

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// embeddedFS holds the schema/example/template files that the Harness needs
// at runtime. They are compiled into the binary and never read from disk, so
// they are invisible to users and to AI agents.
//
//go:embed assets/*.json
var embeddedFS embed.FS

const assetDir = "assets"

// AssetNames returns the basenames of all embedded json files, sorted.
func AssetNames() []string {
	entries, err := fs.ReadDir(embeddedFS, assetDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// ReadAsset returns the raw bytes of an embedded file by basename (e.g.
// "loop-state.schema.json"). The file must exist under assets/.
func ReadAsset(name string) ([]byte, error) {
	// Strip any directory prefix the caller may have passed.
	name = stripDir(name)
	data, err := embeddedFS.ReadFile(fmt.Sprintf("%s/%s", assetDir, name))
	if err != nil {
		return nil, fmt.Errorf("embedded asset %s: %w", name, err)
	}
	return data, nil
}

// AssetExists reports whether an embedded file with the given basename exists.
func AssetExists(name string) bool {
	name = stripDir(name)
	_, err := embeddedFS.ReadFile(fmt.Sprintf("%s/%s", assetDir, name))
	return err == nil
}

func stripDir(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
