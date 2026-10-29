import { createSession, issueForm, receiveAnswer, currentAnswers } from './engine.mjs';

const el = id => document.getElementById(id);
const pretty = value => JSON.stringify(value, null, 2);
const response = await fetch('./s05-template.json');
if (!response.ok) throw new Error('无法读取 s05-template.json。请从原型目录启动本地服务器。');
const definition = await response.json();
let session, packets, drafts, timer, lastSubmission, pendingSave;
const labels = {
  accepted_waiting: '你的审查已接收，工作已完成。', advanced: '已自动进入 S06。',
  needs_answer: '草稿已保存，这一题还没回答。', invalid_answer: '请只修正下面的格子。',
  routed: '完整发现已接收，进入 S02 规格返工。', awaiting_human: '你的工作已完成，等待需求负责人。',
  stale: '这是旧卷，不能把新基线盖到旧结论上。', wrong_owner: '这份卷不属于当前派发责任。',
  superseded: '旧回答已被替代，保留当前结论。', already_received: '已接收过这份回答，当前阶段已更新。',
};

function showReceipt(receipt) {
  el('receipt-title').textContent = labels[receipt.status] ?? receipt.status;
  el('receipt-detail').textContent = [
    ...(receipt.issues ?? []).map(issue => `${issue.pointer}\n${issue.message}`),
    receipt.next,
  ].filter(Boolean).join('\n\n');
  el('receipt').dataset.error = String(['invalid_answer', 'stale', 'wrong_owner', 'needs_answer'].includes(receipt.status));
}

function renderState() {
  const validAnswers = currentAnswers(session);
  el('stage').textContent = `当前阶段：${session.stage}${session.humanRequired ? ' · 人闸' : ''}`;
  el('accepted').textContent = `当前有效：${Object.keys(validAnswers).length} / 2`;
  const waiting = Object.entries(definition.roles).filter(([, slot]) => !validAnswers[slot.key]).map(([owner]) => owner);
  el('pending').textContent = session.humanRequired ? '等待：需求负责人裁定' : session.stage === 'S05' ? `等待：${waiting.join('、') || '系统求门'}` : '当前审查已闭合，下一责任已明确';
  el('aggregate').textContent = pretty(Object.fromEntries(Object.entries(validAnswers).map(([key, event]) => [key, { owner: event.owner, answer: event.answer }])));
  el('events').textContent = pretty({ answers: session.events, transitions: session.transitions });
}

async function showOwner() {
  const owner = el('owner').value;
  packets[owner] ??= issueForm(session, owner);
  drafts[owner] ??= pretty(packets[owner].answer);
  const packet = packets[owner];
  el('form-title').textContent = packet.title;
  el('instruction').textContent = packet.instruction;
  el('conclusion-question').textContent = packet.questions.conclusion.question;
  el('finding-question').textContent = packet.questions.findings.question;
  el('finding-example').textContent = pretty(packet.questions.findings.example);
  el('packet').textContent = pretty(packet);
  el('choices').replaceChildren(...packet.questions.conclusion.choices.map(choice => {
    const row = document.createElement('div'); row.className = 'option';
    const code = document.createElement('code'); code.textContent = choice;
    row.append(code, ` ${packet.questions.conclusion.meaning[choice]}`); return row;
  }));
  el('answer').value = drafts[owner];
  el('sources').replaceChildren(...packet.read.map(source => {
    const details = document.createElement('details');
    const summary = document.createElement('summary'); summary.textContent = `${source.path} · ${source.purpose}`;
    const pre = document.createElement('pre'); pre.textContent = '展开后展示合成资料。';
    details.append(summary, pre);
    details.addEventListener('toggle', async () => {
      if (!details.open) return;
      try { const data = await fetch(source.path); if (!data.ok) throw new Error(`HTTP ${data.status}`); pre.textContent = pretty(await data.json()); }
      catch (error) { pre.textContent = `资料读取失败：${error.message}`; }
    });
    return details;
  }));
  if (currentAnswers(session)[definition.roles[owner].key]) showReceipt({ status: 'already_received', next: `当前 Stage 为 ${session.stage}。本人的答案已经接收，等待归属见汇总。` });
  else showReceipt({ status: 'needs_answer', next: '只填写当前 answer。通过时 findings 为 []；未通过时照左侧示例记录。' });
}

function savePending() {
  clearTimeout(timer);
  if (!pendingSave) return;
  const { owner, text, packet } = pendingSave;
  pendingSave = null;
  drafts[owner] = text;
  let answer;
  try { answer = JSON.parse(drafts[owner]); }
  catch (error) { showReceipt({ status: 'invalid_answer', issues: [{ pointer: '/answer', message: `JSON 尚未完整：${error.message}` }], next: '继续编辑这份草稿。' }); return; }
  lastSubmission = { owner, packet: structuredClone(packet), answer: structuredClone(answer) };
  showReceipt(receiveAnswer(session, owner, packet, answer));
  renderState();
}

function save() {
  const owner = el('owner').value;
  pendingSave = { owner, text: el('answer').value, packet: structuredClone(packets[owner]) };
  savePending();
}

function reset() {
  clearTimeout(timer);
  session = createSession(definition); packets = {}; drafts = {}; lastSubmission = null; pendingSave = null;
  el('owner').value = 'reviewer-spec';
  showOwner(); renderState();
}
el('owner').addEventListener('change', () => { savePending(); showOwner(); });
el('reset').addEventListener('click', reset);
el('answer').addEventListener('input', () => {
  drafts[el('owner').value] = el('answer').value;
  const owner = el('owner').value;
  pendingSave = { owner, text: el('answer').value, packet: structuredClone(packets[owner]) };
  clearTimeout(timer); timer = setTimeout(savePending, 700);
});
el('replay').addEventListener('click', () => {
  savePending();
  if (!lastSubmission) { showReceipt({ status: 'needs_answer', next: '尚无保存可重复投递。' }); return; }
  showReceipt(receiveAnswer(session, lastSubmission.owner, lastSubmission.packet, lastSubmission.answer)); renderState();
});
el('drift').addEventListener('click', () => {
  savePending(); session.subjectVersion += 1;
  renderState();
  showReceipt({ status: 'stale', next: '模拟冻结资料已变。再次保存原卷会被拒收。生产系统应按原变更流程重新发卷；重置可开始新的演示。' });
});
for (const [button, conclusion, incomplete] of [
  ['example-pass', 'pass', false], ['example-incomplete', 'fix_required', true],
  ['example-fix', 'fix_required', false], ['example-human', 'req_change_required', false],
]) el(button).addEventListener('click', () => {
  const finding = structuredClone(packets[el('owner').value].questions.findings.example);
  if (incomplete) finding.observed = '';
  el('answer').value = pretty({ conclusion, findings: conclusion === 'pass' ? [] : [finding] });
  save();
});
reset();
