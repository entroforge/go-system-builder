package e2erun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func parseEvents(r *Receipt, selected map[string]bool) {
	decoder := json.NewDecoder(strings.NewReader(r.RawEvents))
	seen, started, finished := false, map[string]bool{}, false
	tests, ended := map[string]Test{}, map[string]bool{}
	steps := map[string][]Step{}
	inputSet := map[string]bool{}
	for _, input := range r.Inputs {
		inputSet[input.Path] = true
	}
	problem := func(text string) { r.Problems = append(r.Problems, text) }
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			problem("decode reporter event: " + err.Error())
			break
		}
		var event struct {
			Type       string   `json:"type"`
			Tests      []Test   `json:"tests"`
			TestID     string   `json:"test_id"`
			Retry      int      `json:"retry"`
			Status     string   `json:"status"`
			DurationMS float64  `json:"duration_ms"`
			Errors     []string `json:"errors"`
			Error      string   `json:"error"`
			Stream     string   `json:"stream"`
			Text       string   `json:"text"`
			Step       *Step    `json:"step"`
		}
		strict := json.NewDecoder(bytes.NewReader(raw))
		strict.DisallowUnknownFields()
		if err := strict.Decode(&event); err != nil {
			problem("invalid reporter event: " + err.Error())
			continue
		}
		if finished {
			problem("reporter event after end")
			continue
		}
		switch event.Type {
		case "begin":
			if seen {
				problem("duplicate collection begin")
				continue
			}
			seen = true
			for _, test := range event.Tests {
				if test.ID == "" {
					problem("empty test ID")
					continue
				}
				if _, dup := tests[test.ID]; dup {
					problem("duplicate test ID " + test.ID)
					continue
				}
				if _, err := relative(test.Path); err != nil || !inputSet[test.Path] {
					problem("test spec is outside the actual snapshot: " + test.Path)
					continue
				}
				if test.Browser != "chromium" && test.Browser != "firefox" && test.Browser != "webkit" {
					problem("unsupported browser " + test.Browser)
					continue
				}
				if err := validateCollectedTest(test); err != nil {
					problem(err.Error())
					continue
				}
				tests[test.ID] = test
				r.Tests = append(r.Tests, test)
			}
		case "test_begin":
			key := event.TestID + ":" + strconv.Itoa(event.Retry)
			if _, ok := tests[event.TestID]; !seen || !ok || started[key] || event.Retry < 0 {
				problem("invalid/duplicate test attempt begin " + key)
				continue
			}
			started[key] = true
		case "api_step":
			key := event.TestID + ":" + strconv.Itoa(event.Retry)
			if !started[key] || ended[key] || event.Step == nil || event.Step.Category != "pw:api" || event.Step.Title == "" || event.Step.DurationMS < 0 {
				problem("invalid browser API step " + key)
				continue
			}
			steps[key] = append(steps[key], *event.Step)
		case "test_end":
			key := event.TestID + ":" + strconv.Itoa(event.Retry)
			if !started[key] || ended[key] {
				problem("missing/duplicate attempt begin/end " + key)
				continue
			}
			ended[key] = true
			if event.DurationMS < 0 {
				problem("negative attempt duration")
				continue
			}
			switch event.Status {
			case "passed", "failed", "timedOut", "skipped", "interrupted":
			default:
				problem("unknown attempt status " + event.Status)
				continue
			}
			observedPage := false
			for _, step := range steps[key] {
				// This exact event was exercised against the pinned adapter.
				// User test.step titles have category=test.step and cannot
				// qualify. No string search over stdout or source is used.
				observedPage = observedPage || (step.Title == "Create page" && step.Error == "")
			}
			r.Attempts = append(r.Attempts, Attempt{ExecutionID: r.RunID + ":" + key, TestID: event.TestID, Retry: event.Retry, Status: event.Status, DurationMS: event.DurationMS, Errors: event.Errors, BrowserPageObserved: observedPage, Steps: append([]Step{}, steps[key]...)})
		case "error":
			problem("runner: " + event.Error)
		case "output":
			if !seen {
				problem("test output before collection")
			}
		case "end":
			finished = true
			if !seen {
				problem("reporter ended without collection")
			}
			if event.Status != "passed" && event.Status != "failed" {
				problem("runner did not finish normally: " + event.Status)
			}
			if event.Status == "passed" && r.ExitCode != 0 {
				problem("passed report with nonzero exit")
			}
			if event.Status == "failed" && r.ExitCode == 0 {
				problem("failed report with zero exit")
			}
		default:
			problem("unknown reporter event " + event.Type)
		}
	}
	if !seen || !finished {
		problem("missing begin/end reporter events")
	}
	if len(tests) == 0 {
		problem("runner collected zero tests")
	}
	if r.Mode == "collection" && len(r.Attempts) > 0 {
		problem("collection unexpectedly executed tests")
	}
	if r.Mode == "execution" {
		for id := range selected {
			if _, ok := tests[id]; !ok {
				problem("selected test missing from run: " + id)
			}
		}
		for id := range tests {
			if !selected[id] {
				problem("unassigned test executed or collected: " + id)
			}
		}
		for key := range started {
			if !ended[key] {
				problem("unfinished attempt " + key)
			}
		}
		for id := range tests {
			if !ended[id+":0"] {
				problem("test has no initial execution: " + id)
			}
		}
	}
	r.Complete = len(r.Problems) == 0
	if !r.Complete {
		return
	}
	r.Outcome = "pass"
	if r.ExitCode != 0 {
		r.Outcome = "fail"
	}
	if r.Mode == "execution" {
		for _, test := range r.Tests {
			if test.ExpectedStatus != "passed" {
				r.Outcome = "fail"
			}
		}
	}
	for _, attempt := range r.Attempts {
		if attempt.Status != "passed" || attempt.Retry != 0 {
			r.Outcome = "fail"
		}
	}
	if r.Mode == "collection" && r.ExitCode != 0 {
		r.Complete = false
		r.Outcome = "unknown"
		problem(fmt.Sprintf("collection exit %d", r.ExitCode))
	}
}

func validateCollectedTest(test Test) error {
	if test.SelectionPath == "" || len(test.TitlePath) == 0 {
		return fmt.Errorf("collected test %s has no exact selector", test.ID)
	}
	for _, title := range test.TitlePath {
		if title == "" {
			return fmt.Errorf("collected test %s has an empty title segment", test.ID)
		}
	}
	if test.ExpectedStatus != "passed" && test.ExpectedStatus != "failed" && test.ExpectedStatus != "skipped" {
		return fmt.Errorf("collected test %s has an unknown expected status", test.ID)
	}
	for _, ref := range test.CaseRefs {
		if !modulePath.MatchString(ref.ModuleRef) || ref.CaseID == "" || ref.OracleRef == "" || ref.Persona == "" || ref.DataProfile == "" {
			return fmt.Errorf("collected test %s has incomplete CASE/oracle/environment annotation", test.ID)
		}
	}
	return nil
}
