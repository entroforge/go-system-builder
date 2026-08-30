#!/usr/bin/env bash
# Process-boundary smoke for Claude Code Hooks.
#
# This command deliberately has two scopes:
#   1. always: execute the configured Hook command with official payload shapes;
#   2. optional: confirm a real Claude CLI is installed for the interactive
#      same-session exit-2 acceptance described in the platform blueprint.
#
# It never starts an interactive Claude session, spends API budget, or edits
# product files. The latter acceptance still needs a disposable project and a
# human-observed teammate because only Claude Code can prove its continuation
# semantics after a Hook exits 2.
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

# Validate the exact settings-to-command boundary instead of merely grepping
# for event names. Python is used only as a JSON parser; no repository files
# are written by this smoke.
python3 - "$settings" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, encoding="utf-8") as handle:
    settings = json.load(handle)
hooks = settings.get("hooks", {})
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
            if hook.get("type") == "command":
                commands.append(hook)
    if not commands:
        raise SystemExit(f"platform smoke: {event} has no command hook")
    if not any(f"hook --event {event}" in hook.get("command", "") for hook in commands):
        raise SystemExit(f"platform smoke: {event} command does not target the same Harness event")
    for hook in commands:
        timeout = hook.get("timeout", 10)
        if timeout > 10:
            raise SystemExit(f"platform smoke: {event} timeout {timeout}s exceeds the 10s contract")

pretool = json.dumps(hooks.get("PreToolUse", []))
if "mcp__.*" not in pretool:
    raise SystemExit("platform smoke: PreToolUse matcher does not cover mcp__.*")
print(f"PASS settings boundary: {path}")
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
    echo "PASS Claude CLI detected: $version"
    echo "ACCEPTANCE REQUIRED: in a disposable project, observe a real TeammateIdle/SubagentStop exit-2 continuation and record the Claude version."
  fi
else
  if [ "$require_platform" -eq 1 ]; then
    echo "SKIP platform smoke: Claude CLI '$claude_bin' is not installed (exit 77 means environment debt, not a Hook failure)." >&2
    exit 77
  fi
  echo "SKIP Claude platform process: '$claude_bin' is not installed; Hook JSON/process boundary passed."
fi
