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

All five Harness binaries are **statically linked** at build time
(`CGO_ENABLED=0`) and the production layout keeps all five in `.claude/bin/`
permanently. See §2 for the full static-linking contract and the machine-switch
workflow.

This tarball does **not** contain:

- `.claude/loop-state.json` — written by `loop-harness init`
- `.claude/loop-events.jsonl` — created empty by `loop-harness init`
- `.claude/hook-decisions.jsonl` — created at runtime
- Go source (`cmd/`, `internal/`, `go.mod`) — only in the source repo

## 2. Static Linking & Multi-Machine Layout

Follow docs/runtime-portability.md for paired Runtime backup and handoff. Native PowerShell uses .claude/bin/loop-harness.ps1; Bash uses the shell launcher. Never synchronize only the active state through Git.

**Static linking is a build-time property.** The five Harness binaries are
produced in the source repo by `make build-all` (or `make release`) with
`CGO_ENABLED=0`, so each binary:

- Bundles the entire Go runtime — no glibc / musl / MSVCRT dependency
- Ships no `.so` / `.dll` siblings — the binary IS the application
- Runs on a clean machine of its target OS/arch with zero install steps
  (no `apt install libc6`, no Visual C++ Redistributable, nothing)

End users do **not** invoke any linker at install time. The binaries are
already statically linked when they arrive; the apply procedure in §3 just
copies them.

**All five binaries stay in `.claude/bin/` permanently** — this is the
production layout for a target project, not a staging area. They are the
canonical fixture of the install; do not delete them as cruft.

```text
.claude/bin/loop-harness-darwin-amd64       macOS Intel
.claude/bin/loop-harness-linux-arm64        Linux ARM64
.claude/bin/loop-harness-darwin-arm64       macOS arm64  (statically linked)
.claude/bin/loop-harness-linux-amd64        Linux x86_64 (statically linked)
.claude/bin/loop-harness-windows-amd64.exe  Windows x86_64 (statically linked)
.claude/bin/loop-harness                    shell launcher: selects native binary at invocation
.claude/bin/loop-harness.md                 agent-facing Manual (deep-link target for Hook messages)
```

**Switching machines uses the stable launcher; do not copy a platform binary over it.**

Use .claude/bin/loop-harness version in Bash or & .claude/bin/loop-harness.ps1 version in PowerShell to report the executable, platform and build identity.

To check that a binary is in fact
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
# Merge project.gitattributes into existing .gitattributes before binding a REQ.
# New projects without attributes may copy it directly.
cp $TARDIR/AGENTS-template.md AGENTS.md
cp $TARDIR/prelude.md prelude.md
if [ ! -e CLAUDE.md ]; then
  printf '@AGENTS.md\n' > CLAUDE.md
elif ! grep -qxF '@AGENTS.md' CLAUDE.md; then
  printf '\n@AGENTS.md\n' >> CLAUDE.md
fi

# Claude Code runtime assets
mkdir -p .claude/bin .claude/skills .claude/agents
cp $TARDIR/settings.json .claude/settings.json
cp $TARDIR/loop-template.md .claude/loop.md
cp -R $TARDIR/skills/* .claude/skills/
cp -R $TARDIR/agents/* .claude/agents/

# Drop all five statically-linked Harness binaries into .claude/bin/. They
# are pre-linked (no host libc) and stay here permanently as the production
# layout described in §2; the launcher selects the native file each time.
cp $TARDIR/.claude/bin/loop-harness-darwin-arm64      .claude/bin/loop-harness-darwin-arm64
cp $TARDIR/.claude/bin/loop-harness-linux-amd64       .claude/bin/loop-harness-linux-amd64
cp $TARDIR/.claude/bin/loop-harness-windows-amd64.exe .claude/bin/loop-harness-windows-amd64.exe
chmod +x .claude/bin/loop-harness-darwin-arm64 .claude/bin/loop-harness-linux-amd64 .claude/bin/loop-harness-windows-amd64.exe

# Install stable launchers. Each invocation selects the native binary.
cp $TARDIR/.claude/bin/loop-harness-darwin-amd64 .claude/bin/
cp $TARDIR/.claude/bin/loop-harness-linux-arm64 .claude/bin/
cp $TARDIR/tools/loop-harness-launcher.sh .claude/bin/loop-harness
cp $TARDIR/tools/loop-harness-launcher.ps1 .claude/bin/loop-harness.ps1
chmod +x .claude/bin/loop-harness .claude/bin/loop-harness-*
cp $TARDIR/loop-harness.md .claude/bin/loop-harness.md

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
make build-all    # cross-compile all five platforms, CGO_ENABLED=0, statically linked
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
