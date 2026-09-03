# Install: Vibe Coding Loop Engineering Template

> Version: see the tarball directory name `vibe-coding-loop-template-<version>`.

This tarball applies Loop Engineering to a target project. The full apply guide
lives in `prelude.md` §1; this is the short version.

## 1. What Is In This Tarball

```text
AGENTS-template.md                          template source for AGENTS.md
prelude.md                                  full onboarding (read §1 for the complete apply map)
loop-template.md                            Wake-up Prompt source -> .claude/loop.md
loop-harness.md                             agent-facing Manual source -> .claude/bin/loop-harness.md
settings.json                               Hook registration for .claude/settings.json
skills/                                     SKILL.md files -> .claude/skills/
agents/                                     agent definitions -> .claude/agents/
docs/                                       templates + Loop definitions + rules -> docs/
blueprint/                                  layered design docs -> .claude/bin/blueprint
packages/design-tokens/                     DTCG tokens + generated CSS
tools/ui-lab/                               Storybook MCP wiring note
tools/visual-qa/                            snapshot-drift protocol (not Thesis proof)
tools/claude-hook-smoke.sh                  Process-boundary Hook smoke and platform acceptance entry
.claude/bin/loop-harness-darwin-arm64       precompiled Harness, macOS arm64 (statically linked)
.claude/bin/loop-harness-linux-amd64        precompiled Harness, Linux x86_64 (statically linked)
.claude/bin/loop-harness-windows-amd64.exe  precompiled Harness, Windows x86_64 (statically linked)
```

The Manual (`loop-harness.md` at the tarball root) is the agent-facing
specification of what `loop-harness` checks at every transition. It deploys
to `.claude/bin/loop-harness.md` next to the chosen binary, so an `ls
.claude/bin/` shows both. Hook `block` and `warn` messages append a deep link
(`See .claude/bin/loop-harness.md#<rule>`), so an agent that hits a gate can
jump straight to the relevant check.

All three Harness binaries are **statically linked** at build time
(`CGO_ENABLED=0`) and the production layout keeps all three in `.claude/bin/`
permanently. See §2 for the full static-linking contract and the machine-switch
workflow.

This tarball does **not** contain:

- `.claude/loop-state.json` — written by `loop-harness init`
- `.claude/loop-events.jsonl` — created empty by `loop-harness init`
- `.claude/hook-decisions.jsonl` — created at runtime
- Go source (`cmd/`, `internal/`, `go.mod`) — only in the source repo

## 2. Static Linking & Multi-Machine Layout

**Static linking is a build-time property.** The three Harness binaries are
produced in the source repo by `make build-all` (or `make release`) with
`CGO_ENABLED=0`, so each binary:

- Bundles the entire Go runtime — no glibc / musl / MSVCRT dependency
- Ships no `.so` / `.dll` siblings — the binary IS the application
- Runs on a clean machine of its target OS/arch with zero install steps
  (no `apt install libc6`, no Visual C++ Redistributable, nothing)

End users do **not** invoke any linker at install time. The binaries are
already statically linked when they arrive; the apply procedure in §3 just
copies them.

**All three binaries stay in `.claude/bin/` permanently** — this is the
production layout for a target project, not a staging area. They are the
canonical fixture of the install; do not delete them as cruft.

```text
.claude/bin/loop-harness-darwin-arm64       macOS arm64  (statically linked)
.claude/bin/loop-harness-linux-amd64        Linux x86_64 (statically linked)
.claude/bin/loop-harness-windows-amd64.exe  Windows x86_64 (statically linked)
.claude/bin/loop-harness                    active binary: copy of whichever matches the current host
.claude/bin/loop-harness.md                 agent-facing Manual (deep-link target for Hook messages)
```

**Switching work machines is a one-command operation.** When you move from a
macOS laptop to a Linux desktop (or any combination), you do not re-download
or re-extract anything. You only re-run the activation `case` block in §3 —
it picks the matching binary and overwrites `.claude/bin/loop-harness` with
it. The three platform binaries remain untouched in `.claude/bin/` and are
ready for the next switch. Going back to the original machine is the same:
re-run the case block.

To check which binary is currently active, compare its SHA against the three
platform binaries:

```bash
shasum -a 256 .claude/bin/loop-harness .claude/bin/loop-harness-{darwin-arm64,linux-amd64,windows-amd64.exe}
```

The active one matches one of the three. To check that a binary is in fact
statically linked (no dynamic loader dependency):

```bash
file .claude/bin/loop-harness-linux-amd64      # ELF ... statically linked
otool -L .claude/bin/loop-harness-darwin-arm64 # (macOS) shows no @rpath deps
```

## 3. Apply To A Target Project

```bash
# From the target project root, with the tarball extracted next to it:
TARDIR=vibe-coding-loop-template-<version>

# Entry and onboarding
cp $TARDIR/AGENTS-template.md AGENTS.md
cp $TARDIR/prelude.md prelude.md

# Claude Code runtime assets
mkdir -p .claude/bin .claude/skills .claude/agents
cp $TARDIR/settings.json .claude/settings.json
cp $TARDIR/loop-template.md .claude/loop.md
cp -R $TARDIR/skills/* .claude/skills/
cp -R $TARDIR/agents/* .claude/agents/

# Drop all three statically-linked Harness binaries into .claude/bin/. They
# are pre-linked (no host libc) and stay here permanently as the production
# layout described in §2 — switching work machines only rewrites the
# unversioned `loop-harness` below.
cp $TARDIR/.claude/bin/loop-harness-darwin-arm64      .claude/bin/loop-harness-darwin-arm64
cp $TARDIR/.claude/bin/loop-harness-linux-amd64       .claude/bin/loop-harness-linux-amd64
cp $TARDIR/.claude/bin/loop-harness-windows-amd64.exe .claude/bin/loop-harness-windows-amd64.exe
chmod +x .claude/bin/loop-harness-darwin-arm64 .claude/bin/loop-harness-linux-amd64 .claude/bin/loop-harness-windows-amd64.exe

# Activate the binary matching the current host. Re-run this block alone on
# a different machine to switch — the three platform binaries above stay put.
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64|Darwin/aarch64)   HARNESS=loop-harness-darwin-arm64 ;;
  Darwin/x86_64|Darwin/amd64)    HARNESS=loop-harness-darwin-arm64 ;;  # Rosetta runs arm64
  Linux/x86_64|Linux/amd64)      HARNESS=loop-harness-linux-amd64 ;;
  Linux/aarch64|Linux/arm64)     HARNESS=loop-harness-linux-amd64 ;;   # no native aarch64 binary yet, use amd64
  MINGW*/x86_64|MINGW*/amd64)    HARNESS=loop-harness-windows-amd64.exe ;;
  MSYS*/x86_64|MSYS*/amd64)      HARNESS=loop-harness-windows-amd64.exe ;;
  CYGWIN*/x86_64|CYGWIN*/amd64)  HARNESS=loop-harness-windows-amd64.exe ;;
  *) echo "unsupported host: $(uname -s)/$(uname -m)" >&2; exit 1 ;;
esac
cp .claude/bin/"$HARNESS" .claude/bin/loop-harness
chmod +x .claude/bin/loop-harness
cp $TARDIR/loop-harness.md .claude/bin/loop-harness.md

# Design blueprint (layered design docs: L1 philosophy, L2 lifecycle plan, ...)
# Kept next to the Manual for on-demand lookup of design intent by agents and humans.
cp -R $TARDIR/blueprint .claude/bin/blueprint

# Documentation tree (templates + Loop definitions + rules)
cp -R $TARDIR/docs .

# Project Design Foundation implementation (tokens, UI Lab, visual-qa protocol)
mkdir -p packages tools
cp -R $TARDIR/packages/* packages/
cp -R $TARDIR/tools/ui-lab tools/ui-lab
cp -R $TARDIR/tools/visual-qa tools/visual-qa
if [ -f "$TARDIR/tools/claude-hook-smoke.sh" ]; then
  cp "$TARDIR/tools/claude-hook-smoke.sh" tools/claude-hook-smoke.sh
fi

# Initialize runtime (not shipped in tarball)
.claude/bin/loop-harness init --root .
```

`loop-harness init` computes local Loop Definition and Hook policy fingerprints
and writes a schema-valid inactive `.claude/loop-state.json` plus an empty
`.claude/loop-events.jsonl`. No manual editing of the runtime file is needed.

Fill in project-level files:

- `AGENTS.md`: replace `{project name}` and the command block
- `docs/project.yaml`: fill the `project`, `configuration`, `context`, `tech_stack` blocks
- `docs/project-map.md`: copy from `docs/project-map-template.md` and fill

## 4. Verify The Install

```bash
.claude/bin/loop-harness doctor --root .
.claude/bin/loop-harness validate --all --root .
```

Both must pass before starting a Loop. The active binary is whichever
matches your current host (see §2); if your machine is in none of the
release matrix above, install Go and run `make build` from the source repo
to produce a host-platform binary.

## 5. Using Go Source Instead (Alternative)

If you prefer to build from source, clone the source repository and run:

```bash
make build        # host-platform binary only -> .claude/bin/loop-harness (CGO on, dev only)
make build-all    # cross-compile all three platforms, CGO_ENABLED=0, statically linked
                  # -> dist/bin/loop-harness-<goos>-<goarch>[.exe]
```

`make build` is for local iteration against the host — CGO stays on so
debugging tools and host-package integrations work. `make build-all`
reproduces exactly what ships in this tarball: each binary is built with
`CGO_ENABLED=0 -trimpath -ldflags="-s -w"`, statically linked, stripped of
symbol/debug tables, and named by platform so the §3 case block can pick
the right one. `make release` is a thin wrapper over `build-all` plus the
tarball stage. Copy `dist/bin/loop-harness-<platform>` into the target
project as above. The Go source is not part of this tarball.

## 6. Next Steps

Read `prelude.md` §2 onward for the responsibility map, progressive disclosure,
and immutable boundaries. Then read `docs/README.md` for the full conceptual
overview.

Starting a Loop is two independent actions:

1. Bind one human-locked REQ:

   ```bash
   .claude/bin/loop-harness req bind \
     --req docs/requirements/REQ-<id>.md \
     --approved-by <human identity>
   ```

2. Separately, start the bare Claude `/loop` schedule. The Wake-up Prompt is
   already installed at `.claude/loop.md`. `/loop` only delivers the prompt on
   a schedule; it does not bind the REQ and does not authorize release.

The two actions are independent: bind creates the Runtime Bookmark, `/loop`
recovers the Driver after compacts. Neither one implies the other.
