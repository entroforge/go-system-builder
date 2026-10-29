package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// s7PendingReviewGuidance projects the same prerequisites as registration.
// It only reads original facts/files; preparation remains an explicit Main
// action and never overwrites professional work.
func s7PendingReviewGuidance(root string, state map[string]any) string {
	if !assignment.ReviewRoundOpenForDispatch(state) {
		return "S7 review dispatch is not open in the current stage/round; follow the current handoff or recorded pause, not the old queue.\n"
	}
	reviewState, _ := state["review"].(map[string]any)
	rows, _ := reviewState["assignments"].(map[string]any)
	var ids, blocked []string
	for id, raw := range rows {
		row, _ := raw.(map[string]any)
		switch workstate.ReviewAssignmentActivity(row) {
		case "planned":
			ids = append(ids, id)
		case "blocked":
			blocked = append(blocked, id)
		}
	}
	var b strings.Builder
	sort.Strings(blocked)
	for _, id := range blocked {
		b.WriteString(s7BlockedAssignmentGuidance(root, state, id))
	}
	if len(ids) == 0 {
		return b.String()
	}
	sort.Strings(ids)
	plan, _, err := review.LoadPlan(root, state)
	if err != nil {
		return b.String() + "S7 Main restores the original ReviewPlan source before selecting dispatch work: " + err.Error() + "\n"
	}
	for _, id := range ids {
		locks, err := assignment.ReviewDispatchPrerequisite(state, plan, id)
		if err != nil {
			fmt.Fprintf(&b, "S7 assignment=%s waits: %s\n", id, err)
			continue
		}
		if locks != "" {
			fmt.Fprintf(&b, "S7 assignment=%s waits for resource locks %s; preserve its coverage and original queue.\n", id, locks)
			continue
		}
		row, _ := rows[id].(map[string]any)
		if queued := stringValue(row["queued_agent_id"]); queued != "" {
			fmt.Fprintf(&b, "S7 assignment=%s has registered Reviewer %s; Main restores its original queued dispatch now that prerequisites are settled, without creating another workgroup.\n", id, queued)
			continue
		}
		b.WriteString(s7ManifestPreparationHint(root, state, id))
	}
	return b.String()
}

// Main can copy this complete responsibility into the native task. The
// optional capture prefixes use the same source as the issued answer, so a
// new native Worker need not enter the legacy activation listener to find them.
func s7ReviewerTaskGuidance(root, source, view, answer string, packet workpacket.ReviewResultPacket, full bool) string {
	return s7ReviewerIndexTaskGuidance(root, source, view, answer, runtime.ReviewResultPacketIndex{ID: packet.ID, Binding: packet.Binding, ViewPath: packet.ViewPath, AnswerPath: packet.AnswerPath}, full)
}

func s7ReviewerIndexTaskGuidance(root, source, view, answer string, packet runtime.ReviewResultPacketIndex, full bool) string {
	id := packet.Binding.AssignmentID
	if !full {
		return fmt.Sprintf("S7 Reviewer %s assignment=%s: Main carries exact view %s and answer %s into native Agent; read its selected method references and independently inspect its frozen subjects/Claims, save the professional answer. PreToolUse receives ready original answers, with successful PostToolUse and Stop/SubagentStop fallback; report results to Main when finished. Main restores real source/environment blockers.\n", packet.Binding.OwnerAgentID, id, view, answer)
	}
	executable := s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness"))
	execPrefix := executable + " capture exec --root " + s7CaptureShellQuote(root) + " --assignment " + s7CaptureShellQuote(id) + " --cwd " + s7CaptureShellQuote(source) + " --"
	stepPrefix := executable + " capture step --root " + s7CaptureShellQuote(root) + " --assignment " + s7CaptureShellQuote(id)
	buffer := review.CaptureFile(root, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration, id)
	return fmt.Sprintf("S7 Reviewer %s assignment=%s: Main carries this task into native Agent. Read exact view %s, its frozen subjects/Claims and the selected professional methods in its read list; independently perform the assigned lens checks. Fill only the professional answer at %s, preserve actual observations/evidence, save the ready original answer; report results to Main using that reference when finished; PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required; reception does not wait for a final message or returned reference. Ordinary tools need no activation or receipt. Correct specific author-field errors in the same answer; report actual source/environment blockers to Main without inventing conclusions. Ordinary Findings retain safe remaining coverage; a real P0 stops dangerous work and retains capture gaps. Optional capture for command_output evidence: %s followed by the actual check command/arguments. Optional observed steps: %s followed by --action/--observed and actual --evidence, --claim or --finding as applicable. Prefixes alone are incomplete; genuine path: evidence also remains valid. Original capture buffer: %s. Frozen registered sources retain their original recovery.\n", packet.Binding.OwnerAgentID, id, view, answer, execPrefix, stepPrefix, buffer)
}

func s7ManifestPreparationHint(root string, state map[string]any, id string) string {
	reviewState, _ := state["review"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	round, generation := tolerantInt(reviewState["round"]), tolerantInt(baseline["generation"])
	if generation < 1 || round < 1 || id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		return fmt.Sprintf("S7 assignment=%s: Main/system must restore the current ReviewPlan generation, round and Assignment before manifest preparation.\n", id)
	}
	manifest := filepath.Join(root, ".claude/workgroups", fmt.Sprintf("manifest-g%d-r%d-%s.json", generation, round, id))
	if _, err := os.Lstat(manifest); err == nil {
		draft, err := readS7ExistingManifest(manifest, state, id)
		if err != nil {
			return fmt.Sprintf("S7 assignment=%s: preserve existing manifest %s; restore its original source: %v.\n", id, manifest, err)
		}
		return fmt.Sprintf("S7 assignment=%s: preserve and read existing manifest %s. Main retains its actual Reviewer and professional division of work.\n", id, manifest) + s7ManifestRegistrationGuidance(root, manifest, draft)
	} else if !os.IsNotExist(err) {
		return fmt.Sprintf("S7 assignment=%s: Main/system must restore manifest access at %s: %v.\n", id, manifest, err)
	}
	if info, err := os.Stat(filepath.Dir(manifest)); err != nil && !os.IsNotExist(err) {
		return fmt.Sprintf("S7 assignment=%s: Main/system must restore workgroup directory %s: %v.\n", id, filepath.Dir(manifest), err)
	} else if err == nil && !info.IsDir() {
		return fmt.Sprintf("S7 assignment=%s: Main/system must restore %s: the workgroup path is not a directory.\n", id, filepath.Dir(manifest))
	}
	command := s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " s7 manifest-draft --root " + s7CaptureShellQuote(root) + " --assignment " + s7CaptureShellQuote(id) + " --out " + s7CaptureShellQuote(manifest)
	return fmt.Sprintf("S7 assignment=%s: prepare the manifest with %s; this preparation creates a missing output directory and preserves existing files. Read the saved JSON file; stdout also contains guidance and is not a JSON document. Main chooses the real Reviewer and professional division of work, then uses the returned registration command. After registration, carry the issued exact view/answer paths into native Agent.\n", id, command)
}
