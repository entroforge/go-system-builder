package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runSavedBuilderQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	if mainQuestionnaireViewer(request) {
		return false, 0 // Main owns integration; it does not save or sweep Builder answers.
	}
	path, saved := savedQuestionnairePath(request)
	if !saved {
		return false, 0
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return false, 0
	}
	questions, _ := projectS6BuilderQuestionnaires(root, "", snapshot)
	var matched s6BuilderQuestionnaire
	var questionnaire, questionnairePath string
	matches := 0
	for _, q := range questions {
		for _, candidate := range []struct{ name, path string }{{"PLAN", q.PlanPath}, {"Completion", q.ResultPath}} {
			if candidate.path == "" {
				continue
			}
			expected, err := workspace.CanonicalProspective(candidate.path)
			if err == nil && expected == path {
				matches++
				matched, questionnaire, questionnairePath = q, candidate.name, expected
			}
		}
	}
	if matches == 0 {
		return false, 0
	}
	if matches > 1 {
		builderQuestionnaireFeedback(stdout, fmt.Sprintf("S6 save at exact path %s matches multiple current Worker questionnaires; no lifecycle command ran. Resolve the duplicate registry paths.", path), 0, "", "")
		return true, 0
	}
	verb, runtimeVerb, flagName := "begin", "agent-begin", "--plan"
	next := "Continue the assigned TASK work; S6 integration and stage completion remain pending."
	var commandOut, commandErr bytes.Buffer
	if questionnaire == "Completion" {
		verb, runtimeVerb, flagName = "report", "task-complete", "--message"
		next = "Main should use the existing task-integrate flow to inspect, integrate, and verify the Worker result. Integration and stage completion remain pending."
		if matched.CanonicalRecord {
			if err := prepareS6BuilderResult(root, matched, snapshot); err != nil {
				builderQuestionnaireFeedback(stdout, fmt.Sprintf("S6 Result at exact registered path %s could not be prepared: %v. Missing registered context belongs to Main. %s No report command ran and the existing answer is preserved.", questionnairePath, err, s6QuestionnaireCorrectionNext(request, matched, questionnairePath)), 1, "", "")
				return true, 0
			}
		}
	}
	var code int
	if matched.Workspace {
		code = runWorkspaceLifecycle([]string{verb, "--root", root, "--assignment", matched.AssignmentID, "--agent", matched.AgentID}, &commandOut, &commandErr)
	} else {
		code = runRuntime([]string{runtimeVerb, "--root", root, "--agent-id", matched.AgentID, flagName, questionnairePath}, &commandOut, &commandErr)
	}
	if code != 0 {
		next = s6QuestionnaireCorrectionNext(request, matched, questionnairePath)
	} else if questionnaire == "Completion" {
		if current, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot(); err == nil {
			if progress, _ := s6AcceptedResultProgress(root, current, matched); progress != "" {
				next = progress
			} else {
				next = fmt.Sprintf("The original Builder receipt for %s could not be verified. Main owns receipt inspection and the next rework or integration action; do not infer completion.", questionnairePath)
			}
		} else {
			next = fmt.Sprintf("The original Builder receipt for %s could not be read after acceptance. Main owns receipt inspection and the next action; do not infer completion.", questionnairePath)
		}
	} else if questionnaire == "PLAN" && matched.CanonicalRecord {
		if current, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot(); err == nil {
			if err := prepareS6BuilderResult(root, matched, current); err != nil {
				fmt.Fprintf(&commandErr, "Result draft preparation: %v\n", err)
			}
		}
	}
	header := fmt.Sprintf("S6 %s questionnaire saved at exact registered path %s. Next: %s", questionnaire, questionnairePath, next)
	builderQuestionnaireFeedback(stdout, header, code, commandOut.String(), commandErr.String())
	return true, 0
}

func prepareS6BuilderResult(mainRoot string, q s6BuilderQuestionnaire, snapshot runtime.Snapshot) error {
	record, found, err := assignmentrecord.ForOwner(snapshot.State, q.AgentID)
	if err != nil || !found || record.AssignmentID != q.AssignmentID {
		return fmt.Errorf("resolve the current registered Builder Record: %v", err)
	}
	planRef, err := plancheckpoint.ActivatedRef(mainRoot, snapshot, q.AgentID)
	if err != nil || planRef == "" {
		return fmt.Errorf("the original accepted PLAN is not activated: %v", err)
	}
	planBytes, err := workpacket.ReadFile(mainRoot, planRef, 1<<20)
	if err != nil {
		return fmt.Errorf("read accepted PLAN at %s: %w", planRef, err)
	}
	var acceptedPlan struct {
		Assertions []assignmentresult.PlanAssertion `json:"assertion_checks"`
	}
	if err := json.Unmarshal(planBytes, &acceptedPlan); err != nil || len(acceptedPlan.Assertions) == 0 {
		return fmt.Errorf("accepted PLAN at %s has no usable assertion checks: %v", planRef, err)
	}
	dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		return err
	}
	template, _ := agentcontext.ResultTemplate(dispatch, record.RuntimeID, "2026-01-01T00:00:00Z")
	if template == nil {
		return fmt.Errorf("the registered Builder Record has no canonical Result template")
	}
	root, err := filepath.Abs(q.WorkerRoot)
	if err != nil || root == "" {
		return fmt.Errorf("resolve the registered Worker draft root: %v", err)
	}
	resultPath, err := filepath.Abs(q.ResultPath)
	if err != nil {
		return fmt.Errorf("resolve the registered Result path: %w", err)
	}
	rel, err := filepath.Rel(root, resultPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("registered Result path escapes the Worker execution")
	}
	rel = filepath.ToSlash(rel)
	data, err := workpacket.ReadFile(root, rel, 1<<20)
	if err != nil {
		return fmt.Errorf("read the exact Result draft %s: %w", q.ResultPath, err)
	}
	if err := agentcontext.ValidateResultDraft(template, data); err != nil {
		return fmt.Errorf("validate the exact Result draft %s: %w", q.ResultPath, err)
	}
	var draft map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&draft); err != nil || draft == nil {
		return fmt.Errorf("decode the exact Result draft %s: %v", q.ResultPath, err)
	}
	changed := false
	if draft["assertion_evidence"] == nil {
		rows := make([]any, 0, len(acceptedPlan.Assertions))
		for _, item := range acceptedPlan.Assertions {
			rows = append(rows, map[string]any{"assertion": item.Assertion, "oracle": item.Oracle, "evidence_refs": []string{}})
		}
		draft["assertion_evidence"], changed = rows, true
	}
	if record.ClosingContract != nil && draft["closing_assessment"] == nil {
		conditions := make([]any, 0, len(record.DoneWhen))
		for doneWhenIndex := range record.DoneWhen {
			indexes := make([]int, 0)
			for assertionIndex, item := range acceptedPlan.Assertions {
				for _, index := range item.DoneWhenIndexes {
					if index == doneWhenIndex {
						indexes = append(indexes, assertionIndex)
						break
					}
				}
			}
			if len(indexes) == 0 {
				return fmt.Errorf("accepted PLAN has no assertion mapped to DoneWhen %d", doneWhenIndex)
			}
			conditions = append(conditions, map[string]any{"done_when_index": doneWhenIndex, "plan_assertion_indexes": indexes, "judgment": nil, "rationale": nil})
		}
		draft["closing_assessment"], changed = map[string]any{"disposition": nil, "conditions": conditions}, true
	}
	if answer, ok := s6ProfessionalAnswer(draft); ok {
		if assignmentresult.ValidateAnswer(answer, acceptedPlan.Assertions) == nil && assignmentresult.ValidateClosingAssessment(record, acceptedPlan.Assertions, answer) == nil {
			answerDigest, err := assignmentresult.AnswerDigest(answer)
			if err != nil {
				return err
			}
			closureDigest := ""
			if record.ClosingContract != nil {
				closureDigest, err = assignment.CurrentWorkerClosureDigest(context.Background(), mainRoot, snapshot.State, record)
				if err != nil {
					return fmt.Errorf("collect current registered Worker check facts: %w", err)
				}
			}
			read := func(ref string) ([]byte, error) {
				return workpacket.ReadFile(mainRoot, ref, assignmentresult.EvidenceReadLimit)
			}
			history, err := assignmentresult.AcceptedHistory(snapshot.State, record, read)
			if err != nil {
				return fmt.Errorf("read accepted Result history: %w", err)
			}
			messageID := "msg-result-" + answerDigest
			if closureDigest != "" {
				messageID += "-" + closureDigest
			}
			for _, accepted := range history {
				priorClosure, err := assignmentresult.EffectiveToolClosureDigest(record, accepted)
				if err != nil {
					return err
				}
				if accepted.AnswerDigest == answerDigest && priorClosure == closureDigest {
					messageID = accepted.SubmissionKey
					break
				}
			}
			if draft["message_id"] != messageID {
				draft["message_id"], changed = messageID, true
				draft["correlation_id"], changed = "corr-"+strings.TrimPrefix(messageID, "msg-"), true
			}
		}
	}
	if !changed {
		return nil
	}
	if err := agentcontext.ValidateResultDraft(template, mustMarshalS6Draft(draft)); err != nil {
		return fmt.Errorf("prepared Result fields do not match the registered Record: %w", err)
	}
	updated, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if err := replaceS6Draft(root, rel, data, updated); err != nil {
		return fmt.Errorf("update the exact Result draft %s: %w", q.ResultPath, err)
	}
	return nil
}

func s6QuestionnaireCorrectionNext(request policy.Input, q s6BuilderQuestionnaire, path string) string {
	if owner := request.EffectiveAgentID(); owner == "" || owner == "Main" {
		return fmt.Sprintf("The registered Builder %s owns correcting this answer at %s; Main should coordinate the same registered execution and should not fill the Worker's Result.", q.AgentID, path)
	}
	return fmt.Sprintf("Correct the specific validation feedback below, then save this questionnaire again at the same exact path %s.", path)
}

func s6ProfessionalAnswer(draft map[string]any) (assignmentresult.Answer, bool) {
	var envelope struct {
		Summary           *string                               `json:"summary"`
		AssertionEvidence *[]assignmentresult.AssertionEvidence `json:"assertion_evidence"`
		RisksBlockers     *[]string                             `json:"risks_blockers"`
		ScopeDeviations   *[]string                             `json:"scope_deviations"`
		ClosingAssessment *assignmentresult.ClosingAssessment   `json:"closing_assessment"`
	}
	data, err := json.Marshal(draft)
	if err != nil || json.Unmarshal(data, &envelope) != nil || envelope.Summary == nil || envelope.AssertionEvidence == nil || envelope.RisksBlockers == nil || envelope.ScopeDeviations == nil {
		return assignmentresult.Answer{}, false
	}
	return assignmentresult.Answer{Summary: *envelope.Summary, AssertionEvidence: *envelope.AssertionEvidence, RisksBlockers: *envelope.RisksBlockers, ScopeDeviations: *envelope.ScopeDeviations, ClosingAssessment: envelope.ClosingAssessment}, true
}

func mustMarshalS6Draft(draft map[string]any) []byte {
	data, _ := json.Marshal(draft)
	return data
}

func replaceS6Draft(root, relative string, old, updated []byte) error {
	parent, name, err := workpacket.OpenParent(root, relative, false)
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("draft is not a regular file: %v", err)
	}
	current, err := parent.Open(name)
	if err != nil {
		return err
	}
	currentBytes, readErr := io.ReadAll(io.LimitReader(current, (1<<20)+1))
	closeErr := current.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	if !bytes.Equal(currentBytes, old) {
		return fmt.Errorf("draft changed while the original save was being received")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := fmt.Sprintf(".s6-result-%x.tmp", nonce)
	file, err := parent.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer parent.Remove(temporary)
	_, writeErr := file.Write(updated)
	syncErr := file.Sync()
	closeErr = file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := parent.Rename(temporary, name); err != nil {
		return err
	}
	directory, err := parent.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func builderQuestionnaireFeedback(stdout io.Writer, header string, code int, commandOut, commandErr string) {
	context := header + "\n"
	if code == 0 {
		if strings.TrimSpace(commandErr) != "" {
			context += commandErr
			if !strings.HasSuffix(commandErr, "\n") {
				context += "\n"
			}
		}
		context += commandOut
	} else {
		context += fmt.Sprintf("Original lifecycle command returned exit code %d.\n", code) + commandOut + commandErr
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": context}})
}
