package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Lifecycle messages referenced by the Runtime must outlive the Worker's
// worktree. A task-complete message is read once by its consumer, which saves
// the original Result independently, so it needs no additional copy.
func freezeWorkerSubmission(main, worker, path string, e workspace.Execution, state map[string]any, retain bool) (string, error) {
	physical, err := workspace.Canonical(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(worker, physical)
	if err != nil || !filepath.IsLocal(rel) {
		// Existing Main artifacts are already durable. Do not import an
		// arbitrary external file or another Worker's private submission.
		rel, err = filepath.Rel(main, physical)
		if err != nil || !filepath.IsLocal(rel) {
			return "", fmt.Errorf("submission escapes the bound project")
		}
		_, marked, err := workspace.PointerRoot(filepath.Dir(physical))
		if err != nil {
			return "", err
		}
		if marked {
			return "", fmt.Errorf("submission belongs to another Worker")
		}
		return physical, nil
	}
	info, err := os.Stat(physical)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return "", fmt.Errorf("Worker lifecycle submission must be a JSON file no larger than 8 MiB")
	}
	// The completion consumer reads and validates the original once, then
	// persists its own result. Avoid copying or pre-reading the same answer.
	if !retain {
		return physical, nil
	}
	data, err := os.ReadFile(physical)
	if err != nil {
		return "", err
	}
	if !json.Valid(data) {
		return "", fmt.Errorf("Worker lifecycle submission is not valid JSON")
	}
	dir := filepath.Join(filepath.Dir(workspaceExecutionCheckpointPath(main, e)), "submissions")
	if prior := registeredWorkerSubmission(main, dir, e.AgentID, state, data); prior != "" {
		return prior, nil
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	// Each otherwise unconfirmed publication gets its own slot. An unknown
	// existing file is never read as evidence or overwritten.
	f, err := os.CreateTemp(dir, ".submission-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	base := "submission-" + strings.TrimPrefix(filepath.Base(f.Name()), ".submission-")
	for i := 0; ; i++ {
		name := base + ".json"
		if i > 0 {
			name = fmt.Sprintf("%s-%d.json", base, i)
		}
		dest := filepath.Join(dir, name)
		if err = os.Link(f.Name(), dest); os.IsExist(err) {
			continue
		} else if err != nil {
			return "", err
		}
		return dest, nil
	}
}

// Only an actual Runtime reference for this owner may identify a prior
// submission. Comparing its JSON expression is a replay optimization: any
// different or unreadable value is sent to the original consumer as a new
// candidate, never rejected by this adapter.
func registeredWorkerSubmission(main, dir, owner string, state map[string]any, data []byte) string {
	if workpacket.ValidateUniqueJSON(data) != nil {
		return ""
	}
	var current any
	if json.Unmarshal(data, &current) != nil {
		return ""
	}
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent["id"] != owner {
			continue
		}
		for key, value := range agent {
			if !strings.HasSuffix(key, "_ref") {
				continue
			}
			ref, ok := value.(string)
			if !ok || ref == "" {
				continue
			}
			abs := ref
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(main, filepath.FromSlash(ref))
			}
			relDir, err := filepath.Rel(dir, abs)
			if err != nil || !filepath.IsLocal(relDir) {
				continue
			}
			relMain, err := filepath.Rel(main, abs)
			if err != nil || !filepath.IsLocal(relMain) {
				continue
			}
			prior, err := workpacket.ReadFile(main, filepath.ToSlash(relMain), 8<<20)
			if err != nil || workpacket.ValidateUniqueJSON(prior) != nil {
				continue
			}
			var previous any
			if json.Unmarshal(prior, &previous) == nil && reflect.DeepEqual(previous, current) {
				return abs
			}
		}
		break
	}
	return ""
}
