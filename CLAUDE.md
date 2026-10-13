# Framework source maintenance

This checkout builds Loop Harness and reusable project templates. It is not an
installed business project. `AGENTS-template.md` and `loop-template.md` are
installation inputs, not instructions to start a business REQ in this checkout.

## Start here

- Read `README.md` for the repository map.
- Read `packaging/README.md` when changing source/release boundaries or organizing files.
- Read the relevant part of `blueprint/` for framework design, and `docs/` for
  supported behavior. Load only material needed for the current task.
- Go implementation lives in `cmd/` and `internal/`; regression fixtures live in
  `tests/`. Follow the existing package structure.

## Preserve source and evidence

- Preserve existing uncommitted work. Do not reset or bulk-copy a development
  checkout over this tree.
- Keep reusable instructions, schemas, templates and examples in source control.
- Put dated maintenance reports, raw logs, snapshots and backup files under
  ignored `.local/framework-maintenance/`, with original paths and hashes when moving them.
- Keep `docs/reports/` for reusable report templates and their documentation.
  Business-project reports belong in the corresponding installed project.
- Keep package, installation and rollback receipts with their matching assets.
  Historical reports are evidence about a version, not current instructions.

## Change and validate

- `go test ./internal/<package> -run '<relevant tests>'` checks a focused code change.
- Documentation-only organization needs link, inventory and preservation checks;
  it does not require rebuilding binaries or rerunning browser E2E.
- `make ci-verify` is the full pre-publication check, not the default for every edit.
- `make manual` regenerates `loop-harness.md`; do not hand-edit generated gate text.
- `packaging/include.txt` is the explicit release allowlist. Source-maintainer
  instructions, `blueprint/`, `.local/`, logs and backup files must not ship.
- Source `skills/`, `agents/` and `settings.json` are installed into the target's
  `.claude/` locations. Preserve that mapping and matching binary/assets.
- Keep Runtime state, journal and installed-project updates separate from source
  housekeeping. Document exactly which validation ran and its limits.
