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
	"strings"

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
	if out != "" {
		if _, statErr := os.Lstat(out); statErr == nil {
			draft, readErr := readS7ExistingManifest(out, snapshot.State, assignmentID)
			if readErr != nil {
				fmt.Fprintf(stdout, "preserve existing manifest %s; restore its original source: %v\n", out, readErr)
				return 1
			}
			fmt.Fprintf(stdout, "preserved existing manifest %s; read and retain its professional edits.\n%s", out, s7ManifestRegistrationGuidance(root, out, draft))
			return 0
		} else if !os.IsNotExist(statErr) {
			fmt.Fprintf(stdout, "read manifest output: %v\n", statErr)
			return 1
		}
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
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			fmt.Fprintf(stdout, "prepare manifest directory: %v\n", err)
			return 1
		}
		file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			fmt.Fprintf(stdout, "preserve existing manifest or restore its output path %s: %v\n", out, err)
			return 1
		}
		_, writeErr := file.Write(append(data, '\n'))
		closeErr := file.Close()
		if writeErr != nil {
			fmt.Fprintf(stdout, "write draft: %v\n", writeErr)
			return 1
		}
		if closeErr != nil {
			fmt.Fprintf(stdout, "close draft: %v\n", closeErr)
			return 1
		}
		fmt.Fprintf(stdout, "draft team manifest written to %s\n", out)
	} else {
		fmt.Fprintln(stdout, string(data))
	}
	for _, note := range notes {
		fmt.Fprintf(stdout, "note: %s\n", note)
	}
	fmt.Fprintln(stdout, "next: the original Assignment supplies engineering agent_id/group labels, source metadata and default methods. Main retains actual professional division or replacement choices; no opaque platform identity lookup is required. registration rejects remaining professional placeholders. validation is preparation metadata, not evidence of a professional PASS.")
	if out == "" {
		fmt.Fprintln(stdout, "preparation gap: save the manifest with --out before selecting a registration command.")
		return 0
	}
	fmt.Fprint(stdout, s7ManifestRegistrationGuidance(root, out, draft))
	return 0
}

func readS7ExistingManifest(path string, state map[string]any, assignmentID string) (*review.ManifestDraft, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("manifest must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var draft review.ManifestDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		return nil, err
	}
	baseline, _ := state["baseline"].(map[string]any)
	reviewState, _ := state["review"].(map[string]any)
	if draft.RuntimeID != stringValue(state["runtime_id"]) || draft.BaselineGeneration != tolerantInt(baseline["generation"]) || draft.ReviewRound != tolerantInt(reviewState["round"]) {
		return nil, fmt.Errorf("manifest does not belong to the current Runtime, generation and review round")
	}
	count := 0
	for _, row := range draft.Assignments {
		if row.AssignmentID == assignmentID {
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("manifest must bind original Assignment %s exactly once", assignmentID)
	}
	return &draft, nil
}

func s7ManifestRegistrationGuidance(root, out string, draft *review.ManifestDraft) string {
	var stdout strings.Builder
	rootAbs, rootErr := filepath.Abs(root)
	manifestAbs, manifestErr := filepath.Abs(out)
	if rootErr != nil || manifestErr != nil {
		fmt.Fprintln(&stdout, "preparation gap: cannot resolve the actual root or saved manifest path.")
		return stdout.String()
	}
	candidates := 0
	for _, doc := range draft.Documents {
		if !strings.HasPrefix(doc.ID, "TASK-") || doc.Path == "" {
			continue
		}
		candidates++
		fmt.Fprintf(&stdout, "Main TASK choice %s (%s), after completing the draft: %s runtime register-workgroup --root %s --manifest %s --task-id %s --task %s\n", doc.ID, doc.Path, s7CaptureShellQuote(filepath.Join(rootAbs, ".claude/bin/loop-harness")), s7CaptureShellQuote(rootAbs), s7CaptureShellQuote(manifestAbs), s7CaptureShellQuote(doc.ID), s7CaptureShellQuote(doc.Path))
	}
	if candidates == 0 {
		fmt.Fprintln(&stdout, "preparation gap: no current-generation registered TASK document with an ID/path; Main must restore the TASK source before registration.")
	} else {
		fmt.Fprintln(&stdout, "After original registration, Main obtains the issued exact view/answer references and carries both into the native Agent task: read the frozen subjects and assigned Claims; perform independent checks; save only the professional answer at its supplied path and return that exact reference. PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required; reception does not wait for a final message or returned reference. Preserve actual evidence and report a real source/environment blocker to Main; do not invent conclusions or add a generic activation/report handshake.")
	}
	return stdout.String()
}
