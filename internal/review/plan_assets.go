package review

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Canonical plans use the original source declaration at the pinned Main
// commit. Historical disk-mode plans keep their existing contained disk view.
func planAssetReader(ctx context.Context, root string, plan *Plan, state map[string]any) (func(string) ([]byte, error), error) {
	return func(p string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return workpacket.ReadFile(root, p, 2<<20)
	}, nil
}

func caseReferenceParts(ref string) (string, string, error) {
	p, id, ok := strings.Cut(ref, "#")
	if !ok || id == "" || strings.ContainsAny(id, "#/\\") || p != path.Clean(p) || !strings.HasPrefix(p, "docs/design/prototypes/") || path.Base(p) != "cases.json" {
		return "", "", &PlanAnswerError{fmt.Errorf("use an exact cases.json#CASE reference, got %q", ref)}
	}
	return p, id, nil
}
