package transition

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
)

// Shared inputs use the existing design document lifecycle. The stable path
// identity includes examples and local schema dependencies, not only root files.
func sharedModelDocuments(state map[string]any, ctx *ActionContext, register bool) (int, error) {
	root := actionRoot(state, ctx)
	var files fileview.Reader = fileview.Disk{Root: root}
	if ctx.Request != nil && ctx.Request.Files != nil {
		files = ctx.Request.Files
	}
	bound, _ := state["bound_req"].(map[string]any)
	reqID, _ := bound["id"].(string)
	result := sharedmodel.Check(root, files, reqID)
	if len(result.Problems) > 0 {
		return 0, fmt.Errorf("shared model baseline: %s", strings.Join(result.Problems, "; "))
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation := integerOf(baseline["generation"])
	if !register {
		// Check all previously registered model subjects as well: deleting the
		// policy/table must not silently shrink an already reviewed baseline.
		docs, _ := state["documents"].([]any)
		for _, raw := range docs {
			d, _ := raw.(map[string]any)
			id, _ := d["id"].(string)
			if !strings.HasPrefix(id, "shared-model:") || integerOf(d["generation"]) != generation {
				continue
			}
			path, _ := d["path"].(string)
			b, err := files.ReadFile(filepath.Join(root, path))
			if err != nil || SHA256(b) != d["sha256"] {
				return 0, fmt.Errorf("shared model %s changed since S3 registration; revise and re-review the design baseline", path)
			}
			if _, ok := result.Files[path]; !ok {
				return 0, fmt.Errorf("shared model %s disappeared from the declared baseline; return to planning", path)
			}
		}
		for path, b := range result.Files {
			found := false
			for _, raw := range docs {
				d, _ := raw.(map[string]any)
				if d["id"] == "shared-model:"+path && integerOf(d["generation"]) == generation && d["sha256"] == SHA256(b) {
					found = true
					break
				}
			}
			if !found {
				return 0, fmt.Errorf("shared model %s was not registered at S3; return to planning before freezing", path)
			}
		}
		return 0, nil
	}
	// A legitimate return to S3 replaces this generation's shared input set;
	// old generations remain historical. Freezing never performs this replacement.
	docs, _ := state["documents"].([]any)
	kept := make([]any, 0, len(docs))
	for _, raw := range docs {
		d, _ := raw.(map[string]any)
		id, _ := d["id"].(string)
		if strings.HasPrefix(id, "shared-model:") && integerOf(d["generation"]) == generation {
			continue
		}
		kept = append(kept, raw)
	}
	state["documents"] = kept
	paths := make([]string, 0, len(result.Files))
	for p := range result.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	actor := ""
	if ctx.Request != nil {
		actor = ctx.Request.Actor
	}
	for _, path := range paths {
		state["documents"] = appendDocument(state["documents"], map[string]any{
			"id": "shared-model:" + path, "kind": "design", "path": path, "version": "unversioned",
			"sha256": SHA256(result.Files[path]), "status": "locked", "generation": generation,
			"author_agent_id": actor, "registered_at": ctx.OccurredAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return len(paths), nil
}
