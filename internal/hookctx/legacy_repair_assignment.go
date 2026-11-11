package hookctx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

type legacyReadCacheKey struct{}

// LegacyRepairReadCache is scoped by the caller to one request and Runtime revision.
// It caches only successful source reads, never an unavailable or canceled read.
type LegacyRepairReadCache struct {
	mu    sync.Mutex
	reads map[string][]byte
}

func WithLegacyRepairReadCache(ctx context.Context, cache *LegacyRepairReadCache) context.Context {
	return context.WithValue(ctx, legacyReadCacheKey{}, cache)
}

// ErrLegacyRepairOwnerNotRegistered distinguishes a known absence from an unreadable projection.
var ErrLegacyRepairOwnerNotRegistered = errors.New("original repair owner is not registered")

// ResolveLegacyRepairManifestAssignment uses original registered sources. It
// grants no authority, and unavailable projections are reported as unknown.
func ResolveLegacyRepairManifestAssignment(ctx context.Context, root string, snapshot runtime.Snapshot, repairID, owner string) (string, error) {
	_, id, err := ResolveLegacyRepairResponsibility(ctx, root, snapshot, repairID, owner)
	return id, err
}

// ResolveLegacyRepairResponsibility obtains the repair owner and manifest
// coordinate from the two original loader projections. No TASK-to-PlanReport
// relation is inferred. Filesystem reads follow the original loader and are
// not forcibly interruptible.
func ResolveLegacyRepairResponsibility(ctx context.Context, root string, snapshot runtime.Snapshot, repairID, requestedOwner string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if repairID == "" {
		return "", "", fmt.Errorf("original repair responsibility is unavailable")
	}
	data, err := json.Marshal(snapshot.State)
	if err != nil {
		return "", "", err
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return "", "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	cache, _ := ctx.Value(legacyReadCacheKey{}).(*LegacyRepairReadCache)
	if cache == nil {
		cache = &LegacyRepairReadCache{}
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.reads == nil {
		cache.reads = make(map[string][]byte)
	}
	read := func(path string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cached, ok := cache.reads[path]; ok {
			return cached, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cache.reads[path] = data
		return data, nil
	}
	review, _ := snapshot.State["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	owners, _ := pointer["assignment_owners"].(map[string]any)
	registeredOwner, _ := owners[repairID].(string)
	var plan repairPlanHook
	planValid := false
	var sourceErr error
	readArtifact := func(relative string) ([]byte, bool) {
		if strings.TrimSpace(relative) == "" {
			return nil, false
		}
		path, e := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(relative)))
		if e != nil {
			sourceErr = e
			return nil, false
		}
		rel, e := filepath.Rel(rootAbs, path)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			sourceErr = fmt.Errorf("original source path unavailable: %s", relative)
			return nil, false
		}
		b, e := read(path)
		if e != nil {
			sourceErr = e
			return nil, false
		}
		return b, true
	}
	// Existing assignment_owners is itself a registered dispatch fact. For older
	// states lacking it, use the same Plan/PlanReport lookup as LoadFull.
	if registeredOwner == "" {
		path, _ := pointer["plan_ref"].(string)
		if b, ok := readArtifact(path); ok && json.Unmarshal(b, &plan) == nil {
			planValid = true
		}
		if !planValid {
			if sourceErr == nil {
				sourceErr = fmt.Errorf("original repair plan is unavailable")
			}
			return "", "", sourceErr
		}
	}
	owner := registeredOwner
	if requestedOwner != "" && registeredOwner != "" && requestedOwner != registeredOwner {
		return "", "", fmt.Errorf("original owner for %s differs", repairID)
	}
	if registeredOwner == "" {
		// The original selector operates per registered agent. Multiple matching
		// agents cannot establish the one owner required for a workspace command.
		for _, a := range state.Entities.Agents {
			if requestedOwner != "" && a.ID != requestedOwner {
				continue
			}
			selected, _, _ := legacyRepairAgentScope(plan, pointer, a.ID, readArtifact)
			if selected != repairID {
				continue
			}
			if owner != "" && owner != a.ID {
				return "", "", fmt.Errorf("original repair owner is ambiguous")
			}
			owner = a.ID
		}
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
	}
	if owner == "" {
		if sourceErr != nil {
			return "", "", fmt.Errorf("original owner for assignment %s is unknown: %w", repairID, sourceErr)
		}
		return "", "", fmt.Errorf("assignment %s: %w", repairID, ErrLegacyRepairOwnerNotRegistered)
	}
	if requestedOwner != "" && owner != requestedOwner {
		return "", "", fmt.Errorf("original owner for assignment %s differs", repairID)
	}
	refs := registeredManifestRefs(state)[owner]
	var taskIDs []string
	for _, a := range state.Entities.Agents {
		if a.ID == owner {
			taskIDs = append(taskIDs, a.TaskIDs...)
			break
		}
	}
	if len(taskIDs) == 0 {
		return owner, "", fmt.Errorf("no TASKs are registered to owner %s", owner)
	}
	reqID := ""
	if bound, ok := snapshot.State["bound_req"].(map[string]any); ok {
		reqID, _ = bound["id"].(string)
	}
	if reqID == "" && strings.HasPrefix(state.RuntimeID, "loop-REQ-") {
		reqID = strings.TrimPrefix(state.RuntimeID, "loop-")
	}
	readManifest := func(path string) (string, *workgroupManifest, error) {
		b, e := read(path)
		if e != nil {
			return "", nil, e
		}
		var m workgroupManifest
		if e = json.Unmarshal(b, &m); e != nil {
			return "", nil, e
		}
		rel, e := filepath.Rel(rootAbs, path)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return "", nil, fmt.Errorf("manifest outside original root")
		}
		return filepath.ToSlash(filepath.Clean(rel)), &m, nil
	}
	var lastErr error
	for _, taskID := range taskIDs {
		if err := ctx.Err(); err != nil {
			return owner, "", err
		}
		var fallbackErr error
		fallback := func() (string, *workgroupManifest) {
			dir := filepath.Join(rootAbs, ".claude", "workgroups")
			if reqID != "" {
				path, m, e := readManifest(filepath.Join(dir, reqID, taskID, "manifest.json"))
				fallbackErr = e
				return path, m
			}
			entries, e := os.ReadDir(dir)
			if e != nil {
				fallbackErr = e
				return "", nil
			}
			var foundPath string
			var found *workgroupManifest
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					fallbackErr = err
					return "", nil
				}
				if !entry.IsDir() {
					continue
				}
				path, m, e := readManifest(filepath.Join(dir, entry.Name(), taskID, "manifest.json"))
				if e != nil {
					// The original unbound loader ignores unavailable or malformed
					// candidates and selects the sole readable manifest. Cancellation
					// is still unknown, never a partial uniqueness determination.
					if err := ctx.Err(); err != nil {
						fallbackErr = err
						return "", nil
					}
					continue
				}
				if found != nil {
					fallbackErr = fmt.Errorf("unbound TASK manifest is ambiguous")
					return "", nil
				}
				foundPath, found = path, m
			}
			return foundPath, found
		}
		var sourceReadErr error
		reader := func(path string) ([]byte, error) {
			b, e := read(path)
			if e != nil {
				sourceReadErr = e
			}
			return b, e
		}
		_, manifest, fragment := assignmentManifestForRefsWithReader(rootAbs, taskID, refs, reader, fallback)
		if err := ctx.Err(); err != nil {
			return owner, "", err
		}
		if manifest == nil {
			if sourceReadErr != nil {
				lastErr = sourceReadErr
			} else if fallbackErr != nil {
				lastErr = fallbackErr
			} else {
				lastErr = fmt.Errorf("registered manifest for TASK %s unavailable", taskID)
			}
			// The original loader evaluates TASKs in order and continues when
			// this TASK does not resolve; formal-reference priority still applies
			// independently within assignmentManifestForRefsWithReader.
			continue
		}
		if manifest.RuntimeID != "" && state.RuntimeID != "" && manifest.RuntimeID != state.RuntimeID {
			continue
		}
		if a := selectManifestAssignment(manifest, owner, taskID, fragment); a != nil {
			return owner, a.AssignmentID, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("original manifest assignment is unavailable")
	}
	return owner, "", lastErr
}
