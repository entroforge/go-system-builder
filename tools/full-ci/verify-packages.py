#!/usr/bin/env python3
"""Verify package coverage and each phase of full normal/non-CLI race jobs."""
import json
import pathlib
import re
import sys


def verify(root, mode):
    if mode not in ("normal", "race"):
        raise ValueError(f"invalid mode {mode!r}")
    discover = "normal-discover.log" if mode == "normal" else "packages.txt"
    expected = (root / discover).read_text(encoding="utf-8").splitlines()
    if not expected or len(expected) != len(set(expected)) or any(not p.strip() for p in expected):
        raise ValueError("empty or duplicate package discovery")
    observed = {}
    for line in (root / ("test.log" if mode == "normal" else "race.log")).read_text(encoding="utf-8").splitlines():
        match = re.match(r"^(ok|\?|FAIL)\s+(\S+)(?:\s|$)", line)
        if match:
            result, name = match.groups()
            if name in observed:
                raise ValueError(f"duplicate package output: {name}")
            observed[name] = result
    failed = sorted(name for name, result in observed.items() if result == "FAIL")
    report = {"mode": mode, "expected": len(expected), "observed": len(observed),
              "missing": sorted(set(expected) - observed.keys()),
              "extra": sorted(observed.keys() - set(expected)), "failed": failed}
    (root / "package-coverage.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report))
    if report["missing"] or report["extra"] or failed:
        raise ValueError("package coverage/outcomes mismatch")
    required = ["init", "harness", "catalog"]
    if mode == "normal":
        required += ["normal-discover", "checks", "fmt-check", "vet", "test", "doctor", "doctor-staged", "validate", "build"]
    else:
        required += ["discover", "selection", "race"]
    for phase in required:
        if (root / (phase + ".exit")).read_text(encoding="utf-8").strip() != "0":
            raise ValueError(f"failed phase {phase}")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 3:
            raise ValueError("usage: verify-packages.py ROOT normal|race")
        verify(pathlib.Path(sys.argv[1]), sys.argv[2])
    except (ValueError, OSError) as error:
        print(f"FULL CI FAILURE: {error}", file=sys.stderr)
        sys.exit(1)
