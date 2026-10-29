package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// DispatchBackpressureConfig is frozen with the original initialization pair.
type DispatchBackpressureConfig struct {
	MaxUnreceivedResults    int `json:"max_unreceived_results"`
	ResumeUnreceivedResults int `json:"resume_unreceived_results"`
}

func (c DispatchBackpressureConfig) Validate() error {
	if c.MaxUnreceivedResults < 1 || c.ResumeUnreceivedResults < 0 || c.ResumeUnreceivedResults >= c.MaxUnreceivedResults {
		return fmt.Errorf("dispatch backpressure requires max >= 1 and 0 <= resume < max")
	}
	return nil
}

func DispatchConfig(state map[string]any) (*DispatchBackpressureConfig, error) {
	configuration, _ := state["configuration"].(map[string]any)
	optimization, _ := configuration["optimization"].(map[string]any)
	value, exists := optimization["dispatch_backpressure"]
	if !exists {
		return nil, nil
	}
	if !workstate.SupportsAssignments(state["schema_version"]) {
		return nil, fmt.Errorf("dispatch backpressure requires an explicitly initialized packet Runtime")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if _, ok := fields["resume_unreceived_results"]; !ok {
		return nil, fmt.Errorf("dispatch resume threshold is required")
	}
	var config DispatchBackpressureConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}
