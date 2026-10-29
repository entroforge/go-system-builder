package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/classifier"
)

type nativeLiveObservation struct {
	Event    string         `json:"event"`
	Started  int64          `json:"started_ns"`
	Finished int64          `json:"finished_ns"`
	Exit     int            `json:"exit_code"`
	Stdout   string         `json:"stdout"`
	Payload  map[string]any `json:"payload"`
}

type nativeLiveCall struct {
	id, name string
	input    map[string]any
}

// CheckNativeLiveContinuationEvidence is shared by the internal S9 and external
// S6 tests. It checks execution evidence only; original consumer/receipt and
// Main integration assertions remain with each live test. A successful send is
// deliberately insufficient without the original actor's later Stop and SDK
// completion notification. No synthetic Hook event or actor mapping is used.
func CheckNativeLiveContinuationEvidence(artifacts string, stream []byte, continuation bool) error {
	files, err := filepath.Glob(filepath.Join(artifacts, "hook-*.json"))
	if err != nil {
		return err
	}
	var observations []nativeLiveObservation
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var row nativeLiveObservation
		if err := json.Unmarshal(data, &row); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		observations = append(observations, row)
	}
	return checkNativeLiveContinuation(observations, stream, continuation)
}

func checkNativeLiveContinuation(observations []nativeLiveObservation, stream []byte, continuation bool) error {
	var calls []nativeLiveCall
	var frames []map[string]any
	for _, line := range bytes.Split(stream, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			return fmt.Errorf("invalid live stream: %w", err)
		}
		frames = append(frames, row)
		if row["type"] != "assistant" || row["parent_tool_use_id"] != nil {
			continue
		}
		message, _ := row["message"].(map[string]any)
		content, _ := message["content"].([]any)
		for _, raw := range content {
			part, _ := raw.(map[string]any)
			name, _ := part["name"].(string)
			if part["type"] != "tool_use" || (name != "Agent" && name != "Task" && name != "SendMessage") {
				continue
			}
			input, _ := part["input"].(map[string]any)
			id, _ := part["id"].(string)
			calls = append(calls, nativeLiveCall{id, name, input})
		}
	}
	var launch, send *nativeLiveCall
	for i := range calls {
		call := &calls[i]
		if call.id == "" {
			return fmt.Errorf("native tool call has no actual tool_use_id")
		}
		if call.name == "SendMessage" {
			if send != nil {
				return fmt.Errorf("unexpected additional native continuation")
			}
			send = call
		} else {
			if call.input["resume"] != nil {
				return fmt.Errorf("Agent.resume is not the supported same-actor continuation contract")
			}
			if launch != nil {
				return fmt.Errorf("replacement Agent invocation instead of same-actor continuation")
			}
			launch = call
			if call.input["isolation"] != "worktree" {
				return fmt.Errorf("native launch lacks worktree isolation")
			}
		}
	}
	if launch == nil || (send != nil) != continuation {
		return fmt.Errorf("missing launch or unexpected continuation: launch=%v send=%v want=%t", launch != nil, send != nil, continuation)
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].Started < observations[j].Started })
	// Match model requests to the actual parsed platform boundary, not merely
	// model-supplied parameters. Both pre and post must carry the same request.
	match := func(call nativeLiveCall) (nativeLiveObservation, error) {
		pre, post := 0, 0
		var result nativeLiveObservation
		for _, row := range observations {
			if row.Payload["tool_use_id"] != call.id || (row.Event != "PreToolUse" && row.Event != "PostToolUse") {
				continue
			}
			if row.Payload["agent_id"] != "" && row.Payload["agent_id"] != nil && row.Payload["agent_id"] != "Main" {
				continue
			}
			if row.Payload["tool_name"] != call.name {
				return result, fmt.Errorf("native tool changed between request and Hook")
			}
			actual, _ := row.Payload["tool_input"].(map[string]any)
			keys := []string{"isolation", "prompt", "resume"}
			if call.name == "SendMessage" {
				keys = []string{"to", "message"}
			}
			for _, key := range keys {
				if actual[key] != call.input[key] {
					return result, fmt.Errorf("actual %s %s differs from model request", call.name, key)
				}
			}
			if row.Exit != 0 {
				return result, fmt.Errorf("native %s Hook failed", row.Event)
			}
			if row.Event == "PreToolUse" {
				pre++
			} else {
				post++
				result = row
			}
		}
		if pre != 1 || post != 1 {
			return result, fmt.Errorf("missing or ambiguous actual %s Hook pair: %d/%d", call.name, pre, post)
		}
		return result, nil
	}
	launched, err := match(*launch)
	if err != nil {
		return err
	}
	response, _ := launched.Payload["tool_response"].(map[string]any)
	actor, _ := response["agentId"].(string)
	if actor == "" {
		return fmt.Errorf("actual launch response lacks platform agentId")
	}
	var starts, stops []nativeLiveObservation
	cwd := ""
	for _, row := range observations {
		if row.Event != "SubagentStart" && row.Event != "SubagentStop" {
			continue
		}
		if row.Payload["agent_id"] != actor {
			return fmt.Errorf("new actor observed instead of original %s", actor)
		}
		path, _ := row.Payload["cwd"].(string)
		if path == "" {
			return fmt.Errorf("lifecycle lacks actual worktree CWD")
		}
		if cwd == "" {
			cwd = path
		}
		if path != cwd {
			return fmt.Errorf("same actor changed actual worktree")
		}
		if row.Exit != 0 {
			return fmt.Errorf("actual %s failed", row.Event)
		}
		if row.Event == "SubagentStart" {
			starts = append(starts, row)
		} else {
			stops = append(stops, row)
		}
	}
	want := 1
	if continuation {
		want = 2
	}
	if len(starts) != want || len(stops) < want {
		return fmt.Errorf("incomplete same-actor lifecycle: starts=%d stops=%d want=%d", len(starts), len(stops), want)
	}
	if recorded, _ := response["worktreePath"].(string); recorded != "" && recorded != cwd {
		return fmt.Errorf("launch response worktree differs from actual lifecycle")
	}
	var sent nativeLiveObservation
	if continuation {
		if send.input["to"] != actor || send.input["message"] == nil || send.input["message"] == "" {
			return fmt.Errorf("continuation must address original actor with actual task message")
		}
		sent, err = match(*send)
		if err != nil {
			return err
		}
		resumed, _ := sent.Payload["tool_response"].(map[string]any)
		if resumed["success"] != true || resumed["resumedAgentId"] != actor {
			return fmt.Errorf("actual send did not resume original actor")
		}
		if starts[1].Started <= sent.Started {
			return fmt.Errorf("no original actor Start after actual continuation")
		}
		endedBeforeSend := false
		for _, stop := range stops {
			if stop.Started > starts[0].Started && stop.Finished < sent.Started {
				endedBeforeSend = true
			}
		}
		if !endedBeforeSend {
			return fmt.Errorf("continuation sent before first actual Stop completed")
		}
	}
	// Completed SDK task notifications are the actual return carrier. A send's
	// tool_result, async launch, stopped task or a different run proves nothing.
	seenRuns := map[string]bool{}
	for run, start := range starts {
		next := int64(0)
		if run+1 < len(starts) {
			next = starts[run+1].Started
		}
		stopped := false
		for _, stop := range stops {
			if stop.Started > start.Started && (next == 0 || stop.Started < next) {
				stopped = true
			}
		}
		if !stopped {
			return fmt.Errorf("run %d has no actual original Stop", run)
		}
		toolID := launch.id
		if run > 0 {
			toolID = send.id
		}
		foundStart, foundCompletion := false, false
		runID := ""
		for _, frame := range frames {
			if frame["type"] != "system" || frame["task_id"] != actor || frame["tool_use_id"] != toolID {
				continue
			}
			if frame["subtype"] == "task_started" {
				if foundStart {
					return fmt.Errorf("ambiguous SDK run for actual tool %s", toolID)
				}
				foundStart = true
				runID, _ = frame["run_id"].(string)
				if runID == "" || seenRuns[runID] {
					return fmt.Errorf("missing or reused SDK run identity")
				}
				seenRuns[runID] = true
				if run > 0 && frame["is_backgrounded"] != true {
					return fmt.Errorf("continued run is not actual background run")
				}
			}
			if frame["subtype"] == "task_notification" && frame["status"] == "completed" && foundStart && runID != "" && frame["run_id"] == runID {
				summary, _ := frame["summary"].(string)
				if summary != "" {
					foundCompletion = true
				}
			}
		}
		if !foundCompletion {
			return fmt.Errorf("run %d has no real completed task_notification after task_started", run)
		}
	}
	return nil
}

// These are independent fixture expectations, never inferred from a model's
// summary or from the very context being checked. Failure is optional only for
// a successful reception; Accepted always names the original received fact.
type nativeLiveHandoffExpectation struct {
	Actor, Run, ToolID          string
	Owner, Assignment, InputRef string
	Accepted, Failure, Command  string
	ActionCommand               string
	Continuation                bool
}

func CheckNativeLiveHandoffEvidence(artifacts string, stream []byte, want nativeLiveHandoffExpectation) error {
	files, err := filepath.Glob(filepath.Join(artifacts, "hook-*.json"))
	if err != nil {
		return err
	}
	var rows []nativeLiveObservation
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var row nativeLiveObservation
		if err := json.Unmarshal(data, &row); err != nil {
			return err
		}
		rows = append(rows, row)
	}
	return checkNativeLiveHandoff(rows, stream, want)
}

func nativeLiveCommandWords(command string) ([]string, error) {
	tokens, err := classifier.Tokenize(command)
	if err != nil {
		return nil, err
	}
	var words []string
	for _, token := range tokens {
		switch token.Kind {
		case classifier.TkWord, classifier.TkShortFlag, classifier.TkLongFlag, classifier.TkValue, classifier.TkQuotedString:
			words = append(words, token.Value)
		default:
			return nil, fmt.Errorf("consumer action contains a shell operator or substitution")
		}
	}
	return words, nil
}

func checkNativeLiveHandoff(rows []nativeLiveObservation, stream []byte, want nativeLiveHandoffExpectation) error {
	if err := checkNativeLiveContinuation(rows, stream, want.Continuation); err != nil {
		return err
	}
	if want.Actor == "" || want.Run == "" || want.ToolID == "" || want.Owner == "" || want.Assignment == "" || want.InputRef == "" || want.Accepted == "" || want.Command == "" {
		return fmt.Errorf("handoff requires exact fixture responsibility, input, receipt and consumer action")
	}
	actionCommand := want.ActionCommand
	if actionCommand == "" {
		actionCommand = want.Command
	}
	expectedWords, err := nativeLiveCommandWords(actionCommand)
	if err != nil || len(expectedWords) == 0 {
		return fmt.Errorf("invalid expected consumer command: %v", err)
	}
	type locatedCall struct {
		nativeLiveCall
		frame int
	}
	var calls []locatedCall
	completedAt := -1
	for index, line := range bytes.Split(stream, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal(line, &frame); err != nil {
			return err
		}
		if frame["type"] == "system" && frame["subtype"] == "task_notification" && frame["status"] == "completed" && frame["task_id"] == want.Actor && frame["run_id"] == want.Run && frame["tool_use_id"] == want.ToolID {
			if completedAt >= 0 {
				return fmt.Errorf("ambiguous handoff completion notification")
			}
			completedAt = index
		}
		if frame["type"] != "assistant" || frame["parent_tool_use_id"] != nil {
			continue
		}
		message, _ := frame["message"].(map[string]any)
		content, _ := message["content"].([]any)
		for _, raw := range content {
			part, _ := raw.(map[string]any)
			if part["type"] != "tool_use" {
				continue
			}
			id, _ := part["id"].(string)
			name, _ := part["name"].(string)
			input, _ := part["input"].(map[string]any)
			calls = append(calls, locatedCall{nativeLiveCall{id, name, input}, index})
		}
	}
	if completedAt < 0 {
		return fmt.Errorf("no completion for exact original actor/run/tool")
	}
	lastStop := int64(0)
	for _, row := range rows {
		if row.Event == "SubagentStop" && row.Payload["agent_id"] == want.Actor && row.Exit == 0 && row.Finished > lastStop {
			lastStop = row.Finished
		}
	}
	main := func(row nativeLiveObservation) bool {
		owner := row.Payload["agent_id"]
		return owner == nil || owner == "" || owner == "Main"
	}
	for i, delivery := range calls {
		if delivery.frame <= completedAt || i+1 >= len(calls) || calls[i+1].frame <= delivery.frame {
			continue
		}
		for _, row := range rows {
			if row.Event != "PreToolUse" || !main(row) || row.Payload["tool_use_id"] != delivery.id || row.Payload["tool_name"] != delivery.name || row.Exit != 0 || row.Started <= lastStop || row.Finished < row.Started {
				continue
			}
			actual, _ := row.Payload["tool_input"].(map[string]any)
			encodedActual, _ := json.Marshal(actual)
			encodedCall, _ := json.Marshal(delivery.input)
			if !bytes.Equal(encodedActual, encodedCall) {
				continue
			}
			var output struct {
				Hook struct {
					Context string `json:"additionalContext"`
				} `json:"hookSpecificOutput"`
			}
			if json.Unmarshal([]byte(row.Stdout), &output) != nil {
				continue
			}
			context := output.Hook.Context
			complete := true
			for _, exact := range []string{want.Owner, want.Assignment, want.InputRef, want.Accepted, want.Failure, want.Command} {
				if exact != "" && !strings.Contains(context, exact) {
					complete = false
				}
			}
			if !complete {
				continue
			}
			// The tool whose Pre delivers context was selected before that context.
			// Only a later model request can demonstrate Main acting on delivery.
			action := calls[i+1]
			command, _ := action.input["command"].(string)
			words, err := nativeLiveCommandWords(command)
			if action.name != "Bash" || err != nil || !slices.Equal(words, expectedWords) {
				return fmt.Errorf("Main next action does not execute the exact original consumer command")
			}
			pre, post := 0, 0
			for _, performed := range rows {
				if !main(performed) || performed.Payload["tool_use_id"] != action.id || performed.Payload["tool_name"] != "Bash" || performed.Exit != 0 || performed.Started <= row.Finished {
					continue
				}
				input, _ := performed.Payload["tool_input"].(map[string]any)
				actualCommand, _ := input["command"].(string)
				actualWords, err := nativeLiveCommandWords(actualCommand)
				if err != nil || !slices.Equal(actualWords, expectedWords) {
					continue
				}
				if performed.Event == "PreToolUse" {
					pre++
				} else if performed.Event == "PostToolUse" {
					response, _ := performed.Payload["tool_response"].(map[string]any)
					if response["interrupted"] == true || response["is_error"] == true || response["exit_code"] != nil && response["exit_code"] != float64(0) && response["exit_code"] != 0 {
						return fmt.Errorf("original Main consumer action failed")
					}
					post++
				}
			}
			if pre != 1 || post != 1 {
				return fmt.Errorf("no actual Main consumer action Hook pair after context delivery: %d/%d", pre, post)
			}
			return nil
		}
	}
	return fmt.Errorf("no actual Main additionalContext after original completion with exact responsibility/input, accepted facts/failure and consumer command")
}
