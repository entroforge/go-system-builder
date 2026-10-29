// Package assignmentinputs reads the original frozen S6 source namespaces.
// Embedded binary assets are not represented as project filesystem paths.
package assignmentinputs

import (
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const SchemaPrefix = "gsb-embedded/schema/"
const ContextProfileRef = "gsb-embedded/agent-context-profiles.json"

func Read(root, ref string) ([]byte, error) {
	if strings.HasPrefix(ref, SchemaPrefix) {
		name := strings.TrimPrefix(ref, SchemaPrefix)
		if name == "" || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("invalid embedded schema source")
		}
		return schema.ReadAsset(name)
	}
	if ref == ContextProfileRef {
		return agentcontext.ProfileBytes(), nil
	}
	if strings.HasPrefix(ref, "gsb-embedded/") {
		return nil, fmt.Errorf("unknown embedded Assignment source")
	}
	return workpacket.ReadFile(root, ref, 4<<20)
}
