package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

var bindingQuestions = []string{
	"Which requirement is selected?",
	"Who actually authorized this binding?",
	"Which development branch was selected?",
	"What is the explicit release destination?",
}

// Only directory names identify the existing Runtime boundary. Historical
// state, journal and hash proofs are not read to locate the current request.
func bindingQuestionnairePath(root string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".claude", "runtime-archive"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	latest, stamp := "", ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		i := strings.LastIndex(name, "-")
		if i < 0 {
			continue
		}
		candidate := name[i+1:]
		if _, err := time.Parse("20060102T150405.000000000Z", candidate); err != nil {
			continue
		}
		if candidate > stamp {
			latest, stamp = name, candidate
		}
	}
	if latest == "" {
		return filepath.Abs(filepath.Join(root, ".claude", "submissions", "requirement-binding.md"))
	}
	return filepath.Abs(filepath.Join(root, ".claude", "submissions", latest, "requirement-binding.md"))
}

func bindingQuestionnaireAnswers(data []byte) []string {
	answers := make([]string, len(bindingQuestions))
	for _, line := range strings.Split(string(data), "\n") {
		for i, question := range bindingQuestions {
			if answer, ok := strings.CutPrefix(strings.TrimSpace(line), "- "+question); ok {
				// Legacy drafts included this explanation in the question itself.
				// Remove it before reading the actual decision, including an empty draft.
				if i == 1 {
					answer = strings.TrimPrefix(strings.TrimSpace(answer), "Record that human decision; do not infer it from Git identity.")
				}
				answers[i] = strings.TrimSpace(answer)
			}
		}
	}
	return answers
}

func runSavedEntryQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	if !mainQuestionnaireViewer(request) || request.Event != "PostToolUse" || request.Error != "" {
		return false, 0
	}
	if failed, _ := request.ToolResponse["is_error"].(bool); failed {
		return false, 0
	}
	actual, saved := savedQuestionnairePath(request)
	if !saved {
		if path, ok := request.ToolResponse["file_path"].(string); ok {
			located := request
			located.ToolInput = map[string]any{"file_path": path}
			actual, saved = savedQuestionnairePath(located)
		}
	}
	if !saved {
		return false, 0
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")).Snapshot()
	if err != nil {
		return false, 0
	}
	relative, _ := filepath.Rel(root, actual)
	bindingSave := filepath.Base(actual) == "requirement-binding.md" && strings.HasPrefix(filepath.ToSlash(relative), ".claude/submissions/")
	if lifecycleState(snapshot.State) != "inactive" {
		if bindingSave {
			return true, auditQuestionnaireFeedback(stdout, "This binding application is not an inactive responsibility; the existing Runtime/binding receipt remains authoritative. Its save did not bind again or advance another stage.")
		}
		return false, 0
	}
	path, err := bindingQuestionnairePath(root)
	if err != nil {
		return false, 0
	}
	if !samePath(actual, path) {
		if bindingSave {
			return true, auditQuestionnaireFeedback(stdout, "This is an earlier binding application, not the current request at "+path+". Old authorization was not reused; fill only the current responsibility after an actual human startup decision.")
		}
		// S0 has no separate submission: saving the real locked requirement
		// merely prepares the original binding questions, never a default approval.
		rel, err := filepath.Rel(root, actual)
		if err != nil || filepath.Dir(filepath.ToSlash(rel)) != "docs/requirements" || !strings.HasPrefix(filepath.Base(rel), "REQ-") {
			return false, 0
		}
		data, err := os.ReadFile(actual)
		if err != nil || markdownField(string(data), "状态", "Status") != "locked" {
			return false, 0
		}
		context := stageEntryQuestionnaireContext(root, policy.Input{Event: "SessionStart"}, snapshot)
		return true, auditQuestionnaireFeedback(stdout, context)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true, auditQuestionnaireFeedback(stdout, "S1 current binding request "+path+" cannot be read: "+err.Error())
	}
	answers := bindingQuestionnaireAnswers(data)
	var missing []string
	for i, value := range answers {
		if value == "" {
			missing = append(missing, bindingQuestions[i])
		}
	}
	if len(missing) > 0 {
		return true, auditQuestionnaireFeedback(stdout, "S1 draft: "+path+". Still unanswered: "+strings.Join(missing, "; ")+". Write the selected branch refs verbatim, for example develop, main or origin/main; put explanations in a separate paragraph, not on the branch answer lines. Record only actual choices; leave authorization empty unless the human has actually authorized this requirement to start. No binding was recorded.")
	}
	selected := answers[0]
	if filepath.IsAbs(selected) {
		selected, err = filepath.Rel(root, selected)
		if err != nil {
			return true, auditQuestionnaireFeedback(stdout, err.Error())
		}
	}
	var output, errors bytes.Buffer
	code := bindRequirement(root, filepath.ToSlash(selected), answers[1], answers[2], answers[3], "", "", true, &output, &errors)
	if code != 0 {
		return true, auditQuestionnaireFeedback(stdout, "S1 original binding/preparation result for "+path+": "+errors.String()+". Follow the actual recorded outcome above; real Git commits/branch setup are Main's ordinary work, not Hook actions.")
	}
	// Binding has prepared S2 in the new Runtime. Read that current projection;
	// the incoming Post event and its old inactive context cannot select it.
	latest, readErr := runtime.NewStore(filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")).WithContext(contextForHook(request)).Snapshot()
	if readErr != nil {
		return true, auditQuestionnaireFeedback(stdout, "S1 binding recorded through original TR-001. Main/system must restore current S2 material access: "+readErr.Error()+". Do not resubmit the binding or copy Runtime metadata.")
	}
	return true, auditQuestionnaireFeedback(stdout, "S1 binding recorded through original TR-001; current cursor planning.design (S2). No further binding command is required. "+stageDesignQuestionnaireContext(root, latest))
}

// Entry questions reuse the original REQ document and a human-readable binding
// request. Preparing either draft never locks a REQ or records human approval.
func stageEntryQuestionnaireContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if !mainQuestionnaireViewer(request) {
		return ""
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "inactive" {
		return ""
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "S0/S1 questionnaire root is unavailable; Main must restore the project path: " + err.Error()
	}
	readonly := request.Event != "SessionStart"
	candidates := scanRequirements(root)
	var locked []reqSummary
	var drafts []reqSummary
	used := map[string]bool{}
	for _, candidate := range candidates {
		used[candidate.ID] = true
		if candidate.Status == "locked" {
			locked = append(locked, candidate)
		} else if candidate.Status != "archived" && candidate.Status != "locked" {
			drafts = append(drafts, candidate)
		}
	}
	if len(locked) > 0 {
		path, err := bindingQuestionnairePath(root)
		if err != nil {
			return "S1 current binding request path is unavailable: " + err.Error()
		}
		if !readonly {
			var body strings.Builder
			body.WriteString("# Requirement binding request\n\nRead the actual selected locked requirement. Main records the human's actual lock and startup intent; an existing explicit lock plus startup decision is not asked again. A name records the decision, not identity authentication. Leave that answer empty if startup has not been authorized.\n\n## Candidate requirement paths\n\n")
			for _, candidate := range locked {
				fmt.Fprintf(&body, "- %s\n", filepath.Join(root, filepath.FromSlash(candidate.Path)))
			}
			body.WriteString("\n## Questions\n\nWrite each actual answer after its existing question on the same line. Empty is unanswered, not a default choice. Branches must be actual selected project destinations, not guessed from HEAD or a default remote. Write each selected ref verbatim, for example develop, main or origin/main. Put explanations in a separate paragraph; do not append parenthetical notes to a selected ref.\n\n")
			values := make([]string, 4)
			if len(locked) == 1 {
				values[0] = filepath.Join(root, filepath.FromSlash(locked[0].Path))
			}
			known := mapFieldCLI(mapFieldCLI(snapshot.State, "bound_req"), "workspace")
			values[2], values[3] = stringValue(known["dev_branch"]), stringValue(known["release_upstream"])
			for i, question := range bindingQuestions {
				fmt.Fprintf(&body, "- %s %s\n", question, values[i])
			}
			body.WriteString("\nSave this same request by editing, scripts or MCP. The original receiver binds through TR-001 and gives S2; do not run a binding command or copy metadata. The selected locked REQ must actually be committed on the chosen development branch. Main does ordinary Git work; the Hook does not commit, create branches, push or publish.\n")
			if err := prepareQuestionnaireText(path, body.String()); err != nil {
				return "S1 request preparation failed at " + path + ": " + err.Error()
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "S1 current binding request: " + path + ". The responsibility-entry request is missing or unreadable: " + err.Error() + ". Main must resolve this preparation gap; ordinary Pre/Stop do not prepare it."
		}
		answers := bindingQuestionnaireAnswers(data)
		var missing []string
		for i, value := range answers {
			if value == "" {
				missing = append(missing, bindingQuestions[i])
			}
		}
		return "S1 binding questionnaire: " + path + ". Current unanswered choices: " + strings.Join(missing, "; ") + ". Record the human's actual startup decision (name is a record, not authentication), selected requirement and actual project branch destinations on the four question lines. Write branch refs verbatim (for example develop, main or origin/main), with explanations in a separate paragraph, then save normally for original TR-001 and S2. Do not repeat an already explicit lock/start decision or execute a binding command."

	}
	if len(drafts) > 0 {
		var body strings.Builder
		for _, draft := range drafts {
			fmt.Fprintf(&body, "S0 requirement questionnaire: %s. Complete the existing requirement interview and unresolved professional questions; obtain the human's explicit lock decision before S1. Saving a draft does not lock or bind it.\n", filepath.Join(root, filepath.FromSlash(draft.Path)))
		}
		if len(drafts) > 1 {
			body.WriteString("Main must confirm which existing draft is the current responsibility before editing; no draft was selected automatically.")
		}
		return strings.TrimSpace(body.String())
	}
	id := ""
	for n := 1; ; n++ {
		candidate := fmt.Sprintf("REQ-%03d", n)
		if !used[candidate] {
			id = candidate
			break
		}
	}
	path := filepath.Join(root, "docs/requirements", id+".md")
	templatePath := filepath.Join(root, "docs/requirements/REQ-template.md")
	if readonly {
		return "S0 questionnaire is not prepared yet. The responsibility-entry preparation must supply " + path + " from " + templatePath + "; Main owns preparation. No human confirmation or lock is recorded."
	}
	template, err := os.ReadFile(templatePath)
	if err != nil {
		return "S0 questionnaire preparation failed: template " + templatePath + " is unavailable; Main must restore the installed requirement template: " + err.Error()
	}
	draft := strings.ReplaceAll(string(template), "REQ-{id}", id)
	draft = strings.Replace(draft, "> 状态：draft / locked / changed / archived", "> 状态：draft", 1)
	draft = strings.Replace(draft, "> UI impact：none / changed / unknown", "> UI impact：unknown", 1)
	draft = strings.Replace(draft, "YYYY-MM-DD", time.Now().UTC().Format("2006-01-02"), 1)
	if err := prepareQuestionnaireText(path, draft); err != nil {
		return "S0 questionnaire preparation failed at " + path + ": " + err.Error()
	}
	return "S0 requirement questionnaire: " + path + "; source template: " + templatePath + ". Fill the existing requirement interview with the real user's goals, constraints, acceptance criteria and unresolved decisions. Save the draft; obtain explicit human confirmation and lock before S1. No answers or human approval were prefilled."
}

// Choose the responsibility's questions; this makes no tool permission decision.
func mainQuestionnaireViewer(request policy.Input) bool {
	return hookReaderIsMain(request)
}

func prepareQuestionnaireText(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(text)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func currentQuestionnaireLifecycleContext(root string, request policy.Input) string {
	if request.Event == "SubagentStart" && request.EffectiveAgentID() == "" {
		return ""
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return stageAnswerLifecycleContext(root, request)
	}
	return currentQuestionnaireLifecycleContextFromSnapshot(root, request, snapshot)
}

// Observation reuses the control result already read for this Hook. Any actual
// issuer/receiver retains its own authoritative checks and later-state refresh.
func currentQuestionnaireLifecycleContextFromSnapshot(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if request.Event == "SubagentStart" && request.EffectiveAgentID() == "" {
		return ""
	}
	if snapshot.State != nil {
		if mainQuestionnaireViewer(request) && request.Event == "SessionStart" && s7BudgetGateRequired(snapshot.State, root) {
			if _, err := prepareS7BudgetDecisionQuestionnaire(root, snapshot.State); err != nil {
				return "S7 budget application preparation failed; Main/system restores the original decision source: " + err.Error()
			}
		}
		if context := stageS7BudgetDecisionQuestionnaireContext(root, request, snapshot); context != "" {
			return context
		}
		lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
		if runtime.PacketEnabled(snapshot.State) && lifecycle["state"] == "document_verification" && mainQuestionnaireViewer(request) {
			return stageS5ObservedContext(root, snapshot.State)
		}
		if runtime.PacketEnabled(snapshot.State) && lifecycle["state"] == "planning" {
			if !mainQuestionnaireViewer(request) {
				return ""
			}
			switch lifecycle["phase"] {
			case "design":
				return stageDesignQuestionnaireContext(root, snapshot)
			case "contracts", "tasks":
				return stageCurrentPlanningContext(root, snapshot)
			}
		}
		if runtime.PacketEnabled(snapshot.State) && lifecycle["state"] == "bug_resolution" && s9ProfessionalPhase(stringValue(lifecycle["phase"])) && request.EffectiveAgentID() == "" && (request.AgentType != "" || request.AgentTranscriptPath != "" || request.Event == "SubagentStop" || request.Event == "TeammateIdle") {
			return "S9 native Worker responsibility is not identified by this event. Main must provide its exact registered questionnaire path in the actual task prompt; saving that path uses its original registered owner. No Main responsibility was inferred."
		}
		if lifecycle["state"] == "building" {
			return stageBuilderQuestionLifecycleContext(root, request, snapshot)
		}
		if context := stageEntryQuestionnaireContext(root, request, snapshot); context != "" {
			return context
		}
		if context := stageRepairLeadQuestionnaireContext(root, request, snapshot); context != "" {
			if runtime.PacketEnabled(snapshot.State) && lifecycle["state"] == "bug_resolution" && s9ProfessionalPhase(stringValue(lifecycle["phase"])) {
				handoff := ""
				if mainQuestionnaireViewer(request) {
					handoff = s9KnownMainHandoffsContext(contextForHook(request), root, snapshot)
				}
				return strings.TrimSpace(strings.Join([]string{stageRepairVerifierDispatchContext(root, request, snapshot), context, handoff}, "\n"))
			}
			return context
		}
		if runtime.PacketEnabled(snapshot.State) && lifecycle["state"] == "bug_resolution" && s9ProfessionalPhase(stringValue(lifecycle["phase"])) {
			// The listener owns current Main dispatch. Only produce the fallback
			// when it declines, so observation reads and projects each responsibility once.
			if context, handled := s9PacketQuestionnaireLifecycleContext(root, request, snapshot); handled {
				return strings.TrimSpace(strings.Join([]string{stageRepairVerifierDispatchContext(root, request, snapshot), context}, "\n"))
			}
			dispatch := ""
			if request.EffectiveAgentID() != "" || request.AgentType == "" && request.AgentTranscriptPath == "" {
				dispatch = stageRepairDispatchContext(root, request, snapshot)
			}
			return strings.TrimSpace(strings.Join([]string{dispatch, stageRepairVerifierDispatchContext(root, request, snapshot)}, "\n"))
		}
		if context := stageReviewPlanQuestionnaireContext(root, request, snapshot); context != "" {
			return context
		}
		stopFeedback := request.Event == "Stop" || request.Event == "SubagentStop" || request.Event == "TeammateIdle"
		if lifecycle["state"] == "bug_resolution" && lifecycle["phase"] == "investigation" && runtime.PacketEnabled(snapshot.State) {
			ctx, cancel := context.WithTimeout(contextForHook(request), 4*time.Second)
			defer cancel()
			lead := stageInvestigationLeadQuestionnaireContext(root, request, snapshot)
			questions := stageHypothesisResultLifecycleContext(ctx, root, request, nil, snapshot, request.EffectiveAgentID())
			return strings.TrimSpace(strings.Join([]string{lead, questions}, "\n"))
		}
		if lifecycle["state"] == "verification" && runtime.PacketEnabled(snapshot.State) && (request.Event == "PreToolUse" || request.Event == "SessionStart" || request.Event == "SubagentStart" || request.Event == "PreCompact" || stopFeedback) {
			ctx, cancel := context.WithTimeout(contextForHook(request), 4*time.Second)
			defer cancel()
			// No service is needed to read current S7 facts. The original issuer
			// initializes it only if a relevant Pre actually needs preparation.
			return stageReviewResultLifecycleContext(ctx, root, request, nil, snapshot, request.EffectiveAgentID())
		}
		if context := stageAuditQuestionnaireContext(root, request, snapshot); context != "" {
			return context
		}
		if context := stageHumanReleaseQuestionnaireContext(root, request, snapshot); context != "" {
			return context
		}
	}
	return stageAnswerLifecycleContext(root, request)
}
