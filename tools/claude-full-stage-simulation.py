#!/usr/bin/env python3
"""Opt-in disposable S0-S11 native Claude flow test; all professional answers simulated.

Real Main sessions use installed production assets/Hooks and native Agents.
This runner supplies only installation, clearly labeled scenario fixtures and
read-only observation. It never writes Runtime, receipts, stage cursors or Worker
answers, and never weakens a domain gate. No real product acceptance is claimed.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
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
  return {'revision':s.get('revision'),'lifecycle':s.get('lifecycle'),'repair_status':s.get('review',{}).get('repair',{}).get('status')}
 except (OSError,ValueError) as e: return {'observation_error':str(e)}
before=facts();start=time.time_ns();clock_start=time.monotonic()
p=subprocess.run(['bash','--noprofile','--norc','-c',original],input=raw,capture_output=True)
row={'event':event,'started_ns':start,'finished_ns':time.time_ns(),'elapsed_seconds':time.monotonic()-clock_start,'project_dir':os.environ.get('CLAUDE_PROJECT_DIR'),'process_cwd':os.getcwd(),'before':before,'after':facts(),'exit_code':p.returncode,'stdout':p.stdout.decode(errors='replace'),'stderr':p.stderr.decode(errors='replace'),'payload':payload}
(Path(artifacts)/('hook-'+str(start)+'-'+uuid.uuid4().hex+'.json')).write_text(json.dumps(row,ensure_ascii=False))
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr);sys.exit(p.returncode)
'''

BOUNDARY = '''# User-authorized simulation scope

This disposable project exercises S0–S11 with synthetic business inputs,
professional conclusions and human decisions. They are test-only and do not
represent real professional or product acceptance. Use the current installed
instructions, original forms and domain consumers. All required native Agents
perform their own assigned professional answers; Main does not author them.
Native tools remain available for reading, file handling, Git and fixture checks.

Do not implement or repair a real product, fabricate a real observation, edit
Runtime, journal or receipts by hand, bypass an original gate, publish, deploy
or push. Clearly label synthetic professional statements. Actual checks, if
run, must be described as actually run, without claiming they establish real
product quality. Preserve accepted sources and report any real blocker.

The fixture product bytes and modes stay unchanged. For S6 only, if the original
integration requires a source commit, the native Builder may prepare one clearly
labeled `git commit --allow-empty -m SIMULATION-source-fixture` in its own
worktree as test input; it is not product implementation. S9 uses the original
no-change route and does not manufacture a commit or changed artifact. Main
performs any actual checks required by the original integration; `make check`
checks only the pre-existing synthetic marker.
'''

PROMPT = '''在当前隔离项目继续 S0–S11 的合成流转，到模拟 release_authorized 或报告具体原始阻断。先读当前安装指引、simulation/SCENARIO.md 和 simulation/DECISIONS.md。所有专业判断、人类决定及首轮 finding 都是明确标记 SIMULATION 的测试输入，不代表真实业务开发或专业验收。

Main 和受委派原生 Agent 按当前已安装角色、原 brief、原问卷和实际 Hook 反馈工作。Main 自己完成原 Main 责任，Worker 自己填写本人原专业答案；不要代填、手改 Runtime/journal/receipt、伪造事件或绕过原消费者。必要时允许正常工具、目录和 Git 检查；实际系统失败时遵循其给出的原恢复入口，并如实保留原因与介入。不要 push、部署或真实发布。

测试输入中的专业结论为：S5 各适用责任模拟通过；S6 本人模拟完成其合成交付；首轮 S7 使用场景给定的一个模拟 finding，其他适用责任模拟 pass；S8 模拟调查支持该 finding 的原因假设并模拟批准修复；S9 独立 Targeted 模拟 pass；新一轮 S7 各适用责任模拟 pass。`simulation/observations/first-round.txt` 和 `repaired-round.txt` 是明确标记的场景输入。沿原 ObservationBatch、S8、S9、Main 原集成及独立 Targeted 进入新的完整 S7，再沿原 S10 和模拟 S11 决定到 release_authorized。保留完整原责任集合，不把模拟结果说成已执行的真实业务检查；无需真实业务调查、实现或 Worker 产品测试，原流程要求的实际框架检查仍照实执行并记录。

本测试不实现、修复或测试真实业务产品。S6 如原集成要求源提交，可在本人的原生 worktree 仅准备一个明确标记的空 SIMULATION fixture commit；S9 使用实际无变更结果，不伪造改动。原 Main 的 `make check` 仅检查预置测试标记，不证明场景业务功能。不要手工替系统填写接收事实；遇阻报告确切原因。

结束时简要如实反馈实际走过的阶段、曾需猜测的指引或走错的路径、额外手续、主要等待或返工、确切终点或阻断。不知道准确耗时就不要猜造数字。'''

SCENARIOS = {
    'ledger': {
        'title': '模拟交付来源台账', 'candidate_dir': 'tests/fixtures/native-stage-simulation/documents',
        'description': '''本路是纯文档和单 NFR 的来源台账。模拟用户要求受管交付物带有 SIMULATION_ONLY 标记，并可追溯交付来源；无 UI、HTTP、数据库或部署。预置 simulation/products/artifact.txt 是唯一机械标记夹具。提供的候选文档只是未接收的测试输入，可在对应阶段作为 Main 的占位起点，原问卷和消费者仍负责实际登记。''',
        'finding': 'SIMULATION ONLY: first-round QA finding models a missing provenance link for the synthetic delivery marker. It is test data, not a real product defect or executed observation.',
        'after': 'SIMULATION ONLY: the proposed repaired-round conclusion is that the provenance link is present; this is a test input, not an executed observation.'},
    'offline-cli': {
        'title': '离线日志归档 CLI', 'candidate_dir': None,
        'description': '''本路模拟一个离线日志归档 CLI，只有占位文档和预设结论，不实现命令或产品测试。FR-001：输入目录与输出路径产生归档及来源清单；FR-002：dry-run 只显示计划而不写入；FR-003：缺参、输入缺失或不可读、输入输出重叠、目标已存在及写入失败应报告错误并不留下假成功。NFR：不联网、不跟随符号链接、不越过输入根，清单按稳定相对路径排序。无浏览器、HTTP、数据库或部署，但 CLI 可见输出和失败路径属于功能范围。simulation/cli/README.md 是预置非执行占位文本；simulation/products/artifact.txt 和 make check 只是机械测试夹具，不证明 CLI 行为。本路不加载台账候选，请按原安装模板自行形成各阶段最小合成文档。''',
        'finding': 'SIMULATION ONLY: first-round QA finding models a traceability gap between symbolic-link rejection and error-output assertions for the offline archive CLI. It is a synthetic test input, not a real defect or executed observation.',
        'after': 'SIMULATION ONLY: the proposed repaired-round conclusion is that symbolic-link rejection and error-output assertions have the intended traceability; no real CLI was implemented or executed.'},
}



def run(cmd, **kwargs):
    return subprocess.run([str(x) for x in cmd], check=True, **kwargs)


def dump(path, obj):
    path.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + '\n')


def file_inventory(root, paths):
    return {str(path): hashlib.sha256((root/path).read_bytes()).hexdigest()
            for path in sorted(paths)}


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
    release_sha = hashlib.sha256(release_archive.read_bytes()).hexdigest()
    shutil.copy2(release_archive, base/'release.tar.gz')
    if hashlib.sha256((base/'release.tar.gz').read_bytes()).hexdigest() != release_sha:
        raise SystemExit('Release archive changed during copying; preserve this failed run')
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
    (root/'simulation/products').mkdir(parents=True)
    (root/'simulation/observations').mkdir()
    (root/'simulation/products/artifact.txt').write_text('SIMULATION_ONLY\n')
    (root/'Makefile').write_text('.PHONY: check\ncheck:\n\tpython3 -c "from pathlib import Path; assert Path(\'simulation/products/artifact.txt\').read_bytes() == b\'SIMULATION_ONLY\\n\'"\n')
    (root/'simulation/observations/first-round.txt').write_text(SCENARIOS[scenario]['finding'] + '\n')
    (root/'simulation/observations/repaired-round.txt').write_text(SCENARIOS[scenario]['after'] + '\n')
    if scenario == 'offline-cli':
        (root/'simulation/cli').mkdir(parents=True)
        (root/'simulation/cli/README.md').write_text('SIMULATION ONLY: offline archive CLI placeholder; no executable implementation.\n')
    (root/'simulation/SCENARIO.md').write_text('# SIMULATION — ' + SCENARIOS[scenario]['title'] + '\n\n' + SCENARIOS[scenario]['description'] + '\n')
    (root/'simulation/DECISIONS.md').write_text('''# Test-only simulated human decisions

Identity: simulation-human. These are authorized synthetic choices for this disposable workflow test, not real human consent or release authorization.

- Lock and start the scenario requirement at v1.0.0; development=simulation-dev; release target=local simulation-release.
- Endorse the scenario's actual minimal design direction and decide applicability from its stated scope.
- Approve the resulting synthetic RepairContract through its original decision application.
- At S11 choose simulated approve for this fixture only. Do not publish, deploy, push or perform external release.
''')
    with (base/'prepare.log').open('a') as log:
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
    fixture_paths = [path.relative_to(root) for directory in ('simulation/products', 'simulation/cli')
                     for path in (root/directory).rglob('*') if path.is_file()]
    fixture_files = {str(path): {'sha256': hashlib.sha256((root/path).read_bytes()).hexdigest(),
                                  'mode': oct((root/path).stat().st_mode & 0o777)} for path in sorted(fixture_paths)}
    dump(base/'metadata.json', {'scope':'simulation-only S0-S11; real native Claude parent and agents',
         'scenario':scenario, 'project':str(root), 'created_at':time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
         'claude_binary':str(claude_bin), 'claude_binary_sha256':hashlib.sha256(claude_bin.read_bytes()).hexdigest(),
         'claude_version':version, 'source_head_at_preparation':subprocess.check_output(['git','rev-parse','HEAD'],cwd=REPO,text=True).strip(),
         'driver_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
         'launcher_sha256':hashlib.sha256((root/'.claude/bin/loop-harness').read_bytes()).hexdigest(),
         'binary_sha256':hashlib.sha256((root/'.claude/bin/loop-harness-linux-amd64').read_bytes()).hexdigest(),
         'release_archive_sha256':release_sha, 'source_worktree_status':subprocess.check_output(['git','status','--short'],cwd=REPO,text=True),
         'fixture_strategy':'disclosed empty S6 source fixture if required; no-change S9; actual native tools available',
         'fixture_files':fixture_files})
    (base/'prompt.txt').write_text(PROMPT)
    status = subprocess.check_output(['git', 'status', '--porcelain'], cwd=root, text=True)
    if status:
        raise SystemExit('Prepared Git tree is not clean; preserve this failed run')
    dump(base/'prepared-inputs.json', {
        'project_head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(),
        'tracked_files': tracked_inventory(root),
        'candidate_files': candidate_files,
        'prompt_sha256': hashlib.sha256((base/'prompt.txt').read_bytes()).hexdigest(),
        'note': 'Frozen before the first model request. Never clean fixtures during a run.'})
    print(json.dumps({'prepared':str(base),'project':str(root),'initial_lifecycle':initial.get('lifecycle')},ensure_ascii=False), flush=True)


def launch(base, env_file, resume, prompt_file, driver=False):
    root = base/'project'
    if not env_file.is_file():
        raise SystemExit('Provider configuration file is missing; never supply its contents in logs')
    meta = json.loads((base/'metadata.json').read_text())
    claude_bin = Path(meta['claude_binary'])
    if not claude_bin.is_file() or hashlib.sha256(claude_bin.read_bytes()).hexdigest() != meta['claude_binary_sha256']:
        raise SystemExit('The prepared Claude executable changed; preserve this run')
    frozen_files = {base/'release.tar.gz': meta['release_archive_sha256'],
                    root/'.claude/bin/loop-harness': meta['launcher_sha256'],
                    root/'.claude/bin/loop-harness-linux-amd64': meta['binary_sha256'],
                    Path(__file__): meta['driver_sha256']}
    for path, expected in frozen_files.items():
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            raise SystemExit(f'Prepared release, launcher, binary or driver changed: {path}; preserve this run')
    index = len(list(base.glob('claude-run-*.jsonl'))) + 1
    if index == 1:
        frozen = json.loads((base/'prepared-inputs.json').read_text())
        if (subprocess.check_output(['git', 'status', '--porcelain'], cwd=root, text=True)
                or subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip() != frozen['project_head']
                or tracked_inventory(root) != frozen['tracked_files']):
            raise SystemExit('Prepared Git inputs drifted before the first request; prepare a fresh run')
        candidate_root = root/'.claude/test-inputs/candidates'
        actual_candidates = file_inventory(candidate_root, [path.relative_to(candidate_root) for path in candidate_root.rglob('*') if path.is_file()])
        if actual_candidates != frozen['candidate_files']:
            raise SystemExit('Prepared candidate inputs drifted; prepare a fresh run')
        if hashlib.sha256((base/'prompt.txt').read_bytes()).hexdigest() != frozen['prompt_sha256']:
            raise SystemExit('Prepared scenario prompt drifted; prepare a fresh run')
    prompt = prompt_file.read_text() if prompt_file else (base/'prompt.txt').read_text()
    dump(base/f'request-{index}.json', {'prompt_sha256': hashlib.sha256(prompt.encode()).hexdigest(),
         'prompt_source': str(prompt_file) if prompt_file else 'prepared prompt.txt',
         'resume': resume, 'test_driver': driver})
    args = [str(claude_bin),'-p','--permission-mode','bypassPermissions','--output-format','stream-json','--verbose','--include-hook-events','--setting-sources','project']
    if driver:
        system = "This disposable project uses the supplied synthetic scenario and simulated decisions. Follow current installed instructions and actual original forms. Native Agents author their own answers; do not implement real products, alter Runtime by hand, bypass consumers, or publish. Report blockers and describe actual actions honestly."
        (base/f'test-driver-system-{index}.txt').write_text(system)
        args += ['--append-system-prompt',system]
    if resume:
        args += ['--resume',resume]
    script = 'source "$1" >/dev/null 2>&1 && shift && exec "$@"'
    started_ns=time.time_ns()
    started=time.monotonic()
    with (base/f'claude-run-{index}.jsonl').open('w') as out, (base/f'claude-run-{index}.stderr.log').open('w') as err:
        try:
            result=subprocess.run(['bash','--noprofile','--norc','-c',script,'native-stage-simulation',str(env_file),*args],cwd=root,input=prompt,text=True,stdout=out,stderr=err,timeout=3600)
            code=result.returncode
        except subprocess.TimeoutExpired:
            code=124
        except KeyboardInterrupt:
            code=130
    dump(base/f'run-{index}.json',{'exit_code':code,'seconds':round(time.monotonic()-started,3),'started_ns':started_ns,'finished_ns':time.time_ns(),'external_interference':'driver interrupted by SIGINT' if code == 130 else 'driver timeout' if code == 124 else 'signal exit observed' if code < 0 else 'not inferred; inspect external intervention records','timing_scope':'model process wall time includes tools, Hooks and rework; not pure inference','resume':resume,'effort':'provider/default','test_driver':driver,'simulation_only':True})
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
    writes=[];commands=[];violations=[];native_returns=[];transcripts=[]
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
                    data=resolved.read_bytes(); digest=hashlib.sha256(data).hexdigest()
                    dest=base/'actor-transcripts'/f'{digest}.jsonl'; dest.parent.mkdir(exist_ok=True)
                    if not dest.exists(): dest.write_bytes(data)
                    item.update(copy=str(dest.relative_to(base)),sha256=digest)
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
            forbidden=any(x in cmd for x in ('git commit','git add ','go test','go build','npm test','npm run','pytest','cargo test','make '))
            if forbidden: violations.append(item)
    dump(base/'worker-tool-audit.json',{'writes':writes,'bash_commands':commands,'potential_boundary_violations':violations,'note':'Read-only observations, not tool restrictions. Commands and non-private writes require review of actual work content; raw original Hook payloads are authoritative.'})
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
    evidence_drift=[]
    evidence_entries=state.get('evidence', [])
    if not isinstance(evidence_entries,list):
        invalid_observations.append({'file':'.claude/loop-state.json','error':'evidence is not an array'})
        evidence_entries=[]
    for entry in evidence_entries:
        if not isinstance(entry,dict):
            invalid_observations.append({'file':'.claude/loop-state.json','error':'evidence entry is not an object'})
            continue
        if entry.get('status') != 'valid': continue
        ref=entry.get('path'); expected=entry.get('sha256')
        if not ref or not expected: continue
        target=root/ref
        try: actual=hashlib.sha256(target.read_bytes()).hexdigest()
        except OSError: actual=None
        if actual != expected:
            evidence_drift.append({'id':entry.get('id'), 'kind':entry.get('kind'), 'path':ref, 'expected_sha256':expected, 'actual_sha256':actual})
    dump(base/'registered-evidence-source-audit.json', {'valid_source_drift':evidence_drift, 'note':'Read-only test-input hygiene audit; this never repairs evidence, invalidates an entry or changes Runtime.'})
    fixture_expected=json.loads((base/'metadata.json').read_text())['fixture_files']
    fixture_actual={}
    for directory in ('simulation/products','simulation/cli'):
        for path in (root/directory).rglob('*'):
            if path.is_file() and not path.is_symlink():
                fixture_actual[str(path.relative_to(root))]={'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),
                                                            'mode':oct(path.stat().st_mode & 0o777)}
    fixture_changed=set(fixture_actual)!=set(fixture_expected) or any(fixture_actual.get(p,{}).get('sha256')!=v['sha256'] for p,v in fixture_expected.items())
    fixture_mode_changed=set(fixture_actual)!=set(fixture_expected) or any(fixture_actual.get(p,{}).get('mode')!=v['mode'] for p,v in fixture_expected.items())
    s1_bindings=[x['observation_file'] for x in rows if x['event']=='PostToolUse' and cursor(x['before'])[0]=='inactive' and cursor(x['after'])==('planning','design') and 'S1 binding recorded through original TR-001' in x.get('stdout','')]
    data={'simulation_only':True,'original_s1_binding_observations':s1_bindings,'platform_hooks':{'started':len(hook_started),'responses':len(hook_responses),'unreturned_started_ids':sorted(set(hook_started)-set(hook_responses)),'non_success':hook_failures},'invalid_observations':invalid_observations,'invalid_stream_lines':invalid_stream_lines,'journal_transitions':journal_rows,'latest_activity':latest_activity[-3:],'native_actor_count':len(actors),'cursors_observed':cursors,'native_calls':native_calls,'native_returns':native_returns,'native_messages':native_messages,'actor_transcripts':transcripts,'actors':actors,'ordinary_worker_events':len(ordinary),'ordinary_worker_nonzero_exit':sum(x['exit_code']!=0 for x in ordinary),'ordinary_worker_nonempty_stdout':sum(bool(x['stdout']) for x in ordinary),'ordinary_worker_revision_changes':len(revision_windows),'ordinary_revision_windows_with_overlapping_events':sum(bool(x['overlapping_events']) for x in revision_windows),'sessions':sessions,'results':results,'final_revision':state.get('revision'),'final_lifecycle':state.get('lifecycle'),'fixture_changed':fixture_changed,'fixture_mode_changed':fixture_mode_changed,'fixture_actual':fixture_actual,'worker_private_answer_writes':sum(x['private_answer_path'] for x in writes),'worker_other_writes':sum(not x['private_answer_path'] for x in writes),'worker_bash_commands':len(commands),'potential_boundary_violations':violations,'registered_evidence_source_drift':evidence_drift}
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
            'fixture_unchanged':not s['fixture_changed'],'fixture_mode_unchanged':not s['fixture_mode_changed'],
            'registered_evidence_sources_intact':not s['registered_evidence_source_drift'],
            'journal_present':journal_path.is_file(),
            'observations_readable':not s['invalid_observations'] and not s['invalid_stream_lines']}
    failures=[k for k,v in checks.items() if v is False]
    result={'simulation_only':True,'automated_observation_status':'failed' if failures else 'requires_independent_audit',
            'full_flow_proven':None,'stages':stages,'observation_checks':checks,'failed_observations':failures,
            'independent_audit_required':['Associate the actual first-round Finding and accepted source with its S8 Case and Contract.',
                'Associate the original S9 Candidate, Final, Impact, independent Targeted, Handoff/TR-012, new S7 CleanRound/TR-009 and S10/S11 transitions.',
                'Associate each actual native call/start/author input and saved original/Stop/completed return with the same platform actor and engineering responsibility; async completion follows actual platform facts.',
                'Review raw tool/Hook events, source identities, any recovery or cleanup_pending, process interruptions and product boundary.'],
            'worker_activity_needing_review':{'command_signals':s['potential_boundary_violations'],'non_private_write_count':s['worker_other_writes'],'audit_file':'worker-tool-audit.json'},
            'boundary':'Cursor, actor counts and fixture hashes are observations, not a receipt-chain proof. Nonempty Hook output is normal feedback. Commands and paths do not alone establish product work. This is not product or professional acceptance.'}
    dump(base/'verification.json',result)
    print(json.dumps(result,ensure_ascii=False),flush=True)
    raise SystemExit(1 if failures else 2)  # 2: observations need independent audit, not a full PASS.


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
    args=p.parse_args()
    base=args.artifacts.resolve()
    if args.dry_run and args.action != 'prepare': p.error('--dry-run applies only to prepare')
    if args.action=='prepare': prepare(base, args.scenario, args.release_archive.resolve() if args.release_archive else None,
                                       args.env_file.expanduser(), args.claude_bin.expanduser() if args.claude_bin else None, args.dry_run)
    elif args.action=='run': launch(base,args.env_file.expanduser(),args.resume,args.prompt_file,args.driver)
    elif args.action=='summarize': summarize(base)
    else: verify(base)
