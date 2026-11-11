"""Lightweight driver regression tests; never install, build or call Claude."""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
import shlex
import sys
import time
import tempfile
import threading
import unittest
from unittest.mock import patch


DRIVER_PATH = Path(__file__).with_name('claude-full-stage-simulation.py')
SPEC = importlib.util.spec_from_file_location('stage_simulation', DRIVER_PATH)
DRIVER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DRIVER)


class SyntheticCandidateTests(unittest.TestCase):
    def test_repairable_lane_has_a_real_independent_fixture_mismatch(self):
        spec = DRIVER.SCENARIOS['ledger-repairable']
        candidates = DRIVER.REPO/spec['candidate_dir']
        DRIVER.validate_candidate_inputs('ledger-repairable', candidates)
        for relative in ('docs/requirements/REQ-001.md', 'docs/architecture/ARCHITECTURE-001.md',
                         'docs/design/decisions/ADR-001.md', 'docs/dev/contracts/CONTRACTS-001.md',
                         'docs/dev/contracts/BE-001.md', 'docs/dev/tasks/TASK-001-01.md',
                         'docs/dev/tasks/index-REQ-001.md', 'docs/project-map.md'):
            self.assertIn(spec['repairable_path'], (candidates/relative).read_text(), relative)
        requirement = (candidates/'docs/requirements/REQ-001.md').read_text()
        self.assertIn('AC-001 / AC-002', requirement)
        self.assertIn('§7（AC-001）/ §8（AC-002）', (candidates/'docs/dev/contracts/CONTRACTS-001.md').read_text())
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            DRIVER.write_scenario_marker(root, 'ledger-repairable')
            status = root/spec['repairable_path']
            self.assertEqual(status.read_text(), 'PENDING\n')
            self.assertNotEqual(status.read_text(), 'READY\n')
            self.assertEqual(subprocess.run(['make', '-s', 'check'], cwd=root, capture_output=True).returncode, 0)
            delivery = root/spec['delivery_path']
            delivery.parent.mkdir(parents=True)
            delivery.write_text('SIMULATION ONLY: source ' + spec['marker_path'] + '\n')
            self.assertEqual(subprocess.run(shlex.split(DRIVER.delivery_check_command('ledger-repairable')),
                                            cwd=root, capture_output=True).returncode, 0)
            # Repair is a one-line synthetic placeholder edit; the comparison
            # reads the actual file and does not assume a workflow result.
            status.write_text('READY\n')
            self.assertEqual(status.read_text(), 'READY\n')
            self.assertNotIn('S8 模拟调查支持该原因假设', spec['prompt'])
            self.assertIn('不得为了经过 S9 伪造结论', spec['prompt'])

    def test_independent_inputs_exclude_runtime_and_author_answers(self):
        scenarios = [DRIVER.SCENARIOS[name] for name in ('ledger', 'offline-cli')]
        self.assertNotEqual(scenarios[0]['candidate_dir'], scenarios[1]['candidate_dir'])
        self.assertNotEqual(scenarios[0]['marker_path'], scenarios[1]['marker_path'])
        self.assertNotEqual(scenarios[0]['marker'], scenarios[1]['marker'])
        for scenario in scenarios:
            root = DRIVER.REPO/scenario['candidate_dir']
            self.assertTrue((root/'docs/requirements/REQ-001.md').is_file())
            self.assertTrue((root/'docs/dev/tasks/TASK-001-01.md').is_file())
            for file in root.rglob('*'):
                if file.is_file():
                    self.assertNotIn('.claude', file.parts)
                    self.assertNotIn(file.name, ('answer.json', 'loop-state.json', 'loop-events.jsonl'))
                    self.assertNotIn('receipt', file.name.lower())
            task = (root/'docs/dev/tasks/TASK-001-01.md').read_text()
            self.assertIn(scenario['marker_path'], task)
            self.assertIn(scenario['marker'], task)

    def test_marker_check_reports_actual_bytes_for_each_scenario(self):
        for name in ('ledger', 'offline-cli'):
            with self.subTest(scenario=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                DRIVER.write_scenario_marker(root, name)
                command = ['make', '-s', 'check']
                good = subprocess.run(command, cwd=root, capture_output=True, text=True)
                self.assertEqual(good.returncode, 0, good.stderr)
                (root/DRIVER.SCENARIOS[name]['marker_path']).write_text('not-the-synthetic-marker\n')
                bad = subprocess.run(command, cwd=root, capture_output=True, text=True)
                self.assertNotEqual(bad.returncode, 0)

    def test_s6_delivery_is_new_committed_and_checked_after_creation(self):
        for name in ('ledger', 'offline-cli'):
            with self.subTest(scenario=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                scenario = DRIVER.SCENARIOS[name]
                DRIVER.validate_candidate_inputs(name, DRIVER.REPO/scenario['candidate_dir'])
                DRIVER.write_scenario_marker(root, name)
                output = root/scenario['delivery_path']
                self.assertFalse(output.exists())
                # The real make commands distinguish the baseline and delivery.
                baseline = subprocess.run(['make', '-s', 'check'], cwd=root, capture_output=True)
                self.assertEqual(baseline.returncode, 0)
                command = shlex.split(DRIVER.delivery_check_command(name))
                self.assertNotEqual(subprocess.run(command, cwd=root, capture_output=True).returncode, 0)
                for args in (['init', '-q'], ['config', 'user.name', 'Simulation Test'],
                             ['config', 'user.email', 'test@example.invalid'], ['add', '.'],
                             ['commit', '-qm', 'Preset read-only simulation input']):
                    subprocess.run(['git', *args], cwd=root, check=True, capture_output=True)
                initial_head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root)
                output.parent.mkdir(parents=True)
                for invalid in ('', 'SIMULATION missing input reference', scenario['marker_path']):
                    output.write_text(invalid)
                    self.assertNotEqual(subprocess.run(command, cwd=root, capture_output=True).returncode, 0)
                output.write_text('SIMULATION ONLY: source ' + scenario['marker_path'] + '\n')
                subprocess.run(command, cwd=root, check=True, capture_output=True)
                subprocess.run(['git', 'add', scenario['delivery_path']], cwd=root, check=True)
                subprocess.run(['git', 'commit', '-qm', 'Create simulation delivery note'], cwd=root, check=True)
                self.assertNotEqual(subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root), initial_head)
                changed = subprocess.check_output(['git', 'diff', '--name-only', 'HEAD^', 'HEAD'],
                                                  cwd=root, text=True).splitlines()
                self.assertEqual(changed, [scenario['delivery_path']])
                with self.assertRaises(SystemExit):
                    DRIVER.write_scenario_marker(root, name)

    def test_candidates_do_not_claim_previous_runtime_or_review_history(self):
        base = DRIVER.REPO/'tests/fixtures/native-stage-simulation'
        for directory in ('documents', 'ledger-calibrated', 'offline-calibrated'):
            for path in (base/directory).rglob('*.md'):
                body = path.read_text()
                for stale in ('TR-004', 'sha256 ', 'doctor passed', '第 2 轮', '第 3 轮',
                              '状态：locked', '状态：accepted', '绑定回执 TR-001'):
                    self.assertNotIn(stale, body, str(path))



class LaunchTests(unittest.TestCase):
    def launch_case(self, timeout=None, failure=None):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base/'project'
            (root/'.claude/bin').mkdir(parents=True)
            env = base/'provider-env'
            env.write_text('')
            executable = base/'fake-claude'
            executable.write_text('fixture')
            files = [executable, base/'release.tar.gz', root/'.claude/bin/loop-harness',
                     root/'.claude/bin/loop-harness-linux-amd64']
            meta = {'claude_binary':str(executable)}
            for path in files:
                path.write_text('fixture')
            DRIVER.dump(base/'metadata.json', meta)
            (base/'prompt.txt').write_text(DRIVER.PROMPT)
            # Exercise a continuation; first-request freezing is independent.
            (base/'claude-run-1.jsonl').write_text('')
            with patch.object(DRIVER.subprocess, 'run', side_effect=failure,
                              return_value=subprocess.CompletedProcess([], 0)) as process, \
                    patch.object(DRIVER, 'summarize') as summary:
                with self.assertRaises(SystemExit) as result:
                    DRIVER.launch(base, env, 'existing-session', None,
                                  timeout_seconds=timeout)
                self.assertEqual(process.call_args.kwargs['timeout'], timeout)
                command = process.call_args.args[0]
                self.assertEqual(command[-2:], ['--resume', 'existing-session'])
                self.assertIn('source "$1"', command[4])
                self.assertIn('--permission-mode', command)
                self.assertIn('bypassPermissions', command)
                self.assertNotIn('--disallowedTools', command)
                self.assertNotIn('--allowedTools', command)
                summary.assert_called_once_with(base)
            request = json.loads((base/'request-2.json').read_text())
            record = json.loads((base/'run-2.json').read_text())
            self.assertEqual(request['timeout_seconds'], timeout)
            self.assertEqual(record['timeout_seconds'], timeout)
            self.assertGreaterEqual(record['seconds'], 0)
            self.assertEqual(result.exception.code, record['exit_code'])
            return record

    def test_no_deadline_is_default(self):
        self.assertEqual(self.launch_case()['exit_code'], 0)

    def test_explicit_deadline(self):
        self.assertEqual(self.launch_case(27)['exit_code'], 0)

    def test_timeout_records_intervention(self):
        record = self.launch_case(27, subprocess.TimeoutExpired('fake', 27))
        self.assertEqual(record['exit_code'], 124)
        self.assertEqual(record['external_interference'], 'driver timeout')

    def test_interrupt_keeps_record_and_summary(self):
        record = self.launch_case(failure=KeyboardInterrupt())
        self.assertEqual(record['exit_code'], 130)
        self.assertEqual(record['external_interference'], 'driver interrupted by SIGINT')

    def test_deadline_argument_is_positive(self):
        self.assertEqual(DRIVER.positive_seconds('1'), 1)
        for value in ('0', '-1', 'nan', '1.5'):
            with self.assertRaises(argparse.ArgumentTypeError):
                DRIVER.positive_seconds(value)

    def test_empty_evidence_is_unverified_not_green(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            (base/'project/.claude').mkdir(parents=True)
            DRIVER.dump(base/'metadata.json', {'fixture_files':{}})
            DRIVER.dump(base/'project/.claude/loop-state.json',
                        {'lifecycle':{'state':'inactive'}, 'evidence':[]})
            with patch('builtins.print'):
                with self.assertRaises(SystemExit):
                    DRIVER.verify(base)
            result = json.loads((base/'verification.json').read_text())
            self.assertIsNone(result['observation_checks']['registered_evidence_sources_intact'])
            self.assertIsNone(result['full_flow_proven'])
            audit = json.loads((base/'registered-evidence-source-audit.json').read_text())
            self.assertEqual(audit['status'], 'not_checked')


class ObservationTests(unittest.TestCase):
    def test_scope_allows_normal_tools_and_local_commits(self):
        self.assertIn('local Git commits are allowed', DRIVER.BOUNDARY)
        for forbidden in ('Do not commit', 'only if the actual original consumer accepts',
                          'FLOW-LOG', 'BLOCKED-<STAGE>'):
            self.assertNotIn(forbidden, DRIVER.BOUNDARY)
        self.assertIn('不额外制造 ObservationBatch', DRIVER.PROMPT)
        self.assertNotIn('明确禁止', DRIVER.PROMPT)

    def test_stream_sidecar_keeps_full_output_and_arrival_time(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            stdout, stderr = base/'stdout', base/'stderr'
            text = 'full feedback ' + 'x' * 8000 + '\n'
            stdout.write_text(text)
            stderr.write_text('real failure\n')
            stopped = threading.Event()
            stopped.set()
            DRIVER.observe_model_streams([stdout, stderr], base/'timeline', stopped)
            rows = [json.loads(line) for line in (base/'timeline').read_text().splitlines()]
            self.assertEqual([row['text'] for row in rows], [text, 'real failure\n'])
            self.assertTrue(all(row['observed_ns'] > 0 for row in rows))
            self.assertEqual(stdout.read_text(), text)

    def test_original_hook_feedback_and_exit_are_forwarded(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base/'project'
            (project/'.claude').mkdir(parents=True)
            DRIVER.dump(project/'.claude/loop-state.json', {'revision':1,'review':{'repair':None}})
            # A real tiny local command exercises the observer, never Claude/Go.
            command = "printf 'original feedback'; printf 'original error' >&2; exit 7"
            DRIVER.dump(base/'original-settings.json', {'hooks':{
                'PreToolUse':[{'hooks':[{'command':command}]}]}})
            observer = base/'observer.py'
            observer.write_text(DRIVER.OBSERVE)
            result = subprocess.run(['python3', str(observer), str(base), str(project),
                'PreToolUse', '0', '0', command], input='{"agent_id":"test-agent"}',
                text=True, capture_output=True)
            self.assertEqual((result.returncode,result.stdout,result.stderr),
                             (7,'original feedback','original error'))
            record = json.loads(next(base.glob('hook-*.json')).read_text())
            self.assertEqual(record['payload'], {'agent_id':'test-agent'})
            self.assertEqual(record['stdout'], result.stdout)
            self.assertEqual(record['stderr'], result.stderr)
            self.assertGreaterEqual(record['elapsed_seconds'], 0)

    def test_hook_announces_before_completion_and_drains_large_original_output(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base/'project'
            (project/'.claude').mkdir(parents=True)
            DRIVER.dump(project/'.claude/loop-state.json', {'revision':1})
            gate = base/'finish-original-hook'
            script = ('from pathlib import Path\nimport time,sys\n'
                      f'gate=Path({str(gate)!r})\n'
                      'while not gate.exists(): time.sleep(0.01)\n'
                      'sys.stdout.write("x"*131072)\n'
                      'sys.stderr.write("y"*131072)\n'
                      'sys.exit(9)\n')
            command = shlex.join([sys.executable,'-c',script])
            DRIVER.dump(base/'original-settings.json', {'hooks':{
                'SessionStart':[{'hooks':[{'command':command}]}]}})
            observer = base/'observer.py'
            observer.write_text(DRIVER.OBSERVE)
            process = subprocess.Popen([sys.executable,str(observer),str(base),str(project),
                'SessionStart','0','0',command],stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            payload = {'session_id':'actual','transcript_path':str(base/'actual.jsonl')}
            process.stdin.write(json.dumps(payload))
            process.stdin.close()
            process.stdin = None
            try:
                deadline = time.monotonic()+2
                announcement = []
                while time.monotonic() < deadline:
                    announcement = list(base.glob('transcript-input-*.json'))
                    if announcement:
                        try:
                            record = json.loads(announcement[0].read_text())
                            break
                        except ValueError:
                            pass
                    time.sleep(0.01)
                else:
                    self.fail('Original Hook did not announce before completing')
                self.assertIsNone(process.poll())
                self.assertFalse(list(base.glob('hook-*.json')))
                self.assertEqual(record['payload'],payload)
            finally:
                gate.touch()
                try:
                    out,err = process.communicate(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.communicate()
                    raise
            self.assertEqual((process.returncode,out,err),(9,'x'*131072,'y'*131072))

    def test_normal_commands_and_fixture_change_are_diagnostics(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base/'project'
            (project/'.claude').mkdir(parents=True)
            (project/'simulation/products').mkdir(parents=True)
            (project/'simulation/products/new-placeholder.txt').write_text('SIMULATION')
            DRIVER.dump(base/'metadata.json', {'fixture_files':{}})
            DRIVER.dump(project/'.claude/loop-state.json',
                        {'lifecycle':{'state':'inactive'},'evidence':[]})
            DRIVER.dump(base/'hook-1.json', {'event':'PreToolUse','started_ns':1,
                'finished_ns':2,'before':{},'after':{},'exit_code':0,
                'stdout':'','stderr':'','payload':{'agent_id':'worker',
                'tool_name':'Bash','tool_input':{'command':'git add . && git commit -m SIMULATION && make check'}}})
            with patch('builtins.print'):
                with self.assertRaises(SystemExit):
                    DRIVER.verify(base)
            summary = json.loads((base/'summary.json').read_text())
            self.assertIsNone(summary['fixture_changed'])
            self.assertIn('simulation/products/new-placeholder.txt', summary['fixture_actual'])
            self.assertEqual(summary['potential_boundary_violations'], [])
            verification = json.loads((base/'verification.json').read_text())
            self.assertNotIn('fixture_unchanged', verification['observation_checks'])


class TranscriptTests(unittest.TestCase):
    def fixture(self, directory):
        base = Path(directory)/'artifacts'
        projects = Path(directory)/'projects'
        base.mkdir()
        project = projects/'actual-project'
        project.mkdir(parents=True)
        main = project/'actual-session.jsonl'
        DRIVER.dump(base/'transcript-input-session.json', {'event':'SessionStart','payload':{
            'session_id':'actual-session','transcript_path':str(main)}})
        return base, projects, main

    def records(self, sidecar):
        return [json.loads(line) for line in sidecar.index.read_text().splitlines()]

    def test_live_incremental_main_subagent_and_metadata_before_stop(self):
        with tempfile.TemporaryDirectory() as directory:
            base, projects, main = self.fixture(directory)
            # An unannounced adjacent session must never be captured.
            (main.parent/'unrelated-session.jsonl').write_bytes(b'PRIVATE OTHER SESSION')
            subagents = main.parent/'actual-session/subagents'
            subagents.mkdir(parents=True)
            child = subagents/'agent-worker-1.jsonl'
            metadata = subagents/'agent-worker-1.meta.json'
            main.write_bytes(b'Main partial \xff')
            child.write_bytes(b'Worker running')
            metadata.write_bytes(b'{"agentId":"worker-1"}')
            observer = DRIVER.TranscriptSidecar(base, 1, projects)
            self.assertGreater(observer.poll(), 0)
            self.assertEqual(observer.poll(), 0)
            with child.open('ab') as stream:
                stream.write(b' still running')
            self.assertEqual(observer.poll(), len(b' still running'))
            rows = [row for row in self.records(observer) if row['status']=='bytes']
            child_rows = [row for row in rows if row['source']==str(child)]
            self.assertEqual([row['offset_start'] for row in child_rows], [0,len(b'Worker running')])
            self.assertEqual((base/child_rows[0]['artifact']).read_bytes(), b'Worker running still running')
            self.assertTrue(any(row['actor']=='Main' for row in rows))
            self.assertTrue(any(row['kind']=='metadata' for row in rows))
            self.assertTrue(all(row['observed_ns'] > 0 for row in rows))
            captured = b''.join(path.read_bytes() for path in observer.directory.glob('*.bin'))
            self.assertIn(b'Main partial \xff', captured)
            self.assertNotIn(b'PRIVATE OTHER SESSION', captured)

    def test_rotation_and_same_size_truncation_keep_original_segments(self):
        with tempfile.TemporaryDirectory() as directory:
            base, projects, main = self.fixture(directory)
            main.write_bytes(b'original')
            observer = DRIVER.TranscriptSidecar(base, 1, projects)
            observer.poll()
            main.write_bytes(b'replaced')  # Same inode and size: anchor detects overwrite.
            observer.poll()
            main.rename(main.with_suffix('.old'))
            main.write_bytes(b'rotated')
            observer.poll()
            rows = [row for row in self.records(observer) if row['status']=='bytes']
            self.assertEqual([row['segment'] for row in rows], [0,1,2])
            self.assertEqual([(base/row['artifact']).read_bytes() for row in rows],
                             [b'original',b'replaced',b'rotated'])

    def test_missing_and_outside_session_paths_remain_unknown(self):
        with tempfile.TemporaryDirectory() as directory:
            base, projects, main = self.fixture(directory)
            outside = Path(directory)/'provider-env'
            outside.write_text('DO NOT READ')
            DRIVER.dump(base/'transcript-input-outside.json', {'event':'SessionStart','payload':{
                'session_id':'provider-env','transcript_path':str(outside)}})
            DRIVER.dump(base/'transcript-input-missing.json', {'event':'SessionStart','payload':{'session_id':'missing'}})
            observer = DRIVER.TranscriptSidecar(base, 1, projects)
            observer.poll()
            rows = self.records(observer)
            self.assertTrue(any(row.get('status')=='unknown' and row.get('source')==str(main) for row in rows))
            self.assertTrue(any('lacks transcript_path' in row.get('reason','') for row in rows))
            self.assertFalse(list(observer.directory.glob('*.bin')))
            self.assertNotIn('DO NOT READ', observer.index.read_text())

    def test_final_drain_collects_visible_bytes_without_an_actor_stop(self):
        with tempfile.TemporaryDirectory() as directory:
            base, projects, main = self.fixture(directory)
            main.write_bytes(b'final partial output')
            observer = DRIVER.TranscriptSidecar(base, 1, projects)
            stopped = threading.Event()
            stopped.set()  # Simulates model interrupt before the next poll.
            observer.run(stopped)
            self.assertTrue(any(row['status']=='drained' for row in self.records(observer)))
            self.assertEqual(next(observer.directory.glob('*.bin')).read_bytes(), b'final partial output')


if __name__ == '__main__':
    unittest.main()
