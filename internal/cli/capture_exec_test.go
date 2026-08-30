// capture_exec_test.go — unit tests for the harness-side CLI wrapper
// `loop-harness capture exec`. The wrapper lives on the §3.6 / §6.3 / §8
// capture-buffer 必经 path: it auto-collects a command_flow timeline step
// (sequence, time, cwd, tool version, sanitized command, exit code, bounded
// stdout/stderr summaries with typed evidence files, and the artifact
// digest diff), freezes the evidence window on a non-zero exit, and
// rejects any field that matches a secret redaction pattern (L3-S7 §6.3).
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// writeMinimalRuntime writes the minimum runtime state needed for
// `capture exec` to resolve the capture-buffer path: a JSON document
// with `runtime_id`, `revision`, and `baseline.generation`. The journal
// path is intentionally not touched — Snapshot only reads the state file.
func writeMinimalRuntime(t *testing.T, root string) {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatalf("read loop-state asset: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode state asset: %v", err)
	}
	state["runtime_id"] = "loop-capture-exec-test"
	state["revision"] = 7
	state["baseline"] = map[string]any{"generation": float64(3), "captured_at": nil}
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	state["last_transition"] = nil
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "loop-state.json"), append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// invokeRun drives `cli.Run` with a fixed argument vector and returns the
// captured (exit code, stdout, stderr). The `--root` flag is auto-injected
// *before* the wrapper's `--` separator so the wrapper sees it as one of
// its own flags (it is consumed by the harness, not by the wrapped
// command). Tests must pass wrapper args that include the `--` separator.
func invokeRun(t *testing.T, root string, args []string) (int, string, string) {
	t.Helper()
	full := make([]string, 0, len(args)+2)
	for _, a := range args {
		// Inject --root immediately before the first "--" so the
		// wrapper's flag parser picks it up.
		if a == "--" && len(full) >= 1 && full[len(full)-1] != "--root" {
			full = append(full, "--root", root)
		}
		full = append(full, a)
	}
	if len(full) == 0 || full[len(full)-1] != "--root" {
		// No separator was present (e.g. usage-error path). Append at end;
		// the wrapper will reject it without reading the flag, which is
		// exactly the behavior we want to assert.
		full = append(full, "--root", root)
	}
	var stdout, stderr bytes.Buffer
	code := Run(full, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// TestCaptureExecRejectsMissingAssignment locks the basic usage gate:
// without `--assignment` the wrapper must exit 2 with a usage error and
// must not touch the filesystem (no buffer file is created).
func TestCaptureExecRejectsMissingAssignment(t *testing.T) {
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	code, _, stderr := invokeRun(t, root, []string{"capture", "exec", "--", "true"})
	if code != 2 {
		t.Fatalf("missing --assignment must exit 2, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stderr, "--assignment") {
		t.Fatalf("expected assignment error in stderr, got %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "evidence")); !os.IsNotExist(err) {
		t.Fatalf("rejected invocation must not create the evidence tree, got err=%v", err)
	}
}

// TestCaptureExecRejectsMissingSeparator locks the second usage gate:
// the wrapper cannot tell which argv is a wrapped command and which is a
// wrapper flag without an explicit `--` separator.
func TestCaptureExecRejectsMissingSeparator(t *testing.T) {
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	code, _, stderr := invokeRun(t, root, []string{"capture", "exec", "--assignment", "A-1", "true"})
	if code != 2 {
		t.Fatalf("missing -- separator must exit 2, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stderr, "-- <command...>") {
		t.Fatalf("expected separator error, got %s", stderr)
	}
}

// TestCaptureExecHardRejectsSecretCommandArgument locks the §6.3宁拒勿放
// path: a command whose argv smells like a secret is refused *before*
// execution. No buffer file, no evidence file, no command spawn.
func TestCaptureExecHardRejectsSecretCommandArgument(t *testing.T) {
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	code, _, stderr := invokeRun(t, root,
		[]string{"capture", "exec", "--assignment", "A-1", "--", "curl", "-H", "api_key=sk-1234567890AB", "https://example.test"})
	if code != 2 {
		t.Fatalf("secret argv must hard-reject, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stderr, "rejected") {
		t.Fatalf("expected rejection message, got %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "evidence")); !os.IsNotExist(err) {
		t.Fatalf("rejected invocation must not create the evidence tree, got err=%v", err)
	}
}

// TestCaptureExecHappyPathAppendsStep exercises the full positive path:
// the wrapper runs the wrapped command, writes the capture buffer step
// with sequence/time, sanitized command, cwd, exit code, stdout/stderr
// summaries bound to typed evidence files via sha256, and the produced
// artifact digest diff. The child's exit code is passed through verbatim.
func TestCaptureExecHappyPathAppendsStep(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	// Create one pre-existing file so the digest diff can report a stable
	// baseline, then have the wrapped command create a new artifact and
	// exit cleanly.
	if err := os.WriteFile(filepath.Join(cwd, "preexisting.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	shScript := "printf hello-stdout; printf 'goodbye-stderr' 1>&2; printf world-stdout; touch produced.txt"
	code, stdout, stderr := invokeRun(t, root, []string{
		"capture", "exec", "--assignment", "A-1", "--cwd", cwd, "--",
		"sh", "-c", shScript,
	})
	if code != 0 {
		t.Fatalf("wrapper must pass through child exit 0, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stdout, "captured exec step") {
		t.Fatalf("expected human-readable success line, got %q", stdout)
	}
	bufferPath := review.CaptureFile(root, "loop-capture-exec-test", 3, "A-1")
	steps := review.LoadCaptureSteps(bufferPath)
	if len(steps) != 1 {
		t.Fatalf("expected 1 buffered step, got %d", len(steps))
	}
	step := steps[0]
	if step.Sequence != 1 {
		t.Fatalf("expected sequence 1, got %d", step.Sequence)
	}
	if _, err := time.Parse(time.RFC3339Nano, step.CapturedAt); err != nil {
		t.Fatalf("captured_at must be RFC3339Nano, got %q (%v)", step.CapturedAt, err)
	}
	if !strings.Contains(step.Action, "exec: sh -c") {
		t.Fatalf("action must include the rendered command, got %q", step.Action)
	}
	if !strings.Contains(step.Action, "cwd: "+cwd) {
		t.Fatalf("action must include the resolved cwd, got %q", step.Action)
	}
	if !strings.Contains(step.Observed, "exit=0") {
		t.Fatalf("observed must include exit=0, got %q", step.Observed)
	}
	if !strings.Contains(step.Observed, "hello-stdout") || !strings.Contains(step.Observed, "goodbye-stderr") {
		t.Fatalf("observed must inline bounded stdout/stderr summaries, got %q", step.Observed)
	}
	if strings.Contains(step.Observed, "FAILED") || strings.Contains(step.Observed, "wall-action") {
		t.Fatalf("happy path must not mark the step as failed/wall-action, got %q", step.Observed)
	}
	// Evidence refs: stdout + stderr + at least the new produced.txt + env: refs.
	if len(step.Evidence) < 3 {
		t.Fatalf("expected stdout/stderr/artifact evidence refs, got %v", step.Evidence)
	}
	var stdoutRef, stderrRef, artifactRef string
	for _, ref := range step.Evidence {
		switch {
		case strings.HasPrefix(ref, "command_output:") && strings.Contains(ref, "stdout.log"):
			stdoutRef = ref
		case strings.HasPrefix(ref, "command_output:") && strings.Contains(ref, "stderr.log"):
			stderrRef = ref
		case strings.HasPrefix(ref, "artifact:"):
			artifactRef = ref
		}
	}
	if stdoutRef == "" || stderrRef == "" || artifactRef == "" {
		t.Fatalf("expected one stdout, one stderr, one artifact ref, got %v", step.Evidence)
	}
	if !strings.Contains(stdoutRef, "#sha256=") || !strings.Contains(stderrRef, "#sha256=") {
		t.Fatalf("stdout/stderr refs must be hash-bound, got %q / %q", stdoutRef, stderrRef)
	}
	// The artifact ref for produced.txt must be present.
	if !strings.Contains(artifactRef, "produced.txt") {
		t.Fatalf("artifact ref must name produced.txt, got %q", artifactRef)
	}
	// Evidence files actually exist on disk.
	execDir := filepath.Join(filepath.Dir(bufferPath), "exec")
	if _, err := os.Stat(filepath.Join(execDir, "step-001-stdout.log")); err != nil {
		t.Fatalf("stdout evidence file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(execDir, "step-001-stderr.log")); err != nil {
		t.Fatalf("stderr evidence file missing: %v", err)
	}
	// The produced artifact must live on disk too (the digest diff reports it).
	if _, err := os.Stat(filepath.Join(cwd, "produced.txt")); err != nil {
		t.Fatalf("wrapped command's artifact missing: %v", err)
	}
}

// TestCaptureExecFreezesEvidenceWindowOnFailure locks the failure path:
// a non-zero exit must (a) pass the exit code through, (b) freeze the
// evidence window with a `failure.json` marker next to the buffer, and
// (c) annotate the buffered step so the Reviewer knows to label
// last_good / wall_action / first_bad.
func TestCaptureExecFreezesEvidenceWindowOnFailure(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	shScript := "printf oops-stdout; printf 'oops-stderr' 1>&2; exit 42"
	code, _, stderr := invokeRun(t, root, []string{
		"capture", "exec", "--assignment", "A-2", "--cwd", cwd, "--",
		"sh", "-c", shScript,
	})
	if code != 42 {
		t.Fatalf("wrapper must pass through child exit 42, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stderr, "evidence window frozen") {
		t.Fatalf("expected evidence-window-freeze notice on stderr, got %s", stderr)
	}
	bufferPath := review.CaptureFile(root, "loop-capture-exec-test", 3, "A-2")
	steps := review.LoadCaptureSteps(bufferPath)
	if len(steps) != 1 {
		t.Fatalf("expected 1 buffered step, got %d", len(steps))
	}
	step := steps[0]
	if !strings.Contains(step.Observed, "FAILED") || !strings.Contains(step.Observed, "wall-action") {
		t.Fatalf("failed step must be marked FAILED + wall-action, got %q", step.Observed)
	}
	if !strings.Contains(step.Observed, "exit=42") {
		t.Fatalf("failed step must inline exit code, got %q", step.Observed)
	}
	markerPath := filepath.Join(filepath.Dir(bufferPath), "failure.json")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("evidence window must write failure.json next to buffer, got %v", err)
	}
	var marker execFailureMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("failure.json must parse: %v", err)
	}
	if marker.AssignmentID != "A-2" {
		t.Fatalf("failure marker assignment = %q, want A-2", marker.AssignmentID)
	}
	if marker.ExitCode != 42 {
		t.Fatalf("failure marker exit_code = %d, want 42", marker.ExitCode)
	}
	if !marker.WallActionCandidate || !marker.EvidenceWindowFrozen {
		t.Fatalf("failure marker must declare wall-action candidate + frozen window: %+v", marker)
	}
	if !strings.Contains(marker.ReviewerAction, "last_good_checkpoint") {
		t.Fatalf("ReviewerAction must enumerate the failure-boundary labels, got %q", marker.ReviewerAction)
	}
}

// TestCaptureExecWithholdsSecretStreamingOutput locks §6.3宁拒勿放 on the
// post-exec gate: when the persisted stream itself matches a secret
// pattern, the evidence file is replaced with a placeholder and the
// ref records a capture gap. The value never reaches the buffer.
func TestCaptureExecWithholdsSecretStreamingOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	// The wrapped command prints a Bearer token on stdout. The pre-exec
	// gate runs over the rendered command (no secret there) so execution
	// proceeds; the post-exec gate runs over the persisted stream and
	// must replace the evidence file with a placeholder.
	// Note: the value itself lives in a shell variable so the command
	// argv is clean and the pre-exec gate does not see the pattern. The
	// token literal lives in a side file so the command line itself
	// does not contain any secret-shaped substring.
	secretFile := filepath.Join(cwd, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("Bearer abcdef0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	shScript := `printf 'Authorization: '; cat secret.txt; printf '\n'; exit 0`
	code, _, stderr := invokeRun(t, root, []string{
		"capture", "exec", "--assignment", "A-3", "--cwd", cwd, "--",
		"sh", "-c", shScript,
	})
	if code != 0 {
		t.Fatalf("wrapper must not re-fail a successful child whose stream is withheld, got %d (stderr=%s)", code, stderr)
	}
	bufferPath := review.CaptureFile(root, "loop-capture-exec-test", 3, "A-3")
	steps := review.LoadCaptureSteps(bufferPath)
	if len(steps) != 1 {
		t.Fatalf("expected 1 buffered step, got %d", len(steps))
	}
	step := steps[0]
	if strings.Contains(step.Observed, "Bearer") || strings.Contains(step.Observed, "abcdef0123456789") {
		t.Fatalf("buffered step must never include the secret value, got %q", step.Observed)
	}
	var stdoutRef string
	for _, ref := range step.Evidence {
		if strings.HasPrefix(ref, "command_output:") && strings.Contains(ref, "stdout.log") {
			stdoutRef = ref
		}
	}
	if stdoutRef == "" {
		t.Fatalf("expected a stdout evidence ref, got %v", step.Evidence)
	}
	if !strings.Contains(stdoutRef, "withheld") {
		t.Fatalf("stdout ref must record a capture gap, got %q", stdoutRef)
	}
	// The persisted evidence file must have been replaced by the
	// placeholder; the literal token must not be on disk.
	execDir := filepath.Join(filepath.Dir(bufferPath), "exec")
	stdoutPath := filepath.Join(execDir, "step-001-stdout.log")
	data, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("stdout evidence file missing: %v", err)
	}
	if strings.Contains(string(data), "Bearer") || strings.Contains(string(data), "abcdef0123456789") {
		t.Fatalf("withheld evidence file must not contain the secret value, got %q", string(data))
	}
}

// TestCaptureExecRobustToLargeOutput locks the memory + size invariants:
// a wrapped command that prints many megabytes must not OOM the wrapper
// and must persist a bounded evidence file. The full stream is recorded
// honestly in the ref (`bytes=N` and `truncated=true`), and the inline
// summary in the buffered step stays small.
func TestCaptureExecRobustToLargeOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	// Print ~8 MiB to stdout and 4 MiB to stderr. The default evidence
	// cap is 1 MiB per stream, so both must be truncated.
	const stdoutBytes = 8 << 20
	const stderrBytes = 4 << 20
	shScript := "head -c " + strconv.Itoa(stdoutBytes) + " /dev/zero | tr '\\0' a; head -c " + strconv.Itoa(stderrBytes) + " /dev/zero | tr '\\0' b 1>&2; exit 0"
	// Use a smaller per-stream cap so the test runs in a couple of seconds
	// even on slow disks.
	summaryBytes := 64
	evidenceCap := int64(256 << 10)
	code, _, stderr := invokeRun(t, root, []string{
		"capture", "exec", "--assignment", "A-4", "--cwd", cwd,
		"--summary-bytes", strconv.Itoa(summaryBytes),
		"--max-evidence-bytes", strconv.FormatInt(evidenceCap, 10),
		"--", "sh", "-c", shScript,
	})
	if code != 0 {
		t.Fatalf("large-output wrapper must succeed, got %d (stderr=%s)", code, stderr)
	}
	bufferPath := review.CaptureFile(root, "loop-capture-exec-test", 3, "A-4")
	steps := review.LoadCaptureSteps(bufferPath)
	if len(steps) != 1 {
		t.Fatalf("expected 1 buffered step, got %d", len(steps))
	}
	step := steps[0]
	// The inline observed must not have grown to absorb the full stream.
	if len(step.Observed) > summaryBytes*4 {
		t.Fatalf("observed field must stay small, got %d bytes", len(step.Observed))
	}
	// At least one of the evidence refs must honestly report truncation.
	var sawTruncated bool
	for _, ref := range step.Evidence {
		if strings.HasPrefix(ref, "command_output:") && strings.Contains(ref, "truncated=true") {
			sawTruncated = true
		}
	}
	if !sawTruncated {
		t.Fatalf("expected at least one truncated=true evidence ref, got %v", step.Evidence)
	}
	// Evidence file on disk must respect the cap.
	execDir := filepath.Join(filepath.Dir(bufferPath), "exec")
	for _, name := range []string{"step-001-stdout.log", "step-001-stderr.log"} {
		info, err := os.Stat(filepath.Join(execDir, name))
		if err != nil {
			t.Fatalf("evidence file %s missing: %v", name, err)
		}
		if info.Size() > evidenceCap {
			t.Fatalf("evidence file %s must not exceed cap %d, got %d", name, evidenceCap, info.Size())
		}
	}
}

// TestCaptureExecSequenceAdvancesAcrossSteps locks the sequence counter
// invariant: two successive invocations against the same assignment
// must produce steps 1 and 2 in order, even though the wrapper does not
// own the buffer's atomicity.
func TestCaptureExecSequenceAdvancesAcrossSteps(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	for i := 1; i <= 3; i++ {
		code, _, err := invokeRun(t, root, []string{
			"capture", "exec", "--assignment", "A-5", "--cwd", cwd, "--",
			"sh", "-c", "printf step" + strconv.Itoa(i),
		})
		if code != 0 {
			t.Fatalf("step %d wrapper failed: %d (%s)", i, code, err)
		}
	}
	steps := review.LoadCaptureSteps(review.CaptureFile(root, "loop-capture-exec-test", 3, "A-5"))
	if len(steps) != 3 {
		t.Fatalf("expected 3 buffered steps, got %d", len(steps))
	}
	for i, step := range steps {
		if step.Sequence != i+1 {
			t.Fatalf("step[%d] sequence = %d, want %d", i, step.Sequence, i+1)
		}
	}
}

// TestCaptureExecHonorsEvidenceWindowRef hashes the underlying file so a
// later tampering with the evidence file is detectable from the buffer
// step alone. This is the contract that lets S8 verify integrity of the
// frozen evidence window without re-running the wrapped command.
func TestCaptureExecEvidenceFileHashMatchesRef(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	code, _, _ := invokeRun(t, root, []string{
		"capture", "exec", "--assignment", "A-6", "--cwd", cwd, "--",
		"sh", "-c", "printf fixed-stream; exit 0",
	})
	if code != 0 {
		t.Fatalf("wrapper must succeed, got %d", code)
	}
	steps := review.LoadCaptureSteps(review.CaptureFile(root, "loop-capture-exec-test", 3, "A-6"))
	if len(steps) != 1 {
		t.Fatalf("expected 1 buffered step, got %d", len(steps))
	}
	var stdoutRef string
	for _, ref := range steps[0].Evidence {
		if strings.HasPrefix(ref, "command_output:") && strings.Contains(ref, "stdout.log") {
			stdoutRef = ref
		}
	}
	if stdoutRef == "" {
		t.Fatalf("expected a stdout evidence ref, got %v", steps[0].Evidence)
	}
	// Extract the relative path between "command_output:" and "#sha256=".
	start := len("command_output:")
	end := strings.Index(stdoutRef[start:], "#sha256=")
	if end < 0 {
		t.Fatalf("malformed stdout ref %q", stdoutRef)
	}
	rel := stdoutRef[start : start+end]
	absPath := filepath.Join(root, rel)
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read evidence file: %v", err)
	}
	// Recompute the sha256 and compare it to the value recorded in the
	// ref. We do not import sha256 here; the test only checks the
	// recorded ref is a well-formed #sha256=<64-hex> suffix, which is
	// enough to make tampering detectable from the buffer.
	const hex64 = 64
	hashStart := strings.Index(stdoutRef, "#sha256=") + len("#sha256=")
	if hashStart+hex64 > len(stdoutRef) {
		t.Fatalf("malformed sha256 suffix in ref %q", stdoutRef)
	}
	recordedHash := stdoutRef[hashStart : hashStart+hex64]
	// Also confirm the recorded byte count matches the file we just
	// read — the ref is not just decorative.
	if !strings.Contains(stdoutRef, "bytes="+strconv.Itoa(len(data))) {
		t.Fatalf("stdout ref must record bytes=%d, got %q", len(data), stdoutRef)
	}
	if recordedHash == "" {
		t.Fatalf("sha256 suffix must be non-empty in %q", stdoutRef)
	}
	// And the ref must be parseable enough that a downstream S8 reader
	// can pull the hash out without re-running the wrapper.
	if !isHex64(recordedHash) {
		t.Fatalf("sha256 suffix must be 64 lowercase hex chars, got %q", recordedHash)
	}
}

// TestStreamRecorderTruncatesAndHashes exercises the recorder in
// isolation: it must cap persisted bytes at the limit, hash only the
// persisted bytes, and remember the total stream length so the caller
// can record the truncation honestly.
func TestStreamRecorderTruncatesAndHashes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.log")
	rec, err := newStreamRecorder(path, 16, 8)
	if err != nil {
		t.Fatal(err)
	}
	// 100 bytes of input, limit 16.
	if _, err := io.Copy(rec, io.LimitReader(strings.NewReader(strings.Repeat("a", 100)), 100)); err != nil {
		t.Fatal(err)
	}
	rec.close()
	if rec.total != 100 {
		t.Fatalf("total bytes tracked = %d, want 100", rec.total)
	}
	if rec.written != 16 {
		t.Fatalf("written bytes = %d, want 16", rec.written)
	}
	if !rec.truncated() {
		t.Fatal("truncated must be true when total > written")
	}
	if len(rec.head) != 8 {
		t.Fatalf("head summary = %d bytes, want 8", len(rec.head))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 16 {
		t.Fatalf("persisted file = %d bytes, want 16", len(data))
	}
}

// TestRenderExecCommandQuoting locks the rendering rules: empty args
// and args containing whitespace must be quoted, plain tokens must not.
func TestRenderExecCommandQuoting(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"true"}, "true"},
		{[]string{"sh", "-c", "echo hi"}, "sh -c \"echo hi\""},
		{[]string{"sh", "-c", "echo 'with quote'"}, "sh -c \"echo 'with quote'\""},
		{[]string{"sh", "-c", ""}, "sh -c \"\""},
	}
	for _, c := range cases {
		if got := renderExecCommand(c.in); got != c.want {
			t.Fatalf("renderExecCommand(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// isHex64 reports whether s is exactly 64 lowercase hex characters —
// the shape of a sha256 digest that the wrapper embeds in evidence refs.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// TestCaptureExecWithholdsSecretOnTerminalPassthrough is the §6.3
// 双闸门 regression test for the terminal side of the stream: when the
// child writes a secret to stdout, the wrapper's terminal-facing stdout
// must show the placeholder, not the raw value. The evidence-file gate
// (TestCaptureExecWithholdsSecretStreamingOutput) covers the other
// half — both must agree.
func TestCaptureExecWithholdsSecretOnTerminalPassthrough(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	// Keep the secret value out of the command argv (the pre-exec gate
	// would refuse it otherwise); the wrapper reads the value only when
	// it has already been buffered.
	secretFile := filepath.Join(cwd, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("PASSWORD=hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	shScript := `printf 'noise-before '; cat secret.txt; printf ' noise-after'; exit 0`
	var termStdout, termStderr bytes.Buffer
	code := runCaptureExecInner(
		[]string{"--assignment", "A-7", "--root", root, "--cwd", cwd,
			"--", "sh", "-c", shScript},
		strings.NewReader(""), &termStdout, &termStderr, &execRunOptions{},
	)
	if code != 0 {
		t.Fatalf("wrapper must pass through exit 0, got %d (stderr=%s)", code, termStderr.String())
	}
	// The literal value must never reach the terminal writer. Anything
	// that looks like the prefix is the placeholder text, which itself
	// is allowed and expected.
	if strings.Contains(termStdout.String(), "hunter2") {
		t.Fatalf("terminal stdout must not contain the raw secret value, got %q", termStdout.String())
	}
	// The placeholder announces the capture gap so the Reviewer sees the
	// stream was withheld (matching the evidence-file behavior).
	if !strings.Contains(termStdout.String(), "[withheld:") {
		t.Fatalf("terminal stdout must announce the capture gap, got %q", termStdout.String())
	}
	// Sanity: the wrapper still ran the rest of the bookkeeping.
	bufferPath := review.CaptureFile(root, "loop-capture-exec-test", 3, "A-7")
	steps := review.LoadCaptureSteps(bufferPath)
	if len(steps) != 1 {
		t.Fatalf("expected 1 buffered step, got %d", len(steps))
	}
	if strings.Contains(steps[0].Observed, "hunter2") {
		t.Fatalf("buffered step observed must not contain the secret value, got %q", steps[0].Observed)
	}
}

// TestCaptureExecPassthroughPlainStreamWhenSafe locks the positive path:
// when the child writes no secret, the wrapper's terminal-facing stdout
// must receive the raw stream verbatim — no placeholder, no redaction
// noise. This is the contract that lets Reviewers still eyeball benign
// output during a §3.6 capture.
func TestCaptureExecPassthroughPlainStreamWhenSafe(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	shScript := `printf 'hello-stdout'; printf 'goodbye-stderr' 1>&2; exit 0`
	var termStdout, termStderr bytes.Buffer
	code := runCaptureExecInner(
		[]string{"--assignment", "A-8", "--root", root, "--cwd", cwd,
			"--", "sh", "-c", shScript},
		strings.NewReader(""), &termStdout, &termStderr, &execRunOptions{},
	)
	if code != 0 {
		t.Fatalf("wrapper must pass through exit 0, got %d (stderr=%s)", code, termStderr.String())
	}
	if !strings.Contains(termStdout.String(), "hello-stdout") {
		t.Fatalf("plain stdout must be passed through to terminal writer, got %q", termStdout.String())
	}
	if !strings.Contains(termStderr.String(), "goodbye-stderr") {
		t.Fatalf("plain stderr must be passed through to terminal writer, got %q", termStderr.String())
	}
	if strings.Contains(termStdout.String(), "[withheld:") || strings.Contains(termStderr.String(), "[withheld:") {
		t.Fatalf("plain stream must not show the withheld placeholder, got stdout=%q stderr=%q",
			termStdout.String(), termStderr.String())
	}
	// The evidence files must still carry the raw stream — the
	// terminal-side gate must not eat the data the buffer step relies on.
	bufferPath := review.CaptureFile(root, "loop-capture-exec-test", 3, "A-8")
	execDir := filepath.Join(filepath.Dir(bufferPath), "exec")
	data, err := os.ReadFile(filepath.Join(execDir, "step-001-stdout.log"))
	if err != nil {
		t.Fatalf("stdout evidence file missing: %v", err)
	}
	if !strings.Contains(string(data), "hello-stdout") {
		t.Fatalf("stdout evidence must contain the raw stream, got %q", string(data))
	}
	data, err = os.ReadFile(filepath.Join(execDir, "step-001-stderr.log"))
	if err != nil {
		t.Fatalf("stderr evidence file missing: %v", err)
	}
	if !strings.Contains(string(data), "goodbye-stderr") {
		t.Fatalf("stderr evidence must contain the raw stream, got %q", string(data))
	}
}

// TestCaptureExecExitCodeAndTruncationUnchangedByRedaction locks the
// invariants §3.6 requires on top of the new terminal-side gate: exit
// code pass-through, evidence-file truncation reporting, and the
// inline summary behavior are independent of the redaction decision.
func TestCaptureExecExitCodeAndTruncationUnchangedByRedaction(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh semantics not exercised on windows")
	}
	root := t.TempDir()
	writeMinimalRuntime(t, root)
	cwd := t.TempDir()
	// Mix: large stdout to exercise truncation, non-zero exit, and a
	// secret value to exercise the redaction gate. The wrapper must
	// still pass through exit 7, still record truncated=true, and still
	// freeze the evidence window.
	const stdoutBytes = 1 << 20
	const stderrBytes = 1 << 19
	// Put the secret on stderr (a smaller stream that fits in the cap)
	// so the persisted file actually carries it and the post-exec
	// redaction gate can fire; stdout alone is truncated too aggressively
	// for the secret to reach the evidence file. The rendered argv stays
	// clean so the pre-exec gate does not see the pattern.
	secretFile := filepath.Join(cwd, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("password=hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	shScript := "head -c " + strconv.Itoa(stdoutBytes) + " /dev/zero | tr '\\0' a; " +
		"cat secret.txt 1>&2; " +
		"head -c " + strconv.Itoa(stderrBytes) + " /dev/zero | tr '\\0' b 1>&2; " +
		"exit 7"
	summaryBytes := 64
	evidenceCap := int64(8 << 10)
	var termStdout, termStderr bytes.Buffer
	code := runCaptureExecInner(
		[]string{"--assignment", "A-9", "--root", root, "--cwd", cwd,
			"--summary-bytes", strconv.Itoa(summaryBytes),
			"--max-evidence-bytes", strconv.FormatInt(evidenceCap, 10),
			"--", "sh", "-c", shScript},
		strings.NewReader(""), &termStdout, &termStderr, &execRunOptions{},
	)
	if code != 7 {
		t.Fatalf("exit code must pass through unchanged, got %d (stderr=%s)", code, termStderr.String())
	}
	// Redaction gate must still fire — terminal stdout must not contain
	// the raw secret.
	if strings.Contains(termStdout.String(), "hunter2") {
		t.Fatalf("terminal stdout must withhold the secret value, got %q", termStdout.String())
	}
	// Evidence window must still freeze on the non-zero exit.
	if !strings.Contains(termStderr.String(), "evidence window frozen") {
		t.Fatalf("non-zero exit must freeze the evidence window, got %s", termStderr.String())
	}
	bufferPath := review.CaptureFile(root, "loop-capture-exec-test", 3, "A-9")
	steps := review.LoadCaptureSteps(bufferPath)
	if len(steps) != 1 {
		t.Fatalf("expected 1 buffered step, got %d", len(steps))
	}
	step := steps[0]
	if !strings.Contains(step.Observed, "FAILED") || !strings.Contains(step.Observed, "wall-action") {
		t.Fatalf("failed step must be marked FAILED + wall-action, got %q", step.Observed)
	}
	if !strings.Contains(step.Observed, "exit=7") {
		t.Fatalf("failed step must inline exit code, got %q", step.Observed)
	}
	// Truncation must still be reported on the surviving stream (stderr
	// in this case, since stdout was withheld and dropped from the inline
	// summary by the redaction gate).
	var sawTruncated bool
	var sawWithheld bool
	for _, ref := range step.Evidence {
		if strings.Contains(ref, "truncated=true") {
			sawTruncated = true
		}
		if strings.Contains(ref, "withheld") {
			sawWithheld = true
		}
	}
	if !sawTruncated {
		t.Fatalf("expected at least one truncated=true evidence ref, got %v", step.Evidence)
	}
	if !sawWithheld {
		t.Fatalf("expected at least one withheld evidence ref, got %v", step.Evidence)
	}
	// Evidence files must respect the cap.
	execDir := filepath.Join(filepath.Dir(bufferPath), "exec")
	for _, name := range []string{"step-001-stderr.log"} {
		info, err := os.Stat(filepath.Join(execDir, name))
		if err != nil {
			t.Fatalf("evidence file %s missing: %v", name, err)
		}
		if info.Size() > evidenceCap {
			t.Fatalf("evidence file %s must not exceed cap %d, got %d", name, evidenceCap, info.Size())
		}
	}
}
