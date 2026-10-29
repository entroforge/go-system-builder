import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createSession as registerSession, issueForm, receiveAnswer } from './engine.mjs';

const definition = JSON.parse(readFileSync(new URL('./s05-template.json', import.meta.url), 'utf8'));
const createSession = () => registerSession(definition);

const pass = { conclusion: 'pass', findings: [] };
const finding = {
  path: 'fixtures/contracts.json',
  expected: '没有导出权限时拒绝请求。',
  observed: '合同没有定义拒绝结果。',
};
function deliver(session, owner, answer = pass, packet = issueForm(session, owner)) {
  return receiveAnswer(session, owner, packet, answer);
}

test('领卷即能看见填写规则和允许的结论，不必查另一份协议', () => {
  const form = issueForm(createSession(), 'reviewer-spec');
  assert.ok(form?.questions?.conclusion?.choices.includes('pass'));
  assert.ok(form?.questions?.findings?.example?.observed);
  assert.deepEqual(form?.answer, { conclusion: null, findings: [] });
  assert.ok(form?.read?.every(item => item.path && item.purpose));
  assert.equal(form?.instruction?.includes('只修改 answer'), true);
});

test('第一名审查者交卷即被接收，等待他人时不再要求其工作', () => {
  const session = createSession();
  const receipt = deliver(session, 'reviewer-spec');
  assert.equal(receipt?.status, 'accepted_waiting');
  assert.equal(receipt?.work_complete, true);
  assert.equal(receipt?.waiting_for?.[0], 'reviewer-task');
  assert.equal(session?.stage, 'S05');
});

test('最后一份独立答卷落盘立即进入 S6，无须额外工具事件', () => {
  const session = createSession();
  deliver(session, 'reviewer-spec');
  const receipt = deliver(session, 'reviewer-task');
  assert.equal(receipt?.status, 'advanced');
  assert.equal(session?.stage, 'S06');
  assert.equal(session?.transitions?.length, 1);
});

test('重复交卷回放原回执，不追加证据或重复迁移', () => {
  const session = createSession();
  deliver(session, 'reviewer-spec');
  const packet = issueForm(session, 'reviewer-task');
  const receipt = deliver(session, 'reviewer-task', pass, packet);
  const count = session.events.length;
  const repeat = deliver(session, 'reviewer-task', pass, packet);
  assert.deepEqual(repeat, receipt);
  assert.equal(session.events.length, count);
  assert.equal(session.transitions.length, 1);
});

test('两份在同一基线上独立生成的分卷均保留，不覆盖其他人的答案', () => {
  const session = createSession();
  const spec = issueForm(session, 'reviewer-spec');
  const task = issueForm(session, 'reviewer-task');
  deliver(session, 'reviewer-spec', pass, spec);
  deliver(session, 'reviewer-task', pass, task);
  assert.deepEqual(session.answers?.spec?.answer, pass);
  assert.deepEqual(session.answers?.task?.answer, pass);
});

test('空题保留为草稿，错误直接定位 answer 的 JSON Pointer', () => {
  const session = createSession();
  const receipt = deliver(session, 'reviewer-spec', { conclusion: null, findings: [] });
  assert.equal(receipt?.status, 'needs_answer');
  assert.equal(receipt?.issues?.[0]?.pointer, '/answer/conclusion');
  assert.equal(session.events.length, 0);
});

test('失败结论必须带可定位的观察；拒收不写入完成事实', () => {
  const session = createSession();
  const receipt = deliver(session, 'reviewer-spec', { conclusion: 'fix_required', findings: [] });
  assert.equal(receipt?.status, 'invalid_answer');
  assert.equal(receipt?.issues?.[0]?.pointer, '/answer/findings');
  assert.equal(session.events.length, 0);
});

test('有问题的完整答卷自动走规格返工，不把完整误当通过', () => {
  const session = createSession();
  const receipt = deliver(session, 'reviewer-spec', { conclusion: 'fix_required', findings: [finding] });
  assert.equal(receipt?.status, 'routed');
  assert.equal(session?.stage, 'S02');
  assert.equal(receipt?.work_complete, true);
});

test('需求变更答卷只进入人闸，不构造人工批准', () => {
  const session = createSession();
  const receipt = deliver(session, 'reviewer-spec', { conclusion: 'req_change_required', findings: [finding] });
  assert.equal(receipt?.status, 'awaiting_human');
  assert.equal(session?.stage, 'S05');
  assert.equal(session?.humanRequired, true);
  assert.equal(session?.transitions?.length, 0);
});

test('冻结资料变化拒收旧卷，不给旧结论盖上新基线', () => {
  const session = createSession();
  const packet = issueForm(session, 'reviewer-spec');
  session.subjectVersion += 1;
  const receipt = deliver(session, 'reviewer-spec', pass, packet);
  assert.equal(receipt?.status, 'stale');
  assert.equal(session.events.length, 0);
});

test('调用者身份来自派发绑定，不接受抄另一人的卷冒名交卷', () => {
  const session = createSession();
  const packet = issueForm(session, 'reviewer-spec');
  const receipt = deliver(session, 'reviewer-task', pass, packet);
  assert.equal(receipt?.status, 'wrong_owner');
  assert.equal(session.events.length, 0);
});

test('更改题目不能改变需要满足的约束', () => {
  const session = createSession();
  const packet = issueForm(session, 'reviewer-spec');
  assert.ok(packet, '应生成答卷');
  packet.questions.conclusion.choices.push('fail');
  const receipt = deliver(session, 'reviewer-spec', { conclusion: 'fail', findings: [] }, packet);
  assert.equal(receipt?.status, 'invalid_answer');
  assert.equal(receipt?.issues?.[0]?.pointer, '/answer/conclusion');
});

test('不支持的字段和无法读取的引用返回明确修正点', () => {
  const session = createSession();
  const extra = deliver(session, 'reviewer-spec', { ...pass, requested_event: 'document_pass' });
  assert.equal(extra?.issues?.[0]?.pointer, '/answer/requested_event');
  const missing = deliver(session, 'reviewer-spec', {
    conclusion: 'fix_required', findings: [{ ...finding, path: 'missing.json' }],
  });
  assert.equal(missing?.issues?.[0]?.pointer, '/answer/findings/0/path');
  assert.equal(session.events.length, 0);
});

test('等待第二人时修订保留历史，只替代本人当前结论', () => {
  const session = createSession();
  const packet = issueForm(session, 'reviewer-spec');
  deliver(session, 'reviewer-spec', pass, packet);
  const receipt = deliver(session, 'reviewer-spec', { conclusion: 'fix_required', findings: [finding] }, packet);
  assert.equal(receipt?.status, 'routed');
  assert.equal(session.events.length, 2);
  assert.equal(session.events[0].answer.conclusion, 'pass');
  assert.equal(session.events[1].supersedes, session.events[0].id);
});

test('问题由注册 JSON 模板生成，不藏在实现代码里', () => {
  const template = structuredClone(definition);
  template.title = 'S05 实战审查';
  template.roles['reviewer-spec'].question = '这版协议是否兑现全部权限承诺？';
  const session = registerSession(template);
  const packet = issueForm(session, 'reviewer-spec');
  assert.equal(packet.title, 'S05 实战审查 · 规格一致性');
  assert.equal(packet.questions.conclusion.question, template.roles['reviewer-spec'].question);
});

test('未知 adapter 不能被描述成已具备登记能力', () => {
  assert.throws(() => registerSession({ ...definition, adapter: 'arbitrary_workflow' }), /adapter/);
});

test('修订后重新保存原旧答案，不能回放旧回执把本人新结论恢复为通过', () => {
  const session = createSession();
  const packet = issueForm(session, 'reviewer-spec');
  deliver(session, 'reviewer-spec', pass, packet);
  deliver(session, 'reviewer-spec', { conclusion: 'fix_required', findings: [finding] }, packet);
  const old = deliver(session, 'reviewer-spec', pass, packet);
  assert.equal(old?.status, 'superseded');
  assert.equal(old?.stage, 'S02');
  assert.equal(session.answers.spec.answer.conclusion, 'fix_required');
  assert.equal(session.events.length, 2);
});

test('阶段已推进后重复第一人的投递，回执报告当前阶段而非继续等待', () => {
  const session = createSession();
  const packet = issueForm(session, 'reviewer-spec');
  deliver(session, 'reviewer-spec', pass, packet);
  deliver(session, 'reviewer-task');
  const receipt = deliver(session, 'reviewer-spec', pass, packet);
  assert.equal(receipt.status, 'already_received');
  assert.equal(receipt.stage, 'S06');
  assert.equal(session.events.length, 2);
});

test('新基线的第二份答卷不能搭配旧基线第一份 PASS 推进', () => {
  const session = createSession();
  deliver(session, 'reviewer-spec');
  session.subjectVersion += 1;
  const receipt = deliver(session, 'reviewer-task');
  assert.equal(receipt.status, 'accepted_waiting');
  assert.deepEqual(receipt.waiting_for, ['reviewer-spec']);
  assert.equal(session.stage, 'S05');
  assert.equal(session.transitions.length, 0);
});

test('同一 Stage 进入人闸后，重复旧 PASS 回执改为等待需求负责人', () => {
  const session = createSession();
  const packet = issueForm(session, 'reviewer-spec');
  deliver(session, 'reviewer-spec', pass, packet);
  deliver(session, 'reviewer-task', { conclusion: 'req_change_required', findings: [finding] });
  const receipt = deliver(session, 'reviewer-spec', pass, packet);
  assert.equal(receipt.status, 'awaiting_human');
  assert.equal(receipt.owner, '需求负责人');
  assert.equal(receipt.work_complete, true);
  assert.equal(session.events.length, 2);
});
