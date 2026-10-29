// In-memory S5 interaction prototype. Does not call Harness, Git or Claude Hooks.
// subjectVersion represents frozen input identity; it is not a real SHA verifier.
const conclusions = ['pass', 'fix_required', 'req_change_required'];
const clone = value => structuredClone(value);
function stable(value) {
  if (Array.isArray(value)) return '[' + value.map(stable).join(',') + ']';
  if (value && typeof value === 'object') {
    return '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + stable(value[key])).join(',') + '}';
  }
  return JSON.stringify(value);
}

export function createSession(definition) {
  if (definition?.stage !== 'S05' || definition?.adapter !== 'document_review_demo') throw new Error('本原型只支持 S05 的 document_review_demo adapter。');
  if (stable(definition.questions?.conclusion?.choices) !== stable(conclusions)) throw new Error('模板 choices 与 S05 adapter 支持的值不一致。');
  return {
    definition: clone(definition),
    stage: 'S05', subjectVersion: 1, attempt: 1, humanRequired: false,
    answers: {}, events: [], transitions: [], issued: {}, receipts: {},
  };
}

export function issueForm(session, owner) {
  const slot = session.definition.roles[owner];
  if (!slot) throw new Error('演示只派发给 reviewer-spec 和 reviewer-task。');
  const id = `demo-S05-${session.attempt}-v${session.subjectVersion}-${slot.key}`;
  session.issued[id] ??= { owner, slot: slot.key, subjectVersion: session.subjectVersion, attempt: session.attempt };
  return {
    form_id: id,
    title: `${session.definition.title} · ${slot.title}`,
    instruction: session.definition.instruction,
    read: clone(session.definition.read),
    questions: { ...clone(session.definition.questions), conclusion: { ...clone(session.definition.questions.conclusion), question: slot.question } },
    answer: clone(session.definition.answer_template),
  };
}

// The board and admission check consume the same current-input predicate.
export function currentAnswers(session) {
  return Object.fromEntries(Object.entries(session.answers).filter(([, event]) =>
    event.subjectVersion === session.subjectVersion && event.attempt === session.attempt));
}

function validate(answer, sources) {
  const issues = [];
  const issue = (pointer, message) => issues.push({ pointer, message });
  if (!answer || typeof answer !== 'object' || Array.isArray(answer)) {
    return [{ pointer: '/answer', message: '填写题目旁生成的对象，保留 conclusion 和 findings。' }];
  }
  for (const key of Object.keys(answer)) {
    if (!['conclusion', 'findings'].includes(key)) issue(`/answer/${key}`, '这不是本卷的题目；身份、哈希和事件由系统生成。');
  }
  if (!conclusions.includes(answer.conclusion)) issue('/answer/conclusion', '请选择 pass、fix_required 或 req_change_required；null 表示尚未回答。');
  if (!Array.isArray(answer.findings)) {
    issue('/answer/findings', '填写数组；通过时为 []。');
    return issues;
  }
  if (answer.conclusion === 'pass' && answer.findings.length) issue('/answer/findings', '有未处置发现时请选择 fix_required 或 req_change_required。');
  if (['fix_required', 'req_change_required'].includes(answer.conclusion) && !answer.findings.length) issue('/answer/findings', '这项结论需要至少一条具体发现；照题目示例填写。');
  answer.findings.forEach((finding, index) => {
    const base = `/answer/findings/${index}`;
    if (!finding || typeof finding !== 'object' || Array.isArray(finding)) {
      issue(base, '填写含 path、expected、observed 的对象。');
      return;
    }
    for (const key of Object.keys(finding)) {
      if (!['path', 'expected', 'observed'].includes(key)) issue(`${base}/${key}`, '本原型的发现只接收 path、expected、observed。');
    }
    for (const key of ['path', 'expected', 'observed']) {
      if (typeof finding[key] !== 'string' || !finding[key].trim()) issue(`${base}/${key}`, '填写非空内容；写明具体位置、预期和观察。');
    }
    if (typeof finding.path === 'string' && finding.path.trim() && !sources.some(source => source.path === finding.path)) issue(`${base}/path`, '引用无法读取；本演示可引用 read 中的三个 fixture 路径。');
  });
  return issues;
}

export function receiveAnswer(session, owner, packet, answer) {
  // owner is supplied by the simulated dispatch channel, never by the answer.
  const binding = session.issued[packet?.form_id];
  if (!binding || binding.owner !== owner) return { status: 'wrong_owner', work_complete: false, next: '使用派发给自己的答卷；由派发器修正归属。' };
  if (binding.subjectVersion !== session.subjectVersion || binding.attempt !== session.attempt) return { status: 'stale', work_complete: false, next: '冻结资料已变；系统应先发当前卷并标出差异，不能为旧结论重绑定。' };
  const key = packet.form_id + ':' + stable(answer);
  if (session.receipts[key]) {
    const saved = session.receipts[key];
    if (session.answers[binding.slot]?.id !== saved.event_id) return { status: 'superseded', work_complete: true, stage: session.stage, next: '这份旧回答已被新结论替代；当前结论保留，不能重放旧通过。' };
    if (session.humanRequired) return { status: 'awaiting_human', work_complete: true, stage: session.stage, owner: '需求负责人', next: '你的回答已接收。当前等待需求负责人裁定，无需继续等待旧审查责任或重新登记。' };
    if (saved.receipt.stage !== session.stage) return { status: 'already_received', work_complete: true, stage: session.stage, next: '这份回答已接收；当前阶段已经改变，无需再次登记或等待旧责任。' };
    return clone(saved.receipt);
  }
  if (session.stage !== 'S05' || session.humanRequired) return { status: 'stale', work_complete: false, next: '这份卷已不在当前工作阶段；读取当前回执。' };
  const issues = validate(answer, session.definition.read);
  if (issues.length) return {
    status: answer?.conclusion === null && issues.length === 1 ? 'needs_answer' : 'invalid_answer',
    work_complete: false, issues, next: '只修正 issues 所指的 answer 字段并保存。',
  };

  const previous = session.answers[binding.slot];
  const event = { id: `demo-answer-${session.events.length + 1}`, slot: binding.slot, owner, subjectVersion: binding.subjectVersion, attempt: binding.attempt, answer: clone(answer), supersedes: previous?.id ?? null };
  session.events.push(event);
  session.answers[binding.slot] = clone(event);
  let receipt;
  if (answer.conclusion === 'req_change_required') {
    session.humanRequired = true;
    receipt = { status: 'awaiting_human', work_complete: true, stage: 'S05', owner: '需求负责人', next: '需求负责人裁定发现中的需求问题；审查者无需继续尝试推进。' };
  } else if (answer.conclusion === 'fix_required') {
    session.stage = 'S02';
    session.transitions.push({ from: 'S05', to: 'S02', reason: 'document_fix_required' });
    receipt = { status: 'routed', work_complete: true, stage: 'S02', next: '已进入规格返工；规划者按 findings 修正受影响文档。' };
  } else {
    const validAnswers = currentAnswers(session);
    const waiting = Object.entries(session.definition.roles).filter(([, slot]) => !validAnswers[slot.key]).map(([agent]) => agent);
    if (waiting.length) {
      receipt = { status: 'accepted_waiting', work_complete: true, stage: 'S05', waiting_for: waiting, next: '你的审查已接收。等待另一名审查者；无需补登记或推进命令。' };
    } else {
      session.stage = 'S06';
      session.transitions.push({ from: 'S05', to: 'S06', reason: 'independent_document_pass' });
      receipt = { status: 'advanced', work_complete: true, stage: 'S06', next: '两份独立审查均已接收；主会话进入 S06，按已批准 TASK 派发 Builder。' };
    }
  }
  session.receipts[key] = { receipt: clone(receipt), event_id: event.id };
  return receipt;
}
