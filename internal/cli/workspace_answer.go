package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runWorkspaceAnswer(args []string, stdout, stderr io.Writer) int {
	verb := args[0]
	fs := flag.NewFlagSet("runtime workspace "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "registered Worker or Main root")
	id := fs.String("assignment", "", "registered review Assignment")
	owner := fs.String("agent", "", "registered reviewer owner")
	if err := parseWorkspaceFlags(fs, args[1:]); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	b, state, err := workspace.Load(*root)
	if err != nil || b == nil {
		return fail(fmt.Errorf("review registry unavailable: %v", err))
	}
	e, ok := b.Execution(*id, workspace.RuntimeID(state), workspace.Generation(state))
	if !ok || e.AgentID != *owner || e.Status != "ready" || e.Purpose != workspace.DocumentReviewPurpose {
		return fail(fmt.Errorf("answer adapter requires the current ready document reviewer"))
	}
	p, err := workspace.DocumentReviewPacket(state, e)
	if err != nil {
		return fail(err)
	}
	submissionPaths, err := b.WorkerSubmissionPaths(e)
	if err != nil {
		return fail(err)
	}
	s, err := submission.New(b.MainRoot)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	path := filepath.Join(e.Path, filepath.FromSlash(submissionPaths.Answer))
	if verb == "review-check" {
		life, _ := state["lifecycle"].(map[string]any)
		if life["state"] != "document_verification" || p.Binding.StageEntry != stageAnswerEntry(life["phase_revision"]) {
			return fail(fmt.Errorf("review-check origin was replaced"))
		}
		if err := b.ValidateExecution(ctx, e, false); err != nil {
			return fail(err)
		}
		if err := b.ValidateDocumentReviewView(e, p); err != nil {
			return fail(err)
		}
		if err := b.ValidateInputs(e); err != nil {
			return fail(err)
		}
		if err := runtime.ValidateWorkPacket(b.MainRoot, state, p, s.Files); err != nil {
			return fail(err)
		}
		bound, _ := state["bound_req"].(map[string]any)
		reqID, _ := bound["id"].(string)
		result, err := semantic.TasksCheckWithFiles(b.MainRoot, s.Files, reqID)
		if err != nil {
			return fail(err)
		}
		if p.TaskAnalysis != nil {
			frozen, err := workpacket.ReadTaskAnalysis(b.MainRoot, p)
			if err != nil {
				return fail(err)
			}
			if err := json.Unmarshal(frozen.TaskContexts, &result.TaskContexts); err != nil {
				return fail(err)
			}
			if err := json.Unmarshal(frozen.GranularityAdvice, &result.GranularityAdvice); err != nil {
				return fail(err)
			}
			result.ConventionsResolverVersion = frozen.ConventionsResolverVersion
			result.TaskConventions = nil
			if len(frozen.TaskConventions) > 0 {
				if err := json.Unmarshal(frozen.TaskConventions, &result.TaskConventions); err != nil {
					return fail(err)
				}
			}
			result.ContextAnalysisVersion = frozen.ParserVersion
			result.BehaviorProjectionVersion = frozen.BehaviorProjectionVersion
			result.BehaviorExamples = nil
			if len(frozen.BehaviorExamples) > 0 {
				if err := json.Unmarshal(frozen.BehaviorExamples, &result.BehaviorExamples); err != nil {
					return fail(err)
				}
			}
		}
		if code := encodeJSON(stdout, result); code != 0 {
			return code
		}
		if len(result.Problems) > 0 {
			return 1
		}
		return 0
	}
	var r submission.Receipt
	if verb == "receipt" {
		r, err = s.ReceiptContext(ctx, *owner, path)
	} else {
		r, err = s.Save(ctx, *owner, path)
	}
	if code := encodeJSON(stdout, r); code != 0 {
		return code
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

func stageAnswerEntry(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return -1
}

// A top-level S5 Worker Stop owns one review, not Main's planning/review queue.
// The check precedes every Controller mutation, just like Main's preflight.
func documentReviewStopPreflight(root string, input policy.Input) (bool, policy.Decision) {
	if input.Event != "Stop" || !input.Runtime.PacketMode || input.EffectiveAgentID() == "" || input.Runtime.Workspace == nil {
		return false, policy.Decision{}
	}
	b, state, err := workspace.Load(root)
	if err != nil || b == nil {
		return false, policy.Decision{}
	}
	for _, e := range b.Executions {
		if e.Purpose != workspace.DocumentReviewPurpose || e.AgentID != input.EffectiveAgentID() || e.RuntimeID != workspace.RuntimeID(state) || e.BaselineGeneration != workspace.Generation(state) {
			continue
		}
		blocked := func(reason string) (bool, policy.Decision) {
			return true, policy.Decision{Decision: "deny", RuleID: "document_review_stop_pending", Reason: reason, Recovery: []string{"Resolve the reported S5 execution issue before stopping; Main owns peer delivery and human decisions."}}
		}
		cwd, err := workspace.Canonical(input.CWD)
		if err != nil || cwd != e.Path {
			return blocked("S5 Worker Stop must identify its exact registered execution root")
		}
		if e.Status != "ready" {
			return blocked("S5 Worker execution is not ready")
		}
		ctx, cancel := context.WithTimeout(contextForHook(input), 2*time.Second)
		err = b.ValidateExecution(ctx, e, false)
		cancel()
		if err != nil {
			return blocked(err.Error())
		}
		p, err := workspace.DocumentReviewPacket(state, e)
		if err != nil {
			return blocked(err.Error())
		}
		submissionPaths, err := b.WorkerSubmissionPaths(e)
		if err != nil {
			return blocked(err.Error())
		}
		life, _ := state["lifecycle"].(map[string]any)
		if life["state"] != "document_verification" || stageAnswerEntry(life["phase_revision"]) != p.Binding.StageEntry || submission.CompletedOwner(root, e.AgentID) {
			return true, policy.Decision{Decision: "allow"}
		}
		if input.StopHookActive {
			return true, policy.Decision{Decision: "allow"}
		}
		recovery := documentReviewAnswerRecovery(e, p, submissionPaths.Answer)
		reason := "your current S5 answer has not been received; a process exit is not the deliverable"
		if len(recovery) > 0 {
			// Stop feedback with questionnaire context renders Reason and omits
			// Recovery, so keep the exact answer path and fields visible here too.
			reason += ". " + recovery[0]
		}
		return true, policy.Decision{Decision: "deny", RuleID: "document_review_stop_pending", Reason: reason, Recovery: recovery}
	}
	return false, policy.Decision{}
}

func documentReviewAnswerRecovery(e workspace.Execution, p workpacket.Packet, answerPath string) []string {
	fullPath := filepath.Join(e.Path, filepath.FromSlash(answerPath))
	data, err := workpacket.ReadFile(e.Path, answerPath, 256*1024)
	if err != nil {
		return []string{fmt.Sprintf("Fill answer.conclusion and answer.findings in the current S5 answer at %s, then save it; the original consumer records the answer automatically.", fullPath)}
	}
	_, issues := workpacket.ValidateAnswer(p, data)
	missing := make([]string, 0, len(issues))
	seen := map[string]bool{}
	for _, issue := range issues {
		field := strings.TrimSpace(issue.Pointer)
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		missing = append(missing, field)
	}
	if len(missing) > 0 {
		return []string{fmt.Sprintf("Complete %s in the current S5 answer at %s, then save it; the original consumer records the answer automatically.", strings.Join(missing, ", "), fullPath)}
	}
	return []string{fmt.Sprintf("The current S5 answer at %s is complete but has not been received; save it again so the original consumer can record it. Process exit alone is not delivery.", fullPath)}
}
