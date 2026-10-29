package workpacket

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/schema"
)

const LegacyWorkerAnswerPath = ".claude/submissions/stage-answer.json"
const WorkerAnswerPath = ".loop-submissions/stage-answer.json"
const WorkerViewPath = ".claude/inputs/work-packet.json"

func ValidWorkerAnswerPath(path string) bool {
	return path == LegacyWorkerAnswerPath || path == WorkerAnswerPath
}

// ExecutionView is an immutable projection installed with the original
// bootstrap receipt. The packet remains the single issued responsibility;
// execution coordinates never come from the mutable answer.
type ExecutionView struct {
	Packet          Packet `json:"packet"`
	Root            string `json:"execution_root"`
	Generation      int    `json:"execution_generation"`
	Owner           string `json:"owner"`
	AuthoringPath   string `json:"authoring_path"`
	FrozenInputRoot string `json:"frozen_input_root"`
}

func BindExecution(p Packet, root string, generation int, owner string, authoringPath ...string) (ExecutionView, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || generation < 1 || p.Binding.Owner != owner {
		return ExecutionView{}, fmt.Errorf("invalid packet execution binding")
	}
	path := LegacyWorkerAnswerPath
	if len(authoringPath) > 1 {
		return ExecutionView{}, fmt.Errorf("invalid packet execution authoring path")
	}
	if len(authoringPath) == 1 {
		path = authoringPath[0]
	}
	if !ValidWorkerAnswerPath(path) {
		return ExecutionView{}, fmt.Errorf("unsupported packet execution authoring path")
	}
	v := ExecutionView{Packet: p, Root: root, Generation: generation, Owner: owner, AuthoringPath: path, FrozenInputRoot: ".claude/inputs"}
	data, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("work-packet-execution.schema.json", data); err != nil {
		return v, err
	}
	return v, nil
}
