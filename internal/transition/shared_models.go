package transition

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
)

// Shared inputs use the existing design document lifecycle. The stable path
// identity includes examples and local schema dependencies, not only root files.
func sharedModelDocuments(state map[string]any, ctx *ActionContext, register bool, accepted *acceptedS3ContractBatch) (int, error) {
	// S5 already reviewed the original material; a later transition must not
	// rescan the model set to authenticate or expand that professional work.
	if !register {
		return 0, nil
	}
	root := actionRoot(state, ctx)
	var files fileview.Reader = fileview.Disk{Root: root}
	if ctx.Request != nil && ctx.Request.Files != nil {
		files = ctx.Request.Files
	}
	bound, _ := state["bound_req"].(map[string]any)
	reqID, _ := bound["id"].(string)
	var paths []string
	if accepted != nil {
		for path := range accepted.sharedOutputs {
			paths = append(paths, path)
		}
	} else {
		result := sharedmodel.Check(root, files, reqID)
		if len(result.Problems) > 0 {
			return 0, fmt.Errorf("shared model baseline: %s", strings.Join(result.Problems, "; "))
		}
		for path := range result.Files {
			paths = append(paths, path)
		}
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation := integerOf(baseline["generation"])

	// A legitimate return to S3 replaces this generation's shared input set;
	// old generations remain historical. Freezing never performs this replacement.
	docs, _ := state["documents"].([]any)
	kept := make([]any, 0, len(docs))
	for _, raw := range docs {
		d, _ := raw.(map[string]any)
		id, _ := d["id"].(string)
		if accepted == nil && strings.HasPrefix(id, "shared-model:") && integerOf(d["generation"]) == generation {
			continue
		}
		kept = append(kept, raw)
	}
	state["documents"] = kept
	sort.Strings(paths)
	actor := ""
	if ctx.Request != nil {
		actor = ctx.Request.Actor
	}
	for _, path := range paths {
		state["documents"] = appendDocument(state["documents"], map[string]any{
			"id": "shared-model:" + path, "kind": "design", "path": path, "version": "unversioned",
			"status": "locked", "generation": generation,
			"author_agent_id": actor, "registered_at": ctx.OccurredAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return len(paths), nil
}
