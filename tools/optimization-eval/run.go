package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/processtree"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}
type caseResult struct {
	ID             string       `json:"id"`
	Version        string       `json:"version"`
	Mode           string       `json:"mode"`
	Status         string       `json:"status"`
	Reason         string       `json:"reason,omitempty"`
	Target         target       `json:"mechanism_target"`
	Argv           []string     `json:"argv"`
	StartedAt      string       `json:"started_at"`
	ElapsedMS      int64        `json:"elapsed_ms"`
	EventsRef      string       `json:"events_ref,omitempty"`
	EventsSHA      string       `json:"events_sha256,omitempty"`
	StderrRef      string       `json:"stderr_ref,omitempty"`
	StderrSHA      string       `json:"stderr_sha256,omitempty"`
	CaseSHA        string       `json:"case_sha256"`
	Usage          availability `json:"usage"`
	ReadSkillTrace availability `json:"read_skill_trace"`
}
type runReport struct {
	Version         string         `json:"version"`
	RunID           string         `json:"run_id"`
	Mode            string         `json:"mode"`
	Status          string         `json:"status"`
	Source          sourceManifest `json:"source"`
	Cases           []caseResult   `json:"cases"`
	Scope           string         `json:"scope"`
	Artifacts       string         `json:"artifacts"`
	NotAPublication bool           `json:"not_a_publication"`
}
type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

type boundedLog struct {
	Writer    io.Writer
	Remaining int64
	Cancel    context.CancelFunc
}

func (w *boundedLog) Write(data []byte) (int, error) {
	if int64(len(data)) > w.Remaining {
		w.Cancel()
		return 0, fmt.Errorf("case evidence exceeds 64 MiB bound")
	}
	n, err := w.Writer.Write(data)
	w.Remaining -= int64(n)
	return n, err
}

func runPattern(test string) string {
	parts := strings.Split(test, "/")
	for i := range parts {
		parts[i] = "^" + regexp.QuoteMeta(parts[i]) + "$"
	}
	return strings.Join(parts, "/")
}

func evaluateEvents(data []byte, expected target) (string, string) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	ran, passed, skipped, failed := false, false, false, false
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return "error", "malformed Go test JSON event"
		}
		if event.Package != "github.com/entroforge/go-system-builder/internal/cli" {
			continue
		}
		if event.Test == expected.Test {
			switch event.Action {
			case "run":
				ran = true
			case "pass":
				passed = true
			case "skip":
				skipped = true
			case "fail":
				failed = true
			}
		}
		if (event.Test == expected.Test || strings.HasPrefix(event.Test, expected.Test+"/")) && event.Action == "skip" {
			skipped = true
		}
		if event.Action == "fail" {
			failed = true
		}
	}
	if err := scanner.Err(); err != nil {
		return "error", "Go event exceeds bounded decoder"
	}
	if failed {
		return "fail", "original target/package reported failure"
	}
	if skipped {
		return "skip", "required target or descendant skipped; not success"
	}
	if !ran || !passed {
		return "error", "exact required Test/subtest did not run and pass"
	}
	return "pass", "exact original mechanism target ran and passed; not model acceptance"
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".result-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func executeCase(ctx context.Context, source, caseDir, mode string, c evalCase) caseResult {
	caseBytes, _ := json.Marshal(c)
	r := caseResult{ID: c.ID, Version: c.Version, Mode: mode, Status: "not_run", Target: c.Oracle, Argv: []string{}, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), CaseSHA: hash(caseBytes), Usage: availability{false, "no actual model usage provider"}, ReadSkillTrace: availability{false, "no actual model Read/Skill trace provider"}}
	if err := os.Mkdir(caseDir, 0700); err != nil {
		r.Status = "error"
		r.Reason = err.Error()
		return r
	}
	if err := writeJSON(filepath.Join(caseDir, "case.json"), c); err != nil {
		r.Status = "error"
		r.Reason = err.Error()
		return r
	}
	if err := writeJSON(filepath.Join(caseDir, "public-inputs.json"), publicView(c)); err != nil {
		r.Status = "error"
		r.Reason = err.Error()
		return r
	}
	if mode == "real_host" {
		r.Reason = "original real host issuance/oracle isolation adapter unavailable; mechanism execution is not substituted"
		return r
	}
	if ctx.Err() != nil {
		r.Reason = "run interrupted before case"
		return r
	}
	for _, ref := range append(append([]string{}, c.PublicRefs...), c.FixtureRefs...) {
		info, err := os.Lstat(filepath.Join(source, filepath.FromSlash(ref)))
		if err != nil || !info.Mode().IsRegular() {
			r.Status = "error"
			r.Reason = "frozen declared source missing: " + ref
			return r
		}
	}
	r.Argv = []string{"go", "test", "-json", "-count=1", "-timeout", fmt.Sprintf("%ds", c.TimeoutSeconds), c.Oracle.Package, "-run", runPattern(c.Oracle.Test)}
	eventsPath, stderrPath := filepath.Join(caseDir, "events.jsonl"), filepath.Join(caseDir, "stderr.log")
	out, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		r.Status = "error"
		r.Reason = err.Error()
		return r
	}
	errout, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		out.Close()
		r.Status = "error"
		r.Reason = err.Error()
		return r
	}
	caseCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSeconds+15)*time.Second)
	defer cancel()
	command := exec.CommandContext(caseCtx, r.Argv[0], r.Argv[1:]...)
	command.Dir = source
	command.Env = append(os.Environ(), "GOPROXY=off")
	command.Stdout = &boundedLog{out, 64 << 20, cancel}
	command.Stderr = &boundedLog{errout, 64 << 20, cancel}
	started := time.Now()
	runErr := processtree.Run(command)
	r.ElapsedMS = time.Since(started).Milliseconds()
	out.Close()
	errout.Close()
	data, readErr := os.ReadFile(eventsPath)
	stderr, stderrErr := os.ReadFile(stderrPath)
	r.EventsRef = "events.jsonl"
	r.EventsSHA = hash(data)
	r.StderrRef = "stderr.log"
	r.StderrSHA = hash(stderr)
	if readErr != nil || stderrErr != nil {
		r.Status = "error"
		r.Reason = "cannot read original command evidence"
		return r
	}
	if caseCtx.Err() != nil {
		r.Status = "timed_out"
		r.Reason = caseCtx.Err().Error()
		return r
	}
	r.Status, r.Reason = evaluateEvents(data, c.Oracle)
	if runErr != nil {
		r.Status = "fail"
		r.Reason = "original Go command exited unsuccessfully: " + runErr.Error()
	}
	return r
}

func runEvaluation(ctx context.Context, root, output, mode, selection string, catalog []evalCase) (runReport, error) {
	if mode != "mechanism" && mode != "real_host" {
		return runReport{}, fmt.Errorf("mode must be mechanism or real_host")
	}
	selected := []evalCase{}
	for _, c := range catalog {
		if selection == "all" || selection == c.ID {
			selected = append(selected, c)
		}
	}
	if len(selected) == 0 {
		return runReport{}, fmt.Errorf("unknown case %q", selection)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return runReport{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return runReport{}, err
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		return runReport{}, err
	}
	dir, err := os.MkdirTemp(output, "run-")
	if err != nil {
		return runReport{}, err
	}
	report := runReport{Version: "1.0.0", RunID: filepath.Base(dir), Mode: mode, Status: "running", Cases: []caseResult{}, Artifacts: dir, Scope: "original CLI/Hook mechanism fixtures; seeded domain setup is not autonomous model work; no throughput, token, style or quality improvement claim", NotAPublication: true}
	defer func() { writeJSON(filepath.Join(dir, "result.json"), report) }()
	source := filepath.Join(dir, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		return report, err
	}
	report.Source, err = frozenSnapshot(root, source)
	if err != nil {
		report.Status = "error"
		return report, err
	}
	if err := writeJSON(filepath.Join(dir, "source-manifest.json"), report.Source); err != nil {
		report.Status = "error"
		return report, err
	}
	if err := writeJSON(filepath.Join(dir, "catalog.json"), selected); err != nil {
		report.Status = "error"
		return report, err
	}
	for _, c := range selected {
		result := executeCase(ctx, source, filepath.Join(dir, c.ID), mode, c)
		report.Cases = append(report.Cases, result)
		if err := writeJSON(filepath.Join(dir, c.ID, "result.json"), result); err != nil {
			report.Status = "error"
			return report, err
		}
		if err := writeJSON(filepath.Join(dir, "result.json"), report); err != nil {
			report.Status = "error"
			return report, err
		}
	}
	if err := verifySnapshot(source, report.Source); err != nil {
		report.Status = "error"
		return report, err
	}
	report.Status = "complete"
	if err := writeJSON(filepath.Join(dir, "result.json"), report); err != nil {
		report.Status = "error"
		return report, err
	}
	return report, nil
}
