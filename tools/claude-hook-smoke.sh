#!/usr/bin/env bash
# Process-boundary smoke for Claude Code Hooks.
#
# This command deliberately has two scopes:
#   1. always: inspect Harness registrations and invoke the supplied Harness
#      directly with representative Hook payloads;
#   2. optional: confirm a real Claude CLI is installed for the interactive
#      continuation checks described in the platform Hook reference.
#
# It never starts an interactive Claude session, spends API budget, or edits
# product files. Actual event routing and continuation still need a disposable
# Claude session; direct Harness calls cannot prove platform behavior.
set -euo pipefail

root="$(pwd)"
require_platform=0
claude_bin="${CLAUDE_BIN:-claude}"
harness_bin="${HARNESS_BIN:-}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --root)
      [ "$#" -ge 2 ] || { echo "--root requires a path" >&2; exit 2; }
      root="$2"
      shift 2
      ;;
    --require-platform)
      require_platform=1
      shift
      ;;
    --claude-bin)
      [ "$#" -ge 2 ] || { echo "--claude-bin requires a command" >&2; exit 2; }
      claude_bin="$2"
      shift 2
      ;;
    --harness)
      [ "$#" -ge 2 ] || { echo "--harness requires a path" >&2; exit 2; }
      harness_bin="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [--root <repo>] [--require-platform] [--claude-bin <cmd>] [--harness <path>]"
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      exit 2
      ;;
  esac
done

root="$(cd "$root" && pwd)"
settings="$root/.claude/settings.json"
if [ ! -f "$settings" ]; then
  settings="$root/settings.json"
fi
[ -f "$settings" ] || { echo "platform smoke: settings.json not found under $root" >&2; exit 1; }

if [ -z "$harness_bin" ]; then
  harness_bin="$root/.claude/bin/loop-harness"
fi
[ -x "$harness_bin" ] || { echo "platform smoke: executable Harness not found at $harness_bin; run `make build` or pass --harness" >&2; exit 1; }

# Static inspection only: this does not execute the registered shell command
# or reproduce Claude Code's JavaScript matcher engine. Process cases below
# invoke --harness directly; use a disposable root for Hook-owned diagnostics.
python3 - "$settings" <<'PY'
import json
import shlex
import sys

path = sys.argv[1]
with open(path, encoding="utf-8") as handle:
    settings = json.load(handle)
hooks = settings.get("hooks", {})

def targets_event(hook, event):
    if hook.get("type") != "command":
        return False
    try:
        argv = shlex.split(hook.get("command", ""))
    except ValueError:
        return False
    if len(argv) < 3 or argv[0].rsplit("/", 1)[-1] != "loop-harness" or argv[1] != "hook":
        return False
    events = []
    for index, arg in enumerate(argv[2:], 2):
        if arg == "--event" and index + 1 < len(argv):
            events.append(argv[index + 1])
        elif arg.startswith("--event="):
            events.append(arg[len("--event="):])
    return events == [event]

def recognized_mcp_coverage(matcher):
    # These forms have known platform semantics. Do not approximate arbitrary
    # JavaScript regular expressions with Python re or search command text.
    return matcher is None or matcher in ("", "*", "mcp__.*", "^mcp__.*", "mcp__.*$", "^mcp__.*$")

events = [
    "PreToolUse", "SessionStart", "SubagentStart", "SubagentStop",
    "TeammateIdle", "Stop", "PostToolUse", "PostToolUseFailure",
    "ConfigChange", "PreCompact",
]
for event in events:
    registrations = hooks.get(event)
    if not registrations:
        raise SystemExit(f"platform smoke: settings missing {event}")
    commands = []
    for registration in registrations:
        for hook in registration.get("hooks", []):
            if targets_event(hook, event):
                commands.append(hook)
    if not commands:
        raise SystemExit(f"platform smoke: {event} has no recognized same-event Harness command registration")
    for hook in commands:
        timeout = hook.get("timeout", 10)
        limit = 30 if event == "PostToolUse" else 10
        if timeout > limit or timeout <= 0:
            raise SystemExit(f"platform smoke: {event} timeout {timeout}s outside 1..{limit}s")

for event in ("PreToolUse", "PostToolUse"):
    if not any(
        recognized_mcp_coverage(registration.get("matcher"))
        and any(targets_event(hook, event) for hook in registration.get("hooks", []))
        for registration in hooks.get(event, [])
    ):
        raise SystemExit(f"platform smoke: no recognized match-all or MCP-wide {event} Harness registration; other matcher forms require Claude platform verification")
print(f"PASS Harness registrations (static; limited matcher forms): {path}")
PY

tmp="$(mktemp -d "${TMPDIR:-/tmp}/loop-harness-platform-smoke.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

run_case() {
  local name="$1"
  local event="$2"
  local payload="$3"
  local expected_code="$4"
  local output code
  set +e
  output="$(printf '%s\n' "$payload" | CLAUDE_PROJECT_DIR="$root" "$harness_bin" hook --event "$event" --root "$root" 2>"$tmp/$name.stderr")"
  code=$?
  set -e
  if [ "$code" -ne "$expected_code" ]; then
    echo "FAIL $name: exit=$code expected=$expected_code" >&2
    sed -n '1,120p' "$tmp/$name.stderr" >&2 || true
    exit 1
  fi
  if [ -n "$output" ]; then
    printf '%s\n' "$output" | python3 -c 'import json,sys; json.load(sys.stdin)'
  fi
  echo "PASS $name: $event exit=$code"
}

run_case session-start SessionStart \
  '{"hook_event_name":"SessionStart","session_id":"platform-smoke-session","source":"startup"}' 0
run_case subagent-start SubagentStart \
  '{"hook_event_name":"SubagentStart","session_id":"platform-smoke-session","agent_id":"platform-smoke-agent","agent_type":"general-purpose"}' 0
run_case teammate-idle TeammateIdle \
  '{"hook_event_name":"TeammateIdle","session_id":"platform-smoke-session","teammate_name":"platform-smoke-teammate","team_name":"platform-smoke-team"}' 0
run_case subagent-stop SubagentStop \
  '{"hook_event_name":"SubagentStop","session_id":"platform-smoke-session","agent_id":"platform-smoke-agent","agent_type":"general-purpose","stop_hook_active":false,"last_assistant_message":"platform hook smoke"}' 0
run_case pretool-read PreToolUse \
  '{"hook_event_name":"PreToolUse","session_id":"platform-smoke-session","tool_name":"Read","tool_use_id":"platform-smoke-tool","tool_input":{"file_path":"README.md"}}' 0
run_case pretool-mcp PreToolUse \
  '{"hook_event_name":"PreToolUse","session_id":"platform-smoke-session","tool_name":"mcp__memory__search_nodes","tool_use_id":"platform-smoke-mcp","tool_input":{"query":"hook smoke"}}' 0
# A failed non-writing payload checks the PostToolUse process boundary without
# receiving any author-edited questionnaire already present in this root.
# Successful custom-tool reception is exercised in disposable test fixtures.
run_case posttool-mcp-failed PostToolUse \
  '{"hook_event_name":"PostToolUse","session_id":"platform-smoke-session","tool_name":"mcp__sandbox__execute","tool_use_id":"platform-smoke-mcp-save","tool_input":{},"tool_response":{"is_error":true}}' 0

echo "PASS direct Harness payload processes (no Claude event routing or MCP server invoked)"

if command -v "$claude_bin" >/dev/null 2>&1; then
  set +e
  version="$($claude_bin --version 2>&1)"
  version_code=$?
  set -e
  if [ "$version_code" -ne 0 ] || [ -z "$version" ]; then
    if [ "$require_platform" -eq 1 ]; then
      echo "SKIP platform smoke: Claude CLI '$claude_bin' is not runnable (exit=$version_code): $version" >&2
      exit 77
    fi
    echo "SKIP Claude platform process: '$claude_bin' is not runnable (exit=$version_code): $version"
  else
    echo "PASS Claude CLI detected: $version (installation only, not workflow acceptance)"
    detected_version="${version%% *}"
    if [ "$detected_version" != "2.1.276" ]; then
      echo "UNVERIFIED Claude version: $detected_version; reference is 2.1.276. Check docs/claude-platform-compatibility.md before deployment."
      if [ "$require_platform" -eq 1 ]; then exit 78; fi
    fi
    echo "UNVERIFIED platform behavior: actual Hook routing and SubagentStop continuation require a disposable Claude session."
  fi
else
  if [ "$require_platform" -eq 1 ]; then
    echo "SKIP platform smoke: Claude CLI '$claude_bin' is not installed (exit 77 means environment debt, not a Hook failure)." >&2
    exit 77
  fi
  echo "SKIP Claude platform process: '$claude_bin' is not installed; Hook JSON/process boundary passed."
fi
