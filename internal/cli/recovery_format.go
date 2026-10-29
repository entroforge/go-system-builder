package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/recovery"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// Document reconstruction has no authority to invent issued work, reception
// keys or professional activation. Original validated pending commits are
// restored by the Writer before this lower-trust path is considered.
func recoverySeedRuntime(root string, inventory recovery.Inventory, at time.Time) (map[string]any, error) {
	fresh, err := inactiveRuntimeState(root, at)
	if err != nil {
		return nil, err
	}
	var source map[string]any
	for _, input := range inventory.Inputs {
		if input.Kind != recovery.InputKindRuntimeState && input.Kind != recovery.InputKindRuntimePending {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(input.Path)))
		if err != nil {
			return nil, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != input.SHA256 {
			return nil, fmt.Errorf("recovery format input changed: %s", input.Path)
		}
		var value map[string]any
		if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &value); err != nil {
			continue
		}
		candidates := []map[string]any{value}
		for _, key := range []string{"state", "fresh_state"} {
			if nested, ok := value[key].(map[string]any); ok {
				candidates = append(candidates, nested)
			}
		}
		if encoded, ok := value["candidate_state_base64"].(string); ok {
			if data, err := base64.StdEncoding.DecodeString(encoded); err == nil {
				var nested map[string]any
				if json.Unmarshal(data, &nested) == nil {
					candidates = append(candidates, nested)
				}
			}
		}
		for candidateIndex, candidate := range candidates {
			version, _ := candidate["schema_version"].(string)
			if version != "1.1.0" && !workstate.SupportsAssignments(version) {
				if version != "" && candidateIndex == 0 && input.Kind == recovery.InputKindRuntimeState {
					return nil, fmt.Errorf("recovery source Runtime format %q is unsupported; preserve the original pair and matching release", version)
				}
				continue
			}
			// A marker envelope is not itself a Runtime. Require its identity.
			if _, ok := candidate["runtime_id"].(string); !ok {
				continue
			}
			if workstate.SupportsAssignments(version) {
				entities, _ := candidate["entities"].(map[string]any)
				for _, key := range []string{"work_packets", "submissions"} {
					if raw, exists := entities[key]; exists {
						rows, ok := raw.([]any)
						if !ok || len(rows) > 0 {
							return nil, fmt.Errorf("recovery cannot reconstruct issued packet/reception indices from documents (%s); restore the original state/journal and validated pending operations", input.Path)
						}
					}
				}
				agents, _ := entities["agents"].([]any)
				for _, raw := range agents {
					agent, _ := raw.(map[string]any)
					if _, bound := agent["context_binding"]; bound {
						return nil, fmt.Errorf("recovery cannot reconstruct fixed execution context bindings from documents; restore the original pair/checkpoint (%s)", input.Path)
					}
				}
			}
			if source != nil {
				before, _ := source["configuration"].(map[string]any)
				after, _ := candidate["configuration"].(map[string]any)
				if source["schema_version"] != candidate["schema_version"] || !reflect.DeepEqual(before["optimization"], after["optimization"]) {
					return nil, fmt.Errorf("recovery format sources disagree; preserve the original pair and pending markers")
				}
			}
			source = candidate
		}
	}
	if source == nil {
		// New initialization records its format so an unreadable 1.2 state never
		// silently falls back to the legacy artifact reconstruction format.
		data, err := os.ReadFile(filepath.Join(root, ".claude/runtime-initialization.json"))
		if err == nil {
			var receipt struct {
				Version string `json:"runtime_schema_version"`
			}
			if json.Unmarshal(data, &receipt) != nil || receipt.Version != "1.1.0" {
				return nil, fmt.Errorf("new Runtime format is unreadable; restore its original pair/pending source before document reconstruction")
			}
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if recoveryInputExists(inventory.Inputs, recovery.InputKindRuntimeState, ".claude/loop-state.json") {
			source = map[string]any{"schema_version": "1.1.0", "configuration": map[string]any{}}
		} else {
			return fresh, nil
		}
	}
	return runtime.PreserveRuntimeFormat(source, fresh)
}
