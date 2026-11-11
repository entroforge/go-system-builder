package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func s7BlockedAssignmentGuidance(root string, state map[string]any, id string) string {
	reviewState, _ := state["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	row, _ := assignments[id].(map[string]any)
	owner := stringValue(row["agent_id"])
	lead := fmt.Sprintf("S7 assignment=%s Reviewer=%s: this responsibility is unfinished. Main coordinates the actual obstacle; the Reviewer can naturally continue and save new observations in the same answer. No separate recovery submission is required.", id, owner)
	packets, err := runtime.CurrentReviewResultPacketsContext(runtime.WithPacketBodyRoot(context.Background(), root), state, owner)
	if err != nil {
		return lead + " Original answer lookup: " + err.Error() + ".\n"
	}
	for _, packet := range packets {
		if packet.Binding.AssignmentID == id {
			return lead + " Original answer: " + packet.AnswerPath + "; view: " + packet.ViewPath + ".\n"
		}
	}
	return lead + "\n"
}

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

// Main can copy this complete responsibility into the native task.
func s7ReviewerTaskGuidance(root, source, view, answer string, packet workpacket.ReviewResultPacket, full bool) string {
	return s7ReviewerIndexTaskGuidance(root, source, view, answer, runtime.ReviewResultPacketIndex{ID: packet.ID, Binding: packet.Binding, ViewPath: packet.ViewPath, AnswerPath: packet.AnswerPath}, full)
}

func s7ReviewerIndexTaskGuidance(root, source, view, answer string, packet runtime.ReviewResultPacketIndex, full bool) string {
	id := packet.Binding.AssignmentID
	if !full {
		return fmt.Sprintf("S7 Reviewer %s assignment=%s: Main carries exact view %s and answer %s into native Agent; read its selected method references and independently inspect its frozen subjects/Claims, save the professional answer. PreToolUse receives ready original answers, with successful PostToolUse and Stop/SubagentStop fallback; report results to Main when finished. Main restores real source/environment blockers.\n", packet.Binding.OwnerAgentID, id, view, answer)
	}
	return fmt.Sprintf("S7 Reviewer %s assignment=%s: Main carries this task into native Agent. Read exact view %s, its Claims and selected professional methods; independently perform the assigned checks. Fill the professional answer at %s, preserve actual observations and evidence, and save it. PreToolUse observes the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. Report results to Main when finished. No extra submit, returned reference, done or activation is required; ordinary tools remain available. Report actual blockers without inventing conclusions; stop dangerous work for a real P0 and preserve observed gaps.\n", packet.Binding.OwnerAgentID, id, view, answer)
}

func s7ManifestPreparationHint(root string, state map[string]any, id string) string {
	reviewState, _ := state["review"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	generation := tolerantInt(baseline["generation"])
	identity := reviewmeta.FromReview(reviewState)
	if generation < 1 || identity.PlanID == "" || id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		return fmt.Sprintf("S7 assignment=%s: Main/system must restore the current ReviewPlan identity, generation and Assignment before manifest preparation.\n", id)
	}
	directory := s7ManifestIdentityDir(filepath.Join(root, ".claude/workgroups/plans"), identity.PlanID)
	directory = s7ManifestIdentityDir(filepath.Join(directory, "assignments"), id)
	manifest := filepath.Join(directory, "manifest.json")
	for suffix := 2; ; suffix++ {
		if _, err := os.Lstat(manifest); os.IsNotExist(err) {
			break
		} else if err != nil {
			return fmt.Sprintf("S7 assignment=%s: Main/system must restore manifest access at %s: %v.\n", id, manifest, err)
		}
		if draft, err := readS7ExistingManifest(root, manifest, state, id); err == nil {
			return s7ExistingManifestGuidance(root, id, manifest, draft)
		}
		// Preserve files without a known same-responsibility association and
		// offer another free path; they are not a gate on this Assignment.
		manifest = filepath.Join(directory, fmt.Sprintf("manifest-%d.json", suffix))
	}
	// This Assignment's historical registered layout is located by the
	// registered generation and review round. The file name only locates the
	// material: it never qualifies or rejects it, and an unknown round is
	// never written as a fixed placeholder.
	if registered := s7RegisteredManifestPath(root, generation, reviewState, id); registered != "" {
		if draft, err := readS7ExistingManifest(root, registered, state, id); err == nil {
			return s7ExistingManifestGuidance(root, id, registered, draft)
		}
	}
	command := s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " s7 manifest-draft --root " + s7CaptureShellQuote(root) + " --assignment " + s7CaptureShellQuote(id) + " --out " + s7CaptureShellQuote(manifest)
	return fmt.Sprintf("S7 assignment=%s: prepare the manifest with %s; this preparation creates a missing output directory and preserves existing files. Read the saved JSON file; stdout also contains guidance and is not a JSON document. Main chooses the real Reviewer and professional division of work, then uses the returned registration command. After registration, carry the issued exact view/answer paths into native Agent.\n", id, command) + s7ManifestRegistrationShape(root, manifest)
}

// s7RegisteredManifestPath locates the historical registered manifest layout
// of this Assignment. The generation and round in the file name are a
// locating convention only; they never qualify or reject the material.
func s7RegisteredManifestPath(root string, generation int, reviewState map[string]any, id string) string {
	round := tolerantInt(reviewState["round"])
	if round < 1 {
		if plan, _ := reviewState["plan"].(map[string]any); plan != nil {
			round = tolerantInt(plan["review_round"])
		}
	}
	if round < 1 {
		return ""
	}
	return filepath.Join(root, ".claude", "workgroups", fmt.Sprintf("manifest-g%d-r%d-%s.json", generation, round, id))
}

// s7ExistingManifestGuidance preserves the professional edits of a matched
// manifest and hands Main the original registration entry for it.
func s7ExistingManifestGuidance(root, id, manifest string, draft *review.ManifestDraft) string {
	return fmt.Sprintf("S7 assignment=%s: preserve and read existing manifest %s. Main retains its actual Reviewer and professional division of work.\n", id, manifest) + s7ManifestRegistrationGuidance(root, manifest, draft)
}

// s7ManifestRegistrationShape states the original registration entry for a
// manifest that does not exist yet. The TASK ID, the actual REQ/TASK path and
// its business version come from this Assignment's registered TASK document;
// no round, digest or extra author field is required.
func s7ManifestRegistrationShape(root, manifest string) string {
	return fmt.Sprintf("Main registers the original workgroup with %s runtime register-workgroup --root %s --manifest %s --task-id TASK-ID --task TASK-PATH; Main takes the TASK ID, the actual REQ/TASK path and its business version from this Assignment's registered TASK document in the prepared manifest, and the original registration entry rejects remaining professional placeholders.\n",
		s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")), s7CaptureShellQuote(root), s7CaptureShellQuote(manifest))
}

// Split the reversible identity into bounded filesystem components. Segment
// boundaries are fixed, so different original identities cannot collide.
func s7ManifestIdentityDir(parent, identity string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(identity))
	for len(encoded) > 100 {
		parent = filepath.Join(parent, encoded[:100])
		encoded = encoded[100:]
	}
	return filepath.Join(parent, encoded)
}
