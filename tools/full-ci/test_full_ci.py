"""Unit checks for CI partitioning, evidence accounting and phase failures."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


race = load("race_summary", HERE / "race-summary.py")
packages = load("verify_packages", HERE / "verify-packages.py")


def event(action, name):
    return json.dumps({"Action": action, "Test": name}) + "\n"


class RacePartitionTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        # Include real Unicode Go identifiers and all supported test families.
        self.names = ["TestAlpha", "Test测试", "ExampleHello", "FuzzValue", "TestOther", "TestLast", "TestMore"]
        for shard in range(race.SHARDS):
            root = self.base / f"cli-race-shard-{shard}"
            root.mkdir()
            (root / "discover.log").write_text("\n".join(self.names) + "\nok\texample.org/project/internal/cli\t1.2s\n")
            race.select(root, str(shard))
            (root / "runner.txt").write_text(f"shard={shard}\ncheckout=commit-1\n")
            for phase in ("init", "harness", "catalog", "discover", "selection", "race", "summary"):
                (root / f"{phase}.exit").write_text("0\n")
            selected = race.lines(root / "selected.txt")
            (root / "race.jsonl").write_text("".join(event("run", name) + event("pass", name) for name in selected))

    def root(self, shard=0):
        return self.base / f"cli-race-shard-{shard}"

    def test_real_partition_unicode_and_skips(self):
        root = self.root()
        name = race.lines(root / "selected.txt")[0]
        (root / "race.jsonl").write_text(event("run", name) + event("run", name + "/sub") + event("skip", name + "/sub") + event("skip", name) + "".join(event("run", n) + event("pass", n) for n in race.lines(root / "selected.txt")[1:]))
        race.aggregate(self.base, "commit-1")
        self.assertIn(name + "/sub", (root / "summary.txt").read_text())

    def test_unknown_discovery_output_is_not_silently_dropped(self):
        path = self.root() / "discover.log"
        path.write_text(path.read_text() + "unexpected status line\n")
        with self.assertRaises(ValueError):
            race.select(self.root(), "0")

    def test_invalid_or_duplicate_names_fail(self):
        path = self.root() / "discover.log"
        path.write_text("TestAlpha\nTestAlpha\nok\texample.org/p\t1s\n")
        with self.assertRaises(ValueError):
            race.select(self.root(), "0")
        path.write_text("TestFoo/bar\nok\texample.org/p\t1s\n")
        with self.assertRaises(ValueError):
            race.select(self.root(), "0")

    def test_empty_partition_matches_nothing(self):
        root = self.root()
        (root / "discover.log").write_text("TestOne\nok\texample.org/p\t1s\n")
        race.select(root, "5")
        self.assertEqual(race.lines(root / "selected.txt"), [])
        self.assertEqual((root / "pattern.txt").read_text().strip(), "a^")
        (root / "race.jsonl").write_text("")
        race.summarize(root)

    def test_missing_run_terminal_duplicate_extra_failure_and_bad_json(self):
        root = self.root()
        name = race.lines(root / "selected.txt")[0]
        other = race.lines(root / "selected.txt")[1:]
        prefix = "".join(event("run", n) + event("pass", n) for n in other)
        for broken in [event("pass", name), event("run", name),
                       event("run", name) * 2 + event("pass", name),
                       event("run", name) + event("pass", name) * 2,
                       event("run", name) + event("fail", name),
                       event("run", name) + event("pass", name) + event("run", "TestExtra") + event("pass", "TestExtra"),
                       event("run", name) + event("pass", name) + "not json\n"]:
            with self.subTest(broken=broken):
                (root / "race.jsonl").write_text(prefix + broken)
                with self.assertRaises(ValueError):
                    race.summarize(root)

    def test_bad_phase_source_and_missing_shard_fail(self):
        root = self.root()
        (root / "catalog.exit").write_text("1\n")
        with self.assertRaises(ValueError):
            race.aggregate(self.base, "commit-1")
        (root / "catalog.exit").write_text("0\n")
        with self.assertRaises(ValueError):
            race.aggregate(self.base, "other-commit")
        (self.base / "cli-race-shard-5").rename(self.base / "missing-5")
        with self.assertRaises(ValueError):
            race.aggregate(self.base, "commit-1")

    def test_shard_nonzero_fails_even_when_events_pass(self):
        root = self.root()
        (root / "race.exit").write_text("1\n")
        with self.assertRaises(ValueError):
            race.summarize(root)


class PackageCoverageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for phase in ("init", "harness", "catalog", "normal-discover", "checks", "fmt-check", "vet", "test", "doctor", "doctor-staged", "validate", "build", "discover", "selection", "race"):
            (self.root / (phase + ".exit")).write_text("0\n")
        (self.root / "normal-discover.log").write_text("example.org/a\nexample.org/b\n")
        (self.root / "packages.txt").write_text("example.org/a\nexample.org/b\n")
        for name in ("test.log", "race.log"):
            (self.root / name).write_text("ok  \texample.org/a\t1s\n?\texample.org/b\t[no test files]\n")

    def test_both_complete_modes(self):
        packages.verify(self.root, "normal")
        packages.verify(self.root, "race")

    def test_missing_duplicate_failure_or_phase_failure(self):
        test = self.root / "test.log"
        original = test.read_text()
        for body in ["ok\texample.org/a\t1s\n", original + "ok\texample.org/a\t1s\n", original.replace("ok  ", "FAIL")]:
            with self.subTest(body=body):
                test.write_text(body)
                with self.assertRaises(ValueError):
                    packages.verify(self.root, "normal")
        test.write_text(original)
        (self.root / "doctor-staged.exit").write_text("1\n")
        with self.assertRaises(ValueError):
            packages.verify(self.root, "normal")

    def test_observer_runs_a_real_command_and_propagates_failure(self):
        env = {**os.environ, "FULL_CI_RESULTS": str(self.root)}
        cmd = ["bash", str(HERE / "run.sh"), "internal-phase", "smoke", "sh", "-c", "echo out; echo err >&2; exit 7"]
        completed = subprocess.run(cmd, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.assertEqual(completed.returncode, 7)
        self.assertEqual((self.root / "smoke.exit").read_text().strip(), "7")
        self.assertIn("out", (self.root / "smoke.log").read_text())
        self.assertIn("err", (self.root / "smoke.stderr").read_text())


class DriverSmokeTests(unittest.TestCase):
    def test_all_drivers_use_real_shell_wiring_and_propagate_outcomes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bin_dir = root / "bin"
            bin_dir.mkdir()
            (bin_dir / "git").write_text("#!/bin/sh\necho smoke-commit\n")
            # Replace expensive external work, preserving the exact command
            # shapes and output protocols that the shell driver orchestrates.
            (bin_dir / "go").write_text('''#!/usr/bin/env python3
import json, os, pathlib, re, sys
a=sys.argv[1:]
names=["TestA", "TestB", "TestC", "TestD", "TestE", "TestF", "TestG"]
if a[:1]==["version"]: print("go version smoke")
elif a[:1]==["env"]: print("linux\\namd64\\n1")
elif a[:1]==["build"]:
    p=pathlib.Path(a[a.index("-o")+1]); p.write_text("harness")
elif a[:2]==["list", "-m"]: print("example.org/project")
elif a[:1]==["list"]: print("example.org/project/internal/cli\\nexample.org/project/pkg")
elif "-list" in a: print("\\n".join(names)+"\\nok\\texample.org/project/internal/cli\\t1s")
elif "-json" in a:
    pattern=a[a.index("-run")+1]
    for n in names:
        if re.search(pattern,n):
            for action in ("run","pass"): print(json.dumps({"Action":action,"Test":n}))
elif "-race" in a:
    for p in a:
        if p.startswith("example.org/"): print("ok\\t"+p+"\\t1s")
else: print("--- PASS: TestPrepareFixtureCatalog (0.01s)")
''')
            (bin_dir / "make").write_text('''#!/usr/bin/env python3
import os, subprocess, sys
a=sys.argv[1:]
if a and a[0]=="-k":
    status=0
    for arg in a:
        if arg.startswith("full-ci-"):
            name=arg.removeprefix("full-ci-")
            rc=subprocess.call(["bash",os.environ["FULL_CI_DRIVER"],"internal-phase",name,sys.argv[0],name])
            status |= bool(rc)
    sys.exit(status)
if a and a[0]=="test": print("ok\\texample.org/project/internal/cli\\t1s\\nok\\texample.org/project/pkg\\t1s")
''')
            for executable in bin_dir.iterdir():
                executable.chmod(0o755)
            env = {**os.environ, "PATH": str(bin_dir) + os.pathsep + os.environ["PATH"],
                   "RUNNER_TEMP": str(root), "GITHUB_STEP_SUMMARY": str(root / "github-summary")}
            for mode in ("normal", "race"):
                env["FULL_CI_RESULTS"] = str(root / mode)
                proc = subprocess.run(["bash", str(HERE / "run.sh"), mode], cwd=root, env=env,
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                report = json.loads((root / mode / "package-coverage.json").read_text())
                self.assertEqual(report["missing"], [])
                self.assertEqual(report["extra"], [])
            collected = root / "collected"
            collected.mkdir()
            for shard in range(race.SHARDS):
                env["FULL_CI_RESULTS"] = str(collected / f"cli-race-shard-{shard}")
                proc = subprocess.run(["bash", str(HERE / "run.sh"), "cli", str(shard)], cwd=root,
                                      env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            race.aggregate(collected, "smoke-commit")
            self.assertIn("shard 5", (root / "github-summary").read_text())


if __name__ == "__main__":
    unittest.main()
