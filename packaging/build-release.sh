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
    "$GO" build -trimpath -ldflags="-s -w -X github.com/entroforge/go-system-builder/internal/cli.BuildVersion=$version" \
      -o "$harness_bin_dir/loop-harness-${key}${ext}" \
      "$root/cmd/loop-harness"
}

build_harness darwin  arm64 darwin-arm64
build_harness darwin  amd64 darwin-amd64
build_harness linux   arm64 linux-arm64
build_harness linux   amd64 linux-amd64
build_harness windows amd64 windows-amd64

# Pick the binary matching the current host to regenerate the agent-facing
# Manual. The Manual is platform-independent, but the emitter must run on
# this host. Unsupported hosts fail explicitly; there is no cross-architecture fallback.
host_os="$(uname -s)"
host_arch="$(uname -m)"
case "${host_os}/${host_arch}" in
  Darwin/arm64|Darwin/aarch64)   host_bin="loop-harness-darwin-arm64" ;;
  Darwin/x86_64|Darwin/amd64)    host_bin="loop-harness-darwin-amd64" ;;
  Linux/x86_64|Linux/amd64)      host_bin="loop-harness-linux-amd64" ;;
  Linux/aarch64|Linux/arm64)     host_bin="loop-harness-linux-arm64" ;;
  MINGW*/x86_64|MINGW*/amd64)    host_bin="loop-harness-windows-amd64.exe" ;;
  MSYS*/x86_64|MSYS*/amd64)      host_bin="loop-harness-windows-amd64.exe" ;;
  CYGWIN*/x86_64|CYGWIN*/amd64)  host_bin="loop-harness-windows-amd64.exe" ;;
  *)
    echo "host platform not in release matrix: ${host_os}/${host_arch}" >&2
    exit 1
    ;;
esac

# Generate the agent-facing Manual at the tarball root. The Manual is a
# build artifact derived from docs/control/loop-definition.json + the guard_specs
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

# Source and installed navigation have distinct audiences.
cp "$stage_root/packaging/README.installed.md" "$stage_root/docs/README.md"
cp "$stage_root/packaging/DOCUMENT-MAP.installed.md" "$stage_root/docs/DOCUMENT-MAP.md"
mv "$stage_root/packaging/project.gitattributes" "$stage_root/project.gitattributes"
rm -rf "$stage_root/packaging"
cat > "$stage_root/INSTALL.md" <<'ENTRY'
# Install the Loop Harness

Read [the installation guide](docs/guides/install.md). Use the packaged host
binary's `install --source <this-directory> --root <empty-target>` command.
Existing projects must stay on their matching release; never overlay docs.
ENTRY
cat > "$stage_root/prelude.md" <<'ENTRY'
# Getting started

Read [the project onboarding guide](docs/guides/getting-started.md).
ENTRY
# Validate the actual packaged host binary and document closure, not a local substitute.
cp "$stage_root/tools/loop-harness-launcher.sh" "$harness_bin_dir/loop-harness"
cp "$stage_root/tools/loop-harness-launcher.ps1" "$harness_bin_dir/loop-harness.ps1"
chmod 0755 "$harness_bin_dir/loop-harness"
"$harness_bin_dir/loop-harness" release-graph validate --root "$stage_root" >/dev/null

# Inventory exact staged bytes, including generated manual and each binary.
# Verify the extracted package before installation; this is not a signature.
python3 "$root/tools/release-manifest.py" create --root "$stage_root" --source "$root" --version "$version"
python3 "$root/tools/release-manifest.py" verify --root "$stage_root"

mkdir -p "$(dirname "$output")"
rm -f "$output"
tar -czf "$output" -C "$staged" "vibe-coding-loop-template-$version"

echo "Built $output"
echo "Contents:"
tar -tzf "$output" | sed 's/^/  /'