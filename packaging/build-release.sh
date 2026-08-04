#!/usr/bin/env bash
# Build a distributable tarball containing only template assets.
#
# Usage: packaging/build-release.sh <version> <output-tarball-path>
#
# Reads packaging/include.txt (whitelist of paths relative to repo root),
# stages them under a versioned directory, and produces a gzip tarball.
# Portable across macOS bsdtar and GNU tar (no --transform dependency).
#
# The Harness binary is cross-compiled in this script for every supported
# platform (CGO disabled, statically linked, no host-libc dependency) and
# landed in .claude/bin/ with a per-platform suffix so INSTALL.md can pick
# the right one by `uname`. The current host's binary is also used to
# regenerate loop-harness.md (the agent-facing Manual) since the Manual is
# platform-independent but the emitter must run on this host.

set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <version> <output-tarball-path>" >&2
  exit 2
fi

version="$1"
output="$2"
root="$(cd "$(dirname "$0")/.." && pwd)"
include="$root/packaging/include.txt"
GO="${GO:-go}"
staged="$(mktemp -d)"
trap 'rm -rf "$staged"' EXIT

stage_root="$staged/vibe-coding-loop-template-$version"
mkdir -p "$stage_root"

while IFS= read -r line; do
  # Skip comments and blank lines.
  case "$line" in
    ''|'#'*) continue ;;
  esac
  item="$(echo "$line" | sed 's/[[:space:]]*$//; s/^[[:space:]]*//')"
  [ -z "$item" ] && continue
  src="$root/$item"
  dst="$stage_root/$item"
  if [ ! -e "$src" ]; then
    echo "include entry missing: $item" >&2
    exit 1
  fi
  mkdir -p "$(dirname "$dst")"
  if [ -d "$src" ]; then
    cp -R "$src" "$dst"
  else
    cp "$src" "$dst"
  fi
done < "$include"

# Cross-compile the Harness for every release platform with CGO disabled.
# Each binary is statically linked against the Go runtime and ships without
# any host-libc dependency, so the same artifact runs on a clean machine of
# each supported OS/arch. Naming is loop-harness-<goos>-<goarch>[.exe] so
# INSTALL.md's uname-based dispatch can pick the right one.
harness_bin_dir="$stage_root/.claude/bin"
mkdir -p "$harness_bin_dir"

build_harness() {
  local goos="$1" goarch="$2" key="$3"
  local ext=""
  if [ "$goos" = "windows" ]; then ext=".exe"; fi
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    "$GO" build -trimpath -ldflags="-s -w" \
      -o "$harness_bin_dir/loop-harness-${key}${ext}" \
      "$root/cmd/loop-harness"
}

build_harness darwin  arm64 darwin-arm64
build_harness linux   amd64 linux-amd64
build_harness windows amd64 windows-amd64

# Pick the binary matching the current host to regenerate the agent-facing
# Manual. The Manual is platform-independent, but the emitter must run on
# this host. aarch64 Linux and Rosetta-on-Intel macOS fall back to amd64
# binaries (Go's amd64 runs under Rosetta; native aarch64 is not in the
# release matrix yet).
host_os="$(uname -s)"
host_arch="$(uname -m)"
case "${host_os}/${host_arch}" in
  Darwin/arm64|Darwin/aarch64)   host_bin="loop-harness-darwin-arm64" ;;
  Darwin/x86_64|Darwin/amd64)    host_bin="loop-harness-darwin-arm64" ;;
  Linux/x86_64|Linux/amd64)      host_bin="loop-harness-linux-amd64" ;;
  Linux/aarch64|Linux/arm64)     host_bin="loop-harness-linux-amd64" ;;
  MINGW*/x86_64|MINGW*/amd64)    host_bin="loop-harness-windows-amd64.exe" ;;
  MSYS*/x86_64|MSYS*/amd64)      host_bin="loop-harness-windows-amd64.exe" ;;
  CYGWIN*/x86_64|CYGWIN*/amd64)  host_bin="loop-harness-windows-amd64.exe" ;;
  *)
    echo "host platform not in release matrix: ${host_os}/${host_arch}" >&2
    exit 1
    ;;
esac

# Generate the agent-facing Manual at the tarball root. The Manual is a
# build artifact derived from docs/loop-definition.json + the guard_specs
# registry compiled into the binary; it is regenerated on every release so
# the tarball always ships a Manual matching the binary's behavior. The
# tarball ships it at the root (visible template source); the install guide
# copies it to .claude/bin/loop-harness.md in target projects so it sits
# beside the binary and Hook deep links resolve. Target projects can further
# refresh it via `loop-harness manual` or `loop-harness init`.
"$harness_bin_dir/$host_bin" manual \
  --root "$stage_root" \
  --target loop-harness.md >/dev/null
if [ ! -s "$stage_root/loop-harness.md" ]; then
  echo "manual generation produced empty file" >&2
  exit 1
fi

# Rename packaging/install.md -> INSTALL.md at the tarball root.
if [ -f "$stage_root/packaging/install.md" ]; then
  mv "$stage_root/packaging/install.md" "$stage_root/INSTALL.md"
  rmdir "$stage_root/packaging" 2>/dev/null || true
fi

# Exclude the entire design/loop-engineering/ directory from the tarball.
# Its methodology is represented by the reusable Skills; its remaining files
# are source-project rationale rather than target-project template assets.
rm -rf "$stage_root/docs/design/loop-engineering"

# Target projects start with templates, not this source project's instances.
find "$stage_root/docs/reports" -type f ! -name '*-template.md' -delete 2>/dev/null || true
# Clean up any empty subdirectories left behind.
find "$stage_root/docs/reports" -type d -empty -delete 2>/dev/null || true

# Exclude any local instance artifact accidentally present at packaging time.
find "$stage_root/docs/tasks" -type f \( -name 'TASK-[0-9]*.md' -o -name 'index-[0-9]*.md' \) -delete 2>/dev/null || true
find "$stage_root/docs/requirements" -type f -name 'REQ-[0-9]*.md' -delete 2>/dev/null || true
find "$stage_root/docs/contracts" -type f ! -name '*-template.md' ! -name 'README.md' -delete 2>/dev/null || true
find "$stage_root/docs/design" -type f \( -name 'ARCHITECTURE-[0-9]*.md' -o -name '*-039-*.md' \) -delete 2>/dev/null || true
rm -f "$stage_root"/docs/loop-definition.json.bak-*
find "$stage_root/docs/release_audits" -mindepth 1 -maxdepth 1 \
  ! -name 'TEMPLATE.md' ! -name 'protected_commands.json' -exec rm -rf {} + 2>/dev/null || true

mkdir -p "$(dirname "$output")"
rm -f "$output"
tar -czf "$output" -C "$staged" "vibe-coding-loop-template-$version"

echo "Built $output"
echo "Contents:"
tar -tzf "$output" | sed 's/^/  /'