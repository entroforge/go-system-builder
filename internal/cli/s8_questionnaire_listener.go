package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// The listener projects the original current Case and registry. Issuance,
// submission, explicit receipts and recovery retain their complete proofs.
func readS8ListenerCase(root string, state map[string]any) (map[string]any, error) {
	pointer := investigationPointer(state)
	if pointer == nil {
		return nil, nil
	}
	ref, sha := stringValue(pointer["path"]), stringValue(pointer["sha256"])
	if ref == "" || sha == "" {
		return nil, fmt.Errorf("current Case path or SHA is unavailable")
	}
	data, err := workpacket.ReadFile(root, ref, 20<<20)
	if err != nil {
		return nil, err
	}
	if workpacket.Digest(data) != sha {
		return nil, fmt.Errorf("current Case %s differs from its recorded SHA", ref)
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if document["case_id"] != pointer["case_id"] || tolerantInt(document["revision"]) != tolerantInt(pointer["revision"]) || tolerantInt(pointer["revision"]) < 1 {
		return nil, fmt.Errorf("current Case identity/revision differs from its recorded pointer")
	}
	return document, nil
}

func s8ListenerPackets(state, document map[string]any, owner string) ([]workpacket.HypothesisResultPacket, error) {
	entities, _ := state["entities"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	review, _ := state["review"].(map[string]any)
	var packets []workpacket.HypothesisResultPacket
	seen := map[string]bool{}
	for _, row := range objectSliceAny(entities["work_packets"]) {
		if row["template_id"] != "s8-hypothesis-result-v1" {
			continue
		}
		binding, _ := row["binding"].(map[string]any)
		if owner != "" && binding["owner_agent_id"] != owner || binding["runtime_id"] != state["runtime_id"] ||
			tolerantInt(binding["baseline_generation"]) != tolerantInt(baseline["generation"]) ||
			tolerantInt(binding["review_round"]) != tolerantInt(review["round"]) || document == nil || binding["case_id"] != document["case_id"] {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		var packet workpacket.HypothesisResultPacket
		if err := json.Unmarshal(data, &packet); err != nil {
			return nil, err
		}
		if _, accepted := s8ListenerReceipt(document, packet); !accepted {
			current := false
			for _, question := range objectSliceAny(document["hypotheses"]) {
				if question["hypothesis_id"] == packet.Binding.HypothesisID && question["assignment_id"] == packet.Binding.AssignmentID {
					current = true
				}
			}
			if !current {
				continue
			}
		}
		slot := packet.Binding.OwnerAgentID + "/" + packet.Binding.AssignmentID
		if seen[slot] || packet.ID == "" || packet.AnswerPath == "" || packet.ViewPath == "" {
			return nil, fmt.Errorf("current S8 questionnaire identity is missing or ambiguous")
		}
		seen[slot] = true
		packets = append(packets, packet)
	}
	return packets, nil
}

func s8ListenerReceipt(document map[string]any, packet workpacket.HypothesisResultPacket) (runtime.HypothesisResultReceipt, bool) {
	for _, row := range objectSliceAny(document["hypothesis_results"]) {
		raw, _ := row["receipt"].(map[string]any)
		if raw["packet_id"] != packet.ID {
			continue
		}
		data, err := json.Marshal(raw)
		if err != nil {
			return runtime.HypothesisResultReceipt{}, false
		}
		var receipt runtime.HypothesisResultReceipt
		if json.Unmarshal(data, &receipt) != nil {
			return receipt, false
		}
		b := packet.Binding
		return receipt, receipt.Contract == "s8-hypothesis-result-receipt-v1" && receipt.PacketID == packet.ID &&
			receipt.RuntimeID == b.RuntimeID && receipt.BaselineGeneration == b.BaselineGeneration && receipt.ReviewRound == b.ReviewRound &&
			receipt.OwnerAgentID == b.OwnerAgentID && receipt.AssignmentID == b.AssignmentID && receipt.TeamID == b.TeamID && receipt.TaskID == b.TaskID &&
			receipt.SuccessorCaseRef != "" && receipt.SuccessorCaseRevision > b.CaseRevision && len(receipt.AnswerSHA256) == 64 && receipt.ExecutionRoot != ""
	}
	return runtime.HypothesisResultReceipt{}, false
}

func s8HypothesisQuestionnaireContext(ctx context.Context, root string, request policy.Input, service *submission.Service, snapshot runtime.Snapshot, owner string) string {
	main := owner == "" || owner == "Main"
	if !main && !s8RegisteredInvestigator(snapshot.State, owner, "") {
		return ""
	}
	document, err := readS8ListenerCase(root, snapshot.State)
	if err != nil {
		return "S8 questionnaire lookup deferred; " + s8ListenerDiagnosticPaths(root, snapshot.State, filterS8ListenerOwner(owner)) + ". Main must restore the recorded Case source: " + err.Error()
	}
	filterOwner := owner
	if main {
		filterOwner = ""
	}
	packets, err := s8ListenerPackets(snapshot.State, document, filterOwner)
	if err != nil {
		return "S8 questionnaire lookup deferred; " + s8ListenerDiagnosticPaths(root, snapshot.State, filterOwner) + ". Main must inspect the original registry: " + err.Error()
	}
	// A relevant Main Pre prepares only a missing current question/view. Entry,
	// recovery and close-out reminders never invoke a mutation or generic recovery.
	if main && request.Event == "PreToolUse" && s8QuestionnairesNeedPreparation(root, snapshot.State, document, packets) {
		cwd, cwdErr := workspace.Canonical(request.CWD)
		mainRoot, rootErr := workspace.Canonical(root)
		if request.CWD == "" || cwdErr != nil || rootErr != nil || cwd != mainRoot {
			return "S8 questionnaire preparation is pending; " + s8ListenerDiagnosticPaths(root, snapshot.State, "") + ". Main must continue from the registered project root."
		}
		if service == nil {
			service, err = submission.New(root)
		}
		if err == nil {
			_, err = service.IssueHypothesisResults(ctx, "")
		}
		if err != nil {
			return "S8 questionnaire preparation is pending; " + s8ListenerDiagnosticPaths(root, snapshot.State, "") + ". Main owns the original issuance: " + err.Error()
		}
		snapshot, err = runtime.NewStore(service.StatePath, service.JournalPath).Snapshot()
		if err == nil {
			document, err = readS8ListenerCase(root, snapshot.State)
		}
		if err == nil {
			packets, err = s8ListenerPackets(snapshot.State, document, "")
		}
		if err != nil {
			return "S8 questionnaire projection is pending after original issuance; " + s8ListenerDiagnosticPaths(root, snapshot.State, "") + ". Main must inspect the recorded source: " + err.Error()
		}
	}
	var lines []string
	for _, packet := range packets {
		if !s8RegisteredInvestigator(snapshot.State, packet.Binding.OwnerAgentID, packet.Binding.AssignmentID) {
			continue
		}
		receipt, accepted := s8ListenerReceipt(document, packet)
		view, answer, pathErr := hypothesisResultLifecyclePaths(ctx, root, snapshot.State, packet, request.CWD, request.SessionID)
		if accepted {
			// The original receipt retains the actual answer source after a
			// Worker ends; no live execution or authoring file is required.
			answer = filepath.Join(receipt.ExecutionRoot, filepath.FromSlash(packet.AnswerPath))
			lines = append(lines, fmt.Sprintf("S8 Investigator %s assignment=%s: questionnaire: %s. The current Case records your HypothesisResult accepted; work_complete=true. Main owns remaining Case results, routing and approval; do not repeat this answer.", packet.Binding.OwnerAgentID, packet.Binding.AssignmentID, answer))
			continue
		}
		if pathErr != nil {
			lines = append(lines, fmt.Sprintf("S8 Investigator %s assignment=%s: %s. Questionnaire source is not ready; Main must finish the original registered dispatch: %v", packet.Binding.OwnerAgentID, packet.Binding.AssignmentID, s8ListenerDiagnosticPaths(root, snapshot.State, packet.Binding.OwnerAgentID), pathErr))
			continue
		}
		missing := ""
		if !s8QuestionnaireFileExists(view) || !s8QuestionnaireFileExists(answer) {
			missing = " Questionnaire preparation is pending; the next Main tool listener must restore this original view/answer without replacing professional work."
		}
		if main {
			lines = append(lines, fmt.Sprintf("S8 Investigator %s assignment=%s: Main carries exact question %s and private questionnaire answer %s into native Agent. Investigator performs the actual discriminator without product writes, saves the original HypothesisResult and returns its reference; SubagentStop invokes the original Case consumer. No opaque ID lookup, workspace launcher or generic activation is required.%s", packet.Binding.OwnerAgentID, packet.Binding.AssignmentID, view, answer, missing))
		} else {
			lines = append(lines, fmt.Sprintf("S8 Investigator %s assignment=%s: read the exact issued question %s; questionnaire: %s. Complete the professional HypothesisResult and save it through the original Case consumer.%s", packet.Binding.OwnerAgentID, packet.Binding.AssignmentID, view, answer, missing))
		}
	}
	if !main && len(lines) == 0 {
		return fmt.Sprintf("S8 Investigator %s has no current prepared questionnaire. Main owns the original Case dispatch and questionnaire preparation; do not guess an answer path.", owner)
	}
	return strings.Join(lines, "\n")
}

func s8RegisteredInvestigator(state map[string]any, owner, assignment string) bool {
	entities, _ := state["entities"].(map[string]any)
	for _, agent := range objectSliceAny(entities["agents"]) {
		if agent["id"] != owner || agent["role"] != "investigator" {
			continue
		}
		_, registered, found := strings.Cut(stringValue(agent["prompt_ref"]), "#")
		return found && registered != "" && (assignment == "" || registered == assignment)
	}
	return false
}

func s8QuestionnairesNeedPreparation(root string, state, document map[string]any, packets []workpacket.HypothesisResultPacket) bool {
	if document == nil || document["status"] != "investigating" || stringValue(investigationPointer(state)["route_consumed_at"]) != "" {
		return false
	}
	for _, packet := range packets {
		if _, accepted := s8ListenerReceipt(document, packet); accepted {
			continue
		}
		source, err := questionnaireSourceRoot(root, state, packet.Binding.AssignmentID, packet.Binding.OwnerAgentID, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration)
		if err == nil && (!s8QuestionnaireFileExists(filepath.Join(source, filepath.FromSlash(packet.ViewPath))) || !s8QuestionnaireFileExists(filepath.Join(source, filepath.FromSlash(packet.AnswerPath)))) {
			return true
		}
	}
	entities, _ := state["entities"].(map[string]any)
	for _, question := range objectSliceAny(document["hypotheses"]) {
		if question["status"] != "open" {
			continue
		}
		assignment := stringValue(question["assignment_id"])
		for _, agent := range objectSliceAny(entities["agents"]) {
			owner := stringValue(agent["id"])
			activity := workstate.AgentActivity(agent)
			if assignment == "" || (activity != "reading" && activity != "working" && activity != "blocked") || !s8RegisteredInvestigator(state, owner, assignment) {
				continue
			}
			found := false
			for _, packet := range packets {
				if packet.Binding.OwnerAgentID == owner && packet.Binding.AssignmentID == assignment {
					found = true
				}
			}
			if !found {
				return true
			}
		}
	}
	return false
}

func s8QuestionnaireFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func filterS8ListenerOwner(owner string) string {
	if owner == "Main" {
		return ""
	}
	return owner
}

// These paths are diagnostics from the original registry, not a fresh grant
// or evidence of acceptance. They remain useful when a Case cannot be read.
func s8ListenerDiagnosticPaths(root string, state map[string]any, owner string) string {
	var paths []string
	if owner == "" {
		paths = append(paths, investigationLeadQuestionnairePaths(root, state)...)
	}
	entities, _ := state["entities"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	review, _ := state["review"].(map[string]any)
	pointer := investigationPointer(state)
	for _, row := range objectSliceAny(entities["work_packets"]) {
		binding, _ := row["binding"].(map[string]any)
		if row["template_id"] != "s8-hypothesis-result-v1" || owner != "" && binding["owner_agent_id"] != owner ||
			binding["runtime_id"] != state["runtime_id"] || tolerantInt(binding["baseline_generation"]) != tolerantInt(baseline["generation"]) ||
			tolerantInt(binding["review_round"]) != tolerantInt(review["round"]) || pointer == nil || binding["case_id"] != pointer["case_id"] {
			continue
		}
		answer := stringValue(row["answer_path"])
		if answer == "" {
			continue
		}
		source, err := questionnaireSourceRoot(root, state, stringValue(binding["assignment_id"]), stringValue(binding["owner_agent_id"]), stringValue(binding["runtime_id"]), tolerantInt(binding["baseline_generation"]))
		if err != nil {
			registry, decodeErr := workspace.Decode(state)
			if decodeErr != nil || registry == nil {
				continue
			}
			execution, exists := registry.Executions[stringValue(binding["assignment_id"])]
			if !exists || execution.AgentID != binding["owner_agent_id"] || execution.RuntimeID != binding["runtime_id"] || execution.BaselineGeneration != tolerantInt(binding["baseline_generation"]) || !filepath.IsAbs(execution.Path) {
				continue
			}
			source = execution.Path
		}
		paths = append(paths, filepath.Join(source, filepath.FromSlash(answer)))
	}
	if len(paths) == 0 {
		return "the current exact questionnaire path is not prepared; Main owns the original Case dispatch and preparation gap"
	}
	return "questionnaire: " + strings.Join(paths, "; ")
}
