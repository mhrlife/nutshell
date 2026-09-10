// nutshell UI, "reader" layout: the rail on one side holds the conversation
// (question, spoken summary, controls); the document pane on the other side
// shows the full answer of the selected turn.

const $ = (s) => document.querySelector(s);
const el = {
  transcript: $('#transcript'), empty: $('#empty'), emptyHint: $('#empty-hint'),
  status: $('#status'), brand: $('#brand'), input: $('#input'), talk: $('#talk'),
  settingsBtn: $('#settings-btn'), foot: $('#foot'), footNote: $('#foot-note'), doc: $('#doc'), docBody: $('#doc-body'),
  docEmpty: $('#doc-empty'), docCost: $('#doc-cost'), copy: $('#copy'), docClose: $('#doc-close'),
};

let cfg = { lang: 'en', project: '', agent: 'agent', voice: false };
let state = 'idle'; // idle | listening | transcribing | working
let recorder = null;
let timer = null;
let turns = [];
let selected = null;
let player = null; // { audio, turn }
let pendingStt = null; // cost of the last transcription, charged to the next question
let notice = ''; // transient message shown in the status line

const agentName = () => cfg.agent || 'agent';

function setStatus(text, live) {
  el.status.textContent = text;
  el.status.classList.toggle('live', !!live);
}

function applyLanguage() {
  setActiveLanguage(settings.lang);
  el.brand.textContent = t('brand');
  el.brand.title = cfg.project || '';
  el.emptyHint.textContent = t('hint');
  el.input.placeholder = t('placeholder');
  el.docEmpty.querySelector('p').textContent = t('docEmpty');
  el.copy.innerHTML = ICONS.copy;
  el.copy.title = t('copy');
  el.docClose.innerHTML = ICONS.close;
  el.settingsBtn.innerHTML = ICONS.gear;
  el.settingsBtn.title = t('settings');
  turns.forEach(renderMeta);
  renderCosts();
  renderState();
}

function renderState() {
  const live = state !== 'idle' || !!player;
  const waiting = state === 'working' && promptPending();
  setStatus(offline ? t('offline') : notice || (waiting ? t('promptWaiting') : player ? t('speaking') : t(state)), live);
  el.talk.disabled = state === 'working' || state === 'transcribing';
  el.talk.classList.toggle('listening', state === 'listening');
  el.talk.classList.toggle('transcribing', state === 'transcribing');
  el.talk.innerHTML = { listening: ICONS.stop, transcribing: ICONS.spinner }[state] || ICONS.mic;
  el.talk.title = state === 'listening' ? t('listening') : t('speak');
  el.foot.dataset.state = state;
  setFootNote(waiting ? t('promptNote')
    : { listening: t('listenNote'), transcribing: t('transcribingNote') }[state] || '');
  if (state === 'idle') resizeInput();
}

function setFootNote(text) {
  el.footNote.textContent = text;
  el.footNote.hidden = !text;
}

function flash(message) {
  notice = message;
  renderState();
  setTimeout(() => { if (notice === message) { notice = ''; renderState(); } }, 4000);
}

// ---- turns -----------------------------------------------------------------

function addTurn(id, question) {
  const node = document.getElementById('turn-template').content.firstElementChild.cloneNode(true);
  const turn = { id, question, node, answer: null, error: null, audio: null, loadingAudio: false, cost: newCost() };
  turn.cost.stt = pendingStt;
  pendingStt = null;
  node.querySelector('.q').textContent = question;
  el.empty.hidden = true;
  el.transcript.appendChild(node);
  turns.push(turn);
  select(turn);
  return turn;
}

function showActivity(turn, kind, text) {
  const box = turn.node.querySelector('.activity');
  box.hidden = false;
  box.classList.add('live');
  const think = kind === 'think';
  box.innerHTML = `<span class="k"></span><span class="${think ? 'think' : 'v'}"></span>`;
  box.querySelector('.k').textContent = think ? t('think') : kind.toLowerCase();
  box.querySelector(think ? '.think' : '.v').textContent = text;
  if (think) box.querySelector('.think').dir = 'auto';
  scrollToEnd();
}

function finishTurn(turn, answer) {
  turn.answer = answer;
  turn.node.querySelector('.activity').hidden = true;
  const summary = turn.node.querySelector('.summary');
  summary.textContent = plainText(answer.summary);
  summary.hidden = false;
  turn.node.querySelector('.meta').hidden = false;
  turn.cost.agent = answer.cost_known ? answer.cost_usd : null;
  turn.cost.agentKnown = !!answer.cost_known;
  renderMeta(turn);
  renderCosts();
  if (selected === turn) renderDoc();
  scrollToEnd();
  if (synced && settings.autoSpeak && cfg.voice) speak(turn);
}

function failTurn(turn, message) {
  turn.error = message;
  turn.node.querySelector('.activity').hidden = true;
  const box = turn.node.querySelector('.error');
  box.textContent = message;
  box.hidden = false;
  scrollToEnd();
}

function renderMeta(turn) {
  if (!turn.answer) return;
  const speakBtn = turn.node.querySelector('.speak');
  const playing = player && player.turn === turn && !player.audio.paused;
  speakBtn.hidden = !cfg.voice;
  speakBtn.innerHTML = turn.loadingAudio ? ICONS.spinner : (playing ? ICONS.pause : ICONS.play);
  speakBtn.title = turn.loadingAudio ? t('loading') : (playing ? t('pause') : (turn.audio ? t('replay') : t('speak')));
  turn.node.querySelector('.open-full').textContent = t('fullAnswer');
}

function renderCosts() {
  for (const turn of turns) renderCostChip(turn.node.querySelector('.cost'), turn.cost, t('costTurn'), agentName());
  renderSessionCost(turns, agentName());
  if (selected) renderCostChip(el.docCost, selected.cost, t('costTurn'), agentName());
}

function select(turn) {
  if (selected) selected.node.classList.remove('selected');
  selected = turn;
  if (turn) turn.node.classList.add('selected');
  renderDoc();
}

function renderDoc() {
  const has = selected && selected.answer;
  el.docBody.hidden = !has;
  el.docEmpty.hidden = !!has;
  el.copy.hidden = !has;
  el.docCost.hidden = !has;
  if (!has) return;
  el.docBody.innerHTML = marked.parse(selected.answer.full || '');
  el.docBody.dir = isRTL(selected.answer.full) ? 'rtl' : 'ltr';
  renderCostChip(el.docCost, selected.cost, t('costTurn'), agentName());
  el.doc.scrollTop = 0;
}

function scrollToEnd() {
  el.transcript.scrollTop = el.transcript.scrollHeight;
}

// ---- voice in --------------------------------------------------------------

async function startListening() {
  if (!cfg.voice) { flash(t('noVoice')); return; }
  stopPlayback(); // before asking for the mic: the answer must not talk over you
  recorder = new Recorder();
  try {
    await recorder.start();
  } catch {
    recorder = null;
    flash(t('micDenied'));
    return;
  }
  state = 'listening';
  renderState();
  const start = Date.now();
  timer = setInterval(() => setStatus(`${t('listening')} ${fmtTime(Date.now() - start)}`, true), 250);
}

async function stopListening() {
  clearInterval(timer);
  const rec = recorder;
  recorder = null;
  state = 'transcribing';
  renderState();
  try {
    const wav = await rec.stop();
    if (!wav) return;
    const lang = languageByCode(settings.lang) || {};
    const resp = await fetch('/api/transcribe', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ audio: wav, format: 'wav', hint: lang.sttHint || '' }),
    });
    if (!resp.ok) throw new Error((await resp.json()).error || resp.statusText);
    const { text, cost_usd: cost } = await resp.json();
    pendingStt = cost || 0;
    if (!text) return;
    if (settings.autoSend) {
      state = 'idle';
      ask(text);
    } else {
      el.input.value = text;
      resizeInput();
      el.input.focus();
      flash(t('hintEdit'));
    }
  } catch (err) {
    flash(err.message);
  } finally {
    if (state === 'transcribing') { state = 'idle'; renderState(); }
  }
}

function cancelListening() {
  clearInterval(timer);
  if (recorder) recorder.cancel();
  recorder = null;
  state = 'idle';
  renderState();
}

// ---- voice out -------------------------------------------------------------

async function speak(turn) {
  if (!cfg.voice || !turn.answer) return;
  if (player && player.turn === turn) {
    if (player.audio.paused) player.audio.play(); else player.audio.pause();
    renderMeta(turn);
    return;
  }
  stopPlayback();
  if (!turn.audio) {
    turn.loadingAudio = true;
    renderMeta(turn);
    try {
      const resp = await fetch('/api/speak', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: plainText(turn.answer.summary) }),
      });
      if (!resp.ok) throw new Error((await resp.json()).error || resp.statusText);
      turn.audio = URL.createObjectURL(await resp.blob());
      priceClip(turn, resp.headers.get('X-Generation-Id'));
    } catch (err) {
      failTurn(turn, err.message);
      return;
    } finally {
      turn.loadingAudio = false;
    }
  }
  const audio = new Audio(turn.audio);
  player = { audio, turn };
  const refresh = () => { renderMeta(turn); renderState(); };
  audio.onplay = refresh;
  audio.onpause = refresh;
  audio.onended = () => { player = null; refresh(); };
  audio.play();
  refresh();
}

// priceClip fetches the clip's cost once OpenRouter has priced it (a few seconds later).
async function priceClip(turn, id) {
  if (!id) return;
  try {
    const resp = await fetch(`/api/cost?id=${encodeURIComponent(id)}`);
    if (!resp.ok) return;
    turn.cost.tts = (await resp.json()).cost_usd || 0;
    renderCosts();
  } catch { /* cost stays unknown */ }
}

function stopPlayback() {
  if (!player) return;
  const { audio, turn } = player;
  player = null;
  audio.pause();
  renderMeta(turn);
  renderState();
}

// ---- input & keys ----------------------------------------------------------

function resizeInput() {
  el.input.dir = dirFor(el.input.value);
  if (el.input.offsetParent === null) { // hidden by the footer state: measuring would lock it at 0px
    el.input.style.height = '';
    return;
  }
  el.input.style.height = 'auto';
  el.input.style.height = `${el.input.scrollHeight}px`;
  el.input.style.overflowY = el.input.scrollHeight > 120 ? 'auto' : 'hidden';
}

function onTalk() {
  if (state === 'listening') stopListening();
  else if (state === 'idle') startListening();
}

function openDoc() { el.doc.classList.add('open'); }

function closeDoc() { el.doc.classList.remove('open'); }

document.addEventListener('keydown', (e) => {
  const typing = e.target === el.input || e.target.tagName === 'SELECT';
  if (e.key === 'Escape') {
    if (promptPending()) dismissPrompt();
    else if (!document.getElementById('settings').hidden) closeSettings();
    else if (state === 'listening') cancelListening();
    else if (state === 'working') fetch('/api/cancel', { method: 'POST' });
    else if (el.doc.classList.contains('open')) closeDoc();
    else if (typing) el.input.blur();
    return;
  }
  if (typing) {
    if (e.key === 'Enter' && !e.shiftKey && e.target === el.input) { e.preventDefault(); ask(el.input.value); }
    return;
  }
  if (e.key === ' ' && !e.repeat) { e.preventDefault(); onTalk(); }
  else if (e.key === 'f' && selected && selected.answer) openDoc();
});

el.input.addEventListener('input', resizeInput);
el.talk.addEventListener('click', onTalk);
el.settingsBtn.addEventListener('click', toggleSettings);
el.docClose.addEventListener('click', closeDoc);
el.copy.addEventListener('click', () => {
  if (!selected || !selected.answer) return;
  navigator.clipboard.writeText(selected.answer.full).then(() => {
    el.copy.innerHTML = ICONS.check;
    setTimeout(() => { el.copy.innerHTML = ICONS.copy; }, 1500);
  });
});

el.transcript.addEventListener('click', (e) => {
  const node = e.target.closest('.turn');
  const turn = turns.find((x) => x.node === node);
  if (!turn) return;
  if (e.target.closest('.speak')) { speak(turn); return; }
  select(turn);
  if (e.target.closest('.open-full')) openDoc();
});

// plainText strips the Markdown a model sometimes leaves in a summary meant to be spoken.
function plainText(s) {
  return (s || '').replace(/```[\s\S]*?```/g, ' ').replace(/[`*_~#>]/g, '').replace(/\s+/g, ' ').trim();
}

function fmtTime(ms) {
  const s = Math.floor(ms / 1000);
  return num(`${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`);
}

// ---- boot ------------------------------------------------------------------

(async () => {
  try {
    cfg = await (await fetch('/api/config')).json();
  } catch { /* offline defaults */ }
  await loadSettings(cfg.lang);
  marked.setOptions({ gfm: true, breaks: false });
  applyLanguage();
  renderSettingsPanel(applyLanguage);
  renderDoc();
  connect();
  if (!cfg.voice) flash(t('noVoice'));
})();
