package semantic

import (
	"encoding/json"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// RuntimeCandidateValidator is the composition-root adapter used by Runtime
// mutation writers. The semantic package imports Runtime only for the
// interface assertion; Runtime itself does not import semantic, avoiding an
// import cycle while making semantic validation mandatory at commit time.
type RuntimeCandidateValidator struct{}

var _ runtime.CandidateValidator = RuntimeCandidateValidator{}

func (RuntimeCandidateValidator) ValidateCandidate(root string, state map[string]any) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return ValidateRuntimeBytes(root, data)
}
