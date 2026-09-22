package transition

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"path/filepath"
	"strings"
	"time"
)

func planningREQ(state map[string]any) string {
	b, _ := state["bound_req"].(map[string]any)
	s, _ := b["id"].(string)
	return s
}
func dispatchPlanDocuments(state map[string]any, ctx *ActionContext, register bool) error {
	root := actionRoot(state, ctx)
	var files fileview.Reader = fileview.Disk{Root: root}
	if ctx.Request != nil && ctx.Request.Files != nil {
		files = ctx.Request.Files
	}
	p, err := semantic.LoadDispatchPlan(root, files, planningREQ(state))
	if err != nil {
		return err
	}
	if len(p.Problems) > 0 {
		return fmt.Errorf("dispatch plan: %s", strings.Join(p.Problems, "; "))
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation := integerOf(baseline["generation"])
	docs, _ := state["documents"].([]any)
	if !register {
		found := false
		for _, raw := range docs {
			d, _ := raw.(map[string]any)
			if integerOf(d["generation"]) != generation {
				continue
			}
			if d["kind"] == "dispatch_plan" {
				if p.Path == "" || d["path"] != p.Path || d["sha256"] != SHA256(p.Content) {
					return fmt.Errorf("dispatch plan changed since S4; return to planning and re-review")
				}
				found = true
			}
		}
		if p.Path != "" && !found {
			return fmt.Errorf("dispatch plan was not registered at S4")
		}
		if p.Path != "" {
			wanted := map[string]bool{}
			for _, t := range p.Tasks {
				wanted[t.ID] = true
				ok := false
				for _, raw := range docs {
					d, _ := raw.(map[string]any)
					if d["kind"] == "task" && d["id"] == t.ID && integerOf(d["generation"]) == generation {
						b, e := files.ReadFile(filepath.Join(root, t.Path))
						ok = e == nil && d["sha256"] == SHA256(b)
					}
				}
				if !ok {
					return fmt.Errorf("TASK %s differs from S4 registered plan", t.ID)
				}
			}
			for _, raw := range docs {
				d, _ := raw.(map[string]any)
				if d["kind"] == "task" && integerOf(d["generation"]) == generation {
					id, _ := d["id"].(string)
					if !wanted[id] {
						return fmt.Errorf("registered TASK %s is outside dispatch plan", id)
					}
				}
			}
		}
		return nil
	}
	if p.Path == "" {
		return nil
	}
	// Legitimate S4 re-registration replaces this generation's exact set only.
	keep := []any{}
	for _, raw := range docs {
		d, _ := raw.(map[string]any)
		if integerOf(d["generation"]) == generation && (d["kind"] == "dispatch_plan" || d["kind"] == "task") {
			continue
		}
		keep = append(keep, raw)
	}
	state["documents"] = keep
	actor := ""
	if ctx.Request != nil {
		actor = ctx.Request.Actor
	}
	state["documents"] = appendDocument(state["documents"], map[string]any{"id": "dispatch-plan:" + p.REQ, "kind": "dispatch_plan", "path": p.Path, "version": p.Revision, "sha256": SHA256(p.Content), "status": "complete", "generation": generation, "author_agent_id": actor, "registered_at": ctx.OccurredAt.UTC().Format(time.RFC3339Nano)})
	return nil
}
