// Package evidence handles evidence references.
package evidence

import (
	"fmt"
	"strings"
)

// ReferenceID decodes one existing runtime: prefix and identifies index refs.
// It neither rewrites the original ref nor registers an alias. A typed ref
// stays index-bound even when its ID contains ://; bare execution anchors do not.
func ReferenceID(ref string) (id string, indexed bool, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false, fmt.Errorf("evidence_ref %q is empty", ref)
	}
	if strings.HasPrefix(ref, "runtime:") {
		id = strings.TrimPrefix(ref, "runtime:")
		if strings.TrimSpace(id) == "" {
			return "", false, fmt.Errorf("evidence_ref %q requires runtime:<evidence-id>", ref)
		}
		return id, true, nil
	}
	return ref, !strings.Contains(ref, "://"), nil
}
