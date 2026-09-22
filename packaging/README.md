# Source and release boundaries

The source repository contains the reusable framework, implementation, tests,
build tooling and contributor reference material. The installable release
contains the assets explicitly listed in `include.txt`, generated CLI binaries,
the generated manual, installation guide and release inventory.

Keep public usage, upgrade and recovery instructions in `docs/`, blank report
templates in `docs/reports/`, and worked examples in `docs/examples/`. A guide
with “repair” in its name remains public when it describes supported recovery
behavior rather than a specific maintenance session.

Do not commit dated optimization plans, session transcripts, local verification
logs, source snapshots, patches or project-instance evidence to this template.
Local maintenance history may be retained under the ignored
`.local/framework-maintenance/` directory. This is not a durable team archive:
preserve required audit evidence in team artifact storage before removing the
checkout. Release evidence belongs in CI artifacts or release attachments,
with version, source identity and actual platform acceptance status.

Documentation entries in `include.txt` are individual files. Add new public
guides and templates explicitly, then verify the staged release. Do not replace
these entries with recursive `docs` or `docs/reports` entries. Packaging cleanup
rules are additional protection, not the inclusion policy.

Before publishing:

1. Run source checks with `make ci-verify`.
2. Inspect the archive and run the staged release checks (`make doctor-staged`):
   `release-graph validate`, `init`, `doctor` and `validate --all` in a disposable copy.
3. Verify extracted bytes with `tools/release-manifest.py` before initialization
   changes them. Check the license, installation guide, public documentation,
   templates and supported-platform binaries are present.
4. Record actual Claude Code workflow acceptance separately. Packaging checks
   and cross-compilation do not attest that a real Claude session passed.

Building an archive does not publish it or authorize a formal release.
