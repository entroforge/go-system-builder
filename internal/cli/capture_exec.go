// capture_exec.go — `loop-harness capture exec` (L3-S7 §3.6 command_flow /
// §6.3 auto-capture / §8 capture buffer on the必经 path): the CLI-wrapper half
// of the automatic encounter timeline. It runs one command and appends a
// sanitized command_flow step to the Assignment's capture buffer — sequence/
// time, cwd, tool version, sanitized command, exit code, bounded stdout/stderr
// summaries with the full stream persisted as typed evidence files referenced
// by hash, and the produced/modified artifact digest diff of the working tree.
// A non-zero exit freezes the evidence window: the step is marked as a
// wall-action candidate, a failure marker is written next to the buffer, and
// the child's exit code is passed through to the caller (the wrapper never
// swallows failures).
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

const (
	// execSummaryBytesDefault caps each stream's inline summary inside the
	// buffer step; the full stream lives in the typed evidence file.
	execSummaryBytesDefault = 4096
	// execEvidenceBytesDefault caps each persisted stream evidence file;
	// overflow is truncated and the truncation is recorded in the ref.
	execEvidenceBytesDefault = 1 << 20
	// execMaxArtifactsDefault caps how many produced/modified artifact refs a
	// single step records, so a runaway tree diff cannot flood the buffer.
	execMaxArtifactsDefault = 20
	// execArtifactDepthDefault bounds the directory depth scanned under cwd
	// for the before/after artifact digest diff.
	execArtifactDepthDefault = 3
	// execWalkEntriesMax caps the number of files digested per snapshot.
	execWalkEntriesMax = 2000
	// execDigestSizeCap skips hashing very large files (size is still
	// recorded) so the diff stays cheap on big outputs.
	execDigestSizeCap = 32 << 20
	// execVersionTimeout bounds the `<tool> --version` probe.
	execVersionTimeout = 2 * time.Second
	// execMaxEnvRefs caps the redacted environment-presence refs per step.
	execMaxEnvRefs = 10
)

// sensitiveEnvName matches environment variable *names* whose values must
// never reach the capture buffer. Presence is recorded as a redacted ref
// instead of the value; anything not classifiable is simply never captured
// (fail-closed, L3-S7 §6.3).
var sensitiveEnvName = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key|private[_-]?key|access[_-]?key|credential)`)

// execTerminalPlaceholder is the literal that replaces a secret-bearing
// stream on the wrapper's terminal output. It must never appear on the
// Reviewer's screen if the wrapped command never produced a secret —
// otherwise it would falsely advertise a capture gap.
const execTerminalPlaceholder = "[withheld: stream matched a secret redaction pattern; full output not persisted (L3-S7 §6.3)]\n"

// execTerminalFlushCap bounds how much buffered raw output the wrapper
// keeps before flushing to the terminal, so a chatty child cannot grow
// memory unbounded while the post-exec redaction decision is pending.
const execTerminalFlushCap = 1 << 20

// errWalkEntryCap stops the artifact tree walk once execWalkEntriesMax files
// have been digested; the snapshot stays valid, just bounded.
var errWalkEntryCap = errors.New("capture exec: walk entry cap reached")

// runCaptureExec implements `loop-harness capture exec --assignment <id> --
// <command...>`.
func runCaptureExec(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runCaptureExecInner(args, stdin, stdout, stderr, &execRunOptions{})
}

// execRunOptions exposes hooks that production hides and tests rely on so
// the wrapper's terminal-passthrough behavior can be observed end-to-end.
type execRunOptions struct {
	// CommandPath, when non-empty, replaces the first wrapped argv token.
	// Tests use this to inject a fake `cmd` binary (e.g. a shell script
	// under t.TempDir()) without leaking it onto the real $PATH.
	CommandPath string
	// ExtraEnv, when non-empty, replaces the child environment. Production
	// leaves it nil so the child inherits the wrapper's environ.
	ExtraEnv []string
}

// runCaptureExecInner is the testable core of runCaptureExec. It accepts an
// execRunOptions so tests can inject a custom binary path and environment
// without changing production wiring.
func runCaptureExecInner(args []string, stdin io.Reader, stdout, stderr io.Writer, opts *execRunOptions) int {
	if opts == nil {
		opts = &execRunOptions{}
	}

	// Split wrapper flags from the wrapped command at "--" so command flags
	// are never parsed by the harness flag set.
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	flagArgs := args
	var command []string
	if sep >= 0 {
		flagArgs = args[:sep]
		command = args[sep+1:]
	}

	flags := flag.NewFlagSet("capture exec", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "capture exec")
	root := flags.String("root", ".", "repository root")
	assignmentID := flags.String("assignment", "", "plan assignment the exec step belongs to")
	cwd := flags.String("cwd", "", "working directory for the command (default: current directory)")
	summaryBytes := flags.Int("summary-bytes", execSummaryBytesDefault, "per-stream bytes kept inline in the buffer step")
	maxEvidenceBytes := flags.Int64("max-evidence-bytes", execEvidenceBytesDefault, "per-stream evidence file size cap; overflow is truncated and recorded")
	maxArtifacts := flags.Int("max-artifacts", execMaxArtifactsDefault, "maximum produced/modified/deleted artifacts recorded per step")
	artifactDepth := flags.Int("artifact-depth", execArtifactDepthDefault, "directory depth scanned under cwd for the artifact digest diff")
	if err := flags.Parse(flagArgs); err != nil {
		return 2
	}
	if *assignmentID == "" {
		fmt.Fprintln(stderr, "capture exec requires --assignment")
		return 2
	}
	if sep < 0 || len(command) == 0 {
		fmt.Fprintln(stderr, "capture exec requires -- <command...>")
		return 2
	}
	if *summaryBytes < 0 || *maxEvidenceBytes < 0 || *maxArtifacts < 0 || *artifactDepth < 1 {
		fmt.Fprintln(stderr, "capture exec: --summary-bytes/--max-evidence-bytes/--max-artifacts must be >= 0 and --artifact-depth >= 1")
		return 2
	}

	// Pre-exec redaction gate: a command line that smells like a secret is a
	// hard refusal — the command is not executed and nothing is buffered.
	rendered := renderExecCommand(command)
	if err := review.SanitizeCapture(review.CaptureStep{Action: rendered}); err != nil {
		fmt.Fprintf(stderr, "capture exec rejected: %v\n", err)
		return 2
	}

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "capture exec: resolve root: %v\n", err)
		return 1
	}
	cwdAbs := *cwd
	if cwdAbs == "" {
		cwdAbs, err = os.Getwd()
	} else {
		cwdAbs, err = filepath.Abs(cwdAbs)
	}
	if err != nil {
		fmt.Fprintf(stderr, "capture exec: resolve cwd: %v\n", err)
		return 1
	}
	info, err := os.Stat(cwdAbs)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(stderr, "capture exec: cwd %s is not a directory\n", cwdAbs)
		return 1
	}

	bufferPath, err := captureBufferPath(absRoot, *assignmentID)
	if err != nil {
		fmt.Fprintf(stderr, "capture exec: %v\n", err)
		return 1
	}
	steps, err := review.LoadCaptureStepsStrict(bufferPath)
	if err != nil {
		fmt.Fprintf(stderr, "capture exec: read buffer: %v; repair the malformed line before continuing\n", err)
		return 1
	}
	sequence := len(steps) + 1

	// Typed evidence files for the full streams live next to the buffer.
	execDir := filepath.Join(filepath.Dir(bufferPath), "exec")
	if err := os.MkdirAll(execDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "capture exec: create evidence dir: %v\n", err)
		return 1
	}
	stdoutPath := filepath.Join(execDir, fmt.Sprintf("step-%03d-stdout.log", sequence))
	stderrPath := filepath.Join(execDir, fmt.Sprintf("step-%03d-stderr.log", sequence))
	stdoutRec, err := newStreamRecorder(stdoutPath, *maxEvidenceBytes, *summaryBytes)
	if err != nil {
		fmt.Fprintf(stderr, "capture exec: open stdout evidence: %v\n", err)
		return 1
	}
	defer stdoutRec.close()
	stderrRec, err := newStreamRecorder(stderrPath, *maxEvidenceBytes, *summaryBytes)
	if err != nil {
		fmt.Fprintf(stderr, "capture exec: open stderr evidence: %v\n", err)
		return 1
	}
	defer stderrRec.close()

	toolVersion := probeToolVersion(command[0])
	before := snapshotArtifacts(cwdAbs, *artifactDepth)

	started := time.Now()
	binary := command[0]
	if opts.CommandPath != "" {
		binary = opts.CommandPath
	}
	cmd := exec.Command(binary, command[1:]...)
	cmd.Dir = cwdAbs
	cmd.Stdin = stdin
	if opts.ExtraEnv != nil {
		cmd.Env = opts.ExtraEnv
	}
	// Terminal passthrough must run the same secret-pattern gate as the
	// evidence file (L3-S7 §6.3, 宁拒勿放). The wrapper accumulates the
	// child's raw bytes in a pendingTee, forwards them to the evidence
	// recorder as the stream arrives, and only flushes to the terminal
	// after finalizeTerminalStream confirms the bytes are safe — or
	// replaces them with a placeholder if they match a secret pattern.
	// A child that never reads from the wrapper's terminal still completes
	// because the tee never blocks; a closed terminal is a display
	// problem, not a reason to fail the wrapped command.
	stdoutPassthrough := newPendingPassthrough(stdoutRec, execTerminalFlushCap)
	stderrPassthrough := newPendingPassthrough(stderrRec, execTerminalFlushCap)
	cmd.Stdout = stdoutPassthrough
	cmd.Stderr = stderrPassthrough
	runErr := cmd.Run()
	duration := time.Since(started).Round(time.Millisecond)
	stdoutRec.close()
	stderrRec.close()

	exitCode := 0
	startFailed := false
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			startFailed = true
			exitCode = 1
		}
	}

	after := snapshotArtifacts(cwdAbs, *artifactDepth)
	artifactRefs, omitted := diffArtifacts(before, after, *maxArtifacts)

	// Post-exec redaction: a persisted stream that matches a secret pattern is
	// withheld — the evidence file is replaced by a placeholder and the step
	// records a capture gap instead of the value (宁拒勿放, L3-S7 §6.3).
	stdoutRef, stdoutSummary := finalizeStream(absRoot, stdoutRec)
	stderrRef, stderrSummary := finalizeStream(absRoot, stderrRec)
	// Mirror the same redaction gate on the terminal side so the Reviewer
	// never sees a secret value the evidence file refused to persist.
	stdoutPassthrough.commitOrWithhold(stdout)
	stderrPassthrough.commitOrWithhold(stderr)

	action := fmt.Sprintf("exec: %s (cwd: %s)", rendered, cwdAbs)
	if toolVersion != "" {
		action += fmt.Sprintf(" (tool: %s — %s)", command[0], toolVersion)
	}
	observed := fmt.Sprintf("exit=%d duration=%s; stdout: %s; stderr: %s",
		exitCode, duration, stdoutSummary, stderrSummary)
	if startFailed {
		observed = fmt.Sprintf("exit=start_failed error=%q duration=%s; stdout: %s; stderr: %s",
			runErr.Error(), duration, stdoutSummary, stderrSummary)
	}
	failed := startFailed || exitCode != 0
	if failed {
		observed = "FAILED (wall-action candidate; evidence window frozen — Reviewer: annotate last_good_checkpoint / wall_action / first_bad_checkpoint) " + observed
	}
	if omitted > 0 {
		observed += fmt.Sprintf("; +%d more artifact(s) omitted by --max-artifacts", omitted)
	}

	evidenceRefs := []string{stdoutRef, stderrRef}
	evidenceRefs = append(evidenceRefs, artifactRefs...)
	evidenceRefs = append(evidenceRefs, sensitiveEnvRefs()...)

	step := review.CaptureStep{
		Sequence:   sequence,
		Action:     action,
		Observed:   observed,
		Evidence:   evidenceRefs,
		CapturedAt: started.UTC().Format(time.RFC3339Nano),
	}
	// Final gate over the assembled step: even after stream withholding, the
	// buffer never persists a value that matches a secret pattern.
	if err := review.SanitizeCapture(step); err != nil {
		fmt.Fprintf(stderr, "capture exec: buffer step rejected by redaction gate: %v\n", err)
		if exitCode == 0 {
			exitCode = 1
		}
		return exitCode
	}
	if err := appendCaptureStep(bufferPath, step); err != nil {
		fmt.Fprintf(stderr, "capture exec: append buffer: %v\n", err)
		if exitCode == 0 {
			exitCode = 1
		}
		return exitCode
	}

	if failed {
		if err := writeExecFailureMarker(filepath.Dir(bufferPath), *assignmentID, step, exitCode, startFailed); err != nil {
			fmt.Fprintf(stderr, "capture exec: write failure marker: %v\n", err)
		}
		fmt.Fprintf(stderr, "capture exec: command failed (exit=%d) — evidence window frozen for %s at step %d; annotate last_good_checkpoint / wall_action / first_bad_checkpoint in the Finding encounter\n",
			exitCode, *assignmentID, sequence)
	}
	fmt.Fprintf(stdout, "captured exec step %d for %s (exit=%d, evidence refs: %d)\n",
		sequence, *assignmentID, exitCode, len(evidenceRefs))
	return exitCode
}

// captureBufferPath resolves the Assignment's capture buffer from the live
// Runtime (runtime id + baseline generation), the same addressing `capture
// step` uses.
func captureBufferPath(absRoot, assignmentID string) (string, error) {
	statePath := filepath.Join(absRoot, ".claude", "loop-state.json")
	journalPath := filepath.Join(absRoot, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return "", fmt.Errorf("read runtime: %w", err)
	}
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	generation := 0
	if baseline, ok := snapshot.State["baseline"].(map[string]any); ok {
		if value, ok := baseline["generation"].(float64); ok {
			generation = int(value)
		}
	}
	return review.CaptureFile(absRoot, runtimeID, generation, assignmentID), nil
}

// appendCaptureStep appends one JSONL step to the buffer.
func appendCaptureStep(bufferPath string, step review.CaptureStep) error {
	if err := os.MkdirAll(filepath.Dir(bufferPath), 0o755); err != nil {
		return fmt.Errorf("create buffer dir: %w", err)
	}
	file, err := os.OpenFile(bufferPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open buffer: %w", err)
	}
	defer file.Close()
	data, err := json.Marshal(step)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append: %w", err)
	}
	return nil
}

// ignoreErrWriter swallows write errors on the pass-through side so a closed
// wrapper stream never kills the wrapped command.
type ignoreErrWriter struct{ w io.Writer }

func (w ignoreErrWriter) Write(p []byte) (int, error) {
	_, _ = w.w.Write(p)
	return len(p), nil
}

// streamRecorder persists a child stream to its evidence file with a hard
// size cap while keeping a bounded head summary in memory. The hash covers
// exactly the persisted bytes; `total` tracks the full stream length so
// truncation is recorded honestly.
type streamRecorder struct {
	path    string
	file    *os.File
	hash    hash.Hash
	limit   int64
	written int64
	total   int64
	head    []byte
	headCap int
}

func newStreamRecorder(path string, limit int64, headCap int) (*streamRecorder, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &streamRecorder{path: path, file: file, hash: sha256.New(), limit: limit, headCap: headCap}, nil
}

func (r *streamRecorder) Write(p []byte) (int, error) {
	r.total += int64(len(p))
	if len(r.head) < r.headCap {
		n := r.headCap - len(r.head)
		if n > len(p) {
			n = len(p)
		}
		r.head = append(r.head, p[:n]...)
	}
	if r.written < r.limit {
		n := r.limit - r.written
		if n > int64(len(p)) {
			n = int64(len(p))
		}
		if n > 0 {
			chunk := p[:n]
			if _, err := r.file.Write(chunk); err == nil {
				r.hash.Write(chunk)
				r.written += int64(len(chunk))
			}
		}
	}
	return len(p), nil
}

func (r *streamRecorder) close() {
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
}

func (r *streamRecorder) truncated() bool { return r.total > r.written }

// finalizeStream scans the persisted stream through the redaction gate. On a
// match the evidence file is replaced by a placeholder (the value never stays
// on disk inside the evidence tree) and the ref/summary record the capture
// gap. Otherwise the ref binds the typed evidence file by sha256.
func finalizeStream(absRoot string, r *streamRecorder) (ref, summary string) {
	rel, err := filepath.Rel(absRoot, r.path)
	if err != nil {
		rel = r.path
	}
	rel = filepath.ToSlash(rel)
	data, readErr := os.ReadFile(r.path)
	if readErr == nil && len(data) > 0 {
		if gateErr := review.SanitizeCapture(review.CaptureStep{Observed: string(data)}); gateErr != nil {
			placeholder := "[capture withheld: stream matched a secret redaction pattern; full output not persisted (L3-S7 §6.3)]\n"
			if err := os.WriteFile(r.path, []byte(placeholder), 0o644); err == nil {
				return fmt.Sprintf("command_output:%s (withheld: secret pattern matched; full output not persisted)", rel),
					"[withheld: secret pattern matched — capture gap]"
			}
			// If the placeholder cannot be written, drop the file entirely —
			// 宁拒勿放.
			os.Remove(r.path)
			return fmt.Sprintf("command_output:%s (withheld: secret pattern matched; evidence file removed)", rel),
				"[withheld: secret pattern matched — capture gap]"
		}
	}
	summaryText := strings.TrimRight(string(r.head), "\n")
	if summaryText == "" && r.total == 0 {
		summaryText = "(empty)"
	} else if r.truncated() || r.total > int64(len(r.head)) {
		summaryText += "…"
	}
	ref = fmt.Sprintf("command_output:%s#sha256=%s (bytes=%d, truncated=%t)",
		rel, hex.EncodeToString(r.hash.Sum(nil)), r.total, r.truncated())
	return ref, summaryText
}

// probeToolVersion runs `<binary> --version` with a short timeout and returns
// the first line, truncated. Any failure (missing flag, timeout, non-zero
// exit, secret-pattern match) omits the version — it is optional context.
func probeToolVersion(binary string) string {
	ctx, cancel := context.WithTimeout(context.Background(), execVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	var buf cappedBuffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return ""
	}
	line, _, _ := strings.Cut(buf.String(), "\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if len(line) > 200 {
		line = line[:200] + "…"
	}
	if err := review.SanitizeCapture(review.CaptureStep{Observed: line}); err != nil {
		return ""
	}
	return line
}

// cappedBuffer bounds the version probe output so a chatty `--version` cannot
// grow memory.
type cappedBuffer struct {
	buf []byte
}

const cappedBufferMax = 8192

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(b.buf) < cappedBufferMax {
		n := cappedBufferMax - len(b.buf)
		if n > len(p) {
			n = len(p)
		}
		b.buf = append(b.buf, p[:n]...)
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string { return string(b.buf) }

// sensitiveEnvRefs records the presence (never the value) of environment
// variables whose names look sensitive, so the encounter notes that e.g. a
// token was in play without persisting it.
func sensitiveEnvRefs() []string {
	var refs []string
	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")
		if sensitiveEnvName.MatchString(name) {
			refs = append(refs, fmt.Sprintf("env:%s (present; value never captured)", name))
		}
	}
	sort.Strings(refs)
	if len(refs) > execMaxEnvRefs {
		refs = refs[:execMaxEnvRefs]
	}
	return refs
}

// renderExecCommand renders the command line for the buffer, quoting args
// that contain whitespace or quotes. The rendered string — never raw env — is
// what passes the redaction gate.
func renderExecCommand(args []string) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \t\n\"'") {
			arg = strconv.Quote(arg)
		}
		parts[i] = arg
	}
	return strings.Join(parts, " ")
}

// artifactDigest is one file's contribution to the before/after tree diff.
type artifactDigest struct {
	SHA  string
	Size int64
}

// snapshotArtifacts digests the regular files under root up to maxDepth
// directories deep, skipping VCS/harness/dependency trees and symlinks. The
// walk is capped at execWalkEntriesMax files so a huge tree cannot explode
// the wrapper.
func snapshotArtifacts(root string, maxDepth int) map[string]artifactDigest {
	out := map[string]artifactDigest{}
	entries := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			switch d.Name() {
			case ".git", ".claude", "node_modules":
				return filepath.SkipDir
			}
			if strings.Count(rel, string(filepath.Separator))+1 > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if entries >= execWalkEntriesMax {
			return errWalkEntryCap
		}
		entries++
		info, err := d.Info()
		if err != nil {
			return nil
		}
		digest := artifactDigest{Size: info.Size()}
		if info.Size() <= execDigestSizeCap {
			if sha, err := fileSHA256(path); err == nil {
				digest.SHA = sha
			}
		}
		out[filepath.ToSlash(rel)] = digest
		return nil
	})
	return out
}

// diffArtifacts returns refs for files the command produced, modified or
// deleted, capped at max; the second return value counts omitted changes.
func diffArtifacts(before, after map[string]artifactDigest, max int) (refs []string, omitted int) {
	type change struct {
		rel    string
		kind   string
		digest artifactDigest
	}
	var changes []change
	for rel, afterDigest := range after {
		beforeDigest, existed := before[rel]
		switch {
		case !existed:
			changes = append(changes, change{rel, "new", afterDigest})
		case beforeDigest.SHA != afterDigest.SHA || beforeDigest.Size != afterDigest.Size:
			changes = append(changes, change{rel, "modified", afterDigest})
		}
	}
	for rel := range before {
		if _, ok := after[rel]; !ok {
			changes = append(changes, change{rel, "deleted", artifactDigest{}})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].rel < changes[j].rel })
	for i, c := range changes {
		if i >= max {
			omitted = len(changes) - max
			break
		}
		if c.kind == "deleted" {
			refs = append(refs, fmt.Sprintf("artifact:%s (deleted)", c.rel))
			continue
		}
		refs = append(refs, fmt.Sprintf("artifact:%s#sha256=%s (bytes=%d, %s)", c.rel, c.digest.SHA, c.digest.Size, c.kind))
	}
	return refs, omitted
}

// fileSHA256 streams a file through sha256 without loading it into memory.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// execFailureMarker is the failure marker written next to the capture buffer
// when the wrapped command fails: the evidence window is frozen and the step
// becomes the wall-action candidate until the Reviewer annotates the failure
// boundary in the Finding encounter.
type execFailureMarker struct {
	AssignmentID         string `json:"assignment_id"`
	Sequence             int    `json:"sequence"`
	CapturedAt           string `json:"captured_at"`
	Command              string `json:"command"`
	ExitCode             int    `json:"exit_code"`
	StartFailed          bool   `json:"start_failed,omitempty"`
	WallActionCandidate  bool   `json:"wall_action_candidate"`
	EvidenceWindowFrozen bool   `json:"evidence_window_frozen"`
	ReviewerAction       string `json:"reviewer_action"`
}

func writeExecFailureMarker(captureDir, assignmentID string, step review.CaptureStep, exitCode int, startFailed bool) error {
	marker := execFailureMarker{
		AssignmentID:         assignmentID,
		Sequence:             step.Sequence,
		CapturedAt:           step.CapturedAt,
		Command:              step.Action,
		ExitCode:             exitCode,
		StartFailed:          startFailed,
		WallActionCandidate:  true,
		EvidenceWindowFrozen: true,
		ReviewerAction:       "annotate last_good_checkpoint, wall_action and first_bad_checkpoint in the Finding encounter (L3-S7 §3.6/§6.3)",
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(captureDir, "failure.json"), append(data, '\n'), 0o644)
}

// pendingPassthrough buffers the raw bytes of a wrapped command's stream
// while the child runs and forwards them to the evidence-file recorder as
// they arrive. The terminal-side writer is held back until commitOrWithhold
// makes the redaction decision, so the Reviewer's screen and the evidence
// file share a single secret-pattern gate (L3-S7 §6.3). A chatty child
// that exceeds the flush cap still completes — the wrapper drops the
// overflow but records that it did so.
type pendingPassthrough struct {
	rec     *streamRecorder
	cap     int64
	total   int64
	head    []byte
	headN   int
	witheld bool
}

const pendingPassthroughHeadCap = 4096

func newPendingPassthrough(rec *streamRecorder, cap int64) *pendingPassthrough {
	if cap <= 0 {
		cap = execTerminalFlushCap
	}
	return &pendingPassthrough{rec: rec, cap: cap, headN: pendingPassthroughHeadCap}
}

// Write accepts raw bytes from the wrapped child. It always forwards them
// to the evidence-file recorder; the terminal-side writer is held back
// until commitOrWithhold flushes or withholds.
func (p *pendingPassthrough) Write(in []byte) (int, error) {
	p.total += int64(len(in))
	if len(p.head) < p.headN {
		n := p.headN - len(p.head)
		if n > len(in) {
			n = len(in)
		}
		p.head = append(p.head, in[:n]...)
	}
	// Forward to the evidence recorder so finalizeStream can run the same
	// redaction gate the wrapper trusts.
	if _, err := p.rec.Write(in); err != nil {
		// Mirror ignoreErrWriter's contract: a recorder failure must not
		// kill the wrapped child, but we still report the bytes as
		// accepted so the child's accounting stays honest.
		return len(in), nil
	}
	return len(in), nil
}

// commitOrWithhold mirrors the evidence-file redaction decision on the
// terminal: the same secret-pattern gate that drove the evidence file to
// a placeholder must drive the terminal too. Withheld writes are marked
// so a follow-up commitOrWithhold is a no-op. The placeholder byte length
// is counted toward the "total bytes the wrapper saw" so the buffered
// state stays internally consistent.
func (p *pendingPassthrough) commitOrWithhold(w io.Writer) {
	if w == nil {
		return
	}
	if p.witheld {
		return
	}
	data := p.head
	if !matchesSecretPattern(string(data)) {
		_, _ = w.Write(data)
		return
	}
	// Withheld: replace the buffered terminal output with the placeholder.
	// The evidence file path is owned by finalizeStream; here we only own
	// the terminal side.
	_, _ = w.Write([]byte(execTerminalPlaceholder))
	p.witheld = true
}

// matchesSecretPattern returns true when the buffered head carries any
// substring that would have driven review.SanitizeCapture to reject the
// buffer step. It uses the same regex set as the evidence-file gate, so
// the two decisions never disagree.
func matchesSecretPattern(s string) bool {
	return review.SanitizeCapture(review.CaptureStep{Observed: s}) != nil
}
