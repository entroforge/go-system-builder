package hook_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// TestPreToolUseBlockUsesOfficialOutputShape verifies the block-decision payload
// is the official Claude Code Hook PreToolUse shape: hookSpecificOutput.permissionDecision="deny".
func TestPreToolUseBlockUsesOfficialOutputShape(t *testing.T) {
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision:      "block",
		RuleID:        "HOOK_AGENT_NOT_ACTIVATED",
		Reason:        "Phase-one Agent cannot edit files.",
		Recovery:      []string{"Wait for activation."},
		Retry:         "never",
		HumanRequired: true,
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("structured decision must exit 0, got %d", exitCode)
	}

	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	specific := decoded["hookSpecificOutput"].(map[string]any)
	if specific["hookEventName"] != "PreToolUse" {
		t.Fatalf("unexpected event: %v", specific["hookEventName"])
	}
	if specific["permissionDecision"] != "deny" {
		t.Fatalf("unexpected permission decision: %v", specific["permissionDecision"])
	}
	if decoded["systemMessage"] == nil {
		t.Fatalf("systemMessage must accompany block payload")
	}
}

// TestPreToolUseAllowStaysSilent verifies allow decisions produce no payload.
func TestPreToolUseAllowStaysSilent(t *testing.T) {
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{Decision: "allow"}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 || len(output) != 0 {
		t.Fatalf("allow should be silent, output=%q exit=%d", output, exitCode)
	}
}

// TestTeammateIdleWarnReturnsReminderWithoutBlocking verifies a HOOK_*
// warn decision produces a plain systemMessage reminder, not a deny.
func TestTeammateIdleWarnReturnsReminderWithoutBlocking(t *testing.T) {
	output, exitCode, err := hook.RenderWithRoot("", "TeammateIdle", policy.Decision{
		Decision:      "warn",
		RuleID:        "HOOK_TEAMMATE_IDLE_STALE",
		Reason:        "The teammate is idle without a report.",
		Recovery:      []string{"Submit a completion report."},
		Retry:         "not_applicable",
		HumanRequired: false,
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("structured decision must exit 0, got %d", exitCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded["systemMessage"] == nil {
		t.Fatalf("warn must carry a systemMessage, got %v", decoded)
	}
	if decoded["hookSpecificOutput"] != nil {
		t.Fatalf("non-PreToolUse warn must not include hookSpecificOutput, got %v", decoded)
	}
	if !strings.Contains(decoded["systemMessage"].(string), "HOOK_TEAMMATE_IDLE_STALE") {
		t.Fatalf("systemMessage must surface rule_id, got %v", decoded["systemMessage"])
	}
}

// TestPreToolUseWarnRendersAllowWithSystemMessage — a warn on PreToolUse must
// be observation-only (BE-003 §8.5): permissionDecision="allow" + systemMessage.
func TestPreToolUseWarnRendersAllowWithSystemMessage(t *testing.T) {
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision:      "warn",
		RuleID:        "HOOK_UI_PROTOTYPE_GATE",
		Reason:        "UI-impacting contracts require a valid final UI design package.",
		Recovery:      []string{"Create or update the module UI design package and validate it."},
		Retry:         "rerun after recovery validation",
		HumanRequired: false,
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("warn must exit 0, got %d", exitCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	specific := decoded["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != "allow" {
		t.Fatalf("warn on PreToolUse must allow the tool call, got %v", specific["permissionDecision"])
	}
	if specific["permissionDecisionReason"] == nil {
		t.Fatalf("warn on PreToolUse must carry permissionDecisionReason")
	}
}

// TestWarningObservationsStayNonBlocking sweeps warning-only rules and asserts
// none of them ever resolve to a deny. Adapter is stateless and must not promote
// warn to block (BE-003 §8.5).
func TestWarningObservationsStayNonBlocking(t *testing.T) {
	hoRules := []string{
		"HOOK_UI_PROTOTYPE_GATE",
		"HOOK_CLEAN_ROUND_GATE",
		"HOOK_SUBAGENT_REPORT_INCOMPLETE",
		"HOOK_TEAMMATE_IDLE_STALE",
	}
	for _, rule := range hoRules {
		t.Run(rule, func(t *testing.T) {
			for _, event := range []string{"PreToolUse", "PostToolUse"} {
				output, exitCode, err := hook.RenderWithRoot("", event, policy.Decision{
					Decision:      "warn",
					RuleID:        rule,
					Reason:        "HO-* observation",
					Recovery:      []string{"Recover and retry."},
					Retry:         "rerun after recovery validation",
					HumanRequired: false,
				}, policy.RuntimeContext{})
				if err != nil {
					t.Fatalf("render %s: %v", event, err)
				}
				if exitCode != 0 {
					t.Fatalf("HO-* %s on %s must exit 0, got %d", rule, event, exitCode)
				}
				var decoded map[string]any
				if err := json.Unmarshal(output, &decoded); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if specific, ok := decoded["hookSpecificOutput"].(map[string]any); ok {
					if specific["permissionDecision"] == "deny" {
						t.Fatalf("HO-* %s on %s must NEVER deny, got %v", rule, event, specific)
					}
				}
			}
		})
	}
}

func TestRecoverableDenyRejectsWithoutHumanGateway(t *testing.T) {
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "deny", RuleID: "HOOK_AGENT_NOT_ACTIVATED",
		Reason: "Activation is missing.", Recovery: []string{"Activate and retry."},
		Retry: "rerun after recovery validation", HumanRequired: false,
	}, policy.RuntimeContext{})
	if err != nil || exitCode != 0 {
		t.Fatalf("render recoverable deny: exit=%d err=%v", exitCode, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatal(err)
	}
	specific := decoded["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != "deny" {
		t.Fatalf("recoverable deny must reject the tool: %v", specific)
	}
	if strings.Contains(decoded["systemMessage"].(string), "Human required") {
		t.Fatalf("recoverable deny must not fabricate a human Gateway: %v", decoded)
	}
}

// TestHSMinimalSafetyBlocksDenyOnPreToolUse — the minimal REQ-039 safety
// boundary retains only two block-classified rules (HOOK_LOCKED_ARTIFACT_WRITE
// and HOOK_SQUASH_MERGE). They must surface as permissionDecision="deny" on
// PreToolUse and as plain systemMessage on lifecycle events (SessionStart/
// SubagentStart/SubagentStop). RenderWithRoot is the library entry for these
// legacy rule paths; the layered PreToolUse callers go through
// PreToolUseWithQualityGate (adapter.go preamble).
func TestHSMinimalSafetyBlocksDenyOnPreToolUse(t *testing.T) {
	hsRules := []string{"HOOK_LOCKED_ARTIFACT_WRITE", "HOOK_SQUASH_MERGE"}
	for _, rule := range hsRules {
		t.Run(rule, func(t *testing.T) {
			// PreToolUse path
			output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
				Decision:      "block",
				RuleID:        rule,
				Reason:        "HS-* strong block",
				Recovery:      []string{"Surface the matching Gateway."},
				Retry:         "never",
				HumanRequired: true,
			}, policy.RuntimeContext{})
			if err != nil {
				t.Fatalf("render PreToolUse: %v", err)
			}
			if exitCode != 0 {
				t.Fatalf("block must exit 0, got %d", exitCode)
			}
			var decoded map[string]any
			if err := json.Unmarshal(output, &decoded); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			specific := decoded["hookSpecificOutput"].(map[string]any)
			if specific["permissionDecision"] != "deny" {
				t.Fatalf("HS-* %s PreToolUse must deny, got %v", rule, specific["permissionDecision"])
			}

			// Lifecycle event path
			output, _, err = hook.RenderWithRoot("", "SessionStart", policy.Decision{
				Decision:      "block",
				RuleID:        rule,
				Reason:        "HS-* strong block",
				Recovery:      []string{"Surface the matching Gateway."},
				Retry:         "never",
				HumanRequired: true,
			}, policy.RuntimeContext{})
			if err != nil {
				t.Fatalf("render SessionStart: %v", err)
			}
			var lifecycle map[string]any
			if err := json.Unmarshal(output, &lifecycle); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if lifecycle["hookSpecificOutput"] != nil {
				t.Fatalf("HS-* %s on SessionStart must NOT carry hookSpecificOutput, got %v", rule, lifecycle)
			}
			if lifecycle["systemMessage"] == nil {
				t.Fatalf("HS-* %s on SessionStart must carry systemMessage", rule)
			}
		})
	}
}

// TestHAAuditDecisionsAppendOnlyToJSONL — audit decisions must NEVER influence
// the tool caller. They write one JSONL line and produce no protocol payload.
func TestHAAuditDecisionsAppendOnlyToJSONL(t *testing.T) {
	haRules := []string{"HOOK_SESSION_STARTED", "HOOK_SUBAGENT_STARTED", "HOOK_PRECOMPACT_CHECKPOINT"}
	auditPath := setAuditCwd(t)

	for _, rule := range haRules {
		t.Run(rule, func(t *testing.T) {
			before := readAuditLineCount(t, auditPath)
			output, exitCode, err := hook.RenderWithRoot("", "SessionStart", policy.Decision{
				Decision: "audit",
				RuleID:   rule,
				Reason:   "HA-* observation",
			}, policy.RuntimeContext{})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if exitCode != 0 {
				t.Fatalf("audit must exit 0, got %d", exitCode)
			}
			if len(output) != 0 {
				t.Fatalf("audit must produce no payload, got %q", output)
			}
			after := readAuditLineCount(t, auditPath)
			if after-before != 1 {
				t.Fatalf("audit must append exactly one line, before=%d after=%d", before, after)
			}
		})
	}
}

// TestAdapterIsStateless — repeated identical decisions must produce identical
// output. Hook v1 must not carry session-wide state across renders.
func TestAdapterIsStateless(t *testing.T) {
	first, exitFirst, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision:      "warn",
		RuleID:        "HOOK_AGENT_NOT_ACTIVATED",
		Reason:        "Phase-one Agent cannot edit files.",
		Recovery:      []string{"Approve readback and commit a bounded activation envelope."},
		Retry:         "rerun after recovery validation",
		HumanRequired: false,
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, exitSecond, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision:      "warn",
		RuleID:        "HOOK_AGENT_NOT_ACTIVATED",
		Reason:        "Phase-one Agent cannot edit files.",
		Recovery:      []string{"Approve readback and commit a bounded activation envelope."},
		Retry:         "rerun after recovery validation",
		HumanRequired: false,
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if exitFirst != exitSecond {
		t.Fatalf("exit codes diverged: %d vs %d", exitFirst, exitSecond)
	}
	if string(first) != string(second) {
		t.Fatalf("adapter is not stateless: %s vs %s", first, second)
	}
}

// TestRenderWithRootWritesToRootNotCwd locks in Fix #1's canonical entry
// point: RenderWithRoot(root, ...) must append the audit line to
// <root>/.claude/hook-decisions.jsonl, NOT the host cwd. The legacy
// Render(...) entry is preserved as a cwd-fallback for in-process library
// users; this test pins the canonical CLI-facing path.
func TestRenderWithRootWritesToRootNotCwd(t *testing.T) {
	host := t.TempDir()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(host); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	hostAudit := filepath.Join(host, ".claude", "hook-decisions.jsonl")
	rootAudit := filepath.Join(root, ".claude", "hook-decisions.jsonl")

	output, exitCode, err := hook.RenderWithRoot(root, "SessionStart", policy.Decision{
		Decision: "audit",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "RenderWithRoot canonical entry",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("audit must exit 0, got %d", exitCode)
	}
	if len(output) != 0 {
		t.Fatalf("audit must produce no payload, got %q", output)
	}
	if _, err := os.Stat(hostAudit); err == nil {
		t.Fatalf("audit must NOT write to host cwd at %s", hostAudit)
	}
	if _, err := os.Stat(rootAudit); err != nil {
		t.Fatalf("audit must write to <root>/.claude/hook-decisions.jsonl, got %v", err)
	}
}

// TestRenderWithRootEmptyRootFallsBackToCwd verifies the legacy Render(...)
// behavior survives under RenderWithRoot("", ...) — empty root resolves to
// os.Getwd(). Library-mode callers that pass the empty string continue to
// see bit-identical cwd-targeting writes.
func TestRenderWithRootEmptyRootFallsBackToCwd(t *testing.T) {
	auditPath := setAuditCwd(t)
	before := readAuditLineCount(t, auditPath)
	output, exitCode, err := hook.RenderWithRoot("", "SessionStart", policy.Decision{
		Decision: "audit",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "empty-root fallback to cwd",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("audit must exit 0, got %d", exitCode)
	}
	if len(output) != 0 {
		t.Fatalf("audit must produce no payload, got %q", output)
	}
	after := readAuditLineCount(t, auditPath)
	if after-before != 1 {
		t.Fatalf("empty-root must fall back to cwd, before=%d after=%d", before, after)
	}
}

// TestRenderWithRootPreservesCwd — DV-2 §3 stress recommendation: chdir
// into t.TempDir(), call RenderWithRoot, then assert cwd is unchanged
// (still the temp dir we chdir'd to — not whatever RenderWithRoot might
// have moved it to as a side effect). Catches a class of bug where
// RenderWithRoot (or its audit-write path) mutates the process working
// directory as a side effect.
func TestRenderWithRootPreservesCwd(t *testing.T) {
	root := t.TempDir()
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(originalCwd)
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	// Resolve symlinks for macOS (/var/folders → /private/var/folders).
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = root
	}
	if _, _, err := hook.RenderWithRoot(root, "SessionStart", policy.Decision{
		Decision: "audit",
		RuleID:   "HOOK_SESSION_STARTED",
	}, policy.RuntimeContext{}); err != nil {
		t.Fatalf("render: %v", err)
	}
	currentCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after: %v", err)
	}
	resolvedCurrent, err := filepath.EvalSymlinks(currentCwd)
	if err != nil {
		resolvedCurrent = currentCwd
	}
	if resolvedCurrent != resolvedRoot {
		t.Fatalf("RenderWithRoot must not mutate cwd: expected=%s after=%s", resolvedRoot, resolvedCurrent)
	}
}

// TestAuditAppendHighConcurrencyStress — DV-2 §3 stress recommendation #1:
// N=500 parallel Render(audit) calls through auditMu. After T2 the audit
// branch is no longer reached from cli.Run (audit decisions skip Render),
// but Render itself is still library-callable and the audit-write path
// is shared. The test pins the invariant: auditMu serializes N appends,
// final line count == N, no torn lines.
func TestAuditAppendHighConcurrencyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("audit-stress: skipped under -short")
	}
	root := t.TempDir()
	auditPath := filepath.Join(root, ".claude", "hook-decisions.jsonl")
	defer os.Remove(auditPath)
	const N = 500
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			_, _, err := hook.RenderWithRoot(root, "SessionStart", policy.Decision{
				Decision: "audit",
				RuleID:   "HOOK_SESSION_STARTED",
			}, policy.RuntimeContext{})
			if err != nil {
				t.Errorf("audit %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	lines := readAuditLines(t, auditPath)
	if len(lines) != N {
		t.Fatalf("expected %d audit lines, got %d", N, len(lines))
	}
}

// TestAuditAppendFailsWhenMkdirAllFails proves that a read-only parent dir
// produces a non-nil error from Render's audit branch (closing contract:
// hook_decisions_jsonl_writes_report_io_errors).
func TestAuditAppendFailsWhenMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Make .claude read-only so MkdirAll on .claude/<subdir> fails.
	ro := filepath.Join(dir, ".claude")
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	output, exitCode, err := hook.RenderWithRoot("", "SessionStart", policy.Decision{
		Decision: "audit",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "audit under read-only parent",
	}, policy.RuntimeContext{})
	if err == nil {
		t.Fatalf("expected audit append to fail under read-only .claude/, got nil (output=%q exit=%d)", output, exitCode)
	}
	if exitCode == 0 {
		t.Fatalf("audit failure must surface non-zero exit, got %d", exitCode)
	}
	if len(output) != 0 {
		t.Fatalf("audit failure must not produce a protocol payload, got %q", output)
	}
}

// TestAuditAppendFailsWhenAuditPathIsDirectory — pre-creating the audit file
// path as a directory causes OpenFile(O_CREATE|O_APPEND) to fail on every OS,
// which the audit branch must surface as an error (closing contract:
// hook_decisions_jsonl_writes_report_io_errors).
func TestAuditAppendFailsWhenAuditPathIsDirectory(t *testing.T) {
	auditPath := setAuditCwd(t)
	if err := os.MkdirAll(auditPath, 0o755); err != nil {
		t.Fatalf("mkdir audit path: %v", err)
	}
	output, exitCode, err := hook.RenderWithRoot("", "SessionStart", policy.Decision{
		Decision: "audit",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "audit path pre-created as a directory",
	}, policy.RuntimeContext{})
	if err == nil {
		t.Fatalf("expected audit append to fail when audit path is a directory, got nil (output=%q exit=%d)", output, exitCode)
	}
	if exitCode == 0 {
		t.Fatalf("audit failure must surface non-zero exit, got %d", exitCode)
	}
	if len(output) != 0 {
		t.Fatalf("audit failure must not produce a protocol payload, got %q", output)
	}
}

// TestAuditAppendIsConcurrencySafe fires N parallel Render(audit) calls and
// asserts every line lands cleanly in the JSONL — no interleaved bytes, no
// partial writes, line count == N (closing contract:
// hook_decisions_jsonl_writes_are_concurrency_safe).
func TestAuditAppendIsConcurrencySafe(t *testing.T) {
	const n = 25
	auditPath := setAuditCwd(t)
	before := readAuditLineCount(t, auditPath)

	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			output, exitCode, err := hook.RenderWithRoot("", "SessionStart", policy.Decision{
				Decision:       "audit",
				RuleID:         "HOOK_SESSION_STARTED",
				Reason:         "concurrent audit append",
				MatchedRuleIDs: []string{"HOOK_SESSION_STARTED"},
			}, policy.RuntimeContext{})
			if err != nil {
				t.Errorf("goroutine %d: render: %v", i, err)
				return
			}
			if exitCode != 0 {
				t.Errorf("goroutine %d: exit code %d", i, exitCode)
			}
			if len(output) != 0 {
				t.Errorf("goroutine %d: unexpected payload %q", i, output)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	after := readAuditLineCount(t, auditPath)
	if after-before != n {
		t.Fatalf("expected %d new audit lines, got %d (before=%d after=%d)", n, after-before, before, after)
	}
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Allow long records.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	decoded := 0
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("audit line %d is not valid JSON: %v\n%s", decoded, err, scanner.Text())
		}
		if record["decision"] != "audit" || record["rule_id"] != "HOOK_SESSION_STARTED" {
			t.Fatalf("audit line %d has unexpected fields: %#v", decoded, record)
		}
		decoded++
	}
	if decoded != n {
		t.Fatalf("expected %d decodable lines, got %d", n, decoded)
	}
}

// TestRenderAuditOnPreToolUseStaysSilent — audit decisions never surface on
// PreToolUse, regardless of event name. Audit is an append-only observation
// that must not influence the tool caller.
func TestRenderAuditOnPreToolUseStaysSilent(t *testing.T) {
	auditPath := setAuditCwd(t)
	before := readAuditLineCount(t, auditPath)
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "audit",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "audit on PreToolUse must remain silent",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("audit must exit 0, got %d", exitCode)
	}
	if len(output) != 0 {
		t.Fatalf("audit on PreToolUse must produce no payload, got %q", output)
	}
	after := readAuditLineCount(t, auditPath)
	if after-before != 1 {
		t.Fatalf("audit must append exactly one line, before=%d after=%d", before, after)
	}
}

// TestMessageBuildsExactlyFormattedBodyWithAllFourFields pins the message()
// helper's exact contract: rule_id — reason. Missing: …. Recovery: …. Retry=….
// Human required. when all four message sub-sections are populated.
func TestMessageBuildsExactlyFormattedBodyWithAllFourFields(t *testing.T) {
	output, exitCode, err := hook.RenderWithRoot("", "TeammateIdle", policy.Decision{
		Decision:      "warn",
		RuleID:        "HOOK_TEAMMATE_IDLE_STALE",
		Reason:        "The idle assignment has no current-round report.",
		Missing:       []string{"assignment report"},
		Recovery:      []string{"submit progress", "submit blocker status"},
		Retry:         "not_applicable",
		HumanRequired: true,
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("structured decision must exit 0, got %d", exitCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	body, ok := decoded["systemMessage"].(string)
	if !ok {
		t.Fatalf("expected systemMessage string, got %#v", decoded["systemMessage"])
	}
	// QA-2 §2 row 3: replace byte-exact brittleness with a structural
	// shape check. The previous `body != want` would fail on any future
	// copy-edit of the message format — pin the load-bearing fragments
	// instead (rule_id, reason, missing, recovery, retry, human_required)
	// so a cosmetic tweak cannot silently drift the test.
	prefix := "HOOK_TEAMMATE_IDLE_STALE — The idle assignment has no current-round report."
	if !strings.HasPrefix(body, prefix) {
		t.Fatalf("systemMessage must start with %q, got %q", prefix, body)
	}
	for _, fragment := range []string{
		"Missing: assignment report",
		"Recovery: submit progress",
		"submit blocker status",
		"Retry=not_applicable",
		"Human required",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("systemMessage must contain %q, got %q", fragment, body)
		}
	}
}

// TestRenderUnknownDecisionReturnsSilentDefault — the default branch in
// Render must treat any unrecognised decision literal as silent (closing
// contract: hook_decisions_strict_enum_no_passthrough).
func TestRenderUnknownDecisionReturnsSilentDefault(t *testing.T) {
	auditPath := setAuditCwd(t)
	before := readAuditLineCount(t, auditPath)
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "unknown",
		RuleID:   "HOOK_RUNTIME_INTEGRITY",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("unknown decision must not error, got %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("unknown decision must exit 0, got %d", exitCode)
	}
	if len(output) != 0 {
		t.Fatalf("unknown decision must produce no payload, got %q", output)
	}
	after := readAuditLineCount(t, auditPath)
	if after != before {
		t.Fatalf("unknown decision must not touch the audit log, before=%d after=%d", before, after)
	}
}

// TestAuditAppendInvokesSyncOnSuccess covers the new Sync() guard in
// appendAuditLine — closing contract: hook_decisions_jsonl_writes_call_fsync.
// fsync is an OS syscall that is not directly observable from a Go test, so
// the assertion is structural: a successful Render(audit) on a writable
// audit log must surface no error and exit 0, which can only happen if
// Sync() returned nil (the call path between Write and return).
func TestAuditAppendInvokesSyncOnSuccess(t *testing.T) {
	auditPath := setAuditCwd(t)
	before := readAuditLineCount(t, auditPath)
	output, exitCode, err := hook.RenderWithRoot("", "SessionStart", policy.Decision{
		Decision: "audit",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "fsync observable only via Write+Sync round-trip",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("expected successful audit append (Sync call path), got %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("audit must exit 0, got %d", exitCode)
	}
	if len(output) != 0 {
		t.Fatalf("audit must produce no payload, got %q", output)
	}
	after := readAuditLineCount(t, auditPath)
	if after-before != 1 {
		t.Fatalf("audit must append exactly one line, before=%d after=%d", before, after)
	}
	// Verify the line is fully written (n == len(serialized)) — the short-
	// write guard sits between Write and Sync, so a clean append is the
	// strongest signal we have that the Write-then-Sync chain completed.
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("audit line must end with newline (Sync invariant), got %q", data)
	}
}

// TestMessageAppendsManualAnchorOnBlock — REQ-005 P1 discoverability: every
// block message must end with ` See .claude/bin/loop-harness.md#<lowercase(rule_id)>`
// so an agent hitting a gate failure can deep-link to the spec. Uses the
// surviving HOOK_LOCKED_ARTIFACT_WRITE block rule (the minimal REQ-039
// safety boundary retains only locked_artifact_write and squash_merge).
func TestMessageAppendsManualAnchorOnBlock(t *testing.T) {
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision:      "block",
		RuleID:        "HOOK_LOCKED_ARTIFACT_WRITE",
		Reason:        "locked_artifact_write",
		Missing:       []string{"new generation of the locked artifact"},
		Recovery:      []string{"create a new version through the formal rework path"},
		Retry:         "never",
		HumanRequired: true,
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("block must exit 0, got %d", exitCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	specific := decoded["hookSpecificOutput"].(map[string]any)
	reason, ok := specific["permissionDecisionReason"].(string)
	if !ok {
		t.Fatalf("expected permissionDecisionReason string, got %#v", specific["permissionDecisionReason"])
	}
	want := "See .claude/bin/loop-harness.md#hook_locked_artifact_write"
	if !strings.Contains(reason, want) {
		t.Fatalf("block permissionDecisionReason must contain %q, got %q", want, reason)
	}
}

// TestMessageAppendsManualAnchorOnWarn — REQ-005 P1 discoverability: warn
// messages also carry the manual anchor (warn surfaces via systemMessage on
// PreToolUse and lifecycle events, so we check systemMessage directly). Uses
// HOOK_SQUASH_MERGE — the second surviving block-classified rule on the
// minimal REQ-039 safety boundary — driven here as a warn observation.
func TestMessageAppendsManualAnchorOnWarn(t *testing.T) {
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "warn",
		RuleID:   "HOOK_SQUASH_MERGE",
		Reason:   "Warn-only observation on squash_merge candidate.",
		Retry:    "not_applicable",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("warn must exit 0, got %d", exitCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	body, ok := decoded["systemMessage"].(string)
	if !ok {
		t.Fatalf("expected systemMessage string, got %#v", decoded["systemMessage"])
	}
	want := "See .claude/bin/loop-harness.md#hook_squash_merge"
	if !strings.Contains(body, want) {
		t.Fatalf("warn systemMessage must contain %q, got %q", want, body)
	}
}

// TestMessageAnchorUsesLowercaseRuleID — the anchor suffix must be lowercased
// even when the rule ID is mixed-case (e.g. REQ_LOCKED → #req_locked).
func TestMessageAnchorUsesLowercaseRuleID(t *testing.T) {
	output, _, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "warn",
		RuleID:   "REQ_LOCKED",
		Reason:   "Guard surfaced through warn path.",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	body, _ := decoded["systemMessage"].(string)
	if !strings.Contains(body, "#req_locked") {
		t.Fatalf("anchor must use lowercase suffix #req_locked, got %q", body)
	}
	if strings.Contains(body, "#REQ_LOCKED") {
		t.Fatalf("anchor must NOT contain uppercase #REQ_LOCKED, got %q", body)
	}
}

// TestMessageOmitsAnchorWhenRuleIDEmpty — an empty RuleID must skip the
// manual-anchor sentence entirely (no `See .claude/bin/loop-harness.md`
// fragment leaks into the message).
func TestMessageOmitsAnchorWhenRuleIDEmpty(t *testing.T) {
	output, _, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "warn",
		RuleID:   "",
		Reason:   "Warn without an explicit rule id.",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	body, _ := decoded["systemMessage"].(string)
	if strings.Contains(body, "See .claude/bin/loop-harness.md") {
		t.Fatalf("empty RuleID must omit manual anchor, got %q", body)
	}
}

// TestAuditDecisionsDoNotCarryManualAnchor — the audit path is structured
// JSONL data, not a human-facing message. The manual anchor sentence must
// never appear in the audit record's fields (only in warn/block systemMessage).
func TestAuditDecisionsDoNotCarryManualAnchor(t *testing.T) {
	auditPath := setAuditCwd(t)
	before := readAuditLineCount(t, auditPath)
	output, exitCode, err := hook.RenderWithRoot("", "SessionStart", policy.Decision{
		Decision: "audit",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "audit must stay structured data, no manual anchor",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("audit must exit 0, got %d", exitCode)
	}
	if len(output) != 0 {
		t.Fatalf("audit must produce no payload, got %q", output)
	}
	after := readAuditLineCount(t, auditPath)
	if after-before != 1 {
		t.Fatalf("audit must append exactly one line, before=%d after=%d", before, after)
	}
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "See .claude/bin/loop-harness.md") {
		t.Fatalf("audit record must not carry the manual anchor sentence, got %q", string(data))
	}
}

// setAuditCwd switches into a temp dir so appendAuditLine writes to a clean
// .claude/hook-decisions.jsonl. Returns the absolute audit path for assertions.
func setAuditCwd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return filepath.Join(dir, ".claude", "hook-decisions.jsonl")
}

func readAuditLineCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	if len(data) == 0 {
		return 0
	}
	return strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1
}

// readAuditLines returns the audit-file contents split into lines (empty
// entries skipped). Used by the N=500 concurrency stress test to verify
// every concurrent write landed a complete record.
func readAuditLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestStagePrefixOnWarn prepends a "[<S-cursor> <state>.<phase> @ rev=<N>]"
// banner to every warn systemMessage when RuntimeContext is populated, so an
// agent that hits a gate failure immediately sees its position. The prefix is
// absent when RuntimeContext is empty (preserves pre-banner byte-identity for
// integrity-failure paths and existing structural tests).
func TestStagePrefixOnWarn(t *testing.T) {
	rt := policy.RuntimeContext{
		RuntimeID:    "loop-REQ-002",
		Revision:     3,
		CurrentState: "planning",
		CurrentPhase: "design",
	}
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision:      "warn",
		RuleID:        "HOOK_AGENT_NOT_ACTIVATED",
		Reason:        "The subagent is not activated for this tool or path.",
		Recovery:      []string{"approve readback and commit a bounded activation envelope"},
		Retry:         "rerun after recovery validation",
		HumanRequired: false,
	}, rt)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("warn must exit 0, got %d", exitCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	body, ok := decoded["systemMessage"].(string)
	if !ok {
		t.Fatalf("expected systemMessage string, got %#v", decoded["systemMessage"])
	}
	prefix := "[S2 planning.design @ rev=3]"
	if !strings.HasPrefix(body, prefix) {
		t.Fatalf("systemMessage must start with stage prefix %q, got %q", prefix, body)
	}
	if !strings.Contains(body, "HOOK_AGENT_NOT_ACTIVATED") {
		t.Fatalf("systemMessage must still surface rule_id after prefix, got %q", body)
	}
}

// TestStagePrefixOnBlock — same prefix contract on the block path so the
// strongest gate failures also surface the agent's position. Uses the
// surviving HOOK_LOCKED_ARTIFACT_WRITE rule.
func TestStagePrefixOnBlock(t *testing.T) {
	rt := policy.RuntimeContext{
		RuntimeID:    "loop-REQ-002",
		Revision:     5,
		CurrentState: "verification",
		CurrentPhase: "delivery",
	}
	output, _, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "block",
		RuleID:   "HOOK_LOCKED_ARTIFACT_WRITE",
		Reason:   "locked_artifact_write",
		Retry:    "never",
	}, rt)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	specific := decoded["hookSpecificOutput"].(map[string]any)
	reason, _ := specific["permissionDecisionReason"].(string)
	if !strings.HasPrefix(reason, "[S7 verification.delivery @ rev=5]") {
		t.Fatalf("block reason must start with stage prefix, got %q", reason)
	}
}

// TestStagePrefixOmittedWhenRuntimeEmpty — empty RuntimeContext (integrity
// failure / not loaded) produces no prefix, preserving byte-identical output
// for the existing body. This protects callers that don't yet populate
// RuntimeContext (e.g. legacy tests, dry-run paths).
func TestStagePrefixOmittedWhenRuntimeEmpty(t *testing.T) {
	output, _, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "warn",
		RuleID:   "HOOK_AGENT_NOT_ACTIVATED",
		Reason:   "Phase-one Agent cannot edit files.",
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	body, _ := decoded["systemMessage"].(string)
	if strings.HasPrefix(body, "[") {
		t.Fatalf("empty runtime must not prepend a stage prefix, got %q", body)
	}
	if !strings.HasPrefix(body, "HOOK_AGENT_NOT_ACTIVATED") {
		t.Fatalf("body must start with rule_id when prefix is absent, got %q", body)
	}
}

// TestInfoDecisionEmitsStageBanner — info classification emits a standalone
// stage banner (lifecycle context framing). On non-PreToolUse events the
// payload is a plain systemMessage; on PreToolUse it carries permissionDecision
// ="allow" so the original tool action still proceeds.
func TestInfoDecisionEmitsStageBanner(t *testing.T) {
	rt := policy.RuntimeContext{
		RuntimeID:    "loop-REQ-002",
		Revision:     7,
		CurrentState: "building",
		BoundREQPath: "docs/requirements/REQ-002-self-evolution.md",
	}
	output, exitCode, err := hook.RenderWithRoot("", "SessionStart", policy.Decision{
		Decision: "info",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "Record session recovery context.",
	}, rt)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("info must exit 0, got %d", exitCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded["hookSpecificOutput"] != nil {
		t.Fatalf("info on SessionStart must not carry hookSpecificOutput, got %v", decoded)
	}
	body, ok := decoded["systemMessage"].(string)
	if !ok || body == "" {
		t.Fatalf("info must emit a non-empty systemMessage banner, got %#v", decoded["systemMessage"])
	}
	for _, fragment := range []string{"Stage ", "S6 building", "rev=7", "REQ-002-self-evolution"} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("info banner must contain %q, got %q", fragment, body)
		}
	}
}

// TestInfoDecisionOnPreToolUseAllows — info on PreToolUse must never deny;
// it emits permissionDecision="allow" + systemMessage so the original tool
// action proceeds while still surfacing the stage banner.
func TestInfoDecisionOnPreToolUseAllows(t *testing.T) {
	rt := policy.RuntimeContext{
		Revision:     2,
		CurrentState: "planning",
		CurrentPhase: "initialize",
		BoundREQPath: "docs/requirements/REQ-001.md",
	}
	output, exitCode, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "info",
		RuleID:   "HOOK_SESSION_STARTED",
		Reason:   "Record session recovery context.",
	}, rt)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("info must exit 0, got %d", exitCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	specific := decoded["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != "allow" {
		t.Fatalf("info on PreToolUse must allow, got %v", specific["permissionDecision"])
	}
}
