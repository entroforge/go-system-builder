#!/usr/bin/env python3
"""Partition and verify top-level Go CLI race tests without dropping names."""
import collections
import json
import pathlib
import re
import sys

SHARDS = 6
PREFIXES = ("Test", "Example", "Fuzz")
TERMINAL = {"pass", "fail", "skip"}


def fail(message):
    raise ValueError(message)


def lines(path):
    return path.read_text(encoding="utf-8").splitlines()


def discover(path):
    names = []
    package_line = 0
    for line in lines(path):
        if not line:
            continue
        if line.startswith(PREFIXES):
            # Test declarations are Go identifiers. Reject an unexpected form
            # rather than silently filtering a valid test out of the suite.
            if any(c.isspace() for c in line) or any(c in line for c in "/\\.*+?()[]{}|^$"):
                fail(f"unexpected Go test name: {line!r}")
            names.append(line)
        elif re.match(r"^(?:ok|\?)\s+\S+", line):
            package_line += 1
        else:
            fail(f"unexpected go test -list output: {line!r}")
    if not names or len(names) != len(set(names)) or package_line != 1:
        fail("empty/duplicate test discovery or unexpected package result")
    return names


def select(root, shard):
    try:
        shard = int(shard)
    except ValueError:
        fail(f"invalid shard {shard!r}")
    if shard not in range(SHARDS):
        fail(f"invalid shard {shard}")
    discovered = discover(root / "discover.log")
    selected = discovered[shard::SHARDS]
    # An empty partition must match no tests: an empty -run matches everything.
    pattern = "^(" + "|".join(re.escape(n) for n in selected) + ")$" if selected else "a^"
    (root / "discovered.txt").write_text("\n".join(discovered) + "\n", encoding="utf-8")
    (root / "selected.txt").write_text("".join(n + "\n" for n in selected), encoding="utf-8")
    (root / "pattern.txt").write_text(pattern + "\n", encoding="utf-8")


def summarize(root):
    selected = lines(root / "selected.txt")
    if len(selected) != len(set(selected)):
        fail("duplicate names in selected tests")
    expected = set(selected)
    runs = collections.Counter()
    terminal = collections.defaultdict(list)
    skips = []
    bad_json = 0
    with (root / "race.jsonl").open(encoding="utf-8") as src:
        for line in src:
            try:
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise ValueError("not an event object")
            except (json.JSONDecodeError, ValueError):
                bad_json += 1
                continue
            name, action = event.get("Test"), event.get("Action")
            if not name:
                continue
            if not isinstance(name, str):
                bad_json += 1
                continue
            if action == "skip":
                skips.append(name)
            if "/" not in name:
                if action == "run":
                    runs[name] += 1
                elif action in TERMINAL:
                    terminal[name].append(action)
    observed = set(runs) | set(terminal)
    missing = sorted(name for name in expected if not runs[name] or not terminal[name])
    duplicates = sorted(name for name in observed if runs[name] > 1 or len(terminal[name]) > 1)
    extra = sorted(observed - expected)
    failed = sorted(name for name in observed if "fail" in terminal[name])
    states = collections.Counter(value[0] for value in terminal.values() if len(value) == 1)
    output = [f"selected_top={len(selected)} observed_top={len(runs)}",
              f"top_level_terminal={dict(states)}", f"missing_top={missing}",
              f"unexpected_top={extra}", f"duplicate_top={duplicates}", f"failed_top={failed}",
              f"skipped_all_levels={sorted(skips)}", f"invalid_json_lines={bad_json}"]
    report = "\n".join(output) + "\n"
    (root / "summary.txt").write_text(report, encoding="utf-8")
    print(report, end="")
    if missing or extra or duplicates or failed or bad_json or (root / "race.exit").read_text().strip() != "0":
        fail("shard execution failed or coverage incomplete")


def aggregate(root, expected_commit):
    if not expected_commit:
        fail("missing expected checkout commit")
    expected_names = {f"cli-race-shard-{n}" for n in range(SHARDS)}
    actual_names = {p.name for p in root.iterdir() if p.is_dir()}
    if actual_names != expected_names:
        fail(f"artifact mismatch: expected={sorted(expected_names)} actual={sorted(actual_names)}")
    discovered_ref = None
    all_selected = []
    for shard in range(SHARDS):
        item = root / f"cli-race-shard-{shard}"
        metadata = lines(item / "runner.txt")
        if metadata[:2] != [f"shard={shard}", "checkout=" + expected_commit]:
            fail(f"shard {shard} is not from expected checkout or has wrong identity")
        for phase in ("init", "harness", "catalog", "discover", "selection", "race", "summary"):
            if (item / f"{phase}.exit").read_text().strip() != "0":
                fail(f"shard {shard} phase {phase} failed")
        discovered = lines(item / "discovered.txt")
        selected = lines(item / "selected.txt")
        if discovered_ref is None:
            discovered_ref = discovered
        if not discovered or len(discovered) != len(set(discovered)) or discovered != discovered_ref:
            fail(f"shard {shard} discovery differs or is invalid")
        if selected != discovered[shard::SHARDS]:
            fail(f"shard {shard} has wrong selection")
        # Re-parse actual discovery and JSON independently of the job result.
        if discover(item / "discover.log") != discovered:
            fail(f"shard {shard} discovery manifest differs from Go output")
        summarize(item)
        all_selected.extend(selected)
    if len(all_selected) != len(set(all_selected)) or set(all_selected) != set(discovered_ref):
        fail("shard selections do not form an exact partition of discovery")
    print(f"PASS: {len(discovered_ref)} discovered top-level Test/Example/Fuzz, "
          f"exactly once in {SHARDS} independently successful processes")
    print("Note: process isolation differs from a single-process package run.")


def main(args):
    if len(args) < 2:
        fail("usage: race-summary.py select ROOT SHARD | summarize ROOT | aggregate ROOT COMMIT")
    operation, root = args[0], pathlib.Path(args[1])
    if operation == "select" and len(args) == 3:
        select(root, args[2])
    elif operation == "summarize" and len(args) == 2:
        summarize(root)
    elif operation == "aggregate" and len(args) == 3:
        aggregate(root, args[2])
    else:
        fail("invalid operation or arguments")


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except (ValueError, OSError) as error:
        print(f"FULL CI FAILURE: {error}", file=sys.stderr)
        sys.exit(1)
