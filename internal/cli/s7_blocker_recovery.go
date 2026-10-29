package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// The original site_lost receipt owns the blocker. Reading this projection
// neither creates a message nor changes the original responsibility.
type s7BlockerSource struct {
	assignmentID, owner, ref, reason string
	agent                            map[string]any
}

func s7OriginalBlocker(root string, state map[string]any, id string) (s7BlockerSource, error) {
	var source s7BlockerSource
	reviewState, _ := state["review"].(map[string]any)
	rows, _ := reviewState["assignments"].(map[string]any)
	row, _ := rows[id].(map[string]any)
	source.assignmentID, source.owner, source.ref = id, stringValue(row["agent_id"]), stringValue(row["blocker_ref"])
	if !assignment.ReviewRoundOpenForDispatch(state) {
		return source, fmt.Errorf("current S7 round is not open for recovery; follow its recorded pause or stage handoff before blocker_resolved")
	}
	if workstate.ReviewAssignmentActivity(row) != "blocked" || source.owner == "" || source.ref == "" {
		return source, fmt.Errorf("restore the original current S7 blocked Assignment, owner and blocker_ref before recording blocker_resolved")
	}
	matches := 0
	for _, raw := range rows {
		candidate, _ := raw.(map[string]any)
		if stringValue(candidate["agent_id"]) == source.owner && workstate.ReviewAssignmentActivity(candidate) == "blocked" {
			matches++
		}
	}
	if matches != 1 {
		return source, fmt.Errorf("owner %s has no unique original blocked responsibility", source.owner)
	}
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	matches = 0
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if stringValue(agent["id"]) == source.owner {
			source.agent = agent
			matches++
		}
	}
	if matches != 1 || stringValue(source.agent["work_blocked_ref"]) != source.ref {
		return source, fmt.Errorf("restore original registered owner and its work_blocked_ref")
	}
	data, err := workpacket.ReadFile(root, source.ref, 1<<20)
	if err != nil {
		return source, fmt.Errorf("read original blocker: %w", err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(data, &receipt); err != nil {
		return source, fmt.Errorf("decode original blocker: %w", err)
	}
	baseline, _ := state["baseline"].(map[string]any)
	if receipt["kind"] != "review_blocker" || receipt["runtime_id"] != state["runtime_id"] || tolerantInt(receipt["baseline_generation"]) != tolerantInt(baseline["generation"]) || tolerantInt(receipt["review_round"]) != tolerantInt(reviewState["round"]) || receipt["assignment_id"] != id || receipt["producer_agent_id"] != source.owner {
		return source, fmt.Errorf("original blocker does not match the current responsibility")
	}
	evidence, _ := state["evidence"].([]any)
	verified := false
	for _, raw := range evidence {
		entry, _ := raw.(map[string]any)
		if entry["path"] == source.ref && entry["kind"] == "review_blocker" && entry["status"] == "valid" && entry["sha256"] == workpacket.Digest(data) {
			verified = true
		}
	}
	if !verified {
		return source, fmt.Errorf("restore original blocker evidence digest/index")
	}
	source.reason = stringValue(receipt["reason"])
	if strings.TrimSpace(source.reason) == "" {
		return source, fmt.Errorf("original blocker has no recorded reason")
	}
	return source, nil
}

func s7BlockedAssignmentGuidance(root string, state map[string]any, id string) string {
	source, err := s7OriginalBlocker(root, state, id)
	lead := fmt.Sprintf("S7 assignment=%s Reviewer=%s blocked; blocker_ref=%s", id, source.owner, source.ref)
	if err != nil {
		return lead + "; Main restores original recovery source: " + err.Error() + ".\n"
	}
	resume := "Resume the same Reviewer and original ReviewResult answer; save its actual restored observations. PreToolUse receives the ready original answer, with successful PostToolUse and Stop/SubagentStop fallback. Report actual results to Main when finished."
	packets, packetErr := runtime.CurrentReviewResultPacketsContext(runtime.WithPacketBodyRoot(context.Background(), root), state, source.owner)
	if packetErr != nil {
		resume += " Original answer lookup: " + packetErr.Error() + "."
	}
	for _, packet := range packets {
		if packet.Binding.AssignmentID == id {
			resume += " Original answer: " + packet.AnswerPath + "; view: " + packet.ViewPath + "."
		}
	}
	if resolved := stringValue(source.agent["blocker_resolved_ref"]); resolved != "" && workstate.AgentActivity(source.agent) == "working" {
		if err := verifyS7ResolutionReceipt(root, state, source, resolved); err != nil {
			return lead + "; Main restores original blocker_resolved receipt: " + err.Error() + ".\n"
		}
		return lead + "; reason=" + source.reason + "; original blocker_resolved already recorded at " + resolved + ". " + resume + "\n"
	}
	prefix := s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime agent-event --root " + s7CaptureShellQuote(root) + " --event blocker_resolved --agent-id " + s7CaptureShellQuote(source.owner) + " --body"
	return lead + "; reason=" + source.reason + ". Main restores the actual capture/source conditions, then uses this incomplete command prefix: " + prefix + ". Supply one shell-quoted, real professional restoration explanation as the --body value; the original entry derives machine message facts and invokes its existing consumer. " + resume + "\n"
}

// --body is a local preparation adapter for the existing blocker_resolved
// event, not a second protocol or state transition. CAS and lifecycle validation
// stay in AdvanceAgent; retries reuse the immutable original message.
func advanceS7BlockerWithBody(root, statePath, journalPath string, request assignment.AgentEventRequest, body string) (runtime.Snapshot, error) {
	body = strings.TrimSpace(body)
	if request.Event != "blocker_resolved" || body == "" {
		return runtime.Snapshot{}, fmt.Errorf("--body requires S7 blocker_resolved and an actual professional restoration explanation")
	}
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return runtime.Snapshot{}, err
	}
	if request.ExpectedRevision >= 0 && request.ExpectedRevision != snapshot.Revision {
		return runtime.Snapshot{}, fmt.Errorf("expected revision %d differs from current %d", request.ExpectedRevision, snapshot.Revision)
	}
	reviewState, _ := snapshot.State["review"].(map[string]any)
	rows, _ := reviewState["assignments"].(map[string]any)
	id := ""
	for candidate, raw := range rows {
		row, _ := raw.(map[string]any)
		if stringValue(row["agent_id"]) == request.AgentID && workstate.ReviewAssignmentActivity(row) == "blocked" {
			if id != "" {
				return runtime.Snapshot{}, fmt.Errorf("--body requires a unique original blocked Assignment for %s", request.AgentID)
			}
			id = candidate
		}
	}
	source, err := s7OriginalBlocker(root, snapshot.State, id)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	if workstate.AgentActivity(source.agent) != "blocked" && !(workstate.AgentActivity(source.agent) == "working" && stringValue(source.agent["blocker_resolved_ref"]) != "") {
		return runtime.Snapshot{}, fmt.Errorf("original owner is not blocked or awaiting its already recorded recovery")
	}
	tasks := stringSliceAny(source.agent["task_ids"])
	definition := stringValue(source.agent["definition_ref"])
	if len(tasks) != 1 || definition == "" {
		return runtime.Snapshot{}, fmt.Errorf("restore the original owner's unique registered TASK and definition_ref before blocker_resolved")
	}
	baseline, _ := snapshot.State["baseline"].(map[string]any)
	runtimeID := stringValue(snapshot.State["runtime_id"])
	digest := workpacket.Digest([]byte(runtimeID + "\n" + source.ref + "\n" + body))
	relative := filepath.ToSlash(filepath.Join(".claude/evidence", runtimeID, fmt.Sprintf("g%d", tolerantInt(baseline["generation"])), "review-blockers", digest+"-resolution.json"))
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "blocker_resolution", "message_id": "msg-" + digest,
		"correlation_id": "corr-" + digest, "runtime_id": runtimeID, "agent_id": source.owner,
		"agent_definition_ref": definition, "task_id": tasks[0], "bug_id": nil, "team_id": source.agent["team_id"],
		"occurred_at": occurredAt.UTC().Format(time.RFC3339Nano), "body": body,
	}
	if resolved := stringValue(source.agent["blocker_resolved_ref"]); resolved != "" && workstate.AgentActivity(source.agent) == "working" {
		if filepath.Clean(resolveRootPath(root, resolved)) != filepath.Clean(resolveRootPath(root, relative)) {
			return runtime.Snapshot{}, fmt.Errorf("original blocker_resolved is already recorded at %s; resume the same Reviewer without another resolution", resolved)
		}
		data, err := workpacket.ReadFile(root, relative, 1<<20)
		if err != nil {
			return runtime.Snapshot{}, err
		}
		if err := verifyS7ResolutionMessage(data, message); err != nil {
			return runtime.Snapshot{}, err
		}
		if err := verifyS7ResolutionReceipt(root, snapshot.State, source, resolved); err != nil {
			return runtime.Snapshot{}, err
		}
		return snapshot, nil
	}
	data, err := json.MarshalIndent(message, "", "  ")
	if err != nil {
		return runtime.Snapshot{}, err
	}
	winner, err := workpacket.Publish(root, relative, append(data, '\n'))
	if err != nil {
		return runtime.Snapshot{}, err
	}
	if err := verifyS7ResolutionMessage(winner, message); err != nil {
		return runtime.Snapshot{}, err
	}
	request.MessagePath = resolveRootPath(root, relative)
	request.ExpectedRevision = snapshot.Revision
	return assignment.AdvanceAgent(root, statePath, journalPath, request)
}

func verifyS7ResolutionMessage(data []byte, expected map[string]any) error {
	var actual map[string]any
	if err := json.Unmarshal(data, &actual); err != nil {
		return err
	}
	for key, value := range expected {
		if key == "occurred_at" {
			continue
		}
		if actual[key] != value {
			return fmt.Errorf("preserve original blocker resolution: message %s differs", key)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, stringValue(actual["occurred_at"])); err != nil {
		return fmt.Errorf("original blocker resolution timestamp: %w", err)
	}
	return nil
}

// Acceptance is proved by the original journal event, never by a working
// Agent projection or an authored resolution file alone.
func verifyS7ResolutionReceipt(root string, state map[string]any, source s7BlockerSource, ref string) error {
	data, err := workpacket.ReadFile(root, ref, 1<<20)
	if err != nil {
		return err
	}
	if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data); err != nil {
		return err
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		return err
	}
	if message["runtime_id"] != state["runtime_id"] || message["agent_id"] != source.owner || message["message_type"] != "blocker_resolution" || message["agent_definition_ref"] != source.agent["definition_ref"] || !slices.Contains(stringSliceAny(source.agent["task_ids"]), stringValue(message["task_id"])) {
		return fmt.Errorf("resolution message differs from the original owner and Runtime")
	}
	journal, err := runtime.ReadJournalForState(root, state)
	if err != nil {
		return err
	}
	proof, err := controlproof.InspectJournalData(journal)
	if err != nil {
		return err
	}
	if err := controlproof.ValidateStateJournalPair(state, proof); err != nil {
		return err
	}
	baseline, _ := state["baseline"].(map[string]any)
	for _, event := range proof.Events {
		before, after := tolerantInt(event["before_revision"]), tolerantInt(event["after_revision"])
		actor, _ := event["actor"].(map[string]any)
		from, _ := event["from"].(map[string]any)
		if event["event_id"] != fmt.Sprintf("evt-agent-%s-blocker_resolved-r%d", source.owner, after) || event["idempotency_key"] != fmt.Sprintf("runtime:agent:%s:blocker_resolved:%d", source.owner, before) {
			continue
		}
		if event["runtime_id"] == state["runtime_id"] && event["event"] == "transition_committed" && event["outcome"] == "committed" && event["transition_id"] == "AGENT-LIFECYCLE" && tolerantInt(event["baseline_generation"]) == tolerantInt(baseline["generation"]) && after == before+1 && after <= tolerantInt(state["revision"]) && actor["type"] == "orchestrator" && actor["id"] == "orchestrator" && from["state"] == "verification" && event["message"] == "Recorded a schema-valid Agent lifecycle event (blocker_resolved)" && slices.Contains(stringSliceAny(event["evidence_ids"]), ref) {
			return nil
		}
	}
	return fmt.Errorf("no original AGENT-LIFECYCLE blocker_resolved event proves owner %s and resolution %s at the current revision", source.owner, ref)
}
