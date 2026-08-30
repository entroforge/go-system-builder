package cli

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// lifecycleSnapshot loads the runtime for a lifecycle verb command. The
// returned guidance string explains why the verb cannot run when the
// snapshot is unavailable.
func lifecycleSnapshot(root string) (runtime.Snapshot, string) {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).Snapshot()
	if err != nil {
		return runtime.Snapshot{}, "no readable runtime — bind a REQ first (req bind --approved-by <human identity>)"
	}
	return snapshot, ""
}

func snapshotLifecycle(snapshot runtime.Snapshot) (state string, phase any) {
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	state, _ = lifecycle["state"].(string)
	return state, lifecycle["phase"]
}

// writeDecisionArtifact persists the human decision record that backs the
// human_decision evidence for a lifecycle verb. The machine derives the
// envelope; the human supplies only the decision and identity.
func writeDecisionArtifact(root, name string, payload map[string]any) (string, error) {
	dir := filepath.Join(root, ".claude", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create decisions dir: %w", err)
	}
	payload["occurred_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	rel := filepath.ToSlash(filepath.Join(".claude", "decisions", name+".json"))
	if err := os.WriteFile(filepath.Join(root, rel), append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

// registerDecisionEvidence writes the decision artifact and records it as
// current human_decision evidence, returning the evidence id and the
// revision the follow-up transition must expect.
func registerDecisionEvidence(root string, snapshot runtime.Snapshot, id, decision, reason, approvedBy, scopeRef string) (string, int, error) {
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	payload := map[string]any{
		"decision":    decision,
		"reason":      reason,
		"approved_by": approvedBy,
		"runtime_id":  runtimeID,
		"revision":    snapshot.Revision,
		// the scope binds the NEXT revision — the one the follow-up
		// transition will apply at — so the audit artifact states it too.
		"authorization_revision": snapshot.Revision + 1,
	}
	if scopeRef != "" {
		payload["scope"] = scopeRef
	}
	rel, err := writeDecisionArtifact(root, id, payload)
	if err != nil {
		return "", 0, err
	}
	_, err = runtime.RecordEvidence(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		runtime.EvidenceRequest{
			ExpectedRevision: snapshot.Revision,
			ID:               id,
			Kind:             "human_decision",
			Path:             rel,
			ProducedBy:       []string{approvedBy},
			ScopeRefs:        []string{scopeRef},
			Validator:        semantic.RuntimeCandidateValidator{},
		})
	if err != nil {
		return "", 0, fmt.Errorf("register decision evidence: %w", err)
	}
	return id, snapshot.Revision + 1, nil
}

func runRuntimePause(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime pause", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime pause")
	root := flags.String("root", ".", "repository root")
	reason := flags.String("reason", "", "why the loop is paused (recorded in the human decision artifact)")
	approvedBy := flags.String("approved-by", "", "human approver identity")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *approvedBy == "" {
		if identity := detectGitIdentity(*root); identity != "" {
			fmt.Fprintf(stderr, "runtime pause requires --approved-by (detected git identity %q; rerun with --approved-by %q)\n", identity, identity)
		} else {
			fmt.Fprintln(stderr, "runtime pause requires --approved-by <human identity>")
		}
		return 2
	}
	snapshot, guidance := lifecycleSnapshot(*root)
	if guidance != "" {
		fmt.Fprintln(stderr, "runtime pause: "+guidance)
		return 1
	}
	state, phase := snapshotLifecycle(snapshot)
	switch state {
	case "paused":
		fmt.Fprintln(stderr, "runtime pause: already paused — resolve it with `runtime resume`, or amend/abort from the paused state")
		return 1
	case "release_authorized", "aborted":
		fmt.Fprintf(stderr, "runtime pause: runtime is terminal (%s) — use `runtime rollover` to archive and start fresh\n", state)
		return 1
	case "inactive":
		fmt.Fprintln(stderr, "runtime pause: nothing is bound — bind a REQ first (req bind --approved-by <human identity>)")
		return 1
	}
	evID, nextRev, err := registerDecisionEvidence(*root, snapshot,
		fmt.Sprintf("hd-pause-r%d", snapshot.Revision+1), "user_pause_requested", *reason, *approvedBy,
		fmt.Sprintf("runtime_pause:%s@%d", snapshot.State["runtime_id"], snapshot.Revision+1))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime pause", err))
		return 1
	}
	next, err := transition.Apply(*root,
		filepath.Join(*root, ".claude", "loop-state.json"),
		filepath.Join(*root, ".claude", "loop-events.jsonl"),
		transition.Request{
			TransitionID: "GTR-001", ExpectedRevision: nextRev, Actor: "user",
			Evidence: map[string]string{
				"human_decision_record": evID,
				"pause_record":          "generated:pause_checkpoint",
			},
			OccurredAt: time.Now().UTC(),
		})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime pause", err))
		return 1
	}
	_ = next
	fmt.Fprintf(stdout, "paused from %v at revision %d (reason recorded; approved-by %s)\n", cursorLabel(state, phase), next.Revision, *approvedBy)
	fmt.Fprintln(stdout, "resume: loop-harness runtime resume --approved-by <the same human>   (baseline drift on resume routes to amendment)")
	return 0
}

func runRuntimeResume(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime resume", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime resume")
	root := flags.String("root", ".", "repository root")
	approvedBy := flags.String("approved-by", "", "human approver identity")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *approvedBy == "" {
		if identity := detectGitIdentity(*root); identity != "" {
			fmt.Fprintf(stderr, "runtime resume requires --approved-by (detected git identity %q; rerun with --approved-by %q)\n", identity, identity)
		} else {
			fmt.Fprintln(stderr, "runtime resume requires --approved-by <human identity>")
		}
		return 2
	}
	snapshot, guidance := lifecycleSnapshot(*root)
	if guidance != "" {
		fmt.Fprintln(stderr, "runtime resume: "+guidance)
		return 1
	}
	state, _ := snapshotLifecycle(snapshot)
	if state != "paused" {
		fmt.Fprintf(stderr, "runtime resume: runtime is %q, not paused — nothing to resume\n", state)
		return 1
	}
	evID, nextRev, err := registerDecisionEvidence(*root, snapshot,
		fmt.Sprintf("hd-resume-r%d", snapshot.Revision+1), "human_resume_approved", "", *approvedBy,
		fmt.Sprintf("runtime_resume:%s@%d", snapshot.State["runtime_id"], snapshot.Revision+1))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime resume", err))
		return 1
	}
	next, err := transition.Apply(*root,
		filepath.Join(*root, ".claude", "loop-state.json"),
		filepath.Join(*root, ".claude", "loop-events.jsonl"),
		transition.Request{
			TransitionID: "TR-019", ExpectedRevision: nextRev, Actor: "user",
			Evidence: map[string]string{
				"human_decision_record": evID,
				"pause_record":          "generated:pause_checkpoint",
			},
			OccurredAt: time.Now().UTC(),
		})
	if err != nil {
		if errors.Is(err, transition.ErrBaselineDrift) {
			fmt.Fprintln(stderr, "runtime resume: baseline drifted while paused — resume is refused; amend the baseline instead (req amend)")
			return 1
		}
		fmt.Fprintln(stderr, formatFailure("runtime resume", err))
		return 1
	}
	fmt.Fprintf(stdout, "resumed to %v at revision %d (pause checkpoint verified and cleared)\n", cursorLabel(next.State["lifecycle"].(map[string]any)["state"].(string), next.State["lifecycle"].(map[string]any)["phase"]), next.Revision)
	return 0
}

func cursorLabel(state string, phase any) string {
	if phase == nil || phase == "" {
		return state
	}
	return fmt.Sprintf("%s.%v", state, phase)
}

// inFlightEntities lists task/team ids that are still open. Unbind refuses
// to strand them silently; the human can override with --force (a visible
// abandonment, recorded in the archive).
func inFlightEntities(state map[string]any) []string {
	var out []string
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task == nil {
			continue
		}
		if taskState, _ := task["state"].(string); taskState == "in_progress" || taskState == "review" || taskState == "blocked" {
			if id, _ := task["id"].(string); id != "" {
				out = append(out, "task "+id+" ("+taskState+")")
			}
		}
	}
	teams, _ := entities["teams"].([]any)
	for _, raw := range teams {
		team, _ := raw.(map[string]any)
		if team == nil {
			continue
		}
		// Teams are append-only history: only planned/active ones are really
		// in flight. Blocking on completed teams would train --force habit.
		status, _ := team["status"].(string)
		if status != "planned" && status != "active" {
			continue
		}
		if id, _ := team["id"].(string); id != "" {
			out = append(out, "team "+id+" ("+status+")")
		}
	}
	return out
}

func runREQUnbind(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("req unbind", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "req unbind")
	root := flags.String("root", ".", "repository root")
	archive := flags.String("archive-dir", ".claude/runtime-archive", "archive directory relative to repository root")
	approvedBy := flags.String("approved-by", "", "human approver identity")
	reason := flags.String("reason", "", "why the binding is revoked (recorded durably)")
	force := flags.Bool("force", false, "unbind even with in-flight tasks/teams (visible abandonment)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *approvedBy == "" {
		if identity := detectGitIdentity(*root); identity != "" {
			fmt.Fprintf(stderr, "req unbind requires --approved-by (detected git identity %q; rerun with --approved-by %q)\n", identity, identity)
		} else {
			fmt.Fprintln(stderr, "req unbind requires --approved-by <human identity>")
		}
		return 2
	}
	if strings.TrimSpace(*reason) == "" {
		fmt.Fprintln(stderr, "req unbind requires --reason <one sentence> — revocation without a recorded reason is not auditable")
		return 2
	}
	snapshot, guidance := lifecycleSnapshot(*root)
	if guidance != "" {
		fmt.Fprintln(stderr, "req unbind: "+guidance)
		return 1
	}
	state, phase := snapshotLifecycle(snapshot)
	switch state {
	case "release_authorized", "aborted":
		fmt.Fprintf(stderr, "req unbind: runtime is terminal (%s) — use `runtime rollover` to close the period\n", state)
		return 1
	case "inactive":
		fmt.Fprintln(stderr, "req unbind: nothing is bound")
		return 1
	}
	// paused is deliberately allowed: revoking from a paused checkpoint is a
	// legitimate "abandon" (L2 ruling: any non-terminal state), and the
	// archived runtime keeps the checkpoint as part of the audit trail —
	// especially valuable when resume is blocked by baseline drift.
	bound, _ := snapshot.State["bound_req"].(map[string]any)
	boundID, _ := bound["id"].(string)
	if boundID == "" {
		fmt.Fprintln(stderr, "req unbind: no bound REQ in runtime")
		return 1
	}
	if inFlight := inFlightEntities(snapshot.State); len(inFlight) > 0 && !*force {
		fmt.Fprintln(stderr, "req unbind: in-flight entities would be stranded:")
		for _, item := range inFlight {
			fmt.Fprintf(stderr, "  %s\n", item)
		}
		fmt.Fprintln(stderr, "wind them down first, or rerun with --force (the abandonment is recorded in the archive)")
		return 1
	}
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	evID, _, err := registerDecisionEvidence(*root, snapshot,
		fmt.Sprintf("hd-unbind-r%d", snapshot.Revision+1), "req_unbind_requested", *reason, *approvedBy,
		fmt.Sprintf("runtime_unbind:%s@%d", runtimeID, snapshot.Revision+1))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req unbind", err))
		return 1
	}
	now := time.Now().UTC()
	freshState, err := inactiveRuntimeState(*root, now)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req unbind", err))
		return 1
	}
	encoded, err := json.Marshal(freshState)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req unbind", fmt.Errorf("encode fresh runtime: %w", err)))
		return 1
	}
	if err := semantic.ValidateRuntimeBytes(*root, encoded); err != nil {
		fmt.Fprintln(stderr, formatFailure("req unbind", fmt.Errorf("validate fresh runtime: %w", err)))
		return 1
	}
	unbindApproval := runtime.UnbindApproval{ApprovedBy: *approvedBy, EvidenceID: evID, Reason: *reason, Forced: *force}
	unbindApproval.InFlight = inFlightEntities(snapshot.State)
	record, err := runtime.NewWriter(rootedPath(*root, ".claude/loop-state.json"), rootedPath(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{}).Unbind(
		freshState, rootedPath(*root, *archive), unbindApproval, now,
	)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req unbind", err))
		return 1
	}
	fmt.Fprintf(stdout, "unbound %s (was %s, revision %d; reason recorded; approved-by %s)\n", boundID, cursorLabel(state, phase), record.Revision, *approvedBy)
	fmt.Fprintf(stdout, "  archived runtime: %s (disposition=unbound)\n", record.ArchiveDir)
	fmt.Fprintf(stdout, "next: %s returned to the bindable pool — `req list` to pick the next target\n", boundID)
	return 0
}

func runREQAmend(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("req amend", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "req amend")
	root := flags.String("root", ".", "repository root")
	reqPath := flags.String("req", "", "amended locked REQ path (version must strictly exceed the bound one)")
	approvedBy := flags.String("approved-by", "", "human approver identity")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *approvedBy == "" {
		if identity := detectGitIdentity(*root); identity != "" {
			fmt.Fprintf(stderr, "req amend requires --approved-by (detected git identity %q; rerun with --approved-by %q)\n", identity, identity)
		} else {
			fmt.Fprintln(stderr, "req amend requires --approved-by <human identity>")
		}
		return 2
	}
	if *reqPath == "" {
		fmt.Fprintln(stderr, "req amend requires --req <path to the amended locked REQ> — the new baseline generation is a human-approved fact")
		return 2
	}
	snapshot, guidance := lifecycleSnapshot(*root)
	if guidance != "" {
		fmt.Fprintln(stderr, "req amend: "+guidance)
		return 1
	}
	state, _ := snapshotLifecycle(snapshot)
	if state != "paused" {
		fmt.Fprintf(stderr, "req amend: runtime is %q, not paused — pause it first (`runtime pause`) so the amendment starts from a checkpoint\n", state)
		return 1
	}
	bound, _ := snapshot.State["bound_req"].(map[string]any)
	boundID, _ := bound["id"].(string)
	if boundID == "" {
		fmt.Fprintln(stderr, "req amend: no bound REQ in runtime")
		return 1
	}
	data, err := os.ReadFile(filepath.Join(*root, *reqPath))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req amend", err))
		return 1
	}
	version := markdownField(string(data), "版本", "Version")
	status := markdownField(string(data), "状态", "Status")
	if status != "locked" || version == "" {
		fmt.Fprintln(stderr, "req amend: the amended REQ top blockquote must declare `状态：locked` (or `Status: locked`) and `版本：<semver>` — see docs/requirements/REQ-template.md")
		return 1
	}
	id := strings.TrimSuffix(filepath.Base(*reqPath), filepath.Ext(*reqPath))
	if !strings.HasPrefix(id, "REQ-") {
		fmt.Fprintln(stderr, "req amend: filename must start with REQ-")
		return 1
	}
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	evID, _, err := registerDecisionEvidence(*root, snapshot,
		fmt.Sprintf("hd-amend-r%d", snapshot.Revision+1), "req_amendment_approved", *reqPath, *approvedBy,
		fmt.Sprintf("runtime_amend:%s@%d", runtimeID, snapshot.Revision+1))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req amend", err))
		return 1
	}
	now := time.Now().UTC()
	shaHex := fmt.Sprintf("%x", sha256.Sum256(data))
	next, err := transition.Apply(*root,
		filepath.Join(*root, ".claude", "loop-state.json"),
		filepath.Join(*root, ".claude", "loop-events.jsonl"),
		transition.Request{
			TransitionID: "TR-020", ExpectedRevision: snapshot.Revision + 1, Actor: "user",
			Evidence: map[string]string{
				"human_decision_record": evID,
				"req_lock_record":       *reqPath + "@" + shaHex,
			},
			REQ: &transition.LockedREQ{
				ID: id, Path: *reqPath, Version: version, SHA256: shaHex,
				ApprovedBy: *approvedBy, ApprovedAt: now.Format(time.RFC3339Nano),
			},
			OccurredAt: now,
		})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req amend", err))
		return 1
	}
	baseline, _ := next.State["baseline"].(map[string]any)
	invalid := 0
	if items, ok := next.State["evidence"].([]any); ok {
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item != nil && item["status"] == "invalid" {
				invalid++
			}
		}
	}
	fmt.Fprintf(stdout, "amended: bound %s → %s %s (baseline generation %d, revision %d)\n", boundID, id, version, tolerantInt(baseline["generation"]), next.Revision)
	fmt.Fprintf(stdout, "  downstream evidence invalidated: %d item(s); old REQ stays locked (history)\n", invalid)
	fmt.Fprintf(stdout, "  superseded REQ file: keep %s where it is — hook write-protection is path-based, so moving the file would drop it out of protection (its fingerprint already lives in runtime history)\n", boundID)
	fmt.Fprintln(stdout, "next: continue from planning.design — the amendment already left the paused state (checkpoint cleared)")
	return 0
}
