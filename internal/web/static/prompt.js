// Prompts: what the agent needs from you before it can carry on — permission
// to run a tool, or answers to questions it asked. The turn is stopped until
// one is settled, so a prompt takes over the screen. A prompt with several
// questions is walked one question at a time, the way the agent would ask
// them in person.

const promptQueue = [];
let openPrompt = null; // { prompt, onAnswer, choices, index }

// Well-known option ids get a translated label; anything else the agent
// invented is shown as it wrote it.
const OPTION_LABELS = { allow: 'promptAllow', always: 'promptAlways', deny: 'promptDeny' };

// OTHER_OPTION is the UI's own choice, never one the agent offered: picking
// it opens a box to type an answer in.
const OTHER_OPTION = '__other__';

function optionLabel(option) {
  const key = OPTION_LABELS[option.id];
  return key ? t(key) : option.label;
}

// showPrompt queues a prompt and shows it as soon as the screen is free.
function showPrompt(prompt, onAnswer) {
  promptQueue.push({ prompt, onAnswer, choices: {}, index: 0 });
  if (!openPrompt) nextPrompt();
}

// closePrompt drops a prompt the agent withdrew, whether it is on screen or
// still waiting its turn.
function closePrompt(id) {
  const queued = promptQueue.findIndex((p) => p.prompt.id === id);
  if (queued >= 0) promptQueue.splice(queued, 1);
  if (openPrompt && openPrompt.prompt.id === id) {
    openPrompt = null;
    nextPrompt();
  }
}

// dismissPrompt answers nothing, which the agent reads as a refusal — even
// when earlier questions were already answered.
function dismissPrompt() {
  if (openPrompt) settle({});
}

function settle(choices) {
  const { prompt, onAnswer } = openPrompt;
  openPrompt = null;
  onAnswer(prompt.id, choices);
  nextPrompt();
}

function nextPrompt() {
  const sheet = document.getElementById('prompt');
  openPrompt = promptQueue.shift() || null;
  if (!openPrompt) {
    sheet.hidden = true;
    sheet.innerHTML = '';
    renderState();
    return;
  }
  drawStep();
}

// answerStep records this question's answer and moves on, settling the whole
// prompt once the last question is behind us.
function answerStep(picked) {
  const { prompt, index, choices } = openPrompt;
  const id = prompt.questions[index].id;
  if (picked.length) choices[id] = picked;
  else delete choices[id];
  openPrompt.index = index + 1;
  if (openPrompt.index >= prompt.questions.length) settle(choices);
  else drawStep();
}

function drawStep() {
  const sheet = document.getElementById('prompt');
  const { prompt, index } = openPrompt;
  const questions = prompt.questions || [];
  const question = questions[index];
  if (!question) {
    settle(openPrompt.choices);
    return;
  }

  sheet.innerHTML = renderStep(prompt, question, index, questions.length);
  sheet.hidden = false;
  renderState();
  wireStep(sheet, question);
  restoreStep(sheet, question, openPrompt.choices[question.id]);

  const first = sheet.querySelector('.picked') || sheet.querySelector('.choice');
  if (first) first.focus();
}

// restoreStep puts an earlier answer back on screen, so stepping back shows
// what you picked rather than a blank question.
function restoreStep(sheet, question, answered) {
  if (!answered || !answered.length) return;

  const options = new Set((question.options || []).map((o) => o.id));
  const written = answered.find((value) => !options.has(value));
  const typed = sheet.querySelector('.prompt-text');

  if (written && typed) {
    const other = sheet.querySelector(`[data-option="${OTHER_OPTION}"]`);
    openTyping(sheet, other, typed, sheet.querySelector('.prompt-send'));
    typed.querySelector('textarea').value = written;

    return;
  }

  sheet.querySelectorAll('.choice').forEach((b) => {
    b.classList.toggle('picked', answered.includes(b.dataset.option));
  });

  if (question.multi) sheet.querySelector('.prompt-send').hidden = false;
}

function renderStep(prompt, question, index, total) {
  const kind = prompt.kind === 'choice' ? t('promptQuestion') : t('promptPermission');
  const step = total > 1
    ? `<span class="prompt-step">${num(t('promptStep').replace('{n}', index + 1).replace('{total}', total))}</span>`
    : '';
  const head = `<div class="prompt-head"><span class="label">${kind}</span>${
    prompt.title ? `<span class="prompt-title">${escapeHTML(prompt.title)}</span>` : ''}${step}</div>`;
  const detail = prompt.detail ? `<pre class="prompt-detail" dir="ltr">${escapeHTML(prompt.detail)}</pre>` : '';

  const label = question.label ? `<span class="label">${escapeHTML(question.label)}</span>` : '';
  const text = question.text ? `<p class="prompt-question" dir="auto">${escapeHTML(question.text)}</p>` : '';

  const options = (question.options || []).map((option) => choiceButton(option.id, optionLabel(option), option.detail));
  if (question.freeText) options.push(choiceButton(OTHER_OPTION, t('promptOther'), ''));

  const typed = question.freeText
    ? `<div class="prompt-text" hidden><textarea rows="2" dir="auto" placeholder="${t('promptTyped')}"></textarea></div>`
    : '';

  return `<div class="prompt-card" role="dialog" aria-modal="true">
      ${head}${detail}
      <div class="prompt-q">${label}${text}<div class="choices">${options.join('')}</div>${typed}</div>
      <div class="prompt-foot">
        <button type="button" class="prompt-dismiss">${t('promptDismiss')}</button>
        ${index > 0 ? `<button type="button" class="prompt-back">${t('promptBack')}</button>` : ''}
        <button type="button" class="prompt-send" hidden>${t('promptSend')}</button>
      </div>
    </div>`;
}

function choiceButton(id, label, detail) {
  return `<button type="button" class="choice" data-option="${escapeHTML(id)}">
      <span class="choice-label" dir="auto">${escapeHTML(label)}</span>
      ${detail ? `<span class="choice-detail" dir="auto">${escapeHTML(detail)}</span>` : ''}
    </button>`;
}

function wireStep(sheet, question) {
  const send = sheet.querySelector('.prompt-send');
  const typed = sheet.querySelector('.prompt-text');

  sheet.querySelectorAll('.choice').forEach((button) => {
    button.addEventListener('click', () => {
      if (button.dataset.option === OTHER_OPTION) {
        openTyping(sheet, button, typed, send);
        return;
      }
      if (typed) typed.hidden = true;
      if (question.multi) {
        button.classList.toggle('picked');
        send.hidden = false;
        return;
      }
      sheet.querySelectorAll('.choice').forEach((b) => b.classList.toggle('picked', b === button));
      answerStep([button.dataset.option]);
    });
  });

  sheet.querySelector('.prompt-dismiss').addEventListener('click', dismissPrompt);

  const back = sheet.querySelector('.prompt-back');
  if (back) {
    back.addEventListener('click', () => {
      openPrompt.index -= 1;
      drawStep();
    });
  }
  send.addEventListener('click', () => answerStep(collect(sheet, typed)));

  if (typed) {
    typed.querySelector('textarea').addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        answerStep(collect(sheet, typed));
      }
    });
  }
}

function openTyping(sheet, button, typed, send) {
  sheet.querySelectorAll('.choice').forEach((b) => b.classList.toggle('picked', b === button));
  typed.hidden = false;
  send.hidden = false;
  typed.querySelector('textarea').focus();
}

// collect reads the answer to the question on screen: what was typed if the
// box is open, otherwise the options marked.
function collect(sheet, typed) {
  if (typed && !typed.hidden) {
    const written = typed.querySelector('textarea').value.trim();
    return written ? [written] : [];
  }

  return [...sheet.querySelectorAll('.choice.picked')]
    .map((b) => b.dataset.option)
    .filter((id) => id !== OTHER_OPTION);
}

// promptSummary is the one line the transcript keeps about a settled prompt.
function promptSummary(choices) {
  const picked = Object.values(choices).flat();
  if (!picked.length) return t('promptDismissed');

  return picked.map((id) => (OPTION_LABELS[id] ? t(OPTION_LABELS[id]) : id)).join(' · ');
}

// promptPending is true while the turn is stopped on a question.
function promptPending() {
  return openPrompt !== null || promptQueue.length > 0;
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
