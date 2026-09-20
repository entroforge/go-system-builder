package plancheckpoint

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"os"
	"path/filepath"
	"strings"
)

// ActivatedRef derives a missing observation from the existing validated
// activation chain. It never writes Runtime or trusts a working-state label.
func ActivatedRef(root string, snapshot runtime.Snapshot, agentID string) (string, error) {
	entities, _ := snapshot.State["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	for _, raw := range rows {
		a, _ := raw.(map[string]any)
		if a["id"] != agentID {
			continue
		}
		if a["dispatch_mode"] != "plan_checkpoint" {
			return "", nil
		}
		ref := strValue(a["readback_ref"])
		if ref == "" || strValue(a["activation_ref"]) == "" {
			return "", nil
		}
		if err := Validate(root, snapshot, agentID, ref); err != nil {
			return "", err
		}
		plan, err := readInside(root, ref)
		if err != nil {
			return "", err
		}
		activation, err := readInside(root, strValue(a["activation_ref"]))
		if err != nil {
			return "", err
		}
		if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", activation); err != nil {
			return "", err
		}
		var p, m map[string]any
		if err := json.Unmarshal(plan, &p); err != nil {
			return "", err
		}
		if err := json.Unmarshal(activation, &m); err != nil {
			return "", err
		}
		if m["message_type"] != "activation" || m["agent_id"] != agentID || m["runtime_id"] != snapshot.State["runtime_id"] || m["task_id"] != p["task_id"] || m["team_id"] != p["team_id"] {
			return "", fmt.Errorf("activation identity differs from current plan")
		}
		sum := sha256.Sum256(plan)
		if m["approved_readback_sha256"] != fmt.Sprintf("%x", sum) || m["approved_readback_message_id"] != p["message_id"] {
			return "", fmt.Errorf("activation no longer binds the current plan bytes")
		}
		return ref, nil
	}
	return "", nil
}

func readInside(root, ref string) ([]byte, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p, err := filepath.EvalSymlinks(resolveRootPath(root, ref))
	if err != nil {
		return nil, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(realRoot, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("evidence escapes repository")
	}
	return os.ReadFile(p)
}
