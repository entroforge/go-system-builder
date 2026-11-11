#!/usr/bin/env python3
"""Opt-in disposable S0-S11 native Claude flow test; all professional answers simulated.

Real Main sessions use installed production assets/Hooks and native Agents.
This runner supplies only installation, clearly labeled scenario fixtures and
read-only observation. It never writes Runtime, receipts, stage cursors or Worker
answers, and never weakens a domain gate. No real product acceptance is claimed.
"""
import argparse
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
import time

REPO = Path(__file__).resolve().parents[1]

OBSERVE = r'''import json,os,subprocess,sys,time,uuid
from pathlib import Path
artifacts,main,event,i,j=sys.argv[1:6]
raw=sys.stdin.buffer.read()
payload=json.loads(raw)
expected=json.loads((Path(artifacts)/'original-settings.json').read_text())['hooks'][event][int(i)]['hooks'][int(j)]['command']
original=sys.argv[6] if len(sys.argv)>6 else expected
if original!=expected: raise SystemExit('Observer command differs from the frozen original Hook')
def facts():
 try:
  s=json.loads((Path(main)/'.claude/loop-state.json').read_text())
  review=s.get('review') or {}; repair=review.get('repair') or {}
  return {'revision':s.get('revision'),'lifecycle':s.get('lifecycle'),'repair_status':repair.get('status')}
 except (OSError,ValueError,AttributeError,TypeError) as e: return {'observation_error':str(e)}
# Publish the actual input before running a possibly hanging original Hook.
try:
 target=Path(artifacts)/('transcript-input-'+uuid.uuid4().hex+'.json')
 target.write_text(json.dumps({'observed_ns':time.time_ns(),'event':event,'payload':payload},ensure_ascii=False))
except OSError: pass
before=facts();start=time.time_ns();clock_start=time.monotonic()
p=subprocess.run(['bash','--noprofile','--norc','-c',original],input=raw,capture_output=True)
row={'event':event,'started_ns':start,'finished_ns':time.time_ns(),'elapsed_seconds':time.monotonic()-clock_start,'project_dir':os.environ.get('CLAUDE_PROJECT_DIR'),'process_cwd':os.getcwd(),'before':before,'after':facts(),'exit_code':p.returncode,'stdout':p.stdout.decode(errors='replace'),'stderr':p.stderr.decode(errors='replace'),'payload':payload}
try: (Path(artifacts)/('hook-'+str(start)+'-'+uuid.uuid4().hex+'.json')).write_text(json.dumps(row,ensure_ascii=False))
except OSError: pass  # Observation storage cannot change the original Hook result.
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr);sys.exit(p.returncode)
'''

BOUNDARY = '''# User-authorized simulation scope

This disposable project exercises S0–S11 with clearly labeled SIMULATION inputs,
professional conclusions and test-only human choices. They do not represent
real professional or product acceptance. Use the current release installation,
original forms, original Hooks and native Agents. Each Worker owns their answer;
Main performs Main's actual responsibilities and relays this scope on dispatch.

Normal tools remain available. Necessary placeholder documents, small synthetic
fixtures, checks and local Git commits are allowed when the actual workflow needs
them. Do not implement or repair a real business product. A fixture check proves
only the fixture behavior actually checked; report its command and real outcome
in the existing professional answer when relevant, without inventing execution.
No extra work log, submission signal or sidecar report is required from authors.

Never fabricate Runtime, journal, receipt, packet acceptance or a platform event,
or bypass an original consumer. Do not publish, deploy or push. Simulated S11
choices authorize this isolated exercise only. Preserve real errors and original
feedback; continue through applicable original recovery with the same facts.
Do not turn the scenario's expected result into a false PASS or discard a blocker.
The operator's independent sidecar records original Hook input/output, model
streams, actor transcripts and timing. It does not decide professional truth.
'''

PROMPT = '''在当前隔离项目沿当前发布安装、原生 Agent 与原 Hook 继续 S0–S11，到模拟 release_authorized 或如实交接具体阻断。先读当前安装指引、simulation/SCENARIO.md 和 simulation/DECISIONS.md。所有给定专业结论、人类选择及首轮 Finding 均明确标记 SIMULATION，是合成场景输入，不代表实际业务观察、真人发布批准或产品验收。

Main 完成本人原责任；实际原生 Worker 自己填写本人原答卷，Main 不代填。两路各自独立的未接收模拟材料位于 `.claude/test-inputs/candidates/`，可按当前阶段实际需要参考或使用；材料不是已接收事实，真实问题如实处理。本次只需完成原流程动作和必要占位文档，不编写或开发业务产品或测试程序。允许正常全部工具、必要占位文档、合成夹具及本地 Git 提交，按当前原流程实际需要使用。不真实开发业务产品，不 push、不部署、不真实发布，不手改或伪造 Runtime、journal、receipt、接收事实或平台事件，不绕过原消费者。每次原生派发或恢复时把上述范围准确交给 Worker。

遇到阻断、错误、明显等待、指引不清或需要猜测时，在原有对话中及时说明实际阶段与责任、看到的信息或路径、做过的每项实际尝试及其反馈、走错和纠正、尚未解决的疑问与下一步；不确定的事实如实标为未知。Main 每次派发或恢复时也把这一反馈要求交给 Worker，并保留各自的实际表述。继续原工作，不为反馈增加日志文件、答卷字段、提交或等待；一切顺利时无需逐步编写日志。准确耗时由独立 sidecar 记录，不猜测未测量的时间。

合成测试输入为：S5 各适用责任模拟通过；S6 本人完成合成交付；Main 首次 S7 派发时把 simulation/observations/first-round.txt 交适用的原 Reviewer；Reviewer 在本人原答卷明示 Finding 来自 SIMULATION 输入，由原消费者接收交 S8，其他适用责任按真实模拟事实判断；S8 模拟调查支持该原因假设，并按给定测试选择沿原决定入口处理合成合同；S9 独立 Targeted 模拟 pass；新的完整 S7 各适用责任模拟 pass。原 Finding 直接交 Main/S8，不额外制造 ObservationBatch 或先造兼容 BUG。沿 Main 原路由和适用原批准进入 S9，按当前 Main 影响判断、独立 Targeted 接到新的完整 S7，再接 S10 与模拟 S11。

上述是场景预期，不覆盖真实消费者和实际失败。simulation/observations/first-round.txt 与 repaired-round.txt 是预设输入，不是已接受证据，也不证明产品已修复。make check 只检查预置标记；S6 由 TASK 授权 Builder 真创建并提交各自 delivery note，Main post-merge 还须实际运行 TASK 的 make check DELIVERY=对应输出路径，检查文件非空、SIMULATION 用途及输入引用。S0–S5 只预检输入和输出未存在，不要求未来输出已交付。实际框架检查和必要本地操作必须真实执行并如实表达，失败或未知不能强填 PASS。原责任被阻塞时保留实际输出、已经接收的事实和原路径，使用现有恢复入口或交接准确下一责任；不重造历史、不要求额外工作日志。系统接线问题由 Main/系统处理，不能要求已履职作者重存原内容。

结束时汇总实际阶段与确切终点、真实阻断、已经尝试和纠正的步骤、路径歧义、需要猜测的地方及额外手续与介入。完整工具反馈和耗时由独立 sidecar 记录；不知道的时间或业务事实不要猜造。'''

# This lane contains a real, explicitly synthetic mismatch. The observation
# file is a pointer to inspect, not a fabricated Finding or accepted result.
_PROMPT_PARTS = PROMPT.split('\n\n')
REPAIRABLE_PROMPT = '\n\n'.join(_PROMPT_PARTS[:3] + [
    '''本路准备器在初始 Git 基线中放入模拟占位文件 simulation/repairable/status.txt。候选需求规定模拟发布前实际内容应为 READY；初始内容刻意为 PENDING，以便独立 S7 根据真实文件和已接收要求观察。S6 仅按原 TASK 创建并提交台账占位文档，其原检查不检查此独立状态文件。S7 原 Reviewer 实际读取并如实判断；S8 根据真实来源调查，原授权决定修复时，S9 Builder 只需将该模拟占位文件中的一行改为 READY，独立 Targeted 读取实际结果；随后按原流程复验。此为可观察的合成夹具，不是真实产品，不要求写业务代码或测试程序。''',
    '''这些是测试输入和期望，不代表已产生 Finding、批准、修复或通过。若真实文件、原责任、专业判断或消费者反馈与预期不同，保留事实并报告；不得为了经过 S9 伪造结论或绕过原入口。S0–S5 只预检输入与待创建交付，不要求 S6 输出提前存在。必要的本地提交与检查可以真实执行，不要求额外日志、人工重交或关闭正常工具。''',
    _PROMPT_PARTS[5],
])

SCENARIOS = {
    'ledger-repairable': {
        'title': '可修复模拟来源状态', 'candidate_dir': 'tests/fixtures/native-stage-simulation/ledger-repairable',
        'marker_path': 'simulation/products/artifact.txt', 'marker': 'SIMULATION_ONLY',
        'delivery_path': 'simulation/deliveries/ledger-note.txt',
        'repairable_path': 'simulation/repairable/status.txt',
        'prompt': REPAIRABLE_PROMPT,
        'description': '''纯文档与占位文件模拟。准备器在初始基线提供 simulation/repairable/status.txt，其实际初始值为 PENDING；模拟 REQ 在发布前期望 READY。S6 只创建并提交 ledger-note，既有检查只核对标记和台账，不检查独立状态。S7 独立 Reviewer 比较实际状态与已接收要求；S8 调查真实观察，原合同授权修复后 S9 只改该占位行，Targeted 重新读取。此场景不代表真实业务缺陷、产品开发或真实发布。''',
        'finding': 'SIMULATION ONLY: inspect simulation/repairable/status.txt against the accepted READY requirement; this pointer is not an observation or Finding.',
        'after': 'SIMULATION ONLY: reread simulation/repairable/status.txt after any actual repair; this pointer is not an executed verification.'},
    'ledger': {
        'title': '模拟交付来源台账', 'candidate_dir': 'tests/fixtures/native-stage-simulation/ledger-calibrated',
        'marker_path': 'simulation/products/artifact.txt', 'marker': 'SIMULATION_ONLY',
        'delivery_path': 'simulation/deliveries/ledger-note.txt',
        'description': '''本路是纯文档和单 NFR 的来源台账。模拟用户要求受管交付物带有 SIMULATION_ONLY 标记，并可追溯交付来源；无 UI、HTTP、数据库或部署。预置 simulation/products/artifact.txt 是只读标记输入。S6 Builder 必须在原 worktree 真创建此前不存在的 simulation/deliveries/ledger-note.txt，写明 SIMULATION 用途与输入路径，真实检查并提交。Main 实际合并后运行 make check 与 make check DELIVERY=simulation/deliveries/ledger-note.txt。提供的候选文档只是未接收的测试输入，可在对应阶段作为 Main 的占位起点，原问卷和消费者仍负责实际登记。''',
        'finding': 'SIMULATION ONLY: first-round QA finding models a missing provenance link for the synthetic delivery marker. It is test data, not a real product defect or executed observation.',
        'after': 'SIMULATION ONLY: the proposed repaired-round conclusion is that the provenance link is present; this is a test input, not an executed observation.'},
    'offline-cli': {
        'title': '模拟离线归档清单', 'candidate_dir': 'tests/fixtures/native-stage-simulation/offline-calibrated',
        'marker_path': 'simulation/products/offline-sample.txt', 'marker': 'SIMULATION_OFFLINE',
        'delivery_path': 'simulation/deliveries/offline-note.txt',
        'description': '''本路是纯文档的离线归档清单模拟：登记离线样本来源并只读核对预置 simulation/products/offline-sample.txt 的 SIMULATION_OFFLINE 标记；样本缺失或不匹配时如实记为不合规。无实际归档程序、网络、浏览器、数据库或部署。预置候选是尚未接收的合成文档，只供对应原阶段参考；原作者仍按真实责任自然交卷。S6 Builder 必须在原 worktree 真创建此前不存在的 simulation/deliveries/offline-note.txt，写明 SIMULATION 用途与输入路径，真实检查并提交。Main 实际合并后运行 make check 与 make check DELIVERY=simulation/deliveries/offline-note.txt；只证明模拟文件及引用存在，不证明任何归档或产品行为。''',
        'finding': 'SIMULATION ONLY: a QA observation models an omitted link between the offline sample and its inventory entry. This is synthetic input, not an executed observation or a real defect.',
        'after': 'SIMULATION ONLY: the proposed repaired-round conclusion is that the offline sample can be traced to its inventory entry; this is synthetic input, not an executed observation.'}
}


def delivery_check_command(scenario):
    return 'make check DELIVERY=' + SCENARIOS[scenario]['delivery_path']


def write_scenario_marker(root, scenario):
    """Preset read-only input; the S6 Builder, never preparation, creates delivery."""
    spec = SCENARIOS[scenario]
    marker_path, marker = spec['marker_path'], spec['marker']
    if (root/spec['delivery_path']).exists():
        raise SystemExit('S6 delivery must not exist in the initial baseline')
    (root/'simulation/products').mkdir(parents=True, exist_ok=True)
    (root/marker_path).write_text(marker + '\n')
    if spec.get('repairable_path'):
        target = root/spec['repairable_path']
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text('PENDING\n')
    script = ("from pathlib import Path; assert Path(" + repr(marker_path) +
              ").read_bytes() == " + repr((marker + '\n').encode()))
    # DELIVERY is set only by the post-creation TASK command. A plain baseline
    # make check never requires an output which the Builder has not created yet.
    delivery_script = ("from pathlib import Path; p=Path(" + repr(spec['delivery_path']) +
        "); b=p.read_bytes(); assert b.strip(); assert b'SIMULATION' in b; assert " +
        repr(marker_path.encode()) + " in b")
    (root/'Makefile').write_text('.PHONY: check\ncheck:\n\tpython3 -c "' + script +
        '"\nifneq ($(DELIVERY),)\n\t@test "$(DELIVERY)" = "' + spec['delivery_path'] +
        '"\n\tpython3 -c "' + delivery_script + '"\nendif\n')


def validate_candidate_inputs(scenario, candidates):
    """Validate only proposed starting inputs, without inventing accepted facts."""
    spec = SCENARIOS[scenario]
    if (candidates/spec['delivery_path']).exists():
        raise SystemExit('Candidate baseline already contains the future S6 delivery')
    task = (candidates/'docs/dev/tasks/TASK-001-01.md').read_text()
    for required in (spec['marker_path'], spec['marker'], spec['delivery_path'],
                     'assert make check == pass',
                     'assert ' + delivery_check_command(scenario) + ' == pass'):
        if required not in task:
            raise SystemExit('Candidate TASK is missing scenario input/output or an original check: ' + required)
    scope = '| prospective write paths | `' + spec['delivery_path'] + '` |'
    if scope not in task:
        raise SystemExit('Candidate TASK must authorize exactly the new S6 delivery path')



def run(cmd, **kwargs):
    return subprocess.run([str(x) for x in cmd], check=True, **kwargs)


def dump(path, obj):
    path.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + '\n')


def file_inventory(root, paths):
    return {str(path): True for path in sorted(paths) if (root/path).is_file()}


def tracked_inventory(root):
    paths = subprocess.check_output(['git', 'ls-files', '-z'], cwd=root).decode().split('\0')
    return file_inventory(root, [Path(path) for path in paths if path])


def prepare(base, scenario, release_archive, env_file, claude_bin, dry_run=False):
    # A new directory is required: never clean or reuse a live simulation.
    if base.exists():
        raise SystemExit('Preparation requires a new artifacts directory; existing runs are preserved')
    candidate_dir = SCENARIOS[scenario]['candidate_dir']
    candidates = REPO/candidate_dir if candidate_dir else None
    if candidates and not candidates.is_dir():
        raise SystemExit('The configured scenario candidate directory is missing')
    paths = sorted(candidates.rglob('*')) if candidates and candidates.is_dir() else []
    if any(path.is_symlink() or path.is_file() and path.suffix != '.md' for path in paths):
        raise SystemExit('Candidate inputs must be regular Markdown documents without symlinks')
    validate_candidate_inputs(scenario, candidates)
    candidate_files = file_inventory(candidates, [path.relative_to(candidates) for path in paths if path.is_file()]) if candidates else {}
    if dry_run:
        print(json.dumps({'dry_run': True, 'artifacts': str(base), 'scenario': scenario,
                          'candidate_files': candidate_files, 'release_archive': str(release_archive) if release_archive else None,
                          'build_install_model_started': False}, ensure_ascii=False))
        return
    if not release_archive or not release_archive.is_file():
        raise SystemExit('Supply the single prebuilt release archive with --release-archive')
    if not env_file.is_file():
        raise SystemExit('Provider configuration file is missing; never supply its contents in logs')
    if not claude_bin or not claude_bin.is_absolute() or not claude_bin.is_file() or not os.access(claude_bin, os.X_OK):
        raise SystemExit('--claude-bin must be an absolute executable path')
    base.mkdir(parents=True, exist_ok=False)
    started_ns = time.time_ns()
    started = time.monotonic()
    completed = False
    try:
        prepare_project(base, scenario, candidates, candidate_files, release_archive, env_file, claude_bin)
        completed = True
    finally:
        dump(base/'preparation.json', {'started_ns': started_ns, 'finished_ns': time.time_ns(),
             'seconds': round(time.monotonic()-started, 3), 'completed': completed,
             'scope': 'reuse prebuilt release, install, fixture and Git preparation; no model request'})


def prepare_project(base, scenario, candidates, candidate_files, release_archive, env_file, claude_bin):
    shutil.copy2(release_archive, base/'release.tar.gz')
    with (base/'prepare.log').open('w') as log:
        unpack = base/'release'
        unpack.mkdir()
        with tarfile.open(base/'release.tar.gz') as archive:
            archive.extractall(unpack, filter='data')
        packages = sorted(p for p in unpack.glob('vibe-coding-loop-template-*') if p.is_dir())
        if len(packages) != 1:
            raise SystemExit('Release archive must contain exactly one original package directory')
        package = packages[0]
        root = base/'project'
        run([package/'.claude/bin/loop-harness', 'install', '--source', package, '--root', root], stdout=log, stderr=subprocess.STDOUT)
        run(['git', 'init', '-b', 'simulation-dev'], cwd=root, stdout=log, stderr=subprocess.STDOUT)
        run(['git', 'config', 'user.name', 'Simulation Fixture'], cwd=root)
        run(['git', 'config', 'user.email', 'simulation@example.invalid'], cwd=root)
    (root/'.gitignore').write_text('.claude/*\n!.claude/agents/\n!.claude/skills/\n!.claude/settings.json\n!.claude/loop.md\n')
    (root/'CLAUDE.md').write_text(BOUNDARY)
    (root/'simulation').mkdir(exist_ok=True)
    if candidates and candidates.is_dir():
        shutil.copytree(candidates, root/'.claude/test-inputs/candidates')
        if file_inventory(root/'.claude/test-inputs/candidates', [Path(path) for path in candidate_files]) != candidate_files:
            raise SystemExit('Candidate inputs changed during preparation; preserve this failed run')
        inputs = {'kind':'synthetic Main document inputs; no Runtime, receipts or Worker answers', 'files':candidate_files}
        dump(root/'simulation/candidate-inputs.json', inputs)
        dump(base/'candidate-inputs.json', inputs)
    else:
        inputs = {'kind': 'no prewritten Main candidates for this scenario', 'files': {}}
        dump(root/'simulation/candidate-inputs.json', inputs)
        dump(base/'candidate-inputs.json', inputs)
    (root/'simulation/observations').mkdir()
    # The closed scenario catalog provides a prewritten non-product marker.
    # Baseline make check validates only the input; the second TASK command
    # validates delivery after its actual S6 creation and Main merge.
    write_scenario_marker(root, scenario)
    (root/'simulation/observations/first-round.txt').write_text(SCENARIOS[scenario]['finding'] + '\n')
    (root/'simulation/observations/repaired-round.txt').write_text(SCENARIOS[scenario]['after'] + '\n')
    if scenario == 'offline-cli':
        (root/'simulation/cli').mkdir(parents=True)
        (root/'simulation/cli/README.md').write_text('SIMULATION ONLY: offline archive inventory placeholder; no executable implementation.\n')
    (root/'simulation/SCENARIO.md').write_text('# SIMULATION — ' + SCENARIOS[scenario]['title'] + '\n\n' + SCENARIOS[scenario]['description'] + '\n')
    (root/'simulation/DECISIONS.md').write_text('''# Test-only simulated human decisions

Identity: simulation-human. These are authorized synthetic choices for this disposable workflow test, not real human consent or release authorization.

- Lock and start the scenario requirement at v1.0.0; development=simulation-dev; release target=local simulation-release.
- Endorse the scenario's actual minimal design direction and decide applicability from its stated scope.
- At S8 apply simulation-human's synthetic approve to the actual resulting RepairContract through its existing original decision consumer; this text is a choice input, not an accepted approval. If that consumer cannot accept it, preserve and report the real blocker rather than changing Runtime.
- At S11 apply simulation-human's synthetic approve through the existing original release-decision consumer for this fixture only. This text is not an accepted decision. If the consumer cannot accept it, preserve and report that real blocker. Do not publish, deploy, push or perform external release.
''')
    with (base/'prepare.log').open('a') as log:
        run(['make', 'check'], cwd=root, stdout=log, stderr=subprocess.STDOUT)
        if (root/SCENARIOS[scenario]['delivery_path']).exists():
            raise SystemExit('Future S6 output unexpectedly exists before the initial commit')
        run(['git', 'add', '.'], cwd=root, stdout=log, stderr=subprocess.STDOUT)
        run(['git', 'commit', '-qm', 'Install current assets and explicitly synthetic scenario fixtures'], cwd=root, stdout=log, stderr=subprocess.STDOUT)
        run(['git', 'branch', 'simulation-release'], cwd=root, stdout=log, stderr=subprocess.STDOUT)
    settings = json.loads((root/'.claude/settings.json').read_text())
    dump(base/'original-settings.json', settings)
    (base/'observe-hook.py').write_text(OBSERVE)
    for event, registrations in settings['hooks'].items():
        for i, registration in enumerate(registrations):
            for j, hook in enumerate(registration['hooks']):
                hook['command'] = shlex.join(['python3',str(base/'observe-hook.py'),str(base),str(root),event,str(i),str(j),hook['command']])
    dump(root/'.claude/settings.json', settings)
    with (base/'prepare.log').open('a') as log:
        run(['git','add','.claude/settings.json'], cwd=root, stdout=log, stderr=subprocess.STDOUT)
        run(['git','commit','-qm','Observe original Hooks without tracking live Runtime'], cwd=root, stdout=log, stderr=subprocess.STDOUT)
    initial = json.loads((root/'.claude/loop-state.json').read_text())
    dump(base/'initial-runtime.json', initial)
    version_script = 'source "$1" >/dev/null 2>&1 && exec "$2" --version'
    version = subprocess.check_output(['bash', '--noprofile', '--norc', '-c', version_script,
                                       'simulation-version', str(env_file), str(claude_bin)], text=True).strip()
    fixture_paths = [path.relative_to(root) for directory in ('simulation/products', 'simulation/cli', 'simulation/repairable')
                     for path in (root/directory).rglob('*') if path.is_file()]
    fixture_files = {str(path): {'mode': oct((root/path).stat().st_mode & 0o777)} for path in sorted(fixture_paths)}
    dump(base/'metadata.json', {'scope':'simulation-only S0-S11; real native Claude parent and agents',
         'scenario':scenario, 'project':str(root), 'created_at':time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
         'claude_binary':str(claude_bin),
         'claude_version':version,
         'release_archive':str(release_archive), 'source_worktree_status':subprocess.check_output(['git','status','--short'],cwd=REPO,text=True),
         's6_delivery_path':SCENARIOS[scenario]['delivery_path'],
         's6_post_merge_checks':['make check', delivery_check_command(scenario)],
         'fixture_strategy':'synthetic marker and placeholder inputs; necessary local Git commits and normal tools allowed; no real product development or release',
         'fixture_files':fixture_files})
    (base/'prompt.txt').write_text(SCENARIOS[scenario].get('prompt', PROMPT))
    status = subprocess.check_output(['git', 'status', '--porcelain'], cwd=root, text=True)
    if status:
        raise SystemExit('Prepared Git tree is not clean; preserve this failed run')
    dump(base/'prepared-inputs.json', {
        'tracked_files': tracked_inventory(root),
        'candidate_files': candidate_files,
        'note': 'Prepared paths before the first model request. Never clean fixtures during a run.'})
    print(json.dumps({'prepared':str(base),'project':str(root),'initial_lifecycle':initial.get('lifecycle')},ensure_ascii=False), flush=True)


def observe_model_streams(paths, timeline, stopped):
    """Sidecar arrival samples, separate from model/Hook execution and decisions."""
    offsets = {path: 0 for path in paths}
    with timeline.open('w') as output:
        while True:
            for path in paths:
                if not path.exists():
                    continue
                with path.open('rb') as source:
                    source.seek(offsets[path])
                    chunk = source.read()
                    offsets[path] = source.tell()
                if chunk:
                    output.write(json.dumps({'observed_ns': time.time_ns(),
                        'stream': path.name, 'bytes': len(chunk),
                        'text': chunk.decode(errors='replace')}, ensure_ascii=False) + '\n')
                    output.flush()
            if stopped.is_set():
                break
            stopped.wait(0.1)


class TranscriptSidecar:
    """Read only the exact session announced by an actual SessionStart payload."""
    CHUNK_BYTES = 256 * 1024

    def __init__(self, base, run_index, projects_root=None):
        self.base = base
        self.projects_root = (projects_root or Path.home()/'.claude/projects').resolve()
        self.directory = base/f'live-transcripts-{run_index}'
        self.directory.mkdir(exist_ok=False)
        self.index = self.directory/'index.jsonl'
        self.sessions = {}
        self.seen_inputs = set()
        self.files = {}
        self.notices = set()
        self.record(status='unknown', reason='No actual SessionStart transcript_path/session_id observed yet')

    def record(self, **row):
        with self.index.open('a') as output:
            output.write(json.dumps({'observed_ns': time.time_ns(), **row}, ensure_ascii=False)+'\n')

    def notice(self, key, **row):
        if key not in self.notices:
            self.record(**row)
            self.notices.add(key)

    def discover(self):
        for path in self.base.glob('transcript-input-*.json'):
            if path in self.seen_inputs:
                continue
            try:
                announced = json.loads(path.read_text())
            except (OSError, ValueError):
                continue  # A concurrently written input will be retried.
            self.seen_inputs.add(path)
            if announced.get('event') != 'SessionStart':
                continue
            payload = announced.get('payload') or {}
            source, session = payload.get('transcript_path'), payload.get('session_id')
            if not isinstance(source, str) or not isinstance(session, str) or not source or not session:
                self.record(status='unknown', reason='SessionStart lacks transcript_path or session_id', input_file=path.name)
                continue
            candidate = Path(source)
            if not candidate.is_absolute() or candidate.name != session+'.jsonl' or not candidate.resolve().is_relative_to(self.projects_root):
                self.record(status='unknown', reason='Announced transcript is outside the Claude projects root or does not match the exact session', input_file=path.name)
                continue
            self.sessions[(session, str(candidate))] = candidate
            self.record(status='session_announced', session=session, source=str(candidate), input_file=path.name)

    def capture(self, path, session, actor, kind, allowed_directory):
        try:
            resolved = path.resolve(strict=True)
            if path.is_symlink() or not resolved.is_relative_to(self.projects_root) or not resolved.is_relative_to(allowed_directory.resolve()) or not resolved.is_file():
                raise ValueError('Source is not a regular file inside this exact session')
            with resolved.open('rb') as source:
                stat = os.fstat(source.fileno())
                identity = (stat.st_dev, stat.st_ino)
                key = str(path)
                prior = self.files.get(key)
                reset = prior is None or prior['identity'] != identity or stat.st_size < prior['offset']
                if prior and not reset and prior['offset']:
                    source.seek(prior['offset']-len(prior['anchor']))
                    reset = source.read(len(prior['anchor'])) != prior['anchor']
                if reset:
                    segment = prior['segment']+1 if prior else 0
                    target = self.directory/f'source-{len(self.files) if prior is None else prior["number"]}-segment-{segment}.bin'
                    prior = {'identity':identity, 'offset':0, 'segment':segment, 'anchor':b'',
                             'target':target, 'number':len(self.files) if prior is None else prior['number']}
                    self.files[key] = prior
                    self.record(status='segment_started', session=session, actor=actor, kind=kind,
                        source=str(path), segment=segment, artifact=str(target.relative_to(self.base)),
                        reason='first_observation' if segment == 0 else 'rotation_or_truncation')
                start = prior['offset']
                source.seek(start)
                chunk = source.read(self.CHUNK_BYTES)
                if not chunk:
                    return 0
                with prior['target'].open('ab') as output:
                    output.write(chunk)
                prior['offset'] += len(chunk)
                source.seek(max(0, prior['offset']-64))
                prior['anchor'] = source.read(min(64, prior['offset']))
                self.record(status='bytes', session=session, actor=actor, kind=kind, source=str(path),
                    segment=prior['segment'], offset_start=start, offset_end=prior['offset'],
                    bytes=len(chunk), artifact=str(prior['target'].relative_to(self.base)))
                return len(chunk)
        except (OSError, ValueError) as exc:
            self.notice((str(path),str(exc)), status='unknown', session=session, actor=actor,
                        source=str(path), reason=str(exc))
            return 0

    def poll(self):
        self.discover()
        count = 0
        for (session, _), main in list(self.sessions.items()):
            count += self.capture(main, session, 'Main', 'transcript', main.parent)
            subagents = main.parent/session/'subagents'
            if not subagents.is_dir():
                self.notice((session,'subagents_missing'), status='unknown', session=session,
                            reason='Exact session subagents directory is not present', source=str(subagents))
                continue
            for source in sorted(subagents.glob('agent-*')):
                if source.suffix not in ('.jsonl','.json'):
                    continue
                actor = source.name.removeprefix('agent-').split('.')[0]
                count += self.capture(source, session, actor, 'transcript' if source.suffix == '.jsonl' else 'metadata', subagents)
        return count

    def run(self, stopped):
        try:
            while not stopped.is_set():
                self.poll()
                stopped.wait(0.2)
            deadline = time.monotonic()+1.0
            while True:
                remaining = self.poll()
                if not remaining:
                    self.record(status='drained', scope='bytes visible at final observation; future writes are unknown')
                    break
                if time.monotonic() >= deadline:
                    self.record(status='unknown', reason='Final drain budget reached; preserved existing segments')
                    break
        except Exception as exc:
            # Observer failures never become model/Hook failures.
            try:
                self.record(status='unknown', reason='Sidecar failed: '+str(exc))
            except OSError:
                pass



def launch(base, env_file, resume, prompt_file, driver=False, timeout_seconds=None):
    root = base/'project'
    if not env_file.is_file():
        raise SystemExit('Provider configuration file is missing; never supply its contents in logs')
    meta = json.loads((base/'metadata.json').read_text())
    claude_bin = Path(meta['claude_binary'])
    if not claude_bin.is_file():
        raise SystemExit('The configured Claude executable is unavailable')
    index = len(list(base.glob('claude-run-*.jsonl'))) + 1
    prompt = prompt_file.read_text() if prompt_file else (base/'prompt.txt').read_text()
    dump(base/f'request-{index}.json', {'prompt_source': str(prompt_file) if prompt_file else 'prepared prompt.txt',
         'resume': resume, 'test_driver': driver, 'timeout_seconds': timeout_seconds})
    args = [str(claude_bin),'-p','--permission-mode','bypassPermissions','--output-format','stream-json','--verbose','--include-hook-events','--setting-sources','project']
    if driver:
        system = BOUNDARY
        (base/f'test-driver-system-{index}.txt').write_text(system)
        args += ['--append-system-prompt',system]
    if resume:
        args += ['--resume',resume]
    script = 'source "$1" >/dev/null 2>&1 && shift && exec "$@"'
    started_ns=time.time_ns()
    started=time.monotonic()
    stopped = threading.Event()
    stream_paths = [base/f'claude-run-{index}.jsonl', base/f'claude-run-{index}.stderr.log']
    observer = threading.Thread(target=observe_model_streams,
        args=(stream_paths, base/f'model-stream-timing-{index}.jsonl', stopped), daemon=True)
    transcript_sidecar = TranscriptSidecar(base, index)
    transcript_observer = threading.Thread(target=transcript_sidecar.run, args=(stopped,), daemon=True)
    with stream_paths[0].open('w') as out, stream_paths[1].open('w') as err:
        observer.start()
        transcript_observer.start()
        try:
            result=subprocess.run(['bash','--noprofile','--norc','-c',script,'native-stage-simulation',str(env_file),*args],cwd=root,input=prompt,text=True,stdout=out,stderr=err,timeout=timeout_seconds)
            code=result.returncode
        except subprocess.TimeoutExpired:
            code=124
        except KeyboardInterrupt:
            code=130
        finally:
            stopped.set()
            observer.join(timeout=2)
            transcript_observer.join(timeout=2)
            if transcript_observer.is_alive():
                dump(base/f'live-transcripts-{index}-drain.json',
                     {'status':'unknown','reason':'Sidecar still draining; model shutdown was not blocked'})
    dump(base/f'run-{index}.json',{'exit_code':code,'seconds':round(time.monotonic()-started,3),'started_ns':started_ns,'finished_ns':time.time_ns(),'external_interference':'driver interrupted by SIGINT' if code == 130 else 'driver timeout' if code == 124 else 'signal exit observed' if code < 0 else 'not inferred; inspect external intervention records','timeout_seconds':timeout_seconds,'timing_scope':'model process wall time includes tools, Hooks and rework; not pure inference','resume':resume,'effort':'provider/default','test_driver':driver,'simulation_only':True})
    summarize(base)
    raise SystemExit(code)


def summarize(base):
    rows=[];invalid_observations=[]
    for path in base.glob('hook-*.json'):
        try:
            row = json.loads(path.read_text())
            if not isinstance(row,dict) or not all(key in row for key in ('event','started_ns','finished_ns','before','after','payload','exit_code','stdout','stderr')) or not all(isinstance(row[key],dict) for key in ('before','after','payload')):
                raise ValueError('missing or malformed observation fields')
            if not isinstance(row['started_ns'],int) or not isinstance(row['finished_ns'],int) or row['finished_ns'] < row['started_ns'] or not isinstance(row['stdout'],str) or not isinstance(row['stderr'],str):
                raise ValueError('invalid observation timing or output fields')
            row['observation_file'] = path.name
            rows.append(row)
            for label in ('before','after'):
                if row[label].get('observation_error'):
                    invalid_observations.append({'file':path.name,'boundary':label,'error':row[label]['observation_error']})
        except (OSError,ValueError) as exc:
            invalid_observations.append({'file':path.name,'error':str(exc)})
    rows.sort(key=lambda x:x['started_ns'])
    cursors=[];actors={};ordinary=[]
    def cursor(facts):
        life=facts.get('lifecycle') or {}
        return (life.get('state'),life.get('phase'))
    for row in rows:
        for label in ('before','after'):
            c=cursor(row[label])
            if not cursors or cursors[-1]!=list(c): cursors.append(list(c))
        actor=row['payload'].get('agent_id')
        if actor:
            info=actors.setdefault(actor,{'starts':0,'stops':0,'events':0,'stage_at_start':None,'repair_status_at_start':None,'start_observations':[],'stop_diagnostics':[]})
            info['events']+=1
            if row['event']=='SubagentStart':
                info['starts']+=1;info['stage_at_start']=cursor(row['before']);info['repair_status_at_start']=row['before'].get('repair_status');info['start_observations'].append({'at_ns':row['started_ns'],'stage':cursor(row['before']),'repair_status':row['before'].get('repair_status')})
            elif row['event']=='SubagentStop': info['stops']+=1;info['stop_diagnostics'].append({'exit':row['exit_code'],'message':row['stderr'][-2200:]})
            else: ordinary.append(row)
    revision_windows=[]
    for row in ordinary:
        if row['before'].get('revision') == row['after'].get('revision'): continue
        overlaps=[{'file':other['observation_file'], 'event':other['event'], 'actor':other['payload'].get('agent_id'), 'before':other['before'].get('revision'), 'after':other['after'].get('revision')} for other in rows if other is not row and (other['event'] in ('SubagentStart','SubagentStop','SessionStart','PostToolUse') or other['event']=='PreToolUse' and not other['payload'].get('agent_id')) and other['started_ns'] <= row['finished_ns'] and other['finished_ns'] >= row['started_ns'] and other['before'].get('revision') != other['after'].get('revision')]
        revision_windows.append({'file':row['observation_file'], 'event':row['event'], 'actor':row['payload'].get('agent_id'), 'before':row['before'].get('revision'), 'after':row['after'].get('revision'), 'overlapping_events':overlaps})
    dump(base/'concurrent-revision-windows.json', {'windows':revision_windows, 'note':'Before/after samples span concurrent original Hook processes; a revision delta is not attribution. Inspect the exact original journal transaction before alleging an ordinary Worker write.'})
    streams=[];invalid_stream_lines=[]
    for path in sorted(base.glob('claude-run-*.jsonl'), key=lambda p:int(p.stem.rsplit('-',1)[1])):
        for number,line in enumerate(path.read_text().splitlines(),1):
            try:
                row=json.loads(line)
                if not isinstance(row,dict): raise ValueError('stream entry is not an object')
                streams.append(row)
            except ValueError: invalid_stream_lines.append({'file':path.name,'line':number})
    native_calls=[];native_messages=[];results=[];sessions=[];latest_activity=[]
    hook_started={};hook_responses={};hook_failures=[]
    for row in streams:
        if row.get('type') == 'system' and row.get('subtype') == 'hook_started': hook_started[row.get('hook_id')]=row
        if row.get('type') == 'system' and row.get('subtype') == 'hook_response':
            hook_responses[row.get('hook_id')]=row
            if row.get('exit_code') not in (None, 0) or row.get('outcome') != 'success':
                hook_failures.append({key:row.get(key) for key in ('hook_id','hook_name','hook_event','exit_code','outcome','stderr')})
        if row.get('session_id') and row['session_id'] not in sessions: sessions.append(row['session_id'])
        if row.get('type')=='result': results.append({k:row.get(k) for k in ('session_id','is_error','subtype','result','num_turns','duration_ms','duration_api_ms')})
        if row.get('type')=='assistant':
            for part in row.get('message',{}).get('content',[]):
                if part.get('type')=='text': latest_activity.append({'text':part['text'][-350:]})
                elif part.get('type')=='tool_use': latest_activity.append({'tool':part.get('name'),'input_preview':str(part.get('input'))[:180]})
                if isinstance(part,dict) and part.get('type')=='tool_use' and part.get('name') in ('Agent','Task'):
                    native_calls.append({'input':part.get('input'), 'tool_use_id':part.get('id')})
                if isinstance(part,dict) and part.get('type')=='tool_use' and part.get('name')=='SendMessage':
                    native_messages.append({'input':part.get('input'),'tool_use_id':part.get('id')})
    writes=[];commands=[];native_returns=[];transcripts=[]
    for row in rows:
        actor=row['payload'].get('agent_id')
        payload=row['payload']
        if row['event']=='PostToolUse' and payload.get('tool_name') in ('Agent','Task'):
            response=payload.get('tool_response') or {}
            if isinstance(response,dict):
                native_returns.append({'observation_file':row['observation_file'],'tool_use_id':payload.get('tool_use_id'),
                                       'status':response.get('status'),'agent_id':response.get('agentId'),
                                       'worktree_path':response.get('worktreePath')})
        if row['event']=='SubagentStop' and actor:
            transcript=payload.get('agent_transcript_path')
            item={'agent_id':actor,'source':transcript,'copy':None}
            if isinstance(transcript,str) and transcript:
                source=Path(transcript)
                try:
                    projects=(Path.home()/'.claude/projects').resolve()
                    resolved=source.resolve(strict=True)
                    if not resolved.is_relative_to(projects) or not resolved.is_file():
                        raise ValueError('transcript is outside the Claude projects directory or is not a file')
                    data=resolved.read_bytes()
                    dest=base/'actor-transcripts'/f'observation-{len(transcripts):06d}.jsonl'; dest.parent.mkdir(exist_ok=True)
                    if not dest.exists(): dest.write_bytes(data)
                    item.update(copy=str(dest.relative_to(base)))
                except (OSError,ValueError) as exc: item['error']=str(exc)
            else: item['error']='platform did not provide agent_transcript_path'
            transcripts.append(item)
        if not actor or row['event']!='PreToolUse': continue
        tool=payload.get('tool_name'); inp=payload.get('tool_input') or {}
        if tool in ('Write','Edit','MultiEdit'):
            path=inp.get('file_path') or inp.get('path') or ''
            parts=Path(path).parts
            private=any(parts[i:i+2] in (('.loop-submissions','s7'),('.loop-submissions','native-s9'),('.loop-submissions','s9'),('.claude','submissions')) or parts[i:i+3]==('.claude','assignments','native') for i in range(len(parts)))
            item={'actor':actor,'tool':tool,'path':path,'private_answer_path':private}
            writes.append(item)
            # Retain non-private writes for review; a path alone does not prove product work.
        if tool=='Bash':
            cmd=inp.get('command') or ''
            item={'actor':actor,'command':cmd}
            commands.append(item)
    dump(base/'worker-tool-audit.json',{'writes':writes,'bash_commands':commands,'potential_boundary_violations':[],'note':'Read-only observations, not tool restrictions. Commands and non-private writes require review of actual work content; raw original Hook payloads are authoritative.'})
    root=base/'project'
    try:
        state=json.loads((root/'.claude/loop-state.json').read_text())
        if not isinstance(state,dict): raise ValueError('Runtime is not an object')
    except (OSError,ValueError) as exc:
        state={}; invalid_observations.append({'file':'.claude/loop-state.json','error':str(exc)})
    journal_rows=[]
    journal_path=root/'.claude/loop-events.jsonl'
    if journal_path.exists():
        for line in journal_path.read_text().splitlines():
            try: entry=json.loads(line)
            except ValueError: continue
            if entry.get('event') == 'transition_committed':
                journal_rows.append({key:entry.get(key) for key in ('sequence','event_id','transition_id','before_revision','after_revision','from','to','request_id','evidence_ids')})
    # Source bytes and historical digest fields are not reauthenticated by the sidecar.
    evidence_drift=None
    evidence_checked=None
    evidence_unchecked=None
    dump(base/'registered-evidence-source-audit.json', {'status':'not_checked',
         'note':'Original evidence paths remain available; the sidecar does not calculate or verify digests.'})
    fixture_expected=json.loads((base/'metadata.json').read_text())['fixture_files']
    fixture_actual={}
    for directory in ('simulation/products','simulation/cli'):
        for path in (root/directory).rglob('*'):
            if path.is_file() and not path.is_symlink():
                fixture_actual[str(path.relative_to(root))]={'mode':oct(path.stat().st_mode & 0o777)}
    fixture_changed=None  # The sidecar does not claim to detect arbitrary content changes.
    fixture_mode_changed=set(fixture_actual)!=set(fixture_expected) or any(fixture_actual.get(p,{}).get('mode')!=v['mode'] for p,v in fixture_expected.items())
    s1_bindings=[x['observation_file'] for x in rows if x['event']=='PostToolUse' and cursor(x['before'])[0]=='inactive' and cursor(x['after'])==('planning','design') and 'S1 binding recorded through original TR-001' in x.get('stdout','')]
    data={'simulation_only':True,'original_s1_binding_observations':s1_bindings,'platform_hooks':{'started':len(hook_started),'responses':len(hook_responses),'unreturned_started_ids':sorted(set(hook_started)-set(hook_responses)),'non_success':hook_failures},'invalid_observations':invalid_observations,'invalid_stream_lines':invalid_stream_lines,'journal_transitions':journal_rows,'latest_activity':latest_activity[-3:],'native_actor_count':len(actors),'cursors_observed':cursors,'native_calls':native_calls,'native_returns':native_returns,'native_messages':native_messages,'actor_transcripts':transcripts,'actors':actors,'ordinary_worker_events':len(ordinary),'ordinary_worker_nonzero_exit':sum(x['exit_code']!=0 for x in ordinary),'ordinary_worker_nonempty_stdout':sum(bool(x['stdout']) for x in ordinary),'ordinary_worker_revision_changes':len(revision_windows),'ordinary_revision_windows_with_overlapping_events':sum(bool(x['overlapping_events']) for x in revision_windows),'sessions':sessions,'results':results,'final_revision':state.get('revision'),'final_lifecycle':state.get('lifecycle'),'fixture_changed':fixture_changed,'fixture_mode_changed':fixture_mode_changed,'fixture_actual':fixture_actual,'worker_private_answer_writes':sum(x['private_answer_path'] for x in writes),'worker_other_writes':sum(not x['private_answer_path'] for x in writes),'worker_bash_commands':len(commands),'potential_boundary_violations':[],'registered_evidence_source_drift':evidence_drift}
    intervals = sorted((row['started_ns'], row['finished_ns']) for row in rows)
    merged = []
    for start, end in intervals:
        if merged and start <= merged[-1][1]:
            merged[-1][1] = max(merged[-1][1], end)
        else:
            merged.append([start, end])
    data['measurement'] = {
        'preparation': json.loads((base/'preparation.json').read_text()) if (base/'preparation.json').exists() else None,
        'model_process_runs': [json.loads(path.read_text()) for path in sorted(base.glob('run-*.json'))],
        'hook_interval_sum_seconds': round(sum(end-start for start, end in intervals)/1e9, 6),
        'hook_interval_union_seconds': round(sum(end-start for start, end in merged)/1e9, 6),
        'rework': 'Use journal transitions and original responsibility records; no duration inferred from lifecycle alone',
        'external_interference': 'Not automatically attributable; retain SIGINT, preparation failures and external edits separately',
        'boundary': 'Hook intervals overlap and are nested in model process wall time. Do not add these categories, subtract to infer model time, or claim single-variable gains across different scenarios. Provider API duration, when reported, is separate from inference-only time.'}
    data['registered_evidence_sources_checked'] = evidence_checked
    data['registered_evidence_sources_unchecked'] = evidence_unchecked
    data['live_transcript_sidecars'] = [
        {'directory':str(path.parent.relative_to(base)), 'index':str(path.relative_to(base)),
         'scope':'Exact sessions from original SessionStart payloads; raw byte segments and offsets; missing paths remain unknown'}
        for path in sorted(base.glob('live-transcripts-*/index.jsonl'))]
    dump(base/'summary.json',data)
    for name in ('loop-state.json','loop-events.jsonl','hook-decisions.jsonl'):
        p=root/'.claude'/name
        if p.exists(): (base/('final-'+name)).write_bytes(p.read_bytes())
    print(json.dumps({k:data[k] for k in ('cursors_observed','ordinary_worker_events','native_actor_count','final_lifecycle','fixture_changed','latest_activity')},ensure_ascii=False),flush=True)


def verify(base):
    summarize(base)
    s=json.loads((base/'summary.json').read_text())
    root=base/'project'
    journal_path=root/'.claude/loop-events.jsonl'
    journal=journal_path.read_text() if journal_path.is_file() else ''
    cursors=[tuple(x) for x in s['cursors_observed']]
    def seen(state, phase=None):
        return any(a==state and (phase is None or b==phase) for a,b in cursors)
    stages={
        'S0':seen('inactive') and (root/'docs/requirements/REQ-001.md').exists(),
        'S1':bool(s.get('original_s1_binding_observations')) or 'TR-001' in journal or 'TR001' in journal,
        'S2':seen('planning','design'), 'S3':seen('planning','contracts'),
        'S4':seen('planning','tasks'), 'S5':seen('document_verification'),
        'S6':seen('building'), 'S7':seen('verification'),
        'S8':seen('bug_resolution','investigation'),
        'S9':any(a=='bug_resolution' and b not in (None,'investigation') for a,b in cursors),
        'S10':seen('acceptance') and seen('release_audit'),
        'S11':seen('awaiting_human_release') and seen('release_authorized')}
    runs=s['measurement']['model_process_runs']
    final_result=s['results'][-1] if s['results'] else None
    checks={'terminal_state_observed':(s['final_lifecycle'] or {}).get('state')=='release_authorized',
            'stage_cursors_observed':all(stages.values()),
            'native_actor_events_observed':bool(s['actors']),
            'all_observed_actors_have_stop':all(x['stops']>0 for x in s['actors'].values()) if s['actors'] else None,
            'latest_process_exit_zero':runs[-1].get('exit_code')==0 if runs else None,
            'latest_model_result_success':not final_result.get('is_error') and final_result.get('subtype')=='success' if final_result else None,
                        'registered_evidence_sources_intact':None,  # Diagnostic only; no new acceptance gate.
            'journal_present':journal_path.is_file(),
            'observations_readable':not s['invalid_observations'] and not s['invalid_stream_lines']}
    failures=[k for k,v in checks.items() if v is False]
    result={'simulation_only':True,'automated_observation_status':'failed' if failures else 'requires_independent_audit',
            'full_flow_proven':None,'stages':stages,'observation_checks':checks,'failed_observations':failures,
            'independent_audit_required':['Associate the actual first-round Finding and accepted source with its S8 Case and Contract.',
                'Associate the original S9 answers, Main impact judgment, independent Targeted, new full S7 and S10/S11 transitions; legacy records only when actually used.',
                'Associate each actual native call/start/author input and saved original/Stop/completed return with the same platform actor and engineering responsibility; async completion follows actual platform facts.',
                'Review raw tool/Hook events, source identities, any recovery or cleanup_pending, process interruptions and product boundary.'],
            'worker_activity_needing_review':{'command_signals':s['potential_boundary_violations'],'non_private_write_count':s['worker_other_writes'],'audit_file':'worker-tool-audit.json'},
            'boundary':'Cursor, actor counts and fixture paths are observations, not a receipt-chain proof. Nonempty Hook output is normal feedback. Commands and paths do not alone establish product work. This is not product or professional acceptance.'}
    dump(base/'verification.json',result)
    print(json.dumps(result,ensure_ascii=False),flush=True)
    raise SystemExit(1 if failures else 2)  # 2: observations need independent audit, not a full PASS.


def positive_seconds(value):
    try:
        seconds = int(value)
    except ValueError:
        raise argparse.ArgumentTypeError('timeout must be a positive integer number of seconds')
    if seconds <= 0:
        raise argparse.ArgumentTypeError('timeout must be a positive integer number of seconds')
    return seconds


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('action',choices=['prepare','run','summarize','verify'])
    p.add_argument('--artifacts',type=Path,required=True)
    p.add_argument('--env-file',type=Path,default=Path.home()/'.claude/minimax')
    p.add_argument('--scenario',choices=sorted(SCENARIOS),default='ledger',help='Independent synthetic business scenario for prepare')
    p.add_argument('--release-archive',type=Path,help='One prebuilt original release archive shared by all lanes')
    p.add_argument('--claude-bin',type=Path,help='Absolute installed Claude executable used for version and model calls')
    p.add_argument('--dry-run', action='store_true', help='Validate preparation inputs without building, installing or starting a model')
    p.add_argument('--resume')
    p.add_argument('--driver',action='store_true',help='Small native Main simulation prompt; original native tools and Agents remain available')
    p.add_argument('--prompt-file',type=Path)
    p.add_argument('--timeout-seconds',type=positive_seconds,default=None,
                   help='Optional explicit model-process deadline; omitted means no driver deadline')
    args=p.parse_args()
    base=args.artifacts.resolve()
    if args.dry_run and args.action != 'prepare': p.error('--dry-run applies only to prepare')
    if args.timeout_seconds is not None and args.action != 'run': p.error('--timeout-seconds applies only to run')
    if args.action=='prepare': prepare(base, args.scenario, args.release_archive.resolve() if args.release_archive else None,
                                       args.env_file.expanduser(), args.claude_bin.expanduser() if args.claude_bin else None, args.dry_run)
    elif args.action=='run': launch(base,args.env_file.expanduser(),args.resume,args.prompt_file,args.driver,args.timeout_seconds)
    elif args.action=='summarize': summarize(base)
    else: verify(base)
