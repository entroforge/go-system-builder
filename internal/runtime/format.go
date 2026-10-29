package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/workstate"
)

// PreserveRuntimeFormat builds a fresh reset target with the actual source's
// format and complete optimization configuration. It copies data; callers do
// not gain a schema migration capability. History belongs to the sealed pair.
func PreserveRuntimeFormat(source, fresh map[string]any) (map[string]any, error) {
	version, ok := source["schema_version"].(string)
	if !ok || (version != "1.1.0" && version != "1.2.0" && version != workstate.UnifiedVersion) {
		return nil, fmt.Errorf("unsupported source Runtime format %q", version)
	}
	data, err := json.Marshal(fresh)
	if err != nil {
		return nil, err
	}
	var target map[string]any
	if err := json.Unmarshal(data, &target); err != nil {
		return nil, err
	}
	target["schema_version"] = version
	config, ok := target["configuration"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("fresh Runtime configuration is missing")
	}
	delete(config, "optimization")
	if workstate.SupportsAssignments(version) {
		original, _ := source["configuration"].(map[string]any)
		if optimization, exists := original["optimization"]; exists {
			data, err := json.Marshal(optimization)
			if err != nil {
				return nil, err
			}
			var cloned any
			if err := json.Unmarshal(data, &cloned); err != nil {
				return nil, err
			}
			config["optimization"] = cloned
		}
		entities, ok := target["entities"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("fresh Runtime entities are missing")
		}
		entities["work_packets"] = []any{}
		entities["submissions"] = []any{}
	} else {
		entities, _ := target["entities"].(map[string]any)
		delete(entities, "work_packets")
		delete(entities, "submissions")
	}
	return target, nil
}

func validateSameRuntimeFormat(source, target map[string]any) error {
	if source["schema_version"] != target["schema_version"] {
		return fmt.Errorf("ordinary updates/reset cannot change Runtime schema; preserve the original pair and matching release")
	}
	before, _ := source["configuration"].(map[string]any)
	after, _ := target["configuration"].(map[string]any)
	beforeOpt, beforeExists := before["optimization"]
	afterOpt, afterExists := after["optimization"]
	if beforeExists != afterExists || !reflect.DeepEqual(beforeOpt, afterOpt) {
		return fmt.Errorf("ordinary updates/reset cannot change optimization configuration")
	}
	return nil
}
