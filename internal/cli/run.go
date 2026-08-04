package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/audit"
	"github.com/entroforge/go-system-builder/internal/change"
	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	impactanalysis "github.com/entroforge/go-system-builder/internal/impact"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/releasegraph"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/verification"
)

// formatFailure renders a CLI command's failure to stderr in the form
// `<cmd>: <err>` and, when the error wraps a transition-engine guard failure,
// appends ` See .claude/bin/loop-harness.md#<lowercase(rule_id)>.` so a caller
// hitting a gate failure can deep-link straight to the spec. Mirrors the
// convention already enforced for hook payloads in
// `internal/hook/adapter.go` `message()`.
//
// Recognized rule-id sources (in order of preference):
//   - `guard <NAME> failed: ...`         → id = NAME.
//   - `guard <NAME> is not registered`   → id = NAME.
//
// Returns the formatted line; caller is responsible for writing to stderr.
func formatFailure(cmd string, err error) string {
	msg := err.Error()
	if id := extractRuleID(msg); id != "" {
		return fmt.Sprintf("%s: %s See %s#%s.", cmd, msg, transition.ManualTargetPath(), strings.ToLower(id))
	}
	return fmt.Sprintf("%s: %s", cmd, msg)
}

// extractRuleID scans a transition-engine error string for a known
// guard-failure pattern and returns the rule id (the guard name). Returns ""
// when no rule id can be located — callers fall back to plain error output.
func extractRuleID(msg string) string {
	for _, prefix := range []string{"guard ", "guard\t"} {
		idx := strings.Index(msg, prefix)
		if idx < 0 {
			continue
		}
		rest := msg[idx+len(prefix):]
		// "guard NAME failed:" or "guard NAME is not registered"
		for _, sep := range []string{" failed:", " is not registered"} {
			if end := strings.Index(rest, sep); end > 0 {
				return strings.TrimSpace(rest[:end])
			}
		}
	}
	return ""
}

// bindUsage wires a flag.FlagSet's Usage function so that --help / -h prints
// the canonical flag defaults plus a pointer to the gate-level manual.
// Invoked once per subcommand right after SetOutput so that every `flag -h`
// path is consistent. Mirrors the manual-anchor convention enforced for hook
// payloads in `internal/hook/adapter.go`.
func bindUsage(flags *flag.FlagSet, label string) {
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage of %s:\n", label)
		flags.PrintDefaults()
		fmt.Fprintf(flags.Output(), "\nSee %s (gate-level specification).\n", transition.ManualTargetPath())
	}
}

// printTopLevelUsage renders the top-level help text to stdout. Triggered by
// `loop-harness --help` / `-h` / `help`; mirrors the inline usage printed on
// `len(args) == 0` (which goes to stderr because that path indicates a usage
// error).
func printTopLevelUsage(stdout io.Writer) {
	fmt.Fprintln(stdout, "Usage: loop-harness <command> [flags]")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Top-level commands:")
	fmt.Fprintln(stdout, "  init        Initialize .claude/ harness state for a repository")
	fmt.Fprintln(stdout, "  req         Locked REQ operations (req bind)")
	fmt.Fprintln(stdout, "  status      Render current stage projection (read-only; coarse)")
	fmt.Fprintln(stdout, "  next        Render next transition + missing preconditions (coarse)")
	fmt.Fprintln(stdout, "  ready       Dry-run current Quality Gate checklist (diagnostics)")
	fmt.Fprintln(stdout, "  validate    Validate runtime + journal against schema")
	fmt.Fprintln(stdout, "  dry-run     Render an applied transition without writing")
	fmt.Fprintln(stdout, "  hook        Hook adapter entrypoints (PreToolUse, Stop, etc.)")
	fmt.Fprintln(stdout, "  doctor      Surface schema / manual / policy_ref / metrics gaps")
	fmt.Fprintln(stdout, "  runtime     Runtime helpers (including evidence recording and terminal rollover)")
	fmt.Fprintln(stdout, "  team        Team manifest + responsibility checks")
	fmt.Fprintln(stdout, "  impact      Evidence invalidation analysis")
	fmt.Fprintln(stdout, "  verification Verification round evaluators")
	fmt.Fprintln(stdout, "  release-graph Release-graph topological assertions")
	fmt.Fprintln(stdout, "  angles      Module-level angles registry (list/commit/retract/revive/audit)")
	fmt.Fprintln(stdout, "  e2e-coverage  Score E2E scenario inventory fidelity (REQ-039)")
	fmt.Fprintln(stdout, "  manual      Render the gate-level manual")
	fmt.Fprintln(stdout, "  explain     Per-transition details (explain <TR-xxx>)")
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Manual: see %s (gate-level specification).\n", transition.ManualTargetPath())
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: loop-harness <init|req|status|next|ready|validate|dry-run|hook|doctor|runtime|team|impact|verification|release-graph|angles|e2e-coverage|manual|explain>")
		fmt.Fprintln(stderr, "manual:  see .claude/bin/loop-harness.md (gate-level specification)")
		fmt.Fprintln(stderr, "explain: loop-harness explain <TR-xxx> (per-transition details)")
		return 2
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printTopLevelUsage(stdout)
		return 0
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "req":
		return runREQ(args[1:], stdout, stderr)
	case "status":
		return runProjection(args[1:], false, stdout, stderr)
	case "next":
		return runProjection(args[1:], true, stdout, stderr)
	case "ready":
		return runReady(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "dry-run":
		return runDryRun(args[1:], stdout, stderr)
	case "hook":
		return runHook(args[1:], stdin, stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "runtime":
		return runRuntime(args[1:], stdout, stderr)
	case "team":
		return runTeam(args[1:], stdout, stderr)
	case "impact":
		return runImpact(args[1:], stdout, stderr)
	case "verification":
		return runVerification(args[1:], stdout, stderr)
	case "release-graph":
		return runReleaseGraph(args[1:], stdout, stderr)
	case "manual":
		return runManual(args[1:], stdout, stderr)
	case "explain":
		return runExplain(args[1:], stdout, stderr)
	case "angles":
		return runAngles(args[1:], stdout, stderr)
	case "e2e-coverage":
		return runE2ECoverage(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func runREQ(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "bind" {
		fmt.Fprintln(stderr, "req requires <bind>")
		return 2
	}
	flags := flag.NewFlagSet("req bind", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "req bind")
	root := flags.String("root", ".", "repository root")
	reqPath := flags.String("req", "", "locked REQ path")
	approvedBy := flags.String("approved-by", "", "human approver identity")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *reqPath == "" || *approvedBy == "" {
		fmt.Fprintln(stderr, "req bind requires --req and --approved-by")
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, *reqPath))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req bind", err))
		return 1
	}
	status := markdownField(string(data), "状态", "Status")
	version := markdownField(string(data), "版本", "Version")
	if status != "locked" || version == "" {
		fmt.Fprintln(stderr, "req bind: REQ must declare locked status and version")
		return 1
	}
	id := strings.TrimSuffix(filepath.Base(*reqPath), filepath.Ext(*reqPath))
	if !strings.HasPrefix(id, "REQ-") {
		fmt.Fprintln(stderr, "req bind: filename must start with REQ-")
		return 1
	}
	statePath := filepath.Join(*root, ".claude/loop-state.json")
	journalPath := filepath.Join(*root, ".claude/loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req bind", fmt.Errorf("read runtime revision: %w", err)))
		return 1
	}
	now := time.Now().UTC()
	next, err := transition.Apply(*root, statePath, journalPath, transition.Request{
		TransitionID: "TR-001", ExpectedRevision: snapshot.Revision, Actor: "user",
		Evidence: map[string]string{"req_lock_record": id + "#lock", "loop_authorization_record": "binding:" + id},
		REQ:      &transition.LockedREQ{ID: id, Path: *reqPath, Version: version, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), ApprovedBy: *approvedBy, ApprovedAt: now.Format(time.RFC3339Nano)}, OccurredAt: now,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req bind", err))
		return 1
	}
	return encodeJSON(stdout, next.State)
}

func markdownField(content string, names ...string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, sep := range []string{"：", ":"} {
			parts := strings.SplitN(line, sep, 2)
			if len(parts) != 2 {
				continue
			}
			for _, name := range names {
				if strings.EqualFold(strings.TrimSpace(parts[0]), name) {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return ""
}

func runProjection(args []string, nextOnly bool, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("projection", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "projection")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	snapshot, err := runtime.NewStore(
		filepath.Join(*root, ".claude/loop-state.json"),
		filepath.Join(*root, ".claude/loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		fmt.Fprintf(stderr, "projection: read runtime: %v\n", err)
		return 1
	}
	state := snapshot.State
	lifecycle, _ := state["lifecycle"].(map[string]any)
	machine, _ := lifecycle["state"].(string)
	phase, _ := lifecycle["phase"].(string)
	stage, skill, action := projectNext(machine, phase, *root)
	var projection any
	if nextOnly {
		projection = buildNextProjection(state, stage, skill, action, *root)
	} else {
		projection = buildStatusProjection(state, stage, machine, lifecycle["phase"], *root)
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		fmt.Fprintf(stderr, "projection: encode: %v\n", err)
		return 1
	}
	schemaName := "status.schema.json"
	if nextOnly {
		schemaName = "next.schema.json"
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, encoded); err != nil {
		fmt.Fprintf(stderr, "projection: internal contract violation: %v\n", err)
		return 1
	}
	return encodeJSON(stdout, projection)
}

// runReady dry-runs the current cursor Quality Gate and prints the live
// missing checklist. It never commits a Transition or mutates Runtime.
// Usage:
//
//	loop-harness ready --root .
func runReady(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ready", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "ready")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	report, err := controller.EvaluateReady(context.Background(), *root)
	if err != nil {
		fmt.Fprintf(stderr, "ready: %v\n", err)
		return 1
	}
	return encodeJSON(stdout, report)
}

func projectNext(state, phase, root string) (string, string, string) {
	cursor, _ := runtime.StageFor(state, phase, root)
	switch state {
	case "inactive":
		return "S0", "loop-orchestration", "bind one human-locked REQ"
	case "planning":
		return cursor, "specification-planning", "complete the planning phase for " + phase
	case "document_verification":
		return "S5", "document-verification", "complete independent document verification"
	case "building":
		return "S6", "two-phase-activation", "complete Builder assignments"
	case "verification":
		switch phase {
		case "delivery":
			return "S7", "team-planning", "complete Delivery Verifier responsibilities"
		case "qa":
			return "S7", "team-planning", "complete QA responsibilities"
		case "e2e_browser":
			return "S7", "e2e-browser-testing", "complete E2E browser responsibilities via the e2e-browser workgroup"
		case "clean_round_evaluation":
			return "S7", "clean-round-evaluation", "evaluate the complete clean round (guards + evidence)"
		case "clean_round_passed":
			return "S7", "acceptance-and-handoff", "promote clean round into acceptance per TR-009"
		}
		return "S7", "loop-orchestration", "recover the verification sub-phase"
	case "bug_resolution":
		switch phase {
		case "investigation":
			return "S8", "bug-resolution", "investigate findings and determine evidence-backed root causes"
		case "bug_report_review":
			return "S8", "bug-resolution", "accept canonical BUG reports with root cause and Closing Contract"
		}
		return "S9", "bug-resolution", "resolve the canonical BUG and return to full review"
	case "acceptance", "release_audit":
		return "S10", "acceptance-and-handoff", "complete acceptance and release audit"
	case "awaiting_human_release":
		return "S11", "acceptance-and-handoff", "present the release-ready Gateway"
	case "paused":
		return "paused", "loop-orchestration", "resolve the recorded pause condition"
	default:
		return "cross-stage", "loop-orchestration", "recover the runtime checkpoint"
	}
}

func encodeJSON(w io.Writer, value any) int {
	enc := json.NewEncoder(w)
	if err := enc.Encode(value); err != nil {
		return 1
	}
	return 0
}

func runInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "init")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if err := writeInactiveRuntime(*root); err != nil {
		fmt.Fprintf(stderr, "init failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "initialized .claude/loop-state.json")
	// Best-effort: regenerate the agent-facing Manual next to the binary so it
	// matches the on-disk loop-definition.json. Failure is non-fatal — the
	// Manual can be regenerated later via `loop-harness manual`. REQ-005 FR-015.
	if err := regenerateManualBestEffort(*root); err != nil {
		fmt.Fprintf(stderr, "init: warning: manual not regenerated: %v\n", err)
	} else {
		fmt.Fprintf(stdout, "manual regenerated at %s\n", transition.ManualTargetPath())
	}
	return 0
}

// regenerateManualBestEffort regenerates `.claude/bin/loop-harness.md` from
// the on-disk loop-definition.json + the spec registry compiled into this
// binary. Used by `init` so a freshly-initialized project has a current
// Manual alongside its binary without a separate `manual` invocation.
func regenerateManualBestEffort(root string) error {
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	defData, err := os.ReadFile(filepath.Join(root, "docs", "loop-definition.json"))
	if err != nil {
		return fmt.Errorf("read loop-definition.json: %w", err)
	}
	target := transition.ManualTargetPath()
	markdown := transition.RenderManual(catalog.Definition, transition.ManualOptions{
		TargetPath:           target,
		HarnessVersion:       "dev",
		LoopDefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(defData)),
	})
	fullTarget := filepath.Join(root, target)
	if err := os.MkdirAll(filepath.Dir(fullTarget), 0o755); err != nil {
		return fmt.Errorf("create manual dir: %w", err)
	}
	if err := os.WriteFile(fullTarget, []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("write manual: %w", err)
	}
	return nil
}

// writeInactiveRuntime writes a schema-valid inactive runtime whose Loop
// Definition and Hook policy fingerprints match the local files. It is the
// standard way to seed a freshly bootstrapped project.
func writeInactiveRuntime(root string) error {
	markerPath := filepath.Join(root, ".claude/loop-init-pending.json")
	if _, err := os.Lstat(markerPath); err == nil {
		return completePendingInitialization(root, markerPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect pending initialization: %w", err)
	}
	full, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		return err
	}
	paths := []string{
		filepath.Join(root, ".claude/loop-state.json"),
		filepath.Join(root, ".claude/loop-events.jsonl"),
		filepath.Join(root, ".claude/hook-decisions.jsonl"),
		filepath.Join(root, ".claude/loop-state.json.rollover-pending.json"),
		filepath.Join(root, ".claude/loop-state.json.lock"),
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("refusing to initialize over existing runtime file %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect runtime path %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	pendingData, err := json.MarshalIndent(initPending{SchemaVersion: "1.0.0", FreshState: full}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pending initialization: %w", err)
	}
	if err := writeNewFile(markerPath, append(pendingData, '\n')); err != nil {
		return fmt.Errorf("record pending initialization: %w", err)
	}
	return completePendingInitialization(root, markerPath)
}

type initPending struct {
	SchemaVersion string         `json:"schema_version"`
	FreshState    map[string]any `json:"fresh_state"`
}

// completePendingInitialization makes a bootstrap retry-safe: every file is
// checked against the pending marker before it is accepted, and the marker is
// removed only after the complete runtime triplet exists.
func completePendingInitialization(root, markerPath string) error {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return fmt.Errorf("read pending initialization: %w", err)
	}
	var pending initPending
	if err := json.Unmarshal(data, &pending); err != nil {
		return fmt.Errorf("decode pending initialization: %w", err)
	}
	if pending.SchemaVersion != "1.0.0" {
		return fmt.Errorf("unsupported pending initialization schema %q", pending.SchemaVersion)
	}
	stateData, err := json.MarshalIndent(pending.FreshState, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pending runtime state: %w", err)
	}
	files := []struct {
		path string
		data []byte
	}{
		{filepath.Join(root, ".claude/loop-state.json"), append(stateData, '\n')},
		{filepath.Join(root, ".claude/loop-events.jsonl"), nil},
		{filepath.Join(root, ".claude/hook-decisions.jsonl"), nil},
	}
	for _, file := range files {
		if err := ensurePendingInitFile(file.path, file.data); err != nil {
			return fmt.Errorf("complete pending initialization: %w", err)
		}
	}
	if err := os.Remove(markerPath); err != nil {
		return fmt.Errorf("clear pending initialization: %w", err)
	}
	if err := syncDirectory(filepath.Dir(markerPath)); err != nil {
		return fmt.Errorf("sync cleared pending initialization: %w", err)
	}
	return nil
}

func ensurePendingInitFile(path string, want []byte) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := writeNewFile(path, want); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(data, want) {
		return fmt.Errorf("existing file conflicts with pending bootstrap: %s", path)
	}
	return nil
}

func writeNewFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".loop-init-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Link publishes the fully written inode without replacing an existing
	// runtime file. A retry can therefore distinguish a completed file from a
	// missing one without ever observing a partially written destination.
	if err := os.Link(tempPath, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func inactiveRuntimeState(root string, occurredAt time.Time) (map[string]any, error) {
	defPath := filepath.Join(root, "docs/loop-definition.json")
	policyPath := filepath.Join(root, "docs/hook-policy.json")
	defData, err := os.ReadFile(defPath)
	if err != nil {
		return nil, fmt.Errorf("read Loop Definition: %w", err)
	}
	policyData, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("read Hook policy: %w", err)
	}
	var policyMetadata struct {
		Version string `json:"version"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(policyData, &policyMetadata); err != nil {
		return nil, fmt.Errorf("decode Hook policy metadata: %w", err)
	}
	if policyMetadata.Version == "" || policyMetadata.Mode == "" {
		return nil, fmt.Errorf("Hook policy version and mode are required")
	}
	type placeholder struct {
		SchemaVersion string         `json:"schema_version"`
		RuntimeID     string         `json:"runtime_id"`
		Definition    map[string]any `json:"definition"`
		Revision      int            `json:"revision"`
		Lifecycle     map[string]any `json:"lifecycle"`
		HookControl   map[string]any `json:"hook_control"`
	}
	defVersion := "1.1.0"
	if v, ok := extractDefinitionVersion(defData); ok {
		defVersion = v
	}
	state := placeholder{
		SchemaVersion: "1.1.0",
		RuntimeID:     "loop-inactive",
		Definition: map[string]any{
			"path":    "docs/loop-definition.json",
			"version": defVersion,
			"sha256":  fmt.Sprintf("%x", sha256.Sum256(defData)),
		},
		Revision: 0,
		Lifecycle: map[string]any{
			"state":          "inactive",
			"phase":          nil,
			"phase_revision": 0,
		},
		HookControl: map[string]any{
			"policy_ref": map[string]any{
				"path":    "docs/hook-policy.json",
				"version": policyMetadata.Version,
				"sha256":  fmt.Sprintf("%x", sha256.Sum256(policyData)),
			},
			"mode":                 policyMetadata.Mode,
			"health":               "healthy",
			"consecutive_failures": 0,
			"last_checked_at":      nil,
		},
	}
	return map[string]any{
		"schema_version": state.SchemaVersion,
		"runtime_id":     state.RuntimeID,
		"definition":     state.Definition,
		"revision":       state.Revision,
		"lifecycle":      state.Lifecycle,
		"milestone": map[string]any{
			"stage":           "S0",
			"lifecycle_state": "inactive",
			"lifecycle_phase": nil,
			"objective":       "bind one human-locked requirement",
			"action":          "bind one human-locked REQ",
			"protocol_ref":    "docs/agent-protocol.md#s0",
			"manual_ref":      loopManualRef,
			"primary_skill":   "loop-orchestration",
			"read":            []any{"docs/requirements/"},
			"missing":         []any{"locked_req_binding"},
			"done_when":       []any{"a locked REQ is fingerprinted and bound to the runtime"},
			"human_required":  false,
			"blocked":         false,
			"blocker":         nil,
			"event":           "init",
			"instruction":     "LOOP RECOVERY: bind one human-locked REQ.",
			"recovery":        []any{"read docs/agent-protocol.md#s0", "if blocked read .claude/bin/loop-harness.md"},
			"source_revision": 0,
			"updated_at":      occurredAt.UTC().Format(time.RFC3339Nano),
		},
		"authorization": map[string]any{
			"mode":        "none",
			"command":     "",
			"actor":       "",
			"occurred_at": "1970-01-01T00:00:00Z",
		},
		"bound_req": nil,
		"baseline": map[string]any{
			"generation":  0,
			"captured_at": nil,
		},
		"review": map[string]any{
			"round":       0,
			"clean_round": nil,
		},
		"configuration": map[string]any{
			"repair": map[string]any{
				"max_attempts_per_bug":       3,
				"max_same_contract_failures": 2,
				"max_full_review_rounds":     5,
			},
		},
		"hook_control": state.HookControl,
		"documents":    []any{},
		"entities": map[string]any{
			"agents": []any{},
			"tasks":  []any{},
			"bugs":   []any{},
			"teams":  []any{},
		},
		"evidence": []any{},
		"blockers": []any{},
		"pause":    nil,
		"journal": map[string]any{
			"path":          ".claude/loop-events.jsonl",
			"last_sequence": 0,
			"last_event_id": nil,
		},
		"last_transition": nil,
		"updated_at":      occurredAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func extractDefinitionVersion(data []byte) (string, bool) {
	var probe struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && probe.SchemaVersion != "" {
		return probe.SchemaVersion, true
	}
	return "", false
}

func runTeam(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "launch" {
		fmt.Fprintln(stderr, "team requires <launch>")
		return 2
	}
	flags := flag.NewFlagSet("team launch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "team launch")
	root := flags.String("root", ".", "repository root")
	manifestPath := flags.String("manifest", "", "team manifest path relative to root")
	templatePath := flags.String("request-template", "", "readback request template path relative to root")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *manifestPath == "" || *templatePath == "" {
		fmt.Fprintln(stderr, "team launch requires --manifest and --request-template")
		return 2
	}
	manifestData, err := os.ReadFile(filepath.Join(*root, *manifestPath))
	if err != nil {
		fmt.Fprintf(stderr, "read team manifest: %v\n", err)
		return 1
	}
	templateData, err := os.ReadFile(filepath.Join(*root, *templatePath))
	if err != nil {
		fmt.Fprintf(stderr, "read request template: %v\n", err)
		return 1
	}
	var request team.ReadbackRequest
	if err := json.Unmarshal(templateData, &request); err != nil {
		fmt.Fprintf(stderr, "decode request template: %v\n", err)
		return 1
	}
	if err := schemaValidate(*root, "readback-request.schema.json", templateData); err != nil {
		fmt.Fprintf(stderr, "validate request template: %v\n", err)
		return 1
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, request.OccurredAt)
	if err != nil {
		fmt.Fprintf(stderr, "parse request occurred_at: %v\n", err)
		return 1
	}
	requests, err := team.GenerateReadbackRequests(*root, manifestData, team.LaunchOptions{
		TaskID:                  request.TaskID,
		BugID:                   request.BugID,
		ExpectedRuntimeRevision: request.ExpectedRuntimeRevision,
		Documents:               request.Documents,
		OccurredAt:              occurredAt,
	})
	if err != nil {
		fmt.Fprintf(stderr, "generate launch packages: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(requests); err != nil {
		fmt.Fprintf(stderr, "encode launch packages: %v\n", err)
		return 1
	}
	return 0
}

func runRuntime(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "runtime requires <reconcile|migrate-planning|reconcile-policy-ref|rollover|transition|change|evidence|register-workgroup|agent-event|bug-event|fingerprint>")
		return 2
	}
	switch args[0] {
	case "rollover":
		return runRuntimeRollover(args[1:], stdout, stderr)
	case "reconcile":
		flags := flag.NewFlagSet("runtime reconcile", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime reconcile")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		reconciled, err := runtime.NewStore(*statePath, *journalPath).Reconcile()
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime reconcile", err))
			return 1
		}
		if reconciled {
			fmt.Fprintln(stdout, "journal reconciled")
		} else {
			fmt.Fprintln(stdout, "journal already consistent")
		}
		return 0
	case "migrate-planning":
		flags := flag.NewFlagSet("runtime migrate-planning", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime migrate-planning")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		resolvedState := *statePath
		if !filepath.IsAbs(resolvedState) {
			resolvedState = filepath.Join(*root, resolvedState)
		}
		resolvedJournal := *journalPath
		if !filepath.IsAbs(resolvedJournal) {
			resolvedJournal = filepath.Join(*root, resolvedJournal)
		}
		migrated, err := runtime.NewStore(resolvedState, resolvedJournal).MigrateLegacyPlanning(*root)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime migrate-planning", err))
			return 1
		}
		if migrated {
			fmt.Fprintln(stdout, "planning phase migrated")
		} else {
			fmt.Fprintln(stdout, "planning phase already current")
		}
		return 0
	case "reconcile-policy-ref":
		return runRuntimeReconcilePolicyRef(args[1:], stdout, stderr)
	case "transition":
		flags := flag.NewFlagSet("runtime transition", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime transition")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		transitionID := flags.String("id", "", "Loop Definition transition ID")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		actor := flags.String("actor", "", "transition actor")
		occurredAtValue := flags.String("occurred-at", "", "RFC3339 transition time")
		reqID := flags.String("req-id", "", "locked REQ ID for TR-001")
		reqPath := flags.String("req-path", "", "locked REQ path for TR-001")
		reqVersion := flags.String("req-version", "", "locked REQ version for TR-001")
		reqSHA256 := flags.String("req-sha256", "", "locked REQ SHA-256 for TR-001")
		reqApprovedBy := flags.String("req-approved-by", "", "locked REQ approver for TR-001")
		reqApprovedAt := flags.String("req-approved-at", "", "locked REQ approval time for TR-001")
		paramsRaw := flags.String("params", "", "JSON object of guard params (used by generated-evidence transitions like PTR-BUG-02)")
		var evidence stringListFlag
		flags.Var(&evidence, "evidence", "required evidence kind=reference; repeatable")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		var params map[string]any
		if *paramsRaw != "" {
			if err := json.Unmarshal([]byte(*paramsRaw), &params); err != nil {
				fmt.Fprintf(stderr, "runtime transition: invalid --params JSON: %v\n", err)
				return 2
			}
		}
		if *transitionID == "" || *expectedRevision < 0 || *actor == "" {
			fmt.Fprintln(stderr, "runtime transition requires --id, --expected-revision and --actor")
			return 2
		}
		evidenceMap, err := parseEvidence(evidence)
		if err != nil {
			fmt.Fprintf(stderr, "runtime transition: %v\n", err)
			return 2
		}
		var occurredAt time.Time
		if *occurredAtValue != "" {
			occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
			if err != nil {
				fmt.Fprintf(stderr, "runtime transition: invalid --occurred-at: %v\n", err)
				return 2
			}
		}
		var req *transition.LockedREQ
		if *reqID != "" {
			req = &transition.LockedREQ{
				ID: *reqID, Path: *reqPath, Version: *reqVersion, SHA256: *reqSHA256,
				ApprovedBy: *reqApprovedBy, ApprovedAt: *reqApprovedAt,
			}
		}
		next, err := transition.Apply(*root, *statePath, *journalPath, transition.Request{
			TransitionID: *transitionID, ExpectedRevision: *expectedRevision,
			Actor: *actor, Evidence: evidenceMap, REQ: req, OccurredAt: occurredAt,
			Params: params,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime transition", err))
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode runtime transition: %v\n", err)
			return 1
		}
		return 0
	case "change":
		return runRuntimeChange(args[1:], stdout, stderr)
	case "evidence":
		return runRuntimeEvidence(args[1:], stdout, stderr)
	case "register-workgroup":
		flags := flag.NewFlagSet("runtime register-workgroup", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime register-workgroup")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		manifestPath := flags.String("manifest", "", "team manifest path")
		taskID := flags.String("task-id", "", "TASK ID")
		taskPath := flags.String("task", "", "TASK path")
		occurredAtValue := flags.String("occurred-at", "", "RFC3339 registration time")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *expectedRevision < 0 || *manifestPath == "" || *taskID == "" || *taskPath == "" {
			fmt.Fprintln(stderr, "runtime register-workgroup requires --expected-revision, --manifest, --task-id and --task")
			return 2
		}
		var occurredAt time.Time
		var err error
		if *occurredAtValue != "" {
			occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
			if err != nil {
				fmt.Fprintf(stderr, "runtime register-workgroup: invalid --occurred-at: %v\n", err)
				return 2
			}
		}
		next, err := assignment.Register(*root, *statePath, *journalPath, assignment.Request{
			ExpectedRevision: *expectedRevision,
			ManifestPath:     resolveRootPath(*root, *manifestPath),
			TaskID:           *taskID,
			TaskPath:         resolveRootPath(*root, *taskPath),
			OccurredAt:       occurredAt,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime register-workgroup", err))
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode workgroup registration: %v\n", err)
			return 1
		}
		return 0
	case "agent-event":
		flags := flag.NewFlagSet("runtime agent-event", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime agent-event")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		agentID := flags.String("agent-id", "", "Agent ID")
		event := flags.String("event", "", "readback_submitted, understanding_approved, or activated")
		messagePath := flags.String("message", "", "Agent message path")
		occurredAtValue := flags.String("occurred-at", "", "RFC3339 event time")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *expectedRevision < 0 || *agentID == "" || *event == "" || *messagePath == "" {
			fmt.Fprintln(stderr, "runtime agent-event requires --expected-revision, --agent-id, --event and --message")
			return 2
		}
		var occurredAt time.Time
		var err error
		if *occurredAtValue != "" {
			occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
			if err != nil {
				fmt.Fprintf(stderr, "runtime agent-event: invalid --occurred-at: %v\n", err)
				return 2
			}
		}
		next, err := assignment.AdvanceAgent(*root, *statePath, *journalPath, assignment.AgentEventRequest{
			ExpectedRevision: *expectedRevision,
			AgentID:          *agentID,
			Event:            *event,
			MessagePath:      resolveRootPath(*root, *messagePath),
			OccurredAt:       occurredAt,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime agent-event", err))
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode Agent event: %v\n", err)
			return 1
		}
		return 0
	case "bug-event":
		flags := flag.NewFlagSet("runtime bug-event", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime bug-event")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		bugID := flags.String("bug-id", "", "BUG ID")
		event := flags.String("event", "", "BUG lifecycle event (e.g. bug_accepted)")
		messagePath := flags.String("message", "", "BUG message evidence path")
		paramsRaw := flags.String("params", "", "JSON object of guard params")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *expectedRevision < 0 || *bugID == "" || *event == "" {
			fmt.Fprintln(stderr, "runtime bug-event requires --expected-revision, --bug-id and --event")
			return 2
		}
		var params map[string]any
		if *paramsRaw != "" {
			if err := json.Unmarshal([]byte(*paramsRaw), &params); err != nil {
				fmt.Fprintf(stderr, "runtime bug-event: invalid --params JSON: %v\n", err)
				return 2
			}
		}
		next, err := assignment.AdvanceBug(*root, *statePath, *journalPath, assignment.BugEventRequest{
			ExpectedRevision: *expectedRevision,
			BugID:            *bugID,
			Event:            *event,
			MessagePath:      resolveRootPath(*root, *messagePath),
			Params:           params,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime bug-event", err))
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode BUG event: %v\n", err)
			return 1
		}
		return 0
	case "fingerprint":
		flags := flag.NewFlagSet("runtime fingerprint", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime fingerprint")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		result, err := runtime.NewStore(*statePath, *journalPath).RefreshFingerprints(*root)
		if err != nil {
			fmt.Fprintf(stderr, "runtime fingerprint failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "updated=%d unchanged=%d missing=%d\n", len(result.Updated), len(result.Unchanged), len(result.Missing))
		for _, p := range result.Updated {
			fmt.Fprintf(stdout, "updated  %s\n", p)
		}
		for _, p := range result.Missing {
			fmt.Fprintf(stdout, "missing  %s\n", p)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown runtime command %q\n", args[0])
		return 2
	}
}

// runRuntimeReconcilePolicyRef realigns `hook_control.policy_ref` with the
// Hook policy document on disk (BUG-039-12; REQ-039 §11, SYNC-039 §6-7).
//
// This is the fix path that `loop-harness doctor` names when it detects policy
// reference drift. It deliberately reuses Store.RefreshFingerprints rather than
// writing policy_ref directly: fingerprint refresh is the single canonical
// non-semantic housekeeping writer, it goes through the runtime lock, the
// PreCommitValidator and the atomic write, and it does not bump the revision or
// append a journal entry. Realigning an audit snapshot is not a Loop transition,
// so it must not look like one in the journal.
//
// --check reports drift without writing, for use in CI or a pre-flight probe.
func runRuntimeReconcilePolicyRef(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime reconcile-policy-ref", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime reconcile-policy-ref")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	checkOnly := flags.Bool("check", false, "report drift without writing state")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	store := runtime.NewStore(*statePath, *journalPath)
	before, err := store.InspectPolicyRef(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime reconcile-policy-ref", err))
		return 1
	}
	if before.Missing {
		fmt.Fprintln(stderr, "runtime reconcile-policy-ref failed: runtime state has no hook_control.policy_ref; re-bind the runtime")
		return 1
	}
	if before.FileMissing {
		fmt.Fprintf(stderr, "runtime reconcile-policy-ref failed: hook policy %s does not exist on disk\n", before.Path)
		return 1
	}
	if !before.Drifted() {
		fmt.Fprintf(stdout, "policy_ref already consistent path=%s version=%s\n", before.Path, before.RecordedVersion)
		return 0
	}
	if *checkOnly {
		fmt.Fprintf(stderr, "policy_ref drifted path=%s version recorded=%s on-disk=%s sha256 recorded=%s on-disk=%s\n",
			before.Path, before.RecordedVersion, before.OnDiskVersion, before.RecordedSHA256, before.OnDiskSHA256)
		return 1
	}
	if _, err := store.RefreshFingerprints(*root); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime reconcile-policy-ref", err))
		return 1
	}
	after, err := store.InspectPolicyRef(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime reconcile-policy-ref", err))
		return 1
	}
	if after.Drifted() {
		fmt.Fprintf(stderr, "runtime reconcile-policy-ref failed: policy_ref still drifted after refresh (version=%s sha256=%s)\n",
			after.RecordedVersion, after.RecordedSHA256)
		return 1
	}
	fmt.Fprintf(stdout, "policy_ref reconciled path=%s version %s -> %s\n", after.Path, before.RecordedVersion, after.RecordedVersion)
	if before.RecordedSHA256 != after.RecordedSHA256 {
		fmt.Fprintf(stdout, "policy_ref sha256 %s -> %s\n", before.RecordedSHA256, after.RecordedSHA256)
	}
	return 0
}

func runRuntimeRollover(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime rollover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime rollover")
	root := flags.String("root", ".", "repository root")
	archive := flags.String("archive-dir", ".claude/runtime-archive", "archive directory relative to repository root")
	approvedBy := flags.String("approved-by", "", "human approver identity")
	approvalEvidence := flags.String("approval-evidence", "", "valid human_decision evidence ID produced by --approved-by")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*approvedBy) == "" {
		fmt.Fprintln(stderr, "runtime rollover requires --approved-by")
		return 2
	}
	if strings.TrimSpace(*approvalEvidence) == "" {
		fmt.Fprintln(stderr, "runtime rollover requires --approval-evidence")
		return 2
	}
	now := time.Now().UTC()
	freshState, err := inactiveRuntimeState(*root, now)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime rollover", err))
		return 1
	}
	encoded, err := json.Marshal(freshState)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime rollover", fmt.Errorf("encode fresh runtime: %w", err)))
		return 1
	}
	if err := semantic.ValidateRuntimeBytes(*root, encoded); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime rollover", fmt.Errorf("validate fresh runtime: %w", err)))
		return 1
	}
	record, err := runtime.NewStore(rootedPath(*root, ".claude/loop-state.json"), rootedPath(*root, ".claude/loop-events.jsonl")).Rollover(
		freshState, rootedPath(*root, *archive), runtime.RolloverApproval{ApprovedBy: *approvedBy, EvidenceID: *approvalEvidence}, now,
	)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime rollover", err))
		return 1
	}
	fmt.Fprintf(stdout, "runtime rolled over: archived %s revision %d at %s\n", record.RuntimeID, record.Revision, record.ArchiveDir)
	return 0
}

func rootedPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func runRuntimeEvidence(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "add" {
		fmt.Fprintln(stderr, "runtime evidence requires <add>")
		return 2
	}
	flags := flag.NewFlagSet("runtime evidence add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime evidence add")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	id := flags.String("id", "", "evidence ID")
	kind := flags.String("kind", "", "evidence kind")
	path := flags.String("path", "", "evidence artifact path relative to repository root")
	responsibility := flags.String("responsibility", "", "owning responsibility ID")
	reviewRound := flags.Int("review-round", 0, "review round; omit for non-round evidence")
	var producedBy, scopeRefs stringListFlag
	flags.Var(&producedBy, "produced-by", "evidence producer; repeatable")
	flags.Var(&scopeRefs, "scope-ref", "evidence scope reference; repeatable")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *expectedRevision < 0 || *id == "" || *kind == "" || *path == "" || len(producedBy) == 0 {
		fmt.Fprintln(stderr, "runtime evidence add requires --expected-revision, --id, --kind, --path and --produced-by")
		return 2
	}
	var round *int
	if *reviewRound != 0 {
		round = reviewRound
	}
	next, err := runtime.RecordEvidence(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), runtime.EvidenceRequest{
		ExpectedRevision: *expectedRevision,
		ID:               *id,
		Kind:             *kind,
		Path:             *path,
		ProducedBy:       append([]string(nil), producedBy...),
		ResponsibilityID: *responsibility,
		ReviewRound:      round,
		ScopeRefs:        append([]string(nil), scopeRefs...),
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime evidence add", err))
		return 1
	}
	return encodeJSON(stdout, next)
}

func runRuntimeChange(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "create" {
		fmt.Fprintln(stderr, "runtime change requires <create>")
		return 2
	}
	flags := flag.NewFlagSet("runtime change create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime change create")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	inputPath := flags.String("input", "", "JSON Change Record input path")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *expectedRevision < 0 || *inputPath == "" {
		fmt.Fprintln(stderr, "runtime change create requires --expected-revision and --input")
		return 2
	}
	data, err := os.ReadFile(resolveRootPath(*root, *inputPath))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	var input change.Input
	if err := json.Unmarshal(data, &input); err != nil {
		fmt.Fprintf(stderr, "runtime change create: invalid input JSON: %v\n", err)
		return 2
	}
	stateData, err := os.ReadFile(resolveRootPath(*root, *statePath))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	if bound, ok := state["bound_req"].(map[string]any); ok {
		if input.REQRef == "" {
			input.REQRef, _ = bound["id"].(string)
		}
		if input.REQSHA == "" {
			input.REQSHA, _ = bound["sha256"].(string)
		}
	}
	record, err := change.BuildRecord(input)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	next, err := runtime.CreateChange(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), runtime.ChangeRequest{
		ExpectedRevision: *expectedRevision,
		Record:           record,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	return encodeJSON(stdout, next)
}

func resolveRootPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func parseEvidence(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		kind, reference, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(reference) == "" {
			return nil, fmt.Errorf("--evidence must use kind=reference")
		}
		result[strings.TrimSpace(kind)] = strings.TrimSpace(reference)
	}
	return result, nil
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "validate")
	all := flags.Bool("all", false, "validate all Harness artifacts")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if !*all {
		fmt.Fprintln(stderr, "validate requires --all")
		return 2
	}
	if err := semantic.ValidateRepository(*root); err != nil {
		fmt.Fprintln(stderr, formatFailure("validation", err))
		return 1
	}
	fmt.Fprintln(stdout, "validation passed")
	return 0
}

func runDryRun(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("dry-run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "dry-run")
	root := flags.String("root", ".", "repository root")
	fixture := flags.String("fixture", "", "Hook input fixture")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *fixture == "" {
		fmt.Fprintln(stderr, "dry-run requires --fixture")
		return 2
	}
	file, err := os.Open(*fixture)
	if err != nil {
		fmt.Fprintf(stderr, "open fixture: %v\n", err)
		return 1
	}
	defer file.Close()
	return evaluate(*root, "", file, stdout, stderr, false)
}

func runHook(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hook", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "hook")
	event := flags.String("event", "", "Claude Code Hook event")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *event == "" {
		fmt.Fprintln(stderr, "hook requires --event")
		return 2
	}
	return evaluate(*root, *event, stdin, stdout, stderr, true)
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "doctor")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if err := semantic.ValidateManualAgreement(*root); err != nil {
		fmt.Fprintf(stderr, "doctor failed: %v\n", err)
		return 1
	}
	if err := semantic.ValidateRepository(*root); err != nil {
		fmt.Fprintf(stderr, "doctor failed: %v\n", err)
		return 1
	}
	if code := reportPolicyRefDrift(*root, *statePath, *journalPath, stdout, stderr); code != 0 {
		return code
	}
	if out, err := metrics.FormatDoctor(*root); err != nil {
		fmt.Fprintf(stderr, "doctor failed: read loop metrics: %v\n", err)
		return 1
	} else {
		fmt.Fprintln(stdout, out)
	}
	fmt.Fprintln(stdout, "doctor passed: schemas, examples, semantic links valid; manual current")
	return 0
}

// reportPolicyRefDrift surfaces divergence between the Hook policy reference
// recorded in `hook_control.policy_ref` and the policy document on disk
// (BUG-039-12; REQ-039 §11, SYNC-039 §6-7).
//
// `policy_ref` is a bind-time snapshot of the enforced safety boundary. When
// `docs/hook-policy.json` is rewritten in place the snapshot goes stale, and
// the runtime keeps attributing Hook decisions to a policy version/digest that
// is no longer what the Hook actually loads — an audit inconsistency rather
// than a runtime failure. doctor is the detector; the fix path it names is
// `runtime reconcile-policy-ref`.
//
// A repository with no runtime state yet is not a finding: doctor also runs on
// unbound checkouts, so a missing state file is skipped silently.
func reportPolicyRefDrift(root, statePath, journalPath string, stdout, stderr io.Writer) int {
	resolvedState := statePath
	if !filepath.IsAbs(resolvedState) {
		resolvedState = filepath.Join(root, resolvedState)
	}
	if _, err := os.Stat(resolvedState); err != nil {
		return 0
	}
	resolvedJournal := journalPath
	if !filepath.IsAbs(resolvedJournal) {
		resolvedJournal = filepath.Join(root, resolvedJournal)
	}
	drift, err := runtime.NewStore(resolvedState, resolvedJournal).InspectPolicyRef(root)
	if err != nil {
		fmt.Fprintf(stderr, "doctor failed: inspect hook policy reference: %v\n", err)
		return 1
	}
	if !drift.Drifted() {
		fmt.Fprintf(stdout, "doctor: hook_control.policy_ref consistent (version=%s)\n", drift.RecordedVersion)
		return 0
	}
	switch {
	case drift.Missing:
		fmt.Fprintln(stderr, "doctor failed: runtime state has no hook_control.policy_ref; re-bind the runtime or run: loop-harness runtime reconcile-policy-ref --root .")
	case drift.FileMissing:
		fmt.Fprintf(stderr, "doctor failed: hook policy %s recorded in hook_control.policy_ref does not exist on disk\n", drift.Path)
	default:
		fmt.Fprintf(stderr, "doctor failed: hook_control.policy_ref drifted from %s\n", drift.Path)
		if drift.VersionDrifted() {
			fmt.Fprintf(stderr, "  version recorded=%s on-disk=%s\n", drift.RecordedVersion, drift.OnDiskVersion)
		}
		if drift.SHADrifted() {
			fmt.Fprintf(stderr, "  sha256  recorded=%s on-disk=%s\n", drift.RecordedSHA256, drift.OnDiskSHA256)
		}
		fmt.Fprintln(stderr, "  fix: loop-harness runtime reconcile-policy-ref --root .")
	}
	return 1
}

func evaluate(root, expectedEvent string, input io.Reader, stdout, stderr io.Writer, renderHook bool) int {
	var request policy.Input
	if err := json.NewDecoder(input).Decode(&request); err != nil {
		fmt.Fprintf(stderr, "decode Hook input: %v\n", err)
		return 1
	}
	if expectedEvent != "" && request.Event != expectedEvent {
		fmt.Fprintf(stderr, "Hook argument event %q does not match input event %q\n", expectedEvent, request.Event)
		return 1
	}
	if request.Runtime.RuntimeID == "" {
		context, err := hookctx.Load(root, request.AgentID)
		if err != nil {
			if request.Facts == nil {
				request.Facts = make(map[string]bool)
			}
			request.Facts["runtime_integrity_failure"] = true
		} else {
			request.Runtime = context
		}
	}
	populateUIPrototypeFact(root, &request)

	// The Hook policy document is the minimal-safety authority. If it
	// cannot be loaded, evaluate() cannot render a safety decision and
	// the Hook output is meaningless — return the load error instead of
	// silently falling through. The controller cycle (run below) does
	// NOT depend on the policy document; it only needs the catalog and
	// the runtime store.
	if _, err := policy.Load(filepath.Join(root, "docs", "hook-policy.json")); err != nil {
		fmt.Fprintf(stderr, "load policy: %v\n", err)
		return 1
	}

	// The Hook entrypoint delegates the canonical control cycle to the
	// internal/controller package. That cycle runs the eleven steps of
	// BUG-039-02 §4.1 (snapshot → gate → optional one Transition →
	// committed snapshot → milestone refresh → final safety →
	// ControlResult). The minimal safety policy still produces the
	// `Decision` consumed downstream by the envelope and renderer; the
	// controller only adds Quality Gate progress and (when applicable)
	// auto-commits a single Transition before the safety verdict.
	controlResult := runControlCycleForHook(root, request)
	decision := projectControlDecision(controlResult)
	refreshGuidanceFromController(root, &request, &decision, controlResult)
	envelope := buildEnvelopeFromController(root, request, decision, controlResult, time.Now())
	// envelopeWithQualityGate carries the layered Controller projection
	// alongside the legacy hook-policy envelope fields. On PreToolUse the
	// outbox record AND the stdout wire payload must both expose
	// quality_gate so an external auditor and the Agent see the same
	// status (BUG-039-03 §4.1).
	envelopeWithQualityGate := any(envelope)
	if request.Event == "PreToolUse" {
		envelopeWithQualityGate = envelopeWithQualityGateMap(envelope, controlResult)
	}
	if !renderHook {
		if err := json.NewEncoder(stdout).Encode(envelopeWithQualityGate); err != nil {
			fmt.Fprintf(stderr, "encode decision: %v\n", err)
			return 1
		}
		return 0
	}
	if err := audit.NewOutbox(filepath.Join(root, ".claude", "hook-decisions.jsonl")).Append(envelopeWithQualityGate); err != nil {
		// For denying decisions the deny payload is more important than the
		// an audit-write error here would mask the deny. For all other decisions
		// the audit trail is the only durable record, so its failure is fatal.
		fmt.Fprintf(stderr, "append Hook audit: %v\n", err)
		if !isDenyingHookDecision(decision.Decision) {
			return 1
		}
		// Audit write failed but block payload is intact; fall through to write stdout.
	}
	// Audit-classification decisions are observation-only — no user-visible
	// hookSpecificOutput/systemMessage payload and no exit-code signal. The
	// full DecisionEnvelope already landed in the outbox above, so skip the
	// Render call (whose audit branch writes a SECOND, sparse record via
	// appendAuditLine — the dual-write defect DV-1 / QA-1 §4 T2).
	if decision.Decision == "audit" {
		return 0
	}
	if request.Event == "PreToolUse" || decision.Guidance != nil {
		_ = metrics.RecordRecoveryPacket(root)
	}
	// PreToolUse uses the layered Controller-driven render path
	// (PreToolUseWithQualityGate) so the wire envelope carries the
	// `quality_gate` object alongside permissionDecision. Lifecycle events
	// (SessionStart, SubagentStart, ...) continue to flow through the legacy
	// hook-policy renderer. BUG-039-03 §4.1.
	var output []byte
	var code int
	var err error
	if request.Event == "PreToolUse" {
		output, code, err = hook.PreToolUseWithQualityGate(decision, controlResult)
	} else {
		output, code, err = hook.RenderWithRoot(root, request.Event, decision, request.Runtime)
	}
	if err != nil {
		if isDenyingHookDecision(decision.Decision) {
			return 2
		}
		fmt.Fprintf(stderr, "render Hook output: %v\n", err)
		return 1
	}
	if len(output) > 0 {
		if _, err := stdout.Write(append(output, '\n')); err != nil {
			if isDenyingHookDecision(decision.Decision) {
				return 2
			}
			fmt.Fprintf(stderr, "write Hook output: %v\n", err)
			return 1
		}
	}
	if decision.Decision == "block" {
		return 2
	}
	return code
}

// runControlCycleForHook adapts the policy.Input the Hook transport emits
// to the ControlRequest the internal/controller package expects, runs the
// cycle, and returns the resulting ControlResult. The cycle itself is the
// single authority for Quality Gate progress and auto-Transition commits
// (BUG-039-02 §4.1).
func runControlCycleForHook(root string, request policy.Input) controller.ControlResult {
	controlReq := controller.ControlRequest{
		Root:        root,
		Event:       request.Event,
		ToolName:    request.ToolName,
		ToolInput:   request.ToolInput,
		TargetID:    request.TargetID,
		AgentID:     request.AgentID,
		SessionID:   request.SessionID,
		HookPayload: map[string]any{},
	}
	result, err := controller.RunControlCycle(contextForHook(request), controlReq)
	if err != nil {
		// RunControlCycle surfaces user-visible errors via the result
		// struct; reaching here is reserved for programmer errors (e.g.
		// missing root). Surface them as an unknown gate verdict.
		return controller.ControlResult{
			Decision:  policy.Decision{Decision: "allow", Reason: "controller cycle failed: " + err.Error()},
			Error:     err.Error(),
			ErrorCode: "LOOP_RUNTIME_INVALID",
			QualityGate: controller.QualityGateResult{
				Status:       controller.StatusUnknown,
				Missing:      []string{},
				EvidenceRefs: []string{},
			},
		}
	}
	return result
}

func contextForHook(request policy.Input) context.Context {
	if request.SessionID == "" {
		return context.Background()
	}
	type sessionKey struct{}
	return context.WithValue(context.Background(), sessionKey{}, request.SessionID)
}

// projectControlDecision folds a ControlResult back into a policy.Decision
// for the existing envelope + renderer path. Quality Gate `not_ready`,
// `satisfied`, and `unknown` all map to safety `allow` (BE-039 §3.2 /
// §5.2 / REQ-039 §10.2); only an actual safety block is projected as
// `block` and only `advanced` keeps the tool-default `allow` after a
// successful transition.
func projectControlDecision(result controller.ControlResult) policy.Decision {
	if result.Decision.Decision == "block" {
		return result.Decision
	}
	switch result.QualityGate.Status {
	case controller.StatusBlocked:
		// Quality gate was projected to blocked because the final safety
		// layer denied. The Decision field already carries the block
		// payload; we just ensure the gate status survives the round-trip.
		decision := result.Decision
		if decision.Decision != "block" {
			decision.Decision = "block"
			decision.RuleID = policy.RuleLockedArtifactWrite
			decision.Reason = "final safety block"
		}
		return decision
	case controller.StatusAdvanced, controller.StatusSatisfied, controller.StatusNotReady, controller.StatusUnknown:
		// Always allow the tool; the Quality Gate verdict is the
		// positive side (Guidance), not a permission verdict.
		decision := result.Decision
		decision.Decision = "allow"
		decision.RuleID = ""
		decision.Reason = "no policy rule blocked this action"
		decision.Retry = "not_applicable"
		decision.HumanRequired = false
		decision.MatchedRuleIDs = nil
		return decision
	}
	return result.Decision
}

// refreshGuidanceFromController attaches the positive Guidance packet
// the controller produced (or the legacy helper when the cycle fell back
// to read-only projection) to the Decision so the renderer can include
// the recovery / next step in the systemMessage.
//
// For PreToolUse, when the Controller produced a non-empty QualityGate,
// this helper also persists the gate into the Runtime Milestone via
// refreshMilestoneWithGate so the milestone projection matches what the
// hook emitted on the wire (BUG-039-07 wiring).
func refreshGuidanceFromController(root string, request *policy.Input, decision *policy.Decision, result controller.ControlResult) {
	persistGateForPreToolUse(root, request, decision, result)
	if decision.Guidance != nil {
		return
	}
	if request.Event == "PreToolUse" && result.Snapshot.State != nil {
		guidance := BuildGuidanceForState(root, result.Snapshot.State, request.Event, *request)
		decision.Guidance = &guidance
		return
	}
	if isGuidanceEvent(request.Event) {
		guidance, _, err := ReconcileGuidanceForController(root, request.Event, *request)
		if err == nil {
			decision.Guidance = &guidance
			return
		}
	}
	if result.Snapshot.State != nil {
		guidance := BuildGuidanceForState(root, result.Snapshot.State, request.Event, *request)
		decision.Guidance = &guidance
		return
	}
	decision.Guidance = FallbackGuidanceForController(request.Event)
}

// persistGateForPreToolUse threads the Controller's QualityGate into the
// Runtime Milestone for PreToolUse events. The gate is the source of truth
// for the milestone.quality_gate projection; without this wiring the field
// would be empty in production even though the schema and helpers support it.
// Other event types (SessionStart/PreCompact/SubagentStop/TeammateIdle) do
// not run the control cycle and therefore have no gate to persist; their
// milestone refresh continues to use the zero-gate legacy path.
//
// The presence test is `Status`, not `GateID`/`Fingerprint` (BUG-039-12
// repair). A gate that the Controller could not resolve to a single candidate
// still carries a real observation — e.g. `status=unknown` with
// `error_code=LOOP_TRIGGER_CONFLICT` has no gate ID and no fingerprint, yet it
// is precisely the state the Agent needs to see in the Recovery Packet after a
// compact. Gating on GateID/Fingerprint silently dropped every unknown gate and
// left milestone.quality_gate absent on exactly the runtimes that needed it.
// Only a wholly zero-valued gate (no Status at all, i.e. the cycle never ran)
// is skipped.
func persistGateForPreToolUse(root string, request *policy.Input, decision *policy.Decision, result controller.ControlResult) {
	if request == nil || request.Event != "PreToolUse" {
		return
	}
	if result.QualityGate.Status == "" {
		return
	}
	if decision.Guidance == nil {
		guidance := BuildGuidanceForState(root, result.Snapshot.State, request.Event, *request)
		decision.Guidance = &guidance
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if _, _, err := refreshMilestoneWithGate(root, statePath, journalPath, result.Snapshot, *decision.Guidance, request.Event, result.QualityGate); err != nil {
		// Persistence failure is non-fatal for the hook verdict: the
		// wire envelope still carries quality_gate. Log via the audit
		// trail by leaving decision as-is; milestone will converge on
		// the next successful CAS.
		_ = err
	}
}

// buildEnvelopeFromController wraps the engine.Envelope call so the
// evaluate() entrypoint continues to render the same JSON schema. The
// minimal safety engine is still required to populate the policy metadata
// fields (policy_id/policy_version/policy_sha256) used by the outbox and
// audit pipeline.
//
// For PreToolUse the envelope is augmented with the Controller's layered
// `quality_gate` projection so the outbox record mirrors the wire payload
// (BUG-039-03 §4.1). The Controller-produced quality_gate is the single
// source of truth; this helper never fabricates status="advanced".
func buildEnvelopeFromController(root string, request policy.Input, decision policy.Decision, controlResult controller.ControlResult, evaluatedAt time.Time) policy.DecisionEnvelope {
	engine, err := policy.Load(filepath.Join(root, "docs", "hook-policy.json"))
	if err != nil {
		// The minimal safety policy load failure is not fatal: the
		// controller's verdict is authoritative. Synthesize an envelope
		// with empty policy metadata.
		envelope := policy.DecisionEnvelope{
			SchemaVersion: "1.1.0",
			DecisionID:    fmt.Sprintf("hook-decision-%d", evaluatedAt.UnixNano()),
			HookEvent:     request.Event,
			SessionID:     request.SessionID,
			Decision:      decision.Decision,
			Reason:        decision.Reason,
			Recovery:      decision.Recovery,
			Retry:         decision.Retry,
			HumanRequired: decision.HumanRequired,
			EvaluatedAt:   evaluatedAt.UTC().Format(time.RFC3339Nano),
		}
		if decision.Guidance != nil {
			envelope.Guidance = decision.Guidance
		}
		return envelope
	}
	return engine.Envelope(request, decision, evaluatedAt)
}

// qualityGateEnvelopeFields converts the Controller's Quality Gate
// projection into the wire shape required by hook-decision.schema.json
// (status, gate_id, candidate_transition, observed_revision, fingerprint,
// missing, evidence_refs, transition_committed, next_cursor). The map is
// intended to be merged into a serialised DecisionEnvelope as the
// `quality_gate` top-level property so the outbox and the wire payload
// agree on what the Controller observed (BUG-039-03 §4.1).
func qualityGateEnvelopeFields(qg controller.QualityGateResult) map[string]any {
	missing := qg.Missing
	if missing == nil {
		missing = []string{}
	}
	evidenceRefs := qg.EvidenceRefs
	if evidenceRefs == nil {
		evidenceRefs = []string{}
	}
	return map[string]any{
		"quality_gate": map[string]any{
			"status":               string(qg.Status),
			"gate_id":              qg.GateID,
			"candidate_transition": qg.CandidateTransition,
			"observed_revision":    qg.ObservedRevision,
			"fingerprint":          qg.Fingerprint,
			"missing":              missing,
			"evidence_refs":        evidenceRefs,
			"transition_committed": qg.TransitionCommitted,
			"next_cursor":          qg.NextCursor,
		},
	}
}

// envelopeWithQualityGateMap returns a map[string]any copy of the supplied
// DecisionEnvelope with the Controller's quality_gate block merged in. The
// map is what the audit outbox + stdout serialiser consume so the layered
// projection lands in both channels without adding a field to
// policy.DecisionEnvelope (which would create a controller -> policy
// import cycle per BUG-039-03 §4.2).
//
// Returns the bare envelope when the marshal round-trip fails so the
// evaluation pipeline never fails because of an audit-shape defect.
func envelopeWithQualityGateMap(envelope policy.DecisionEnvelope, result controller.ControlResult) any {
	data, err := json.Marshal(envelope)
	if err != nil {
		return envelope
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return envelope
	}
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range qualityGateEnvelopeFields(result.QualityGate) {
		out[k] = v
	}
	return out
}

func isDenyingHookDecision(decision string) bool {
	return decision == "deny" || decision == "block"
}

// runImpact exposes the evidence-impact analysis as a CLI subcommand. It reads
// the current runtime state, computes which historical evidence is affected by
// the supplied changed paths, and prints the result as JSON.
//
// Usage:
//
//	loop-harness impact analyze --root . --changed docs/contracts/CONTRACTS-002.md
func runImpact(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: loop-harness impact analyze --root . --changed <path>...")
		return 2
	}
	if args[0] != "analyze" {
		fmt.Fprintf(stderr, "unknown impact subcommand %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("impact analyze", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "impact analyze")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	var changed changedPaths
	flags.Var(&changed, "changed", "changed path (repeatable)")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if len(changed) == 0 {
		fmt.Fprintln(stderr, "impact analyze: at least one --changed path is required")
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, *statePath))
	if err != nil {
		fmt.Fprintf(stderr, "read state: %v\n", err)
		return 1
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintf(stderr, "parse state: %v\n", err)
		return 1
	}
	impacts := impactanalysis.ComputeImpact(state, changed)
	result := map[string]any{
		"changed_paths":   changed,
		"impacted_count":  len(impacts),
		"newly_affected":  countNewlyAffected(impacts),
		"already_invalid": countAlreadyInvalid(impacts),
		"impacts":         impacts,
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode result: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	return 0
}

type changedPaths []string

func (c *changedPaths) String() string     { return strings.Join(*c, ", ") }
func (c *changedPaths) Set(v string) error { *c = append(*c, v); return nil }

func countNewlyAffected(impacts []impactanalysis.EvidenceImpact) int {
	n := 0
	for _, item := range impacts {
		if !item.AlreadyInvalid {
			n++
		}
	}
	return n
}

func countAlreadyInvalid(impacts []impactanalysis.EvidenceImpact) int {
	n := 0
	for _, item := range impacts {
		if item.AlreadyInvalid {
			n++
		}
	}
	return n
}

// runVerification exposes the clean-round evaluation as a CLI subcommand.
//
// Usage:
//
//	loop-harness verification clean-round --root .
func runVerification(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: loop-harness verification clean-round --root .")
		return 2
	}
	if args[0] != "clean-round" {
		fmt.Fprintf(stderr, "unknown verification subcommand %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("verification clean-round", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "verification clean-round")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, *statePath))
	if err != nil {
		fmt.Fprintf(stderr, "read state: %v\n", err)
		return 1
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintf(stderr, "parse state: %v\n", err)
		return 1
	}
	result := verification.EvaluateCleanRound(state)
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode result: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	if !result.Passed {
		return 1
	}
	return 0
}

// populateUIPrototypeFact is the producer side of HOOK_UI_PROTOTYPE_GATE. When
// the request writes under docs/contracts/ against a bound REQ whose ui_impact
// is "changed", the helper checks docs/design/prototypes/{module}/ for at
// least one complete final UI design package per
// docs/rules/ui-prototype.md §4:
//
//	docs/design/prototypes/{module}/index.html  (4-field header)
//	docs/design/prototypes/{module}/stories.md  (>=1 S-NNN with REQ-id)
//	docs/design/prototypes/{module}/flows.md    (>=1 F-NNN with REQ-id)
//	docs/design/prototypes/{module}/<page>.html (>=1 page HTML, 4-field header)
//
// If no complete package is present, it sets
// request.Facts["ui_contract_before_prototype"] = true so the policy engine
// fires HOOK_UI_PROTOTYPE_GATE. Closing contract:
// TestHookCommandWarnUIPrototypeWhenNoDesignPackageExists + the negative
// branches in TestHookCommandAllowsUIPrototypeWhenDesignPackageExists and
// TestHookCommandIgnoresUIPrototypeWhenImpactNotChanged.
func populateUIPrototypeFact(root string, request *policy.Input) {
	if request.ToolName != "Write" &&
		request.ToolName != "Edit" &&
		request.ToolName != "MultiEdit" &&
		request.ToolName != "NotebookEdit" {
		return
	}
	if request.Runtime.BoundREQUIImpact != "changed" {
		return
	}
	filePath := hookTargetPath(request.ToolInput)
	if !strings.HasPrefix(filePath, "docs/contracts/") {
		return
	}
	protoDir := filepath.Join(root, "docs", "design", "prototypes")
	complete, err := hasCompleteUIDesignPackage(protoDir)
	if err != nil || !complete {
		if request.Facts == nil {
			request.Facts = make(map[string]bool)
		}
		request.Facts["ui_contract_before_prototype"] = true
		return
	}
}

func hookTargetPath(input map[string]any) string {
	for _, key := range []string{"file_path", "path", "notebook_path"} {
		if value, _ := input[key].(string); value != "" {
			return value
		}
	}
	return ""
}

func hasCompleteUIDesignPackage(protoDir string) (bool, error) {
	entries, err := os.ReadDir(protoDir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		moduleDir := filepath.Join(protoDir, entry.Name())
		var (
			indexPath, storiesPath, flowsPath string
			pagePaths                         []string
		)
		if err := filepath.WalkDir(moduleDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			switch name {
			case "index.html":
				indexPath = path
			case "stories.md":
				storiesPath = path
			case "flows.md":
				flowsPath = path
			}
			if strings.HasSuffix(name, ".html") && name != "index.html" {
				pagePaths = append(pagePaths, path)
			}
			return nil
		}); err != nil {
			return false, err
		}
		if indexPath == "" || storiesPath == "" || flowsPath == "" || len(pagePaths) == 0 {
			continue
		}
		if !hasProtoMetaHeader(indexPath) {
			continue
		}
		allPagesHaveHeader := true
		for _, p := range pagePaths {
			if !hasProtoMetaHeader(p) {
				allPagesHaveHeader = false
				break
			}
		}
		if !allPagesHaveHeader {
			continue
		}
		if !hasStoryIDWithReqID(storiesPath) {
			continue
		}
		if !hasFlowIDWithReqID(flowsPath) {
			continue
		}
		return true, nil
	}
	return false, nil
}

// hasProtoMetaHeader enforces the 4-field header mandate from
// docs/rules/ui-prototype.md §5: 设计代数 / 更新 / 路由 / index 链接.
// All four tokens must appear in the file (any ordering); each is matched as a
// fixed substring so the check stays decoupled from HTML structure.
func hasProtoMetaHeader(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	body := string(data)
	for _, marker := range []string{"设计代数", "更新", "路由", "index.html"} {
		if !strings.Contains(body, marker) {
			return false
		}
	}
	return true
}

// hasStoryIDWithReqID verifies stories.md carries at least one S-NNN entry
// referencing a REQ-id, per docs/rules/ui-prototype.md §6.
func hasStoryIDWithReqID(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	body := string(data)
	storyRe := regexp.MustCompile(`(?m)^#+\s*S-\d{3,}\b`)
	reqRe := regexp.MustCompile(`REQ-\d{3,}`)
	loc := storyRe.FindStringIndex(body)
	if loc == nil {
		return false
	}
	return reqRe.MatchString(body[loc[0]:])
}

// hasFlowIDWithReqID verifies flows.md carries at least one F-NNN entry
// referencing a REQ-id, per docs/rules/ui-prototype.md §7.
func hasFlowIDWithReqID(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	body := string(data)
	flowRe := regexp.MustCompile(`(?m)^#+\s*F-\d{3,}\b`)
	reqRe := regexp.MustCompile(`REQ-\d{3,}`)
	loc := flowRe.FindStringIndex(body)
	if loc == nil {
		return false
	}
	return reqRe.MatchString(body[loc[0]:])
}

// runReleaseGraph validates a staged release tree by walking its Skill
// references and asserting each resolves to a file in the tree.
//
// Usage:
//
//	loop-harness release-graph validate --root <staged-tree-path>
func runReleaseGraph(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "validate" {
		fmt.Fprintln(stderr, "usage: loop-harness release-graph validate --root <path>")
		return 2
	}
	flags := flag.NewFlagSet("release-graph validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "release-graph validate")
	root := flags.String("root", ".", "staged release tree root")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if err := releasegraph.ValidateStagedRelease(*root); err != nil {
		fmt.Fprintf(stderr, "release-graph validation failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "release-graph validation passed")
	return 0
}

// runManual renders the agent-facing gate specification markdown from
// docs/loop-definition.json plus the guard/action spec registries. Output goes
// to --target (default .claude/bin/loop-harness.md, sitting beside the binary)
// or to stdout when --stdout is set.
//
// Usage:
//
//	loop-harness manual --root . [--target <path>] [--stdout]
func runManual(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("manual", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "manual")
	root := flags.String("root", ".", "repository root")
	target := flags.String("target", transition.ManualTargetPath(), "output path relative to root; defaults to .claude/bin/loop-harness.md next to the binary")
	toStdout := flags.Bool("stdout", false, "write to stdout instead of --target")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	catalog, err := transition.LoadCatalog(*root)
	if err != nil {
		fmt.Fprintf(stderr, "manual: load catalog: %v\n", err)
		return 1
	}
	defData, err := os.ReadFile(filepath.Join(*root, "docs", "loop-definition.json"))
	if err != nil {
		fmt.Fprintf(stderr, "manual: read loop-definition.json: %v\n", err)
		return 1
	}
	markdown := transition.RenderManual(catalog.Definition, transition.ManualOptions{
		TargetPath:           filepath.ToSlash(*target),
		HarnessVersion:       "dev",
		LoopDefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(defData)),
	})
	if *toStdout {
		fmt.Fprint(stdout, markdown)
		return 0
	}
	fullTarget := filepath.Join(*root, *target)
	if err := os.MkdirAll(filepath.Dir(fullTarget), 0o755); err != nil {
		fmt.Fprintf(stderr, "manual: create target dir: %v\n", err)
		return 1
	}
	if err := os.WriteFile(fullTarget, []byte(markdown), 0o644); err != nil {
		fmt.Fprintf(stderr, "manual: write target: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "manual written to %s\n", filepath.ToSlash(*target))
	return 0
}

// runExplain renders the per-transition details for one transition ID. The
// output is the same shape as one entry in the manual, but without the manual
// header or TOC. Used by agents that have just hit a gate failure and want to
// understand one transition quickly.
//
// Usage:
//
//	loop-harness explain <TR-xxx> --root .
func runExplain(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: loop-harness explain <TR-xxx> [--root <path>]")
		return 2
	}
	id := args[0]
	flags := flag.NewFlagSet("explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "explain")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	catalog, err := transition.LoadCatalog(*root)
	if err != nil {
		fmt.Fprintf(stderr, "explain: load catalog: %v\n", err)
		return 1
	}
	body := transition.RenderTransition(catalog.Definition, id)
	if body == "" {
		fmt.Fprintf(stderr, "explain: transition %q not found in top-level, phase, or global scope\n", id)
		return 1
	}
	fmt.Fprint(stdout, body)
	return 0
}

// schemaValidate validates in-memory bytes against an embedded schema by
// basename. It is a thin wrapper over schema.NewValidator to keep CLI call
// sites terse.
func schemaValidate(root, schemaName string, data []byte) error {
	return schema.NewValidator(root).ValidateBytes(schemaName, data)
}
