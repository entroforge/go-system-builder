// s7_manifest_draft.go implements `loop-harness s7 manifest-draft`: a
// read-only scaffold of the reviewer team-manifest for one ReviewPlan
// Assignment (L3-S7 §13 known-friction: the 19-field manifest was
// handwritten). The draft pre-fills every control-plane fact and marks the
// rest TODO(planner); `runtime register-workgroup` validates the result
// normally.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// runS7ManifestDraft generates the manifest draft. It performs no writes to
// the control plane; --out (or stdout) is the only output.
func runS7ManifestDraft(root, assignmentID, out string, stdout io.Writer) int {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		fmt.Fprintf(stdout, "read runtime: %v\n", err)
		return 1
	}
	draft, notes, err := review.DraftManifest(root, snapshot.State, assignmentID)
	if err != nil {
		fmt.Fprintf(stdout, "manifest draft: %v\n", err)
		return 1
	}
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		fmt.Fprintf(stdout, "encode draft: %v\n", err)
		return 1
	}
	if out != "" {
		if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
			fmt.Fprintf(stdout, "write draft: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "draft team manifest written to %s\n", out)
	} else {
		fmt.Fprintln(stdout, string(data))
	}
	for _, note := range notes {
		fmt.Fprintf(stdout, "note: %s\n", note)
	}
	fmt.Fprintf(stdout, "next: replace every TODO(planner) marker — especially `agent_id` with the real platform Agent identity — then dispatch with `runtime register-workgroup --manifest %s --task-id <TASK> --task <path>`; registration rejects authoring placeholders\n", out)
	return 0
}
