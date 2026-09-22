#!/usr/bin/env sh
# Installed as .claude/bin/loop-harness. Select per invocation, never overwrite a binary.
set -eu
bin_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64|Darwin/aarch64) target=darwin-arm64 ;;
  Darwin/x86_64|Darwin/amd64) target=darwin-amd64 ;;
  Linux/aarch64|Linux/arm64) target=linux-arm64 ;;
  Linux/x86_64|Linux/amd64) target=linux-amd64 ;;
  MINGW*/x86_64|MSYS*/x86_64|CYGWIN*/x86_64|MINGW*/amd64|MSYS*/amd64|CYGWIN*/amd64) target=windows-amd64.exe ;;
  *) echo "Unsupported host: $(uname -s)/$(uname -m); install a native Harness build." >&2; exit 1 ;;
esac
binary="$bin_dir/loop-harness-$target"
if [ ! -f "$binary" ]; then
  echo "Missing native Harness: $binary. Install the same release for this platform." >&2
  exit 1
fi
exec "$binary" "$@"
